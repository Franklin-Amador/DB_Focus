package catalog

import "strings"

func New() *Catalog {
	c := &Catalog{
		tables:     make(map[string]map[string]*Table),
		procedures: make(map[string]*Procedure),
		triggers:   make(map[string][]*Trigger),
		jobs:       make(map[string]*Job),
	}
	// Siempre crear el schema public
	c.tables["public"] = make(map[string]*Table)
	c.initSystemCatalog()
	return c
}

// GetInformationSchemaTables returns information_schema.tables data based on actual catalog tables
func (c *Catalog) GetInformationSchemaTables() [][]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var rows [][]interface{}
	for schema, tables := range c.tables {
		for name := range tables {
			// Skip pg_catalog tables
			if strings.HasPrefix(name, "pg_catalog.") {
				continue
			}
			rows = append(rows, []interface{}{
				"focusdb",    // table_catalog
				schema,       // table_schema
				name,         // table_name
				"BASE TABLE", // table_type
			})
		}
	}
	return rows
}

// GetInformationSchemaColumns returns information_schema.columns data based on actual catalog columns
func (c *Catalog) GetInformationSchemaColumns() [][]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var rows [][]interface{}
	for schema, tables := range c.tables {
		for tName, table := range tables {
			// Skip pg_catalog tables
			if strings.HasPrefix(tName, "pg_catalog.") {
				continue
			}
			for i, col := range table.Columns {
				rows = append(rows, []interface{}{
					"focusdb",    // table_catalog
					schema,       // table_schema
					tName,        // table_name
					col.Name,     // column_name
					int32(i + 1), // ordinal_position
					col.Type,     // data_type
				})
			}
		}
	}
	return rows
}
