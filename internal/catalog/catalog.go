package catalog

import "strings"

func New() *Catalog {
	c := &Catalog{
		tables:     make(map[string]map[string]*Table),
		views:      make(map[string]map[string]*View),
		procedures: make(map[string]*Procedure),
		triggers:   make(map[string][]*Trigger),
		jobs:       make(map[string]*Job),
	}
	// Siempre crear el schema public
	c.tables["public"] = make(map[string]*Table)
	c.views["public"] = make(map[string]*View)
	c.initSystemCatalog()
	return c
}

func resolveDatabaseSchema(currentDatabase string) string {
	if currentDatabase == "" || currentDatabase == "postgres" {
		return "public"
	}
	return currentDatabase
}

// GetInformationSchemaTables returns information_schema.tables data based on actual catalog tables
func (c *Catalog) GetInformationSchemaTables() [][]interface{} {
	return c.GetInformationSchemaTablesForDatabase("postgres")
}

// GetInformationSchemaTablesForDatabase returns information_schema.tables for the active database.
func (c *Catalog) GetInformationSchemaTablesForDatabase(currentDatabase string) [][]interface{} {
	if currentDatabase == "" {
		currentDatabase = "postgres"
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	var rows [][]interface{}
	targetSchema := resolveDatabaseSchema(currentDatabase)
	for schema, tables := range c.tables {
		if schema != targetSchema {
			continue
		}
		for name := range tables {
			// Skip pg_catalog tables
			if strings.HasPrefix(name, "pg_catalog.") {
				continue
			}
			rows = append(rows, []interface{}{
				currentDatabase, // table_catalog
				schema,          // table_schema
				name,            // table_name
				"BASE TABLE",    // table_type
			})
		}
	}

	if views, ok := c.views[targetSchema]; ok {
		for name := range views {
			rows = append(rows, []interface{}{
				currentDatabase, // table_catalog
				targetSchema,    // table_schema
				name,            // table_name
				"VIEW",          // table_type
			})
		}
	}
	return rows
}

// GetInformationSchemaColumns returns information_schema.columns data based on actual catalog columns
func (c *Catalog) GetInformationSchemaColumns() [][]interface{} {
	return c.GetInformationSchemaColumnsForDatabase("postgres")
}

// GetInformationSchemaColumnsForDatabase returns information_schema.columns for the active database.
func (c *Catalog) GetInformationSchemaColumnsForDatabase(currentDatabase string) [][]interface{} {
	if currentDatabase == "" {
		currentDatabase = "postgres"
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	var rows [][]interface{}
	targetSchema := resolveDatabaseSchema(currentDatabase)
	for schema, tables := range c.tables {
		if schema != targetSchema {
			continue
		}
		for tName, table := range tables {
			// Skip pg_catalog tables
			if strings.HasPrefix(tName, "pg_catalog.") {
				continue
			}
			for i, col := range table.Columns {
				rows = append(rows, []interface{}{
					currentDatabase, // table_catalog
					schema,          // table_schema
					tName,           // table_name
					col.Name,        // column_name
					int32(i + 1),    // ordinal_position
					col.Type,        // data_type
				})
			}
		}
	}

	if views, ok := c.views[targetSchema]; ok {
		for vName, view := range views {
			for i, col := range view.Columns {
				rows = append(rows, []interface{}{
					currentDatabase,
					targetSchema,
					vName,
					col.Name,
					int32(i + 1),
					col.Type,
				})
			}
		}
	}
	return rows
}
