package server

import (
	"strings"
	"testing"
)

func TestSplitStatementsDollarQuote(t *testing.T) {
	// A dollar-quoted body with internal semicolons must stay a single statement.
	q := `CREATE TABLE t (id INT);
CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    INSERT INTO audit (msg) VALUES ('x');
    RETURN NEW;
END;
$function$;
INSERT INTO t (id) VALUES (1);`
	got := splitStatements(q)
	if len(got) != 3 {
		t.Fatalf("expected 3 statements, got %d: %#v", len(got), got)
	}
	if !strings.HasPrefix(got[0], "CREATE TABLE") {
		t.Fatalf("stmt0 = %q", got[0])
	}
	if !strings.HasPrefix(got[1], "CREATE FUNCTION") || !strings.Contains(got[1], "RETURN NEW;") || !strings.Contains(got[1], "$function$") {
		t.Fatalf("CREATE FUNCTION was split or mangled: %q", got[1])
	}
	if !strings.HasPrefix(got[2], "INSERT INTO t") {
		t.Fatalf("stmt2 = %q", got[2])
	}
}

func TestSplitStatementsUntagged(t *testing.T) {
	q := `CREATE PROCEDURE p() AS $$ BEGIN INSERT INTO t (id) VALUES (1); END; $$; SELECT 1;`
	got := splitStatements(q)
	if len(got) != 2 {
		t.Fatalf("expected 2 statements, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[0], "END;") {
		t.Fatalf("untagged dollar body was split: %q", got[0])
	}
}

func TestSplitStatementsStringLiteralSemicolon(t *testing.T) {
	q := `INSERT INTO t (s) VALUES ('a;b'); SELECT 1;`
	got := splitStatements(q)
	if len(got) != 2 {
		t.Fatalf("expected 2 statements, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[0], "'a;b'") {
		t.Fatalf("semicolon inside string literal split the statement: %q", got[0])
	}
}
