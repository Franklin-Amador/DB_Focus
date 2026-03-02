package storage

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cockroachdb/pebble"

	"dbf/internal/ast"
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

func (ps *PebbleStorage) SaveProcedure(proc *catalog.Procedure) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	pd := ProcedureData{
		Name:       proc.Name,
		Parameters: proc.Parameters,
		Body:       proc.Body,
	}
	if err := enc.Encode(pd); err != nil {
		return fmt.Errorf("failed to encode procedure %s: %w", proc.Name, err)
	}

	key := []byte("proc:" + proc.Name)
	if err := ps.db.Set(key, buf.Bytes(), ps.wal); err != nil {
		return fmt.Errorf("failed to save procedure %s: %w", proc.Name, err)
	}
	return nil
}

func (ps *PebbleStorage) DeleteProcedure(name string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	key := []byte("proc:" + name)
	if err := ps.db.Delete(key, ps.wal); err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("failed to delete procedure %s: %w", name, err)
	}
	return nil
}

func (ps *PebbleStorage) SaveTrigger(trigger *catalog.Trigger) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	td := TriggerData{
		Name:       trigger.Name,
		Timing:     trigger.Timing,
		Event:      trigger.Event,
		Table:      trigger.Table,
		ForEachRow: trigger.ForEachRow,
		Body:       trigger.Body,
	}
	if err := enc.Encode(td); err != nil {
		return fmt.Errorf("failed to encode trigger %s: %w", trigger.Name, err)
	}

	key := []byte("trig:" + trigger.Name)
	if err := ps.db.Set(key, buf.Bytes(), ps.wal); err != nil {
		return fmt.Errorf("failed to save trigger %s: %w", trigger.Name, err)
	}
	return nil
}

func (ps *PebbleStorage) DeleteTrigger(name string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	key := []byte("trig:" + name)
	if err := ps.db.Delete(key, ps.wal); err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("failed to delete trigger %s: %w", name, err)
	}
	return nil
}

func (ps *PebbleStorage) SaveJob(job *catalog.Job) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	jd := JobData{
		Name:     job.Name,
		Interval: job.Interval,
		Unit:     job.Unit,
		Body:     job.Body,
		Enabled:  job.Enabled,
	}
	if err := enc.Encode(jd); err != nil {
		return fmt.Errorf("failed to encode job %s: %w", job.Name, err)
	}

	key := []byte("job:" + job.Name)
	if err := ps.db.Set(key, buf.Bytes(), ps.wal); err != nil {
		return fmt.Errorf("failed to save job %s: %w", job.Name, err)
	}
	return nil
}

func (ps *PebbleStorage) DeleteJob(name string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	key := []byte("job:" + name)
	if err := ps.db.Delete(key, ps.wal); err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("failed to delete job %s: %w", name, err)
	}
	return nil
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

type ProcedureData struct {
	Name       string
	Parameters []ast.Parameter
	Body       []ast.Statement
}

type TriggerData struct {
	Name       string
	Timing     string
	Event      string
	Table      string
	ForEachRow bool
	Body       []ast.Statement
}

type JobData struct {
	Name     string
	Interval int
	Unit     string
	Body     []ast.Statement
	Enabled  bool
}

func registerGobTypes() {
	gob.Register(&ast.Insert{})
	gob.Register(&ast.Update{})
	gob.Register(&ast.Delete{})
	gob.Register(&ast.Select{})
	gob.Register(&ast.SelectFunction{})
	gob.Register(&ast.Set{})
	gob.Register(&ast.CallProcedure{})
	gob.Register(&ast.CreateTable{})
	gob.Register(&ast.CreateSchema{})
	gob.Register(&ast.CreateDatabase{})
	gob.Register(&ast.DropTable{})
	gob.Register(&ast.DropSchema{})
	gob.Register(&ast.DropProcedure{})
	gob.Register(&ast.CreateProcedure{})
	gob.Register(&ast.CreateTrigger{})
	gob.Register(&ast.DropTrigger{})
	gob.Register(&ast.CreateJob{})
	gob.Register(&ast.DropJob{})
	gob.Register(&ast.AlterJob{})
	gob.Register(&ast.PrimaryKeyConstraint{})
	gob.Register(&ast.ForeignKeyConstraint{})
	gob.Register(&ast.UniqueConstraint{})
	gob.Register(&ast.NotNullConstraint{})
}

// NewPebbleStorage creates a new Pebble-backed storage engine
func NewPebbleStorage(dir string) (*PebbleStorage, error) {
	registerGobTypes()
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

// SaveTableWithSchema persists a table under the provided schema name.
func (ps *PebbleStorage) SaveTableWithSchema(table *catalog.Table, schema string) error {
	if schema == "" {
		schema = "public"
	}
	return ps.saveTableWithSchema(table, schema)
}

// DeleteTable removes a table from persistent storage and metadata.
func (ps *PebbleStorage) DeleteTable(name string, schema string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if schema == "" {
		schema = "public"
	}

	key := []byte("table:" + schema + ":" + name)
	if err := ps.db.Delete(key, ps.wal); err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("failed to delete table %s.%s: %w", schema, name, err)
	}

	if _, ok := ps.meta.Tables[schema]; ok {
		delete(ps.meta.Tables[schema], name)
	}
	return ps.saveMetadata()
}

// CreateSchema persists an empty schema namespace into metadata.
func (ps *PebbleStorage) CreateSchema(name string) error {
	if name == "" {
		return fmt.Errorf("schema name cannot be empty")
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if _, ok := ps.meta.Tables[name]; ok {
		return fmt.Errorf("schema %s already exists", name)
	}
	ps.meta.Tables[name] = make(map[string]*TableSchema)
	return ps.saveMetadata()
}

// DeleteSchema removes a schema and all its tables from persistent storage.
func (ps *PebbleStorage) DeleteSchema(name string) error {
	if name == "" {
		return fmt.Errorf("schema name cannot be empty")
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if _, ok := ps.meta.Tables[name]; !ok {
		return fmt.Errorf("schema %s does not exist", name)
	}

	// Delete all tables in this schema from Pebble
	tablesInSchema := ps.meta.Tables[name]
	for tableName := range tablesInSchema {
		key := []byte("table:" + name + ":" + tableName)
		if err := ps.db.Delete(key, ps.wal); err != nil && err != pebble.ErrNotFound {
			return fmt.Errorf("failed to delete table %s.%s from pebble: %w", name, tableName, err)
		}
	}

	// Remove schema from metadata
	delete(ps.meta.Tables, name)
	return ps.saveMetadata()
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

	// Recreate persisted schemas first, including empty schemas (without tables)
	for schema := range ps.meta.Tables {
		if schema == "" || schema == "public" {
			continue
		}
		if err := cat.CreateSchema(schema); err != nil {
			// Ignore "already exists" and continue
			if !strings.Contains(err.Error(), "already exists") {
				log.Printf("[storage] warning: failed to recreate schema %s: %v", schema, err)
			}
		}
	}

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

			// Load ALL tables, including system catalog tables like pg_catalog.pg_database
			// This ensures that user-created databases persist across restarts
			if err := ps.loadTableInternal(cat, tableName, schema); err != nil {
				log.Printf("[storage] warning: failed to load table %s.%s: %v", schema, tableName, err)
			}
		} else if strings.HasPrefix(key, "proc:") {
			val := append([]byte(nil), iter.Value()...)
			var pd ProcedureData
			dec := gob.NewDecoder(bytes.NewReader(val))
			if err := dec.Decode(&pd); err != nil {
				log.Printf("[storage] warning: failed to decode procedure %s: %v", strings.TrimPrefix(key, "proc:"), err)
				continue
			}
			if err := cat.LoadProcedure(pd.Name, pd.Parameters, pd.Body); err != nil {
				log.Printf("[storage] warning: failed to load procedure %s: %v", pd.Name, err)
			}
		} else if strings.HasPrefix(key, "trig:") {
			val := append([]byte(nil), iter.Value()...)
			var td TriggerData
			dec := gob.NewDecoder(bytes.NewReader(val))
			if err := dec.Decode(&td); err != nil {
				log.Printf("[storage] warning: failed to decode trigger %s: %v", strings.TrimPrefix(key, "trig:"), err)
				continue
			}
			if err := cat.LoadTrigger(td.Name, td.Timing, td.Event, td.Table, td.ForEachRow, td.Body); err != nil {
				log.Printf("[storage] warning: failed to load trigger %s: %v", td.Name, err)
			}
		} else if strings.HasPrefix(key, "job:") {
			val := append([]byte(nil), iter.Value()...)
			var jd JobData
			dec := gob.NewDecoder(bytes.NewReader(val))
			if err := dec.Decode(&jd); err != nil {
				log.Printf("[storage] warning: failed to decode job %s: %v", strings.TrimPrefix(key, "job:"), err)
				continue
			}
			if err := cat.LoadJob(jd.Name, jd.Interval, jd.Unit, jd.Body, jd.Enabled); err != nil {
				log.Printf("[storage] warning: failed to load job %s: %v", jd.Name, err)
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

// Meta returns a copy of the current metadata (for inspection).
func (ps *PebbleStorage) Meta() *TableMetadata {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.meta
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
