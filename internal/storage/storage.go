package storage

import (
	"encoding/json"
	"strconv"

	"dbf/internal/catalog"
)

// The storage contract is split into small, responsibility-scoped interfaces
// so consumers can depend on only what they use and new persistent object
// types get their own interface instead of enlarging one flat contract.
// Backend composes them all; the Pebble backend satisfies the whole set.

// TableStore persists tables and their row/column data.
type TableStore interface {
	SaveTable(table *catalog.Table) error
	// SaveTableWithSchema persists a table under a specific schema name.
	SaveTableWithSchema(table *catalog.Table, schema string) error
	DeleteTable(name string, schema string) error
	LoadTable(cat *catalog.Catalog, name string) error
	// DropColumnData removes a column from all rows in a table.
	DropColumnData(tableName string, columnName string, schema string) error
	// RenameColumnData renames a column in all rows in a table.
	RenameColumnData(tableName string, oldName string, newName string, schema string) error
}

// ViewStore persists views.
type ViewStore interface {
	SaveView(view *catalog.View, schema string) error
	DeleteView(name string, schema string) error
}

// ProcedureStore persists stored procedures.
type ProcedureStore interface {
	SaveProcedure(proc *catalog.Procedure) error
	DeleteProcedure(name string) error
}

// TriggerStore persists triggers.
type TriggerStore interface {
	SaveTrigger(trigger *catalog.Trigger) error
	DeleteTrigger(name string) error
}

// JobStore persists scheduled jobs.
type JobStore interface {
	SaveJob(job *catalog.Job) error
	DeleteJob(name string) error
}

// SchemaStore persists schema namespaces.
type SchemaStore interface {
	// CreateSchema creates a new schema namespace in persistent storage.
	CreateSchema(name string) error
	// DeleteSchema removes a schema and all its tables from persistent storage.
	DeleteSchema(name string) error
}

// Backend is the full storage contract, composing every per-responsibility
// store plus lifecycle operations. Implementations (e.g. PebbleStorage) satisfy
// the whole interface; callers may depend on a narrower store where possible.
type Backend interface {
	TableStore
	ViewStore
	ProcedureStore
	TriggerStore
	JobStore
	SchemaStore
	LoadAll(cat *catalog.Catalog) error
	Close() error
}

type TableData struct {
	Name        string           `json:"name"`
	Columns     []ColumnData     `json:"columns"`
	Constraints []ConstraintData `json:"constraints"`
	Indexes     []IndexData      `json:"indexes,omitempty"`
	Rows        [][]interface{}  `json:"rows"`
}

type IndexData struct {
	Name        string   `json:"name"`
	ColumnNames []string `json:"column_names,omitempty"`
	// ColumnName is kept for backward compatibility with previous persisted format.
	ColumnName string `json:"column_name,omitempty"`
}

// indexColumnsFromData returns the indexed column names for a persisted index,
// tolerating both the current multi-column format and the legacy single-column
// field. Shared by the Pebble backend when rehydrating indexes.
func indexColumnsFromData(idx IndexData) []string {
	if len(idx.ColumnNames) > 0 {
		return append([]string(nil), idx.ColumnNames...)
	}
	if idx.ColumnName != "" {
		return []string{idx.ColumnName}
	}
	return nil
}

type ColumnData struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	NotNull       bool   `json:"not_null"`
	Identity      bool   `json:"identity"`
	IdentityValue int    `json:"identity_value"`
}

type ConstraintData struct {
	Type            string `json:"type"`
	ColumnName      string `json:"column_name"`
	ReferencedTable string `json:"referenced_table,omitempty"`
	ReferencedCol   string `json:"referenced_col,omitempty"`
}

// syncIdentityValues normalizes a table's IDENTITY columns after a bulk load:
// it computes the maximum existing identity value, backfills any missing ones,
// and advances the column's identity counter accordingly. Shared by the Pebble
// backend after rehydrating table rows.
func syncIdentityValues(table *catalog.Table) {
	table.Mu().Lock()
	defer table.Mu().Unlock()

	for colIdx, col := range table.Columns {
		if !col.Identity {
			continue
		}
		maxValue := col.IdentityValue
		for _, row := range table.Rows {
			if colIdx >= len(row) {
				continue
			}
			value := row[colIdx]
			if value == nil {
				continue
			}

			switch v := value.(type) {
			case int:
				if v > maxValue {
					maxValue = v
				}
			case int64:
				if int(v) > maxValue {
					maxValue = int(v)
				}
			case float64:
				if int(v) > maxValue {
					maxValue = int(v)
				}
			case json.Number:
				if parsed, err := v.Int64(); err == nil && int(parsed) > maxValue {
					maxValue = int(parsed)
				}
			case string:
				if parsed, err := strconv.Atoi(v); err == nil && parsed > maxValue {
					maxValue = parsed
				}
			}
		}

		// Backfill missing identity values
		for i, row := range table.Rows {
			if colIdx >= len(row) {
				continue
			}
			if row[colIdx] == nil {
				maxValue++
				table.Rows[i][colIdx] = maxValue
			}
		}

		if maxValue > table.Columns[colIdx].IdentityValue {
			table.Columns[colIdx].IdentityValue = maxValue
		}
	}
}
