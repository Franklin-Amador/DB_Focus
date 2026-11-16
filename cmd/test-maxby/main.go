// test-maxby covers the two-argument aggregates MAX_BY(value, order) and
// MIN_BY(value, order), scalar and grouped.
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

func scalar(ctx context.Context, e *executor.Executor, sql string) string {
	r := run(ctx, e, sql)
	if len(r.Rows) != 1 || len(r.Rows[0]) != 1 {
		log.Fatalf("%q: esperaba 1x1, got %+v", sql, r.Rows)
	}
	return fmt.Sprintf("%v", r.Rows[0][0])
}

func mustFail(ctx context.Context, e *executor.Executor, sql, label string) {
	st, err := parser.NewParser(sql).ParseStatement()
	if err != nil {
		fmt.Printf("OK  %-40s rechazado al parsear: %v\n", label, err)
		return
	}
	if _, err := e.Execute(ctx, st); err != nil {
		fmt.Printf("OK  %-40s rechazado: %v\n", label, err)
		return
	}
	log.Fatalf("FAIL %s: debía fallar", label)
}

func main() {
	ctx := context.Background()
	cat := catalog.New()
	e := executor.New(cat, nil)

	// Un historial de estados por entidad, con timestamp.
	run(ctx, e, "CREATE TABLE eventos (id INT, entidad INT, status TEXT, updated_at INT)")
	run(ctx, e, "INSERT INTO eventos VALUES (1, 10, 'nuevo', 100)")
	run(ctx, e, "INSERT INTO eventos VALUES (2, 10, 'pagado', 300)")   // más reciente de entidad 10
	run(ctx, e, "INSERT INTO eventos VALUES (3, 10, 'enviado', 200)")
	run(ctx, e, "INSERT INTO eventos VALUES (4, 20, 'nuevo', 150)")
	run(ctx, e, "INSERT INTO eventos VALUES (5, 20, 'cancelado', 50)") // el más viejo de entidad 20

	fmt.Println("=== MAX_BY / MIN_BY escalar ===")
	if got := scalar(ctx, e, "SELECT MAX_BY(status, updated_at) FROM eventos"); got != "pagado" {
		log.Fatalf("MAX_BY escalar: esperaba 'pagado', got %q", got)
	}
	fmt.Println("OK  MAX_BY(status, updated_at) -> pagado (updated_at=300)")
	if got := scalar(ctx, e, "SELECT MIN_BY(status, updated_at) FROM eventos"); got != "cancelado" {
		log.Fatalf("MIN_BY escalar: esperaba 'cancelado', got %q", got)
	}
	fmt.Println("OK  MIN_BY(status, updated_at) -> cancelado (updated_at=50)")

	fmt.Println("=== MAX_BY con GROUP BY (status más reciente por entidad) ===")
	r := run(ctx, e, "SELECT entidad, MAX_BY(status, updated_at) AS ultimo FROM eventos GROUP BY entidad")
	got := map[string]string{}
	for _, row := range r.Rows {
		got[fmt.Sprintf("%v", row[0])] = fmt.Sprintf("%v", row[1])
	}
	if got["10"] != "pagado" || got["20"] != "nuevo" {
		log.Fatalf("MAX_BY GROUP BY: %v (esperaba 10=pagado, 20=nuevo)", got)
	}
	fmt.Printf("OK  por entidad -> 10=%s, 20=%s\n", got["10"], got["20"])

	// GROUP BY ALL + MAX_BY juntos (combina las dos features).
	r = run(ctx, e, "SELECT entidad, MIN_BY(status, updated_at) AS primero FROM eventos GROUP BY ALL")
	got = map[string]string{}
	for _, row := range r.Rows {
		got[fmt.Sprintf("%v", row[0])] = fmt.Sprintf("%v", row[1])
	}
	if got["10"] != "nuevo" || got["20"] != "cancelado" {
		log.Fatalf("MIN_BY GROUP BY ALL: %v", got)
	}
	fmt.Printf("OK  MIN_BY + GROUP BY ALL -> 10=%s, 20=%s\n", got["10"], got["20"])

	fmt.Println("=== Errores de aridad ===")
	mustFail(ctx, e, "SELECT MAX_BY(status) FROM eventos", "MAX_BY con 1 argumento")

	fmt.Println("=== Todo OK ===")
}
