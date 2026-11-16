package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
	"dbf/internal/storage"
)

func run(ctx context.Context, exe *executor.Executor, sql string) *executor.Result {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for %q: %v", sql, err)
	}
	res, err := exe.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("exec error for %q: %v", sql, err)
	}
	return res
}

func main() {
	testDir := "./data_function_test"
	defer os.RemoveAll(testDir)

	fmt.Println("=== Testing CREATE FUNCTION (practical subset) ===")
	ctx := context.Background()

	st1, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	cat1 := catalog.New()
	exe := executor.New(cat1, st1)

	run(ctx, exe, "CREATE TABLE cursos (id INT IDENTITY PRIMARY KEY, titulo TEXT, creditos INT)")
	run(ctx, exe, "INSERT INTO cursos (titulo, creditos) VALUES ('Algebra', 4)")
	run(ctx, exe, "INSERT INTO cursos (titulo, creditos) VALUES ('Historia', 3)")
	run(ctx, exe, "INSERT INTO cursos (titulo, creditos) VALUES ('Fisica', 5)")

	// 1) No-arg function, RETURNS TABLE, body uses RETURN QUERY (DBeaver style).
	run(ctx, exe, `CREATE OR REPLACE FUNCTION fn_listar_cursos()
RETURNS TABLE (id INTEGER, titulo VARCHAR, creditos INTEGER)
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    SELECT c.id, c.titulo, c.creditos
    FROM cursos c;
END;
$$`)

	res := run(ctx, exe, "SELECT * FROM fn_listar_cursos()")
	if len(res.Rows) != 3 {
		log.Fatalf("fn_listar_cursos: expected 3 rows, got %d (%+v)", len(res.Rows), res.Rows)
	}
	if len(res.Columns) != 3 || res.Columns[0] != "id" || res.Columns[1] != "titulo" || res.Columns[2] != "creditos" {
		log.Fatalf("fn_listar_cursos: expected RETURNS TABLE column names, got %v", res.Columns)
	}
	fmt.Printf("  ok: fn_listar_cursos() -> %d rows, cols=%v\n", len(res.Rows), res.Columns)

	// 2) Function result composes with the outer query (WHERE / ORDER BY).
	res = run(ctx, exe, "SELECT titulo FROM fn_listar_cursos() WHERE creditos >= 4 ORDER BY titulo")
	if len(res.Rows) != 2 || fmt.Sprintf("%v", res.Rows[0][0]) != "Algebra" || fmt.Sprintf("%v", res.Rows[1][0]) != "Fisica" {
		log.Fatalf("composed query: expected [Algebra Fisica], got %+v", res.Rows)
	}
	fmt.Printf("  ok: WHERE+ORDER BY over function -> %+v\n", res.Rows)

	// 3) Function WITH a parameter (argument bound into the body SELECT).
	run(ctx, exe, `CREATE FUNCTION fn_cursos_por_credito(min_cred INTEGER)
RETURNS TABLE (titulo VARCHAR, creditos INTEGER)
AS $$
BEGIN
    RETURN QUERY
    SELECT c.titulo, c.creditos FROM cursos c WHERE c.creditos >= min_cred;
END;
$$`)
	res = run(ctx, exe, "SELECT * FROM fn_cursos_por_credito(5)")
	if len(res.Rows) != 1 || fmt.Sprintf("%v", res.Rows[0][0]) != "Fisica" {
		log.Fatalf("fn_cursos_por_credito(5): expected [Fisica], got %+v", res.Rows)
	}
	fmt.Printf("  ok: parameterized function fn_cursos_por_credito(5) -> %+v\n", res.Rows)

	// 3b) Hardened binding: a parameter name that collides with a qualified
	// column (c.titulo) and with a string literal ('titulo') must not corrupt
	// them — only the bare parameter reference is bound.
	run(ctx, exe, `CREATE FUNCTION fn_por_titulo(titulo VARCHAR)
RETURNS TABLE (id INTEGER, titulo VARCHAR) AS $$
BEGIN
    RETURN QUERY
    SELECT c.id, c.titulo FROM cursos c WHERE c.titulo = titulo OR c.titulo = 'titulo';
END;
$$`)
	// Only the bare parameter "titulo" is bound to 'Fisica'; the qualified
	// column c.titulo and the string literal 'titulo' are left intact, so the
	// query returns exactly the Fisica row (no course is literally named titulo).
	res = run(ctx, exe, "SELECT * FROM fn_por_titulo('Fisica')")
	if len(res.Rows) != 1 || fmt.Sprintf("%v", res.Rows[0][1]) != "Fisica" {
		log.Fatalf("fn_por_titulo('Fisica'): expected exactly the Fisica row, got %+v", res.Rows)
	}
	fmt.Printf("  ok: param/column/string-literal collision handled -> %+v\n", res.Rows)

	st1.Close()

	// 4) Persistence: reopen, reload, functions survive restart.
	st2, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("reopen storage: %v", err)
	}
	cat2 := catalog.New()
	exe2 := executor.New(cat2, st2)
	if err := st2.LoadAll(cat2); err != nil {
		log.Fatalf("LoadAll: %v", err)
	}
	res = run(ctx, exe2, "SELECT * FROM fn_listar_cursos()")
	if len(res.Rows) != 3 {
		log.Fatalf("after reload: fn_listar_cursos expected 3 rows, got %d", len(res.Rows))
	}
	res = run(ctx, exe2, "SELECT * FROM fn_cursos_por_credito(4)")
	if len(res.Rows) != 2 {
		log.Fatalf("after reload: fn_cursos_por_credito(4) expected 2 rows, got %d", len(res.Rows))
	}
	fmt.Println("  ok: functions survive restart (reloaded from storage)")

	// 5) DROP FUNCTION.
	run(ctx, exe2, "DROP FUNCTION fn_cursos_por_credito")
	if _, err := cat2.GetFunction("fn_cursos_por_credito"); err == nil {
		log.Fatalf("expected fn_cursos_por_credito to be dropped")
	}
	run(ctx, exe2, "DROP FUNCTION IF EXISTS fn_no_existe")
	fmt.Println("  ok: DROP FUNCTION + DROP FUNCTION IF EXISTS")

	st2.Close()
	fmt.Println("=== All function tests passed! ===")
}
