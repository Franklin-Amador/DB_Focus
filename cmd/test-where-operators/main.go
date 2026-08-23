package main

import (
	"context"
	"fmt"
	"log"

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

func expectRows(ctx context.Context, exe *executor.Executor, sql string, want int) {
	res := run(ctx, exe, sql)
	if len(res.Rows) != want {
		log.Fatalf("%q: expected %d rows, got %d (%+v)", sql, want, len(res.Rows), res.Rows)
	}
	fmt.Printf("OK  %-64s -> %d rows\n", sql, len(res.Rows))
}

func main() {
	fmt.Println("=== Testing WHERE comparison + compound (AND/OR) operators ===")

	cat := catalog.New()
	exe := executor.New(cat, nil)
	ctx := context.Background()

	run(ctx, exe, "CREATE TABLE productos (id INT IDENTITY PRIMARY KEY, nombre TEXT, precio INT)")
	run(ctx, exe, "INSERT INTO productos (nombre, precio) VALUES ('Laptop', 1000)")
	run(ctx, exe, "INSERT INTO productos (nombre, precio) VALUES ('Mouse', 25)")
	run(ctx, exe, "INSERT INTO productos (nombre, precio) VALUES ('Teclado', 50)")
	run(ctx, exe, "INSERT INTO productos (nombre, precio) VALUES ('Monitor', 300)")

	// --- Paso 1: single comparison operators ---
	expectRows(ctx, exe, "SELECT * FROM productos WHERE precio > 100", 2)
	expectRows(ctx, exe, "SELECT * FROM productos WHERE precio <= 50", 2)
	expectRows(ctx, exe, "SELECT * FROM productos WHERE precio != 25", 3)
	expectRows(ctx, exe, "SELECT * FROM productos WHERE precio = 50", 1)
	expectRows(ctx, exe, "SELECT * FROM productos WHERE nombre > 'M'", 3)

	// --- Paso 2: compound predicates ---
	// AND: only Monitor (300) is between 100 and 500.
	expectRows(ctx, exe, "SELECT * FROM productos WHERE precio > 100 AND precio < 500", 1)
	// OR: Mouse (25) or Laptop (1000).
	expectRows(ctx, exe, "SELECT * FROM productos WHERE precio < 30 OR precio > 500", 2)
	// AND with a string inequality: Teclado (50) and Monitor (300).
	expectRows(ctx, exe, "SELECT * FROM productos WHERE precio >= 50 AND nombre <> 'Laptop'", 2)
	// Precedence: AND binds tighter than OR -> precio<30 OR (precio>100 AND precio<500).
	// Mouse (25) and Monitor (300) match.
	expectRows(ctx, exe, "SELECT * FROM productos WHERE precio < 30 OR precio > 100 AND precio < 500", 2)
	// Parentheses change grouping. Without parens: precio=25 OR (precio=50 AND nombre='ZZZ') -> Mouse.
	expectRows(ctx, exe, "SELECT * FROM productos WHERE precio = 25 OR precio = 50 AND nombre = 'ZZZ'", 1)
	// With parens: (precio=25 OR precio=50) AND nombre='ZZZ' -> none.
	expectRows(ctx, exe, "SELECT * FROM productos WHERE (precio = 25 OR precio = 50) AND nombre = 'ZZZ'", 0)

	// Compound UPDATE: rename the two mid-range products.
	run(ctx, exe, "UPDATE productos SET nombre = 'MID' WHERE precio >= 50 AND precio <= 300")
	expectRows(ctx, exe, "SELECT * FROM productos WHERE nombre = 'MID'", 2) // Teclado, Monitor

	// Compound DELETE: remove the extremes (Mouse 25 and Laptop 1000).
	run(ctx, exe, "DELETE FROM productos WHERE precio < 30 OR precio > 900")
	expectRows(ctx, exe, "SELECT * FROM productos", 2) // Teclado[MID], Monitor[MID]

	// --- JOIN + compound WHERE (exercises the joined-row filter path) ---
	run(ctx, exe, "CREATE TABLE clientes (id INT PRIMARY KEY, nombre TEXT)")
	run(ctx, exe, "CREATE TABLE pedidos (id INT PRIMARY KEY, cliente_id INT, total INT)")
	run(ctx, exe, "INSERT INTO clientes VALUES (1, 'Ana')")
	run(ctx, exe, "INSERT INTO clientes VALUES (2, 'Beto')")
	run(ctx, exe, "INSERT INTO pedidos VALUES (1, 1, 100)")
	run(ctx, exe, "INSERT INTO pedidos VALUES (2, 1, 500)")
	run(ctx, exe, "INSERT INTO pedidos VALUES (3, 2, 50)")

	// Two pedidos exceed 80 (100 and 500), both belong to Ana.
	expectRows(ctx, exe, "SELECT * FROM clientes INNER JOIN pedidos ON clientes.id = pedidos.cliente_id WHERE pedidos.total > 80", 2)
	expectRows(ctx, exe, "SELECT * FROM clientes INNER JOIN pedidos ON clientes.id = pedidos.cliente_id WHERE pedidos.total > 80 AND clientes.nombre = 'Ana'", 2)
	expectRows(ctx, exe, "SELECT * FROM clientes INNER JOIN pedidos ON clientes.id = pedidos.cliente_id WHERE pedidos.total > 80 AND clientes.nombre = 'Beto'", 0)

	fmt.Println("=== WHERE compound operators test passed ===")
}
