// Command test-union exercises UNION and UNION ALL, including deduplication,
// multi-way chains with mixed operators, a trailing ORDER BY over the combined
// result, and the column-count mismatch error.
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

	run(ctx, e, "CREATE TABLE a (id INT IDENTITY PRIMARY KEY, v INT)")
	run(ctx, e, "CREATE TABLE b (id INT IDENTITY PRIMARY KEY, v INT)")
	run(ctx, e, "INSERT INTO a (v) VALUES (1),(2)")
	run(ctx, e, "INSERT INTO b (v) VALUES (2),(3)")

	fmt.Println("=== UNION (dedup) ===")
	eq("UNION", col0(run(ctx, e, "SELECT v FROM a UNION SELECT v FROM b ORDER BY v")), []string{"1", "2", "3"})

	fmt.Println("=== UNION ALL (keep duplicates) ===")
	eq("UNION ALL", col0(run(ctx, e, "SELECT v FROM a UNION ALL SELECT v FROM b ORDER BY v")), []string{"1", "2", "2", "3"})

	fmt.Println("=== ORDER BY DESC over the union ===")
	eq("ORDER BY DESC", col0(run(ctx, e, "SELECT v FROM a UNION SELECT v FROM b ORDER BY v DESC")), []string{"3", "2", "1"})

	fmt.Println("=== 3-way mixed (ALL then UNION dedups all) ===")
	eq("3-way", col0(run(ctx, e, "SELECT v FROM a UNION ALL SELECT v FROM b UNION SELECT v FROM a ORDER BY v")), []string{"1", "2", "3"})

	fmt.Println("=== WHERE on each operand ===")
	eq("filtered operands", col0(run(ctx, e, "SELECT v FROM a WHERE v = 1 UNION SELECT v FROM b WHERE v = 3 ORDER BY v")), []string{"1", "3"})

	fmt.Println("=== Column-count mismatch is rejected ===")
	stmt, err := parser.NewParser("SELECT id, v FROM a UNION SELECT v FROM b").ParseStatement()
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}
	if _, err := e.Execute(ctx, stmt); err == nil {
		log.Fatal("expected a column-count mismatch error")
	}
	fmt.Println("  ok: mismatched column counts rejected")

	fmt.Println("=== All UNION tests passed! ===")
}
