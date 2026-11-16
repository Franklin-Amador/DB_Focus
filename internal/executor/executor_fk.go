package executor

import (
	"fmt"
	"strings"

	"dbf/internal/ast"
	"dbf/internal/catalog"
)

// maxCascadeDepth bounds referential-action recursion so a cyclic set of
// foreign keys (e.g. mutual CASCADE) cannot loop forever.
const maxCascadeDepth = 64

// normalizeAction canonicalizes a referential action. An empty action means the
// SQL default, RESTRICT. RESTRICT and NO ACTION both block (we have no
// transactions, so there is nothing to defer).
func normalizeAction(action string) string {
	a := strings.ToUpper(strings.TrimSpace(action))
	if a == "" {
		return "RESTRICT"
	}
	return a
}

// enforceDeleteReferences applies ON DELETE referential actions for every parent
// row that `where` selects. It MUST be called without holding any table lock:
// it snapshots the matched parent rows under a read lock, then mutates and
// persists child tables (possibly the same table, for self-references). This
// also fixes a latent deadlock in the old design, where the FK check ran while
// the parent's write lock was held and self-referencing tables re-locked it.
func (e *Executor) enforceDeleteReferences(schema, tableName string, table *catalog.Table, where *ast.WhereClause) error {
	refs := e.catalog.ForeignKeyReferencers(schemaOrPublic(schema), tableName)
	if len(refs) == 0 {
		return nil
	}
	deleted, err := e.snapshotMatchedRows(table, where)
	if err != nil {
		return err
	}
	visited := map[string]bool{}
	for _, row := range deleted {
		if err := e.cascadeDeleteRow(schema, tableName, table, row, refs, visited, 0); err != nil {
			return err
		}
	}
	return nil
}

// cascadeDeleteRow enforces the referential action of each child FK against one
// parent row being removed.
func (e *Executor) cascadeDeleteRow(schema, tableName string, table *catalog.Table, row []interface{}, refs []catalog.FKReferencer, visited map[string]bool, depth int) error {
	if depth > maxCascadeDepth {
		return fmt.Errorf("foreign key cascade too deep (possible cycle) deleting from %s", qualifiedName(schemaOrPublic(schema), tableName))
	}

	for _, ref := range refs {
		refColIdx := findColumnIndex(table.Columns, ref.ReferencedCol)
		if refColIdx == -1 || refColIdx >= len(row) {
			continue
		}
		value := row[refColIdx]

		switch normalizeAction(ref.OnDelete) {
		case "CASCADE":
			childRows := snapshotMatchingRows(ref.Table, ref.FKColIndex, value)
			if len(childRows) == 0 {
				continue
			}
			childRefs := e.catalog.ForeignKeyReferencers(ref.Schema, ref.TableName)
			for _, childRow := range childRows {
				key := ref.Schema + "." + ref.TableName + "#" + rowKey(childRow)
				if visited[key] {
					continue
				}
				visited[key] = true
				if err := e.cascadeDeleteRow(ref.Schema, ref.TableName, ref.Table, childRow, childRefs, visited, depth+1); err != nil {
					return err
				}
			}
			deleteRowsWhere(ref.Table, ref.FKColIndex, value)
			e.persistTableWarn(ref.Table, ref.Schema)

		case "SET NULL":
			if setNullRowsWhere(ref.Table, ref.FKColIndex, value) {
				e.persistTableWarn(ref.Table, ref.Schema)
			}

		default: // RESTRICT / NO ACTION
			if hasMatchingRow(ref.Table, ref.FKColIndex, value) {
				return fmt.Errorf("cannot delete record: foreign key reference exists in table %s column %s",
					qualifiedName(ref.Schema, ref.TableName), ref.FKColumn)
			}
		}
	}
	return nil
}

// enforceUpdateReferences applies ON UPDATE referential actions when the updated
// column is referenced by a child foreign key and its value changes. This closes
// the gap where UPDATE of a referenced key silently orphaned child rows: with
// the default (RESTRICT) it now blocks, exactly like DELETE. It must be called
// before the parent rows are modified and without holding any table lock.
func (e *Executor) enforceUpdateReferences(schema, tableName string, table *catalog.Table, where *ast.WhereClause, updatedCol string, newValue interface{}) error {
	allRefs := e.catalog.ForeignKeyReferencers(schemaOrPublic(schema), tableName)
	var refs []catalog.FKReferencer
	for _, r := range allRefs {
		if r.ReferencedCol == updatedCol {
			refs = append(refs, r)
		}
	}
	if len(refs) == 0 {
		return nil
	}

	updatedIdx := findColumnIndex(table.Columns, updatedCol)
	if updatedIdx == -1 {
		return nil
	}
	oldValues, err := e.distinctMatchedValues(table, where, updatedIdx)
	if err != nil {
		return err
	}

	parentLabel := qualifiedName(schemaOrPublic(schema), tableName)
	visited := map[string]bool{}
	for _, oldVal := range oldValues {
		if catalog.ValuesEqual(oldVal, newValue) {
			continue
		}
		if err := e.cascadeUpdateValue(parentLabel, refs, oldVal, newValue, visited, 0); err != nil {
			return err
		}
	}
	return nil
}

// cascadeUpdateValue propagates a changed key value (oldValue -> newValue) to the
// child foreign keys in refs.
func (e *Executor) cascadeUpdateValue(parentLabel string, refs []catalog.FKReferencer, oldValue, newValue interface{}, visited map[string]bool, depth int) error {
	if depth > maxCascadeDepth {
		return fmt.Errorf("foreign key cascade too deep (possible cycle) updating %s", parentLabel)
	}

	for _, ref := range refs {
		if !hasMatchingRow(ref.Table, ref.FKColIndex, oldValue) {
			continue
		}
		switch normalizeAction(ref.OnUpdate) {
		case "CASCADE":
			setColumnRowsWhere(ref.Table, ref.FKColIndex, oldValue, newValue)
			e.persistTableWarn(ref.Table, ref.Schema)
			// Propagate further if this child's FK column is itself referenced.
			grand := e.catalog.ForeignKeyReferencers(ref.Schema, ref.TableName)
			var relevant []catalog.FKReferencer
			for _, g := range grand {
				if g.ReferencedCol == ref.FKColumn {
					relevant = append(relevant, g)
				}
			}
			if len(relevant) > 0 {
				key := ref.Schema + "." + ref.TableName + "|" + ref.FKColumn + "|" + fmt.Sprintf("%v", oldValue)
				if !visited[key] {
					visited[key] = true
					childLabel := qualifiedName(ref.Schema, ref.TableName)
					if err := e.cascadeUpdateValue(childLabel, relevant, oldValue, newValue, visited, depth+1); err != nil {
						return err
					}
				}
			}

		case "SET NULL":
			if setNullRowsWhere(ref.Table, ref.FKColIndex, oldValue) {
				e.persistTableWarn(ref.Table, ref.Schema)
			}

		default: // RESTRICT / NO ACTION
			return fmt.Errorf("cannot update %s: foreign key reference exists in table %s column %s",
				parentLabel, qualifiedName(ref.Schema, ref.TableName), ref.FKColumn)
		}
	}
	return nil
}

// --- Row helpers (each manages its own locking; never call while holding a
// lock on the same table). ---

// snapshotMatchedRows returns copies of the rows matched by `where`.
func (e *Executor) snapshotMatchedRows(table *catalog.Table, where *ast.WhereClause) ([][]interface{}, error) {
	resolve := columnResolver(table)
	table.Mu().RLock()
	defer table.Mu().RUnlock()

	var out [][]interface{}
	for _, row := range table.Rows {
		ok, err := evalWhere(where, row, resolve)
		if err != nil {
			return nil, err
		}
		if ok {
			rowCopy := make([]interface{}, len(row))
			copy(rowCopy, row)
			out = append(out, rowCopy)
		}
	}
	return out, nil
}

// distinctMatchedValues returns the distinct values of column colIdx among the
// rows matched by `where`.
func (e *Executor) distinctMatchedValues(table *catalog.Table, where *ast.WhereClause, colIdx int) ([]interface{}, error) {
	resolve := columnResolver(table)
	table.Mu().RLock()
	defer table.Mu().RUnlock()

	seen := map[string]bool{}
	var out []interface{}
	for _, row := range table.Rows {
		ok, err := evalWhere(where, row, resolve)
		if err != nil {
			return nil, err
		}
		if !ok || colIdx >= len(row) {
			continue
		}
		k := fmt.Sprintf("%v", row[colIdx])
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, row[colIdx])
	}
	return out, nil
}

// snapshotMatchingRows returns copies of rows whose column colIdx equals value.
func snapshotMatchingRows(table *catalog.Table, colIdx int, value interface{}) [][]interface{} {
	table.Mu().RLock()
	defer table.Mu().RUnlock()

	var out [][]interface{}
	for _, row := range table.Rows {
		if colIdx < len(row) && catalog.ValuesEqual(row[colIdx], value) {
			rowCopy := make([]interface{}, len(row))
			copy(rowCopy, row)
			out = append(out, rowCopy)
		}
	}
	return out
}

// hasMatchingRow reports whether any row has column colIdx equal to value.
func hasMatchingRow(table *catalog.Table, colIdx int, value interface{}) bool {
	table.Mu().RLock()
	defer table.Mu().RUnlock()

	for _, row := range table.Rows {
		if colIdx < len(row) && catalog.ValuesEqual(row[colIdx], value) {
			return true
		}
	}
	return false
}

// deleteRowsWhere removes every row whose column colIdx equals value.
func deleteRowsWhere(table *catalog.Table, colIdx int, value interface{}) {
	table.Mu().Lock()
	kept := make([][]interface{}, 0, len(table.Rows))
	for _, row := range table.Rows {
		if colIdx < len(row) && catalog.ValuesEqual(row[colIdx], value) {
			continue
		}
		kept = append(kept, row)
	}
	table.Rows = kept
	table.Mu().Unlock()
	table.RebuildIndexes()
}

// setNullRowsWhere sets column colIdx to NULL for every row where it equals
// value. It reports whether any row changed.
func setNullRowsWhere(table *catalog.Table, colIdx int, value interface{}) bool {
	table.Mu().Lock()
	changed := false
	for i, row := range table.Rows {
		if colIdx < len(row) && catalog.ValuesEqual(row[colIdx], value) {
			table.Rows[i][colIdx] = nil
			changed = true
		}
	}
	table.Mu().Unlock()
	if changed {
		table.RebuildIndexes()
	}
	return changed
}

// setColumnRowsWhere replaces oldValue with newValue in column colIdx.
func setColumnRowsWhere(table *catalog.Table, colIdx int, oldValue, newValue interface{}) {
	table.Mu().Lock()
	changed := false
	for i, row := range table.Rows {
		if colIdx < len(row) && catalog.ValuesEqual(row[colIdx], oldValue) {
			table.Rows[i][colIdx] = newValue
			changed = true
		}
	}
	table.Mu().Unlock()
	if changed {
		table.RebuildIndexes()
	}
}

// rowKey builds a canonical string key for a row, used to dedupe cascade work.
func rowKey(row []interface{}) string {
	parts := make([]string, len(row))
	for i, v := range row {
		parts[i] = fmt.Sprintf("%v", v)
	}
	return strings.Join(parts, "\x1f")
}
