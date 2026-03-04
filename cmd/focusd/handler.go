package main

import (
	"context"
	"fmt"
	"strings"

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
	// 1. Intercept system catalog queries (pg_catalog, information_schema, etc.)
	//    These are handled before the parser to avoid complexity.
	if result, ok := h.catalog.HandleSystemQuery(query); ok {
		return &server.QueryResult{
			Columns: result.Columns,
			Rows:    result.Rows,
			Tag:     result.Tag,
		}, nil
	}

	// 2. Rewrite system functions to literals the parser can handle
	query = rewriteSystemFunctions(query)

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

		result, err := h.executor.Execute(context.Background(), stmt)
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

// rewriteSystemFunctions replaces PostgreSQL built-in functions/keywords
// that the parser doesn't support with literal equivalents.
var systemFunctionRewrites = map[string]string{
	"current_user":       "'postgres'",
	"current_database()": "'postgres'",
	"pg_backend_pid()":   "0",
}

func rewriteSystemFunctions(query string) string {
	result := query
	upper := strings.ToUpper(query)
	for pattern, replacement := range systemFunctionRewrites {
		if !strings.Contains(upper, strings.ToUpper(pattern)) {
			continue
		}
		result = replaceAllCaseInsensitive(result, pattern, replacement)
		upper = strings.ToUpper(result)
	}
	return result
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

// userInfoResult returns the focus.users table contents
func userInfoResult(cat *catalog.Catalog) *server.QueryResult {
	userTable, err := cat.GetTable("focus.users")
	if err != nil {
		return &server.QueryResult{
			Columns: []string{"username", "superuser", "created_at"},
			Rows:    [][]interface{}{},
			Tag:     "SELECT 0",
		}
	}
	allRows := userTable.SelectAll()
	rows := make([][]interface{}, len(allRows))
	copy(rows, allRows)
	return &server.QueryResult{
		Columns: []string{"username", "superuser", "created_at"},
		Rows:    rows,
		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
	}
}
