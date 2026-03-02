package catalog

import (
	"dbf/internal/constants"
	"fmt"
	"sync"
)

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
	return nil
}

func (t *Table) SelectAll() [][]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return copyRows(t.Rows)
}

func (t *Table) SelectWhere(colName string, value interface{}) ([][]interface{}, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	colIdx := columnIndex(t.Columns, colName)
	if colIdx == -1 {
		return nil, fmt.Errorf("column %s not found", colName)
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
