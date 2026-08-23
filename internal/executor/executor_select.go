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

func init() {
	registerExec((*Executor).executeSelect)
	registerExec((*Executor).executeSelectFunction)
}

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
	if err := checkCtx(ctx); err != nil {
		return nil, err
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
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	// Handle SELECT without FROM clause
	if stmt.Table.Name == "" {
		return e.executeSelectNoTable(stmt)
	}

	// Handle JOIN queries
	if stmt.Join != nil {
		return e.executeJoinSelect(ctx, stmt)
	}

	schema := stmt.Table.Alias
	table, err := e.catalog.GetTable(stmt.Table.Name, schema)
	if err != nil {
		view, vErr := e.catalog.GetView(stmt.Table.Name, schema)
		if vErr != nil {
			return nil, fmt.Errorf("table %s not found: %w", stmt.Table.Name, err)
		}

		viewResult, execErr := e.executeSelect(ctx, view.Query)
		if execErr != nil {
			return nil, fmt.Errorf("failed to resolve view %s: %w", view.Name, execErr)
		}

		// Use view.Columns which contains explicit column names if defined
		virtualCols := make([]catalog.Column, len(view.Columns))
		copy(virtualCols, view.Columns)
		table = &catalog.Table{Name: view.Name, Columns: virtualCols, Rows: viewResult.Rows}
	}

	return e.executeSelectFromTable(ctx, stmt, table)
}

func (e *Executor) executeSelectFromTable(ctx context.Context, stmt *ast.Select, table *catalog.Table) (*Result, error) {
	// Check context cancellation
	if err := checkCtx(ctx); err != nil {
		return nil, err
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

	// Apply ORDER BY on the full rows before projecting, using the table's
	// column schema. This mirrors the SELECT * path and lets ORDER BY reference
	// any table column, not only projected ones. (Projecting first would leave
	// ApplyOrderBy resolving column indexes against a mismatched schema.)
	if len(stmt.OrderBy) > 0 {
		rows = queryutil.ApplyOrderBy(rows, stmt.OrderBy, table.Columns)
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
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	colSpecs := make([]colSpec, len(stmt.Columns))
	columns := make([]string, len(stmt.Columns))

	// Parse columns
	for i, col := range stmt.Columns {
		outputName := col.Name
		if col.Alias != "" {
			outputName = col.Alias
		}
		columns[i] = outputName
		colSpecs[i].alias = outputName

		if fn, arg, ok := parseAggregate(col.Name); ok {
			colSpecs[i].isAggregate = true
			colSpecs[i].aggFunc = fn
			if arg == "" || arg == "*" {
				colSpecs[i].colIdx = -1
			} else {
				colIdx := queryutil.IndexOfColumn(table.Columns, arg)
				if colIdx == -1 {
					return nil, fmt.Errorf("column %s not found", arg)
				}
				colSpecs[i].colIdx = colIdx
			}
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
		if spec.isAggregate {
			resultRow[i] = computeAggregate(spec.aggFunc, rows, spec.colIdx)
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
			if spec.isAggregate {
				resultRow[i] = computeAggregate(spec.aggFunc, groupRows, spec.colIdx)
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

// joinColumn identifies a column in the accumulated (joined) row: its owning
// table reference (alias or name) and the column name/type.
type joinColumn struct {
	ref  string
	name string
	typ  string
}

// executeJoinSelect handles JOIN queries, including chains of N joins
// (FROM a JOIN b ON … JOIN c ON … …). The joins are folded left-to-right into a
// single accumulated rowset whose columns are tracked as (ref, name) pairs so
// that WHERE, ORDER BY, projection and aggregates can resolve qualified and
// unqualified column references across every joined table.
func (e *Executor) executeJoinSelect(ctx context.Context, stmt *ast.Select) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	joins := stmt.Joins
	if len(joins) == 0 && stmt.Join != nil {
		joins = []*ast.JoinClause{stmt.Join} // gob-loaded views carry only Join
	}

	// Base (left-most) table.
	baseTable, err := e.catalog.GetTable(stmt.Table.Name)
	if err != nil {
		return nil, fmt.Errorf("table %s not found: %w", stmt.Table.Name, err)
	}
	accRows := baseTable.SelectAll()
	accCols := joinColsFor(refName(stmt.Table), baseTable.Columns)

	// Fold each JOIN into the accumulated result.
	for _, j := range joins {
		rightTable, err := e.catalog.GetTable(j.Table.Name)
		if err != nil {
			return nil, fmt.Errorf("joined table %s not found: %w", j.Table.Name, err)
		}
		accRows, accCols, err = e.foldJoin(j, accRows, accCols,
			rightTable.SelectAll(), joinColsFor(refName(j.Table), rightTable.Columns))
		if err != nil {
			return nil, err
		}
	}

	// WHERE over the joined rows.
	if stmt.Where != nil {
		resolve := func(name string) (int, bool) {
			idx, err := resolveJoinColumn(accCols, name)
			if err != nil {
				return -1, false
			}
			return idx, true
		}
		filtered := make([][]interface{}, 0, len(accRows))
		for _, row := range accRows {
			ok, err := evalWhere(stmt.Where, row, resolve)
			if err != nil {
				return nil, err
			}
			if ok {
				filtered = append(filtered, row)
			}
		}
		accRows = filtered
	}

	// Aggregates / GROUP BY over the joined rows (virtual table carries "ref.name").
	if e.hasAggregates(stmt) || len(stmt.GroupBy) > 0 {
		return e.executeGroupedSelect(ctx, stmt, joinVirtualTable(accCols, accRows), accRows)
	}

	// ORDER BY on the full joined rows (qualified or unique-unqualified refs).
	if len(stmt.OrderBy) > 0 {
		accRows = applyJoinOrderBy(accRows, stmt.OrderBy, accCols)
	}

	// SELECT * : emit every joined column as "ref.name".
	if stmt.Star {
		columns := make([]string, len(accCols))
		for i, c := range accCols {
			columns[i] = c.ref + "." + c.name
		}
		rows := accRows
		if stmt.Distinct {
			rows = queryutil.RemoveDuplicateRows(rows)
		}
		rows = queryutil.ApplyLimitOffset(rows, stmt.Limit, stmt.Offset)
		return &Result{Columns: columns, Rows: rows, Tag: constants.ResultSelectTag(len(rows))}, nil
	}

	// Project specific columns.
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
		idx, err := resolveJoinColumn(accCols, id.Name)
		if err != nil {
			return nil, err
		}
		colIdxs[i] = idx
	}
	projected := make([][]interface{}, 0, len(accRows))
	for _, row := range accRows {
		out := make([]interface{}, len(colIdxs))
		for i, idx := range colIdxs {
			if idx < 0 || idx >= len(row) {
				out[i] = nil
				continue
			}
			out[i] = row[idx]
		}
		projected = append(projected, out)
	}
	if stmt.Distinct {
		projected = queryutil.RemoveDuplicateRows(projected)
	}
	projected = queryutil.ApplyLimitOffset(projected, stmt.Limit, stmt.Offset)
	return &Result{Columns: columns, Rows: projected, Tag: constants.ResultSelectTag(len(projected))}, nil
}

// refName returns the reference used to qualify a table's columns (alias if set).
func refName(id ast.Identifier) string {
	if id.Alias != "" {
		return id.Alias
	}
	return id.Name
}

func joinColsFor(ref string, cols []catalog.Column) []joinColumn {
	out := make([]joinColumn, len(cols))
	for i, c := range cols {
		out[i] = joinColumn{ref: ref, name: c.Name, typ: c.Type}
	}
	return out
}

// resolveJoinColumn resolves a (possibly qualified) column reference to its index
// in the accumulated joined row. Qualified refs must match a table reference;
// unqualified refs must be unambiguous.
func resolveJoinColumn(cols []joinColumn, ref string) (int, error) {
	qualifier, colName := queryutil.SplitQualified(ref)
	if qualifier != "" {
		refSeen := false
		for i, c := range cols {
			if strings.EqualFold(c.ref, qualifier) {
				refSeen = true
				if strings.EqualFold(c.name, colName) {
					return i, nil
				}
			}
		}
		if refSeen {
			return -1, fmt.Errorf("column %s not found in table %s", colName, qualifier)
		}
		return -1, fmt.Errorf("unknown table qualifier %s", qualifier)
	}
	found := -1
	for i, c := range cols {
		if strings.EqualFold(c.name, colName) {
			if found != -1 {
				return -1, fmt.Errorf("ambiguous column reference %s (exists in multiple tables)", colName)
			}
			found = i
		}
	}
	if found == -1 {
		return -1, fmt.Errorf("column %s not found in either table", colName)
	}
	return found, nil
}

// firstJoinColumnMatch resolves a reference to the first matching column index
// (no ambiguity error). Used for ORDER BY, which historically tolerated
// unqualified references shared by multiple joined tables.
func firstJoinColumnMatch(cols []joinColumn, ref string) int {
	qualifier, colName := queryutil.SplitQualified(ref)
	for i, c := range cols {
		if qualifier != "" && !strings.EqualFold(c.ref, qualifier) {
			continue
		}
		if strings.EqualFold(c.name, colName) {
			return i
		}
	}
	return -1
}

// foldJoin joins the accumulated rows (left) with a new table's rows (right),
// returning the new rows and the extended column set.
//
// It supports explicit ON, NATURAL (join on all common column names) and
// USING (join on the named common columns). For NATURAL/USING the shared columns
// are coalesced: the right-side duplicate is dropped from the output and its value
// merged into the surviving left column (COALESCE semantics for outer joins).
func (e *Executor) foldJoin(j *ast.JoinClause, accRows [][]interface{}, accCols []joinColumn, rightRows [][]interface{}, rightCols []joinColumn) ([][]interface{}, []joinColumn, error) {
	joinType := j.Type
	if joinType == "" {
		joinType = constants.JoinInner
	}
	accW, rightW := len(accCols), len(rightCols)
	unionCols := append(append([]joinColumn{}, accCols...), rightCols...)

	// CROSS JOIN: cartesian product, no join condition, no coalesce.
	if joinType == constants.JoinCross {
		out := make([][]interface{}, 0, len(accRows)*len(rightRows))
		for _, l := range accRows {
			for _, r := range rightRows {
				out = append(out, concatRow(l, r))
			}
		}
		return out, unionCols, nil
	}

	// Build the AND-ed equality pairs (as union-row indices). For NATURAL/USING
	// the same pairs are coalesced (right duplicate dropped from output).
	coalesce := j.Natural || len(j.Using) > 0
	var pairs [][2]int
	dropRight := make(map[int]bool)
	switch {
	case j.Natural:
		// Every right column whose name also exists on the left is a join column.
		for k, rc := range rightCols {
			if li := firstJoinColumnMatch(accCols, rc.name); li >= 0 {
				pairs = append(pairs, [2]int{li, accW + k})
				dropRight[accW+k] = true
			}
		}
		// No common columns -> NATURAL degrades to CROSS (SQL standard).
	case len(j.Using) > 0:
		for _, name := range j.Using {
			li := firstJoinColumnMatch(accCols, name)
			if li < 0 {
				return nil, nil, fmt.Errorf("USING column %s not found on the left side of the join", name)
			}
			rk := -1
			for k, rc := range rightCols {
				if strings.EqualFold(rc.name, name) {
					rk = k
					break
				}
			}
			if rk < 0 {
				return nil, nil, fmt.Errorf("USING column %s not found in table %s", name, refName(j.Table))
			}
			pairs = append(pairs, [2]int{li, accW + rk})
			dropRight[accW+rk] = true
		}
	default:
		li, err := resolveJoinColumn(unionCols, j.Left.Name)
		if err != nil {
			return nil, nil, err
		}
		ri, err := resolveJoinColumn(unionCols, j.Right.Name)
		if err != nil {
			return nil, nil, err
		}
		pairs = append(pairs, [2]int{li, ri})
	}

	// Output columns: all union columns except coalesced right duplicates.
	keep := make([]int, 0, len(unionCols))
	newCols := make([]joinColumn, 0, len(unionCols))
	for i, c := range unionCols {
		if dropRight[i] {
			continue
		}
		keep = append(keep, i)
		newCols = append(newCols, c)
	}

	match := func(u []interface{}) bool {
		for _, p := range pairs {
			var lv, rv interface{}
			if p[0] < len(u) {
				lv = u[p[0]]
			}
			if p[1] < len(u) {
				rv = u[p[1]]
			}
			if fmt.Sprintf("%v", lv) != fmt.Sprintf("%v", rv) {
				return false
			}
		}
		return true
	}
	project := func(u []interface{}) []interface{} {
		if coalesce {
			for _, p := range pairs {
				if u[p[0]] == nil && u[p[1]] != nil {
					u[p[0]] = u[p[1]]
				}
			}
		}
		out := make([]interface{}, len(keep))
		for i, k := range keep {
			out[i] = u[k]
		}
		return out
	}

	out := [][]interface{}{}
	switch joinType {
	case constants.JoinInner:
		for _, l := range accRows {
			for _, r := range rightRows {
				if u := concatRow(l, r); match(u) {
					out = append(out, project(u))
				}
			}
		}
	case constants.JoinLeft:
		for _, l := range accRows {
			matched := false
			for _, r := range rightRows {
				if u := concatRow(l, r); match(u) {
					out = append(out, project(u))
					matched = true
				}
			}
			if !matched {
				out = append(out, project(concatRow(l, make([]interface{}, rightW))))
			}
		}
	case constants.JoinRight:
		for _, r := range rightRows {
			matched := false
			for _, l := range accRows {
				if u := concatRow(l, r); match(u) {
					out = append(out, project(u))
					matched = true
				}
			}
			if !matched {
				out = append(out, project(concatRow(make([]interface{}, accW), r)))
			}
		}
	case constants.JoinFull:
		rightMatched := make([]bool, len(rightRows))
		for _, l := range accRows {
			matched := false
			for k, r := range rightRows {
				if u := concatRow(l, r); match(u) {
					out = append(out, project(u))
					matched = true
					rightMatched[k] = true
				}
			}
			if !matched {
				out = append(out, project(concatRow(l, make([]interface{}, rightW))))
			}
		}
		for k, r := range rightRows {
			if !rightMatched[k] {
				out = append(out, project(concatRow(make([]interface{}, accW), r)))
			}
		}
	default:
		return nil, nil, fmt.Errorf("unsupported join type: %s", joinType)
	}
	return out, newCols, nil
}

func concatRow(l, r []interface{}) []interface{} {
	out := make([]interface{}, 0, len(l)+len(r))
	out = append(out, l...)
	out = append(out, r...)
	return out
}

func joinVirtualTable(cols []joinColumn, rows [][]interface{}) *catalog.Table {
	cc := make([]catalog.Column, len(cols))
	for i, c := range cols {
		cc[i] = catalog.Column{Name: c.ref + "." + c.name, Type: c.typ}
	}
	return &catalog.Table{Name: "__join_virtual__", Columns: cc, Rows: rows}
}

// applyJoinOrderBy sorts joined rows, rewriting each ORDER BY column to its
// qualified "ref.name" form so queryutil.ApplyOrderBy (exact-name match) resolves it.
func applyJoinOrderBy(rows [][]interface{}, orderBy []ast.OrderByClause, cols []joinColumn) [][]interface{} {
	cc := make([]catalog.Column, len(cols))
	for i, c := range cols {
		cc[i] = catalog.Column{Name: c.ref + "." + c.name, Type: c.typ}
	}
	rewritten := make([]ast.OrderByClause, len(orderBy))
	for i, ob := range orderBy {
		rewritten[i] = ob
		if idx := firstJoinColumnMatch(cols, ob.Column.Name); idx >= 0 {
			rewritten[i].Column = ast.Identifier{Name: cc[idx].Name}
		}
	}
	return queryutil.ApplyOrderBy(rows, rewritten, cc)
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
	if where == nil {
		return table.SelectAll(), nil
	}
	// Fast path: a single equality predicate can use an index when present.
	if where.IsLeaf() && (where.Operator == "" || where.Operator == "=") {
		return table.SelectWhere(where.Column.Name, where.Value.Value)
	}
	// Otherwise scan the table and evaluate the predicate tree per row.
	resolve := func(name string) (int, bool) {
		i := findColumnIndex(table.Columns, name)
		return i, i != -1
	}
	all := table.SelectAll()
	filtered := make([][]interface{}, 0, len(all))
	for _, row := range all {
		ok, err := evalWhere(where, row, resolve)
		if err != nil {
			return nil, err
		}
		if ok {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

func (e *Executor) hasAggregates(stmt *ast.Select) bool {
	for _, col := range stmt.Columns {
		if _, _, ok := parseAggregate(col.Name); ok {
			return true
		}
	}
	return false
}

// executeSelectFunction handles special SELECT function calls (e.g., pg_catalog queries).
func (e *Executor) executeSelectFunction(ctx context.Context, stmt *ast.SelectFunction) (*Result, error) {
	// Check context cancellation
	if err := checkCtx(ctx); err != nil {
		return nil, err
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
