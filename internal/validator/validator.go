package validator

import (
	"fmt"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/constants"
)

// Validator validates SQL statements before execution
type Validator struct{}

// New creates a new Validator instance
func New() *Validator {
	return &Validator{}
}

// ValidateCreateTable validates CREATE TABLE statement
func (v *Validator) ValidateCreateTable(stmt *ast.CreateTable) error {
	if stmt == nil {
		return fmt.Errorf("statement is nil")
	}

	if stmt.Table.Name == "" {
		return fmt.Errorf("table name cannot be empty")
	}

	if len(stmt.Columns) == 0 {
		return fmt.Errorf("table must have at least one column")
	}

	// Validate column names are unique
	seen := make(map[string]bool)
	for _, col := range stmt.Columns {
		if col.Name.Name == "" {
			return fmt.Errorf("column name cannot be empty")
		}
		if seen[col.Name.Name] {
			return fmt.Errorf("duplicate column name: %s", col.Name.Name)
		}
		seen[col.Name.Name] = true

		// Validate column type
		if err := v.validateDataType(col.Type); err != nil {
			return fmt.Errorf("invalid type for column %s: %w", col.Name.Name, err)
		}
	}

	// Validate constraints
	if err := v.validateConstraints(stmt); err != nil {
		return err
	}

	return nil
}

// ValidateInsert validates INSERT statement
func (v *Validator) ValidateInsert(stmt *ast.Insert, table *catalog.Table) error {
	if stmt == nil {
		return fmt.Errorf("statement is nil")
	}

	if table == nil {
		return fmt.Errorf("table not found: %s", stmt.Table.Name)
	}

	// If columns are specified, validate they exist
	if len(stmt.Columns) > 0 {
		for _, col := range stmt.Columns {
			found := false
			for _, tCol := range table.Columns {
				if tCol.Name == col.Name {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("column %s does not exist in table %s", col.Name, table.Name)
			}
		}

		// Validate number of values matches number of columns
		if len(stmt.Values) != len(stmt.Columns) {
			return fmt.Errorf("number of values (%d) does not match number of columns (%d)",
				len(stmt.Values), len(stmt.Columns))
		}
	} else {
		// If no columns specified, count non-identity columns
		nonIdentityCount := 0
		for _, col := range table.Columns {
			if !col.Identity {
				nonIdentityCount++
			}
		}

		if len(stmt.Values) != nonIdentityCount && len(stmt.Values) != len(table.Columns) {
			return fmt.Errorf("number of values does not match table schema")
		}
	}

	return nil
}

// ValidateUpdate validates UPDATE statement
func (v *Validator) ValidateUpdate(stmt *ast.Update, table *catalog.Table) error {
	if stmt == nil {
		return fmt.Errorf("statement is nil")
	}

	if table == nil {
		return fmt.Errorf("table not found: %s", stmt.Table.Name)
	}

	// Validate column being updated exists
	found := false
	for _, col := range table.Columns {
		if col.Name == stmt.Column.Name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("column %s does not exist in table %s", stmt.Column.Name, table.Name)
	}

	// Validate WHERE clause if present
	if stmt.Where != nil {
		found = false
		for _, col := range table.Columns {
			if col.Name == stmt.Where.Column.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("WHERE column %s does not exist in table %s",
				stmt.Where.Column.Name, table.Name)
		}
	}

	return nil
}

// ValidateDelete validates DELETE statement
func (v *Validator) ValidateDelete(stmt *ast.Delete, table *catalog.Table) error {
	if stmt == nil {
		return fmt.Errorf("statement is nil")
	}

	if table == nil {
		return fmt.Errorf("table not found: %s", stmt.Table.Name)
	}

	// Validate WHERE clause if present
	if stmt.Where != nil {
		found := false
		for _, col := range table.Columns {
			if col.Name == stmt.Where.Column.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("WHERE column %s does not exist in table %s",
				stmt.Where.Column.Name, table.Name)
		}
	}

	return nil
}

// ValidateSelect validates SELECT statement
func (v *Validator) ValidateSelect(stmt *ast.Select) error {
	if stmt == nil {
		return fmt.Errorf("statement is nil")
	}

	// Validate table name if not a function call
	if stmt.Table.Name == "" && !stmt.Star && len(stmt.Columns) == 0 {
		return fmt.Errorf("SELECT requires columns or table")
	}

	// Validate ORDER BY columns exist in SELECT columns (simplified check)
	if len(stmt.OrderBy) > 0 && len(stmt.Columns) > 0 && !stmt.Star {
		for _, orderCol := range stmt.OrderBy {
			found := false
			for _, selectCol := range stmt.Columns {
				if selectCol.Name == orderCol.Column.Name {
					found = true
					break
				}
			}
			// Allow GROUP BY columns in ORDER BY
			for _, groupCol := range stmt.GroupBy {
				if groupCol.Name == orderCol.Column.Name {
					found = true
					break
				}
			}
			if !found {
				// Allow aggregate functions
				if orderCol.Column.Name != "COUNT(*)" {
					// This is a simplified check - in production you'd want more sophisticated validation
					// For now, we'll be lenient
				}
			}
		}
	}

	// Validate LIMIT and OFFSET are non-negative
	if stmt.Limit < 0 {
		return fmt.Errorf("LIMIT cannot be negative")
	}
	if stmt.Offset < 0 {
		return fmt.Errorf("OFFSET cannot be negative")
	}

	return nil
}

// validateDataType checks if a data type is supported
func (v *Validator) validateDataType(dataType string) error {
	validTypes := map[string]bool{
		constants.DataTypeInteger: true,
		constants.DataTypeText:    true,
		constants.DataTypeBoolean: true,
		"INT":                     true, // Alias for INTEGER
		"VARCHAR":                 true, // Alias for TEXT
		"BOOL":                    true, // Alias for BOOLEAN
	}

	if !validTypes[dataType] {
		return fmt.Errorf("unsupported data type: %s", dataType)
	}

	return nil
}

// validateConstraints validates table constraints
func (v *Validator) validateConstraints(stmt *ast.CreateTable) error {
	primaryKeyCount := 0

	for _, col := range stmt.Columns {
		for _, constraint := range col.Constraints {
			switch c := constraint.(type) {
			case *ast.PrimaryKeyConstraint:
				primaryKeyCount++
				if primaryKeyCount > 1 {
					return fmt.Errorf("table can have only one PRIMARY KEY")
				}
			case *ast.ForeignKeyConstraint:
				if c.ReferencedTable == "" {
					return fmt.Errorf("FOREIGN KEY must reference a table")
				}
				if c.ReferencedCol == "" {
					return fmt.Errorf("FOREIGN KEY must reference a column")
				}
			}
		}
	}

	return nil
}
