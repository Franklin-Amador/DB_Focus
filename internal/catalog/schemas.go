package catalog

import "sort"

// systemSchemas are the namespaces owned by the engine. They are hidden from
// schema listings and can never be dropped.
var systemSchemas = map[string]bool{
	"pg_catalog":         true,
	"information_schema": true,
	"pg_toast":           true,
	"focus":              true, // internal registry (focus.users)
}

// IsSystemSchema reports whether name is an engine-owned namespace.
func IsSystemSchema(name string) bool {
	return systemSchemas[name]
}

// IsProtectedSchema reports whether name can never be dropped: the system
// schemas plus "public", the default namespace every session falls back to.
func IsProtectedSchema(name string) bool {
	return name == "public" || IsSystemSchema(name)
}

// SchemaInfo summarizes a user-visible schema.
type SchemaInfo struct {
	Name   string
	Tables int
	Views  int
}

// ListSchemas returns every user-visible schema (system schemas excluded)
// with its table and view counts, sorted by name with "public" first.
func (c *Catalog) ListSchemas() []SchemaInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]SchemaInfo, 0, len(c.tables))
	for name, tables := range c.tables {
		if IsSystemSchema(name) {
			continue
		}
		n := 0
		for tname := range tables {
			if !isSystemTable(tname) {
				n++
			}
		}
		out = append(out, SchemaInfo{Name: name, Tables: n, Views: len(c.views[name])})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Name == "public") != (out[j].Name == "public") {
			return out[i].Name == "public"
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// SchemaExists reports whether a schema namespace exists in the catalog.
func (c *Catalog) SchemaExists(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.tables[name]
	return ok
}
