package catalog

import (
	"dbf/internal/constants"
	"fmt"
)

func (c *Catalog) CreateTable(name string, columns []Column, constraints []Constraint, schemaOpt ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema := "public"
	tableName := name

	// Parse qualified name (e.g., "pg_catalog.pg_database")
	if parts := splitQualifiedName(name); len(parts) == 2 {
		schema = parts[0]
		tableName = parts[1]
	} else if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	if _, ok := c.tables[schema]; !ok {
		c.tables[schema] = make(map[string]*Table)
	}
	if _, exists := c.tables[schema][tableName]; exists {
		return fmt.Errorf("table %s.%s already exists", schema, tableName)
	}

	c.tables[schema][tableName] = &Table{
		Name:        tableName,
		Columns:     columns,
		Constraints: constraints,
		Rows:        [][]interface{}{},
	}
	return nil
}

func (c *Catalog) DropTable(name string, schemaOpt ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema := "public"
	if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}
	if _, ok := c.tables[schema]; !ok {
		return fmt.Errorf("schema %s does not exist", schema)
	}
	if _, exists := c.tables[schema][name]; !exists {
		return fmt.Errorf("table %s.%s does not exist", schema, name)
	}

	delete(c.tables[schema], name)
	return nil
}

func (c *Catalog) GetTable(name string, schemaOpt ...string) (*Table, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.getTableUnlocked(name, schemaOpt...)
}

// getTableUnlocked is an internal helper that retrieves a table without locking.
// Caller must hold at least a read lock on c.mu.
func (c *Catalog) getTableUnlocked(name string, schemaOpt ...string) (*Table, error) {
	schema := "public"
	tableName := name

	// Parse qualified name (e.g., "pg_catalog.pg_database")
	if parts := splitQualifiedName(name); len(parts) == 2 {
		schema = parts[0]
		tableName = parts[1]
	} else if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	if _, ok := c.tables[schema]; !ok {
		return nil, fmt.Errorf("schema %s does not exist", schema)
	}
	table, exists := c.tables[schema][tableName]
	if !exists {
		return nil, fmt.Errorf("table %s.%s not found", schema, tableName)
	}
	return table, nil
}

func (c *Catalog) TableExists(name string, schemaOpt ...string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	schema := "public"
	if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}
	if _, ok := c.tables[schema]; !ok {
		return false
	}
	_, exists := c.tables[schema][name]
	return exists
}

func (c *Catalog) GetConstraint(constraintType, tableName, colName string, schemaOpt ...string) *Constraint {
	c.mu.RLock()
	defer c.mu.RUnlock()

	schema := "public"
	if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	tablesInSchema, ok := c.tables[schema]
	if !ok {
		return nil
	}

	table, exists := tablesInSchema[tableName]
	if !exists {
		return nil
	}

	for i, constraint := range table.Constraints {
		if constraint.Type == constraintType && constraint.ColumnName == colName {
			return &table.Constraints[i]
		}
	}
	return nil
}

func (c *Catalog) GetAllTables() map[string]*Table {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*Table)
	for schema, tables := range c.tables {
		for name, table := range tables {
			result[schema+"."+name] = table
		}
	}
	return result
}

func (c *Catalog) CheckForeignKeyReferences(tableName, pkColName string, pkValue interface{}) error {
	allTables := c.GetAllTables()

	for tName, table := range allTables {
		if tName == tableName {
			continue
		}

		for _, constraint := range table.Constraints {
			if constraint.Type == constants.ConstraintForeignKey && constraint.ReferencedTable == tableName && constraint.ReferencedCol == pkColName {
				fkColIdx := columnIndex(table.Columns, constraint.ColumnName)
				if fkColIdx == -1 {
					continue
				}

				table.Mu().RLock()
				for _, row := range table.Rows {
					if row[fkColIdx] == pkValue {
						table.Mu().RUnlock()
						return fmt.Errorf("cannot delete record: foreign key reference exists in table %s column %s", tName, constraint.ColumnName)
					}
				}
				table.Mu().RUnlock()
			}
		}
	}

	return nil
}
