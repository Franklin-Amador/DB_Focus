// test-asof-join covers ASOF JOIN ... MATCH_CONDITION (nearest-earlier join),
// with and without an ON key.
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

func main() {
	ctx := context.Background()
	cat := catalog.New()
	e := executor.New(cat, nil)

	// trades: cada trade busca la cotización vigente (la más reciente <= su ts).
	run(ctx, e, "CREATE TABLE trades (sym TEXT, ts INT, qty INT)")
	run(ctx, e, "INSERT INTO trades VALUES ('AAA', 100, 10)")
	run(ctx, e, "INSERT INTO trades VALUES ('AAA', 250, 5)")
	run(ctx, e, "INSERT INTO trades VALUES ('BBB', 120, 7)")
	run(ctx, e, "INSERT INTO trades VALUES ('AAA', 90, 1)") // antes de cualquier quote AAA -> sin match

	run(ctx, e, "CREATE TABLE quotes (sym TEXT, ts INT, price INT)")
	run(ctx, e, "INSERT INTO quotes VALUES ('AAA', 100, 55)")
	run(ctx, e, "INSERT INTO quotes VALUES ('AAA', 200, 60)")
	run(ctx, e, "INSERT INTO quotes VALUES ('BBB', 110, 33)")

	fmt.Println("=== ASOF JOIN con ON (clave sym) ===")
	// Para cada trade, el price de la quote del mismo sym con el ts más grande <= trade.ts.
	r := run(ctx, e, "SELECT t.sym, t.ts, q.price FROM trades t ASOF JOIN quotes q MATCH_CONDITION (t.ts >= q.ts) ON t.sym = q.sym ORDER BY t.sym, t.ts")
	// Esperado:
	//  AAA 90  -> NULL  (no hay quote AAA con ts<=90)
	//  AAA 100 -> 55    (quote ts=100)
	//  AAA 250 -> 60    (quote ts=200, la más reciente <=250)
	//  BBB 120 -> 33    (quote ts=110)
	want := [][3]string{
		{"AAA", "90", "<nil>"},
		{"AAA", "100", "55"},
		{"AAA", "250", "60"},
		{"BBB", "120", "33"},
	}
	if len(r.Rows) != len(want) {
		log.Fatalf("ASOF ON: esperaba %d filas, got %d (%+v)", len(want), len(r.Rows), r.Rows)
	}
	for i, row := range r.Rows {
		got := [3]string{fmt.Sprintf("%v", row[0]), fmt.Sprintf("%v", row[1]), fmt.Sprintf("%v", row[2])}
		if got != want[i] {
			log.Fatalf("ASOF ON fila %d: esperaba %v, got %v", i, want[i], got)
		}
	}
	fmt.Println("OK  cada trade emparejó la quote vigente del mismo símbolo (o NULL)")

	fmt.Println("=== ASOF JOIN sin ON (solo MATCH_CONDITION) ===")
	// Sin ON cruza símbolos: para cada trade, la quote con mayor ts <= trade.ts.
	r = run(ctx, e, "SELECT t.sym, t.ts, q.sym, q.ts, q.price FROM trades t ASOF JOIN quotes q MATCH_CONDITION (t.ts >= q.ts) ORDER BY t.ts")
	// trade ts=90 -> sin quote <=90 -> NULL; ts=100 -> quote ts=100; ts=120 -> quote ts=110; ts=250 -> quote ts=200
	byTs := map[string]string{}
	for _, row := range r.Rows {
		byTs[fmt.Sprintf("%v", row[1])] = fmt.Sprintf("%v", row[3]) // trade.ts -> quote.ts
	}
	if byTs["90"] != "<nil>" || byTs["100"] != "100" || byTs["120"] != "110" || byTs["250"] != "200" {
		log.Fatalf("ASOF sin ON: %v", byTs)
	}
	fmt.Println("OK  sin ON toma la quote más reciente global <= ts")

	fmt.Println("=== ASOF con <= (nearest-later) ===")
	// Con <=: la quote con menor ts >= trade.ts.
	r = run(ctx, e, "SELECT t.ts, q.ts FROM trades t ASOF JOIN quotes q MATCH_CONDITION (t.ts <= q.ts) ON t.sym = q.sym ORDER BY t.sym, t.ts")
	// AAA 90->100, AAA 100->100, AAA 250-> NULL (no quote AAA ts>=250), BBB 120-> NULL (quote BBB ts=110 <120)
	pairs := [][2]string{}
	for _, row := range r.Rows {
		pairs = append(pairs, [2]string{fmt.Sprintf("%v", row[0]), fmt.Sprintf("%v", row[1])})
	}
	wantPairs := [][2]string{{"90", "100"}, {"100", "100"}, {"250", "<nil>"}, {"120", "<nil>"}}
	if len(pairs) != len(wantPairs) {
		log.Fatalf("ASOF <=: filas %+v", pairs)
	}
	for i := range wantPairs {
		if pairs[i] != wantPairs[i] {
			log.Fatalf("ASOF <= fila %d: esperaba %v, got %v", i, wantPairs[i], pairs[i])
		}
	}
	fmt.Println("OK  <= toma la quote más cercana posterior (o NULL)")

	fmt.Println("=== Todo OK ===")
}
