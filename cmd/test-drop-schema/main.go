package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
	"dbf/internal/storage"
)

func execSQL(ctx context.Context, exec *executor.Executor, sql string) error {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		return fmt.Errorf("parse error for %q: %w", sql, err)
	}
	_, err = exec.Execute(ctx, stmt)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	fmt.Println("=== Testing DROP SCHEMA CASCADE/RESTRICT ===")

	st, err := storage.NewPebbleStorage("data_test_drop_schema")
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}
	defer st.Close()

	cat := catalog.New()
	exec := executor.New(cat, st)
	ctx := context.Background()

	if err := execSQL(ctx, exec, "CREATE SCHEMA demo"); err != nil {
		log.Fatalf("setup create schema failed: %v", err)
	}
	if err := execSQL(ctx, exec, "CREATE TABLE demo.users (id INT PRIMARY KEY, name TEXT)"); err != nil {
		log.Fatalf("setup create table failed: %v", err)
	}

	if err := execSQL(ctx, exec, "DROP SCHEMA demo RESTRICT"); err == nil {
		log.Fatalf("expected RESTRICT failure for non-empty schema")
	} else {
		if !strings.Contains(err.Error(), "schema is not empty") {
			log.Fatalf("unexpected RESTRICT error: %v", err)
		}
		fmt.Println("OK: RESTRICT blocks non-empty schema")
	}

	if err := execSQL(ctx, exec, "DROP SCHEMA demo CASCADE"); err != nil {
		log.Fatalf("DROP SCHEMA CASCADE failed: %v", err)
	}
	fmt.Println("OK: CASCADE dropped non-empty schema")

	if err := execSQL(ctx, exec, "DROP SCHEMA IF EXISTS demo CASCADE"); err != nil {
		log.Fatalf("DROP SCHEMA IF EXISTS should be idempotent: %v", err)
	}
	fmt.Println("OK: IF EXISTS is idempotent")

	fmt.Println("=== DROP SCHEMA tests passed ===")
}
