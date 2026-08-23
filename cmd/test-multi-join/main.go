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

// expectErr asserts the statement fails with an error containing want.
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
	fmt.Printf("OK  (rechazado) %-52s -> %s\n", trunc(sql, 52), want)
}

func expectRows(ctx context.Context, exe *executor.Executor, sql string, want int) *executor.Result {
	res := run(ctx, exe, sql)
	if len(res.Rows) != want {
		log.Fatalf("%q: expected %d rows, got %d (%+v)", sql, want, len(res.Rows), res.Rows)
	}
	fmt.Printf("OK  %-64s -> %d rows\n", trunc(sql, 64), len(res.Rows))
	return res
}

// expectCell asserts a single value at [row][col] equals want (string form).
func expectCell(ctx context.Context, exe *executor.Executor, sql string, row, col int, want string) {
	res := run(ctx, exe, sql)
	if row >= len(res.Rows) || col >= len(res.Rows[row]) {
		log.Fatalf("%q: cell [%d][%d] out of range (%d rows)", sql, row, col, len(res.Rows))
	}
	got := fmt.Sprintf("%v", res.Rows[row][col])
	if got != want {
		log.Fatalf("%q: cell [%d][%d] = %q, want %q", sql, row, col, got, want)
	}
	fmt.Printf("OK  %-64s [%d][%d]=%s\n", trunc(sql, 64), row, col, want)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func main() {
	fmt.Println("=== Testing FK vs IDENTITY + N-way JOINs ===")

	cat := catalog.New()
	exe := executor.New(cat, nil)
	ctx := context.Background()

	// ── FIX 1: foreign key referencing an IDENTITY primary key ──────────────
	// Parent id is auto-generated (stored as int); child FK value is a literal
	// (stored as string). The FK check must treat 1 == "1" as a match.
	run(ctx, exe, "CREATE TABLE users (id INTEGER IDENTITY PRIMARY KEY, name TEXT)")
	run(ctx, exe, "CREATE TABLE orders (id INTEGER IDENTITY PRIMARY KEY, user_id INTEGER, product TEXT, FOREIGN KEY (user_id) REFERENCES users(id))")
	run(ctx, exe, "INSERT INTO users (name) VALUES ('Estiven')") // id=1
	run(ctx, exe, "INSERT INTO users (name) VALUES ('Ana')")     // id=2
	// These used to fail with "foreign key violation: value 1 not found in users(id)".
	run(ctx, exe, "INSERT INTO orders (user_id, product) VALUES (1, 'laptop')")
	run(ctx, exe, "INSERT INTO orders (user_id, product) VALUES (1, 'teclado')")
	run(ctx, exe, "INSERT INTO orders (user_id, product) VALUES (2, 'mouse')")
	// Orphan still rejected.
	expectErr(ctx, exe, "INSERT INTO orders (user_id, product) VALUES (99, 'ghost')", "foreign key violation")

	// A 2-table join across the IDENTITY key must now return the child rows.
	expectRows(ctx, exe, "SELECT users.name, orders.product FROM orders INNER JOIN users ON orders.user_id = users.id", 3)

	// ── FIX 2: N-way (3+ table) chained joins ───────────────────────────────
	run(ctx, exe, "CREATE TABLE estudiantes (id INTEGER PRIMARY KEY, nombre TEXT)")
	run(ctx, exe, "CREATE TABLE cursos (id INTEGER PRIMARY KEY, titulo TEXT)")
	run(ctx, exe, "CREATE TABLE inscripciones (id INTEGER PRIMARY KEY, est_id INTEGER, cur_id INTEGER, nota INTEGER, FOREIGN KEY (est_id) REFERENCES estudiantes(id), FOREIGN KEY (cur_id) REFERENCES cursos(id))")

	run(ctx, exe, "INSERT INTO estudiantes VALUES (1, 'Ana')")
	run(ctx, exe, "INSERT INTO estudiantes VALUES (2, 'Bob')")
	run(ctx, exe, "INSERT INTO estudiantes VALUES (3, 'Carla')") // no enrollments -> LEFT JOIN NULL
	run(ctx, exe, "INSERT INTO cursos VALUES (1, 'BD')")
	run(ctx, exe, "INSERT INTO cursos VALUES (2, 'Redes')")
	run(ctx, exe, "INSERT INTO inscripciones VALUES (1, 1, 1, 90)")
	run(ctx, exe, "INSERT INTO inscripciones VALUES (2, 1, 2, 80)")
	run(ctx, exe, "INSERT INTO inscripciones VALUES (3, 2, 1, 70)")

	// 3-way INNER join (used to fail with "unknown table qualifier cursos").
	expectRows(ctx, exe,
		"SELECT estudiantes.nombre, cursos.titulo, inscripciones.nota FROM inscripciones INNER JOIN estudiantes ON inscripciones.est_id = estudiantes.id INNER JOIN cursos ON inscripciones.cur_id = cursos.id", 3)

	// 3-way with ORDER BY nota DESC -> top row is Ana/BD/90.
	expectCell(ctx, exe,
		"SELECT estudiantes.nombre, cursos.titulo, inscripciones.nota FROM inscripciones INNER JOIN estudiantes ON inscripciones.est_id = estudiantes.id INNER JOIN cursos ON inscripciones.cur_id = cursos.id ORDER BY inscripciones.nota DESC", 0, 0, "Ana")

	// 3-way with compound WHERE.
	expectRows(ctx, exe,
		"SELECT estudiantes.nombre FROM inscripciones INNER JOIN estudiantes ON inscripciones.est_id = estudiantes.id INNER JOIN cursos ON inscripciones.cur_id = cursos.id WHERE inscripciones.nota >= 80 AND cursos.titulo = 'BD'", 1)

	// 3-way LEFT join keeps Carla (no enrollment) with NULLs -> 4 rows total
	// (Ana x2, Bob x1, Carla x1).
	expectRows(ctx, exe,
		"SELECT estudiantes.nombre, cursos.titulo FROM estudiantes LEFT JOIN inscripciones ON estudiantes.id = inscripciones.est_id LEFT JOIN cursos ON inscripciones.cur_id = cursos.id", 4)

	// Aggregate + GROUP BY over a 3-way join: 2 courses.
	expectRows(ctx, exe,
		"SELECT cursos.titulo, AVG(inscripciones.nota) FROM inscripciones INNER JOIN estudiantes ON inscripciones.est_id = estudiantes.id INNER JOIN cursos ON inscripciones.cur_id = cursos.id GROUP BY cursos.titulo", 2)

	// SELECT * over a 3-way join exposes qualified "ref.col" columns (4+2+2 = 8).
	starRes := run(ctx, exe, "SELECT * FROM inscripciones INNER JOIN estudiantes ON inscripciones.est_id = estudiantes.id INNER JOIN cursos ON inscripciones.cur_id = cursos.id LIMIT 1")
	if len(starRes.Columns) != 8 {
		log.Fatalf("SELECT * 3-way: expected 8 columns, got %d (%v)", len(starRes.Columns), starRes.Columns)
	}
	fmt.Printf("OK  SELECT * 3-way -> %d columns: %v\n", len(starRes.Columns), starRes.Columns)

	// Unknown qualifier still reported clearly.
	expectErr(ctx, exe,
		"SELECT noexiste.x FROM estudiantes INNER JOIN cursos ON estudiantes.id = cursos.id", "unknown table qualifier")

	fmt.Println("=== FK-vs-IDENTITY + N-way JOIN test passed ===")
}
