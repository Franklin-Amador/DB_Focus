package catalog

import "strings"

func columnIndex(columns []Column, name string) int {
	for i, col := range columns {
		if col.Name == name {
			return i
		}
	}
	return -1
}

// copyRows makes a deep-enough copy of rows for safe concurrent use.
// When all rows have the same column count (the common case), a single
// contiguous backing array is allocated for all cells, reducing N+1
// heap allocations to just 2.
func copyRows(rows [][]interface{}) [][]interface{} {
	if len(rows) == 0 {
		return nil
	}

	numCols := len(rows[0])
	uniform := numCols > 0
	if uniform {
		for _, row := range rows {
			if len(row) != numCols {
				uniform = false
				break
			}
		}
	}

	result := make([][]interface{}, len(rows))

	if uniform {
		// Single allocation for all cell data: 2 allocs instead of N+1.
		flat := make([]interface{}, len(rows)*numCols)
		for i, row := range rows {
			start := i * numCols
			copy(flat[start:], row)
			result[i] = flat[start : start+numCols : start+numCols]
		}
		return result
	}

	// Fallback for variable-width rows.
	for i, row := range rows {
		rowCopy := make([]interface{}, len(row))
		copy(rowCopy, row)
		result[i] = rowCopy
	}
	return result
}

// splitQualifiedName splits a qualified name like "schema.table" into parts
func splitQualifiedName(name string) []string {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts
	}
	return []string{name}
}
