// test-fk-actions exercises ON DELETE / ON UPDATE referential actions
// (RESTRICT default, CASCADE, SET NULL), the UPDATE-orphan fix, self-referencing
// cascade (which also covers the former self-FK delete deadlock), multi-level
// cascade, and persistence of the actions across a reload.
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

func rows(ctx context.Context, e *executor.Executor, sql string) [][]interface{} {
	return run(ctx, e, sql).Rows
}

func wantCount(ctx context.Context, e *executor.Executor, table string, n int) {
	got := len(rows(ctx, e, "SELECT * FROM "+table))
	if got != n {
		log.Fatalf("%s: esperaba %d filas, got %d", table, n, got)
	}
}

func cell(ctx context.Context, e *executor.Executor, sql string) interface{} {
	r := rows(ctx, e, sql)
	if len(r) == 0 || len(r[0]) == 0 {
		log.Fatalf("%q: sin resultado", sql)
	}
	return r[0][0]
}

func main() {
	ctx := context.Background()
	cat := catalog.New()
	e := executor.New(cat, nil)

	fmt.Println("=== A · RESTRICT por defecto (DELETE y UPDATE del padre) ===")
	run(ctx, e, "CREATE TABLE autor (id INT PRIMARY KEY, nombre TEXT)")
	run(ctx, e, "CREATE TABLE libro (id INT PRIMARY KEY, autor_id INT REFERENCES autor(id), titulo TEXT)")
	run(ctx, e, "INSERT INTO autor VALUES (1, 'Borges'), (2, 'Cortazar')")
	run(ctx, e, "INSERT INTO libro VALUES (10, 1, 'Ficciones'), (11, 1, 'Aleph'), (12, 2, 'Rayuela')")
	mustFail(ctx, e, "DELETE FROM autor WHERE id = 1", "DELETE padre referenciado")
	mustFail(ctx, e, "UPDATE autor SET id = 99 WHERE id = 1", "UPDATE de PK referenciada")
	wantCount(ctx, e, "autor", 2)
	if got := fmt.Sprintf("%v", cell(ctx, e, "SELECT autor_id FROM libro WHERE id = 10")); got != "1" {
		log.Fatalf("A: libro.autor_id no debía cambiar, got %v", got)
	}
	fmt.Println("OK  padre intacto; hijos intactos")

	fmt.Println("=== B · ON DELETE CASCADE ===")
	run(ctx, e, "CREATE TABLE cliente (id INT PRIMARY KEY, nombre TEXT)")
	run(ctx, e, "CREATE TABLE pedido (id INT PRIMARY KEY, cliente_id INT REFERENCES cliente(id) ON DELETE CASCADE, total INT)")
	run(ctx, e, "INSERT INTO cliente VALUES (1, 'Ana'), (2, 'Beto')")
	run(ctx, e, "INSERT INTO pedido VALUES (1, 1, 100), (2, 1, 200), (3, 2, 50)")
	r := run(ctx, e, "DELETE FROM cliente WHERE id = 1")
	if r.Tag != "DELETE 1" {
		log.Fatalf("B: tag esperado 'DELETE 1', got %q", r.Tag)
	}
	wantCount(ctx, e, "cliente", 1)
	wantCount(ctx, e, "pedido", 1) // pedidos 1 y 2 cascadearon
	if got := fmt.Sprintf("%v", cell(ctx, e, "SELECT id FROM pedido")); got != "3" {
		log.Fatalf("B: debía quedar el pedido 3, got %v", got)
	}
	fmt.Println("OK  borró el cliente y sus 2 pedidos; dejó el ajeno")

	fmt.Println("=== C · ON DELETE SET NULL ===")
	run(ctx, e, "CREATE TABLE depto (id INT PRIMARY KEY, nombre TEXT)")
	run(ctx, e, "CREATE TABLE emp (id INT PRIMARY KEY, depto_id INT REFERENCES depto(id) ON DELETE SET NULL, nombre TEXT)")
	run(ctx, e, "INSERT INTO depto VALUES (1, 'IT'), (2, 'HR')")
	run(ctx, e, "INSERT INTO emp VALUES (1, 1, 'X'), (2, 1, 'Y'), (3, 2, 'Z')")
	run(ctx, e, "DELETE FROM depto WHERE id = 1")
	wantCount(ctx, e, "depto", 1)
	wantCount(ctx, e, "emp", 3) // ningún empleado se borra
	if v := cell(ctx, e, "SELECT depto_id FROM emp WHERE id = 1"); v != nil {
		log.Fatalf("C: emp.depto_id debía ser NULL, got %v", v)
	}
	if got := fmt.Sprintf("%v", cell(ctx, e, "SELECT depto_id FROM emp WHERE id = 3")); got != "2" {
		log.Fatalf("C: emp 3 no debía cambiar, got %v", got)
	}
	fmt.Println("OK  empleados preservados; FK del depto borrado puesta en NULL")

	fmt.Println("=== D · ON UPDATE CASCADE ===")
	run(ctx, e, "CREATE TABLE pais (codigo INT PRIMARY KEY, nombre TEXT)")
	run(ctx, e, "CREATE TABLE ciudad (id INT PRIMARY KEY, pais_cod INT REFERENCES pais(codigo) ON UPDATE CASCADE, nombre TEXT)")
	run(ctx, e, "INSERT INTO pais VALUES (1, 'AR'), (2, 'BR')")
	run(ctx, e, "INSERT INTO ciudad VALUES (10, 1, 'BsAs'), (11, 1, 'Cordoba'), (12, 2, 'Rio')")
	run(ctx, e, "UPDATE pais SET codigo = 99 WHERE codigo = 1")
	if got := fmt.Sprintf("%v", cell(ctx, e, "SELECT pais_cod FROM ciudad WHERE id = 10")); got != "99" {
		log.Fatalf("D: ciudad 10 debía cascadear a 99, got %v", got)
	}
	if got := fmt.Sprintf("%v", cell(ctx, e, "SELECT pais_cod FROM ciudad WHERE id = 12")); got != "2" {
		log.Fatalf("D: ciudad 12 no debía cambiar, got %v", got)
	}
	fmt.Println("OK  cambio de PK propagado a las ciudades hijas")

	fmt.Println("=== E · acciones mixtas (ON DELETE CASCADE ON UPDATE RESTRICT) ===")
	run(ctx, e, "CREATE TABLE m (id INT PRIMARY KEY)")
	run(ctx, e, "CREATE TABLE n (id INT PRIMARY KEY, m_id INT REFERENCES m(id) ON DELETE CASCADE ON UPDATE RESTRICT)")
	run(ctx, e, "INSERT INTO m VALUES (1)")
	run(ctx, e, "INSERT INTO n VALUES (1, 1)")
	mustFail(ctx, e, "UPDATE m SET id = 2 WHERE id = 1", "UPDATE con ON UPDATE RESTRICT")
	run(ctx, e, "DELETE FROM m WHERE id = 1")
	wantCount(ctx, e, "n", 0) // ON DELETE CASCADE
	fmt.Println("OK  UPDATE bloqueado, DELETE cascada en la misma FK")

	fmt.Println("=== F · self-FK CASCADE multinivel (y fix del deadlock) ===")
	run(ctx, e, "CREATE TABLE cat2 (id INT PRIMARY KEY, parent_id INT, nombre TEXT, FOREIGN KEY (parent_id) REFERENCES cat2(id) ON DELETE CASCADE)")
	run(ctx, e, "INSERT INTO cat2 (id, nombre) VALUES (1, 'root')")              // parent_id NULL (omitido)
	run(ctx, e, "INSERT INTO cat2 (id, parent_id, nombre) VALUES (2, 1, 'a'), (3, 1, 'b')")
	run(ctx, e, "INSERT INTO cat2 (id, parent_id, nombre) VALUES (4, 2, 'a1'), (5, 2, 'a2')")
	run(ctx, e, "DELETE FROM cat2 WHERE id = 1") // cascada 1 -> {2,3} -> {4,5}
	wantCount(ctx, e, "cat2", 0)
	fmt.Println("OK  cascada recursiva borró el árbol completo sin deadlock")

	// Self-FK con RESTRICT por defecto: debe BLOQUEAR (no deadlock).
	run(ctx, e, "CREATE TABLE tree (id INT PRIMARY KEY, parent_id INT, FOREIGN KEY (parent_id) REFERENCES tree(id))")
	run(ctx, e, "INSERT INTO tree (id) VALUES (1)")
	run(ctx, e, "INSERT INTO tree (id, parent_id) VALUES (2, 1)")
	mustFail(ctx, e, "DELETE FROM tree WHERE id = 1", "DELETE self-FK referenciado (RESTRICT)")
	wantCount(ctx, e, "tree", 2)
	fmt.Println("OK  self-FK RESTRICT bloquea sin deadlock")

	fmt.Println("=== G · persistencia de las acciones (reload) ===")
	testFKActionPersistence(ctx)

	fmt.Println("=== Todo OK ===")
}

// testFKActionPersistence creates an ON DELETE CASCADE FK, closes the store,
// reopens and reloads it, then verifies the action still fires — proving the
// OnDelete field round-trips through storage.
func testFKActionPersistence(ctx context.Context) {
	dir := "./data_test_fk_actions"
	defer os.RemoveAll(dir)

	st1, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	cat1 := catalog.New()
	e1 := executor.New(cat1, st1)
	run(ctx, e1, "CREATE TABLE padre (id INT PRIMARY KEY, nombre TEXT)")
	run(ctx, e1, "CREATE TABLE hijo (id INT PRIMARY KEY, padre_id INT REFERENCES padre(id) ON DELETE CASCADE, dato TEXT)")
	run(ctx, e1, "INSERT INTO padre VALUES (1, 'P')")
	run(ctx, e1, "INSERT INTO hijo VALUES (1, 1, 'h1'), (2, 1, 'h2')")
	st1.Close()

	st2, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatalf("reopen storage: %v", err)
	}
	defer st2.Close()
	cat2 := catalog.New()
	e2 := executor.New(cat2, st2)
	if err := st2.LoadAll(cat2); err != nil {
		log.Fatalf("LoadAll: %v", err)
	}
	// If OnDelete did not persist, this DELETE would be blocked by RESTRICT.
	run(ctx, e2, "DELETE FROM padre WHERE id = 1")
	if got := len(rows(ctx, e2, "SELECT * FROM hijo")); got != 0 {
		log.Fatalf("G: ON DELETE CASCADE no persistió; quedan %d hijos", got)
	}
	fmt.Println("OK  ON DELETE CASCADE sobrevivió al reinicio y cascadeó")
}
