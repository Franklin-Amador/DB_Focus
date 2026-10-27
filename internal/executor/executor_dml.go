package executor

import (
	"context"
	"fmt"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/constants"
)

func init() {
	registerExec((*Executor).executeInsert)
	registerExec((*Executor).executeUpdate)
	registerExec((*Executor).executeDelete)
}

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
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	// Collect the VALUES tuples. Rows is the multi-row form; fall back to the
	// single Values row for gob-decoded statements created before Rows existed.
	rows := stmt.Rows
	if len(rows) == 0 {
		rows = [][]ast.Literal{stmt.Values}
	}

	qual := qualifiedName(schema, stmt.Table.Name)
	count := 0
	var insertErr error

	for _, rowLits := range rows {
		// BEFORE INSERT triggers (per row, like PostgreSQL FOR EACH ROW).
		if e.triggersEnabled {
			if err := e.executeTriggers(ctx, qual, constants.TriggerBefore, constants.TriggerInsert, nil, nil); err != nil {
				insertErr = fmt.Errorf("BEFORE INSERT trigger failed: %w", err)
				break
			}
		}

		values, err := e.buildInsertValues(table, stmt.Columns, rowLits)
		if err != nil {
			insertErr = err
			break
		}

		if err := table.InsertRow(values, e.catalog); err != nil {
			insertErr = fmt.Errorf("failed to insert row: %w", err)
			break
		}
		count++

		// AFTER INSERT triggers (per row).
		if e.triggersEnabled {
			if err := e.executeTriggers(ctx, qual, constants.TriggerAfter, constants.TriggerInsert, nil, values); err != nil {
				insertErr = fmt.Errorf("AFTER INSERT trigger failed: %w", err)
				break
			}
		}
	}

	// Persist whatever was inserted (keeps memory and disk consistent even if a
	// later row in the batch failed; there are no transactions).
	if count > 0 {
		e.persistTableWarn(table, schema)
	}
	if insertErr != nil {
		return nil, insertErr
	}

	if count == 1 {
		return &Result{Tag: constants.ResultInsert}, nil
	}
	return &Result{Tag: fmt.Sprintf("INSERT 0 %d", count)}, nil
}

// buildInsertValues maps one VALUES tuple to a full row aligned with the
// table's columns, auto-generating IDENTITY columns. It supports an explicit
// column list, a full positional tuple, or a positional tuple covering only
// the non-IDENTITY columns.
func (e *Executor) buildInsertValues(table *catalog.Table, columns []ast.Identifier, rowLits []ast.Literal) ([]interface{}, error) {
	values := make([]interface{}, len(table.Columns))

	if len(columns) > 0 {
		if len(rowLits) != len(columns) {
			return nil, fmt.Errorf("number of values (%d) does not match number of columns (%d)", len(rowLits), len(columns))
		}
		// Map specified values to their columns (others stay nil).
		for i, col := range columns {
			colIdx := findColumnIndex(table.Columns, col.Name)
			if colIdx == -1 {
				return nil, fmt.Errorf("column %s not found in table %s", col.Name, table.Name)
			}
			values[colIdx] = rowLits[i].Value
		}
		// Fill in IDENTITY columns that were not provided.
		for i, col := range table.Columns {
			if col.Identity && values[i] == nil {
				table.Columns[i].IdentityValue++
				values[i] = table.Columns[i].IdentityValue
			}
		}
		return values, nil
	}

	// No column list. Either a full positional tuple, or one covering only the
	// non-IDENTITY columns (IDENTITY auto-generated).
	nonIdentityCount := 0
	for _, col := range table.Columns {
		if !col.Identity {
			nonIdentityCount++
		}
	}

	switch len(rowLits) {
	case len(table.Columns):
		for i, lit := range rowLits {
			values[i] = lit.Value
		}
	case nonIdentityCount:
		valueIdx := 0
		for i, col := range table.Columns {
			if col.Identity {
				table.Columns[i].IdentityValue++
				values[i] = table.Columns[i].IdentityValue
				continue
			}
			values[i] = rowLits[valueIdx].Value
			valueIdx++
		}
	default:
		return nil, fmt.Errorf("number of values does not match table schema")
	}
	return values, nil
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
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	// Execute BEFORE UPDATE triggers
	if e.triggersEnabled {
		qual := qualifiedName(schema, stmt.Table.Name)
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
	table.RebuildIndexes()

	// Persist to storage
	e.persistTableWarn(table, schema)

	// Execute AFTER UPDATE triggers
	if e.triggersEnabled {
		qual := qualifiedName(schema, stmt.Table.Name)
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
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	// Execute BEFORE DELETE triggers
	if e.triggersEnabled {
		qual := qualifiedName(schema, stmt.Table.Name)
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
	table.RebuildIndexes()

	// Persist to storage
	e.persistTableWarn(table, schema)

	// Execute AFTER DELETE triggers
	if e.triggersEnabled {
		qual := qualifiedName(schema, stmt.Table.Name)
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
		if catalog.ValuesEqual(row[refColIdx], value) {
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
		resolve := columnResolver(table)
		for i, row := range table.Rows {
			ok, err := evalWhere(stmt.Where, row, resolve)
			if err != nil {
				return 0, err
			}
			if ok {
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

// columnResolver returns a resolver that maps a column name to its index in
// the given table, for use with evalWhere.
func columnResolver(table *catalog.Table) func(string) (int, bool) {
	return func(name string) (int, bool) {
		i := findColumnIndex(table.Columns, name)
		return i, i != -1
	}
}

func (e *Executor) performDelete(table *catalog.Table, stmt *ast.Delete, pkColName string) (int, error) {
	table.Mu().Lock()
	defer table.Mu().Unlock()

	deleteCount := 0

	if stmt.Where != nil {
		resolve := columnResolver(table)

		// Check FK references if necessary
		if pkColName != "" {
			pkColIdx := findColumnIndex(table.Columns, pkColName)
			if pkColIdx != -1 {
				for _, row := range table.Rows {
					matched, err := evalWhere(stmt.Where, row, resolve)
					if err != nil {
						return 0, err
					}
					if matched {
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
			matched, err := evalWhere(stmt.Where, row, resolve)
			if err != nil {
				return 0, err
			}
			if matched {
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
