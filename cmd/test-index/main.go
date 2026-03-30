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
	fmt.Println("=== Testing CREATE INDEX and indexed lookups ===")

	cat := catalog.New()
	st, err := storage.NewPebbleStorage("data_test_index")
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}

	exec := executor.New(cat, st)
	ctx := context.Background()

	execSQL(ctx, exec, "CREATE TABLE users (id INT PRIMARY KEY, email TEXT, age INT)")
	execSQL(ctx, exec, "INSERT INTO users VALUES (1, 'a@x.com', 30)")
	execSQL(ctx, exec, "INSERT INTO users VALUES (2, 'b@x.com', 25)")
	execSQL(ctx, exec, "INSERT INTO users VALUES (3, 'c@x.com', 30)")
	execSQL(ctx, exec, "CREATE INDEX idx_users_age ON users (age)")
	execSQL(ctx, exec, "CREATE INDEX idx_users_email_age ON users (email, age)")

	tbl, err := cat.GetTable("users")
	if err != nil {
		log.Fatalf("failed to get table users: %v", err)
	}
	if _, ok := tbl.Indexes["idx_users_age"]; !ok {
		log.Fatalf("index idx_users_age was not created")
	}
	if _, ok := tbl.Indexes["idx_users_email_age"]; !ok {
		log.Fatalf("composite index idx_users_email_age was not created")
	}
	fmt.Println("OK: index created")

	rows30, err := tbl.SelectWhere("age", "30")
	if err != nil {
		log.Fatalf("select where failed: %v", err)
	}
	if len(rows30) != 2 {
		log.Fatalf("expected 2 rows for age=30, got %d", len(rows30))
	}
	fmt.Println("OK: indexed lookup returned expected rows")

	rowsEmail, err := tbl.SelectWhere("email", "a@x.com")
	if err != nil {
		log.Fatalf("select where by email failed: %v", err)
	}
	if len(rowsEmail) != 1 {
		log.Fatalf("expected 1 row for email=a@x.com, got %d", len(rowsEmail))
	}
	fmt.Println("OK: composite index prefix lookup returned expected row")

	execSQL(ctx, exec, "UPDATE users SET age = 40 WHERE id = 1")
	rows40, err := tbl.SelectWhere("age", "40")
	if err != nil {
		log.Fatalf("select where after update failed: %v", err)
	}
	if len(rows40) != 1 {
		log.Fatalf("expected 1 row for age=40 after update, got %d", len(rows40))
	}
	fmt.Println("OK: index stays consistent after update")

	execSQL(ctx, exec, "DELETE FROM users WHERE id = 2")
	rows25, err := tbl.SelectWhere("age", "25")
	if err != nil {
		log.Fatalf("select where after delete failed: %v", err)
	}
	if len(rows25) != 0 {
		log.Fatalf("expected 0 rows for age=25 after delete, got %d", len(rows25))
	}
	fmt.Println("OK: index stays consistent after delete")

	st.Close()
	st, err = storage.NewPebbleStorage("data_test_index")
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
		log.Fatalf("failed to get users after reload: %v", err)
	}
	if _, ok := tbl2.Indexes["idx_users_age"]; !ok {
		log.Fatalf("index idx_users_age missing after reload")
	}
	if _, ok := tbl2.Indexes["idx_users_email_age"]; !ok {
		log.Fatalf("composite index idx_users_email_age missing after reload")
	}
	rows40Reload, err := tbl2.SelectWhere("age", "40")
	if err != nil {
		log.Fatalf("select where after reload failed: %v", err)
	}
	if len(rows40Reload) != 1 {
		log.Fatalf("expected 1 row for age=40 after reload, got %d", len(rows40Reload))
	}
	rowsEmailReload, err := tbl2.SelectWhere("email", "a@x.com")
	if err != nil {
		log.Fatalf("select where by email after reload failed: %v", err)
	}
	if len(rowsEmailReload) != 1 {
		log.Fatalf("expected 1 row for email=a@x.com after reload, got %d", len(rowsEmailReload))
	}

	fmt.Println("OK: index persisted and reloaded")
	fmt.Println("=== All index tests passed! ===")
}
