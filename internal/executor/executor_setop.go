package executor

import (
	"context"
	"fmt"
	"strings"

	"dbf/internal/ast"
	"dbf/internal/constants"
	"dbf/internal/queryutil"
)

func init() {
	registerExec((*Executor).executeSetOp)
}

// executeSetOp evaluates a chain of set operations (UNION / UNION ALL), folding
// the operands left to right. UNION deduplicates the accumulated rows at that
// boundary; UNION ALL keeps duplicates. The output column names are taken from
// the first operand, and every operand must yield the same number of columns.
// A trailing ORDER BY / LIMIT / OFFSET applies to the combined result.
func (e *Executor) executeSetOp(ctx context.Context, stmt *ast.SetOp) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	if len(stmt.Selects) == 0 {
		return &Result{Tag: constants.ResultSelectTag(0)}, nil
	}

	first, err := e.executeSelect(ctx, stmt.Selects[0])
	if err != nil {
		return nil, err
	}
	columns := first.Columns
	rows := first.Rows

	for i := 1; i < len(stmt.Selects); i++ {
		r, err := e.executeSelect(ctx, stmt.Selects[i])
		if err != nil {
			return nil, err
		}
		if len(r.Columns) != len(columns) {
			return nil, fmt.Errorf("each UNION query must have the same number of columns (%d vs %d)", len(columns), len(r.Columns))
		}
		rows = append(rows, r.Rows...)
		// All[i-1] is the operator between Selects[i-1] and Selects[i].
		if i-1 < len(stmt.All) && !stmt.All[i-1] {
			rows = queryutil.RemoveDuplicateRows(rows)
		}
	}

	// Trailing ORDER BY resolves against the output column names.
	if len(stmt.OrderBy) > 0 {
		keys := make([]queryutil.OrderKey, 0, len(stmt.OrderBy))
		for _, ob := range stmt.OrderBy {
			idx := -1
			for j, name := range columns {
				if strings.EqualFold(name, ob.Column.Name) {
					idx = j
					break
				}
			}
			if idx == -1 {
				return nil, fmt.Errorf("ORDER BY: column %s not found in UNION result", ob.Column.Name)
			}
			keys = append(keys, queryutil.OrderKey{Index: idx, Desc: ob.Direction == "DESC"})
		}
		rows = queryutil.SortRowsByKeys(rows, keys)
	}

	rows = queryutil.ApplyLimitOffset(rows, stmt.Limit, stmt.Offset)

	return &Result{
		Columns: columns,
		Rows:    rows,
		Tag:     constants.ResultSelectTag(len(rows)),
	}, nil
}
