// Command test-subqueries exercises subquery support: scalar subqueries in
// WHERE, IN (subquery) and IN (list), NOT IN, correlated EXISTS / NOT EXISTS,
// and derived tables (FROM (SELECT ...) alias), including a derived table used
// as the left side of a JOIN.
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

	run(ctx, e, "CREATE TABLE emp (id INT IDENTITY PRIMARY KEY, dept INT, salario INT)")
	run(ctx, e, "INSERT INTO emp (dept, salario) VALUES (1,100),(1,200),(2,300)")

	fmt.Println("=== Scalar subquery in WHERE ===")
	eq("= (SELECT MAX)", col0(run(ctx, e, "SELECT salario FROM emp WHERE salario = (SELECT MAX(salario) FROM emp)")), []string{"300"})

	fmt.Println("=== IN (subquery) ===")
	eq("IN subquery", col0(run(ctx, e, "SELECT salario FROM emp WHERE dept IN (SELECT dept FROM emp WHERE salario > 150) ORDER BY salario")), []string{"100", "200", "300"})

	fmt.Println("=== IN (list) and NOT IN ===")
	eq("IN list", col0(run(ctx, e, "SELECT salario FROM emp WHERE dept IN (2)")), []string{"300"})
	eq("NOT IN list", col0(run(ctx, e, "SELECT salario FROM emp WHERE dept NOT IN (2) ORDER BY salario")), []string{"100", "200"})

	fmt.Println("=== Correlated EXISTS / NOT EXISTS ===")
	eq("EXISTS", col0(run(ctx, e, "SELECT salario FROM emp e WHERE EXISTS (SELECT 1 FROM emp x WHERE x.dept = e.dept AND x.salario > 150) ORDER BY salario")), []string{"100", "200", "300"})
	eq("NOT EXISTS", col0(run(ctx, e, "SELECT salario FROM emp e WHERE NOT EXISTS (SELECT 1 FROM emp x WHERE x.dept = e.dept AND x.salario > 250) ORDER BY salario")), []string{"100", "200"})

	fmt.Println("=== Derived table (FROM (SELECT ...) alias) ===")
	eq("derived", col0(run(ctx, e, "SELECT dept FROM (SELECT dept FROM emp WHERE salario > 150) sub ORDER BY dept")), []string{"1", "2"})
	eq("derived + WHERE alias", col0(run(ctx, e, "SELECT sub.salario FROM (SELECT dept, salario FROM emp) sub WHERE sub.salario >= 200 ORDER BY sub.salario")), []string{"200", "300"})

	fmt.Println("=== Derived table joined to a base table ===")
	run(ctx, e, "CREATE TABLE deptos (id INT IDENTITY PRIMARY KEY, dept INT, nombre TEXT)")
	run(ctx, e, "INSERT INTO deptos (dept, nombre) VALUES (1,'Ventas'),(2,'IT')")
	res := run(ctx, e, "SELECT d.nombre FROM (SELECT dept FROM emp WHERE salario = 300) sub INNER JOIN deptos d ON sub.dept = d.dept")
	eq("derived JOIN", col0(res), []string{"IT"})

	fmt.Println("=== All subquery tests passed! ===")
}
