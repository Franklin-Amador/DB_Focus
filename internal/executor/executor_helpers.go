package executor

import (
	"context"
	"fmt"

	"dbf/internal/catalog"
)

// checkCtx returns the context error if the context has been cancelled,
// or nil otherwise. It replaces the repeated `select { case <-ctx.Done() }`
// guard that appeared at the top of most statement handlers.
func checkCtx(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// schemaOrPublic resolves an (possibly empty) schema name to the default
// "public" schema. Historically several handlers defaulted an empty schema
// to "" and others to "public"; for persistence the canonical value is
// always "public".
func schemaOrPublic(schema string) string {
	if schema == "" {
		return "public"
	}
	return schema
}

// qualifiedName builds a schema-qualified object name (`schema.name`).
// An empty schema yields the bare name, matching the previous inline logic
// used to address trigger targets.
func qualifiedName(schema, name string) string {
	if schema != "" {
		return schema + "." + name
	}
	return name
}

// persistTable saves a table (and its schema) through the storage backend,
// defaulting an empty schema to "public". It is a no-op when no storage
// backend is configured. Callers decide whether a persistence failure is a
// warning or a hard error.
func (e *Executor) persistTable(table *catalog.Table, schema string) error {
	if e.storage == nil {
		return nil
	}
	return e.storage.SaveTableWithSchema(table, schemaOrPublic(schema))
}

// persistTableWarn persists a table and, on failure, logs the same
// non-fatal warning the handlers previously emitted inline. Use this for the
// call sites whose original behavior was "warn and continue".
func (e *Executor) persistTableWarn(table *catalog.Table, schema string) {
	if err := e.persistTable(table, schema); err != nil {
		fmt.Printf("warning: failed to persist table %s.%s: %v\n", schemaOrPublic(schema), table.Name, err)
	}
}
