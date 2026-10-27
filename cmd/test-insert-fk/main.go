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
		fmt.Printf("OK  %-52s rechazado al parsear\n", label)
		return
	}
	if _, err := e.Execute(ctx, st); err != nil {
		fmt.Printf("OK  %-52s rechazado: %v\n", label, err)
		return
	}
	log.Fatalf("FAIL %s: debía fallar y tuvo éxito", label)
}

func count(ctx context.Context, e *executor.Executor, table string) int {
	r := run(ctx, e, "SELECT * FROM "+table)
	if r == nil {
		return 0
	}
	return len(r.Rows)
}

func main() {
	fmt.Println("=== INSERT multi-fila + FK (inline y DELETE) ===")
	ctx := context.Background()
	cat := catalog.New()
	e := executor.New(cat, nil)

	// --- Multi-row INSERT, forma Postgres ---
	run(ctx, e, "CREATE TABLE productos (id INT IDENTITY PRIMARY KEY, nombre TEXT, precio INT)")
	r := run(ctx, e, "INSERT INTO productos (nombre, precio) VALUES ('A', 10), ('B', 20), ('C', 30)")
	if r.Tag != "INSERT 0 3" {
		log.Fatalf("tag esperado 'INSERT 0 3', got %q", r.Tag)
	}
	if n := count(ctx, e, "productos"); n != 3 {
		log.Fatalf("esperaba 3 filas, got %d", n)
	}
	fmt.Printf("OK  %-52s %q, %d filas\n", "INSERT multi-fila (lista de columnas)", r.Tag, 3)

	// IDENTITY autogenerado por fila + valores correctos
	res := run(ctx, e, "SELECT id, nombre, precio FROM productos ORDER BY id")
	want := [][]string{{"1", "A", "10"}, {"2", "B", "20"}, {"3", "C", "30"}}
	for i, row := range res.Rows {
		for j := range want[i] {
			if fmt.Sprintf("%v", row[j]) != want[i][j] {
				log.Fatalf("fila %d col %d: esperaba %q got %v", i, j, want[i][j], row[j])
			}
		}
	}
	fmt.Printf("OK  %-52s ids 1..3 autogenerados\n", "IDENTITY por fila en multi-fila")

	// Multi-row posicional (sin lista de columnas, solo no-IDENTITY)
	run(ctx, e, "INSERT INTO productos VALUES ('D', 40), ('E', 50)")
	if n := count(ctx, e, "productos"); n != 5 {
		log.Fatalf("esperaba 5 filas tras posicional, got %d", n)
	}
	fmt.Printf("OK  %-52s 5 filas\n", "INSERT multi-fila posicional")

	// --- FK declarada inline (en una línea) ---
	run(ctx, e, "CREATE TABLE pedidos (id INT IDENTITY PRIMARY KEY, producto_id INT REFERENCES productos(id), cantidad INT)")
	run(ctx, e, "INSERT INTO pedidos (producto_id, cantidad) VALUES (1, 2)") // producto 1 existe
	fmt.Printf("OK  %-52s FK inline aceptó referencia válida\n", "FK inline en runtime")
	mustFail(ctx, e, "INSERT INTO pedidos (producto_id, cantidad) VALUES (999, 1)", "INSERT con FK inexistente")

	// --- DELETE bloqueado por FK (PK IDENTITY) ---
	mustFail(ctx, e, "DELETE FROM productos WHERE id = 1", "DELETE de padre referenciado")
	if n := count(ctx, e, "productos"); n != 5 {
		log.Fatalf("el DELETE no debía borrar nada, quedan %d (esperaba 5)", n)
	}
	// Tras borrar el hijo, el padre se puede borrar
	run(ctx, e, "DELETE FROM pedidos WHERE producto_id = 1")
	run(ctx, e, "DELETE FROM productos WHERE id = 1")
	if n := count(ctx, e, "productos"); n != 4 {
		log.Fatalf("tras liberar FK, esperaba 4, got %d", n)
	}
	fmt.Printf("OK  %-52s DELETE permitido tras liberar la FK\n", "DELETE padre sin referencias")

	// --- SELECT por id IDENTITY sin índice ---
	sel := run(ctx, e, "SELECT nombre FROM productos WHERE id = 3")
	if len(sel.Rows) != 1 || fmt.Sprintf("%v", sel.Rows[0][0]) != "C" {
		log.Fatalf("SELECT WHERE id=3 esperaba ['C'], got %v", sel.Rows)
	}
	fmt.Printf("OK  %-52s WHERE id=3 -> C\n", "SELECT por PK IDENTITY sin índice")

	// --- Procedure con parámetros INSERT (guarda de la sustitución) ---
	run(ctx, e, "CREATE PROCEDURE add_prod(nom TEXT, pr INT) AS BEGIN INSERT INTO productos (nombre, precio) VALUES (nom, pr); END")
	run(ctx, e, "CALL add_prod('Z', 99)")
	zrows := run(ctx, e, "SELECT nombre, precio FROM productos WHERE nombre = 'Z'")
	if len(zrows.Rows) != 1 || fmt.Sprintf("%v", zrows.Rows[0][1]) != "99" {
		log.Fatalf("procedure param insert falló: %v", zrows.Rows)
	}
	fmt.Printf("OK  %-52s procedure insertó Z/99 (no el nombre del parámetro)\n", "Procedure param INSERT")

	fmt.Println("=== Todo OK ===")
}
