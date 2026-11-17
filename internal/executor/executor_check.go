package executor

import (
	"fmt"
	"sync"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/constants"
	"dbf/internal/parser"
)

// checkCache memoizes compiled CHECK predicates keyed by their source text, so a
// constraint is parsed once rather than on every row. CHECK expressions are
// re-parsed (not gob-decoded) to keep persistence AST-independent.
var checkCache sync.Map // string -> *ast.WhereClause

// compileCheck parses a CHECK expression text into a predicate tree, caching it.
func compileCheck(text string) (*ast.WhereClause, error) {
	if v, ok := checkCache.Load(text); ok {
		return v.(*ast.WhereClause), nil
	}
	w, err := parser.ParsePredicate(text)
	if err != nil {
		return nil, fmt.Errorf("invalid CHECK expression %q: %w", text, err)
	}
	checkCache.Store(text, w)
	return w, nil
}

// tableHasChecks reports whether a table carries any CHECK constraint.
func tableHasChecks(table *catalog.Table) bool {
	for _, c := range table.Constraints {
		if c.Type == constants.ConstraintCheck {
			return true
		}
	}
	return false
}

// enforceRowChecks verifies every CHECK constraint against a full row, returning
// an error on the first violation. A CHECK predicate references the row's own
// columns and is evaluated with the same comparison semantics as WHERE.
func (e *Executor) enforceRowChecks(table *catalog.Table, row []interface{}) error {
	resolve := columnResolver(table)
	for _, c := range table.Constraints {
		if c.Type != constants.ConstraintCheck || c.CheckExpr == "" {
			continue
		}
		pred, err := compileCheck(c.CheckExpr)
		if err != nil {
			return err
		}
		ok, err := evalCheckPredicate(pred, row, resolve)
		if err != nil {
			return fmt.Errorf("CHECK constraint %s: %w", checkLabel(c), err)
		}
		if !ok {
			return fmt.Errorf("new row for table %s violates CHECK constraint %s", table.Name, checkLabel(c))
		}
	}
	return nil
}

// checkRowsAfterUpdate enforces CHECK constraints against the post-update image
// of every row the WHERE clause selects: it snapshots the matched rows, applies
// the assignments to the copies, and validates them before the real mutation.
func (e *Executor) checkRowsAfterUpdate(table *catalog.Table, where *ast.WhereClause, apply func([]interface{})) error {
	if !tableHasChecks(table) {
		return nil
	}
	rows, err := e.snapshotMatchedRows(table, where)
	if err != nil {
		return err
	}
	for _, row := range rows {
		apply(row)
		if err := e.enforceRowChecks(table, row); err != nil {
			return err
		}
	}
	return nil
}

// evalCheckPredicate evaluates a CHECK predicate against a row. Unlike the plain
// WHERE evaluator, the right-hand operand of a comparison may be another column
// of the same row (CHECK (fin > inicio)): an identifier operand that resolves to
// a column is read from the row; otherwise it is treated as a literal. IN / EXISTS
// (unusual in a CHECK) defer to the standard evaluator.
func evalCheckPredicate(where *ast.WhereClause, row []interface{}, resolve func(string) (int, bool)) (bool, error) {
	if where == nil {
		return true, nil
	}
	if where.Conj != "" {
		left, err := evalCheckPredicate(where.Left, row, resolve)
		if err != nil {
			return false, err
		}
		if where.Conj == "OR" && left {
			return true, nil
		}
		if where.Conj == "AND" && !left {
			return false, nil
		}
		return evalCheckPredicate(where.Right, row, resolve)
	}
	if where.Operator == "IN" || where.Operator == "EXISTS" || where.Sub != nil {
		return evalWhere(where, row, resolve)
	}

	idx, ok := resolve(where.Column.Name)
	if !ok {
		return false, fmt.Errorf("column %s not found", where.Column.Name)
	}
	if idx < 0 || idx >= len(row) {
		return false, nil
	}
	if where.Operator == "BETWEEN" {
		lo := checkOperand(where.Value, row, resolve)
		hi := checkOperand(where.Value2, row, resolve)
		return compareCells(row[idx], lo) >= 0 && compareCells(row[idx], hi) <= 0, nil
	}
	rhs := checkOperand(where.Value, row, resolve)
	return whereMatches(row[idx], where.Operator, rhs), nil
}

// checkOperand resolves a CHECK comparison operand: an identifier that names a
// column of the row yields that column's value (enabling column-to-column
// comparisons); anything else is the literal's own value.
func checkOperand(lit ast.Literal, row []interface{}, resolve func(string) (int, bool)) interface{} {
	if lit.Kind == "identifier" {
		if idx, ok := resolve(lit.Value); ok && idx >= 0 && idx < len(row) {
			return row[idx]
		}
	}
	return lit.Value
}

// checkLabel names a CHECK constraint for error messages: its name if given,
// otherwise its expression text.
func checkLabel(c catalog.Constraint) string {
	if c.Name != "" {
		return c.Name
	}
	return fmt.Sprintf("(%s)", c.CheckExpr)
}
