// test-range-index verifies range (<, <=, >, >=) and BETWEEN predicates,
// including index-backed lookups. It proves correctness by running every query
// against two identical tables — one with an index on the filtered column, one
// without — and asserting the result sets match exactly (so the index path and
// the scan path agree). It also checks numeric vs lexicographic ordering, index
// maintenance across INSERT/UPDATE/DELETE, duplicates, empty ranges, and that
// the index still serves ranges after a reload.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"

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

// ids returns the sorted (numeric) list of the first column of a result.
func ids(r *executor.Result) []int {
	out := make([]int, 0, len(r.Rows))
	for _, row := range r.Rows {
		n, _ := strconv.Atoi(fmt.Sprintf("%v", row[0]))
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

func eq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// both runs the SELECT id-projection of a predicate against the indexed table
// (idx) and the unindexed twin (plain), asserts they match, and returns the ids.
func both(ctx context.Context, e *executor.Executor, pred string) []int {
	idxRes := ids(run(ctx, e, "SELECT id FROM idx WHERE "+pred))
	plainRes := ids(run(ctx, e, "SELECT id FROM plain WHERE "+pred))
	if !eq(idxRes, plainRes) {
		log.Fatalf("MISMATCH for %q: indexed=%v scan=%v", pred, idxRes, plainRes)
	}
	return idxRes
}

func wantIDs(ctx context.Context, e *executor.Executor, pred string, want []int) {
	got := both(ctx, e, pred)
	if !eq(got, want) {
		log.Fatalf("%q: esperaba %v, got %v", pred, want, got)
	}
	fmt.Printf("OK  %-28s -> %v\n", pred, got)
}

func main() {
	ctx := context.Background()
	cat := catalog.New()
	e := executor.New(cat, nil)

	// Two identical tables; only `idx` has an index on n.
	for _, t := range []string{"idx", "plain"} {
		run(ctx, e, "CREATE TABLE "+t+" (id INT PRIMARY KEY, n INT, etiqueta TEXT)")
	}
	// n = id here; values 1..20 (note: 10..20 sort BEFORE 9 lexicographically).
	for i := 1; i <= 20; i++ {
		vals := fmt.Sprintf("(%d, %d, 'r%d')", i, i, i)
		run(ctx, e, "INSERT INTO idx VALUES "+vals)
		run(ctx, e, "INSERT INTO plain VALUES "+vals)
	}
	run(ctx, e, "CREATE INDEX idx_n ON idx (n)")

	fmt.Println("=== Rangos básicos (orden numérico, no lexicográfico) ===")
	// If the index ordered keys as strings, n > 9 would drop 10..19.
	wantIDs(ctx, e, "n > 9", []int{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
	wantIDs(ctx, e, "n >= 15", []int{15, 16, 17, 18, 19, 20})
	wantIDs(ctx, e, "n < 3", []int{1, 2})
	wantIDs(ctx, e, "n <= 2", []int{1, 2})

	fmt.Println("=== BETWEEN ===")
	wantIDs(ctx, e, "n BETWEEN 5 AND 8", []int{5, 6, 7, 8})
	wantIDs(ctx, e, "n BETWEEN 19 AND 100", []int{19, 20})
	wantIDs(ctx, e, "n BETWEEN 10 AND 5", []int{}) // rango vacío

	fmt.Println("=== Mantenimiento del índice tras INSERT/UPDATE/DELETE ===")
	run(ctx, e, "INSERT INTO idx VALUES (21, 25, 'r25')")
	run(ctx, e, "INSERT INTO plain VALUES (21, 25, 'r25')")
	wantIDs(ctx, e, "n >= 20", []int{20, 21}) // id 21 tiene n=25

	run(ctx, e, "UPDATE idx SET n = 1 WHERE id = 20")
	run(ctx, e, "UPDATE plain SET n = 1 WHERE id = 20")
	wantIDs(ctx, e, "n <= 1", []int{1, 20}) // id 20 ahora n=1

	run(ctx, e, "DELETE FROM idx WHERE id = 21")
	run(ctx, e, "DELETE FROM plain WHERE id = 21")
	wantIDs(ctx, e, "n > 18", []int{19}) // ya no está el n=25

	fmt.Println("=== Valores duplicados en la columna indexada ===")
	run(ctx, e, "CREATE TABLE idx2 (id INT PRIMARY KEY, grupo INT)")
	run(ctx, e, "CREATE TABLE plain2 (id INT PRIMARY KEY, grupo INT)")
	// grupos: varias filas con el mismo valor
	for i, g := range []int{1, 1, 2, 2, 2, 3, 3, 3, 3} {
		vals := fmt.Sprintf("(%d, %d)", i+1, g)
		run(ctx, e, "INSERT INTO idx2 VALUES "+vals)
		run(ctx, e, "INSERT INTO plain2 VALUES "+vals)
	}
	run(ctx, e, "CREATE INDEX idx2_g ON idx2 (grupo)")
	a := ids(run(ctx, e, "SELECT id FROM idx2 WHERE grupo >= 2"))
	b := ids(run(ctx, e, "SELECT id FROM plain2 WHERE grupo >= 2"))
	if !eq(a, b) || len(a) != 7 {
		log.Fatalf("duplicados: indexed=%v scan=%v (esperaba 7 filas)", a, b)
	}
	fmt.Printf("OK  grupo >= 2 con duplicados -> %d filas, index == scan\n", len(a))

	fmt.Println("=== Persistencia: el rango sigue andando tras reload ===")
	testRangePersistence(ctx)

	fmt.Println("=== Todo OK ===")
}

func testRangePersistence(ctx context.Context) {
	dir := "./data_test_range_index"
	defer os.RemoveAll(dir)

	st1, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	cat1 := catalog.New()
	e1 := executor.New(cat1, st1)
	run(ctx, e1, "CREATE TABLE t (id INT PRIMARY KEY, n INT)")
	for i := 1; i <= 10; i++ {
		run(ctx, e1, fmt.Sprintf("INSERT INTO t VALUES (%d, %d)", i, i*10))
	}
	run(ctx, e1, "CREATE INDEX t_n ON t (n)")
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
	got := ids(run(ctx, e2, "SELECT id FROM t WHERE n BETWEEN 30 AND 70"))
	want := []int{3, 4, 5, 6, 7}
	if !eq(got, want) {
		log.Fatalf("persistencia: esperaba %v, got %v", want, got)
	}
	fmt.Printf("OK  BETWEEN tras reload -> %v (índice rehidratado y ordenado)\n", got)
}
