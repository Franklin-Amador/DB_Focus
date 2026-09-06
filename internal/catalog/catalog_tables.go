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
		Indexes:     make(map[string]*Index),
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
	c.views[name] = make(map[string]*View)
	return nil
}

// DropSchema removes a schema and all its tables from the catalog. System
// schemas and "public" are protected and can never be dropped.
func (c *Catalog) DropSchema(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if name == "" {
		return fmt.Errorf("schema name cannot be empty")
	}
	if IsProtectedSchema(name) {
		return fmt.Errorf("cannot drop schema %s: it is a system schema", name)
	}
	if _, ok := c.tables[name]; !ok {
		return fmt.Errorf("schema %s does not exist", name)
	}
	delete(c.tables, name)
	delete(c.views, name)
	return nil
}

// SchemaIsEmpty reports whether a schema has no tables and no views.
func (c *Catalog) SchemaIsEmpty(name string) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tablesInSchema, ok := c.tables[name]
	if !ok {
		return false, fmt.Errorf("schema %s does not exist", name)
	}
	viewsInSchema := c.views[name]

	return len(tablesInSchema) == 0 && len(viewsInSchema) == 0, nil
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

// FindForeignKeyDependents returns qualified table names that reference schema.tableName.
func (c *Catalog) FindForeignKeyDependents(schema, tableName string) []string {
	allTables := c.GetAllTables()
	targetQualified := schema + "." + tableName
	dependents := make([]string, 0)

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
				dependents = append(dependents, srcQualified)
				break
			}
		}
	}

	return dependents
}

// RemoveForeignKeyReferencesToTable removes FK constraints from other tables that reference schema.tableName.
// Returns the qualified names of tables modified.
func (c *Catalog) RemoveForeignKeyReferencesToTable(schema, tableName string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	targetQualified := schema + "." + tableName
	updated := make([]string, 0)

	for srcSchema, tablesInSchema := range c.tables {
		for srcName, table := range tablesInSchema {
			if srcSchema == schema && srcName == tableName {
				continue
			}

			newConstraints := make([]Constraint, 0, len(table.Constraints))
			removed := false
			for _, constraint := range table.Constraints {
				if constraint.Type == constants.ConstraintForeignKey {
					ref := constraint.ReferencedTable
					if ref == tableName || ref == targetQualified {
						removed = true
						continue
					}
				}
				newConstraints = append(newConstraints, constraint)
			}

			if removed {
				table.Constraints = newConstraints
				updated = append(updated, srcSchema+"."+srcName)
			}
		}
	}

	return updated, nil
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

func (c *Catalog) CreateIndex(tableName string, indexName string, columnNames []string, schemaOpt ...string) error {
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

	return table.CreateIndex(indexName, columnNames)
}

func (c *Catalog) DropIndex(tableName string, indexName string, schemaOpt ...string) error {
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

	return table.DropIndex(indexName)
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

	// Find column index before mutating schema.
	colIdx := -1
	for i, col := range table.Columns {
		if col.Name == columnName {
			colIdx = i
			break
		}
	}
	if colIdx == -1 {
		return fmt.Errorf("column %s does not exist in table %s.%s", columnName, schema, tableName)
	}

	// Check if column is a primary key
	for _, constraint := range table.Constraints {
		if constraint.Type == constants.ConstraintPrimaryKey && constraint.ColumnName == columnName {
			return fmt.Errorf("cannot drop column %s: it is a primary key", columnName)
		}
	}

	newColumns := make([]Column, 0, len(table.Columns))
	for _, col := range table.Columns {
		if col.Name != columnName {
			newColumns = append(newColumns, col)
		}
	}

	table.Columns = newColumns

	// Remove indexes defined over the dropped column.
	for name, idx := range table.Indexes {
		for _, idxCol := range idx.ColumnNames {
			if idxCol == columnName {
				delete(table.Indexes, name)
				break
			}
		}
	}

	// Also remove the column data from all rows
	table.Mu().Lock()
	defer table.Mu().Unlock()

	for i := range table.Rows {
		if colIdx < len(table.Rows[i]) {
			table.Rows[i] = append(table.Rows[i][:colIdx], table.Rows[i][colIdx+1:]...)
		}
	}
	table.rebuildIndexesLocked()

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

	for _, idx := range table.Indexes {
		for i := range idx.ColumnNames {
			if idx.ColumnNames[i] == oldName {
				idx.ColumnNames[i] = newName
			}
		}
	}
	table.RebuildIndexes()

	return nil
}

// DatabaseExists checks if a database exists in pg_catalog.pg_database
func (c *Catalog) DatabaseExists(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if name == "" {
		return false
	}
	if name == "postgres" {
		return true
	}

	// In FocusDB, non-system schemas represent database namespaces.
	if _, exists := c.tables[name]; exists {
		if name != "pg_catalog" && name != "information_schema" && name != "pg_toast" {
			return true
		}
	}

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
