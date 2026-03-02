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

func copyRows(rows [][]interface{}) [][]interface{} {
	result := make([][]interface{}, len(rows))
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
