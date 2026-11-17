// Command test-trigger-function exercises PostgreSQL/DBeaver trigger functions:
// CREATE FUNCTION ... RETURNS trigger with a statement body (INSERT + RETURN
// NEW), fired through CREATE TRIGGER ... EXECUTE FUNCTION fn(), plus a scalar
// function body (RETURN expr) that parses, and a persistence round-trip.
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

func run(ctx context.Context, e *executor.Executor, sql string) *executor.Result {
	stmt, err := parser.NewParser(sql).ParseStatement()
	if err != nil {
		log.Fatalf("parse error for %q: %v", sql, err)
	}
	res, err := e.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("exec error for %q: %v", sql, err)
	}
	return res
}

func cnt(ctx context.Context, e *executor.Executor, where string) string {
	return fmt.Sprintf("%v", run(ctx, e, "SELECT COUNT(*) FROM audit WHERE "+where).Rows[0][0])
}

const triggerFn = `CREATE FUNCTION audit_fn() RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    INSERT INTO audit (origen, valor) VALUES ('ventas', NEW.monto);
    RETURN NEW;
END;
$function$`

func main() {
	ctx := context.Background()
	dir := "./data_trigger_function_test"
	defer os.RemoveAll(dir)

	st, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	cat := catalog.New()
	e := executor.New(cat, st)

	run(ctx, e, "CREATE TABLE ventas (id INT IDENTITY PRIMARY KEY, monto INT)")
	run(ctx, e, "CREATE TABLE audit (id INT IDENTITY PRIMARY KEY, origen TEXT, valor INT)")

	fmt.Println("=== Trigger function (RETURNS trigger, RETURN NEW) ===")
	run(ctx, e, triggerFn)
	run(ctx, e, "CREATE TRIGGER trg AFTER INSERT ON ventas FOR EACH ROW EXECUTE FUNCTION audit_fn()")
	run(ctx, e, "INSERT INTO ventas (monto) VALUES (500)")
	res := run(ctx, e, "SELECT origen, valor FROM audit")
	if len(res.Rows) != 1 || fmt.Sprintf("%v", res.Rows[0][0]) != "ventas" || fmt.Sprintf("%v", res.Rows[0][1]) != "500" {
		log.Fatalf("trigger function: expected [ventas 500], got %+v", res.Rows)
	}
	fmt.Println("  ok: EXECUTE FUNCTION ran the trigger function body with NEW binding")

	fmt.Println("=== Scalar function body (RETURN expr) parses and stores ===")
	run(ctx, e, "CREATE FUNCTION inc(x INT) RETURNS INT LANGUAGE plpgsql AS $$ BEGIN RETURN x + 1; END; $$")
	if _, err := cat.GetFunction("inc"); err != nil {
		log.Fatalf("scalar function not stored: %v", err)
	}
	fmt.Println("  ok: scalar function stored")

	fmt.Println("=== Set-returning function still works (regression) ===")
	run(ctx, e, "CREATE TABLE cursos (id INT IDENTITY PRIMARY KEY, titulo TEXT)")
	run(ctx, e, "INSERT INTO cursos (titulo) VALUES ('A'),('B')")
	run(ctx, e, "CREATE FUNCTION listar() RETURNS TABLE (id INTEGER, titulo VARCHAR) AS $$ BEGIN RETURN QUERY SELECT c.id, c.titulo FROM cursos c; END; $$")
	if n := len(run(ctx, e, "SELECT * FROM listar()").Rows); n != 2 {
		log.Fatalf("set-returning function: expected 2 rows, got %d", n)
	}
	fmt.Println("  ok: set-returning function returns rows")

	st.Close()

	fmt.Println("=== Persistence round-trip (trigger function survives reload) ===")
	st2, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatalf("reopen storage: %v", err)
	}
	cat2 := catalog.New()
	e2 := executor.New(cat2, st2)
	if err := st2.LoadAll(cat2); err != nil {
		log.Fatalf("LoadAll: %v", err)
	}
	before := cnt(ctx, e2, "valor = 900")
	run(ctx, e2, "INSERT INTO ventas (monto) VALUES (900)")
	after := cnt(ctx, e2, "valor = 900")
	if before != "0" || after != "1" {
		log.Fatalf("after reload: trigger function did not fire (before=%s after=%s)", before, after)
	}
	// The set-returning function also survives.
	if n := len(run(ctx, e2, "SELECT * FROM listar()").Rows); n != 2 {
		log.Fatalf("after reload: set-returning function expected 2 rows, got %d", n)
	}
	st2.Close()
	fmt.Println("  ok: trigger function and set-returning function survive restart")

	fmt.Println("=== All trigger-function tests passed! ===")
}
