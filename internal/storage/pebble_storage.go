package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cockroachdb/pebble"

	"dbf/internal/catalog"
)

// PebbleStorage wraps Pebble DB for persistent table storage with WAL
type PebbleStorage struct {
	db   *pebble.DB
	dir  string
	mu   sync.RWMutex
	wal  *pebble.WriteOptions
	meta *TableMetadata
}

// TableMetadata stores schema information
type TableMetadata struct {
	Tables map[string]map[string]*TableSchema `json:"tables"` // schema -> table -> TableSchema
}

type TableSchema struct {
	Name        string           `json:"name"`
	Columns     []ColumnData     `json:"columns"`
	Constraints []ConstraintData `json:"constraints"`
}

// NewPebbleStorage creates a new Pebble-backed storage engine
func NewPebbleStorage(dir string) (*PebbleStorage, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dir, "pebble.db")
	db, err := pebble.Open(dbPath, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble database: %w", err)
	}

	ps := &PebbleStorage{
		db:   db,
		dir:  dir,
		wal:  &pebble.WriteOptions{Sync: true}, // WAL sync enabled
		meta: &TableMetadata{Tables: make(map[string]map[string]*TableSchema)},
	}

	// Load existing metadata
	if err := ps.loadMetadata(); err != nil {
		log.Printf("[storage] warning: could not load metadata: %v", err)
	}

	return ps, nil
}

// SaveTable persists a table to Pebble
func (ps *PebbleStorage) SaveTable(table *catalog.Table) error {
	return ps.saveTableWithSchema(table, "public")
}

// saveTableWithSchema persists a table with explicit schema
func (ps *PebbleStorage) saveTableWithSchema(table *catalog.Table, schema string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Serialize table data with key: "table:<schema>:<name>"
	key := []byte("table:" + schema + ":" + table.Name)
	data, err := json.Marshal(TableData{
		Name:        table.Name,
		Columns:     convertColumns(table.Columns),
		Constraints: convertConstraints(table.Constraints),
		Rows:        table.SelectAll(),
	})
	if err != nil {
		return fmt.Errorf("failed to marshal table %s: %w", table.Name, err)
	}

	// Write with WAL sync
	if err := ps.db.Set(key, data, ps.wal); err != nil {
		return fmt.Errorf("failed to save table %s: %w", table.Name, err)
	}

	// Update metadata
	if _, ok := ps.meta.Tables[schema]; !ok {
		ps.meta.Tables[schema] = make(map[string]*TableSchema)
	}
	ps.meta.Tables[schema][table.Name] = &TableSchema{
		Name:        table.Name,
		Columns:     convertColumns(table.Columns),
		Constraints: convertConstraints(table.Constraints),
	}

	// Persist metadata
	return ps.saveMetadata()
}

// LoadTable retrieves a table from Pebble (implements Backend interface)
func (ps *PebbleStorage) LoadTable(cat *catalog.Catalog, name string) error {
	return ps.loadTableInternal(cat, name, "public")
}

// loadTableInternal retrieves a table from Pebble with schema support
func (ps *PebbleStorage) loadTableInternal(cat *catalog.Catalog, name string, schema string) error {
	if strings.HasPrefix(name, "pg_catalog.") {
		return nil
	}

	ps.mu.RLock()
	defer ps.mu.RUnlock()

	key := []byte("table:" + schema + ":" + name)
	val, closer, err := ps.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil // Table not found, which is ok
		}
		return fmt.Errorf("failed to load table %s: %w", name, err)
	}
	defer closer.Close()

	var td TableData
	if err := json.Unmarshal(val, &td); err != nil {
		return fmt.Errorf("failed to unmarshal table %s: %w", name, err)
	}
	if strings.HasPrefix(td.Name, "pg_catalog.") {
		return nil
	}

	// Recreate table in catalog
	cols := make([]catalog.Column, len(td.Columns))
	for i, c := range td.Columns {
		cols[i] = catalog.Column{
			Name:          c.Name,
			Type:          c.Type,
			NotNull:       c.NotNull,
			Identity:      c.Identity,
			IdentityValue: c.IdentityValue,
		}
	}

	constraints := make([]catalog.Constraint, len(td.Constraints))
	for i, c := range td.Constraints {
		constraints[i] = catalog.Constraint{
			Type:            c.Type,
			ColumnName:      c.ColumnName,
			ReferencedTable: c.ReferencedTable,
			ReferencedCol:   c.ReferencedCol,
		}
	}

	// Create table in catalog
	if err := cat.CreateTable(td.Name, cols, constraints, schema); err != nil {
		return fmt.Errorf("failed to create table in catalog: %w", err)
	}

	// Load rows
	if table, err := cat.GetTable(td.Name, schema); err == nil {
		for _, row := range td.Rows {
			_ = table.InsertRowUnsafe(row)
		}
		syncIdentityValues(table)
	}

	return nil
}

// LoadAll loads all tables from Pebble
func (ps *PebbleStorage) LoadAll(cat *catalog.Catalog) error {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	iter, err := ps.db.NewIter(&pebble.IterOptions{})
	if err != nil {
		return fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Iterate through all keys
	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		if strings.HasPrefix(key, "table:") {
			// key format: table:schema:table
			parts := strings.SplitN(key, ":", 3)
			if len(parts) != 3 {
				continue
			}
			schema := parts[1]
			tableName := parts[2]
			if strings.HasPrefix(tableName, "pg_catalog.") {
				continue
			}
			if err := ps.loadTableInternal(cat, tableName, schema); err != nil {
				log.Printf("[storage] warning: failed to load table %s.%s: %v", schema, tableName, err)
			}
		}
	}

	return iter.Error()
}

// Close closes the Pebble database
func (ps *PebbleStorage) Close() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.db != nil {
		return ps.db.Close()
	}
	return nil
}

// Helper functions
func (ps *PebbleStorage) saveMetadata() error {
	data, err := json.Marshal(ps.meta)
	if err != nil {
		return err
	}
	return ps.db.Set([]byte("meta:schema"), data, ps.wal)
}

func (ps *PebbleStorage) loadMetadata() error {
	val, closer, err := ps.db.Get([]byte("meta:schema"))
	if err != nil {
		if err == pebble.ErrNotFound {
			ps.meta.Tables = make(map[string]map[string]*TableSchema)
			return nil
		}
		return err
	}
	defer closer.Close()

	return json.Unmarshal(val, ps.meta)
}

func convertColumns(cols []catalog.Column) []ColumnData {
	result := make([]ColumnData, len(cols))
	for i, c := range cols {
		result[i] = ColumnData{
			Name:          c.Name,
			Type:          c.Type,
			NotNull:       c.NotNull,
			Identity:      c.Identity,
			IdentityValue: c.IdentityValue,
		}
	}
	return result
}

func convertConstraints(constraints []catalog.Constraint) []ConstraintData {
	result := make([]ConstraintData, len(constraints))
	for i, c := range constraints {
		result[i] = ConstraintData{
			Type:            c.Type,
			ColumnName:      c.ColumnName,
			ReferencedTable: c.ReferencedTable,
			ReferencedCol:   c.ReferencedCol,
		}
	}
	return result
}
