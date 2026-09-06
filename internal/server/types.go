package server

import "dbf/internal/catalog"

// CatalogProvider is what the wire layer needs from the object hierarchy:
// database validation at startup, the login registry and system-catalog
// queries answered inside the connection's database. Both *catalog.Cluster
// and a single *catalog.Catalog satisfy it.
type CatalogProvider interface {
	DatabaseExists(name string) bool
	RegisterUser(username string, superuser bool) error
	HandleSystemQueryForDatabase(query, database string) (*catalog.SystemResult, bool)
}

// QueryHandler handles SQL queries and returns results for the wire layer.
type QueryHandler interface {
	Handle(query string) (*QueryResult, error)
}

// DatabaseQueryHandler is an optional extension for handlers that need
// connection-level database context.
type DatabaseQueryHandler interface {
	HandleWithDatabase(query string, database string) (*QueryResult, error)
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
