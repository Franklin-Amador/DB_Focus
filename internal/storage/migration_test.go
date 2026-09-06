package storage

import (
	"encoding/json"
	"testing"

	"github.com/cockroachdb/pebble"

	"dbf/internal/catalog"
)

// TestLegacyLayoutMigratesIntoDefaultDatabase opens a store that only holds the
// pre-database key layout (table:<schema>:<name> + flat metadata) and checks
// that it is moved under "db:postgres:" once, then loads normally.
func TestLegacyLayoutMigratesIntoDefaultDatabase(t *testing.T) {
	dir := t.TempDir()

	ps, err := NewPebbleStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Write a legacy table + legacy metadata straight into Pebble.
	legacyTable := TableData{
		Name:    "clientes",
		Columns: []ColumnData{{Name: "id", Type: "INT"}, {Name: "nombre", Type: "TEXT"}},
		Rows:    [][]interface{}{{1, "ana"}, {2, "luis"}},
	}
	raw, _ := json.Marshal(legacyTable)
	if err := ps.core.db.Set([]byte("table:public:clientes"), raw, ps.core.wal); err != nil {
		t.Fatal(err)
	}
	legacyMeta, _ := json.Marshal(map[string]interface{}{
		"tables": map[string]interface{}{
			"public": map[string]interface{}{"clientes": map[string]interface{}{"name": "clientes", "columns": legacyTable.Columns}},
			"viejo":  map[string]interface{}{},
		},
	})
	if err := ps.core.db.Set([]byte("meta:schema"), legacyMeta, ps.core.wal); err != nil {
		t.Fatal(err)
	}
	if err := ps.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen: migration runs in NewPebbleStorage.
	ps, err = NewPebbleStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	if _, closer, err := ps.core.db.Get([]byte("table:public:clientes")); err != pebble.ErrNotFound {
		if err == nil {
			closer.Close()
		}
		t.Fatalf("legacy key must be gone after migration, err=%v", err)
	}
	if _, closer, err := ps.core.db.Get([]byte("db:postgres:table:public:clientes")); err != nil {
		t.Fatalf("migrated key missing: %v", err)
	} else {
		closer.Close()
	}
	meta := ps.Meta()
	if len(meta.Tables["public"]) != 1 || meta.Databases["postgres"] == nil {
		t.Fatalf("metadata not migrated: %+v", meta)
	}
	if _, ok := meta.Tables["viejo"]; !ok {
		t.Fatalf("empty legacy schema lost in migration: %+v", meta.Tables)
	}

	cl := catalog.NewCluster()
	if err := ps.LoadAll(cl.Default()); err != nil {
		t.Fatal(err)
	}
	tbl, err := cl.Default().GetTable("clientes")
	if err != nil {
		t.Fatalf("migrated table not loaded: %v", err)
	}
	if rows := tbl.SelectAll(); len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if !cl.Default().SchemaExists("viejo") {
		t.Fatal("empty legacy schema not recreated")
	}
}

// TestDatabasesPersistSeparately checks that two databases keep separate key
// spaces and metadata, survive a reopen, and that DeleteDatabase removes
// exactly one of them.
func TestDatabasesPersistSeparately(t *testing.T) {
	dir := t.TempDir()
	ps, err := NewPebbleStorage(dir)
	if err != nil {
		t.Fatal(err)
	}

	cl := catalog.NewCluster()
	ventas, err := cl.CreateDatabase("ventas")
	if err != nil {
		t.Fatal(err)
	}
	if err := ps.CreateDatabase("ventas"); err != nil {
		t.Fatal(err)
	}
	cols := []catalog.Column{{Name: "id", Type: "INT"}}
	for _, c := range []*catalog.Catalog{cl.Default(), ventas} {
		if err := c.CreateTable("t", cols, nil); err != nil {
			t.Fatal(err)
		}
	}
	tDefault, _ := cl.Default().GetTable("t")
	tDefault.InsertRowUnsafe([]interface{}{1})
	tVentas, _ := ventas.GetTable("t")
	tVentas.InsertRowUnsafe([]interface{}{1})
	tVentas.InsertRowUnsafe([]interface{}{2})
	if err := ps.SaveTable(tDefault); err != nil {
		t.Fatal(err)
	}
	if err := ps.ForDatabase("ventas").SaveTable(tVentas); err != nil {
		t.Fatal(err)
	}
	if err := ps.Close(); err != nil {
		t.Fatal(err)
	}

	ps, err = NewPebbleStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()
	cl2 := catalog.NewCluster()
	if err := ps.LoadAll(cl2.Default()); err != nil {
		t.Fatal(err)
	}
	v2, ok := cl2.Database("ventas")
	if !ok {
		t.Fatal("ventas not recreated on load")
	}
	d, _ := cl2.Default().GetTable("t")
	v, _ := v2.GetTable("t")
	if len(d.SelectAll()) != 1 || len(v.SelectAll()) != 2 {
		t.Fatalf("databases mixed up: default=%d ventas=%d", len(d.SelectAll()), len(v.SelectAll()))
	}

	if err := ps.DeleteDatabase("ventas"); err != nil {
		t.Fatal(err)
	}
	if err := ps.DeleteDatabase(catalog.DefaultDatabase); err == nil {
		t.Fatal("deleting the default database must fail")
	}
	if _, ok := ps.Meta().Databases["ventas"]; ok {
		t.Fatal("ventas metadata still present")
	}
	if _, closer, err := ps.core.db.Get([]byte("db:ventas:table:public:t")); err != pebble.ErrNotFound {
		if err == nil {
			closer.Close()
		}
		t.Fatalf("ventas keys still present: %v", err)
	}
	if _, closer, err := ps.core.db.Get([]byte("db:postgres:table:public:t")); err != nil {
		t.Fatalf("default database data lost: %v", err)
	} else {
		closer.Close()
	}
}
