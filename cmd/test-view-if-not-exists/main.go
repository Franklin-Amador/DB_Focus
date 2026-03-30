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
	fmt.Println("=== Testing CREATE VIEW IF NOT EXISTS ===")

	cat := catalog.New()
	exe := executor.New(cat, nil)
	ctx := context.Background()

	if err := execSQL(ctx, exe, "CREATE TABLE users (id INT PRIMARY KEY, name TEXT)"); err != nil {
		log.Fatalf("setup table failed: %v", err)
	}
	if err := execSQL(ctx, exe, "CREATE VIEW v_users AS SELECT id, name FROM users"); err != nil {
		log.Fatalf("setup view failed: %v", err)
	}

	if err := execSQL(ctx, exe, "CREATE VIEW IF NOT EXISTS v_users AS SELECT id FROM users"); err != nil {
		log.Fatalf("CREATE VIEW IF NOT EXISTS should be idempotent: %v", err)
	}
	fmt.Println("OK: existing view does not error with IF NOT EXISTS")

	if err := execSQL(ctx, exe, "CREATE VIEW IF NOT EXISTS v_users_2 AS SELECT id FROM users"); err != nil {
		log.Fatalf("CREATE VIEW IF NOT EXISTS for new view failed: %v", err)
	}
	fmt.Println("OK: new view created with IF NOT EXISTS")

	fmt.Println("=== CREATE VIEW IF NOT EXISTS tests passed ===")
}
