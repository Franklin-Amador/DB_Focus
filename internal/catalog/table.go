package catalog

import (
	"dbf/internal/constants"
	"fmt"
	"strings"
	"sync"
)

const compositeIndexSeparator = "\x1f"

func normalizeIndexValue(value interface{}) string {
	if value == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", value)
}

func makeCompositeIndexKey(values []interface{}) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = normalizeIndexValue(v)
	}
	return strings.Join(parts, compositeIndexSeparator)
}

func (t *Table) Mu() *sync.RWMutex {
	return &t.mu
}

func (t *Table) InsertRow(values []interface{}, catalog *Catalog) error {
	return t.insertRowWithValidation(values, catalog, true)
}

func (t *Table) InsertRowUnsafe(values []interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(values) != len(t.Columns) {
		return fmt.Errorf("column count mismatch: expected %d, got %d", len(t.Columns), len(values))
	}

	t.Rows = append(t.Rows, values)
	t.indexRowLocked(len(t.Rows) - 1)
	return nil
}

func (t *Table) insertRowWithValidation(values []interface{}, catalog *Catalog, validateFK bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(values) != len(t.Columns) {
		return fmt.Errorf("column count mismatch: expected %d, got %d", len(t.Columns), len(values))
	}

	for _, constraint := range t.Constraints {
		if constraint.Type == constants.ConstraintPrimaryKey {
			colIdx := columnIndex(t.Columns, constraint.ColumnName)
			if colIdx == -1 {
				continue
			}
			for _, row := range t.Rows {
				if row[colIdx] == values[colIdx] {
					return fmt.Errorf("duplicate primary key value %v for column %s", values[colIdx], constraint.ColumnName)
				}
			}
		}
	}

	for _, constraint := range t.Constraints {
		if constraint.Type == constants.ConstraintUnique {
			colIdx := columnIndex(t.Columns, constraint.ColumnName)
			if colIdx == -1 {
				continue
			}
			for _, row := range t.Rows {
				if row[colIdx] == values[colIdx] {
					return fmt.Errorf("duplicate value %v for unique column %s", values[colIdx], constraint.ColumnName)
				}
			}
		}
	}

	for i, col := range t.Columns {
		if col.NotNull && (values[i] == nil || values[i] == "") {
			return fmt.Errorf("column %s cannot be NULL", col.Name)
		}
	}

	if validateFK {
		for _, constraint := range t.Constraints {
			if constraint.Type == constants.ConstraintForeignKey {
				colIdx := columnIndex(t.Columns, constraint.ColumnName)
				if colIdx != -1 && values[colIdx] != nil && values[colIdx] != "" {
					refTable, err := catalog.GetTable(constraint.ReferencedTable)
					if err != nil {
						return fmt.Errorf("referenced table %s not found", constraint.ReferencedTable)
					}

					refColIdx := columnIndex(refTable.Columns, constraint.ReferencedCol)
					if refColIdx == -1 {
						return fmt.Errorf("referenced column %s not found", constraint.ReferencedCol)
					}

					found := false
					for _, refRow := range refTable.Rows {
						if refRow[refColIdx] == values[colIdx] {
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("foreign key violation: value %v not found in %s(%s)", values[colIdx], constraint.ReferencedTable, constraint.ReferencedCol)
					}
				}
			}
		}
	}

	t.Rows = append(t.Rows, values)
	t.indexRowLocked(len(t.Rows) - 1)
	return nil
}

func (t *Table) indexRowLocked(rowIdx int) {
	if len(t.Indexes) == 0 || rowIdx < 0 || rowIdx >= len(t.Rows) {
		return
	}
	row := t.Rows[rowIdx]
	for _, idx := range t.Indexes {
		if len(idx.ColumnNames) == 0 {
			continue
		}

		vals := make([]interface{}, 0, len(idx.ColumnNames))
		valid := true
		for _, colName := range idx.ColumnNames {
			colIdx := columnIndex(t.Columns, colName)
			if colIdx == -1 || colIdx >= len(row) {
				valid = false
				break
			}
			vals = append(vals, row[colIdx])
		}
		if !valid {
			continue
		}

		key := makeCompositeIndexKey(vals)
		idx.Values[key] = append(idx.Values[key], rowIdx)
	}
}

func (t *Table) rebuildIndexesLocked() {
	if len(t.Indexes) == 0 {
		return
	}

	for _, idx := range t.Indexes {
		idx.Values = make(map[string][]int)
	}

	for rowIdx := range t.Rows {
		t.indexRowLocked(rowIdx)
	}
}

func (t *Table) RebuildIndexes() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rebuildIndexesLocked()
}

func (t *Table) CreateIndex(name string, columnNames []string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if name == "" {
		return fmt.Errorf("index name cannot be empty")
	}
	if len(columnNames) == 0 {
		return fmt.Errorf("index must include at least one column")
	}
	if _, exists := t.Indexes[name]; exists {
		return fmt.Errorf("index %s already exists", name)
	}

	seen := make(map[string]struct{}, len(columnNames))
	for _, columnName := range columnNames {
		if columnName == "" {
			return fmt.Errorf("index column cannot be empty")
		}
		if _, ok := seen[columnName]; ok {
			return fmt.Errorf("duplicate column %s in index definition", columnName)
		}
		seen[columnName] = struct{}{}
		if columnIndex(t.Columns, columnName) == -1 {
			return fmt.Errorf("column %s not found", columnName)
		}
	}

	if t.Indexes == nil {
		t.Indexes = make(map[string]*Index)
	}

	t.Indexes[name] = &Index{
		Name:        name,
		ColumnNames: append([]string(nil), columnNames...),
		Values:      make(map[string][]int),
	}
	t.rebuildIndexesLocked()
	return nil
}

func (t *Table) DropIndex(name string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if name == "" {
		return fmt.Errorf("index name cannot be empty")
	}
	if t.Indexes == nil {
		return fmt.Errorf("index %s does not exist", name)
	}
	if _, exists := t.Indexes[name]; !exists {
		return fmt.Errorf("index %s does not exist", name)
	}

	delete(t.Indexes, name)
	return nil
}

func (t *Table) SelectAll() [][]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return copyRows(t.Rows)
}

// UseRows calls fn with the table's raw row slice held under a read lock.
// The fn MUST NOT modify the slice or any individual row element.
// Use this for zero-copy read access (e.g., serialization).
func (t *Table) UseRows(fn func([][]interface{})) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	fn(t.Rows)
}

// SetRows replaces the table's row slice directly, bypassing row-by-row
// insertion. Used only during initial load from storage to avoid repeated
// append-and-grow allocations.
func (t *Table) SetRows(rows [][]interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Rows = rows
	t.rebuildIndexesLocked()
}

func (t *Table) SelectWhere(colName string, value interface{}) ([][]interface{}, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	colIdx := columnIndex(t.Columns, colName)
	if colIdx == -1 {
		return nil, fmt.Errorf("column %s not found", colName)
	}

	for _, idx := range t.Indexes {
		if len(idx.ColumnNames) == 0 || idx.ColumnNames[0] != colName {
			continue
		}

		var positions []int
		if len(idx.ColumnNames) == 1 {
			positions = idx.Values[normalizeIndexValue(value)]
		} else {
			prefix := normalizeIndexValue(value) + compositeIndexSeparator
			for key, rows := range idx.Values {
				if strings.HasPrefix(key, prefix) {
					positions = append(positions, rows...)
				}
			}
		}

		result := make([][]interface{}, 0, len(positions))
		for _, pos := range positions {
			if pos < 0 || pos >= len(t.Rows) {
				continue
			}
			rowCopy := make([]interface{}, len(t.Rows[pos]))
			copy(rowCopy, t.Rows[pos])
			result = append(result, rowCopy)
		}
		return result, nil
	}

	var result [][]interface{}
	for _, row := range t.Rows {
		if row[colIdx] == value {
			rowCopy := make([]interface{}, len(row))
			copy(rowCopy, row)
			result = append(result, rowCopy)
		}
	}
	return result, nil
}

// DeleteWhere deletes rows matching a column value
func (t *Table) DeleteWhere(colName string, value interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	colIdx := columnIndex(t.Columns, colName)
	if colIdx == -1 {
		return fmt.Errorf("column %s not found", colName)
	}

	var newRows [][]interface{}
	for _, row := range t.Rows {
		if row[colIdx] != value {
			newRows = append(newRows, row)
		}
	}
	t.Rows = newRows
	t.rebuildIndexesLocked()
	return nil
}
