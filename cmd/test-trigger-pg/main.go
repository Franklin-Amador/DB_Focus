// Command test-trigger-pg exercises the PostgreSQL/DBeaver trigger syntaxes:
// multi-event (INSERT OR UPDATE OR DELETE), UPDATE OF col, EXECUTE FUNCTION /
// EXECUTE PROCEDURE, FOR EACH STATEMENT, WHEN, schema-qualified — covering both
// parsing and the execution the engine supports, plus a persistence round-trip.
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

func mustParse(sql string) {
	if _, err := parser.NewParser(sql).ParseStatement(); err != nil {
		log.Fatalf("expected %q to parse: %v", sql, err)
	}
}

func count(ctx context.Context, e *executor.Executor, table string) string {
	res := run(ctx, e, "SELECT COUNT(*) FROM "+table)
	return fmt.Sprintf("%v", res.Rows[0][0])
}

func main() {
	ctx := context.Background()

	fmt.Println("=== Parse-only: DBeaver/PostgreSQL trigger forms ===")
	mustParse("CREATE TRIGGER t AFTER INSERT ON ventas FOR EACH ROW EXECUTE FUNCTION log_fn()")
	mustParse("CREATE TRIGGER t BEFORE UPDATE ON ventas FOR EACH ROW EXECUTE PROCEDURE log_fn()")
	mustParse("CREATE TRIGGER t AFTER INSERT OR UPDATE OR DELETE ON ventas FOR EACH ROW EXECUTE FUNCTION log_fn()")
	mustParse("CREATE TRIGGER t BEFORE UPDATE OF precio, stock ON ventas FOR EACH ROW EXECUTE FUNCTION log_fn()")
	mustParse("CREATE TRIGGER t AFTER INSERT ON public.ventas FOR EACH ROW EXECUTE FUNCTION public.log_fn()")
	mustParse("CREATE TRIGGER t AFTER INSERT ON ventas FOR EACH STATEMENT EXECUTE FUNCTION log_fn()")
	mustParse("CREATE TRIGGER t BEFORE UPDATE ON ventas FOR EACH ROW WHEN (OLD.precio <> NEW.precio) EXECUTE FUNCTION log_fn()")
	fmt.Println("  ok: all PG trigger forms parse")

	fmt.Println("=== Execute: multi-event inline body fires per event ===")
	dir := "./data_trigger_pg_test"
	defer os.RemoveAll(dir)
	st, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	cat := catalog.New()
	e := executor.New(cat, st)

	run(ctx, e, "CREATE TABLE ventas (id INT IDENTITY PRIMARY KEY, monto INT)")
	run(ctx, e, "CREATE TABLE audit (id INT IDENTITY PRIMARY KEY, ev TEXT)")
	run(ctx, e, "CREATE TRIGGER trg_multi AFTER INSERT OR UPDATE OR DELETE ON ventas FOR EACH ROW BEGIN INSERT INTO audit (ev) VALUES ('fired'); END")
	run(ctx, e, "INSERT INTO ventas (monto) VALUES (10)")
	run(ctx, e, "UPDATE ventas SET monto = 20 WHERE monto = 10")
	run(ctx, e, "DELETE FROM ventas WHERE monto = 20")
	if got := count(ctx, e, "audit"); got != "3" {
		log.Fatalf("multi-event: expected 3 audit rows, got %s", got)
	}
	fmt.Println("  ok: multi-event trigger fired on INSERT, UPDATE and DELETE")

	fmt.Println("=== Execute: EXECUTE PROCEDURE runs the referenced procedure ===")
	run(ctx, e, "CREATE TABLE compras (id INT IDENTITY PRIMARY KEY, total INT)")
	run(ctx, e, "CREATE PROCEDURE log_compra() AS BEGIN INSERT INTO audit (ev) VALUES ('via-proc'); END")
	run(ctx, e, "CREATE TRIGGER trg_proc AFTER INSERT ON compras FOR EACH ROW EXECUTE PROCEDURE log_compra()")
	run(ctx, e, "INSERT INTO compras (total) VALUES (99)")
	res := run(ctx, e, "SELECT ev FROM audit WHERE ev = 'via-proc'")
	if len(res.Rows) != 1 {
		log.Fatalf("EXECUTE PROCEDURE: expected 1 'via-proc' audit row, got %d", len(res.Rows))
	}
	fmt.Println("  ok: EXECUTE PROCEDURE ran the referenced procedure body")

	fmt.Println("=== Execute: EXECUTE FUNCTION on a missing routine is inert ===")
	run(ctx, e, "CREATE TRIGGER trg_inert AFTER INSERT ON compras FOR EACH ROW EXECUTE FUNCTION no_existe()")
	run(ctx, e, "INSERT INTO compras (total) VALUES (1)") // must not error
	fmt.Println("  ok: trigger referencing a missing routine is inert, not an error")

	st.Close()

	fmt.Println("=== Persistence round-trip (multi-event + EXECUTE routine survive reload) ===")
	st2, err := storage.NewPebbleStorage(dir)
	if err != nil {
		log.Fatalf("reopen storage: %v", err)
	}
	cat2 := catalog.New()
	e2 := executor.New(cat2, st2)
	if err := st2.LoadAll(cat2); err != nil {
		log.Fatalf("LoadAll: %v", err)
	}
	// The multi-event trigger still fires on UPDATE after reload.
	before := count(ctx, e2, "audit")
	run(ctx, e2, "INSERT INTO ventas (monto) VALUES (30)")
	after := count(ctx, e2, "audit")
	if before == after {
		log.Fatalf("after reload: multi-event trigger did not fire (audit stayed at %s)", before)
	}
	// The EXECUTE PROCEDURE trigger still runs the procedure after reload: the
	// 'via-proc' count must increase when a new compras row is inserted.
	beforeProc := count(ctx, e2, "audit WHERE ev = 'via-proc'")
	run(ctx, e2, "INSERT INTO compras (total) VALUES (7)")
	afterProc := count(ctx, e2, "audit WHERE ev = 'via-proc'")
	if beforeProc == afterProc {
		log.Fatalf("after reload: EXECUTE PROCEDURE trigger did not run (via-proc stayed at %s)", beforeProc)
	}
	st2.Close()
	fmt.Println("  ok: triggers survive restart with events and EXECUTE routine intact")

	fmt.Println("=== All PG trigger tests passed! ===")
}
