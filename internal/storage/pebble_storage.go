package storage

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/parser"
)

// parseViewQuery re-parses a stored view SELECT text into an AST. Used on load
// so that persisted views are defined by stable SQL rather than a serialized
// AST whose shape may change over time.
func parseViewQuery(text string) (*ast.Select, error) {
	stmt, err := parser.NewParser(text).ParseStatement()
	if err != nil {
		return nil, err
	}
	sel, ok := stmt.(*ast.Select)
	if !ok {
		return nil, fmt.Errorf("view definition is not a SELECT")
	}
	return sel, nil
}

// parseBody re-parses a stored routine body (the statements between BEGIN and
// END) into an AST. Used on load so procedures/triggers/jobs are defined by
// stable SQL text rather than a serialized AST whose shape may change.
func parseBody(text string) ([]ast.Statement, error) {
	p := parser.NewParser(text)
	var body []ast.Statement
	for !p.AtEOF() {
		st, err := p.ParseStatement()
		if err != nil {
			return nil, err
		}
		if st != nil {
			body = append(body, st)
		}
	}
	return body, nil
}

// pebbleCore is the shared Pebble instance behind every database-bound
// PebbleStorage value: one file set, one lock, one metadata document.
type pebbleCore struct {
	db    *pebble.DB
	dir   string
	mu    sync.RWMutex
	wal   *pebble.WriteOptions
	meta  *TableMetadata
	cache *pebble.Cache
}

// PebbleStorage wraps Pebble DB for persistent table storage with WAL. A value
// is bound to one database: every key it reads or writes is prefixed with
// "db:<name>:" and its metadata lives under Databases[<name>].
type PebbleStorage struct {
	core   *pebbleCore
	dbName string
}

// key namespaces a storage key under the bound database.
func (ps *PebbleStorage) key(k string) []byte {
	return []byte(ps.prefix() + k)
}

func (ps *PebbleStorage) prefix() string {
	return "db:" + ps.dbName + ":"
}

// tables returns the bound database's table metadata for reading, or nil when
// the database is not registered in storage. The caller must hold core.mu.
func (ps *PebbleStorage) tables() map[string]map[string]*TableSchema {
	dm := ps.core.meta.Databases[ps.dbName]
	if dm == nil {
		return nil
	}
	return dm.Tables
}

// tablesForWrite returns the bound database's table metadata for mutation.
// The database must have been registered (CreateDatabase / default at open):
// a write against a dropped database is an error, never a silent re-creation.
// The caller must hold core.mu.
func (ps *PebbleStorage) tablesForWrite() (map[string]map[string]*TableSchema, error) {
	dm := ps.core.meta.Databases[ps.dbName]
	if dm == nil {
		return nil, fmt.Errorf("database %s does not exist in storage", ps.dbName)
	}
	if dm.Tables == nil {
		dm.Tables = make(map[string]map[string]*TableSchema)
	}
	return dm.Tables, nil
}

// ensureDefaultDatabase registers the default database's metadata entry.
func (ps *PebbleStorage) ensureDefaultDatabase() {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()
	if ps.core.meta.Databases[catalog.DefaultDatabase] == nil {
		ps.core.meta.Databases[catalog.DefaultDatabase] = &DatabaseMeta{Tables: make(map[string]map[string]*TableSchema)}
	}
}

// DatabaseName returns the database this storage value is bound to.
func (ps *PebbleStorage) DatabaseName() string { return ps.dbName }

// ForDatabase returns the same storage bound to another database.
func (ps *PebbleStorage) ForDatabase(name string) Backend {
	if name == "" {
		name = catalog.DefaultDatabase
	}
	return &PebbleStorage{core: ps.core, dbName: name}
}

// CreateDatabase registers an empty database in the metadata.
func (ps *PebbleStorage) CreateDatabase(name string) error {
	if name == "" {
		return fmt.Errorf("database name cannot be empty")
	}
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()
	if _, ok := ps.core.meta.Databases[name]; ok {
		return fmt.Errorf("database %s already exists", name)
	}
	ps.core.meta.Databases[name] = &DatabaseMeta{Tables: make(map[string]map[string]*TableSchema)}
	return ps.saveMetadata()
}

// DeleteDatabase removes every key of a database plus its metadata.
func (ps *PebbleStorage) DeleteDatabase(name string) error {
	if name == "" {
		return fmt.Errorf("database name cannot be empty")
	}
	if name == catalog.DefaultDatabase {
		return fmt.Errorf("cannot delete the default database")
	}
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()
	if err := ps.deletePrefixLocked("db:" + name + ":"); err != nil {
		return err
	}
	delete(ps.core.meta.Databases, name)
	return ps.saveMetadata()
}

// deletePrefixLocked deletes every key starting with prefix in one range
// tombstone (a single synced write). Caller holds core.mu.
func (ps *PebbleStorage) deletePrefixLocked(prefix string) error {
	if err := ps.core.db.DeleteRange([]byte(prefix), []byte(prefix+"\xff"), ps.core.wal); err != nil {
		return fmt.Errorf("failed to delete keys under %s: %w", prefix, err)
	}
	return nil
}

// gobBufPool reuses bytes.Buffer instances across gob encode/decode calls
// to reduce GC pressure during DDL operations (procedures, triggers, jobs).
var gobBufPool = sync.Pool{
	New: func() interface{} { return &bytes.Buffer{} },
}

func (ps *PebbleStorage) SaveProcedure(proc *catalog.Procedure) error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	buf := gobBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer gobBufPool.Put(buf)

	enc := gob.NewEncoder(buf)
	pd := ProcedureData{
		Name:       proc.Name,
		Parameters: proc.Parameters,
		Body:       proc.Body,
		BodyText:   proc.BodyText,
	}
	if err := enc.Encode(pd); err != nil {
		return fmt.Errorf("failed to encode procedure %s: %w", proc.Name, err)
	}

	key := ps.key("proc:" + proc.Name)
	if err := ps.core.db.Set(key, buf.Bytes(), ps.core.wal); err != nil {
		return fmt.Errorf("failed to save procedure %s: %w", proc.Name, err)
	}
	return nil
}

func (ps *PebbleStorage) DeleteProcedure(name string) error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	key := ps.key("proc:" + name)
	if err := ps.core.db.Delete(key, ps.core.wal); err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("failed to delete procedure %s: %w", name, err)
	}
	return nil
}

func (ps *PebbleStorage) SaveView(view *catalog.View, schema string) error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	if schema == "" {
		schema = "public"
	}

	buf := gobBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer gobBufPool.Put(buf)

	enc := gob.NewEncoder(buf)
	vd := ViewData{Name: view.Name, Schema: schema, Query: view.Query, QueryText: view.QueryText}
	vd.Columns = make([]ColumnData, len(view.Columns))
	for i, col := range view.Columns {
		vd.Columns[i] = ColumnData{Name: col.Name, Type: col.Type, NotNull: col.NotNull, Identity: col.Identity, IdentityValue: col.IdentityValue}
	}
	if err := enc.Encode(vd); err != nil {
		return fmt.Errorf("failed to encode view %s.%s: %w", schema, view.Name, err)
	}

	key := ps.key("view:" + schema + ":" + view.Name)
	if err := ps.core.db.Set(key, buf.Bytes(), ps.core.wal); err != nil {
		return fmt.Errorf("failed to save view %s.%s: %w", schema, view.Name, err)
	}
	return nil
}

func (ps *PebbleStorage) DeleteView(name string, schema string) error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	if schema == "" {
		schema = "public"
	}

	key := ps.key("view:" + schema + ":" + name)
	if err := ps.core.db.Delete(key, ps.core.wal); err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("failed to delete view %s.%s: %w", schema, name, err)
	}
	return nil
}

func (ps *PebbleStorage) SaveTrigger(trigger *catalog.Trigger) error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	buf := gobBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer gobBufPool.Put(buf)

	enc := gob.NewEncoder(buf)
	td := TriggerData{
		Name:       trigger.Name,
		Timing:     trigger.Timing,
		Event:      trigger.Event,
		Table:      trigger.Table,
		ForEachRow: trigger.ForEachRow,
		Body:       trigger.Body,
		BodyText:   trigger.BodyText,
	}
	if err := enc.Encode(td); err != nil {
		return fmt.Errorf("failed to encode trigger %s: %w", trigger.Name, err)
	}

	key := ps.key("trig:" + trigger.Name)
	if err := ps.core.db.Set(key, buf.Bytes(), ps.core.wal); err != nil {
		return fmt.Errorf("failed to save trigger %s: %w", trigger.Name, err)
	}
	return nil
}

func (ps *PebbleStorage) DeleteTrigger(name string) error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	key := ps.key("trig:" + name)
	if err := ps.core.db.Delete(key, ps.core.wal); err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("failed to delete trigger %s: %w", name, err)
	}
	return nil
}

func (ps *PebbleStorage) SaveJob(job *catalog.Job) error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	buf := gobBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer gobBufPool.Put(buf)

	enc := gob.NewEncoder(buf)
	jd := JobData{
		Name:     job.Name,
		Interval: job.Interval,
		Unit:     job.Unit,
		Body:     job.Body,
		BodyText: job.BodyText,
		Enabled:  job.Enabled,
	}
	if err := enc.Encode(jd); err != nil {
		return fmt.Errorf("failed to encode job %s: %w", job.Name, err)
	}

	key := ps.key("job:" + job.Name)
	if err := ps.core.db.Set(key, buf.Bytes(), ps.core.wal); err != nil {
		return fmt.Errorf("failed to save job %s: %w", job.Name, err)
	}
	return nil
}

func (ps *PebbleStorage) DeleteJob(name string) error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	key := ps.key("job:" + name)
	if err := ps.core.db.Delete(key, ps.core.wal); err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("failed to delete job %s: %w", name, err)
	}
	return nil
}

// TableMetadata is the persisted schema document ("meta:schema").
//
// Tables is the legacy pre-database layout (schema -> table); it is migrated
// into Databases["postgres"] on first open and left empty afterwards.
type TableMetadata struct {
	Tables    map[string]map[string]*TableSchema `json:"tables,omitempty"`
	Databases map[string]*DatabaseMeta           `json:"databases,omitempty"`
}

// DatabaseMeta holds one database's schema metadata: schema -> table -> TableSchema.
type DatabaseMeta struct {
	Tables map[string]map[string]*TableSchema `json:"tables"`
}

type TableSchema struct {
	Name        string           `json:"name"`
	Columns     []ColumnData     `json:"columns"`
	Constraints []ConstraintData `json:"constraints"`
	Indexes     []IndexData      `json:"indexes,omitempty"`
}

type ProcedureData struct {
	Name       string
	Parameters []ast.Parameter
	Body       []ast.Statement
	BodyText   string
}

type ViewData struct {
	Name    string
	Schema  string
	Columns []ColumnData
	Query   *ast.Select
	// QueryText is the original SELECT SQL. When present it is the canonical,
	// AST-independent definition: on load it is re-parsed with the current
	// parser, so changes to AST node shapes do not invalidate persisted views.
	QueryText string
}

type TriggerData struct {
	Name       string
	Timing     string
	Event      string
	Table      string
	ForEachRow bool
	Body       []ast.Statement
	BodyText   string
}

type JobData struct {
	Name     string
	Interval int
	Unit     string
	Body     []ast.Statement
	BodyText string
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
	gob.Register(&ast.CreateView{})
	gob.Register(&ast.CreateIndex{})
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

	// EXTREME memory optimization for 512MB systems
	cache := pebble.NewCache(1 << 20) // 1MB cache (was 4MB)
	opts := &pebble.Options{
		Cache:                       cache,
		MemTableSize:                256 << 10, // 256KB memtable
		MemTableStopWritesThreshold: 2,         // Only 2 memtables max
		MaxOpenFiles:                50,        // Limit file handles (was 100)
		L0CompactionThreshold:       2,         // Trigger compaction early
		L0StopWritesThreshold:       4,         // Stop writes if too many L0 files
		LBaseMaxBytes:               1 << 20,   // 1MB base level (was 64MB default)
	}

	db, err := pebble.Open(dbPath, opts)
	if err != nil {
		cache.Unref() // Clean up cache on error
		return nil, fmt.Errorf("failed to open pebble database: %w", err)
	}

	core := &pebbleCore{
		db:    db,
		dir:   dir,
		wal:   &pebble.WriteOptions{Sync: true}, // WAL sync enabled
		meta:  &TableMetadata{Databases: make(map[string]*DatabaseMeta)},
		cache: cache,
	}
	ps := &PebbleStorage{core: core, dbName: catalog.DefaultDatabase}

	// Load existing metadata
	if err := ps.loadMetadata(); err != nil {
		log.Printf("[storage] warning: could not load metadata: %v", err)
	}

	// A data directory written before databases existed is migrated once,
	// after a file-level backup of the closed store (the SSTs and WAL are
	// only consistent while Pebble is closed).
	if ps.hasLegacyLayout() {
		backup, err := ps.backupClosedStore(dbPath, opts)
		if err != nil {
			return nil, fmt.Errorf("legacy layout detected but backup failed (nothing migrated): %w", err)
		}
		log.Printf("[storage] legacy layout detected; backup written to %s", backup)
		if err := ps.migrateLegacyLayout(); err != nil {
			log.Printf("[storage] warning: legacy layout migration failed: %v", err)
		}
	}
	ps.ensureDefaultDatabase()

	return ps, nil
}

// hasLegacyLayout reports whether the store still holds pre-database keys or
// the flat metadata map.
func (ps *PebbleStorage) hasLegacyLayout() bool {
	ps.core.mu.RLock()
	defer ps.core.mu.RUnlock()
	if len(ps.core.meta.Tables) > 0 {
		return true
	}
	for _, prefix := range legacyPrefixes {
		iter, err := ps.core.db.NewIter(&pebble.IterOptions{LowerBound: []byte(prefix), UpperBound: []byte(prefix + "\xff")})
		if err != nil {
			return false
		}
		found := iter.First()
		iter.Close()
		if found {
			return true
		}
	}
	return false
}

// backupClosedStore closes Pebble, copies the whole store directory to a
// sibling "pebble.db.backup-<timestamp>" directory and reopens it. Returns the
// backup path.
func (ps *PebbleStorage) backupClosedStore(dbPath string, opts *pebble.Options) (string, error) {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	if err := ps.core.db.Close(); err != nil {
		return "", fmt.Errorf("close before backup: %w", err)
	}
	backup := dbPath + ".backup-" + time.Now().Format("20060102-150405")
	if err := copyDir(dbPath, backup); err != nil {
		// Reopen so the caller can still fail cleanly.
		if db, openErr := pebble.Open(dbPath, opts); openErr == nil {
			ps.core.db = db
		}
		return "", err
	}
	db, err := pebble.Open(dbPath, opts)
	if err != nil {
		return "", fmt.Errorf("reopen after backup: %w", err)
	}
	ps.core.db = db
	return backup, nil
}

// copyDir copies a directory tree (regular files only; Pebble's LOCK file is
// skipped so the backup can be opened independently).
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !info.Mode().IsRegular() || info.Name() == "LOCK" {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

// legacyPrefixes are the key families of the pre-database on-disk layout.
var legacyPrefixes = []string{"table:", "view:", "proc:", "trig:", "job:"}

// legacyDatabases returns the names the pre-database engine registered with
// CREATE DATABASE (rows of the persisted pg_catalog.pg_database table). Each
// of them was materialized as a schema of the same name; the migration turns
// those schemas back into real databases so "psql -d name" keeps working.
// Caller holds core.mu.
func (ps *PebbleStorage) legacyDatabases() map[string]bool {
	out := map[string]bool{}
	val, closer, err := ps.core.db.Get([]byte("table:public:pg_database"))
	if err != nil {
		return out
	}
	defer closer.Close()
	var td TableData
	if err := json.Unmarshal(val, &td); err != nil {
		return out
	}
	nameIdx := -1
	for i, c := range td.Columns {
		if c.Name == "datname" {
			nameIdx = i
		}
	}
	if nameIdx < 0 {
		return out
	}
	for _, row := range td.Rows {
		if nameIdx < len(row) {
			if name, ok := row[nameIdx].(string); ok && name != "" && name != catalog.DefaultDatabase {
				out[name] = true
			}
		}
	}
	return out
}

// migrateLegacyLayout moves data persisted before databases existed (keys
// "table:<schema>:<name>", "view:...", "proc:...", "trig:...", "job:..." and
// the flat metadata Tables map) into the database layout. It runs once: a
// store that has already been migrated has no legacy keys left.
//
//   - Schemas created by the old CREATE DATABASE (listed in the persisted
//     pg_database table) become databases: "table:x:t" → "db:x:table:public:t".
//   - Everything else lands in the default database with its schema intact:
//     "table:s:t" → "db:postgres:table:s:t"; procedures, triggers and jobs
//     (global before) belong to the default database.
//   - The persisted pg_database table is dropped: the cluster is the source
//     of truth for databases now.
func (ps *PebbleStorage) migrateLegacyLayout() error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	legacyDBs := ps.legacyDatabases()
	target := func(kind, key string) string {
		// key is "kind:rest"; for table/view the rest is "<schema>:<name>".
		if kind == "table" || kind == "view" {
			parts := strings.SplitN(strings.TrimPrefix(key, kind+":"), ":", 2)
			if len(parts) == 2 && legacyDBs[parts[0]] {
				return "db:" + parts[0] + ":" + kind + ":public:" + parts[1]
			}
		}
		return "db:" + catalog.DefaultDatabase + ":" + key
	}

	// Every key move goes into one batch: a single synced write instead of two
	// fsyncs per object, and the migration is applied atomically.
	batch := ps.core.db.NewBatch()
	defer batch.Close()
	moved := 0
	for _, prefix := range legacyPrefixes {
		iter, err := ps.core.db.NewIter(&pebble.IterOptions{LowerBound: []byte(prefix), UpperBound: []byte(prefix + "\xff")})
		if err != nil {
			return err
		}
		kind := strings.TrimSuffix(prefix, ":")
		for iter.First(); iter.Valid(); iter.Next() {
			key := string(iter.Key())
			if key != "table:public:pg_database" {
				if err := batch.Set([]byte(target(kind, key)), iter.Value(), nil); err != nil {
					iter.Close()
					return fmt.Errorf("migrate %s: %w", key, err)
				}
			}
			if err := batch.Delete(iter.Key(), nil); err != nil {
				iter.Close()
				return fmt.Errorf("migrate %s: %w", key, err)
			}
			moved++
		}
		if err := iter.Close(); err != nil {
			return err
		}
	}
	if moved > 0 {
		if err := ps.core.db.Apply(batch, ps.core.wal); err != nil {
			return fmt.Errorf("apply migration batch: %w", err)
		}
	}

	if len(ps.core.meta.Tables) > 0 {
		dbMeta := func(name string) *DatabaseMeta {
			dm := ps.core.meta.Databases[name]
			if dm == nil {
				dm = &DatabaseMeta{Tables: make(map[string]map[string]*TableSchema)}
				ps.core.meta.Databases[name] = dm
			}
			if dm.Tables == nil {
				dm.Tables = make(map[string]map[string]*TableSchema)
			}
			return dm
		}
		for schema, tables := range ps.core.meta.Tables {
			dm, targetSchema := dbMeta(catalog.DefaultDatabase), schema
			if legacyDBs[schema] {
				dm, targetSchema = dbMeta(schema), "public"
			}
			if _, ok := dm.Tables[targetSchema]; !ok {
				dm.Tables[targetSchema] = make(map[string]*TableSchema)
			}
			for name, ts := range tables {
				if schema == "public" && name == "pg_database" {
					continue
				}
				dm.Tables[targetSchema][name] = ts
			}
		}
		ps.core.meta.Tables = nil
		moved++
	}
	for name := range legacyDBs {
		if ps.core.meta.Databases[name] == nil {
			ps.core.meta.Databases[name] = &DatabaseMeta{Tables: make(map[string]map[string]*TableSchema)}
		}
	}

	if moved > 0 {
		log.Printf("[storage] migrated legacy layout: %d entries into database %s, %d legacy database(s) promoted", moved, catalog.DefaultDatabase, len(legacyDBs))
		return ps.saveMetadata()
	}
	return nil
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
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	if schema == "" {
		schema = "public"
	}

	key := ps.key("table:" + schema + ":" + name)
	if err := ps.core.db.Delete(key, ps.core.wal); err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("failed to delete table %s.%s: %w", schema, name, err)
	}

	if _, ok := ps.tables()[schema]; ok {
		delete(ps.tables()[schema], name)
	}
	return ps.saveMetadata()
}

// CreateSchema persists an empty schema namespace into metadata.
func (ps *PebbleStorage) CreateSchema(name string) error {
	if name == "" {
		return fmt.Errorf("schema name cannot be empty")
	}
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	tables, err := ps.tablesForWrite()
	if err != nil {
		return err
	}
	if _, ok := tables[name]; ok {
		return fmt.Errorf("schema %s already exists", name)
	}
	tables[name] = make(map[string]*TableSchema)
	return ps.saveMetadata()
}

// DeleteSchema removes a schema and all its tables from persistent storage.
func (ps *PebbleStorage) DeleteSchema(name string) error {
	if name == "" {
		return fmt.Errorf("schema name cannot be empty")
	}
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	if _, ok := ps.tables()[name]; !ok {
		return fmt.Errorf("schema %s does not exist", name)
	}

	// Delete all tables in this schema from Pebble
	tablesInSchema := ps.tables()[name]
	for tableName := range tablesInSchema {
		key := ps.key("table:" + name + ":" + tableName)
		if err := ps.core.db.Delete(key, ps.core.wal); err != nil && err != pebble.ErrNotFound {
			return fmt.Errorf("failed to delete table %s.%s from pebble: %w", name, tableName, err)
		}
	}

	// Remove schema from metadata
	delete(ps.tables(), name)
	return ps.saveMetadata()
}

// saveTableWithSchema persists a table with explicit schema
func (ps *PebbleStorage) saveTableWithSchema(table *catalog.Table, schema string) error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	if _, err := ps.tablesForWrite(); err != nil {
		return err
	}
	key := ps.key("table:" + schema + ":" + table.Name)

	// Encode rows inside the table's read lock to avoid copying all rows into
	// a separate slice. json.Marshal runs while the lock is held, preventing
	// concurrent mutations from racing with the serializer.
	var data []byte
	var marshalErr error
	table.UseRows(func(rows [][]interface{}) {
		data, marshalErr = json.Marshal(TableData{
			Name:        table.Name,
			Columns:     convertColumns(table.Columns),
			Constraints: convertConstraints(table.Constraints),
			Indexes:     convertIndexes(table.Indexes),
			Rows:        rows,
		})
	})
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal table %s: %w", table.Name, marshalErr)
	}

	// Write with WAL sync
	if err := ps.core.db.Set(key, data, ps.core.wal); err != nil {
		return fmt.Errorf("failed to save table %s: %w", table.Name, err)
	}

	// Update metadata
	tables, err := ps.tablesForWrite()
	if err != nil {
		return err
	}
	if _, ok := tables[schema]; !ok {
		tables[schema] = make(map[string]*TableSchema)
	}
	tables[schema][table.Name] = &TableSchema{
		Name:        table.Name,
		Columns:     convertColumns(table.Columns),
		Constraints: convertConstraints(table.Constraints),
		Indexes:     convertIndexes(table.Indexes),
	}

	// Persist metadata
	return ps.saveMetadata()
}

// LoadTable retrieves a table from Pebble (implements Backend interface)
func (ps *PebbleStorage) LoadTable(cat *catalog.Catalog, name string) error {
	ps.core.mu.RLock()
	defer ps.core.mu.RUnlock()
	return ps.loadTableLocked(cat, name, "public")
}

// loadTableLocked retrieves a table from Pebble with schema support. The
// caller must hold core.mu (read or write): it is never re-acquired here, so
// loadDatabase can hold a single read lock for the whole load.
func (ps *PebbleStorage) loadTableLocked(cat *catalog.Catalog, name string, schema string) error {
	if strings.HasPrefix(name, "pg_catalog.") {
		return nil
	}

	key := ps.key("table:" + schema + ":" + name)
	val, closer, err := ps.core.db.Get(key)
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
		for _, idx := range td.Indexes {
			if err := table.CreateIndex(idx.Name, indexColumnsFromData(idx)); err != nil {
				return fmt.Errorf("failed to create index %s on table %s.%s: %w", idx.Name, schema, td.Name, err)
			}
		}

		// Assign decoded rows directly instead of inserting one-by-one.
		// InsertRowUnsafe would repeatedly append, causing multiple backing-array
		// reallocations. SetRows reuses the slice JSON already allocated.
		if len(td.Rows) > 0 {
			table.SetRows(td.Rows)
		}
		syncIdentityValues(table)
	}

	return nil
}

// LoadAll loads the bound database into cat and, when cat belongs to a
// cluster, every other persisted database into that cluster (creating the
// missing catalogs).
func (ps *PebbleStorage) LoadAll(cat *catalog.Catalog) error {
	if err := ps.loadDatabase(cat); err != nil {
		return err
	}
	cl := cat.Cluster()
	if cl == nil {
		return nil
	}
	ps.core.mu.RLock()
	names := make([]string, 0, len(ps.core.meta.Databases))
	for name := range ps.core.meta.Databases {
		if name != ps.dbName {
			names = append(names, name)
		}
	}
	ps.core.mu.RUnlock()
	for _, name := range names {
		dbCat, ok := cl.Database(name)
		if !ok {
			created, err := cl.CreateDatabase(name)
			if err != nil {
				log.Printf("[storage] warning: cannot recreate database %s: %v", name, err)
				continue
			}
			dbCat = created
		}
		if err := ps.ForDatabase(name).(*PebbleStorage).loadDatabase(dbCat); err != nil {
			log.Printf("[storage] warning: failed to load database %s: %v", name, err)
		}
	}
	return nil
}

// loadDatabase loads every object of the bound database into cat under a
// single read lock (no nested locking on the load path).
func (ps *PebbleStorage) loadDatabase(cat *catalog.Catalog) error {
	ps.core.mu.RLock()
	defer ps.core.mu.RUnlock()

	var tables map[string]map[string]*TableSchema
	if dm := ps.core.meta.Databases[ps.dbName]; dm != nil {
		tables = dm.Tables
	}

	// Recreate persisted schemas first, including empty schemas (without tables)
	for schema := range tables {
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

	prefix := ps.prefix()
	iter, err := ps.core.db.NewIter(&pebble.IterOptions{LowerBound: []byte(prefix), UpperBound: []byte(prefix + "\xff")})
	if err != nil {
		return fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Iterate through the database's keys
	for iter.First(); iter.Valid(); iter.Next() {
		key := strings.TrimPrefix(string(iter.Key()), prefix)
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
			if err := ps.loadTableLocked(cat, tableName, schema); err != nil {
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
			body := pd.Body
			if pd.BodyText != "" {
				if reparsed, perr := parseBody(pd.BodyText); perr == nil {
					body = reparsed
				} else {
					log.Printf("[storage] warning: failed to re-parse procedure %s body, using stored AST: %v", pd.Name, perr)
				}
			}
			if err := cat.LoadProcedure(pd.Name, pd.Parameters, body, pd.BodyText); err != nil {
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
			tbody := td.Body
			if td.BodyText != "" {
				if reparsed, perr := parseBody(td.BodyText); perr == nil {
					tbody = reparsed
				} else {
					log.Printf("[storage] warning: failed to re-parse trigger %s body, using stored AST: %v", td.Name, perr)
				}
			}
			if err := cat.LoadTrigger(td.Name, td.Timing, td.Event, td.Table, td.ForEachRow, tbody, td.BodyText); err != nil {
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
			jbody := jd.Body
			if jd.BodyText != "" {
				if reparsed, perr := parseBody(jd.BodyText); perr == nil {
					jbody = reparsed
				} else {
					log.Printf("[storage] warning: failed to re-parse job %s body, using stored AST: %v", jd.Name, perr)
				}
			}
			if err := cat.LoadJob(jd.Name, jd.Interval, jd.Unit, jbody, jd.Enabled, jd.BodyText); err != nil {
				log.Printf("[storage] warning: failed to load job %s: %v", jd.Name, err)
			}
		} else if strings.HasPrefix(key, "view:") {
			val := append([]byte(nil), iter.Value()...)
			var vd ViewData
			dec := gob.NewDecoder(bytes.NewReader(val))
			if err := dec.Decode(&vd); err != nil {
				log.Printf("[storage] warning: failed to decode view %s: %v", strings.TrimPrefix(key, "view:"), err)
				continue
			}
			cols := make([]catalog.Column, len(vd.Columns))
			for i, col := range vd.Columns {
				cols[i] = catalog.Column{Name: col.Name, Type: col.Type, NotNull: col.NotNull, Identity: col.Identity, IdentityValue: col.IdentityValue}
			}
			// Prefer re-parsing the stored SQL text (AST-independent). Fall back
			// to the serialized AST for views persisted before QueryText existed.
			query := vd.Query
			if vd.QueryText != "" {
				if reparsed, perr := parseViewQuery(vd.QueryText); perr == nil {
					query = reparsed
				} else {
					log.Printf("[storage] warning: failed to re-parse view %s.%s text, using stored AST: %v", vd.Schema, vd.Name, perr)
				}
			}
			if err := cat.LoadView(vd.Name, cols, query, vd.Schema); err != nil {
				log.Printf("[storage] warning: failed to load view %s.%s: %v", vd.Schema, vd.Name, err)
			}
		}
	}

	return iter.Error()
}

// Close closes the Pebble database and releases cache
func (ps *PebbleStorage) Close() error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	var err error
	if ps.core.db != nil {
		err = ps.core.db.Close()
	}
	if ps.core.cache != nil {
		ps.core.cache.Unref()
	}
	return err
}

// Meta returns the current metadata for inspection: Tables is the bound
// database's schema map (legacy accessor), Databases the whole cluster.
func (ps *PebbleStorage) Meta() *TableMetadata {
	ps.core.mu.RLock()
	defer ps.core.mu.RUnlock()
	return &TableMetadata{Tables: ps.tables(), Databases: ps.core.meta.Databases}
}

// Helper functions
func (ps *PebbleStorage) saveMetadata() error {
	data, err := json.Marshal(ps.core.meta)
	if err != nil {
		return err
	}
	return ps.core.db.Set([]byte("meta:schema"), data, ps.core.wal)
}

func (ps *PebbleStorage) loadMetadata() error {
	val, closer, err := ps.core.db.Get([]byte("meta:schema"))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil
		}
		return err
	}
	defer closer.Close()

	if err := json.Unmarshal(val, ps.core.meta); err != nil {
		return err
	}
	if ps.core.meta.Databases == nil {
		ps.core.meta.Databases = make(map[string]*DatabaseMeta)
	}
	return nil
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

func convertIndexes(indexes map[string]*catalog.Index) []IndexData {
	if len(indexes) == 0 {
		return nil
	}
	result := make([]IndexData, 0, len(indexes))
	for _, idx := range indexes {
		result = append(result, IndexData{
			Name:        idx.Name,
			ColumnNames: append([]string(nil), idx.ColumnNames...),
		})
	}
	return result
}

// DropColumnData removes a column from all rows in a table
func (ps *PebbleStorage) DropColumnData(tableName string, columnName string, schema string) error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	// Load table data
	key := ps.key("table:" + schema + ":" + tableName)
	val, closer, err := ps.core.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil // Table not found, nothing to do
		}
		return fmt.Errorf("failed to get table %s: %w", tableName, err)
	}

	valCopy := make([]byte, len(val))
	copy(valCopy, val)
	closer.Close()

	var tableData TableData
	if err := json.Unmarshal(valCopy, &tableData); err != nil {
		return fmt.Errorf("failed to unmarshal table %s: %w", tableName, err)
	}

	// Find column index
	colIdx := -1
	for i, col := range tableData.Columns {
		if col.Name == columnName {
			colIdx = i
			break
		}
	}

	if colIdx < 0 {
		return nil // Column not found, nothing to do
	}

	// Remove column from schema
	newColumns := make([]ColumnData, 0, len(tableData.Columns)-1)
	for i, col := range tableData.Columns {
		if i != colIdx {
			newColumns = append(newColumns, col)
		}
	}
	tableData.Columns = newColumns

	// Remove indexes that target dropped column.
	if len(tableData.Indexes) > 0 {
		newIndexes := make([]IndexData, 0, len(tableData.Indexes))
		for _, idx := range tableData.Indexes {
			idxCols := indexColumnsFromData(idx)
			drop := false
			for _, c := range idxCols {
				if c == columnName {
					drop = true
					break
				}
			}
			if !drop {
				newIndexes = append(newIndexes, idx)
			}
		}
		tableData.Indexes = newIndexes
	}

	// Remove column data from each row
	for i := range tableData.Rows {
		if colIdx < len(tableData.Rows[i]) {
			tableData.Rows[i] = append(tableData.Rows[i][:colIdx], tableData.Rows[i][colIdx+1:]...)
		}
	}

	// Save updated table
	data, err := json.Marshal(tableData)
	if err != nil {
		return fmt.Errorf("failed to marshal table %s: %w", tableName, err)
	}

	if err := ps.core.db.Set(key, data, ps.core.wal); err != nil {
		return fmt.Errorf("failed to save table %s: %w", tableName, err)
	}

	// Update metadata
	if schemaMap, ok := ps.tables()[schema]; ok {
		if _, ok := schemaMap[tableName]; ok {
			ps.tables()[schema][tableName].Columns = newColumns
			if len(ps.tables()[schema][tableName].Indexes) > 0 {
				newIndexes := make([]IndexData, 0, len(ps.tables()[schema][tableName].Indexes))
				for _, idx := range ps.tables()[schema][tableName].Indexes {
					idxCols := indexColumnsFromData(idx)
					drop := false
					for _, c := range idxCols {
						if c == columnName {
							drop = true
							break
						}
					}
					if !drop {
						newIndexes = append(newIndexes, idx)
					}
				}
				ps.tables()[schema][tableName].Indexes = newIndexes
			}
			return ps.saveMetadata()
		}
	}

	return nil
}

// RenameColumnData renames a column in all rows in a table
// Note: Since rows are stored as arrays, not maps, we only need to update the schema
func (ps *PebbleStorage) RenameColumnData(tableName string, oldName string, newName string, schema string) error {
	ps.core.mu.Lock()
	defer ps.core.mu.Unlock()

	// Load table data
	key := ps.key("table:" + schema + ":" + tableName)
	val, closer, err := ps.core.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil // Table not found, nothing to do
		}
		return fmt.Errorf("failed to get table %s: %w", tableName, err)
	}

	valCopy := make([]byte, len(val))
	copy(valCopy, val)
	closer.Close()

	var tableData TableData
	if err := json.Unmarshal(valCopy, &tableData); err != nil {
		return fmt.Errorf("failed to unmarshal table %s: %w", tableName, err)
	}

	// Find and rename column in schema
	found := false
	for i := range tableData.Columns {
		if tableData.Columns[i].Name == oldName {
			tableData.Columns[i].Name = newName
			found = true
			break
		}
	}

	if !found {
		return nil // Column not found, nothing to do
	}

	for i := range tableData.Indexes {
		cols := indexColumnsFromData(tableData.Indexes[i])
		for j := range cols {
			if cols[j] == oldName {
				cols[j] = newName
			}
		}
		tableData.Indexes[i].ColumnNames = cols
		tableData.Indexes[i].ColumnName = ""
	}

	// Save updated table (rows don't need to change, only schema)
	data, err := json.Marshal(tableData)
	if err != nil {
		return fmt.Errorf("failed to marshal table %s: %w", tableName, err)
	}

	if err := ps.core.db.Set(key, data, ps.core.wal); err != nil {
		return fmt.Errorf("failed to save table %s: %w", tableName, err)
	}

	// Update metadata
	if schemaMap, ok := ps.tables()[schema]; ok {
		if _, ok := schemaMap[tableName]; ok {
			for i := range ps.tables()[schema][tableName].Columns {
				if ps.tables()[schema][tableName].Columns[i].Name == oldName {
					ps.tables()[schema][tableName].Columns[i].Name = newName
					break
				}
			}
			for i := range ps.tables()[schema][tableName].Indexes {
				cols := indexColumnsFromData(ps.tables()[schema][tableName].Indexes[i])
				for j := range cols {
					if cols[j] == oldName {
						cols[j] = newName
					}
				}
				ps.tables()[schema][tableName].Indexes[i].ColumnNames = cols
				ps.tables()[schema][tableName].Indexes[i].ColumnName = ""
			}
			return ps.saveMetadata()
		}
	}

	return nil
}
