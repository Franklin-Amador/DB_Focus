package queryutil

import (
	"fmt"
	"sort"
	"strings"

	"dbf/internal/ast"
	"dbf/internal/catalog"
)

// IndexOfColumn finds the index of a column by name (case-insensitive)
func IndexOfColumn(cols []catalog.Column, name string) int {
	if name == "" {
		return -1
	}
	for i, col := range cols {
		if strings.EqualFold(col.Name, name) {
			return i
		}
	}
	return -1
}

// SplitQualified splits a qualified column reference into table and column name
// Example: "users.id" -> ("users", "id"), "name" -> ("", "name")
func SplitQualified(name string) (qualifier string, columnName string) {
	idx := strings.LastIndex(name, ".")
	if idx == -1 {
		return "", name
	}
	return name[:idx], name[idx+1:]
}

// ResolveJoinColumn resolves a column reference in a JOIN context
// Returns (isLeftTable bool, columnIndex int, error)
func ResolveJoinColumn(left, right *catalog.Table, leftTableName, rightTableName, columnRef string) (bool, int, error) {
	qualifier, colName := SplitQualified(columnRef)

	// If explicitly qualified, resolve to specific table
	if qualifier != "" {
		switch {
		case strings.EqualFold(qualifier, leftTableName):
			idx := IndexOfColumn(left.Columns, colName)
			if idx == -1 {
				return false, -1, fmt.Errorf("column %s not found in table %s", colName, leftTableName)
			}
			return true, idx, nil
		case strings.EqualFold(qualifier, rightTableName):
			idx := IndexOfColumn(right.Columns, colName)
			if idx == -1 {
				return false, -1, fmt.Errorf("column %s not found in table %s", colName, rightTableName)
			}
			return false, idx, nil
		default:
			return false, -1, fmt.Errorf("unknown table qualifier %s", qualifier)
		}
	}

	// Unqualified reference - search both tables
	leftIdx := IndexOfColumn(left.Columns, colName)
	rightIdx := IndexOfColumn(right.Columns, colName)

	switch {
	case leftIdx == -1 && rightIdx == -1:
		return false, -1, fmt.Errorf("column %s not found in either table", colName)
	case leftIdx != -1 && rightIdx != -1:
		return false, -1, fmt.Errorf("ambiguous column reference %s (exists in both tables)", colName)
	case leftIdx != -1:
		return true, leftIdx, nil
	default:
		return false, rightIdx, nil
	}
}

// ResolveCombinedIndex resolves a column reference to an index in the combined result set
// (left columns first, then right columns)
func ResolveCombinedIndex(left, right *catalog.Table, leftTableName, rightTableName, columnRef string) (int, error) {
	qualifier, colName := SplitQualified(columnRef)

	if qualifier != "" {
		switch {
		case strings.EqualFold(qualifier, leftTableName):
			idx := IndexOfColumn(left.Columns, colName)
			if idx == -1 {
				return -1, fmt.Errorf("column %s not found in table %s", colName, leftTableName)
			}
			return idx, nil
		case strings.EqualFold(qualifier, rightTableName):
			idx := IndexOfColumn(right.Columns, colName)
			if idx == -1 {
				return -1, fmt.Errorf("column %s not found in table %s", colName, rightTableName)
			}
			return len(left.Columns) + idx, nil
		default:
			return -1, fmt.Errorf("unknown table qualifier %s", qualifier)
		}
	}

	// Unqualified - search both tables
	leftIdx := IndexOfColumn(left.Columns, colName)
	rightIdx := IndexOfColumn(right.Columns, colName)

	switch {
	case leftIdx == -1 && rightIdx == -1:
		return -1, fmt.Errorf("column %s not found in either table", colName)
	case leftIdx != -1 && rightIdx != -1:
		return -1, fmt.Errorf("ambiguous column reference %s", colName)
	case leftIdx != -1:
		return leftIdx, nil
	default:
		return len(left.Columns) + rightIdx, nil
	}
}

// GetRowValue retrieves a value from either left or right row based on isLeft flag
func GetRowValue(isLeft bool, leftRow, rightRow []interface{}, index int) interface{} {
	switch isLeft {
	case true:
		if index >= len(leftRow) {
			return nil
		}
		return leftRow[index]
	default:
		if index >= len(rightRow) {
			return nil
		}
		return rightRow[index]
	}
}

// IdentifiersToNames converts AST identifiers to string column names (with aliases)
func IdentifiersToNames(identifiers []ast.Identifier) []string {
	if len(identifiers) == 0 {
		return nil
	}
	names := make([]string, len(identifiers))
	for i, id := range identifiers {
		name := id.Name
		if id.Alias != "" {
			name = id.Alias
		}
		names[i] = name
	}
	return names
}

// RemoveDuplicateRows removes duplicate rows from a result set
func RemoveDuplicateRows(rows [][]interface{}) [][]interface{} {
	if len(rows) == 0 {
		return rows
	}

	seen := make(map[string]bool, len(rows))
	result := make([][]interface{}, 0, len(rows))

	for _, row := range rows {
		// Build a stable key from row values
		parts := make([]string, len(row))
		for i, v := range row {
			parts[i] = fmt.Sprintf("%v", v)
		}
		key := strings.Join(parts, "\x1f")
		if !seen[key] {
			seen[key] = true
			// copy row to avoid aliasing
			rcopy := make([]interface{}, len(row))
			copy(rcopy, row)
			result = append(result, rcopy)
		}
	}

	return result
}

// OrderKey is a resolved ORDER BY term: the row index to compare and its
// direction. Callers resolve column names to indexes themselves, which lets the
// same comparator serve ORDER BY, window partitions and peer detection.
type OrderKey struct {
	Index int
	Desc  bool
}

// CompareByKeys orders two rows by the given keys and returns -1, 0 or 1.
// NULL sorts before every other value in ascending order (and therefore after
// them in descending order). Rows that are equal on every key compare 0, which
// window functions use to detect peers.
func CompareByKeys(a, b []interface{}, keys []OrderKey) int {
	for _, k := range keys {
		if k.Index < 0 || k.Index >= len(a) || k.Index >= len(b) {
			continue
		}
		va, vb := a[k.Index], b[k.Index]
		var cmp int
		switch {
		case va == nil && vb == nil:
			continue
		case va == nil:
			cmp = -1
		case vb == nil:
			cmp = 1
		default:
			cmp = compareValues(va, vb)
		}
		if cmp == 0 {
			continue
		}
		if k.Desc {
			return -cmp
		}
		return cmp
	}
	return 0
}

// SortRowsByKeys returns a stably sorted copy of rows ordered by keys.
func SortRowsByKeys(rows [][]interface{}, keys []OrderKey) [][]interface{} {
	if len(keys) == 0 || len(rows) == 0 {
		return rows
	}
	sorted := make([][]interface{}, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		return CompareByKeys(sorted[i], sorted[j], keys) < 0
	})
	return sorted
}

// compareValues compares two values and returns -1, 0, or 1
// compareValues and numeric helpers live in compare.go

// ApplyLimitOffset applies LIMIT and OFFSET to result rows
func ApplyLimitOffset(rows [][]interface{}, limit, offset int) [][]interface{} {
	if len(rows) == 0 {
		return rows
	}

	// Apply OFFSET
	if offset > 0 {
		if offset >= len(rows) {
			return [][]interface{}{}
		}
		rows = rows[offset:]
	}

	// Apply LIMIT
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}

	return rows
}
