package executor

import (
	"context"
	"fmt"
	"strings"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/constants"
	"dbf/internal/queryutil"
)

// colSpec represents a column specification for GROUP BY queries
type colSpec struct {
	isAggregate bool
	aggFunc     string
	colIdx      int
	name        string
	alias       string
}

// executeSelect executes a SELECT statement, handling CTEs (WITH clause) and delegating to specialized handlers.
func (e *Executor) executeSelect(ctx context.Context, stmt *ast.Select) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Execute CTEs (WITH clause)
	cteTables := []string{}
	if len(stmt.With) > 0 {
		for _, cte := range stmt.With {
			// Execute CTE SELECT
			cteResult, err := e.executeSelect(ctx, cte.Select)
			if err != nil {
				// Clean up any CTEs created so far
				e.cleanupCTEs(cteTables)
				return nil, fmt.Errorf("error executing CTE %s: %w", cte.Name.Name, err)
			}

			// Create temporary table from CTE result
			if err := e.createCTETable(cte.Name.Name, cteResult); err != nil {
				e.cleanupCTEs(cteTables)
				return nil, fmt.Errorf("error creating CTE table %s: %w", cte.Name.Name, err)
			}

			// Insert CTE results
			table, _ := e.catalog.GetTable(cte.Name.Name)
			for _, row := range cteResult.Rows {
				table.InsertRowUnsafe(row)
			}

			cteTables = append(cteTables, cte.Name.Name)
		}
	}

	// Execute main SELECT
	result, err := e.executeSelectMain(ctx, stmt)

	// Clean up CTE tables
	e.cleanupCTEs(cteTables)

	return result, err
}

// executeSelectMain handles the main SELECT logic, routing to JOIN or simple SELECT handlers.
func (e *Executor) executeSelectMain(ctx context.Context, stmt *ast.Select) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Handle SELECT without FROM clause
	if stmt.Table.Name == "" {
		return e.executeSelectNoTable(stmt)
	}

	// Handle JOIN queries
	if stmt.Join != nil {
		return e.executeJoinSelect(ctx, stmt)
	}

	// Simple SELECT from single table
	table, err := e.catalog.GetTable(stmt.Table.Name)
	if err != nil {
		return nil, fmt.Errorf("table %s not found: %w", stmt.Table.Name, err)
	}

	// Fetch rows
	rows, err := e.fetchRows(table, stmt.Where)
	if err != nil {
		return nil, err
	}

	// Check for aggregates or GROUP BY
	if e.hasAggregates(stmt) || len(stmt.GroupBy) > 0 {
		return e.executeGroupedSelect(ctx, stmt, table, rows)
	}

	// Handle SELECT *
	if stmt.Star {
		return e.executeSelectStar(table, rows, stmt)
	}

	// Project specific columns
	return e.projectColumns(table, rows, stmt)
}

// executeSelectNoTable handles SELECT without FROM clause.
func (e *Executor) executeSelectNoTable(stmt *ast.Select) (*Result, error) {
	if stmt.Star {
		return &Result{Tag: constants.ResultSelectTag(0)}, nil
	}

	rows := [][]interface{}{}
	if len(stmt.Columns) > 0 {
		row := make([]interface{}, len(stmt.Columns))
		rows = append(rows, row)
	}

	return &Result{
		Columns: queryutil.IdentifiersToNames(stmt.Columns),
		Rows:    rows,
		Tag:     constants.ResultSelectTag(len(rows)),
	}, nil
}

// executeSelectStar handles SELECT * queries.
func (e *Executor) executeSelectStar(table *catalog.Table, rows [][]interface{}, stmt *ast.Select) (*Result, error) {
	columns := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		columns[i] = col.Name
	}

	// Apply DISTINCT
	if stmt.Distinct {
		rows = queryutil.RemoveDuplicateRows(rows)
	}

	// Apply ORDER BY
	if len(stmt.OrderBy) > 0 {
		rows = queryutil.ApplyOrderBy(rows, stmt.OrderBy, table.Columns)
	}

	// Apply LIMIT/OFFSET
	rows = queryutil.ApplyLimitOffset(rows, stmt.Limit, stmt.Offset)

	return &Result{
		Columns: columns,
		Rows:    rows,
		Tag:     constants.ResultSelectTag(len(rows)),
	}, nil
}

// projectColumns projects specific columns from rows.
func (e *Executor) projectColumns(table *catalog.Table, rows [][]interface{}, stmt *ast.Select) (*Result, error) {
	colIdxs := make([]int, len(stmt.Columns))
	columns := make([]string, len(stmt.Columns))

	for i, id := range stmt.Columns {
		outputName := id.Name
		if id.Alias != "" {
			outputName = id.Alias
		}
		columns[i] = outputName

		if id.Name == "" {
			if !stmt.AllowMissing {
				return nil, fmt.Errorf("column %s not found", outputName)
			}
			colIdxs[i] = -1
			continue
		}

		qualifier, colName := queryutil.SplitQualified(id.Name)
		if qualifier != "" && !strings.EqualFold(qualifier, stmt.Table.Name) {
			return nil, fmt.Errorf("unknown table %s in column %s", qualifier, id.Name)
		}

		colIdx := queryutil.IndexOfColumn(table.Columns, colName)
		if colIdx == -1 && !stmt.AllowMissing {
			return nil, fmt.Errorf("column %s not found", outputName)
		}
		colIdxs[i] = colIdx
	}

	projected := make([][]interface{}, 0, len(rows))
	for _, row := range rows {
		out := make([]interface{}, len(colIdxs))
		for i, idx := range colIdxs {
			if idx == -1 {
				out[i] = nil
				continue
			}
			out[i] = row[idx]
		}
		projected = append(projected, out)
	}

	// Apply DISTINCT
	if stmt.Distinct {
		projected = queryutil.RemoveDuplicateRows(projected)
	}

	// Apply ORDER BY
	if len(stmt.OrderBy) > 0 {
		projected = queryutil.ApplyOrderBy(projected, stmt.OrderBy, table.Columns)
	}

	// Apply LIMIT/OFFSET
	projected = queryutil.ApplyLimitOffset(projected, stmt.Limit, stmt.Offset)

	return &Result{
		Columns: columns,
		Rows:    projected,
		Tag:     constants.ResultSelectTag(len(projected)),
	}, nil
}

// executeGroupedSelect handles GROUP BY and aggregate functions.
func (e *Executor) executeGroupedSelect(ctx context.Context, stmt *ast.Select, table *catalog.Table, rows [][]interface{}) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	colSpecs := make([]colSpec, len(stmt.Columns))
	columns := make([]string, len(stmt.Columns))

	// Parse columns
	for i, col := range stmt.Columns {
		upper := strings.ToUpper(col.Name)
		outputName := col.Name
		if col.Alias != "" {
			outputName = col.Alias
		}
		columns[i] = outputName
		colSpecs[i].alias = outputName

		if strings.HasPrefix(upper, "COUNT(") {
			colSpecs[i].isAggregate = true
			colSpecs[i].aggFunc = "COUNT"
			colSpecs[i].colIdx = -1
		} else {
			// Regular column
			colIdx := queryutil.IndexOfColumn(table.Columns, col.Name)
			if colIdx == -1 {
				return nil, fmt.Errorf("column %s not found", col.Name)
			}
			colSpecs[i].colIdx = colIdx
			colSpecs[i].name = col.Name
		}
	}

	// No GROUP BY: aggregate all rows
	if len(stmt.GroupBy) == 0 {
		return e.aggregateAllRows(columns, colSpecs, rows), nil
	}

	// GROUP BY: aggregate by groups
	return e.aggregateByGroups(ctx, stmt, table, columns, colSpecs, rows)
}

// aggregateAllRows aggregates all rows without grouping.
func (e *Executor) aggregateAllRows(columns []string, colSpecs []colSpec, rows [][]interface{}) *Result {
	resultRow := make([]interface{}, len(colSpecs))
	for i, spec := range colSpecs {
		if spec.isAggregate && spec.aggFunc == "COUNT" {
			resultRow[i] = len(rows)
		} else {
			// Non-aggregate column: take first value
			if len(rows) > 0 && spec.colIdx >= 0 {
				resultRow[i] = rows[0][spec.colIdx]
			} else {
				resultRow[i] = nil
			}
		}
	}

	return &Result{
		Columns: columns,
		Rows:    [][]interface{}{resultRow},
		Tag:     constants.ResultSelectTag(1),
	}
}

// aggregateByGroups aggregates rows by GROUP BY columns.
func (e *Executor) aggregateByGroups(ctx context.Context, stmt *ast.Select, table *catalog.Table, columns []string, colSpecs []colSpec, rows [][]interface{}) (*Result, error) {
	// Build GROUP BY column indexes
	groupByIdxs := make([]int, len(stmt.GroupBy))
	for i, gbCol := range stmt.GroupBy {
		idx := queryutil.IndexOfColumn(table.Columns, gbCol.Name)
		if idx == -1 {
			return nil, fmt.Errorf("GROUP BY column %s not found", gbCol.Name)
		}
		groupByIdxs[i] = idx
	}

	// Group rows
	type groupKey struct {
		values string
	}
	groups := make(map[groupKey][][]interface{})
	groupOrder := []groupKey{}

	for _, row := range rows {
		keyVals := make([]interface{}, len(groupByIdxs))
		for i, idx := range groupByIdxs {
			keyVals[i] = row[idx]
		}
		key := groupKey{values: fmt.Sprintf("%v", keyVals)}
		if _, exists := groups[key]; !exists {
			groupOrder = append(groupOrder, key)
			groups[key] = [][]interface{}{}
		}
		groups[key] = append(groups[key], row)
	}

	// Generate result rows
	resultRows := [][]interface{}{}
	for _, key := range groupOrder {
		groupRows := groups[key]
		resultRow := make([]interface{}, len(colSpecs))

		for i, spec := range colSpecs {
			if spec.isAggregate && spec.aggFunc == "COUNT" {
				resultRow[i] = len(groupRows)
			} else {
				// Non-aggregate column: take first value from group
				if len(groupRows) > 0 && spec.colIdx >= 0 {
					resultRow[i] = groupRows[0][spec.colIdx]
				} else {
					resultRow[i] = nil
				}
			}
		}
		resultRows = append(resultRows, resultRow)
	}

	// Create temporary columns for ORDER BY
	tempCols := make([]catalog.Column, len(columns))
	for i, colName := range columns {
		tempCols[i] = catalog.Column{Name: colName, Type: "TEXT"}
	}

	// Apply ORDER BY
	if len(stmt.OrderBy) > 0 {
		resultRows = queryutil.ApplyOrderBy(resultRows, stmt.OrderBy, tempCols)
	}

	// Apply LIMIT/OFFSET
	resultRows = queryutil.ApplyLimitOffset(resultRows, stmt.Limit, stmt.Offset)

	return &Result{
		Columns: columns,
		Rows:    resultRows,
		Tag:     constants.ResultSelectTag(len(resultRows)),
	}, nil
}

// executeJoinSelect handles JOIN queries.
func (e *Executor) executeJoinSelect(ctx context.Context, stmt *ast.Select) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	leftTable, err := e.catalog.GetTable(stmt.Table.Name)
	if err != nil {
		return nil, fmt.Errorf("left table %s not found: %w", stmt.Table.Name, err)
	}

	rightTable, err := e.catalog.GetTable(stmt.Join.Table.Name)
	if err != nil {
		return nil, fmt.Errorf("right table %s not found: %w", stmt.Join.Table.Name, err)
	}

	// Determine reference names
	leftRefName := stmt.Table.Name
	if stmt.Table.Alias != "" {
		leftRefName = stmt.Table.Alias
	}
	rightRefName := stmt.Join.Table.Name
	if stmt.Join.Table.Alias != "" {
		rightRefName = stmt.Join.Table.Alias
	}

	leftRows := leftTable.SelectAll()
	rightRows := rightTable.SelectAll()

	// Perform JOIN
	joinedRows, err := e.performJoin(stmt.Join.Type, leftTable, rightTable, leftRows, rightRows, stmt.Join, leftRefName, rightRefName)
	if err != nil {
		return nil, err
	}

	// Apply WHERE clause
	if stmt.Where != nil {
		joinedRows, err = e.filterJoinedRows(joinedRows, stmt.Where, leftTable, rightTable, leftRefName, rightRefName)
		if err != nil {
			return nil, err
		}
	}

	// Handle aggregates or GROUP BY with joins
	if e.hasAggregates(stmt) || len(stmt.GroupBy) > 0 {
		virtualTable := e.createVirtualTable(leftTable, rightTable, leftRefName, rightRefName, joinedRows)
		return e.executeGroupedSelect(ctx, stmt, virtualTable, joinedRows)
	}

	// Handle SELECT *
	if stmt.Star {
		return e.projectJoinStar(leftTable, rightTable, leftRefName, rightRefName, joinedRows, stmt)
	}

	// Project specific columns
	return e.projectJoinColumns(leftTable, rightTable, leftRefName, rightRefName, joinedRows, stmt)
}

// performJoin performs the JOIN operation based on join type.
func (e *Executor) performJoin(joinType string, leftTable, rightTable *catalog.Table, leftRows, rightRows [][]interface{}, join *ast.JoinClause, leftRefName, rightRefName string) ([][]interface{}, error) {
	if joinType == "" {
		joinType = constants.JoinInner
	}

	joinedRows := [][]interface{}{}

	// CROSS JOIN: cartesian product
	if joinType == constants.JoinCross {
		for _, lrow := range leftRows {
			for _, rrow := range rightRows {
				combined := append(append([]interface{}{}, lrow...), rrow...)
				joinedRows = append(joinedRows, combined)
			}
		}
		return joinedRows, nil
	}

	// Resolve ON clause columns
	leftIsLeft, leftColIdx, err := queryutil.ResolveJoinColumn(leftTable, rightTable, leftRefName, rightRefName, join.Left.Name)
	if err != nil {
		return nil, err
	}
	rightIsLeft, rightColIdx, err := queryutil.ResolveJoinColumn(leftTable, rightTable, leftRefName, rightRefName, join.Right.Name)
	if err != nil {
		return nil, err
	}

	switch joinType {
	case constants.JoinInner:
		return e.performInnerJoin(leftRows, rightRows, leftIsLeft, leftColIdx, rightIsLeft, rightColIdx), nil
	case constants.JoinLeft:
		return e.performLeftJoin(leftRows, rightRows, leftIsLeft, leftColIdx, rightIsLeft, rightColIdx, rightTable), nil
	case constants.JoinRight:
		return e.performRightJoin(leftRows, rightRows, leftIsLeft, leftColIdx, rightIsLeft, rightColIdx, leftTable), nil
	case constants.JoinFull:
		return e.performFullJoin(leftRows, rightRows, leftIsLeft, leftColIdx, rightIsLeft, rightColIdx, leftTable, rightTable), nil
	default:
		return nil, fmt.Errorf("unsupported join type: %s", joinType)
	}
}

// performInnerJoin performs an INNER JOIN.
func (e *Executor) performInnerJoin(leftRows, rightRows [][]interface{}, leftIsLeft bool, leftColIdx int, rightIsLeft bool, rightColIdx int) [][]interface{} {
	joinedRows := [][]interface{}{}
	for _, lrow := range leftRows {
		for _, rrow := range rightRows {
			lv := getRowValue(leftIsLeft, lrow, rrow, leftColIdx)
			rv := getRowValue(rightIsLeft, lrow, rrow, rightColIdx)
			if fmt.Sprintf("%v", lv) == fmt.Sprintf("%v", rv) {
				combined := append(append([]interface{}{}, lrow...), rrow...)
				joinedRows = append(joinedRows, combined)
			}
		}
	}
	return joinedRows
}

// performLeftJoin performs a LEFT JOIN.
func (e *Executor) performLeftJoin(leftRows, rightRows [][]interface{}, leftIsLeft bool, leftColIdx int, rightIsLeft bool, rightColIdx int, rightTable *catalog.Table) [][]interface{} {
	joinedRows := [][]interface{}{}
	for _, lrow := range leftRows {
		matched := false
		for _, rrow := range rightRows {
			lv := getRowValue(leftIsLeft, lrow, rrow, leftColIdx)
			rv := getRowValue(rightIsLeft, lrow, rrow, rightColIdx)
			if fmt.Sprintf("%v", lv) == fmt.Sprintf("%v", rv) {
				combined := append(append([]interface{}{}, lrow...), rrow...)
				joinedRows = append(joinedRows, combined)
				matched = true
			}
		}
		if !matched {
			nullRight := make([]interface{}, len(rightTable.Columns))
			for i := range nullRight {
				nullRight[i] = nil
			}
			combined := append(append([]interface{}{}, lrow...), nullRight...)
			joinedRows = append(joinedRows, combined)
		}
	}
	return joinedRows
}

// performRightJoin performs a RIGHT JOIN.
func (e *Executor) performRightJoin(leftRows, rightRows [][]interface{}, leftIsLeft bool, leftColIdx int, rightIsLeft bool, rightColIdx int, leftTable *catalog.Table) [][]interface{} {
	joinedRows := [][]interface{}{}
	for _, rrow := range rightRows {
		matched := false
		for _, lrow := range leftRows {
			lv := getRowValue(leftIsLeft, lrow, rrow, leftColIdx)
			rv := getRowValue(rightIsLeft, lrow, rrow, rightColIdx)
			if fmt.Sprintf("%v", lv) == fmt.Sprintf("%v", rv) {
				combined := append(append([]interface{}{}, lrow...), rrow...)
				joinedRows = append(joinedRows, combined)
				matched = true
			}
		}
		if !matched {
			nullLeft := make([]interface{}, len(leftTable.Columns))
			for i := range nullLeft {
				nullLeft[i] = nil
			}
			combined := append(append([]interface{}{}, nullLeft...), rrow...)
			joinedRows = append(joinedRows, combined)
		}
	}
	return joinedRows
}

// performFullJoin performs a FULL OUTER JOIN.
func (e *Executor) performFullJoin(leftRows, rightRows [][]interface{}, leftIsLeft bool, leftColIdx int, rightIsLeft bool, rightColIdx int, leftTable, rightTable *catalog.Table) [][]interface{} {
	joinedRows := [][]interface{}{}
	rightMatched := make([]bool, len(rightRows))

	// First pass: match left rows with right rows
	for _, lrow := range leftRows {
		matched := false
		for rIdx, rrow := range rightRows {
			lv := getRowValue(leftIsLeft, lrow, rrow, leftColIdx)
			rv := getRowValue(rightIsLeft, lrow, rrow, rightColIdx)
			if fmt.Sprintf("%v", lv) == fmt.Sprintf("%v", rv) {
				combined := append(append([]interface{}{}, lrow...), rrow...)
				joinedRows = append(joinedRows, combined)
				matched = true
				rightMatched[rIdx] = true
			}
		}
		if !matched {
			nullRight := make([]interface{}, len(rightTable.Columns))
			for i := range nullRight {
				nullRight[i] = nil
			}
			combined := append(append([]interface{}{}, lrow...), nullRight...)
			joinedRows = append(joinedRows, combined)
		}
	}

	// Second pass: add unmatched right rows
	for rIdx, rrow := range rightRows {
		if !rightMatched[rIdx] {
			nullLeft := make([]interface{}, len(leftTable.Columns))
			for i := range nullLeft {
				nullLeft[i] = nil
			}
			combined := append(append([]interface{}{}, nullLeft...), rrow...)
			joinedRows = append(joinedRows, combined)
		}
	}

	return joinedRows
}

// Helper functions

func (e *Executor) cleanupCTEs(cteTables []string) {
	for _, ct := range cteTables {
		e.catalog.DropTable(ct)
	}
}

func (e *Executor) createCTETable(name string, result *Result) error {
	columns := make([]catalog.Column, len(result.Columns))
	for i, colName := range result.Columns {
		columns[i] = catalog.Column{Name: colName, Type: "TEXT"} // Default to TEXT for CTEs
	}
	return e.catalog.CreateTable(name, columns, nil)
}

func (e *Executor) fetchRows(table *catalog.Table, where *ast.WhereClause) ([][]interface{}, error) {
	if where != nil {
		return table.SelectWhere(where.Column.Name, where.Value.Value)
	}
	return table.SelectAll(), nil
}

func (e *Executor) hasAggregates(stmt *ast.Select) bool {
	for _, col := range stmt.Columns {
		upper := strings.ToUpper(col.Name)
		if strings.HasPrefix(upper, "COUNT(") {
			return true
		}
	}
	return false
}

func (e *Executor) filterJoinedRows(rows [][]interface{}, where *ast.WhereClause, leftTable, rightTable *catalog.Table, leftRefName, rightRefName string) ([][]interface{}, error) {
	filtered := [][]interface{}{}
	whereIdx, err := queryutil.ResolveCombinedIndex(leftTable, rightTable, leftRefName, rightRefName, where.Column.Name)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if whereIdx >= 0 && row[whereIdx] == where.Value.Value {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

func (e *Executor) createVirtualTable(leftTable, rightTable *catalog.Table, leftRefName, rightRefName string, joinedRows [][]interface{}) *catalog.Table {
	combinedCols := make([]catalog.Column, 0, len(leftTable.Columns)+len(rightTable.Columns))
	for _, col := range leftTable.Columns {
		combinedCols = append(combinedCols, catalog.Column{Name: leftRefName + "." + col.Name, Type: col.Type})
	}
	for _, col := range rightTable.Columns {
		combinedCols = append(combinedCols, catalog.Column{Name: rightRefName + "." + col.Name, Type: col.Type})
	}
	return &catalog.Table{
		Name:    "__join_virtual__",
		Columns: combinedCols,
		Rows:    joinedRows,
	}
}

func (e *Executor) projectJoinStar(leftTable, rightTable *catalog.Table, leftRefName, rightRefName string, joinedRows [][]interface{}, stmt *ast.Select) (*Result, error) {
	columns := make([]string, 0, len(leftTable.Columns)+len(rightTable.Columns))
	for _, col := range leftTable.Columns {
		columns = append(columns, leftRefName+"."+col.Name)
	}
	for _, col := range rightTable.Columns {
		columns = append(columns, rightRefName+"."+col.Name)
	}

	// Apply DISTINCT
	if stmt.Distinct {
		joinedRows = queryutil.RemoveDuplicateRows(joinedRows)
	}

	// Create combined columns for ORDER BY
	combinedCols := append(append([]catalog.Column{}, leftTable.Columns...), rightTable.Columns...)

	// Apply ORDER BY
	if len(stmt.OrderBy) > 0 {
		joinedRows = queryutil.ApplyOrderBy(joinedRows, stmt.OrderBy, combinedCols)
	}

	// Apply LIMIT/OFFSET
	joinedRows = queryutil.ApplyLimitOffset(joinedRows, stmt.Limit, stmt.Offset)

	return &Result{
		Columns: columns,
		Rows:    joinedRows,
		Tag:     constants.ResultSelectTag(len(joinedRows)),
	}, nil
}

func (e *Executor) projectJoinColumns(leftTable, rightTable *catalog.Table, leftRefName, rightRefName string, joinedRows [][]interface{}, stmt *ast.Select) (*Result, error) {
	columns := make([]string, len(stmt.Columns))
	colIdxs := make([]int, len(stmt.Columns))

	for i, id := range stmt.Columns {
		outputName := id.Name
		if id.Alias != "" {
			outputName = id.Alias
		}
		columns[i] = outputName

		if id.Name == "" {
			if !stmt.AllowMissing {
				return nil, fmt.Errorf("column %s not found", outputName)
			}
			colIdxs[i] = -1
			continue
		}

		idx, err := queryutil.ResolveCombinedIndex(leftTable, rightTable, leftRefName, rightRefName, id.Name)
		if err != nil {
			return nil, err
		}
		colIdxs[i] = idx
	}

	projected := make([][]interface{}, 0, len(joinedRows))
	for _, row := range joinedRows {
		out := make([]interface{}, len(colIdxs))
		for i, idx := range colIdxs {
			if idx == -1 {
				out[i] = nil
				continue
			}
			out[i] = row[idx]
		}
		projected = append(projected, out)
	}

	// Apply DISTINCT
	if stmt.Distinct {
		projected = queryutil.RemoveDuplicateRows(projected)
	}

	// Create combined columns for ORDER BY
	combinedCols := append(append([]catalog.Column{}, leftTable.Columns...), rightTable.Columns...)

	// Apply ORDER BY
	if len(stmt.OrderBy) > 0 {
		projected = queryutil.ApplyOrderBy(projected, stmt.OrderBy, combinedCols)
	}

	// Apply LIMIT/OFFSET
	projected = queryutil.ApplyLimitOffset(projected, stmt.Limit, stmt.Offset)

	return &Result{
		Columns: columns,
		Rows:    projected,
		Tag:     constants.ResultSelectTag(len(projected)),
	}, nil
}

// executeSelectFunction handles special SELECT function calls (e.g., pg_catalog queries).
func (e *Executor) executeSelectFunction(ctx context.Context, stmt *ast.SelectFunction) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Handle specific functions
	switch stmt.Name {
	case "version":
		return &Result{
			Columns: []string{"version"},
			Rows:    [][]interface{}{{"FocusDB 1.0 (PostgreSQL 16.1 compatible)"}},
			Tag:     constants.ResultSelectTag(1),
		}, nil
	default:
		// For unknown functions, return empty result
		return &Result{
			Columns: []string{},
			Rows:    [][]interface{}{},
			Tag:     constants.ResultSelectTag(0),
		}, nil
	}
}

// getRowValue gets a value from either left or right row based on the flag.
func getRowValue(isLeft bool, leftRow, rightRow []interface{}, colIdx int) interface{} {
	if isLeft {
		if colIdx < len(leftRow) {
			return leftRow[colIdx]
		}
	} else {
		if colIdx < len(rightRow) {
			return rightRow[colIdx]
		}
	}
	return nil
}
