// test-snowflake-select covers the Snowflake SELECT * conveniences
// (EXCLUDE, RENAME, ILIKE) and GROUP BY ALL.
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

func cols(r *executor.Result) string { return strings.Join(r.Columns, ",") }

func wantCols(r *executor.Result, want string) {
	if got := cols(r); got != want {
		log.Fatalf("columnas: esperaba [%s], got [%s]", want, got)
	}
}

func main() {
	ctx := context.Background()
	cat := catalog.New()
	e := executor.New(cat, nil)

	run(ctx, e, "CREATE TABLE customers (id INT, nombre TEXT, email TEXT, ssn TEXT, loaded_at TEXT)")
	run(ctx, e, "INSERT INTO customers VALUES (1, 'Ana', 'a@x.com', '111', '2026-01-01')")
	run(ctx, e, "INSERT INTO customers VALUES (2, 'Beto', 'b@x.com', '222', '2026-01-02')")

	fmt.Println("=== SELECT * EXCLUDE ===")
	r := run(ctx, e, "SELECT * EXCLUDE (ssn, loaded_at) FROM customers")
	wantCols(r, "id,nombre,email")
	if len(r.Rows) != 2 || len(r.Rows[0]) != 3 {
		log.Fatalf("EXCLUDE: filas/columnas inesperadas: %+v", r.Rows)
	}
	fmt.Printf("OK  * EXCLUDE (ssn, loaded_at) -> [%s]\n", cols(r))

	// EXCLUDE de una sola columna sin paréntesis.
	r = run(ctx, e, "SELECT * EXCLUDE ssn FROM customers")
	wantCols(r, "id,nombre,email,loaded_at")
	fmt.Printf("OK  * EXCLUDE ssn (sin paréntesis) -> [%s]\n", cols(r))

	fmt.Println("=== SELECT * RENAME ===")
	r = run(ctx, e, "SELECT * RENAME (nombre AS name) FROM customers")
	wantCols(r, "id,name,email,ssn,loaded_at")
	fmt.Printf("OK  * RENAME (nombre AS name) -> [%s]\n", cols(r))

	fmt.Println("=== SELECT * EXCLUDE + RENAME combinados ===")
	r = run(ctx, e, "SELECT * EXCLUDE (ssn, loaded_at) RENAME (nombre AS name) FROM customers")
	wantCols(r, "id,name,email")
	fmt.Printf("OK  combinados -> [%s]\n", cols(r))

	fmt.Println("=== SELECT * ILIKE ===")
	run(ctx, e, "CREATE TABLE payments (id INT, gross_amount INT, net_amount INT, tax_amount INT, nota TEXT)")
	run(ctx, e, "INSERT INTO payments VALUES (1, 100, 80, 20, 'x')")
	r = run(ctx, e, "SELECT * ILIKE '%_amount' FROM payments")
	wantCols(r, "gross_amount,net_amount,tax_amount")
	fmt.Printf("OK  * ILIKE '%%_amount' -> [%s]\n", cols(r))

	fmt.Println("=== GROUP BY ALL ===")
	run(ctx, e, "CREATE TABLE productos (id INT, categoria TEXT, precio INT)")
	run(ctx, e, "INSERT INTO productos VALUES (1, 'A', 10), (2, 'A', 20), (3, 'B', 5)")
	r = run(ctx, e, "SELECT categoria, SUM(precio) FROM productos GROUP BY ALL")
	if len(r.Rows) != 2 {
		log.Fatalf("GROUP BY ALL: esperaba 2 grupos, got %d (%+v)", len(r.Rows), r.Rows)
	}
	// Verificá los totales por categoría.
	totals := map[string]string{}
	for _, row := range r.Rows {
		totals[fmt.Sprintf("%v", row[0])] = fmt.Sprintf("%v", row[1])
	}
	if totals["A"] != "30" || totals["B"] != "5" {
		log.Fatalf("GROUP BY ALL totales: %v", totals)
	}
	fmt.Printf("OK  GROUP BY ALL -> A=%s, B=%s\n", totals["A"], totals["B"])

	// GROUP BY ALL con dos columnas no agregadas.
	run(ctx, e, "INSERT INTO productos VALUES (4, 'A', 7)")
	r = run(ctx, e, "SELECT categoria, COUNT(*) FROM productos GROUP BY ALL")
	totals = map[string]string{}
	for _, row := range r.Rows {
		totals[fmt.Sprintf("%v", row[0])] = fmt.Sprintf("%v", row[1])
	}
	if totals["A"] != "3" || totals["B"] != "1" {
		log.Fatalf("GROUP BY ALL COUNT: %v", totals)
	}
	fmt.Printf("OK  GROUP BY ALL + COUNT(*) -> A=%s, B=%s\n", totals["A"], totals["B"])

	fmt.Println("=== Todo OK ===")
}
