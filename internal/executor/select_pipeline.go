package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dbf/internal/ast"
	"dbf/internal/constants"
	"dbf/internal/queryutil"
)

// errAmbiguousColumn marks a column reference that matches several joined
// tables. It must surface even when the select list tolerates missing columns.
var errAmbiguousColumn = errors.New("ambiguous column reference")

// rowset is the intermediate relation that flows through the SELECT pipeline
// once FROM/JOIN and WHERE have produced the source rows. Every later stage
// (grouping, window functions, QUALIFY, ORDER BY, projection, DISTINCT, LIMIT)
// reads and rewrites it, so column resolution lives in a single place instead
// of being duplicated per execution path.
//
// Logical evaluation order implemented by finishSelect:
//
//	FROM/JOIN → WHERE → GROUP BY + aggregates → window functions → QUALIFY
//	→ ORDER BY → projection → DISTINCT → LIMIT/OFFSET
type rowset struct {
	cols      []joinColumn
	rows      [][]interface{}
	qualified bool // SELECT * emits "ref.name" (joins) instead of "name"
}

// normExpr canonicalizes an expression text for matching (upper-case, no
// spaces) so that "sum( monto )" and "SUM(monto)" refer to the same column.
func normExpr(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, " ", ""))
}

// resolve maps a column reference to its index in the rowset.
//
// Lookup order: a computed/output column whose name or source expression
// matches (this covers aggregate text such as "SUM(monto)" and window aliases),
// then a qualified "ref.name" source column, then an unqualified source column
// name (which must be unambiguous across joined tables).
func (rs *rowset) resolve(ref string) (int, error) {
	if ref == "" {
		return -1, fmt.Errorf("empty column reference")
	}
	want := normExpr(ref)
	for i, c := range rs.cols {
		if c.ref == "" && strings.EqualFold(c.name, ref) {
			return i, nil
		}
		if c.expr != "" && normExpr(c.expr) == want {
			return i, nil
		}
	}
	if strings.Contains(ref, "(") {
		return -1, fmt.Errorf("column %s not found", ref)
	}
	return resolveJoinColumn(rs.cols, ref)
}

// resolveWithAliases resolves a reference against the rowset and, failing
// that, against the SELECT list aliases (ORDER BY total, QUALIFY rn, ...),
// following the alias to its source expression.
func (rs *rowset) resolveWithAliases(stmt *ast.Select, ref string) (int, error) {
	idx, err := rs.resolve(ref)
	if err == nil {
		return idx, nil
	}
	for _, id := range stmt.Columns {
		if id.Alias == "" || !strings.EqualFold(id.Alias, ref) {
			continue
		}
		if id.Window != nil || id.Name == "" {
			// Window columns are registered under their alias once computed;
			// reaching here means they are not available yet (or the item
			// is a placeholder expression).
			break
		}
		if idx, err2 := rs.resolve(id.Name); err2 == nil {
			return idx, nil
		}
	}
	return -1, err
}

// isPlaceholderAlias reports whether ref names a select-list item the parser
// could not reduce to a column or function (it projects as NULL).
func isPlaceholderAlias(stmt *ast.Select, ref string) bool {
	for _, id := range stmt.Columns {
		if id.Window == nil && id.Name == "" && id.Alias != "" && strings.EqualFold(id.Alias, ref) {
			return true
		}
	}
	return false
}

// finishSelect runs every post-WHERE stage of a SELECT over rs and builds the
// final Result. Both the single-table and the JOIN paths converge here.
func (e *Executor) finishSelect(ctx context.Context, stmt *ast.Select, rs *rowset) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	// GROUP BY + aggregates. An aggregate referenced outside the select list
	// (ORDER BY, QUALIFY, OVER) is only meaningful on a grouped query.
	grouped := needsGrouping(stmt)
	if !grouped {
		if ref := firstAggregateRef(stmt); ref != "" {
			return nil, fmt.Errorf("aggregate %s in ORDER BY/QUALIFY/OVER requires GROUP BY or an aggregate in the select list", ref)
		}
	} else {
		g, err := groupRows(stmt, rs)
		if err != nil {
			return nil, err
		}
		rs = g
	}

	// Select-list items are bound to column indexes once, before window
	// columns are appended, so an alias can never shadow a source column.
	itemIdx, err := selectItemIndexes(stmt, rs, grouped)
	if err != nil {
		return nil, err
	}

	// Window functions (select list + QUALIFY), appended as extra columns.
	qualify, hidden, err := computeWindows(stmt, rs, itemIdx)
	if err != nil {
		return nil, err
	}

	// QUALIFY.
	if qualify != nil {
		cache := map[string]int{}
		resolve := func(name string) (int, bool) {
			if idx, ok := hidden[name]; ok {
				return idx, true
			}
			if idx, ok := cache[name]; ok {
				return idx, idx >= 0
			}
			idx, err := rs.resolveWithAliases(stmt, name)
			if err != nil {
				idx = -1
			}
			cache[name] = idx
			return idx, err == nil
		}
		filtered := make([][]interface{}, 0, len(rs.rows))
		for _, row := range rs.rows {
			ok, err := evalWhere(qualify, row, resolve)
			if err != nil {
				return nil, fmt.Errorf("QUALIFY: %w", err)
			}
			if ok {
				filtered = append(filtered, row)
			}
		}
		rs.rows = filtered
	}

	// ORDER BY on the full rows, before projection, so it may reference any
	// source column, alias or window column.
	if len(stmt.OrderBy) > 0 {
		keys, err := orderKeys(stmt, rs)
		if err != nil {
			return nil, err
		}
		rs.rows = queryutil.SortRowsByKeys(rs.rows, keys)
	}

	columns, rows := projectRows(stmt, rs, itemIdx)

	if stmt.Distinct {
		rows = queryutil.RemoveDuplicateRows(rows)
	}
	rows = queryutil.ApplyLimitOffset(rows, stmt.Limit, stmt.Offset)

	return &Result{
		Columns: columns,
		Rows:    rows,
		Tag:     constants.ResultSelectTag(len(rows)),
	}, nil
}

// selectItemIndexes binds every non-window select-list item to its column in
// rs. Window items are left at -1 and filled in by computeWindows. On a
// grouped rowset the binding is positional (groupRows emits the select-list
// items first, in order); otherwise names resolve against the source columns.
func selectItemIndexes(stmt *ast.Select, rs *rowset, grouped bool) ([]int, error) {
	idxs := make([]int, len(stmt.Columns))
	next := 0
	for i, id := range stmt.Columns {
		idxs[i] = -1
		if id.Window != nil {
			continue
		}
		if grouped {
			idxs[i] = next
			next++
			continue
		}
		if id.Name == "" {
			if !stmt.AllowMissing {
				return nil, fmt.Errorf("column %s not found", outputName(id))
			}
			continue
		}
		idx, err := rs.resolve(id.Name)
		if err != nil {
			if errors.Is(err, errAmbiguousColumn) || !stmt.AllowMissing {
				return nil, err
			}
			continue
		}
		idxs[i] = idx
	}
	return idxs, nil
}

// outputName is the result column name of a select-list item.
func outputName(id ast.Identifier) string {
	if id.Alias != "" {
		return id.Alias
	}
	return id.Name
}

// orderKeys resolves the ORDER BY terms against the rowset. Unqualified names
// shared by several joined tables fall back to the first match, as ORDER BY
// historically tolerated; a term naming a placeholder select item (which
// projects as NULL) is ignored since every row compares equal on it.
func orderKeys(stmt *ast.Select, rs *rowset) ([]queryutil.OrderKey, error) {
	keys := make([]queryutil.OrderKey, 0, len(stmt.OrderBy))
	for _, ob := range stmt.OrderBy {
		idx, err := rs.resolveWithAliases(stmt, ob.Column.Name)
		if err != nil {
			if isPlaceholderAlias(stmt, ob.Column.Name) {
				continue
			}
			if loose := firstJoinColumnMatch(rs.cols, ob.Column.Name); loose >= 0 {
				idx = loose
			} else {
				return nil, fmt.Errorf("ORDER BY: %w", err)
			}
		}
		keys = append(keys, queryutil.OrderKey{Index: idx, Desc: ob.Direction == "DESC"})
	}
	return keys, nil
}

// projectRows builds the output column names and rows: every visible column
// for SELECT *, or the select-list items through their bound indexes.
func projectRows(stmt *ast.Select, rs *rowset, itemIdx []int) ([]string, [][]interface{}) {
	var columns []string
	var idxs []int

	if stmt.Star {
		for i, c := range rs.cols {
			if c.hidden {
				continue
			}
			name := c.name
			if rs.qualified {
				name = c.ref + "." + c.name
			}
			columns = append(columns, name)
			idxs = append(idxs, i)
		}
	} else {
		columns = make([]string, len(stmt.Columns))
		for i, id := range stmt.Columns {
			columns[i] = outputName(id)
		}
		idxs = itemIdx
	}

	// Identity projection: hand the rows through without copying.
	if len(idxs) == len(rs.cols) {
		identity := true
		for i, idx := range idxs {
			if idx != i {
				identity = false
				break
			}
		}
		if identity {
			return columns, rs.rows
		}
	}

	projected := make([][]interface{}, 0, len(rs.rows))
	for _, row := range rs.rows {
		out := make([]interface{}, len(idxs))
		for i, idx := range idxs {
			if idx < 0 || idx >= len(row) {
				out[i] = nil
				continue
			}
			out[i] = row[idx]
		}
		projected = append(projected, out)
	}
	return columns, projected
}

// ─── Grouping ────────────────────────────────────────────────────────────────

// isAggregateExpr reports whether a reference is aggregate call text.
func isAggregateExpr(ref string) bool {
	_, _, ok := parseAggregate(ref)
	return ok
}

// needsGrouping reports whether the query goes through the GROUP BY /
// aggregate stage: an explicit GROUP BY or an aggregate in the select list.
func needsGrouping(stmt *ast.Select) bool {
	if len(stmt.GroupBy) > 0 {
		return true
	}
	for _, c := range stmt.Columns {
		if c.Window == nil && isAggregateExpr(c.Name) {
			return true
		}
	}
	return false
}

// outerRefs lists every reference made outside the select list: window
// specifications, QUALIFY leaves (non-window) and ORDER BY terms.
func outerRefs(stmt *ast.Select) []string {
	refs := windowRefs(stmt)
	for _, leaf := range stmt.Qualify.Leaves() {
		if leaf.Column.Window == nil {
			refs = append(refs, leaf.Column.Name)
		}
	}
	for _, ob := range stmt.OrderBy {
		refs = append(refs, ob.Column.Name)
	}
	return refs
}

// firstAggregateRef returns the first aggregate expression referenced outside
// the select list, or "" when there is none.
func firstAggregateRef(stmt *ast.Select) string {
	for _, ref := range outerRefs(stmt) {
		if isAggregateExpr(ref) {
			return ref
		}
	}
	return ""
}

// groupCol is one column of the grouped rowset: either an aggregate computed
// per group or a plain column (first value of the group).
type groupCol struct {
	name   string // output name (alias or expression text)
	expr   string // source expression text
	aggFn  string // "" for a plain column
	srcIdx int    // source column index; -1 for COUNT(*) or a missing expression
	hidden bool   // not part of the select list (needed by windows/QUALIFY/ORDER BY)
}

// groupRows reduces rs to one row per group. The output rowset carries every
// select-list item (in order) plus hidden columns for aggregates and GROUP BY
// keys referenced by window functions, QUALIFY or ORDER BY that the select
// list does not already provide, so later stages can resolve them.
//
// Select-list columns that are neither aggregated nor grouped keep the
// engine's historical leniency (first value of the group); a hidden column
// may only be an aggregate or a GROUP BY key.
func groupRows(stmt *ast.Select, rs *rowset) (*rowset, error) {
	// Group keys.
	groupIdxs := make([]int, len(stmt.GroupBy))
	for i, g := range stmt.GroupBy {
		idx, err := rs.resolveWithAliases(stmt, g.Name)
		if err != nil {
			return nil, fmt.Errorf("GROUP BY: %w", err)
		}
		groupIdxs[i] = idx
	}
	isGroupKey := func(idx int) bool {
		for _, g := range groupIdxs {
			if g == idx {
				return true
			}
		}
		return false
	}

	var cols []groupCol
	find := func(ref string) int {
		want := normExpr(ref)
		for i, c := range cols {
			if normExpr(c.expr) == want || (c.expr != "" && strings.EqualFold(c.name, ref)) {
				return i
			}
		}
		return -1
	}
	add := func(name, expr string, hidden bool) error {
		gc := groupCol{name: name, expr: expr, hidden: hidden, srcIdx: -1}
		if expr == "" {
			cols = append(cols, gc) // placeholder expression → NULL
			return nil
		}
		if fn, arg, ok := parseAggregate(expr); ok {
			gc.aggFn = fn
			if arg != "" && arg != "*" {
				idx, err := rs.resolve(arg)
				if err != nil {
					return fmt.Errorf("%s: %w", expr, err)
				}
				gc.srcIdx = idx
			}
		} else {
			idx, err := rs.resolveWithAliases(stmt, expr)
			if err != nil {
				return err
			}
			if hidden && !isGroupKey(idx) {
				return fmt.Errorf("column %s must appear in GROUP BY or be used in an aggregate function", expr)
			}
			gc.srcIdx = idx
		}
		cols = append(cols, gc)
		return nil
	}

	// (a) Select-list items (window items are computed later, over the groups).
	for _, id := range stmt.Columns {
		if id.Window != nil {
			continue
		}
		if err := add(outputName(id), id.Name, false); err != nil {
			return nil, err
		}
	}

	// (b) Hidden columns for everything else the later stages reference.
	extra := make([]string, 0, len(stmt.GroupBy))
	for _, g := range stmt.GroupBy {
		extra = append(extra, g.Name)
	}
	extra = append(extra, outerRefs(stmt)...)
	for _, ref := range extra {
		if ref == "" || find(ref) >= 0 || isAlias(stmt, ref) {
			continue
		}
		if err := add(ref, ref, true); err != nil {
			return nil, err
		}
	}

	// Partition the rows, preserving first-appearance order. Without GROUP BY
	// every row (even none) forms a single group.
	groups := map[string][][]interface{}{}
	var order []string
	if len(groupIdxs) == 0 {
		order = []string{""}
		groups[""] = rs.rows
	} else {
		for _, row := range rs.rows {
			key := partitionKey(row, groupIdxs)
			if _, ok := groups[key]; !ok {
				order = append(order, key)
			}
			groups[key] = append(groups[key], row)
		}
	}

	outRows := make([][]interface{}, 0, len(order))
	for _, key := range order {
		groupRows := groups[key]
		row := make([]interface{}, len(cols))
		for i, c := range cols {
			switch {
			case c.aggFn != "":
				row[i] = computeAggregate(c.aggFn, groupRows, c.srcIdx)
			case c.srcIdx >= 0 && len(groupRows) > 0 && c.srcIdx < len(groupRows[0]):
				row[i] = groupRows[0][c.srcIdx] // non-aggregate: first value of the group
			default:
				row[i] = nil
			}
		}
		outRows = append(outRows, row)
	}

	outCols := make([]joinColumn, len(cols))
	for i, c := range cols {
		outCols[i] = joinColumn{name: c.name, expr: c.expr, typ: "TEXT", hidden: c.hidden}
	}
	return &rowset{cols: outCols, rows: outRows}, nil
}

// isAlias reports whether ref names a select-list alias.
func isAlias(stmt *ast.Select, ref string) bool {
	for _, id := range stmt.Columns {
		if id.Alias != "" && strings.EqualFold(id.Alias, ref) {
			return true
		}
	}
	return false
}

// partitionKey renders the values at idxs as a canonical string key. Values are
// compared by their printed form, consistent with GROUP BY and JOIN matching.
func partitionKey(row []interface{}, idxs []int) string {
	if len(idxs) == 0 {
		return ""
	}
	parts := make([]string, len(idxs))
	for i, idx := range idxs {
		if idx >= 0 && idx < len(row) {
			parts[i] = fmt.Sprintf("%v", row[idx])
		}
	}
	return strings.Join(parts, "\x1f")
}
