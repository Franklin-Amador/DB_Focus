package catalog

import (
	"dbf/internal/constants"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const compositeIndexSeparator = "\x1f"

// compareIndexKeys orders two canonical index keys, returning -1, 0 or 1. It is
// numeric when both keys parse as numbers, lexicographic otherwise. This MUST
// match the executor's compareCells semantics so that an index-backed range
// lookup returns exactly the same rows a full scan would.
func compareIndexKeys(a, b string) int {
	af, aerr := strconv.ParseFloat(a, 64)
	bf, berr := strconv.ParseFloat(b, 64)
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
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// keyForRow builds this index's composite key for a row, reporting ok=false
// when a referenced column is missing from the row.
func (idx *Index) keyForRow(columns []Column, row []interface{}) (string, bool) {
	if len(idx.ColumnNames) == 0 {
		return "", false
	}
	vals := make([]interface{}, 0, len(idx.ColumnNames))
	for _, colName := range idx.ColumnNames {
		ci := columnIndex(columns, colName)
		if ci == -1 || ci >= len(row) {
			return "", false
		}
		vals = append(vals, row[ci])
	}
	return makeCompositeIndexKey(vals), true
}

// insertSortedKey inserts a new distinct key into sortedKeys, preserving order.
func (idx *Index) insertSortedKey(key string) {
	pos := sort.Search(len(idx.sortedKeys), func(i int) bool {
		return compareIndexKeys(idx.sortedKeys[i], key) >= 0
	})
	idx.sortedKeys = append(idx.sortedKeys, "")
	copy(idx.sortedKeys[pos+1:], idx.sortedKeys[pos:])
	idx.sortedKeys[pos] = key
}

// ValuesEqual reports whether two cell values are equal, tolerant of the mixed
// Go types the engine stores (literals as strings, IDENTITY keys as ints). It
// compares by canonical string form, matching JOIN ON equality semantics, so a
// foreign-key value "1" matches an IDENTITY-generated parent key 1.
func ValuesEqual(a, b interface{}) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

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
				if colIdx < len(row) && ValuesEqual(row[colIdx], values[colIdx]) {
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
				if colIdx < len(row) && ValuesEqual(row[colIdx], values[colIdx]) {
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

	// UNIQUE index enforcement (CREATE UNIQUE INDEX), in addition to UNIQUE
	// column constraints handled above.
	if err := t.checkUniqueIndexesLocked(values); err != nil {
		return err
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
						// Compare by string form so a literal FK value ("1") matches an
						// IDENTITY-generated parent key stored as an int (1). This mirrors
						// how JOIN ON equality compares values.
						if ValuesEqual(refRow[refColIdx], values[colIdx]) {
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
		key, ok := idx.keyForRow(t.Columns, row)
		if !ok {
			continue
		}
		existing := idx.Values[key]
		idx.Values[key] = append(existing, rowIdx)
		if len(existing) == 0 {
			// New distinct key: keep the ordered key list in sync.
			idx.insertSortedKey(key)
		}
	}
}

func (t *Table) rebuildIndexesLocked() {
	if len(t.Indexes) == 0 {
		return
	}

	for _, idx := range t.Indexes {
		idx.Values = make(map[string][]int)
		idx.sortedKeys = nil
	}

	for rowIdx, row := range t.Rows {
		for _, idx := range t.Indexes {
			key, ok := idx.keyForRow(t.Columns, row)
			if !ok {
				continue
			}
			idx.Values[key] = append(idx.Values[key], rowIdx)
		}
	}

	// Sort each index's distinct keys once (cheaper than incremental inserts).
	for _, idx := range t.Indexes {
		idx.sortedKeys = make([]string, 0, len(idx.Values))
		for k := range idx.Values {
			idx.sortedKeys = append(idx.sortedKeys, k)
		}
		sort.Slice(idx.sortedKeys, func(i, j int) bool {
			return compareIndexKeys(idx.sortedKeys[i], idx.sortedKeys[j]) < 0
		})
	}
}

func (t *Table) RebuildIndexes() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rebuildIndexesLocked()
}

func (t *Table) CreateIndex(name string, columnNames []string, unique bool) error {
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

	newIdx := &Index{
		Name:        name,
		ColumnNames: append([]string(nil), columnNames...),
		Unique:      unique,
		Values:      make(map[string][]int),
	}

	// A UNIQUE index cannot be built over data that already contains duplicate
	// keys. Check before registering it so a failed CREATE leaves no index.
	if unique {
		existing := make(map[string]struct{}, len(t.Rows))
		for _, row := range t.Rows {
			key, ok := newIdx.keyForRow(t.Columns, row)
			if !ok {
				continue
			}
			if _, dup := existing[key]; dup {
				return fmt.Errorf("could not create unique index %s: table contains duplicate values", name)
			}
			existing[key] = struct{}{}
		}
	}

	t.Indexes[name] = newIdx
	t.rebuildIndexesLocked()
	return nil
}

// checkUniqueIndexesLocked reports an error if inserting the given row would
// violate any UNIQUE index (a key already present). The table lock must be held.
func (t *Table) checkUniqueIndexesLocked(row []interface{}) error {
	for _, idx := range t.Indexes {
		if !idx.Unique {
			continue
		}
		key, ok := idx.keyForRow(t.Columns, row)
		if !ok {
			continue
		}
		if len(idx.Values[key]) > 0 {
			return fmt.Errorf("duplicate value violates unique index %s", idx.Name)
		}
	}
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
		if colIdx < len(row) && ValuesEqual(row[colIdx], value) {
			rowCopy := make([]interface{}, len(row))
			copy(rowCopy, row)
			result = append(result, rowCopy)
		}
	}
	return result, nil
}

// SelectRange returns the rows satisfying a range predicate on a single column,
// using a single-column index on that column via binary search over its sorted
// keys. op is one of "<", "<=", ">", ">=" (low is the bound) or "BETWEEN" (low
// and high are the inclusive bounds). The second result reports whether an index
// handled the query; when false, the caller must fall back to a scan. Results
// are identical to a scan because the index orders keys with compareIndexKeys,
// the same comparator the executor applies.
func (t *Table) SelectRange(colName, op string, low, high interface{}) ([][]interface{}, bool, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	colIdx := columnIndex(t.Columns, colName)
	if colIdx == -1 {
		return nil, false, fmt.Errorf("column %s not found", colName)
	}

	var idx *Index
	for _, candidate := range t.Indexes {
		if len(candidate.ColumnNames) == 1 && candidate.ColumnNames[0] == colName {
			idx = candidate
			break
		}
	}
	if idx == nil {
		return nil, false, nil // no suitable index; caller scans
	}

	keys := idx.sortedKeys
	lowKey := normalizeIndexValue(low)

	var start, end int
	switch op {
	case ">":
		start = sort.Search(len(keys), func(i int) bool { return compareIndexKeys(keys[i], lowKey) > 0 })
		end = len(keys)
	case ">=":
		start = sort.Search(len(keys), func(i int) bool { return compareIndexKeys(keys[i], lowKey) >= 0 })
		end = len(keys)
	case "<":
		start = 0
		end = sort.Search(len(keys), func(i int) bool { return compareIndexKeys(keys[i], lowKey) >= 0 })
	case "<=":
		start = 0
		end = sort.Search(len(keys), func(i int) bool { return compareIndexKeys(keys[i], lowKey) > 0 })
	case "BETWEEN":
		highKey := normalizeIndexValue(high)
		start = sort.Search(len(keys), func(i int) bool { return compareIndexKeys(keys[i], lowKey) >= 0 })
		end = sort.Search(len(keys), func(i int) bool { return compareIndexKeys(keys[i], highKey) > 0 })
	default:
		return nil, false, nil
	}
	if start > end {
		start = end // empty range (e.g. BETWEEN 10 AND 5)
	}

	result := make([][]interface{}, 0)
	for _, key := range keys[start:end] {
		for _, pos := range idx.Values[key] {
			if pos < 0 || pos >= len(t.Rows) {
				continue
			}
			rowCopy := make([]interface{}, len(t.Rows[pos]))
			copy(rowCopy, t.Rows[pos])
			result = append(result, rowCopy)
		}
	}
	return result, true, nil
}

