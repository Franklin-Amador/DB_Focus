// Command test-constraints exercises the DDL/DML compatibility fixes: CHECK
// constraints, column DEFAULT values (literal and CURRENT_TIMESTAMP), named
// table-level CONSTRAINT, NULL literals in INSERT, UNIQUE INDEX, INDEX DESC and
// multi-column UPDATE — including a persistence round-trip.
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
	stmt, err := parser.NewParser(sql).ParseStatement()
	if err != nil {
		log.Fatalf("parse error for %q: %v", sql, err)
	}
	res, err := e.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("exec error for %q: %v", sql, err)
	}
	return res
}

func mustFail(ctx context.Context, e *executor.Executor, sql, why string) {
	stmt, err := parser.NewParser(sql).ParseStatement()
	if err != nil {
		return // a parse error also satisfies "must fail"
	}
	if _, err := e.Execute(ctx, stmt); err == nil {
		log.Fatalf("expected %q to fail (%s), but it succeeded", sql, why)
	}
}

func cell(res *executor.Result, row, col int) string {
	return fmt.Sprintf("%v", res.Rows[row][col])
}

func main() {
	ctx := context.Background()
	dir := "./data_constraints_test"
	defer os.RemoveAll(dir)

	st, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	cat := catalog.New()
	e := executor.New(cat, st)

	fmt.Println("=== CHECK constraints ===")
	run(ctx, e, "CREATE TABLE personas (id INT IDENTITY PRIMARY KEY, edad INTEGER CHECK (edad >= 18))")
	mustFail(ctx, e, "INSERT INTO personas (edad) VALUES (15)", "edad < 18")
	run(ctx, e, "INSERT INTO personas (edad) VALUES (25)")
	// Match by value, not id: the rejected insert above still advanced the
	// IDENTITY counter (gaps on failed inserts are expected, as in PostgreSQL).
	mustFail(ctx, e, "UPDATE personas SET edad = 10 WHERE edad = 25", "update below CHECK")
	fmt.Println("  ok: column CHECK enforced on INSERT and UPDATE")

	// Named table-level CHECK.
	run(ctx, e, "CREATE TABLE productos (id INT IDENTITY PRIMARY KEY, precio INT, CONSTRAINT chk_precio CHECK (precio > 0))")
	mustFail(ctx, e, "INSERT INTO productos (precio) VALUES (0)", "precio <= 0")
	run(ctx, e, "INSERT INTO productos (precio) VALUES (50)")
	fmt.Println("  ok: named table-level CHECK enforced")

	fmt.Println("=== Named CONSTRAINT FOREIGN KEY ===")
	run(ctx, e, "CREATE TABLE padre (id INT IDENTITY PRIMARY KEY, nombre TEXT)")
	run(ctx, e, "CREATE TABLE hijo (id INT IDENTITY PRIMARY KEY, padre_id INT, CONSTRAINT fk_padre FOREIGN KEY (padre_id) REFERENCES padre(id))")
	run(ctx, e, "INSERT INTO padre (nombre) VALUES ('A')")
	run(ctx, e, "INSERT INTO hijo (padre_id) VALUES (1)")
	mustFail(ctx, e, "INSERT INTO hijo (padre_id) VALUES (99)", "orphan FK")
	fmt.Println("  ok: named CONSTRAINT FK parsed and enforced")

	fmt.Println("=== DEFAULT values ===")
	run(ctx, e, "CREATE TABLE con_default (id INT IDENTITY PRIMARY KEY, estado TEXT DEFAULT 'activo', creado TIMESTAMP DEFAULT CURRENT_TIMESTAMP)")
	run(ctx, e, "INSERT INTO con_default (id) VALUES (1)")
	res := run(ctx, e, "SELECT estado, creado FROM con_default")
	if cell(res, 0, 0) != "activo" {
		log.Fatalf("DEFAULT literal: expected 'activo', got %q", cell(res, 0, 0))
	}
	if cell(res, 0, 1) == "<nil>" || cell(res, 0, 1) == "" {
		log.Fatalf("DEFAULT CURRENT_TIMESTAMP: expected a timestamp, got %q", cell(res, 0, 1))
	}
	fmt.Printf("  ok: DEFAULT literal=%q, CURRENT_TIMESTAMP=%q\n", cell(res, 0, 0), cell(res, 0, 1))

	fmt.Println("=== NULL literal in INSERT ===")
	run(ctx, e, "CREATE TABLE nullable (id INT IDENTITY PRIMARY KEY, nota TEXT)")
	run(ctx, e, "INSERT INTO nullable (nota) VALUES (NULL)")
	res = run(ctx, e, "SELECT nota FROM nullable")
	if res.Rows[0][0] != nil {
		log.Fatalf("NULL literal: expected nil cell, got %v (%T)", res.Rows[0][0], res.Rows[0][0])
	}
	fmt.Println("  ok: NULL literal stored as NULL")

	fmt.Println("=== UNIQUE INDEX ===")
	run(ctx, e, "CREATE TABLE usuarios (id INT IDENTITY PRIMARY KEY, email TEXT)")
	run(ctx, e, "CREATE UNIQUE INDEX ix_email ON usuarios(email)")
	run(ctx, e, "INSERT INTO usuarios (email) VALUES ('a@x.com')")
	mustFail(ctx, e, "INSERT INTO usuarios (email) VALUES ('a@x.com')", "duplicate unique index")
	run(ctx, e, "INSERT INTO usuarios (email) VALUES ('b@x.com')")
	// UPDATE must also respect the unique index, not only INSERT.
	mustFail(ctx, e, "UPDATE usuarios SET email = 'a@x.com' WHERE email = 'b@x.com'", "update into duplicate unique index key")
	run(ctx, e, "UPDATE usuarios SET email = 'c@x.com' WHERE email = 'b@x.com'") // free value is allowed
	fmt.Println("  ok: UNIQUE INDEX rejects duplicates on INSERT and UPDATE")

	fmt.Println("=== INDEX DESC ===")
	run(ctx, e, "CREATE TABLE ordenado (id INT IDENTITY PRIMARY KEY, n INT)")
	run(ctx, e, "CREATE INDEX ix_n ON ordenado(n DESC)")
	run(ctx, e, "INSERT INTO ordenado (n) VALUES (5),(10),(1)")
	res = run(ctx, e, "SELECT n FROM ordenado WHERE n = 10")
	if len(res.Rows) != 1 {
		log.Fatalf("INDEX DESC lookup: expected 1 row, got %d", len(res.Rows))
	}
	fmt.Println("  ok: INDEX with DESC direction accepted and usable")

	fmt.Println("=== Multi-column UPDATE ===")
	run(ctx, e, "CREATE TABLE clientes (id INT IDENTITY PRIMARY KEY, nombre TEXT, email TEXT)")
	run(ctx, e, "INSERT INTO clientes (nombre, email) VALUES ('Viejo', 'viejo@x.com')")
	run(ctx, e, "UPDATE clientes SET nombre = 'Nuevo', email = 'nuevo@x.com' WHERE id = 1")
	res = run(ctx, e, "SELECT nombre, email FROM clientes")
	if cell(res, 0, 0) != "Nuevo" || cell(res, 0, 1) != "nuevo@x.com" {
		log.Fatalf("multi-column UPDATE: expected [Nuevo nuevo@x.com], got [%s %s]", cell(res, 0, 0), cell(res, 0, 1))
	}
	fmt.Println("  ok: UPDATE SET a=..,b=.. updates every column")

	st.Close()

	fmt.Println("=== Persistence round-trip ===")
	st2, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatalf("reopen storage: %v", err)
	}
	cat2 := catalog.New()
	e2 := executor.New(cat2, st2)
	if err := st2.LoadAll(cat2); err != nil {
		log.Fatalf("LoadAll: %v", err)
	}
	// CHECK survives restart.
	mustFail(ctx, e2, "INSERT INTO personas (edad) VALUES (5)", "CHECK after reload")
	// DEFAULT survives restart.
	run(ctx, e2, "INSERT INTO con_default (id) VALUES (2)")
	res = run(ctx, e2, "SELECT estado FROM con_default WHERE id = 2")
	if cell(res, 0, 0) != "activo" {
		log.Fatalf("DEFAULT after reload: expected 'activo', got %q", cell(res, 0, 0))
	}
	// UNIQUE INDEX survives restart (a@x.com still exists after the updates above).
	mustFail(ctx, e2, "INSERT INTO usuarios (email) VALUES ('a@x.com')", "unique index after reload")
	st2.Close()
	fmt.Println("  ok: CHECK, DEFAULT and UNIQUE INDEX survive restart")

	fmt.Println("=== All constraint/default tests passed! ===")
}
