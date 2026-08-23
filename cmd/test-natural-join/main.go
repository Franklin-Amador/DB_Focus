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

func run(ctx context.Context, exe *executor.Executor, sql string) *executor.Result {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for %q: %v", sql, err)
	}
	res, err := exe.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("exec error for %q: %v", sql, err)
	}
	return res
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
	fmt.Printf("OK  (rechazado) %-46s -> %s\n", trunc(sql, 46), want)
}

func expectRowsCols(ctx context.Context, exe *executor.Executor, sql string, wantRows, wantCols int) *executor.Result {
	res := run(ctx, exe, sql)
	if wantRows >= 0 && len(res.Rows) != wantRows {
		log.Fatalf("%q: expected %d rows, got %d (%+v)", sql, wantRows, len(res.Rows), res.Rows)
	}
	if wantCols >= 0 && len(res.Columns) != wantCols {
		log.Fatalf("%q: expected %d cols, got %d (%v)", sql, wantCols, len(res.Columns), res.Columns)
	}
	fmt.Printf("OK  %-58s -> %d rows, %d cols\n", trunc(sql, 58), len(res.Rows), len(res.Columns))
	return res
}

func expectCell(ctx context.Context, exe *executor.Executor, sql string, row, col int, want string) {
	res := run(ctx, exe, sql)
	if row >= len(res.Rows) || col >= len(res.Rows[row]) {
		log.Fatalf("%q: cell [%d][%d] out of range (%d rows)", sql, row, col, len(res.Rows))
	}
	got := "NULL"
	if v := res.Rows[row][col]; v != nil {
		got = fmt.Sprintf("%v", v)
	}
	if got != want {
		log.Fatalf("%q: cell [%d][%d] = %q, want %q", sql, row, col, got, want)
	}
	fmt.Printf("OK  %-58s [%d][%d]=%s\n", trunc(sql, 58), row, col, want)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func main() {
	fmt.Println("=== Testing NATURAL JOIN and JOIN ... USING ===")

	cat := catalog.New()
	exe := executor.New(cat, nil)
	ctx := context.Background()

	// empleados.depto_id <-> departamentos.depto_id is the common column.
	run(ctx, exe, "CREATE TABLE empleados (id INTEGER PRIMARY KEY, nombre TEXT, depto_id INTEGER)")
	run(ctx, exe, "CREATE TABLE departamentos (depto_id INTEGER PRIMARY KEY, depto_nombre TEXT)")
	run(ctx, exe, "INSERT INTO empleados VALUES (1, 'Ana', 10)")
	run(ctx, exe, "INSERT INTO empleados VALUES (2, 'Bob', 20)")
	run(ctx, exe, "INSERT INTO empleados VALUES (3, 'Cy', 10)")
	run(ctx, exe, "INSERT INTO empleados VALUES (4, 'Dan', 99)") // depto inexistente
	run(ctx, exe, "INSERT INTO departamentos VALUES (10, 'Ventas')")
	run(ctx, exe, "INSERT INTO departamentos VALUES (20, 'IT')")
	run(ctx, exe, "INSERT INTO departamentos VALUES (30, 'RH')") // sin empleados

	// NATURAL INNER JOIN: une por depto_id; la columna común aparece UNA sola vez.
	// empleados(3 cols) + departamentos(2 cols) - 1 duplicada = 4 columnas.
	expectRowsCols(ctx, exe, "SELECT * FROM empleados NATURAL JOIN departamentos", 3, 4)

	// La columna coalescida se referencia sin calificar.
	expectCell(ctx, exe,
		"SELECT nombre, depto_id, depto_nombre FROM empleados NATURAL JOIN departamentos ORDER BY nombre", 0, 2, "Ventas") // Ana -> Ventas

	// NATURAL LEFT JOIN: conserva a Dan (depto 99) con depto_nombre NULL y depto_id=99.
	res := expectRowsCols(ctx, exe, "SELECT nombre, depto_id, depto_nombre FROM empleados NATURAL LEFT JOIN departamentos ORDER BY nombre", 4, 3)
	_ = res
	expectCell(ctx, exe, "SELECT nombre, depto_id, depto_nombre FROM empleados NATURAL LEFT JOIN departamentos ORDER BY nombre", 3, 2, "NULL") // Dan -> NULL
	expectCell(ctx, exe, "SELECT nombre, depto_id, depto_nombre FROM empleados NATURAL LEFT JOIN departamentos ORDER BY nombre", 3, 1, "99")   // Dan depto_id=99 (del lado izq)

	// NATURAL RIGHT JOIN: conserva RH (sin empleados); depto_id se coalesce DESDE la derecha (30).
	expectCell(ctx, exe, "SELECT nombre, depto_id, depto_nombre FROM empleados NATURAL RIGHT JOIN departamentos ORDER BY depto_id", 3, 1, "30")   // RH
	expectCell(ctx, exe, "SELECT nombre, depto_id, depto_nombre FROM empleados NATURAL RIGHT JOIN departamentos ORDER BY depto_id", 3, 0, "NULL") // sin empleado

	// JOIN ... USING (depto_id): equivalente explícito del NATURAL de arriba.
	expectRowsCols(ctx, exe, "SELECT * FROM empleados JOIN departamentos USING (depto_id)", 3, 4)
	// Orden por nombre: Ana(Ventas), Bob(IT), Cy(Ventas).
	expectCell(ctx, exe,
		"SELECT nombre, depto_nombre FROM empleados INNER JOIN departamentos USING (depto_id) ORDER BY nombre", 2, 1, "Ventas") // Cy -> Ventas

	// USING con múltiples columnas.
	run(ctx, exe, "CREATE TABLE a (k1 INTEGER, k2 INTEGER, va TEXT)")
	run(ctx, exe, "CREATE TABLE b (k1 INTEGER, k2 INTEGER, vb TEXT)")
	run(ctx, exe, "INSERT INTO a VALUES (1, 1, 'a11')")
	run(ctx, exe, "INSERT INTO a VALUES (1, 2, 'a12')")
	run(ctx, exe, "INSERT INTO b VALUES (1, 1, 'b11')")
	run(ctx, exe, "INSERT INTO b VALUES (1, 2, 'b12')")
	// Une por (k1,k2): 2 filas; columnas = k1,k2,va,vb = 4 (k1,k2 una sola vez).
	expectRowsCols(ctx, exe, "SELECT * FROM a JOIN b USING (k1, k2)", 2, 4)

	// USING con columna inexistente -> error claro.
	expectErr(ctx, exe, "SELECT * FROM a JOIN b USING (nope)", "USING column nope not found")

	// NATURAL sin columnas comunes -> se comporta como CROSS (producto cartesiano).
	run(ctx, exe, "CREATE TABLE x (xa TEXT)")
	run(ctx, exe, "CREATE TABLE y (yb TEXT)")
	run(ctx, exe, "INSERT INTO x VALUES ('x1')")
	run(ctx, exe, "INSERT INTO x VALUES ('x2')")
	run(ctx, exe, "INSERT INTO y VALUES ('y1')")
	expectRowsCols(ctx, exe, "SELECT * FROM x NATURAL JOIN y", 2, 2)

	// NATURAL encadenado con un tercer join explícito. Tras el NATURAL, la columna
	// común sobrevive con el ref del lado izquierdo (empleados.depto_id); se usa esa.
	expectRowsCols(ctx, exe,
		"SELECT empleados.nombre, departamentos.depto_nombre FROM empleados NATURAL JOIN departamentos INNER JOIN empleados AS e2 ON empleados.depto_id = e2.depto_id", -1, -1)

	fmt.Println("=== NATURAL JOIN / USING test passed ===")
}
