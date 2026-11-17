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

	// Handle a derived table as the row source: FROM (SELECT ...) alias.
	if stmt.Table.Subquery != nil && stmt.Join == nil {
		dt, err := e.derivedTable(ctx, stmt.Table)
		if err != nil {
			return nil, err
		}
		return e.executeSelectFromTable(ctx, stmt, dt)
	}

	// Handle SELECT without FROM clause
	if stmt.Table.Name == "" && stmt.Table.Subquery == nil {
		return e.executeSelectNoTable(stmt)
	}

	// Handle a set-returning function call as the row source: FROM fn(args).
	if stmt.Table.IsCall {
		fnTable, err := e.resolveFunctionTable(ctx, stmt.Table)
		if err != nil {
			return nil, err
		}
		return e.executeSelectFromTable(ctx, stmt, fnTable)
	}

	// Handle JOIN queries
	if stmt.Join != nil {
		return e.executeJoinSelect(ctx, stmt)
	}

	schema := stmt.Table.Schema
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

	// FROM + WHERE (index fast-path for single equality predicates).
	rows, err := e.fetchRows(ctx, table, stmt)
	if err != nil {
		return nil, err
	}

	// Every later stage (grouping, windows, QUALIFY, ORDER BY, projection)
	// runs on the shared pipeline.
	rs := &rowset{cols: joinColsFor(refName(stmt.Table), stmt.Table.Schema, table.Columns), rows: rows}
	return e.finishSelect(ctx, stmt, rs)
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

// joinColumn describes one column of a rowset flowing through the SELECT
// pipeline: its owning table reference (alias or name; empty for computed
// columns such as aggregates and window functions), the column/output name,
// its type, the canonical source expression when the column was computed
// from one (e.g. "SUM(monto)"), and whether SELECT * should hide it.
type joinColumn struct {
	ref    string
	schema string // schema of the owning table when explicitly qualified
	name   string
	typ    string
	expr   string
	hidden bool
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

	// Base (left-most) table (which may itself be a derived table or function).
	baseTable, err := e.joinSourceTable(ctx, stmt.Table)
	if err != nil {
		return nil, err
	}
	accRows := baseTable.SelectAll()
	accCols := joinColsFor(refName(stmt.Table), stmt.Table.Schema, baseTable.Columns)

	// Fold each JOIN into the accumulated result.
	for _, j := range joins {
		rightTable, err := e.joinSourceTable(ctx, j.Table)
		if err != nil {
			return nil, err
		}
		accRows, accCols, err = e.foldJoin(j, accRows, accCols,
			rightTable.SelectAll(), joinColsFor(refName(j.Table), j.Table.Schema, rightTable.Columns))
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
		useSub := whereHasSubquery(stmt.Where)
		filtered := make([][]interface{}, 0, len(accRows))
		for _, row := range accRows {
			var ok bool
			var err error
			if useSub {
				ok, err = e.evalWhereSub(ctx, stmt.Where, row, resolve)
			} else {
				ok, err = evalWhere(stmt.Where, row, resolve)
			}
			if err != nil {
				return nil, err
			}
			if ok {
				filtered = append(filtered, row)
			}
		}
		accRows = filtered
	}

	return e.finishSelect(ctx, stmt, &rowset{cols: accCols, rows: accRows, qualified: true})
}

// joinSourceTable resolves a FROM/JOIN table reference to an in-memory table:
// a derived table (subquery), a set-returning function call, a base table, or a
// view (materialized by running its query).
func (e *Executor) joinSourceTable(ctx context.Context, id ast.Identifier) (*catalog.Table, error) {
	if id.Subquery != nil {
		return e.derivedTable(ctx, id)
	}
	if id.IsCall {
		return e.resolveFunctionTable(ctx, id)
	}
	table, err := e.catalog.GetTable(id.Name, id.Schema)
	if err == nil {
		return table, nil
	}
	view, vErr := e.catalog.GetView(id.Name, id.Schema)
	if vErr != nil {
		return nil, fmt.Errorf("table %s not found: %w", id.Name, err)
	}
	vres, execErr := e.executeSelect(ctx, view.Query)
	if execErr != nil {
		return nil, fmt.Errorf("failed to resolve view %s: %w", view.Name, execErr)
	}
	vcols := make([]catalog.Column, len(view.Columns))
	copy(vcols, view.Columns)
	return &catalog.Table{Name: view.Name, Columns: vcols, Rows: vres.Rows}, nil
}

// stripTableQualifier removes a "qualifier." prefix from a column reference
// when the qualifier designates the given table (its alias, its name or
// "schema.name"). Any other qualifier is preserved.
func stripTableQualifier(ref string, table ast.Identifier) string {
	qualifier, col := queryutil.SplitQualified(ref)
	if qualifier == "" {
		return ref
	}
	switch {
	case strings.EqualFold(qualifier, refName(table)),
		strings.EqualFold(qualifier, table.Name),
		table.Schema != "" && strings.EqualFold(qualifier, table.Schema+"."+table.Name):
		return col
	}
	return ref
}

// refName returns the reference used to qualify a table's columns (alias if set).
func refName(id ast.Identifier) string {
	if id.Alias != "" {
		return id.Alias
	}
	return id.Name
}

func joinColsFor(ref, schema string, cols []catalog.Column) []joinColumn {
	out := make([]joinColumn, len(cols))
	for i, c := range cols {
		out[i] = joinColumn{ref: ref, schema: schema, name: c.Name, typ: c.Type}
	}
	return out
}

// refMatches reports whether a column's table reference matches qualifier:
// the alias/name itself, or "schema.name" for an explicitly qualified table.
func refMatches(c joinColumn, qualifier string) bool {
	if strings.EqualFold(c.ref, qualifier) {
		return true
	}
	return c.schema != "" && strings.EqualFold(c.schema+"."+c.ref, qualifier)
}

// resolveJoinColumn resolves a (possibly qualified) column reference to its index
// in the accumulated joined row. Qualified refs must match a table reference;
// unqualified refs must be unambiguous.
func resolveJoinColumn(cols []joinColumn, ref string) (int, error) {
	qualifier, colName := queryutil.SplitQualified(ref)
	if qualifier != "" {
		refSeen := false
		for i, c := range cols {
			if refMatches(c, qualifier) {
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
				return -1, fmt.Errorf("%w %s (exists in multiple tables)", errAmbiguousColumn, colName)
			}
			found = i
		}
	}
	if found == -1 {
		return -1, fmt.Errorf("column %s not found", colName)
	}
	return found, nil
}

// firstJoinColumnMatch resolves a reference to the first matching column index
// (no ambiguity error). Used for ORDER BY, which historically tolerated
// unqualified references shared by multiple joined tables.
func firstJoinColumnMatch(cols []joinColumn, ref string) int {
	qualifier, colName := queryutil.SplitQualified(ref)
	for i, c := range cols {
		if qualifier != "" && !refMatches(c, qualifier) {
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

	// ASOF JOIN: for each left row keep the nearest right row satisfying the
	// MATCH_CONDITION (and the optional ON equality). Like a LEFT join, every
	// left row survives (right columns are NULL when there is no match).
	if j.Asof {
		return e.foldAsofJoin(j, accRows, rightRows, unionCols, rightW)
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

// foldAsofJoin implements ASOF JOIN: each accumulated (left) row is paired with
// the nearest right row whose MATCH_CONDITION holds — for `left >= right` /
// `left > right` the right row with the greatest match value, for `<` / `<=` the
// smallest — restricted to rows that also satisfy the optional ON equality.
// Unmatched left rows keep NULLs on the right (LEFT-join semantics).
func (e *Executor) foldAsofJoin(j *ast.JoinClause, accRows, rightRows [][]interface{}, unionCols []joinColumn, rightW int) ([][]interface{}, []joinColumn, error) {
	mLeftIdx, err := resolveJoinColumn(unionCols, j.MatchLeft.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("ASOF MATCH_CONDITION: %w", err)
	}
	mRightIdx, err := resolveJoinColumn(unionCols, j.MatchRight.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("ASOF MATCH_CONDITION: %w", err)
	}
	hasOn := j.Left.Name != ""
	onLeftIdx, onRightIdx := -1, -1
	if hasOn {
		if onLeftIdx, err = resolveJoinColumn(unionCols, j.Left.Name); err != nil {
			return nil, nil, fmt.Errorf("ASOF ON: %w", err)
		}
		if onRightIdx, err = resolveJoinColumn(unionCols, j.Right.Name); err != nil {
			return nil, nil, fmt.Errorf("ASOF ON: %w", err)
		}
	}

	op := j.MatchOp
	wantMax := op == ">" || op == ">=" // nearest-earlier picks the greatest match value

	out := make([][]interface{}, 0, len(accRows))
	for _, l := range accRows {
		var best []interface{}
		var bestVal interface{}
		for _, r := range rightRows {
			u := concatRow(l, r)
			if hasOn && fmt.Sprintf("%v", u[onLeftIdx]) != fmt.Sprintf("%v", u[onRightIdx]) {
				continue
			}
			lv, rv := u[mLeftIdx], u[mRightIdx]
			if !whereMatches(lv, op, rv) {
				continue
			}
			if best == nil {
				best, bestVal = r, rv
				continue
			}
			c := compareCells(rv, bestVal)
			if (wantMax && c > 0) || (!wantMax && c < 0) {
				best, bestVal = r, rv
			}
		}
		if best == nil {
			best = make([]interface{}, rightW) // no match: right side NULL
		}
		out = append(out, concatRow(l, best))
	}
	return out, unionCols, nil
}

func concatRow(l, r []interface{}) []interface{} {
	out := make([]interface{}, 0, len(l)+len(r))
	out = append(out, l...)
	out = append(out, r...)
	return out
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

// fetchRows returns the rows of a single-table SELECT that satisfy its WHERE
// clause. Column references may be qualified with the table's alias, name or
// "schema.name" (WHERE v.monto > 1); other qualifiers are left as-is so they
// fail with "column x.y not found".
func (e *Executor) fetchRows(ctx context.Context, table *catalog.Table, stmt *ast.Select) ([][]interface{}, error) {
	where := stmt.Where
	if where == nil {
		return table.SelectAll(), nil
	}
	// Subquery / EXISTS / IN-subquery predicates need executor-backed evaluation
	// (and possible per-row correlation), so scan and evaluate with evalWhereSub.
	if whereHasSubquery(where) {
		resolve := func(name string) (int, bool) {
			i := findColumnIndex(table.Columns, stripTableQualifier(name, stmt.Table))
			return i, i != -1
		}
		all := table.SelectAll()
		filtered := make([][]interface{}, 0, len(all))
		for _, row := range all {
			ok, err := e.evalWhereSub(ctx, where, row, resolve)
			if err != nil {
				return nil, err
			}
			if ok {
				filtered = append(filtered, row)
			}
		}
		return filtered, nil
	}
	// Fast path: a single leaf predicate can use an index when present. Equality
	// goes through SelectWhere (which scans when there is no index); range and
	// BETWEEN go through SelectRange, which reports whether an index handled it
	// and otherwise leaves the scan below to do the work.
	if where.IsLeaf() {
		col := stripTableQualifier(where.Column.Name, stmt.Table)
		switch where.Operator {
		case "", "=":
			return table.SelectWhere(col, where.Value.Value)
		case "<", "<=", ">", ">=":
			if rows, handled, err := table.SelectRange(col, where.Operator, where.Value.Value, nil); err != nil {
				return nil, err
			} else if handled {
				return rows, nil
			}
		case "BETWEEN":
			if rows, handled, err := table.SelectRange(col, "BETWEEN", where.Value.Value, where.Value2.Value); err != nil {
				return nil, err
			} else if handled {
				return rows, nil
			}
		}
	}
	// Otherwise scan the table and evaluate the predicate tree per row.
	resolve := func(name string) (int, bool) {
		i := findColumnIndex(table.Columns, stripTableQualifier(name, stmt.Table))
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
