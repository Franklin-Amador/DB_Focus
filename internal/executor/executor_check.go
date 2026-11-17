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
		ok, err := evalWhere(pred, row, resolve)
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

// checkLabel names a CHECK constraint for error messages: its name if given,
// otherwise its expression text.
func checkLabel(c catalog.Constraint) string {
	if c.Name != "" {
		return c.Name
	}
	return fmt.Sprintf("(%s)", c.CheckExpr)
}
