package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
)

// Regression test for the "FK referencing an IDENTITY primary key" bug.
//
// Root cause: the engine stores cell values with heterogeneous Go types —
// SQL literals as string, IDENTITY-generated keys as int. The FK check used a
// type-sensitive `==`, so a literal child value ("1") never matched an
// IDENTITY-generated parent key (1). Fix: compare by canonical form via
// catalog.ValuesEqual (same semantics as JOIN ON).

func run(ctx context.Context, exe *executor.Executor, sql string) {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for %q: %v", sql, err)
	}
	if _, err := exe.Execute(ctx, stmt); err != nil {
		log.Fatalf("exec error for %q: %v", sql, err)
	}
}

func expectErr(ctx context.Context, exe *executor.Executor, sql, want string) {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err == nil {
		_, err = exe.Execute(ctx, stmt)
	}
	if err == nil {
		log.Fatalf("%q: expected error containing %q, got success", sql, want)
	}
	if !strings.Contains(err.Error(), want) {
		log.Fatalf("%q: expected error containing %q, got %v", sql, want, err)
	}
	fmt.Printf("OK  (rechazado) %-50s -> %s\n", trunc(sql, 50), want)
}

func rowCount(ctx context.Context, exe *executor.Executor, sql string) int {
	p := parser.NewParser(sql)
	stmt, _ := p.ParseStatement()
	res, err := exe.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("exec error for %q: %v", sql, err)
	}
	return len(res.Rows)
}

func expectRows(ctx context.Context, exe *executor.Executor, sql string, want int) {
	got := rowCount(ctx, exe, sql)
	if got != want {
		log.Fatalf("%q: expected %d rows, got %d", sql, want, got)
	}
	fmt.Printf("OK  %-60s -> %d rows\n", trunc(sql, 60), got)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func main() {
	fmt.Println("=== Testing FOREIGN KEY referencing an IDENTITY primary key ===")

	cat := catalog.New()
	exe := executor.New(cat, nil)
	ctx := context.Background()

	// ── Exact repro from the bug report: both tables use IDENTITY PKs ──
	run(ctx, exe, "CREATE TABLE users (id INTEGER IDENTITY PRIMARY KEY, name TEXT, email TEXT)")
	run(ctx, exe, "CREATE TABLE orders (id INTEGER IDENTITY PRIMARY KEY, user_id INTEGER, product TEXT, total INTEGER, FOREIGN KEY (user_id) REFERENCES users(id))")

	run(ctx, exe, "INSERT INTO users (name, email) VALUES ('Estiven', 'a@a.com')") // id=1
	run(ctx, exe, "INSERT INTO users (name, email) VALUES ('Ana', 'b@b.com')")     // id=2
	run(ctx, exe, "INSERT INTO users (name, email) VALUES ('Bob', 'c@c.com')")     // id=3
	expectRows(ctx, exe, "SELECT * FROM users", 3)

	// Valid child inserts against IDENTITY parent keys — these used to fail with
	// "foreign key violation: value N not found in users(id)".
	run(ctx, exe, "INSERT INTO orders (user_id, product, total) VALUES (1, 'laptop', 1200)")
	run(ctx, exe, "INSERT INTO orders (user_id, product, total) VALUES (2, 'mouse', 25)")
	run(ctx, exe, "INSERT INTO orders (user_id, product, total) VALUES (3, 'teclado', 50)")
	run(ctx, exe, "INSERT INTO orders (user_id, product, total) VALUES (1, 'monitor', 300)")
	expectRows(ctx, exe, "SELECT * FROM orders", 4)

	// Orphan child insert must still be rejected.
	expectErr(ctx, exe, "INSERT INTO orders (user_id, product, total) VALUES (99, 'ghost', 1)", "foreign key violation")

	// UPDATE path FK validation (executor.validateForeignKey): retarget to an
	// existing IDENTITY key succeeds; to a missing one fails.
	run(ctx, exe, "UPDATE orders SET user_id = 3 WHERE product = 'laptop'")
	expectErr(ctx, exe, "UPDATE orders SET user_id = 77 WHERE product = 'laptop'", "foreign key violation")

	// ── Control: FK against a NON-identity (explicit) PK still works ──
	run(ctx, exe, "CREATE TABLE cat (id INTEGER PRIMARY KEY, nombre TEXT)")
	run(ctx, exe, "CREATE TABLE item (id INTEGER PRIMARY KEY, cat_id INTEGER, FOREIGN KEY (cat_id) REFERENCES cat(id))")
	run(ctx, exe, "INSERT INTO cat VALUES (1, 'A')")
	run(ctx, exe, "INSERT INTO item VALUES (1, 1)")
	expectErr(ctx, exe, "INSERT INTO item VALUES (2, 5)", "foreign key violation")
	expectRows(ctx, exe, "SELECT * FROM item", 1)

	fmt.Println("=== FK-vs-IDENTITY test passed ===")
}
