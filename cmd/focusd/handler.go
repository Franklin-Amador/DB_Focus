package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
	"dbf/internal/server"
	"dbf/internal/storage"
)

// executeHandler executes SQL for the wire protocol and the GUI against a
// cluster of databases. Each database gets its own executor (and job
// scheduler), created lazily and discarded when the database is dropped.
type executeHandler struct {
	cluster *catalog.Cluster
	storage storage.Backend // bound to the default database; nil = memory only
	ctx     context.Context // lifetime of the job schedulers (nil = no schedulers)

	mu      sync.Mutex
	execs   map[string]*executor.Executor
	cancels map[string]context.CancelFunc
}

// newExecuteHandler wires the executors of every database already present in
// the cluster (starting their job schedulers) and returns the handler.
func newExecuteHandler(ctx context.Context, cl *catalog.Cluster, st storage.Backend) *executeHandler {
	h := &executeHandler{
		cluster: cl,
		storage: st,
		ctx:     ctx,
		execs:   map[string]*executor.Executor{},
		cancels: map[string]context.CancelFunc{},
	}
	for _, info := range cl.ListDatabases() {
		if _, _, err := h.executorFor(info.Name); err != nil {
			fmt.Printf("warning: cannot start database %s: %v\n", info.Name, err)
		}
	}
	return h
}

// executorFor returns the executor and catalog of a database, creating the
// executor (with its storage view and job scheduler) on first use.
func (h *executeHandler) executorFor(database string) (*executor.Executor, *catalog.Catalog, error) {
	if database == "" {
		database = catalog.DefaultDatabase
	}
	cat, ok := h.cluster.Database(database)
	if !ok {
		return nil, nil, fmt.Errorf("database %q does not exist", database)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if exe, ok := h.execs[database]; ok {
		return exe, cat, nil
	}
	var st storage.Backend
	if h.storage != nil {
		st = h.storage.ForDatabase(database)
	}
	exe := executor.New(cat, st)
	if h.ctx != nil {
		jobCtx, cancel := context.WithCancel(h.ctx)
		exe.StartJobScheduler(jobCtx)
		h.cancels[database] = cancel
	}
	h.execs[database] = exe
	return exe, cat, nil
}

// pruneExecutors drops the executors (and stops the schedulers) of databases
// that no longer exist in the cluster.
func (h *executeHandler) pruneExecutors() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for name := range h.execs {
		if h.cluster.DatabaseExists(name) {
			continue
		}
		if cancel, ok := h.cancels[name]; ok {
			cancel()
			delete(h.cancels, name)
		}
		delete(h.execs, name)
	}
}

// ─── server.QueryHandler / DatabaseQueryHandler ──────────────────────────────

func (h *executeHandler) Handle(query string) (*server.QueryResult, error) {
	return h.HandleWithDatabase(query, catalog.DefaultDatabase)
}

func (h *executeHandler) HandleWithDatabase(query string, currentDatabase string) (*server.QueryResult, error) {
	// Wire-protocol path: no request context available, keep prior behavior.
	return h.HandleWithDatabaseCtx(context.Background(), query, currentDatabase)
}

// HandleWithDatabaseCtx executes inside a database with the default "public"
// schema as the session schema.
func (h *executeHandler) HandleWithDatabaseCtx(ctx context.Context, query string, currentDatabase string) (*server.QueryResult, error) {
	return h.HandleQueryCtx(ctx, query, currentDatabase, "public")
}

// HandleQueryCtx is the context-aware execution path: the statements run in
// the given database, and unqualified object names resolve inside schema.
// The GUI HTTP handlers pass their request context (plus timeout) so long
// queries are cancelled when the client disconnects or the deadline expires.
func (h *executeHandler) HandleQueryCtx(ctx context.Context, query string, database string, schema string) (*server.QueryResult, error) {
	if database == "" {
		database = catalog.DefaultDatabase
	}
	if schema == "" {
		schema = "public"
	}
	exe, cat, err := h.executorFor(database)
	if err != nil {
		return nil, err
	}

	// 1. Intercept system catalog queries (pg_catalog, information_schema, etc.)
	//    These are handled before the parser to avoid complexity.
	if result, ok := cat.HandleSystemQueryForDatabase(query, schema); ok {
		return &server.QueryResult{
			Columns: result.Columns,
			Rows:    result.Rows,
			Tag:     result.Tag,
		}, nil
	}

	// 2. Rewrite system functions to literals the parser can handle
	query = rewriteSystemFunctions(query, database)

	// 3. Parse and execute all statements
	p := parser.NewParser(query)
	var lastResult *server.QueryResult

	for !p.AtEOF() {
		stmt, err := p.ParseStatement()
		if err != nil {
			return nil, err
		}
		if stmt == nil {
			continue // bare semicolon
		}

		applySchemaContext(stmt, schema)

		result, err := exe.Execute(ctx, stmt)
		if err != nil {
			return nil, err
		}
		if _, dropped := stmt.(*ast.DropDatabase); dropped {
			h.pruneExecutors()
		}
		lastResult = &server.QueryResult{
			Columns: result.Columns,
			Rows:    result.Rows,
			Tag:     result.Tag,
		}
	}

	if lastResult == nil {
		return &server.QueryResult{Tag: "EMPTY"}, nil
	}
	return lastResult, nil
}

// scriptStatementResult is the outcome of one statement inside a script run.
type scriptStatementResult struct {
	Index     int             `json:"index"`
	SQL       string          `json:"sql"`
	Tag       string          `json:"tag"`
	Columns   []string        `json:"columns"`
	Rows      [][]interface{} `json:"rows"`
	ElapsedMs int64           `json:"elapsedMs"`
	Truncated bool            `json:"truncated,omitempty"`
}

// scriptResult aggregates a full script run: one entry per executed statement,
// stopping at the first failure (FailedIndex == -1 when everything succeeded).
type scriptResult struct {
	Results     []scriptStatementResult
	FailedIndex int
	FailedSQL   string
	Err         error
}

// HandleScript parses and executes every statement in sql, collecting one
// result per statement. Statement boundaries come from the parser itself
// (Parser.Pos), so quoted semicolons and dollar-quoted bodies are attributed
// correctly. Execution stops at the first error; prior statements remain
// applied (there is no transaction support in the engine).
func (h *executeHandler) HandleScript(ctx context.Context, sql string, maxRows int, database string, schema string) *scriptResult {
	out := &scriptResult{FailedIndex: -1}

	if database == "" {
		database = catalog.DefaultDatabase
	}
	if schema == "" {
		schema = "public"
	}
	exe, _, err := h.executorFor(database)
	if err != nil {
		out.FailedIndex = 0
		out.Err = err
		return out
	}

	query := rewriteSystemFunctions(sql, database)
	p := parser.NewParser(query)
	idx := 0

	for !p.AtEOF() {
		start := p.Pos()
		stmt, err := p.ParseStatement()
		end := p.Pos()
		stmtSQL := strings.TrimSpace(sliceBetween(query, start, end))

		if err != nil {
			out.FailedIndex = idx
			out.FailedSQL = stmtSQL
			out.Err = err
			return out
		}
		if stmt == nil {
			continue // bare semicolon
		}
		applySchemaContext(stmt, schema)

		t0 := time.Now()
		result, err := exe.Execute(ctx, stmt)
		elapsed := time.Since(t0).Milliseconds()
		if err != nil {
			out.FailedIndex = idx
			out.FailedSQL = stmtSQL
			out.Err = err
			return out
		}
		if _, dropped := stmt.(*ast.DropDatabase); dropped {
			h.pruneExecutors()
		}

		rows := result.Rows
		truncated := false
		if maxRows > 0 && len(rows) > maxRows {
			rows = rows[:maxRows]
			truncated = true
		}
		out.Results = append(out.Results, scriptStatementResult{
			Index:     idx,
			SQL:       stmtSQL,
			Tag:       result.Tag,
			Columns:   result.Columns,
			Rows:      rows,
			ElapsedMs: elapsed,
			Truncated: truncated,
		})
		idx++
	}
	return out
}

// sliceBetween returns query[start:end] guarding against out-of-range offsets.
func sliceBetween(query string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(query) {
		end = len(query)
	}
	if start >= end {
		return ""
	}
	return query[start:end]
}

// rewriteSystemFunctions replaces PostgreSQL built-in functions/keywords
// that the parser doesn't support with literal equivalents.
var systemFunctionRewrites = map[string]string{
	"current_user":     "'postgres'",
	"pg_backend_pid()": "0",
}

func rewriteSystemFunctions(query string, currentDatabase string) string {
	result := query
	upper := strings.ToUpper(query)
	if currentDatabase == "" {
		currentDatabase = catalog.DefaultDatabase
	}
	result = replaceAllCaseInsensitive(result, "current_database()", fmt.Sprintf("'%s'", currentDatabase))
	upper = strings.ToUpper(result)
	for pattern, replacement := range systemFunctionRewrites {
		if !strings.Contains(upper, strings.ToUpper(pattern)) {
			continue
		}
		result = replaceAllCaseInsensitive(result, pattern, replacement)
		upper = strings.ToUpper(result)
	}
	return result
}

// applySchemaContext qualifies unqualified object references with the
// session schema ("public" needs nothing: it is the engine's default).
func applySchemaContext(stmt ast.Statement, schema string) {
	if schema == "" || schema == "public" {
		return
	}
	ast.ApplyDefaultSchema(stmt, schema)
}

func replaceAllCaseInsensitive(input, pattern, replacement string) string {
	upperInput := strings.ToUpper(input)
	upperPattern := strings.ToUpper(pattern)
	var out strings.Builder
	pos := 0
	for {
		idx := strings.Index(upperInput[pos:], upperPattern)
		if idx == -1 {
			out.WriteString(input[pos:])
			break
		}
		start := pos + idx
		out.WriteString(input[pos:start])
		out.WriteString(replacement)
		pos = start + len(pattern)
	}
	return out.String()
}
