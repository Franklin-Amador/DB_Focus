package server

// QueryHandler handles SQL queries and returns results for the wire layer.
type QueryHandler interface {
	Handle(query string) (*QueryResult, error)
}

// QueryResult is a minimal representation of query results used by pgwire.
type QueryResult struct {
	Columns []string
	Rows    [][]interface{}
	Tag     string
}

// BypassResult represents the result of a bypassed query
type BypassResult struct {
	Columns []string
	Rows    [][]interface{}
	Tag     string
}
