package executor

import (
	"fmt"
	"sort"

	"dbf/internal/ast"
	"dbf/internal/queryutil"
)

// windowUse is one window-function occurrence in a query: a select-list item
// (item = its index in stmt.Columns) or a QUALIFY leaf (leaf != nil,
// materialized as a hidden column).
type windowUse struct {
	w    *ast.WindowFunc
	item int
	leaf *ast.WhereClause
}

// collectWindows lists every window-function call in the select list and the
// QUALIFY predicate, in that order.
func collectWindows(stmt *ast.Select) []windowUse {
	var uses []windowUse
	for i, id := range stmt.Columns {
		if id.Window != nil {
			uses = append(uses, windowUse{w: id.Window, item: i})
		}
	}
	for _, leaf := range stmt.Qualify.Leaves() {
		if leaf.Column.Window != nil {
			uses = append(uses, windowUse{w: leaf.Column.Window, item: -1, leaf: leaf})
		}
	}
	return uses
}

// windowRefs returns every column/expression referenced inside the window
// specifications of a query (argument, PARTITION BY and ORDER BY terms).
func windowRefs(stmt *ast.Select) []string {
	var refs []string
	for _, u := range collectWindows(stmt) {
		if u.w.Arg != "" && u.w.Arg != "*" {
			refs = append(refs, u.w.Arg)
		}
		for _, p := range u.w.PartitionBy {
			refs = append(refs, p.Name)
		}
		for _, o := range u.w.OrderBy {
			refs = append(refs, o.Column.Name)
		}
	}
	return refs
}

// computeWindows evaluates every window function of the query over rs and
// appends one column per call. Select-list windows bind their column index
// into itemIdx; QUALIFY inline windows become hidden columns whose indexes are
// returned by name, and the returned predicate is a copy of stmt.Qualify whose
// window leaves point at them (the statement AST itself is never mutated,
// since views share it).
func computeWindows(stmt *ast.Select, rs *rowset, itemIdx []int) (*ast.WhereClause, map[string]int, error) {
	uses := collectWindows(stmt)
	if len(uses) == 0 {
		return stmt.Qualify, nil, nil
	}

	base := len(rs.cols)
	newCols := make([]joinColumn, 0, len(uses))
	values := make([][]interface{}, 0, len(uses))
	subst := map[*ast.WhereClause]string{}
	hidden := map[string]int{}

	resolve := func(ref, clause string) (int, error) {
		idx, err := rs.resolveWithAliases(stmt, ref)
		if err != nil {
			return -1, fmt.Errorf("%s: %w", clause, err)
		}
		return idx, nil
	}

	for i, u := range uses {
		argIdx := -1
		if u.w.Arg != "" && u.w.Arg != "*" {
			idx, err := resolve(u.w.Arg, u.w.Func+"("+u.w.Arg+")")
			if err != nil {
				return nil, nil, err
			}
			argIdx = idx
		}
		partIdx := make([]int, len(u.w.PartitionBy))
		for k, p := range u.w.PartitionBy {
			idx, err := resolve(p.Name, "PARTITION BY")
			if err != nil {
				return nil, nil, err
			}
			partIdx[k] = idx
		}
		keys := make([]queryutil.OrderKey, len(u.w.OrderBy))
		for k, o := range u.w.OrderBy {
			idx, err := resolve(o.Column.Name, "OVER (ORDER BY)")
			if err != nil {
				return nil, nil, err
			}
			keys[k] = queryutil.OrderKey{Index: idx, Desc: o.Direction == "DESC"}
		}

		colIdx := base + len(newCols)
		var name string
		if u.leaf != nil {
			name = fmt.Sprintf("__qualify_%d", i)
			subst[u.leaf] = name
			hidden[name] = colIdx
		} else {
			name = stmt.Columns[u.item].Alias
			itemIdx[u.item] = colIdx
		}
		newCols = append(newCols, joinColumn{name: name, typ: windowType(u.w.Func), hidden: u.leaf != nil})
		values = append(values, evalWindow(u.w.Func, rs.rows, argIdx, partIdx, keys))
	}

	// Widen every row with fresh slices: source rows may share backing arrays
	// with the catalog, so appending in place would be unsafe.
	rows := make([][]interface{}, len(rs.rows))
	for r, old := range rs.rows {
		nr := make([]interface{}, base+len(newCols))
		copy(nr, old)
		for k := range newCols {
			nr[base+k] = values[k][r]
		}
		rows[r] = nr
	}
	rs.rows = rows
	rs.cols = append(append([]joinColumn{}, rs.cols...), newCols...)

	return substituteLeaves(stmt.Qualify, subst), hidden, nil
}

// windowType gives a nominal column type for a window function's output.
func windowType(fn string) string {
	if isRankingWindow(fn) || fn == "COUNT" {
		return "INTEGER"
	}
	return "NUMERIC"
}

// substituteLeaves returns a copy of the predicate tree where leaves present
// in subst reference the named column instead of an inline window call.
func substituteLeaves(w *ast.WhereClause, subst map[*ast.WhereClause]string) *ast.WhereClause {
	if w == nil {
		return nil
	}
	if w.Conj != "" {
		return &ast.WhereClause{
			Conj:  w.Conj,
			Left:  substituteLeaves(w.Left, subst),
			Right: substituteLeaves(w.Right, subst),
		}
	}
	leaf := *w
	if name, ok := subst[w]; ok {
		leaf.Column = ast.Identifier{Name: name}
	}
	return &leaf
}

// evalWindow computes one window function over rows and returns a value per
// row (same order as rows).
//
// Rows are partitioned by partIdx (first-appearance order) and, inside each
// partition, ordered by keys. Rows equal on every key are peers. Without an
// ORDER BY every row of the partition is a peer of every other.
//
//   - ROW_NUMBER: 1..n in window order.
//   - RANK: 1 + number of rows preceding the peer group (gaps).
//   - DENSE_RANK: 1-based index of the peer group (no gaps).
//   - COUNT/SUM/AVG/MIN/MAX: aggregate over the standard default frame,
//     RANGE BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW — i.e. every row up to
//     and including the current peer group (the whole partition without ORDER BY).
//     The frame only grows, so the aggregate is accumulated incrementally.
func evalWindow(fn string, rows [][]interface{}, argIdx int, partIdx []int, keys []queryutil.OrderKey) []interface{} {
	out := make([]interface{}, len(rows))

	parts := map[string][]int{}
	var order []string
	for i, row := range rows {
		key := partitionKey(row, partIdx)
		if _, ok := parts[key]; !ok {
			order = append(order, key)
		}
		parts[key] = append(parts[key], i)
	}

	for _, key := range order {
		idxs := parts[key]
		if len(keys) > 0 {
			sort.SliceStable(idxs, func(a, b int) bool {
				return queryutil.CompareByKeys(rows[idxs[a]], rows[idxs[b]], keys) < 0
			})
		}

		acc := windowAccumulator{fn: fn, argIdx: argIdx}
		dense := 0
		for pos := 0; pos < len(idxs); {
			// Find the end of the current peer group.
			end := len(idxs)
			if len(keys) > 0 {
				end = pos + 1
				for end < len(idxs) && queryutil.CompareByKeys(rows[idxs[pos]], rows[idxs[end]], keys) == 0 {
					end++
				}
			}
			dense++

			var agg interface{}
			if !isRankingWindow(fn) {
				for _, ri := range idxs[pos:end] {
					acc.add(rows[ri])
				}
				agg = acc.value()
			}

			for j := pos; j < end; j++ {
				switch fn {
				case "ROW_NUMBER":
					out[idxs[j]] = j + 1
				case "RANK":
					out[idxs[j]] = pos + 1
				case "DENSE_RANK":
					out[idxs[j]] = dense
				default:
					out[idxs[j]] = agg
				}
			}
			pos = end
		}
	}
	return out
}

func isRankingWindow(fn string) bool {
	return fn == "ROW_NUMBER" || fn == "RANK" || fn == "DENSE_RANK"
}

// windowAccumulator keeps a running aggregate over a growing frame with the
// same semantics as computeAggregate: COUNT(*) counts every row, COUNT(col)
// non-NULL values, SUM/AVG only values that parse as numbers, MIN/MAX order
// non-NULL values with compareCells.
type windowAccumulator struct {
	fn     string
	argIdx int
	rows   int
	nonNil int
	numCnt int
	sum    float64
	best   interface{}
}

func (a *windowAccumulator) add(row []interface{}) {
	a.rows++
	if a.argIdx < 0 || a.argIdx >= len(row) || row[a.argIdx] == nil {
		return
	}
	v := row[a.argIdx]
	a.nonNil++
	switch a.fn {
	case "SUM", "AVG":
		if f, ok := toFloat(v); ok {
			a.sum += f
			a.numCnt++
		}
	case "MIN", "MAX":
		if a.best == nil {
			a.best = v
			return
		}
		c := compareCells(v, a.best)
		if (a.fn == "MIN" && c < 0) || (a.fn == "MAX" && c > 0) {
			a.best = v
		}
	}
}

func (a *windowAccumulator) value() interface{} {
	switch a.fn {
	case "COUNT":
		if a.argIdx < 0 {
			return a.rows
		}
		return a.nonNil
	case "SUM":
		return numberValue(a.sum)
	case "AVG":
		if a.numCnt == 0 {
			return nil
		}
		return a.sum / float64(a.numCnt)
	case "MIN", "MAX":
		return a.best
	}
	return nil
}
