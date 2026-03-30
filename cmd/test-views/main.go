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

func mustExec(ctx context.Context, exe *executor.Executor, sql string) {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for %q: %v", sql, err)
	}
	if _, err := exe.Execute(ctx, stmt); err != nil {
		log.Fatalf("execute error for %q: %v", sql, err)
	}
}

func main() {
	testDir := "./data_test_views"
	defer os.RemoveAll(testDir)

	cat := catalog.New()
	st, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}
	defer st.Close()

	exe := executor.New(cat, st)
	ctx := context.Background()

	fmt.Println("=== Views test ===")
	mustExec(ctx, exe, "CREATE TABLE users (id INT PRIMARY KEY, name TEXT)")
	mustExec(ctx, exe, "INSERT INTO users VALUES (1, 'Ana')")
	mustExec(ctx, exe, "INSERT INTO users VALUES (2, 'Luis')")
	mustExec(ctx, exe, "CREATE VIEW v_users AS SELECT * FROM users")

	p := parser.NewParser("SELECT * FROM v_users")
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for SELECT from view: %v", err)
	}
	res, err := exe.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("failed selecting from view: %v", err)
	}
	if len(res.Rows) != 2 {
		log.Fatalf("expected 2 rows from view, got %d", len(res.Rows))
	}

	p = parser.NewParser("SELECT name FROM v_users WHERE id = 2")
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for filtered SELECT from view: %v", err)
	}
	res, err = exe.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("failed filtered select from view: %v", err)
	}
	if len(res.Rows) != 1 || len(res.Rows[0]) != 1 || res.Rows[0][0] != "Luis" {
		log.Fatalf("expected one row [Luis], got %+v", res.Rows)
	}

	// Replace the view definition and validate updated projection
	mustExec(ctx, exe, "CREATE OR REPLACE VIEW v_users AS SELECT id FROM users")
	p = parser.NewParser("SELECT * FROM v_users")
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for replaced view select: %v", err)
	}
	res, err = exe.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("failed selecting from replaced view: %v", err)
	}
	if len(res.Columns) != 1 || res.Columns[0] != "id" {
		log.Fatalf("expected replaced view columns [id], got %+v", res.Columns)
	}

	// Drop the view and ensure selects fail.
	mustExec(ctx, exe, "DROP VIEW v_users")
	p = parser.NewParser("SELECT * FROM v_users")
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error after drop view: %v", err)
	}
	if _, err := exe.Execute(ctx, stmt); err == nil {
		log.Fatalf("expected error selecting dropped view")
	}

	// IF EXISTS should not fail for a missing view.
	mustExec(ctx, exe, "DROP VIEW IF EXISTS v_users")

	// Duplicate output column names in view definition should fail.
	p = parser.NewParser("CREATE VIEW v_dup AS SELECT id, id FROM users")
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for duplicate-column view: %v", err)
	}
	if _, err := exe.Execute(ctx, stmt); err == nil {
		log.Fatalf("expected duplicate-column CREATE VIEW to fail")
	}

	fmt.Println("=== Views test passed ===")
}
