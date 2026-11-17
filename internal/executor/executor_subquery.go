package executor

import (
	"context"
	"fmt"

	"dbf/internal/ast"
	"dbf/internal/catalog"
)

// whereHasSubquery reports whether a predicate tree contains a subquery, IN or
// EXISTS leaf that needs executor-backed evaluation.
func whereHasSubquery(where *ast.WhereClause) bool {
	if where == nil {
		return false
	}
	if where.Conj != "" {
		return whereHasSubquery(where.Left) || whereHasSubquery(where.Right)
	}
	return where.Sub != nil || where.Operator == "EXISTS" || (where.Operator == "IN" && where.Sub != nil)
}

// evalWhereSub evaluates a predicate tree that may contain subqueries against a
// row. Non-subquery leaves (including IN with a literal list) defer to evalWhere;
// subquery/EXISTS/IN-subquery leaves run their inner SELECT, binding correlated
// references to the outer row's values.
func (e *Executor) evalWhereSub(ctx context.Context, where *ast.WhereClause, row []interface{}, resolve func(string) (int, bool)) (bool, error) {
	if where == nil {
		return true, nil
	}
	if where.Conj != "" {
		left, err := e.evalWhereSub(ctx, where.Left, row, resolve)
		if err != nil {
			return false, err
		}
		if where.Conj == "OR" && left {
			return true, nil
		}
		if where.Conj == "AND" && !left {
			return false, nil
		}
		return e.evalWhereSub(ctx, where.Right, row, resolve)
	}

	switch {
	case where.Operator == "EXISTS":
		res, err := e.runSubquery(ctx, where.Sub, row, resolve)
		if err != nil {
			return false, err
		}
		exists := len(res.Rows) > 0
		if where.Negate {
			return !exists, nil
		}
		return exists, nil

	case where.Operator == "IN" && where.Sub != nil:
		idx, ok := resolve(where.Column.Name)
		if !ok {
			return false, fmt.Errorf("column %s not found", where.Column.Name)
		}
		if idx < 0 || idx >= len(row) {
			return false, nil
		}
		res, err := e.runSubquery(ctx, where.Sub, row, resolve)
		if err != nil {
			return false, err
		}
		found := false
		for _, r := range res.Rows {
			if len(r) > 0 && compareCells(row[idx], r[0]) == 0 {
				found = true
				break
			}
		}
		if where.Negate {
			return !found, nil
		}
		return found, nil

	case where.Sub != nil:
		// Scalar subquery: col OP (SELECT one value).
		idx, ok := resolve(where.Column.Name)
		if !ok {
			return false, fmt.Errorf("column %s not found", where.Column.Name)
		}
		if idx < 0 || idx >= len(row) {
			return false, nil
		}
		res, err := e.runSubquery(ctx, where.Sub, row, resolve)
		if err != nil {
			return false, err
		}
		var rhs interface{}
		if len(res.Rows) > 0 && len(res.Rows[0]) > 0 {
			rhs = res.Rows[0][0]
		}
		return whereMatches(row[idx], where.Operator, rhs), nil

	default:
		return evalWhere(where, row, resolve)
	}
}

// runSubquery executes an inner SELECT, binding any correlated references to the
// outer row before running it. Correlation is resolved by replacing identifier-
// valued predicates in the inner WHERE whose name resolves against the outer row
// (e.g. inner `x.dept = e.dept` where `e.dept` is an outer column).
func (e *Executor) runSubquery(ctx context.Context, sub *ast.Select, outerRow []interface{}, outerResolve func(string) (int, bool)) (*Result, error) {
	bound := bindOuterRefs(sub, outerRow, outerResolve)
	return e.executeSelect(ctx, bound)
}

// bindOuterRefs returns a shallow copy of sub whose WHERE tree has correlated
// identifier references replaced by the outer row's values. The original AST is
// never mutated (view/function queries share their AST).
func bindOuterRefs(sub *ast.Select, outerRow []interface{}, outerResolve func(string) (int, bool)) *ast.Select {
	if sub == nil || sub.Where == nil {
		return sub
	}
	newWhere := bindWhereRefs(sub.Where, outerRow, outerResolve)
	cp := *sub
	cp.Where = newWhere
	return &cp
}

func bindWhereRefs(w *ast.WhereClause, row []interface{}, resolve func(string) (int, bool)) *ast.WhereClause {
	if w == nil {
		return nil
	}
	if w.Conj != "" {
		return &ast.WhereClause{
			Conj:  w.Conj,
			Left:  bindWhereRefs(w.Left, row, resolve),
			Right: bindWhereRefs(w.Right, row, resolve),
		}
	}
	nw := *w
	// A comparison whose right-hand side is an identifier that resolves against
	// the OUTER row is a correlated reference: bind it to the outer value.
	if w.Sub == nil && w.Value.Kind == "identifier" {
		if idx, ok := resolve(w.Value.Value); ok && idx >= 0 && idx < len(row) {
			nw.Value = ast.Literal{Kind: "bound", Value: fmt.Sprintf("%v", row[idx])}
		}
	}
	return &nw
}

// derivedTable runs a derived-table subquery (FROM (SELECT ...) alias) and wraps
// its result as an in-memory table whose name is the alias.
func (e *Executor) derivedTable(ctx context.Context, id ast.Identifier) (*catalog.Table, error) {
	res, err := e.executeSelect(ctx, id.Subquery)
	if err != nil {
		return nil, fmt.Errorf("derived table: %w", err)
	}
	cols := make([]catalog.Column, len(res.Columns))
	for i, n := range res.Columns {
		cols[i] = catalog.Column{Name: n, Type: "TEXT"}
	}
	name := id.Alias
	if name == "" {
		name = "subquery"
	}
	return &catalog.Table{Name: name, Columns: cols, Rows: res.Rows}, nil
}
