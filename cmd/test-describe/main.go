package main

import (
	"context"
	"fmt"
	"log"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
)

func exec(ctx context.Context, e *executor.Executor, sql string) *executor.Result {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for %q: %v", sql, err)
	}
	res, err := e.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("execute error for %q: %v", sql, err)
	}
	return res
}

// cell returns the string form of row[name] by resolving the column index
// from the result's column list.
func cell(res *executor.Result, row int, col string) string {
	idx := -1
	for i, c := range res.Columns {
		if c == col {
			idx = i
			break
		}
	}
	if idx < 0 {
		log.Fatalf("result has no column %q (columns=%v)", col, res.Columns)
	}
	return fmt.Sprintf("%v", res.Rows[row][idx])
}

func main() {
	fmt.Println("=== Testing DESCRIBE ===")

	cat := catalog.New()
	e := executor.New(cat, nil) // no persistence needed for DESCRIBE
	ctx := context.Background()

	exec(ctx, e, "CREATE TABLE categorias (id INT IDENTITY PRIMARY KEY, nombre TEXT NOT NULL)")
	exec(ctx, e, `CREATE TABLE productos (
		id INT IDENTITY PRIMARY KEY,
		sku TEXT UNIQUE,
		nombre TEXT,
		categoria_id INT REFERENCES categorias(id)
	)`)

	// DESCRIBE productos
	res := exec(ctx, e, "DESCRIBE productos")
	if res.Tag != "DESCRIBE" {
		log.Fatalf("expected tag DESCRIBE, got %q", res.Tag)
	}
	wantCols := []string{"Column", "Type", "Null", "Key", "Identity"}
	if fmt.Sprint(res.Columns) != fmt.Sprint(wantCols) {
		log.Fatalf("expected columns %v, got %v", wantCols, res.Columns)
	}
	if len(res.Rows) != 4 {
		log.Fatalf("expected 4 columns described, got %d", len(res.Rows))
	}

	// Row 0: id — IDENTITY, PRIMARY KEY
	assertEq("id.Column", cell(res, 0, "Column"), "id")
	assertEq("id.Key", cell(res, 0, "Key"), "PRI")
	assertEq("id.Identity", cell(res, 0, "Identity"), "YES")

	// Row 1: sku — UNIQUE, nullable
	assertEq("sku.Key", cell(res, 1, "Key"), "UNI")
	assertEq("sku.Null", cell(res, 1, "Null"), "YES")
	assertEq("sku.Identity", cell(res, 1, "Identity"), "NO")

	// Row 2: nombre — no key
	assertEq("nombre.Key", cell(res, 2, "Key"), "")

	// Row 3: categoria_id — FOREIGN KEY -> MUL
	assertEq("categoria_id.Key", cell(res, 3, "Key"), "MUL")

	fmt.Println("OK: DESCRIBE productos reports columns/type/null/key/identity")

	// NOT NULL reporting on categorias.nombre
	res = exec(ctx, e, "DESC categorias")
	assertEq("categorias.nombre.Null", cell(res, 1, "Null"), "NO")
	fmt.Println("OK: DESC alias and NOT NULL reporting")

	// Unknown table must error
	p := parser.NewParser("DESCRIBE no_existe")
	stmt, _ := p.ParseStatement()
	if _, err := e.Execute(ctx, stmt); err == nil {
		log.Fatalf("expected error describing a non-existent table")
	}
	fmt.Println("OK: DESCRIBE of unknown table errors")

	fmt.Println("=== DESCRIBE tests passed ===")
}

func assertEq(what, got, want string) {
	if got != want {
		log.Fatalf("%s: expected %q, got %q", what, want, got)
	}
}
