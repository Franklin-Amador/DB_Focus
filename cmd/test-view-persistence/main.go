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
	testDir := "./data_view_persistence"
	defer os.RemoveAll(testDir)

	fmt.Println("=== Testing view persistence via stored SQL text ===")

	ctx := context.Background()

	// Phase 1: create a table + view, then close.
	st1, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	cat1 := catalog.New()
	exe1 := executor.New(cat1, st1)

	run(ctx, exe1, "CREATE TABLE productos (id INT IDENTITY PRIMARY KEY, nombre TEXT, precio INT)")
	run(ctx, exe1, "INSERT INTO productos (nombre, precio) VALUES ('Laptop', 1000)")
	run(ctx, exe1, "INSERT INTO productos (nombre, precio) VALUES ('Mouse', 25)")
	run(ctx, exe1, "INSERT INTO productos (nombre, precio) VALUES ('Monitor', 300)")
	// View definition uses a comparison predicate (exercises re-parse fidelity).
	run(ctx, exe1, "CREATE VIEW v_caros AS SELECT nombre, precio FROM productos WHERE precio > 100")

	res := run(ctx, exe1, "SELECT * FROM v_caros")
	if len(res.Rows) != 2 {
		log.Fatalf("phase 1: expected 2 rows from view, got %d (%+v)", len(res.Rows), res.Rows)
	}
	fmt.Printf("Phase 1: view returns %d rows before restart\n", len(res.Rows))
	st1.Close()

	// Phase 2: reopen, reload from disk (view is re-parsed from stored SQL text).
	st2, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("reopen storage: %v", err)
	}
	cat2 := catalog.New()
	exe2 := executor.New(cat2, st2)
	if err := st2.LoadAll(cat2); err != nil {
		log.Fatalf("LoadAll: %v", err)
	}

	res2 := run(ctx, exe2, "SELECT * FROM v_caros")
	if len(res2.Rows) != 2 {
		log.Fatalf("phase 2: expected 2 rows from reloaded view, got %d (%+v)", len(res2.Rows), res2.Rows)
	}
	fmt.Printf("Phase 2: reloaded view returns %d rows\n", len(res2.Rows))
	st2.Close()

	fmt.Println("=== View persistence test passed ===")
}
