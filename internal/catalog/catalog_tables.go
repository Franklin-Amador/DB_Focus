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

// CreateSchema creates a new schema namespace in the catalog.
func (c *Catalog) CreateSchema(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if name == "" {
		return fmt.Errorf("schema name cannot be empty")
	}
	if _, ok := c.tables[name]; ok {
		return fmt.Errorf("schema %s already exists", name)
	}
	c.tables[name] = make(map[string]*Table)
	return nil
}

// DropSchema removes a schema and all its tables from the catalog.
func (c *Catalog) DropSchema(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if name == "" {
		return fmt.Errorf("schema name cannot be empty")
	}
	if _, ok := c.tables[name]; !ok {
		return fmt.Errorf("schema %s does not exist", name)
	}
	delete(c.tables, name)
	return nil
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

// HasForeignKeyDependents checks whether any table has a FK referencing the target table.
func (c *Catalog) HasForeignKeyDependents(schema, tableName string) (bool, string) {
	allTables := c.GetAllTables()
	targetQualified := schema + "." + tableName

	for srcQualified, table := range allTables {
		if srcQualified == targetQualified {
			continue
		}
		for _, constraint := range table.Constraints {
			if constraint.Type != constants.ConstraintForeignKey {
				continue
			}
			ref := constraint.ReferencedTable
			if ref == tableName || ref == targetQualified {
				return true, srcQualified
			}
		}
	}

	return false, ""
}

// checkColumnForeignKeyReferences verifies if a column is referenced by any foreign key
func (c *Catalog) checkColumnForeignKeyReferences(tableName, columnName, schema string) error {
	allTables := c.GetAllTables()
	targetQualified := schema + "." + tableName

	// Check if any other table has a FK pointing to this column
	for srcQualified, table := range allTables {
		if srcQualified == targetQualified {
			continue
		}
		for _, constraint := range table.Constraints {
			if constraint.Type == constants.ConstraintForeignKey {
				ref := constraint.ReferencedTable
				refCol := constraint.ReferencedCol

				// Check if this FK references the column we're trying to drop
				if (ref == tableName || ref == targetQualified) && refCol == columnName {
					return fmt.Errorf("cannot drop column %s: it is referenced by foreign key in table %s column %s",
						columnName, srcQualified, constraint.ColumnName)
				}
			}
		}
	}

	return nil
}

// AddColumn adds a new column to an existing table
func (c *Catalog) AddColumn(tableName string, column *Column, schemaOpt ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema := "public"
	if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	table, err := c.getTableUnlocked(tableName, schema)
	if err != nil {
		return err
	}

	// Check if column already exists
	for _, col := range table.Columns {
		if col.Name == column.Name {
			return fmt.Errorf("column %s already exists in table %s.%s", column.Name, schema, tableName)
		}
	}

	table.Columns = append(table.Columns, *column)
	return nil
}

// AddColumnWithConstraint adds a column and its constraint to a table
func (c *Catalog) AddColumnWithConstraint(tableName string, column *Column, constraint *Constraint, schemaOpt ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema := "public"
	if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	table, err := c.getTableUnlocked(tableName, schema)
	if err != nil {
		return err
	}

	// Check if column already exists
	for _, col := range table.Columns {
		if col.Name == column.Name {
			return fmt.Errorf("column %s already exists in table %s.%s", column.Name, schema, tableName)
		}
	}

	// If adding PRIMARY KEY, check if one already exists
	if constraint != nil && constraint.Type == constants.ConstraintPrimaryKey {
		for _, existingConstraint := range table.Constraints {
			if existingConstraint.Type == constants.ConstraintPrimaryKey {
				return fmt.Errorf("table %s.%s already has a primary key on column %s", schema, tableName, existingConstraint.ColumnName)
			}
		}
	}

	table.Columns = append(table.Columns, *column)
	if constraint != nil {
		table.Constraints = append(table.Constraints, *constraint)
	}
	return nil
}

// DropColumn removes a column from an existing table
func (c *Catalog) DropColumn(tableName string, columnName string, schemaOpt ...string) error {
	schema := "public"
	if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	// Check constraints BEFORE acquiring lock to avoid deadlock
	// Check if column is referenced by any foreign key
	if err := c.checkColumnForeignKeyReferences(tableName, columnName, schema); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	table, err := c.getTableUnlocked(tableName, schema)
	if err != nil {
		return err
	}

	// Check if column is a primary key
	for _, constraint := range table.Constraints {
		if constraint.Type == constants.ConstraintPrimaryKey && constraint.ColumnName == columnName {
			return fmt.Errorf("cannot drop column %s: it is a primary key", columnName)
		}
	}

	// Find and remove column
	found := false
	newColumns := make([]Column, 0, len(table.Columns))
	for _, col := range table.Columns {
		if col.Name == columnName {
			found = true
		} else {
			newColumns = append(newColumns, col)
		}
	}

	if !found {
		return fmt.Errorf("column %s does not exist in table %s.%s", columnName, schema, tableName)
	}

	table.Columns = newColumns

	// Also remove the column data from all rows
	table.Mu().Lock()
	defer table.Mu().Unlock()

	// Find column index to remove
	colIdx := -1
	for i, col := range table.Columns {
		if col.Name == columnName {
			colIdx = i
			break
		}
	}

	// Remove column from each row if found
	if colIdx >= 0 && colIdx < len(table.Columns) {
		for i := range table.Rows {
			if colIdx < len(table.Rows[i]) {
				table.Rows[i] = append(table.Rows[i][:colIdx], table.Rows[i][colIdx+1:]...)
			}
		}
	}

	return nil
}

// AlterColumnType changes the type of an existing column
func (c *Catalog) AlterColumnType(tableName string, columnName string, newType string, schemaOpt ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema := "public"
	if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	table, err := c.getTableUnlocked(tableName, schema)
	if err != nil {
		return err
	}

	// Find and update column type
	found := false
	for i := range table.Columns {
		if table.Columns[i].Name == columnName {
			table.Columns[i].Type = newType
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("column %s does not exist in table %s.%s", columnName, schema, tableName)
	}

	return nil
}

// RenameColumn renames a column in an existing table
func (c *Catalog) RenameColumn(tableName string, oldName string, newName string, schemaOpt ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema := "public"
	if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	table, err := c.getTableUnlocked(tableName, schema)
	if err != nil {
		return err
	}

	// Check if new name already exists
	for _, col := range table.Columns {
		if col.Name == newName {
			return fmt.Errorf("column %s already exists in table %s.%s", newName, schema, tableName)
		}
	}

	// Find and rename column
	found := false
	for i := range table.Columns {
		if table.Columns[i].Name == oldName {
			table.Columns[i].Name = newName
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("column %s does not exist in table %s.%s", oldName, schema, tableName)
	}

	return nil
}

// DatabaseExists checks if a database exists in pg_catalog.pg_database
func (c *Catalog) DatabaseExists(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Get pg_database table
	pgDatabase, exists := c.tables["pg_catalog"]["pg_database"]
	if !exists {
		return false
	}

	// Find column index for datname
	for i, col := range pgDatabase.Columns {
		if col.Name == "datname" {
			// Check if any row has this database name
			for _, row := range pgDatabase.Rows {
				if row[i] == name {
					return true
				}
			}
			break
		}
	}
	return false
}
