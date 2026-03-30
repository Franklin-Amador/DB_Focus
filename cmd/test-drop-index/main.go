package main

import (
	"context"
	"fmt"
	"log"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
	"dbf/internal/storage"
)

func execSQL(ctx context.Context, exec *executor.Executor, sql string) {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for %q: %v", sql, err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		log.Fatalf("execute error for %q: %v", sql, err)
	}
}

func main() {
	fmt.Println("=== Testing DROP INDEX lifecycle ===")

	st, err := storage.NewPebbleStorage("data_test_drop_index")
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}

	cat := catalog.New()
	exec := executor.New(cat, st)
	ctx := context.Background()

	execSQL(ctx, exec, "CREATE TABLE users (id INT PRIMARY KEY, age INT, email TEXT)")
	execSQL(ctx, exec, "INSERT INTO users VALUES (1, 30, 'a@x.com')")
	execSQL(ctx, exec, "INSERT INTO users VALUES (2, 40, 'b@x.com')")
	execSQL(ctx, exec, "CREATE INDEX idx_users_age ON users (age)")

	tbl, err := cat.GetTable("users")
	if err != nil {
		log.Fatalf("failed to get users table: %v", err)
	}
	if _, ok := tbl.Indexes["idx_users_age"]; !ok {
		log.Fatalf("index idx_users_age should exist after CREATE INDEX")
	}
	fmt.Println("OK: index created")

	execSQL(ctx, exec, "DROP INDEX idx_users_age ON users")
	if _, ok := tbl.Indexes["idx_users_age"]; ok {
		log.Fatalf("index idx_users_age should not exist after DROP INDEX")
	}
	fmt.Println("OK: index dropped")

	// Re-open storage and ensure dropped index metadata does not come back.
	if err := st.Close(); err != nil {
		log.Fatalf("failed to close storage: %v", err)
	}
	st, err = storage.NewPebbleStorage("data_test_drop_index")
	if err != nil {
		log.Fatalf("failed to reopen storage: %v", err)
	}
	defer st.Close()

	cat2 := catalog.New()
	if err := st.LoadAll(cat2); err != nil {
		log.Fatalf("failed to load storage: %v", err)
	}

	tbl2, err := cat2.GetTable("users")
	if err != nil {
		log.Fatalf("failed to get users table after reload: %v", err)
	}
	if _, ok := tbl2.Indexes["idx_users_age"]; ok {
		log.Fatalf("index idx_users_age should remain deleted after reload")
	}
	fmt.Println("OK: dropped index state persisted")

	fmt.Println("=== DROP INDEX tests passed ===")
}
