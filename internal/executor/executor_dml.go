package executor

import (
	"context"
	"fmt"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/constants"
)

// executeInsert handles INSERT statements
func (e *Executor) executeInsert(ctx context.Context, stmt *ast.Insert) (*Result, error) {
	// Determine schema
	schema := ""
	if stmt.Table.Alias != "" {
		schema = stmt.Table.Alias
	}

	// Get table
	table, err := e.catalog.GetTable(stmt.Table.Name, schema)
	if err != nil {
		return nil, fmt.Errorf("table not found: %w", err)
	}

	// Validate statement
	if err := e.validator.ValidateInsert(stmt, table); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Execute BEFORE INSERT triggers
	if e.triggersEnabled {
		qual := stmt.Table.Name
		if schema != "" {
			qual = schema + "." + stmt.Table.Name
		}
		if err := e.executeTriggers(ctx, qual, constants.TriggerBefore, constants.TriggerInsert, nil, nil); err != nil {
			return nil, fmt.Errorf("BEFORE INSERT trigger failed: %w", err)
		}
	}

	// Build final values array
	values := make([]interface{}, len(table.Columns))

	if len(stmt.Columns) > 0 {
		// Columns specified - map values to columns
		// Initialize all values to nil
		for i := range values {
			values[i] = nil
		}

		// Map specified values to their columns
		for i, col := range stmt.Columns {
			colIdx := -1
			for j, tableCol := range table.Columns {
				if tableCol.Name == col.Name {
					colIdx = j
					break
				}
			}
			if colIdx == -1 {
				return nil, fmt.Errorf("column %s not found in table %s", col.Name, stmt.Table.Name)
			}
			values[colIdx] = stmt.Values[i].Value
		}

		// Fill in IDENTITY columns
		for i, col := range table.Columns {
			if col.Identity && values[i] == nil {
				table.Columns[i].IdentityValue++
				values[i] = table.Columns[i].IdentityValue
			}
		}
	} else {
		// No columns specified - values should match table columns in order
		for i, lit := range stmt.Values {
			if table.Columns[i].Identity {
				table.Columns[i].IdentityValue++
				values[i] = table.Columns[i].IdentityValue
			} else {
				values[i] = lit.Value
			}
		}
	}

	// Insert row
	if err := table.InsertRow(values, e.catalog); err != nil {
		return nil, fmt.Errorf("failed to insert row: %w", err)
	}

	// Persist to storage
	if e.storage != nil {
		schemaToUse := "public"
		if schema != "" {
			schemaToUse = schema
		}
		if err := e.storage.SaveTableWithSchema(table, schemaToUse); err != nil {
			fmt.Printf("warning: failed to persist table %s.%s: %v\n", schemaToUse, stmt.Table.Name, err)
		}
	}

	// Execute AFTER INSERT triggers
	if e.triggersEnabled {
		qual := stmt.Table.Name
		if schema != "" {
			qual = schema + "." + stmt.Table.Name
		}
		if err := e.executeTriggers(ctx, qual, constants.TriggerAfter, constants.TriggerInsert, nil, values); err != nil {
			return nil, fmt.Errorf("AFTER INSERT trigger failed: %w", err)
		}
	}

	return &Result{Tag: constants.ResultInsert}, nil
}

// executeUpdate handles UPDATE statements
func (e *Executor) executeUpdate(ctx context.Context, stmt *ast.Update) (*Result, error) {
	// Determine schema
	schema := ""
	if stmt.Table.Alias != "" {
		schema = stmt.Table.Alias
	}

	// Get table
	table, err := e.catalog.GetTable(stmt.Table.Name, schema)
	if err != nil {
		return nil, fmt.Errorf("table not found: %w", err)
	}

	// Validate statement
	if err := e.validator.ValidateUpdate(stmt, table); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Execute BEFORE UPDATE triggers
	if e.triggersEnabled {
		qual := stmt.Table.Name
		if schema != "" {
			qual = schema + "." + stmt.Table.Name
		}
		if err := e.executeTriggers(ctx, qual, constants.TriggerBefore, constants.TriggerUpdate, nil, nil); err != nil {
			return nil, fmt.Errorf("BEFORE UPDATE trigger failed: %w", err)
		}
	}

	// Find column index
	colIdx := findColumnIndex(table.Columns, stmt.Column.Name)
	if colIdx == -1 {
		return nil, fmt.Errorf("column %s not found", stmt.Column.Name)
	}

	// Validate constraints
	if err := e.validateUpdateConstraints(table, stmt, colIdx); err != nil {
		return nil, err
	}

	// Perform update
	updateCount, err := e.performUpdate(table, stmt, colIdx)
	if err != nil {
		return nil, err
	}

	// Persist to storage
	if e.storage != nil {
		schemaToUse := "public"
		if schema != "" {
			schemaToUse = schema
		}
		if err := e.storage.SaveTableWithSchema(table, schemaToUse); err != nil {
			fmt.Printf("warning: failed to persist table %s.%s: %v\n", schemaToUse, stmt.Table.Name, err)
		}
	}

	// Execute AFTER UPDATE triggers
	if e.triggersEnabled {
		qual := stmt.Table.Name
		if schema != "" {
			qual = schema + "." + stmt.Table.Name
		}
		if err := e.executeTriggers(ctx, qual, constants.TriggerAfter, constants.TriggerUpdate, nil, nil); err != nil {
			return nil, fmt.Errorf("AFTER UPDATE trigger failed: %w", err)
		}
	}

	return &Result{Tag: fmt.Sprintf("UPDATE %d", updateCount)}, nil
}

// executeDelete handles DELETE statements
func (e *Executor) executeDelete(ctx context.Context, stmt *ast.Delete) (*Result, error) {
	// Determine schema
	schema := ""
	if stmt.Table.Alias != "" {
		schema = stmt.Table.Alias
	}

	// Get table
	table, err := e.catalog.GetTable(stmt.Table.Name, schema)
	if err != nil {
		return nil, fmt.Errorf("table not found: %w", err)
	}

	// Validate statement
	if err := e.validator.ValidateDelete(stmt, table); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Execute BEFORE DELETE triggers
	if e.triggersEnabled {
		qual := stmt.Table.Name
		if schema != "" {
			qual = schema + "." + stmt.Table.Name
		}
		if err := e.executeTriggers(ctx, qual, constants.TriggerBefore, constants.TriggerDelete, nil, nil); err != nil {
			return nil, fmt.Errorf("BEFORE DELETE trigger failed: %w", err)
		}
	}

	// Find primary key column for FK checks
	pkColName := findPrimaryKeyColumn(table)

	// Perform deletion
	deleteCount, err := e.performDelete(table, stmt, pkColName)
	if err != nil {
		return nil, err
	}

	// Persist to storage
	if e.storage != nil {
		schemaToUse := "public"
		if schema != "" {
			schemaToUse = schema
		}
		if err := e.storage.SaveTableWithSchema(table, schemaToUse); err != nil {
			fmt.Printf("warning: failed to persist table %s.%s: %v\n", schemaToUse, stmt.Table.Name, err)
		}
	}

	// Execute AFTER DELETE triggers
	if e.triggersEnabled {
		qual := stmt.Table.Name
		if schema != "" {
			qual = schema + "." + stmt.Table.Name
		}
		if err := e.executeTriggers(ctx, qual, constants.TriggerAfter, constants.TriggerDelete, nil, nil); err != nil {
			return nil, fmt.Errorf("AFTER DELETE trigger failed: %w", err)
		}
	}

	return &Result{Tag: fmt.Sprintf("DELETE %d", deleteCount)}, nil
}

// Helper functions

func findColumnIndex(columns []catalog.Column, name string) int {
	for i, col := range columns {
		if col.Name == name {
			return i
		}
	}
	return -1
}

func findPrimaryKeyColumn(table *catalog.Table) string {
	for _, constraint := range table.Constraints {
		if constraint.Type == constants.ConstraintPrimaryKey {
			return constraint.ColumnName
		}
	}
	return ""
}

func (e *Executor) validateUpdateConstraints(table *catalog.Table, stmt *ast.Update, colIdx int) error {
	updatingCol := table.Columns[colIdx]

	// NOT NULL validation
	if updatingCol.NotNull && stmt.Value.Value == "" {
		return fmt.Errorf("column %s cannot be NULL", stmt.Column.Name)
	}

	// Foreign key validation
	for _, constraint := range table.Constraints {
		if constraint.Type == constants.ConstraintForeignKey && constraint.ColumnName == stmt.Column.Name {
			if stmt.Value.Value != "" {
				if err := e.validateForeignKey(constraint.ReferencedTable, constraint.ReferencedCol, stmt.Value.Value); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (e *Executor) validateForeignKey(refTable, refCol string, value interface{}) error {
	table, err := e.catalog.GetTable(refTable)
	if err != nil {
		return fmt.Errorf("referenced table %s not found", refTable)
	}

	refColIdx := findColumnIndex(table.Columns, refCol)
	if refColIdx == -1 {
		return fmt.Errorf("referenced column %s not found in table %s", refCol, refTable)
	}

	table.Mu().RLock()
	defer table.Mu().RUnlock()

	for _, row := range table.Rows {
		if row[refColIdx] == value {
			return nil
		}
	}

	return fmt.Errorf("foreign key violation: value %v not found in %s(%s)", value, refTable, refCol)
}

func (e *Executor) performUpdate(table *catalog.Table, stmt *ast.Update, colIdx int) (int, error) {
	table.Mu().Lock()
	defer table.Mu().Unlock()

	updateCount := 0

	if stmt.Where != nil {
		whereColIdx := findColumnIndex(table.Columns, stmt.Where.Column.Name)
		if whereColIdx == -1 {
			return 0, fmt.Errorf("column %s not found", stmt.Where.Column.Name)
		}

		for i, row := range table.Rows {
			if row[whereColIdx] == stmt.Where.Value.Value {
				table.Rows[i][colIdx] = stmt.Value.Value
				updateCount++
			}
		}
	} else {
		// Update all rows
		updateCount = len(table.Rows)
		for i := range table.Rows {
			table.Rows[i][colIdx] = stmt.Value.Value
		}
	}

	return updateCount, nil
}

func (e *Executor) performDelete(table *catalog.Table, stmt *ast.Delete, pkColName string) (int, error) {
	table.Mu().Lock()
	defer table.Mu().Unlock()

	deleteCount := 0

	if stmt.Where != nil {
		whereColIdx := findColumnIndex(table.Columns, stmt.Where.Column.Name)
		if whereColIdx == -1 {
			return 0, fmt.Errorf("column %s not found", stmt.Where.Column.Name)
		}

		// Check FK references if necessary
		if pkColName != "" {
			pkColIdx := findColumnIndex(table.Columns, pkColName)
			if pkColIdx != -1 {
				for _, row := range table.Rows {
					if row[whereColIdx] == stmt.Where.Value.Value {
						if err := e.catalog.CheckForeignKeyReferences(stmt.Table.Name, pkColName, row[pkColIdx]); err != nil {
							return 0, err
						}
					}
				}
			}
		}

		// Perform deletion
		newRows := [][]interface{}{}
		for _, row := range table.Rows {
			if row[whereColIdx] == stmt.Where.Value.Value {
				deleteCount++
			} else {
				newRows = append(newRows, row)
			}
		}
		table.Rows = newRows
	} else {
		// Delete all rows
		if pkColName != "" {
			pkColIdx := findColumnIndex(table.Columns, pkColName)
			if pkColIdx != -1 {
				for _, row := range table.Rows {
					if err := e.catalog.CheckForeignKeyReferences(stmt.Table.Name, pkColName, row[pkColIdx]); err != nil {
						return 0, err
					}
				}
			}
		}

		deleteCount = len(table.Rows)
		table.Rows = [][]interface{}{}
	}

	return deleteCount, nil
}
