package main

import (
	"context"
	"fmt"
	"log"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
)

func query(ctx context.Context, exe *executor.Executor, sql string) *executor.Result {
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

// scalar runs a single-value aggregate query and returns res.Rows[0][0] as text.
func scalar(ctx context.Context, exe *executor.Executor, sql string) string {
	res := query(ctx, exe, sql)
	if len(res.Rows) != 1 || len(res.Rows[0]) < 1 {
		log.Fatalf("%q: expected 1x1 result, got %+v", sql, res.Rows)
	}
	return fmt.Sprintf("%v", res.Rows[0][0])
}

func expectScalar(ctx context.Context, exe *executor.Executor, sql, want string) {
	got := scalar(ctx, exe, sql)
	if got != want {
		log.Fatalf("%q: expected %q, got %q", sql, want, got)
	}
	fmt.Printf("OK  %-50s = %s\n", sql, got)
}

// groupMap runs a `SELECT key, agg ... GROUP BY key` query and returns a
// key->value(text) map.
func groupMap(ctx context.Context, exe *executor.Executor, sql string) map[string]string {
	res := query(ctx, exe, sql)
	m := map[string]string{}
	for _, row := range res.Rows {
		if len(row) < 2 {
			log.Fatalf("%q: expected 2 columns, got %+v", sql, row)
		}
		m[fmt.Sprintf("%v", row[0])] = fmt.Sprintf("%v", row[1])
	}
	return m
}

func expectGroup(ctx context.Context, exe *executor.Executor, sql string, want map[string]string) {
	got := groupMap(ctx, exe, sql)
	if len(got) != len(want) {
		log.Fatalf("%q: expected %d groups, got %d (%+v)", sql, len(want), len(got), got)
	}
	for k, v := range want {
		if got[k] != v {
			log.Fatalf("%q: group %q expected %q, got %q (all=%+v)", sql, k, v, got[k], got)
		}
	}
	fmt.Printf("OK  %-50s = %+v\n", sql, got)
}

func main() {
	fmt.Println("=== Testing aggregate functions ===")

	cat := catalog.New()
	exe := executor.New(cat, nil)
	ctx := context.Background()

	query(ctx, exe, "CREATE TABLE ventas (id INT IDENTITY PRIMARY KEY, categoria TEXT, monto INT)")
	query(ctx, exe, "INSERT INTO ventas (categoria, monto) VALUES ('A', 100)")
	query(ctx, exe, "INSERT INTO ventas (categoria, monto) VALUES ('A', 200)")
	query(ctx, exe, "INSERT INTO ventas (categoria, monto) VALUES ('B', 100)")
	query(ctx, exe, "INSERT INTO ventas (categoria, monto) VALUES ('B', 200)")
	query(ctx, exe, "INSERT INTO ventas (categoria, monto) VALUES ('B', 300)")

	// Scalar aggregates (no GROUP BY).
	expectScalar(ctx, exe, "SELECT COUNT(*) FROM ventas", "5")
	expectScalar(ctx, exe, "SELECT SUM(monto) FROM ventas", "900")
	expectScalar(ctx, exe, "SELECT AVG(monto) FROM ventas", "180")
	expectScalar(ctx, exe, "SELECT MIN(monto) FROM ventas", "100")
	expectScalar(ctx, exe, "SELECT MAX(monto) FROM ventas", "300")

	// Scalar aggregate combined with a WHERE predicate.
	expectScalar(ctx, exe, "SELECT SUM(monto) FROM ventas WHERE categoria = 'B'", "600")

	// GROUP BY aggregates.
	expectGroup(ctx, exe, "SELECT categoria, COUNT(*) FROM ventas GROUP BY categoria",
		map[string]string{"A": "2", "B": "3"})
	expectGroup(ctx, exe, "SELECT categoria, SUM(monto) FROM ventas GROUP BY categoria",
		map[string]string{"A": "300", "B": "600"})
	expectGroup(ctx, exe, "SELECT categoria, AVG(monto) FROM ventas GROUP BY categoria",
		map[string]string{"A": "150", "B": "200"})
	expectGroup(ctx, exe, "SELECT categoria, MIN(monto) FROM ventas GROUP BY categoria",
		map[string]string{"A": "100", "B": "100"})
	expectGroup(ctx, exe, "SELECT categoria, MAX(monto) FROM ventas GROUP BY categoria",
		map[string]string{"A": "200", "B": "300"})

	fmt.Println("=== Aggregate functions test passed ===")
}
