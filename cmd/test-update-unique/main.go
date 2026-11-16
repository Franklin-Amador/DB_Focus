// test-update-unique verifies that UPDATE rejects changes that would create a
// duplicate PRIMARY KEY or UNIQUE value (a check INSERT already did but UPDATE
// previously skipped), while still allowing no-op and non-colliding updates.
package main

import (
	"context"
	"fmt"
	"log"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
)

func run(ctx context.Context, e *executor.Executor, sql string) *executor.Result {
	st, err := parser.NewParser(sql).ParseStatement()
	if err != nil {
		log.Fatalf("parse error %q: %v", sql, err)
	}
	r, err := e.Execute(ctx, st)
	if err != nil {
		log.Fatalf("exec error %q: %v", sql, err)
	}
	return r
}

func mustFail(ctx context.Context, e *executor.Executor, sql, label string) {
	st, err := parser.NewParser(sql).ParseStatement()
	if err != nil {
		fmt.Printf("OK  %-48s rechazado al parsear\n", label)
		return
	}
	if _, err := e.Execute(ctx, st); err != nil {
		fmt.Printf("OK  %-48s bloqueado: %v\n", label, err)
		return
	}
	log.Fatalf("FAIL %s: debía fallar y tuvo éxito", label)
}

func cell(ctx context.Context, e *executor.Executor, sql string) string {
	r := run(ctx, e, sql)
	if len(r.Rows) == 0 || len(r.Rows[0]) == 0 {
		log.Fatalf("%q: sin resultado", sql)
	}
	return fmt.Sprintf("%v", r.Rows[0][0])
}

func count(ctx context.Context, e *executor.Executor, sql string) int {
	return len(run(ctx, e, sql).Rows)
}

func main() {
	ctx := context.Background()
	cat := catalog.New()
	e := executor.New(cat, nil)

	fmt.Println("=== UPDATE: unicidad de PRIMARY KEY ===")
	run(ctx, e, "CREATE TABLE t (id INT PRIMARY KEY, nombre TEXT)")
	run(ctx, e, "INSERT INTO t VALUES (1, 'a'), (2, 'b'), (3, 'c')")

	// Colisión con otra fila existente.
	mustFail(ctx, e, "UPDATE t SET id = 2 WHERE id = 1", "UPDATE PK a valor existente")
	if cell(ctx, e, "SELECT nombre FROM t WHERE id = 1") != "a" {
		log.Fatal("la fila 1 no debía cambiar")
	}
	if count(ctx, e, "SELECT * FROM t WHERE id = 2") != 1 {
		log.Fatal("no debía haber dos filas con id = 2")
	}

	// Varias filas al mismo PK.
	mustFail(ctx, e, "UPDATE t SET id = 9 WHERE id < 3", "UPDATE de 2 filas al mismo PK")
	if count(ctx, e, "SELECT * FROM t WHERE id = 9") != 0 {
		log.Fatal("ninguna fila debía quedar en id = 9")
	}

	// Valor nuevo y libre: permitido.
	r := run(ctx, e, "UPDATE t SET id = 5 WHERE id = 1")
	if r.Tag != "UPDATE 1" {
		log.Fatalf("esperaba 'UPDATE 1', got %q", r.Tag)
	}
	if cell(ctx, e, "SELECT nombre FROM t WHERE id = 5") != "a" {
		log.Fatal("id debió cambiar a 5")
	}
	fmt.Println("OK  PK: colisión bloqueada, valor libre permitido")

	// No-op: poner el mismo valor que ya tiene.
	run(ctx, e, "UPDATE t SET id = 5 WHERE id = 5")
	fmt.Println("OK  PK: no-op (mismo valor) permitido")

	fmt.Println("=== UPDATE: unicidad de columna UNIQUE ===")
	run(ctx, e, "CREATE TABLE u (id INT PRIMARY KEY, email TEXT UNIQUE)")
	run(ctx, e, "INSERT INTO u VALUES (1, 'a@x.com'), (2, 'b@x.com')")
	mustFail(ctx, e, "UPDATE u SET email = 'b@x.com' WHERE id = 1", "UPDATE UNIQUE a valor existente")
	if cell(ctx, e, "SELECT email FROM u WHERE id = 1") != "a@x.com" {
		log.Fatal("email no debía cambiar")
	}
	run(ctx, e, "UPDATE u SET email = 'c@x.com' WHERE id = 1")
	if cell(ctx, e, "SELECT email FROM u WHERE id = 1") != "c@x.com" {
		log.Fatal("email debió cambiar a c@x.com")
	}
	fmt.Println("OK  UNIQUE: colisión bloqueada, valor libre permitido")

	fmt.Println("=== UPDATE: columna sin constraint no se ve afectada ===")
	run(ctx, e, "UPDATE t SET nombre = 'z' WHERE id = 5")
	if cell(ctx, e, "SELECT nombre FROM t WHERE id = 5") != "z" {
		log.Fatal("UPDATE de columna normal debió funcionar")
	}
	fmt.Println("OK  columna normal sin cambios de comportamiento")

	fmt.Println("=== Todo OK ===")
}
