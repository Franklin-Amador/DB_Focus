package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

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
	testDir := "./data_test_trigger_recursion"
	defer os.RemoveAll(testDir)

	cat := catalog.New()
	st, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}
	defer st.Close()

	exe := executor.New(cat, st)
	ctx := context.Background()

	fmt.Println("=== Trigger Recursion Test ===")

	mustExec(ctx, exe, "CREATE TABLE events (id INTEGER IDENTITY PRIMARY KEY, msg TEXT)")
	mustExec(ctx, exe, `CREATE TRIGGER recursive_events AFTER INSERT ON events FOR EACH ROW BEGIN
		INSERT INTO events (msg) VALUES ('nested');
	END;`)

	p := parser.NewParser("INSERT INTO events (msg) VALUES ('root')")
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for root insert: %v", err)
	}

	_, err = exe.Execute(ctx, stmt)
	if err == nil {
		log.Fatal("expected recursion depth error, got nil")
	}
	if !strings.Contains(err.Error(), "trigger recursion depth exceeded") {
		log.Fatalf("expected recursion depth error, got: %v", err)
	}
	fmt.Printf("✓ Recursion guard activated as expected: %v\n", err)

	tbl, err := cat.GetTable("events")
	if err != nil {
		log.Fatalf("failed to read events table: %v", err)
	}
	rows := tbl.SelectAll()
	if len(rows) <= 1 {
		log.Fatalf("expected recursive inserts to generate more than one row, got %d", len(rows))
	}
	fmt.Printf("✓ Recursive trigger execution generated %d rows before guard stopped the chain\n", len(rows))

	fmt.Println("=== Trigger recursion behavior verified ===")
}
