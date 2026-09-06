package catalog

// New returns the default database of a fresh cluster. Code that only needs
// one database (tests, integration programs) keeps working unchanged, while
// CREATE DATABASE and friends reach the cluster through Catalog.Cluster().
func New() *Catalog {
	return NewCluster().Default()
}

// newCatalog builds the catalog of one database: public schema + system catalog.
func newCatalog(name string, cl *Cluster) *Catalog {
	c := &Catalog{
		name:       name,
		cluster:    cl,
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

// resolveDatabaseSchema maps the session context passed to the system-query
// handlers to a schema name. Callers pass the session schema ("public" by
// default); the historical database aliases "" and "postgres" also mean public.
func resolveDatabaseSchema(currentDatabase string) string {
	if currentDatabase == "" || currentDatabase == DefaultDatabase {
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
			if isSystemTable(name) {
				continue
			}
			rows = append(rows, []interface{}{
				c.Name(),     // table_catalog
				schema,       // table_schema
				name,         // table_name
				"BASE TABLE", // table_type
			})
		}
	}

	if views, ok := c.views[targetSchema]; ok {
		for name := range views {
			rows = append(rows, []interface{}{
				c.Name(),     // table_catalog
				targetSchema, // table_schema
				name,         // table_name
				"VIEW",       // table_type
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
			if isSystemTable(tName) {
				continue
			}
			for i, col := range table.Columns {
				rows = append(rows, []interface{}{
					c.Name(),     // table_catalog
					schema,       // table_schema
					tName,        // table_name
					col.Name,     // column_name
					int32(i + 1), // ordinal_position
					col.Type,     // data_type
				})
			}
		}
	}

	if views, ok := c.views[targetSchema]; ok {
		for vName, view := range views {
			for i, col := range view.Columns {
				rows = append(rows, []interface{}{
					c.Name(),
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
