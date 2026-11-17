// Command test-followups covers the compatibility follow-ups: negative numeric
// literals, CHECK constraints that compare two columns, and (non-correlated)
// subqueries in the WHERE of UPDATE and DELETE.
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

func mustFail(ctx context.Context, e *executor.Executor, sql, why string) {
	stmt, err := parser.NewParser(sql).ParseStatement()
	if err != nil {
		return
	}
	if _, err := e.Execute(ctx, stmt); err == nil {
		log.Fatalf("expected %q to fail (%s), but it succeeded", sql, why)
	}
}

func col0(res *executor.Result) []string {
	out := make([]string, len(res.Rows))
	for i, r := range res.Rows {
		out[i] = fmt.Sprintf("%v", r[0])
	}
	return out
}

func eq(label string, got, want []string) {
	if fmt.Sprint(got) != fmt.Sprint(want) {
		log.Fatalf("%s: expected %v, got %v", label, want, got)
	}
	fmt.Printf("  ok: %s -> %v\n", label, got)
}

func main() {
	ctx := context.Background()
	e := executor.New(catalog.New(), nil)

	fmt.Println("=== Negative numeric literals ===")
	run(ctx, e, "CREATE TABLE cuentas (id INT IDENTITY PRIMARY KEY, saldo INT CHECK (saldo >= -100))")
	run(ctx, e, "INSERT INTO cuentas (saldo) VALUES (-50), (20)")
	mustFail(ctx, e, "INSERT INTO cuentas (saldo) VALUES (-200)", "saldo < -100 viola CHECK")
	eq("WHERE saldo < 0", col0(run(ctx, e, "SELECT saldo FROM cuentas WHERE saldo < 0")), []string{"-50"})
	run(ctx, e, "UPDATE cuentas SET saldo = -30 WHERE saldo = 20")
	eq("UPDATE a negativo", col0(run(ctx, e, "SELECT saldo FROM cuentas WHERE saldo = -30")), []string{"-30"})

	fmt.Println("=== CHECK columna-contra-columna ===")
	run(ctx, e, "CREATE TABLE rango (id INT IDENTITY PRIMARY KEY, inicio INT, fin INT, CONSTRAINT chk CHECK (fin > inicio))")
	mustFail(ctx, e, "INSERT INTO rango (inicio, fin) VALUES (5, 3)", "fin <= inicio")
	run(ctx, e, "INSERT INTO rango (inicio, fin) VALUES (5, 10)")
	mustFail(ctx, e, "UPDATE rango SET fin = 1 WHERE inicio = 5", "update deja fin <= inicio")
	fmt.Println("  ok: CHECK (fin > inicio) comparando dos columnas")

	fmt.Println("=== Subconsultas en UPDATE / DELETE WHERE ===")
	run(ctx, e, "CREATE TABLE emp (id INT IDENTITY PRIMARY KEY, dept INT, sal INT)")
	run(ctx, e, "INSERT INTO emp (dept, sal) VALUES (1,100),(1,200),(2,300)")
	// UPDATE con IN (subquery)
	run(ctx, e, "UPDATE emp SET sal = 0 WHERE dept IN (SELECT dept FROM emp WHERE sal > 250)")
	eq("UPDATE IN (subquery)", col0(run(ctx, e, "SELECT sal FROM emp ORDER BY id")), []string{"100", "200", "0"})
	// DELETE con subquery escalar
	run(ctx, e, "DELETE FROM emp WHERE sal = (SELECT MAX(sal) FROM emp)")
	eq("DELETE escalar subquery", col0(run(ctx, e, "SELECT sal FROM emp ORDER BY id")), []string{"100", "0"})
	// DELETE con IN lista
	run(ctx, e, "DELETE FROM emp WHERE sal IN (0)")
	eq("DELETE IN (lista)", col0(run(ctx, e, "SELECT sal FROM emp")), []string{"100"})

	fmt.Println("=== All follow-up tests passed! ===")
}
