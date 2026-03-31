package main

import (
	"context"
	"fmt"
	"log"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
)

func execSQL(ctx context.Context, exe *executor.Executor, sql string) error {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		return fmt.Errorf("parse error for %q: %w", sql, err)
	}
	_, err = exe.Execute(ctx, stmt)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	fmt.Println("=== Testing IDENTITY + INSERT positional mapping ===")

	cat := catalog.New()
	exe := executor.New(cat, nil)
	ctx := context.Background()

	if err := execSQL(ctx, exe, "CREATE TABLE test (id INT IDENTITY PRIMARY KEY, name TEXT)"); err != nil {
		log.Fatalf("create table failed: %v", err)
	}

	if err := execSQL(ctx, exe, "INSERT INTO test VALUES ('Estiven')"); err != nil {
		log.Fatalf("insert 1 failed: %v", err)
	}
	if err := execSQL(ctx, exe, "INSERT INTO test VALUES ('Oscar')"); err != nil {
		log.Fatalf("insert 2 failed: %v", err)
	}

	p := parser.NewParser("SELECT * FROM test ORDER BY id ASC")
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse select failed: %v", err)
	}
	res, err := exe.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("select failed: %v", err)
	}

	if len(res.Rows) != 2 {
		log.Fatalf("expected 2 rows, got %d", len(res.Rows))
	}
	if len(res.Rows[0]) != 2 || len(res.Rows[1]) != 2 {
		log.Fatalf("expected 2 columns per row, got %+v", res.Rows)
	}
	if res.Rows[0][0] != 1 || res.Rows[0][1] != "Estiven" {
		log.Fatalf("unexpected row 1: %+v", res.Rows[0])
	}
	if res.Rows[1][0] != 2 || res.Rows[1][1] != "Oscar" {
		log.Fatalf("unexpected row 2: %+v", res.Rows[1])
	}

	fmt.Println("OK: identity autoincrement works and non-identity values are persisted")
	fmt.Println("=== IDENTITY insert test passed ===")
}
