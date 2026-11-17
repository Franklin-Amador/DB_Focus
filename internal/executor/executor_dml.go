package executor

import (
	"context"
	"fmt"
	"strings"
	"time"

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

		// Enforce CHECK constraints against the full row before inserting it.
		if err := e.enforceRowChecks(table, values); err != nil {
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
		// Map specified values to their columns; track which were provided so an
		// explicit NULL is not later overwritten by a column DEFAULT.
		provided := make([]bool, len(table.Columns))
		for i, col := range columns {
			colIdx := findColumnIndex(table.Columns, col.Name)
			if colIdx == -1 {
				return nil, fmt.Errorf("column %s not found in table %s", col.Name, table.Name)
			}
			values[colIdx] = literalCell(rowLits[i])
			provided[colIdx] = true
		}
		// Fill in IDENTITY columns and DEFAULT values for columns not provided.
		for i, col := range table.Columns {
			if provided[i] {
				continue
			}
			if col.Identity {
				table.Columns[i].IdentityValue++
				values[i] = table.Columns[i].IdentityValue
				continue
			}
			if col.DefaultKind != "" {
				values[i] = defaultCell(col)
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
			values[i] = literalCell(lit)
		}
	case nonIdentityCount:
		valueIdx := 0
		for i, col := range table.Columns {
			if col.Identity {
				table.Columns[i].IdentityValue++
				values[i] = table.Columns[i].IdentityValue
				continue
			}
			values[i] = literalCell(rowLits[valueIdx])
			valueIdx++
		}
	default:
		return nil, fmt.Errorf("number of values does not match table schema")
	}
	return values, nil
}

// defaultCell evaluates a column's DEFAULT into the value to store. Literal
// defaults use their text; the CURRENT_TIMESTAMP / NOW / LOCALTIMESTAMP markers
// evaluate to the current time at insert.
func defaultCell(col catalog.Column) interface{} {
	switch col.DefaultKind {
	case "null":
		return nil
	case "ident":
		switch strings.ToUpper(col.DefaultValue) {
		case "CURRENT_TIMESTAMP", "NOW", "LOCALTIMESTAMP", "CURRENT_DATE", "CURRENT_TIME":
			return time.Now().Format("2006-01-02 15:04:05")
		default:
			return col.DefaultValue
		}
	default:
		return col.DefaultValue
	}
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

	// Resolve every SET assignment to (column index, value). An UPDATE may set
	// several columns at once (SET a = 1, b = 2); gob-decoded statements that
	// predate Assignments fall back to the single Column/Value pair.
	assignments := stmt.Assignments
	if len(assignments) == 0 {
		assignments = []ast.Assignment{{Column: stmt.Column, Value: stmt.Value}}
	}
	type setOp struct {
		colIdx int
		value  interface{}
	}
	setOps := make([]setOp, 0, len(assignments))
	for _, a := range assignments {
		colIdx := findColumnIndex(table.Columns, a.Column.Name)
		if colIdx == -1 {
			return nil, fmt.Errorf("column %s not found", a.Column.Name)
		}
		newVal := literalCell(a.Value)

		// Per-column constraint, uniqueness and referential-action checks.
		if err := e.validateUpdateConstraints(table, a.Column.Name, colIdx, newVal); err != nil {
			return nil, err
		}
		if err := e.validateUpdateUniqueness(table, stmt.Where, a.Column.Name, colIdx, newVal); err != nil {
			return nil, err
		}
		if err := e.enforceUpdateReferences(schema, stmt.Table.Name, table, stmt.Where, a.Column.Name, newVal); err != nil {
			return nil, err
		}
		setOps = append(setOps, setOp{colIdx: colIdx, value: newVal})
	}

	// Enforce CHECK constraints against the post-update image of each affected row.
	if err := e.checkRowsAfterUpdate(table, stmt.Where, func(row []interface{}) {
		for _, op := range setOps {
			if op.colIdx >= 0 && op.colIdx < len(row) {
				row[op.colIdx] = op.value
			}
		}
	}); err != nil {
		return nil, err
	}

	// Snapshot the affected rows before mutating so AFTER triggers can see the
	// OLD (pre-update) and NEW (post-update) images. NEW mirrors what
	// performUpdate writes: every assigned column set to its new value.
	var oldRows, newRows [][]interface{}
	if e.triggersEnabled {
		oldRows, _ = e.snapshotMatchedRows(table, stmt.Where)
		for _, r := range oldRows {
			nr := append([]interface{}(nil), r...)
			for _, op := range setOps {
				if op.colIdx >= 0 && op.colIdx < len(nr) {
					nr[op.colIdx] = op.value
				}
			}
			newRows = append(newRows, nr)
		}
	}

	// Perform update (all assignments applied to each matched row).
	updateCount, err := e.performUpdate(table, stmt.Where, func(row []interface{}) {
		for _, op := range setOps {
			if op.colIdx >= 0 && op.colIdx < len(row) {
				row[op.colIdx] = op.value
			}
		}
	})
	if err != nil {
		return nil, err
	}
	table.RebuildIndexes()

	// Persist to storage
	e.persistTableWarn(table, schema)

	// Execute AFTER UPDATE triggers, once per affected row (FOR EACH ROW).
	if e.triggersEnabled {
		qual := qualifiedName(schema, stmt.Table.Name)
		for i := range oldRows {
			if err := e.executeTriggers(ctx, qual, constants.TriggerAfter, constants.TriggerUpdate, oldRows[i], newRows[i]); err != nil {
				return nil, fmt.Errorf("AFTER UPDATE trigger failed: %w", err)
			}
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

	// Enforce ON DELETE referential actions (RESTRICT by default) before removing
	// any parent rows. This runs without holding the table lock, so it works for
	// self-referencing tables and can cascade / set null across children.
	if err := e.enforceDeleteReferences(schema, stmt.Table.Name, table, stmt.Where); err != nil {
		return nil, err
	}

	// Snapshot the rows about to be deleted so AFTER triggers can see the OLD
	// image (NEW is nil for DELETE).
	var oldRows [][]interface{}
	if e.triggersEnabled {
		oldRows, _ = e.snapshotMatchedRows(table, stmt.Where)
	}

	// Perform deletion
	deleteCount, err := e.performDelete(table, stmt)
	if err != nil {
		return nil, err
	}
	table.RebuildIndexes()

	// Persist to storage
	e.persistTableWarn(table, schema)

	// Execute AFTER DELETE triggers, once per deleted row (FOR EACH ROW).
	if e.triggersEnabled {
		qual := qualifiedName(schema, stmt.Table.Name)
		for _, old := range oldRows {
			if err := e.executeTriggers(ctx, qual, constants.TriggerAfter, constants.TriggerDelete, old, nil); err != nil {
				return nil, fmt.Errorf("AFTER DELETE trigger failed: %w", err)
			}
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

// literalCell maps a parsed literal to the Go value stored in a cell. A SQL NULL
// literal becomes a Go nil (not the empty string), so NULL round-trips and is
// distinguishable from an empty text value.
func literalCell(lit ast.Literal) interface{} {
	if lit.Kind == "null" {
		return nil
	}
	return lit.Value
}

func (e *Executor) validateUpdateConstraints(table *catalog.Table, colName string, colIdx int, newVal interface{}) error {
	updatingCol := table.Columns[colIdx]

	// NOT NULL validation
	if updatingCol.NotNull && (newVal == nil || newVal == "") {
		return fmt.Errorf("column %s cannot be NULL", colName)
	}

	// Foreign key validation
	for _, constraint := range table.Constraints {
		if constraint.Type == constants.ConstraintForeignKey && constraint.ColumnName == colName {
			if newVal != nil && newVal != "" {
				if err := e.validateForeignKey(constraint.ReferencedTable, constraint.ReferencedCol, newVal); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// validateUpdateUniqueness rejects an UPDATE that would leave a duplicate value
// in a PRIMARY KEY or UNIQUE column. It projects the post-update values of that
// column (rows matched by WHERE take the new value, the rest keep theirs) and
// fails on the first collision. This mirrors the duplicate check INSERT already
// performs, which UPDATE previously skipped (so `UPDATE t SET id = 2 WHERE id = 1`
// could create two rows with id = 2). It must run before performUpdate and takes
// only a read lock, so it does not nest with the write lock there.
func (e *Executor) validateUpdateUniqueness(table *catalog.Table, where *ast.WhereClause, colName string, colIdx int, newVal interface{}) error {
	kind := ""
	for _, c := range table.Constraints {
		if c.ColumnName != colName {
			continue
		}
		if c.Type == constants.ConstraintPrimaryKey {
			kind = "primary key"
			break
		}
		if c.Type == constants.ConstraintUnique {
			kind = "unique"
		}
	}
	if kind == "" {
		return nil
	}

	resolve := columnResolver(table)

	table.Mu().RLock()
	defer table.Mu().RUnlock()

	seen := make(map[string]bool, len(table.Rows))
	for _, row := range table.Rows {
		if colIdx >= len(row) {
			continue
		}
		val := row[colIdx]
		matched, err := evalWhere(where, row, resolve)
		if err != nil {
			return err
		}
		if matched {
			val = newVal
		}
		key := fmt.Sprintf("%v", val)
		if seen[key] {
			if kind == "primary key" {
				return fmt.Errorf("duplicate primary key value %v for column %s", val, colName)
			}
			return fmt.Errorf("duplicate value %v for unique column %s", val, colName)
		}
		seen[key] = true
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

// performUpdate applies `apply` to every row selected by `where` (or all rows
// when there is none), returning the number of rows changed. `apply` mutates a
// row in place, setting each assigned column to its new value.
func (e *Executor) performUpdate(table *catalog.Table, where *ast.WhereClause, apply func([]interface{})) (int, error) {
	table.Mu().Lock()
	defer table.Mu().Unlock()

	updateCount := 0

	if where != nil {
		resolve := columnResolver(table)
		for i := range table.Rows {
			ok, err := evalWhere(where, table.Rows[i], resolve)
			if err != nil {
				return 0, err
			}
			if ok {
				apply(table.Rows[i])
				updateCount++
			}
		}
	} else {
		// Update all rows
		updateCount = len(table.Rows)
		for i := range table.Rows {
			apply(table.Rows[i])
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

// performDelete removes the rows selected by the statement's WHERE clause (or
// all rows when there is none). Referential integrity is enforced beforehand by
// enforceDeleteReferences, so this does pure row removal under the table lock.
func (e *Executor) performDelete(table *catalog.Table, stmt *ast.Delete) (int, error) {
	table.Mu().Lock()
	defer table.Mu().Unlock()

	if stmt.Where == nil {
		deleteCount := len(table.Rows)
		table.Rows = [][]interface{}{}
		return deleteCount, nil
	}

	resolve := columnResolver(table)
	deleteCount := 0
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

	return deleteCount, nil
}
