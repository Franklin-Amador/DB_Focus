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
	st, err := parser.NewParser(sql).ParseStatement()
	if err != nil {
		log.Fatalf("parse error for %q: %v", sql, err)
	}
	r, err := e.Execute(ctx, st)
	if err != nil {
		log.Fatalf("exec error for %q: %v", sql, err)
	}
	return r
}

func count(ctx context.Context, e *executor.Executor, table string) int {
	r := run(ctx, e, "SELECT * FROM "+table)
	if r == nil {
		return 0
	}
	return len(r.Rows)
}

func main() {
	dir := "./data_routine_persistence"
	defer os.RemoveAll(dir)
	fmt.Println("=== Testing routine (proc/trigger) persistence via body text ===")
	ctx := context.Background()

	// Phase 1: create tables, a trigger and a procedure with multi-statement
	// bodies, then close.
	st1, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	e1 := executor.New(catalog.New(), st1)
	run(ctx, e1, "CREATE TABLE productos (id INT IDENTITY PRIMARY KEY, nombre TEXT)")
	run(ctx, e1, "CREATE TABLE auditoria (id INT IDENTITY PRIMARY KEY, accion TEXT)")
	run(ctx, e1, "CREATE TRIGGER log_insert AFTER INSERT ON productos FOR EACH ROW BEGIN\n  INSERT INTO auditoria (accion) VALUES ('INSERT');\nEND")
	run(ctx, e1, "CREATE PROCEDURE add_prod(nom TEXT) AS BEGIN\n  INSERT INTO productos (nombre) VALUES (nom);\nEND")
	st1.Close()

	// Phase 2: reopen and reload — proc/trigger bodies come from stored SQL text.
	st2, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatalf("reopen: %v", err)
	}
	cat2 := catalog.New()
	e2 := executor.New(cat2, st2)
	if err := st2.LoadAll(cat2); err != nil {
		log.Fatalf("LoadAll: %v", err)
	}

	// Calling the reloaded procedure must insert a product (proves its body
	// re-parsed and executes), which in turn fires the reloaded trigger.
	run(ctx, e2, "CALL add_prod('Laptop')")
	if got := count(ctx, e2, "productos"); got != 1 {
		log.Fatalf("expected 1 product after CALL, got %d", got)
	}
	if got := count(ctx, e2, "auditoria"); got != 1 {
		log.Fatalf("expected 1 audit row from reloaded trigger, got %d", got)
	}
	fmt.Println("OK  reloaded procedure inserted product (body re-parsed)")
	fmt.Println("OK  reloaded trigger fired on insert (body re-parsed)")

	// A direct insert should fire the trigger again.
	run(ctx, e2, "INSERT INTO productos (nombre) VALUES ('Mouse')")
	if got := count(ctx, e2, "auditoria"); got != 2 {
		log.Fatalf("expected 2 audit rows after direct insert, got %d", got)
	}
	fmt.Println("OK  reloaded trigger fires on subsequent inserts")
	st2.Close()

	fmt.Println("=== Routine persistence test passed ===")
}
