// Command test-schemas exercises schema namespaces end to end: the active
// schema (ast.ApplyDefaultSchema, as used by the GUI and the wire protocol),
// schema-qualified table references with aliases, views bound to their
// schema, cross-schema JOINs, CREATE SCHEMA IF NOT EXISTS, protected schemas,
// databases as isolated containers (own schemas, executor and storage), and
// persistence of schemas, views and databases across a storage reload.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
	"dbf/internal/storage"
)

func run(ctx context.Context, exe *executor.Executor, schema, sql string) (*executor.Result, error) {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	if schema != "" && schema != "public" {
		ast.ApplyDefaultSchema(stmt, schema)
	}
	return exe.Execute(ctx, stmt)
}

func query(ctx context.Context, exe *executor.Executor, schema, sql string) *executor.Result {
	res, err := run(ctx, exe, schema, sql)
	if err != nil {
		log.Fatalf("[%s] %q: %v", schema, sql, err)
	}
	return res
}

func rowsText(res *executor.Result) []string {
	out := make([]string, 0, len(res.Rows))
	for _, row := range res.Rows {
		parts := make([]string, len(row))
		for i, v := range row {
			parts[i] = fmt.Sprintf("%v", v)
		}
		out = append(out, strings.Join(parts, "|"))
	}
	return out
}

func expectRows(ctx context.Context, exe *executor.Executor, schema, sql string, want []string) {
	got := rowsText(query(ctx, exe, schema, sql))
	if strings.Join(got, ";") != strings.Join(want, ";") {
		log.Fatalf("[%s] %q:\n  want %v\n  got  %v", schema, sql, want, got)
	}
	fmt.Printf("OK  [%-8s] %-80s -> %d rows\n", schema, sql, len(got))
}

func expectTag(ctx context.Context, exe *executor.Executor, schema, sql, tag string) {
	res := query(ctx, exe, schema, sql)
	if res.Tag != tag {
		log.Fatalf("[%s] %q: expected tag %q, got %q", schema, sql, tag, res.Tag)
	}
	fmt.Printf("OK  [%-8s] %-80s -> %s\n", schema, sql, res.Tag)
}

func expectErr(ctx context.Context, exe *executor.Executor, schema, sql, fragment string) {
	_, err := run(ctx, exe, schema, sql)
	if err == nil {
		log.Fatalf("[%s] %q: expected error containing %q, got success", schema, sql, fragment)
	}
	if !strings.Contains(err.Error(), fragment) {
		log.Fatalf("[%s] %q: expected error containing %q, got %v", schema, sql, fragment, err)
	}
	fmt.Printf("OK  [%-8s] %-80s -> error: %v\n", schema, sql, err)
}

func main() {
	fmt.Println("=== Testing schema namespaces ===")
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "focusdb-schemas-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	cat := catalog.New()
	st, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatal(err)
	}
	exe := executor.New(cat, st)

	fmt.Println("\n--- Active schema ---")
	expectTag(ctx, exe, "", "CREATE SCHEMA IF NOT EXISTS tienda", "CREATE SCHEMA")
	expectTag(ctx, exe, "", "CREATE SCHEMA IF NOT EXISTS tienda", "CREATE SCHEMA")
	expectErr(ctx, exe, "", "CREATE SCHEMA tienda", "already exists")
	query(ctx, exe, "", "CREATE TABLE ventas (id INT IDENTITY PRIMARY KEY, categoria TEXT, monto INT)")
	query(ctx, exe, "", "INSERT INTO ventas (categoria, monto) VALUES ('A', 100)")
	query(ctx, exe, "tienda", "CREATE TABLE productos (id INT IDENTITY PRIMARY KEY, nombre TEXT, precio INT)")
	query(ctx, exe, "tienda", "INSERT INTO productos (nombre, precio) VALUES ('lapiz', 5)")
	query(ctx, exe, "tienda", "INSERT INTO productos (nombre, precio) VALUES ('tinta', 20)")
	expectRows(ctx, exe, "tienda", "SELECT nombre FROM productos ORDER BY precio DESC", []string{"tinta", "lapiz"})
	expectErr(ctx, exe, "", "SELECT * FROM productos", "not found")
	expectRows(ctx, exe, "", "SELECT nombre FROM tienda.productos ORDER BY nombre", []string{"lapiz", "tinta"})
	expectRows(ctx, exe, "tienda", "SELECT nombre, ROW_NUMBER() OVER (ORDER BY precio DESC) AS rn FROM productos QUALIFY rn = 1", []string{"tinta|1"})
	expectRows(ctx, exe, "tienda", "WITH c AS (SELECT * FROM productos WHERE precio > 1) SELECT nombre FROM c ORDER BY nombre", []string{"lapiz", "tinta"})

	fmt.Println("\n--- Aliases and qualified references ---")
	expectRows(ctx, exe, "", "SELECT v.monto FROM ventas AS v WHERE v.monto > 1", []string{"100"})
	expectRows(ctx, exe, "", "SELECT v.monto, monto FROM ventas v", []string{"100|100"})
	expectRows(ctx, exe, "tienda", "SELECT p.nombre, p.precio FROM productos p WHERE p.precio > 10", []string{"tinta|20"})
	expectRows(ctx, exe, "", "SELECT p.nombre FROM tienda.productos AS p WHERE p.precio > 10", []string{"tinta"})
	expectRows(ctx, exe, "", "SELECT tienda.productos.nombre FROM tienda.productos WHERE tienda.productos.precio > 10", []string{"tinta"})
	expectRows(ctx, exe, "", "SELECT p.nombre, v.monto FROM tienda.productos AS p INNER JOIN public.ventas AS v ON p.id = v.id", []string{"lapiz|100"})
	expectRows(ctx, exe, "tienda", "SELECT p.nombre, v.monto FROM productos p INNER JOIN public.ventas v ON p.id = v.id", []string{"lapiz|100"})
	expectRows(ctx, exe, "", "SELECT tienda.productos.nombre, ventas.monto FROM tienda.productos INNER JOIN ventas ON tienda.productos.id = ventas.id", []string{"lapiz|100"})
	expectErr(ctx, exe, "", "SELECT x.monto FROM ventas v", "unknown table qualifier x")

	fmt.Println("\n--- Views bound to their schema ---")
	query(ctx, exe, "tienda", "CREATE VIEW caros AS SELECT nombre, precio FROM productos WHERE precio > 10")
	expectRows(ctx, exe, "tienda", "SELECT * FROM caros", []string{"tinta|20"})
	expectRows(ctx, exe, "", "SELECT * FROM tienda.caros", []string{"tinta|20"})
	expectErr(ctx, exe, "", "SELECT * FROM caros", "not found")

	fmt.Println("\n--- Protected schemas / databases ---")
	expectErr(ctx, exe, "", "DROP SCHEMA public", "system schema")
	expectErr(ctx, exe, "", "DROP SCHEMA pg_catalog CASCADE", "system schema")
	expectErr(ctx, exe, "", "DROP SCHEMA tienda", "not empty")
	expectErr(ctx, exe, "", "DROP DATABASE postgres", "default database")

	fmt.Println("\n--- Databases as isolated containers ---")
	cl := cat.Cluster()
	expectTag(ctx, exe, "", "CREATE DATABASE analytics", "CREATE DATABASE")
	expectErr(ctx, exe, "", "CREATE DATABASE analytics", "already exists")
	anCat, ok := cl.Database("analytics")
	if !ok {
		log.Fatal("analytics database missing from cluster")
	}
	anExe := executor.New(anCat, st.ForDatabase("analytics"))
	// Same names, different databases: no interference.
	query(ctx, anExe, "", "CREATE SCHEMA tienda")
	query(ctx, anExe, "tienda", "CREATE TABLE productos (id INT IDENTITY PRIMARY KEY, nombre TEXT, precio INT)")
	query(ctx, anExe, "tienda", "INSERT INTO productos (nombre, precio) VALUES ('cuaderno', 30)")
	expectRows(ctx, anExe, "tienda", "SELECT nombre FROM productos", []string{"cuaderno"})
	expectRows(ctx, exe, "tienda", "SELECT nombre FROM productos ORDER BY nombre", []string{"lapiz", "tinta"})
	query(ctx, anExe, "", "CREATE TABLE hechos (id INT)")
	expectErr(ctx, exe, "", "SELECT * FROM hechos", "not found")
	// Procedures are per database too.
	query(ctx, anExe, "", "CREATE PROCEDURE limpiar() AS BEGIN DELETE FROM hechos WHERE id = 0; END")
	if _, err := cat.GetProcedure("limpiar"); err == nil {
		log.Fatal("procedure of analytics leaked into postgres")
	}
	fmt.Println("OK  procedure limpiar only exists in analytics")
	expectErr(ctx, anExe, "", "DROP DATABASE analytics", "currently open")
	names := []string{}
	for _, d := range cl.ListDatabases() {
		names = append(names, fmt.Sprintf("%s(%d schemas)", d.Name, d.Schemas))
	}
	fmt.Println("OK  ListDatabases ->", names)
	if len(names) != 2 || names[1] != "analytics(2 schemas)" {
		log.Fatalf("unexpected database listing %v", names)
	}
	names = names[:0]
	for _, s := range cat.ListSchemas() {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "public,tienda" {
		log.Fatalf("ListSchemas: expected [public tienda], got %v", names)
	}
	fmt.Println("OK  ListSchemas ->", names)

	fmt.Println("\n--- Persistence across reload ---")
	if err := st.Close(); err != nil {
		log.Fatal(err)
	}
	cat2 := catalog.New()
	st2, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer st2.Close()
	if err := st2.LoadAll(cat2); err != nil {
		log.Fatal(err)
	}
	exe2 := executor.New(cat2, st2)
	// The second database came back with its own schema, table and procedure.
	an2, ok := cat2.Cluster().Database("analytics")
	if !ok {
		log.Fatal("analytics database not reloaded")
	}
	anExe2 := executor.New(an2, st2.ForDatabase("analytics"))
	expectRows(ctx, anExe2, "tienda", "SELECT nombre FROM productos", []string{"cuaderno"})
	if _, err := an2.GetProcedure("limpiar"); err != nil {
		log.Fatalf("procedure not reloaded in analytics: %v", err)
	}
	expectTag(ctx, exe2, "", "DROP DATABASE analytics", "DROP DATABASE")
	if cat2.Cluster().DatabaseExists("analytics") {
		log.Fatal("analytics still exists after DROP DATABASE")
	}
	expectRows(ctx, exe2, "tienda", "SELECT nombre FROM productos ORDER BY nombre", []string{"lapiz", "tinta"})
	expectRows(ctx, exe2, "tienda", "SELECT * FROM caros", []string{"tinta|20"})
	expectRows(ctx, exe2, "", "SELECT * FROM tienda.caros", []string{"tinta|20"})
	names = names[:0]
	for _, s := range cat2.ListSchemas() {
		names = append(names, fmt.Sprintf("%s(%d/%d)", s.Name, s.Tables, s.Views))
	}
	if strings.Join(names, ",") != "public(1/0),tienda(1/1)" {
		log.Fatalf("ListSchemas after reload: got %v", names)
	}
	fmt.Println("OK  ListSchemas after reload ->", names)
	expectTag(ctx, exe2, "", "DROP SCHEMA tienda CASCADE", "DROP SCHEMA")

	fmt.Println("\n=== All schema tests passed! ===")
}
