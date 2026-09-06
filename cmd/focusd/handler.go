package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
	"dbf/internal/server"
)

// executeHandler implements server.QueryHandler
type executeHandler struct {
	executor *executor.Executor
	catalog  *catalog.Catalog
}

func (h executeHandler) Handle(query string) (*server.QueryResult, error) {
	return h.HandleWithDatabase(query, "postgres")
}

func (h executeHandler) HandleWithDatabase(query string, currentDatabase string) (*server.QueryResult, error) {
	// Wire-protocol path: no request context available, keep prior behavior.
	return h.HandleWithDatabaseCtx(context.Background(), query, currentDatabase)
}

// HandleWithDatabaseCtx is the context-aware execution path. The GUI HTTP
// handlers pass their request context (plus timeout) so long queries are
// cancelled when the client disconnects or the deadline expires.
func (h executeHandler) HandleWithDatabaseCtx(ctx context.Context, query string, currentDatabase string) (*server.QueryResult, error) {
	if currentDatabase == "" {
		currentDatabase = "postgres"
	}

	// 1. Intercept system catalog queries (pg_catalog, information_schema, etc.)
	//    These are handled before the parser to avoid complexity.
	if result, ok := h.catalog.HandleSystemQueryForDatabase(query, currentDatabase); ok {
		return &server.QueryResult{
			Columns: result.Columns,
			Rows:    result.Rows,
			Tag:     result.Tag,
		}, nil
	}

	// 2. Rewrite system functions to literals the parser can handle
	query = rewriteSystemFunctions(query, currentDatabase)

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

		applyDatabaseContext(stmt, currentDatabase)

		result, err := h.executor.Execute(ctx, stmt)
		if err != nil {
			return nil, err
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
func (h executeHandler) HandleScript(ctx context.Context, sql string, maxRows int, currentDatabase string) *scriptResult {
	out := &scriptResult{FailedIndex: -1}

	if currentDatabase == "" {
		currentDatabase = "postgres"
	}
	query := rewriteSystemFunctions(sql, currentDatabase)
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
		applyDatabaseContext(stmt, currentDatabase)

		t0 := time.Now()
		result, err := h.executor.Execute(ctx, stmt)
		elapsed := time.Since(t0).Milliseconds()
		if err != nil {
			out.FailedIndex = idx
			out.FailedSQL = stmtSQL
			out.Err = err
			return out
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
		currentDatabase = "postgres"
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

// applyDatabaseContext qualifies unqualified object references with the
// session's database, which FocusDB maps to a schema namespace ("postgres"
// and "" mean the default "public" schema, where nothing needs qualifying).
func applyDatabaseContext(stmt ast.Statement, currentDatabase string) {
	if currentDatabase == "" || currentDatabase == "postgres" || currentDatabase == "public" {
		return
	}
	ast.ApplyDefaultSchema(stmt, currentDatabase)
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
