package executor

import (
	"fmt"
	"strconv"

	"dbf/internal/ast"
)

// evalWhere evaluates a (possibly compound) WHERE predicate tree against a row.
// resolve maps a column name to its index within the row, reporting ok=false
// when the column is unknown. A nil clause matches everything.
func evalWhere(where *ast.WhereClause, row []interface{}, resolve func(string) (int, bool)) (bool, error) {
	if where == nil {
		return true, nil
	}
	if where.Conj != "" {
		left, err := evalWhere(where.Left, row, resolve)
		if err != nil {
			return false, err
		}
		// Short-circuit like SQL boolean logic.
		if where.Conj == "OR" && left {
			return true, nil
		}
		if where.Conj == "AND" && !left {
			return false, nil
		}
		return evalWhere(where.Right, row, resolve)
	}

	idx, ok := resolve(where.Column.Name)
	if !ok {
		return false, fmt.Errorf("column %s not found", where.Column.Name)
	}
	if idx < 0 || idx >= len(row) {
		return false, nil
	}
	return whereMatches(row[idx], where.Operator, where.Value.Value), nil
}

// whereMatches reports whether a stored cell satisfies `cell <op> target`.
//
// All operators compare by canonical value: numerically when both operands
// parse as numbers, lexicographically otherwise. Equality must NOT use Go's
// strict `==` because the engine stores mixed Go types per column — literals
// arrive as string while IDENTITY-generated keys are int — so `WHERE id = 1`
// on a scan (UPDATE/DELETE, or SELECT without index) would silently match
// nothing. This mirrors the index path, whose keys are canonical strings, and
// JOIN ON / FK validation, which already compare by canonical form.
func whereMatches(cell interface{}, op string, target interface{}) bool {
	c := compareCells(cell, target)
	switch op {
	case "", "=":
		return c == 0
	case "<>", "!=":
		return c != 0
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	case ">=":
		return c >= 0
	default:
		return false
	}
}

// compareCells orders two values, returning -1, 0, or 1. If both render as
// numbers they are compared numerically; otherwise they are compared as
// strings.
func compareCells(a, b interface{}) int {
	as := fmt.Sprintf("%v", a)
	bs := fmt.Sprintf("%v", b)

	af, aerr := strconv.ParseFloat(as, 64)
	bf, berr := strconv.ParseFloat(bs, 64)
	if aerr == nil && berr == nil {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	}

	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}
