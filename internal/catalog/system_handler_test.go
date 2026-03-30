package catalog

import (
	"testing"

	"dbf/internal/ast"
)

func TestHandleSystemQueryForDatabaseFiltersTables(t *testing.T) {
	c := New()

	if err := c.CreateSchema("db_a"); err != nil {
		t.Fatalf("create schema db_a: %v", err)
	}
	if err := c.CreateSchema("db_b"); err != nil {
		t.Fatalf("create schema db_b: %v", err)
	}

	cols := []Column{{Name: "id", Type: "INT"}}
	if err := c.CreateTable("orders_a", cols, nil, "db_a"); err != nil {
		t.Fatalf("create table orders_a: %v", err)
	}
	if err := c.CreateTable("orders_b", cols, nil, "db_b"); err != nil {
		t.Fatalf("create table orders_b: %v", err)
	}

	resA, ok := c.HandleSystemQueryForDatabase("SELECT * FROM information_schema.tables", "db_a")
	if !ok {
		t.Fatalf("information_schema.tables query was not handled")
	}
	if len(resA.Rows) != 1 {
		t.Fatalf("expected 1 table for db_a, got %d", len(resA.Rows))
	}
	if got := resA.Rows[0][2]; got != "orders_a" {
		t.Fatalf("expected orders_a in db_a, got %v", got)
	}

	resB, ok := c.HandleSystemQueryForDatabase("SELECT * FROM information_schema.tables", "db_b")
	if !ok {
		t.Fatalf("information_schema.tables query was not handled")
	}
	if len(resB.Rows) != 1 {
		t.Fatalf("expected 1 table for db_b, got %d", len(resB.Rows))
	}
	if got := resB.Rows[0][2]; got != "orders_b" {
		t.Fatalf("expected orders_b in db_b, got %v", got)
	}

	classA, ok := c.HandleSystemQueryForDatabase("SELECT relname FROM pg_catalog.pg_class", "db_a")
	if !ok {
		t.Fatalf("pg_class query was not handled")
	}
	if len(classA.Rows) != 1 {
		t.Fatalf("expected 1 pg_class row for db_a, got %d", len(classA.Rows))
	}
	if got := classA.Rows[0][2]; got != "orders_a" {
		t.Fatalf("expected relname orders_a, got %v", got)
	}
}

func TestHandleSystemQueryForDatabaseDtPattern(t *testing.T) {
	c := New()

	if err := c.CreateSchema("db_a"); err != nil {
		t.Fatalf("create schema db_a: %v", err)
	}
	if err := c.CreateSchema("db_b"); err != nil {
		t.Fatalf("create schema db_b: %v", err)
	}

	cols := []Column{{Name: "id", Type: "INT"}}
	if err := c.CreateTable("only_a", cols, nil, "db_a"); err != nil {
		t.Fatalf("create table only_a: %v", err)
	}
	if err := c.CreateTable("only_b", cols, nil, "db_b"); err != nil {
		t.Fatalf("create table only_b: %v", err)
	}

	dtQuery := `SELECT n.nspname as "Schema",
  c.relname as "Name",
  CASE c.relkind WHEN 'r' THEN 'table' WHEN 'v' THEN 'view' WHEN 'm' THEN 'materialized view' WHEN 'i' THEN 'index' WHEN 'S' THEN 'sequence' WHEN 't' THEN 'TOAST table' WHEN 'f' THEN 'foreign table' WHEN 'p' THEN 'partitioned table' WHEN 'I' THEN 'partitioned index' END as "Type",
  pg_catalog.pg_get_userbyid(c.relowner) as "Owner"
FROM pg_catalog.pg_class c
     LEFT JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
     LEFT JOIN pg_catalog.pg_am am ON am.oid = c.relam
WHERE c.relkind IN ('r','p','')
      AND n.nspname <> 'pg_catalog'
      AND n.nspname !~ '^pg_toast'
      AND n.nspname <> 'information_schema'
  AND pg_catalog.pg_table_is_visible(c.oid)
ORDER BY 1,2;`

	resA, ok := c.HandleSystemQueryForDatabase(dtQuery, "db_a")
	if !ok {
		t.Fatalf("dt query should be handled")
	}
	if len(resA.Rows) != 1 {
		t.Fatalf("expected 1 relation for db_a, got %d", len(resA.Rows))
	}
	if got := resA.Rows[0][1]; got != "only_a" {
		t.Fatalf("expected only_a for db_a, got %v", got)
	}

	resB, ok := c.HandleSystemQueryForDatabase(dtQuery, "db_b")
	if !ok {
		t.Fatalf("dt query should be handled")
	}
	if len(resB.Rows) != 1 {
		t.Fatalf("expected 1 relation for db_b, got %d", len(resB.Rows))
	}
	if got := resB.Rows[0][1]; got != "only_b" {
		t.Fatalf("expected only_b for db_b, got %v", got)
	}
}

func TestHandleSystemQueryForDatabaseDvPattern(t *testing.T) {
	c := New()

	cols := []Column{{Name: "id", Type: "INT"}, {Name: "name", Type: "TEXT"}}
	if err := c.CreateTable("users", cols, nil); err != nil {
		t.Fatalf("create table users: %v", err)
	}
	if err := c.CreateView("v_users", cols, &ast.Select{Table: ast.Identifier{Name: "users"}, Star: true}); err != nil {
		t.Fatalf("create view v_users: %v", err)
	}

	dvQuery := `SELECT n.nspname as "Schema",
  c.relname as "Name",
  CASE c.relkind WHEN 'r' THEN 'table' WHEN 'v' THEN 'view' END as "Type",
  pg_catalog.pg_get_userbyid(c.relowner) as "Owner"
FROM pg_catalog.pg_class c
     LEFT JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('v','')
  AND pg_catalog.pg_table_is_visible(c.oid)
ORDER BY 1,2;`

	res, ok := c.HandleSystemQueryForDatabase(dvQuery, "postgres")
	if !ok {
		t.Fatalf("dv query should be handled")
	}
	if len(res.Rows) != 1 {
		t.Fatalf("expected 1 view row, got %d", len(res.Rows))
	}
	if got := res.Rows[0][1]; got != "v_users" {
		t.Fatalf("expected v_users in dv output, got %v", got)
	}
	if gotType := res.Rows[0][2]; gotType != "view" {
		t.Fatalf("expected relation type view, got %v", gotType)
	}
}

func TestHandleSystemQueryForDatabaseDtPatternNamespaceFirst(t *testing.T) {
	c := New()

	cols := []Column{{Name: "id", Type: "INT"}}
	if err := c.CreateTable("users", cols, nil); err != nil {
		t.Fatalf("create table users: %v", err)
	}

	query := `SELECT n.nspname AS "Schema", c.relname AS "Name"
FROM pg_catalog.pg_namespace n
JOIN pg_catalog.pg_class c ON n.oid = c.relnamespace
WHERE c.relkind IN ('r')
  AND pg_catalog.pg_table_is_visible(c.oid)
ORDER BY 1,2;`

	res, ok := c.HandleSystemQueryForDatabase(query, "postgres")
	if !ok {
		t.Fatalf("namespace-first dt query should be handled")
	}
	if len(res.Rows) == 0 {
		t.Fatalf("expected at least one relation row")
	}
	foundUsers := false
	for _, row := range res.Rows {
		if len(row) >= 2 && row[1] == "users" {
			foundUsers = true
			break
		}
	}
	if !foundUsers {
		t.Fatalf("expected users table in dt-like results, got %+v", res.Rows)
	}
}

func TestHandleSystemQueryPgDatabaseIncludesUserDatabases(t *testing.T) {
	c := New()

	if err := c.CreateSchema("testdb"); err != nil {
		t.Fatalf("create schema testdb: %v", err)
	}

	res, ok := c.HandleSystemQueryForDatabase("SELECT * FROM pg_catalog.pg_database", "postgres")
	if !ok {
		t.Fatalf("pg_database query was not handled")
	}

	foundPostgres := false
	foundTestdb := false
	for _, row := range res.Rows {
		if len(row) < 2 {
			continue
		}
		name, _ := row[1].(string)
		if name == "postgres" {
			foundPostgres = true
		}
		if name == "testdb" {
			foundTestdb = true
		}
	}

	if !foundPostgres || !foundTestdb {
		t.Fatalf("expected postgres and testdb in pg_database rows, got %+v", res.Rows)
	}
}

func TestHandleSystemQueryPgDatabaseUnqualified(t *testing.T) {
	c := New()
	if err := c.CreateSchema("testdb"); err != nil {
		t.Fatalf("create schema testdb: %v", err)
	}

	res, ok := c.HandleSystemQueryForDatabase("SELECT datname FROM pg_database", "postgres")
	if !ok {
		t.Fatalf("unqualified pg_database query was not handled")
	}
	if len(res.Rows) < 2 {
		t.Fatalf("expected at least 2 database rows, got %d", len(res.Rows))
	}
}

func TestDatabaseExistsAcceptsUserSchemaDatabase(t *testing.T) {
	c := New()
	if err := c.CreateSchema("testdb"); err != nil {
		t.Fatalf("create schema testdb: %v", err)
	}

	if !c.DatabaseExists("testdb") {
		t.Fatalf("expected DatabaseExists(testdb)=true")
	}
	if c.DatabaseExists("pg_catalog") {
		t.Fatalf("expected DatabaseExists(pg_catalog)=false")
	}
}
