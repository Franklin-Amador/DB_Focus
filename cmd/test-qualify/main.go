// Command test-qualify exercises window functions (ROW_NUMBER, RANK,
// DENSE_RANK, aggregates OVER), the QUALIFY clause and HAVING end to end
// against an in-memory catalog: partitions, ordering, peers, the default
// cumulative frame, QUALIFY by alias and inline, combination with GROUP BY,
// JOINs, CTEs and error reporting.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
)

func run(ctx context.Context, exe *executor.Executor, sql string) (*executor.Result, error) {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return exe.Execute(ctx, stmt)
}

func query(ctx context.Context, exe *executor.Executor, sql string) *executor.Result {
	res, err := run(ctx, exe, sql)
	if err != nil {
		log.Fatalf("%q: %v", sql, err)
	}
	return res
}

// rowsText renders every row as "v1|v2|..." for compact comparisons.
func rowsText(res *executor.Result) []string {
	out := make([]string, 0, len(res.Rows))
	for _, row := range res.Rows {
		parts := make([]string, len(row))
		for i, v := range row {
			parts[i] = fmt.Sprintf("%v", v)
		}
		out = append(out, strings.Join(parts, "|"))
	}
	return out
}

func expectRows(ctx context.Context, exe *executor.Executor, sql string, want []string) {
	res := query(ctx, exe, sql)
	got := rowsText(res)
	if strings.Join(got, ";") != strings.Join(want, ";") {
		log.Fatalf("%q:\n  want %v\n  got  %v", sql, want, got)
	}
	fmt.Printf("OK  %-95s -> %d rows\n", sql, len(got))
}

func expectColumns(ctx context.Context, exe *executor.Executor, sql string, want []string) {
	res := query(ctx, exe, sql)
	if strings.Join(res.Columns, ",") != strings.Join(want, ",") {
		log.Fatalf("%q: columns want %v, got %v", sql, want, res.Columns)
	}
	fmt.Printf("OK  %-95s -> columns %v\n", sql, res.Columns)
}

func expectErr(ctx context.Context, exe *executor.Executor, sql, fragment string) {
	_, err := run(ctx, exe, sql)
	if err == nil {
		log.Fatalf("%q: expected error containing %q, got success", sql, fragment)
	}
	if !strings.Contains(err.Error(), fragment) {
		log.Fatalf("%q: expected error containing %q, got %v", sql, fragment, err)
	}
	fmt.Printf("OK  %-95s -> error: %v\n", sql, err)
}

func main() {
	fmt.Println("=== Testing window functions + QUALIFY ===")

	cat := catalog.New()
	exe := executor.New(cat, nil)
	ctx := context.Background()

	query(ctx, exe, "CREATE TABLE ventas (id INT IDENTITY PRIMARY KEY, categoria TEXT, monto INT)")
	for _, ins := range []string{
		"INSERT INTO ventas (categoria, monto) VALUES ('A', 100)",
		"INSERT INTO ventas (categoria, monto) VALUES ('A', 200)",
		"INSERT INTO ventas (categoria, monto) VALUES ('B', 100)",
		"INSERT INTO ventas (categoria, monto) VALUES ('B', 300)",
		"INSERT INTO ventas (categoria, monto) VALUES ('B', 300)",
		"INSERT INTO ventas (categoria, monto) VALUES ('C', 50)",
	} {
		query(ctx, exe, ins)
	}
	query(ctx, exe, "CREATE TABLE categorias (nombre TEXT PRIMARY KEY, region TEXT)")
	query(ctx, exe, "INSERT INTO categorias (nombre, region) VALUES ('A', 'norte')")
	query(ctx, exe, "INSERT INTO categorias (nombre, region) VALUES ('B', 'sur')")
	query(ctx, exe, "INSERT INTO categorias (nombre, region) VALUES ('C', 'norte')")

	fmt.Println("\n--- Ranking functions ---")
	expectRows(ctx, exe,
		"SELECT categoria, monto, ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) AS rn FROM ventas ORDER BY categoria, rn",
		[]string{"A|200|1", "A|100|2", "B|300|1", "B|300|2", "B|100|3", "C|50|1"})
	expectRows(ctx, exe,
		"SELECT categoria, monto, RANK() OVER (PARTITION BY categoria ORDER BY monto DESC) AS r FROM ventas WHERE categoria = 'B' ORDER BY r, id",
		[]string{"B|300|1", "B|300|1", "B|100|3"})
	expectRows(ctx, exe,
		"SELECT categoria, monto, DENSE_RANK() OVER (PARTITION BY categoria ORDER BY monto DESC) AS dr FROM ventas WHERE categoria = 'B' ORDER BY dr, id",
		[]string{"B|300|1", "B|300|1", "B|100|2"})
	// Global ranking (no PARTITION BY), ties on 300 and 100.
	expectRows(ctx, exe,
		"SELECT monto, RANK() OVER (ORDER BY monto DESC) AS r, DENSE_RANK() OVER (ORDER BY monto DESC) AS dr FROM ventas ORDER BY r, id",
		[]string{"300|1|1", "300|1|1", "200|3|2", "100|4|3", "100|4|3", "50|6|4"})
	// Unaliased window column gets a generated name.
	expectColumns(ctx, exe, "SELECT categoria, ROW_NUMBER() OVER (ORDER BY id) FROM ventas", []string{"categoria", "expr2"})

	fmt.Println("\n--- Aggregates OVER ---")
	expectRows(ctx, exe,
		"SELECT categoria, monto, SUM(monto) OVER (PARTITION BY categoria) AS total_cat, COUNT(*) OVER () AS n FROM ventas ORDER BY id",
		[]string{"A|100|300|6", "A|200|300|6", "B|100|700|6", "B|300|700|6", "B|300|700|6", "C|50|50|6"})
	// Default frame with ORDER BY: cumulative, peers share the value.
	expectRows(ctx, exe,
		"SELECT categoria, monto, SUM(monto) OVER (PARTITION BY categoria ORDER BY monto) AS acumulado FROM ventas ORDER BY categoria, monto, id",
		[]string{"A|100|100", "A|200|300", "B|100|100", "B|300|700", "B|300|700", "C|50|50"})
	expectRows(ctx, exe,
		"SELECT categoria, monto, MAX(monto) OVER (PARTITION BY categoria) AS maximo, AVG(monto) OVER (PARTITION BY categoria) AS media FROM ventas WHERE categoria = 'A' ORDER BY id",
		[]string{"A|100|200|150", "A|200|200|150"})

	fmt.Println("\n--- QUALIFY ---")
	expectRows(ctx, exe,
		"SELECT categoria, monto, ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) AS rn FROM ventas QUALIFY rn = 1 ORDER BY categoria",
		[]string{"A|200|1", "B|300|1", "C|50|1"})
	expectRows(ctx, exe,
		"SELECT categoria, monto FROM ventas QUALIFY ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) = 1 ORDER BY categoria",
		[]string{"A|200", "B|300", "C|50"})
	expectColumns(ctx, exe,
		"SELECT categoria, monto FROM ventas QUALIFY ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) = 1",
		[]string{"categoria", "monto"})
	// SELECT * must not leak the hidden window column.
	expectColumns(ctx, exe,
		"SELECT * FROM ventas QUALIFY ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) = 1",
		[]string{"id", "categoria", "monto"})
	expectRows(ctx, exe,
		"SELECT * FROM ventas QUALIFY ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) = 1 ORDER BY id",
		[]string{"2|A|200", "4|B|300", "6|C|50"})
	// Compound predicate mixing an inline window and a plain column.
	expectRows(ctx, exe,
		"SELECT categoria, monto FROM ventas QUALIFY ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) <= 2 AND monto > 60 ORDER BY categoria, monto DESC",
		[]string{"A|200", "A|100", "B|300", "B|300"})
	// QUALIFY + WHERE + ORDER BY + LIMIT.
	expectRows(ctx, exe,
		"SELECT categoria, monto, ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) AS rn FROM ventas WHERE monto > 60 QUALIFY rn <= 2 ORDER BY monto DESC, categoria LIMIT 2",
		[]string{"B|300|1", "B|300|2"})
	// QUALIFY over a plain alias (lenient extension: no window involved).
	expectRows(ctx, exe,
		"SELECT categoria, monto AS importe FROM ventas QUALIFY importe >= 200 ORDER BY id",
		[]string{"A|200", "B|300", "B|300"})
	// DISTINCT after QUALIFY.
	expectRows(ctx, exe,
		"SELECT DISTINCT categoria FROM ventas QUALIFY ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) <= 2 ORDER BY categoria",
		[]string{"A", "B", "C"})

	fmt.Println("\n--- Windows over GROUP BY ---")
	expectRows(ctx, exe,
		"SELECT categoria, SUM(monto) AS total, RANK() OVER (ORDER BY SUM(monto) DESC) AS pos FROM ventas GROUP BY categoria QUALIFY pos <= 2 ORDER BY pos",
		[]string{"B|700|1", "A|300|2"})
	// Aggregate referenced only inside the window (not projected).
	expectRows(ctx, exe,
		"SELECT categoria, RANK() OVER (ORDER BY SUM(monto) DESC) AS pos FROM ventas GROUP BY categoria ORDER BY pos",
		[]string{"B|1", "A|2", "C|3"})
	// Window referencing the aggregate through its alias.
	expectRows(ctx, exe,
		"SELECT categoria, COUNT(*) AS n, DENSE_RANK() OVER (ORDER BY n DESC) AS pos FROM ventas GROUP BY categoria ORDER BY pos, categoria",
		[]string{"B|3|1", "A|2|2", "C|1|3"})
	// Share of total: aggregate OVER () on grouped rows.
	expectRows(ctx, exe,
		"SELECT categoria, SUM(monto) AS total, SUM(SUM(monto)) OVER () AS gran_total FROM ventas GROUP BY categoria ORDER BY categoria",
		[]string{"A|300|1050", "B|700|1050", "C|50|1050"})
	// ORDER BY an aggregate expression and QUALIFY on an aggregate (HAVING-like).
	expectRows(ctx, exe,
		"SELECT categoria, SUM(monto) FROM ventas GROUP BY categoria ORDER BY SUM(monto) DESC",
		[]string{"B|700", "A|300", "C|50"})
	expectRows(ctx, exe,
		"SELECT categoria FROM ventas GROUP BY categoria QUALIFY SUM(monto) > 100 ORDER BY categoria",
		[]string{"A", "B"})
	// Existing aggregate behaviour untouched.
	expectRows(ctx, exe, "SELECT COUNT(*), SUM(monto), MIN(monto), MAX(monto) FROM ventas", []string{"6|1050|50|300"})
	expectRows(ctx, exe, "SELECT categoria, COUNT(*) AS n FROM ventas GROUP BY categoria ORDER BY categoria", []string{"A|2", "B|3", "C|1"})

	fmt.Println("\n--- JOINs ---")
	expectRows(ctx, exe,
		"SELECT c.region, v.categoria, v.monto, RANK() OVER (PARTITION BY c.region ORDER BY v.monto DESC) AS r FROM ventas AS v INNER JOIN categorias AS c ON v.categoria = c.nombre QUALIFY r = 1 ORDER BY c.region, v.id",
		[]string{"norte|A|200|1", "sur|B|300|1", "sur|B|300|1"})
	expectRows(ctx, exe,
		"SELECT region, SUM(monto) AS total, RANK() OVER (ORDER BY SUM(monto) DESC) AS pos FROM ventas INNER JOIN categorias ON ventas.categoria = categorias.nombre GROUP BY region ORDER BY pos",
		[]string{"sur|700|1", "norte|350|2"})
	expectColumns(ctx, exe,
		"SELECT * FROM ventas AS v INNER JOIN categorias AS c ON v.categoria = c.nombre QUALIFY ROW_NUMBER() OVER (PARTITION BY c.region ORDER BY v.monto DESC) = 1",
		[]string{"v.id", "v.categoria", "v.monto", "c.nombre", "c.region"})

	fmt.Println("\n--- CTE + view ---")
	expectRows(ctx, exe,
		"WITH top AS (SELECT categoria, monto FROM ventas QUALIFY ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) = 1) SELECT COUNT(*) FROM top",
		[]string{"3"})
	query(ctx, exe, "CREATE VIEW mejores AS SELECT categoria, monto, ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) AS rn FROM ventas QUALIFY rn = 1")
	expectRows(ctx, exe, "SELECT categoria, monto FROM mejores ORDER BY categoria", []string{"A|200", "B|300", "C|50"})
	expectRows(ctx, exe, "SELECT categoria, monto FROM mejores ORDER BY categoria", []string{"A|200", "B|300", "C|50"}) // view AST reused: must not be mutated

	fmt.Println("\n--- HAVING ---")
	expectRows(ctx, exe,
		"SELECT categoria, SUM(monto) AS total FROM ventas GROUP BY categoria HAVING SUM(monto) > 100 ORDER BY categoria",
		[]string{"A|300", "B|700"})
	// Alias, unprojected aggregate, compound predicate.
	expectRows(ctx, exe,
		"SELECT categoria, COUNT(*) AS n FROM ventas GROUP BY categoria HAVING n >= 2 AND MAX(monto) < 250 ORDER BY categoria",
		[]string{"A|2"})
	// HAVING without GROUP BY: the whole table is one group.
	expectRows(ctx, exe, "SELECT COUNT(*) FROM ventas HAVING COUNT(*) > 3", []string{"6"})
	expectRows(ctx, exe, "SELECT COUNT(*) FROM ventas HAVING COUNT(*) > 30", []string{})
	// HAVING on a GROUP BY key, then window + QUALIFY over the surviving groups.
	expectRows(ctx, exe,
		"SELECT categoria, SUM(monto) AS total, RANK() OVER (ORDER BY SUM(monto) DESC) AS pos FROM ventas GROUP BY categoria HAVING categoria <> 'B' QUALIFY pos = 1",
		[]string{"A|300|1"})
	// HAVING over a JOIN with qualified refs, then ORDER BY + LIMIT.
	expectRows(ctx, exe,
		"SELECT c.region, SUM(v.monto) AS total FROM ventas AS v INNER JOIN categorias AS c ON v.categoria = c.nombre GROUP BY c.region HAVING SUM(v.monto) >= 350 ORDER BY total DESC LIMIT 1",
		[]string{"sur|700"})
	expectErr(ctx, exe, "SELECT categoria, SUM(monto) FROM ventas GROUP BY categoria HAVING monto > 100", "must appear in GROUP BY")
	expectErr(ctx, exe, "SELECT categoria FROM ventas GROUP BY categoria HAVING zzz > 1", "column zzz not found")

	fmt.Println("\n--- Review regressions ---")
	// An alias equal to a source column must not shadow it (positional projection).
	expectRows(ctx, exe,
		"SELECT monto, ROW_NUMBER() OVER (ORDER BY id) AS monto FROM ventas WHERE categoria = 'A' ORDER BY id",
		[]string{"100|1", "200|2"})
	expectRows(ctx, exe,
		"SELECT categoria AS monto, monto FROM ventas GROUP BY categoria ORDER BY categoria",
		[]string{"A|100", "B|100", "C|50"})
	// A window wrapped in an expression is unsupported: NULL placeholder, never a bare window value.
	expectRows(ctx, exe,
		"SELECT categoria, ROW_NUMBER() OVER (ORDER BY id) * 2 AS x FROM ventas WHERE categoria = 'C'",
		[]string{"C|<nil>"})
	// ORDER BY on a placeholder alias is ignored instead of failing.
	expectRows(ctx, exe,
		"SELECT 1 AS one, categoria FROM ventas WHERE categoria = 'A' ORDER BY one",
		[]string{"<nil>|A", "<nil>|A"})
	// Cumulative accumulators for every aggregate kind.
	expectRows(ctx, exe,
		"SELECT monto, COUNT(*) OVER (ORDER BY monto) AS c, AVG(monto) OVER (ORDER BY monto) AS av, MAX(monto) OVER (ORDER BY monto) AS mx, MIN(monto) OVER (ORDER BY monto DESC) AS mn FROM ventas WHERE categoria = 'A' ORDER BY monto",
		[]string{"100|1|100|100|100", "200|2|150|200|200"})
	// Aggregates outside the select list need a grouped query; non-grouped columns
	// referenced by windows/ORDER BY/QUALIFY on a grouped query are rejected.
	expectErr(ctx, exe, "SELECT categoria, monto FROM ventas ORDER BY COUNT(*)", "requires GROUP BY")
	expectErr(ctx, exe, "SELECT categoria, monto FROM ventas QUALIFY SUM(monto) > 0", "requires GROUP BY")
	expectErr(ctx, exe, "SELECT categoria, SUM(monto) OVER (PARTITION BY categoria) AS t FROM ventas GROUP BY categoria", "must appear in GROUP BY")
	expectErr(ctx, exe, "SELECT categoria, SUM(monto) AS total FROM ventas GROUP BY categoria ORDER BY monto", "must appear in GROUP BY")
	// Ambiguity is reported even when another item enabled the NULL placeholder mode.
	query(ctx, exe, "CREATE TABLE otra (id INT, nota TEXT)")
	query(ctx, exe, "INSERT INTO otra (id, nota) VALUES (100, 'x')")
	expectErr(ctx, exe, "SELECT id, CAST(id AS TEXT) AS x FROM ventas INNER JOIN otra ON ventas.monto = otra.id", "ambiguous")
	expectErr(ctx, exe, "SELECT COUNT(*) FROM ventas INNER JOIN otra ON ventas.monto = otra.id GROUP BY id", "GROUP BY: ambiguous")

	fmt.Println("\n--- Errors ---")
	expectErr(ctx, exe, "SELECT ROW_NUMBER() OVER (PARTITION BY nope) AS rn FROM ventas", "nope")
	expectErr(ctx, exe, "SELECT categoria FROM ventas QUALIFY zzz = 1", "zzz")
	expectErr(ctx, exe, "SELECT SUM(nope) OVER () AS s FROM ventas", "nope")
	expectErr(ctx, exe, "SELECT categoria FROM ventas ORDER BY nope", "nope")
	expectErr(ctx, exe, "SELECT ROW_NUMBER() AS rn FROM ventas", "OVER")

	fmt.Println("\n=== All window/QUALIFY/HAVING tests passed! ===")
}
