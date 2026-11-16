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

func main() {
	fmt.Println("=== Testing trigger OLD/NEW row references ===")
	ctx := context.Background()
	cat := catalog.New()
	e := executor.New(cat, nil)

	run(ctx, e, "CREATE TABLE productos (id INT IDENTITY PRIMARY KEY, nombre TEXT, precio INT)")
	run(ctx, e, "CREATE TABLE auditoria (id INT IDENTITY PRIMARY KEY, evento TEXT, prod_id INT, prod_nombre TEXT, prod_precio INT)")

	run(ctx, e, `CREATE TRIGGER trg_ins AFTER INSERT ON productos FOR EACH ROW BEGIN
		INSERT INTO auditoria (evento, prod_id, prod_nombre, prod_precio)
		VALUES ('INSERT', NEW.id, NEW.nombre, NEW.precio);
	END;`)
	run(ctx, e, `CREATE TRIGGER trg_upd AFTER UPDATE ON productos FOR EACH ROW BEGIN
		INSERT INTO auditoria (evento, prod_id, prod_nombre, prod_precio)
		VALUES ('UPDATE', OLD.id, NEW.nombre, NEW.precio);
	END;`)
	run(ctx, e, `CREATE TRIGGER trg_del AFTER DELETE ON productos FOR EACH ROW BEGIN
		INSERT INTO auditoria (evento, prod_id, prod_nombre, prod_precio)
		VALUES ('DELETE', OLD.id, OLD.nombre, OLD.precio);
	END;`)

	run(ctx, e, "INSERT INTO productos (nombre, precio) VALUES ('Laptop', 1000)")
	run(ctx, e, "UPDATE productos SET precio = 1200 WHERE nombre = 'Laptop'")
	run(ctx, e, "DELETE FROM productos WHERE nombre = 'Laptop'")

	res := run(ctx, e, "SELECT evento, prod_id, prod_nombre, prod_precio FROM auditoria")
	if len(res.Rows) != 3 {
		log.Fatalf("expected 3 audit rows, got %d: %+v", len(res.Rows), res.Rows)
	}

	cell := func(r, c int) string { return fmt.Sprintf("%v", res.Rows[r][c]) }

	// Row 0: INSERT with NEW values (id=1, Laptop, 1000)
	if cell(0, 0) != "INSERT" || cell(0, 1) != "1" || cell(0, 2) != "Laptop" || cell(0, 3) != "1000" {
		log.Fatalf("INSERT audit wrong: %+v", res.Rows[0])
	}
	// Row 1: UPDATE with OLD.id=1 and NEW.precio=1200
	if cell(1, 0) != "UPDATE" || cell(1, 1) != "1" || cell(1, 2) != "Laptop" || cell(1, 3) != "1200" {
		log.Fatalf("UPDATE audit wrong (expected OLD.id=1, NEW.precio=1200): %+v", res.Rows[1])
	}
	// Row 2: DELETE with OLD values (the row had precio 1200 after the update)
	if cell(2, 0) != "DELETE" || cell(2, 1) != "1" || cell(2, 2) != "Laptop" || cell(2, 3) != "1200" {
		log.Fatalf("DELETE audit wrong (expected OLD id=1, Laptop, 1200): %+v", res.Rows[2])
	}

	for _, r := range res.Rows {
		fmt.Printf("  %v\n", r)
	}
	fmt.Println("=== OLD/NEW trigger references work for INSERT/UPDATE/DELETE ===")
}
