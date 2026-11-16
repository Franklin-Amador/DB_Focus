package server

import (
	"testing"

	"dbf/internal/catalog"
)

// countingHandler records how many times the executor path runs, so a test can
// assert that Describe does not execute a statement that Execute will run.
type countingHandler struct{ calls int }

func (h *countingHandler) Handle(query string) (*QueryResult, error) {
	h.calls++
	return &QueryResult{Tag: "INSERT 0 1"}, nil
}

// noSystemCatalog never claims a query, so computeExtended falls through to the
// executor (the path we want to guard).
type noSystemCatalog struct{}

func (noSystemCatalog) DatabaseExists(string) bool            { return true }
func (noSystemCatalog) RegisterUser(string, bool) error       { return nil }
func (noSystemCatalog) HandleSystemQueryForSession(string, string, string) (*catalog.SystemResult, bool) {
	return nil, false
}

func TestIsReadOnlyQuery(t *testing.T) {
	readOnly := []string{
		"SELECT 1", "select * from t", "  \n WITH a AS (SELECT 1) SELECT * FROM a",
		"(SELECT 1)", "VALUES (1)", "TABLE t", "SHOW search_path", "EXPLAIN SELECT 1",
		"DESCRIBE t", "desc t",
	}
	mutating := []string{
		"INSERT INTO t VALUES (1)", "UPDATE t SET a=1", "DELETE FROM t",
		"CREATE TABLE t (id INT)", "DROP TABLE t", "ALTER TABLE t ADD COLUMN c INT",
		"CALL p()", "SET search_path TO x", "TRUNCATE t",
	}
	for _, q := range readOnly {
		if !isReadOnlyQuery(q) {
			t.Errorf("isReadOnlyQuery(%q) = false, want true", q)
		}
	}
	for _, q := range mutating {
		if isReadOnlyQuery(q) {
			t.Errorf("isReadOnlyQuery(%q) = true, want false", q)
		}
	}
}

// TestDescribeDoesNotExecuteMutation is the regression for the double-execution
// finding: a Describe of a state-changing statement must not run it (Execute
// runs it exactly once).
func TestDescribeDoesNotExecuteMutation(t *testing.T) {
	h := &countingHandler{}
	sess := &session{database: "postgres", schema: "public"}
	const insert = "INSERT INTO t VALUES (1)"

	// Describe pass: must not touch the executor.
	if _, _, _, kind, err := computeExtended(h, noSystemCatalog{}, insert, sess, true); err != nil {
		t.Fatalf("describe err: %v", err)
	} else if kind != "skip" {
		t.Fatalf("describe kind = %q, want skip", kind)
	}
	if h.calls != 0 {
		t.Fatalf("executor ran during Describe: %d calls", h.calls)
	}

	// Execute pass: runs it exactly once.
	if _, _, _, _, err := computeExtended(h, noSystemCatalog{}, insert, sess, false); err != nil {
		t.Fatalf("execute err: %v", err)
	}
	if h.calls != 1 {
		t.Fatalf("executor calls after Execute = %d, want 1", h.calls)
	}
}

// TestDescribeRunsReadOnly confirms a SELECT still resolves at Describe (so the
// RowDescription can be emitted for DBeaver's metadata queries).
func TestDescribeRunsReadOnly(t *testing.T) {
	h := &countingHandler{}
	sess := &session{database: "postgres", schema: "public"}
	if _, _, _, kind, err := computeExtended(h, noSystemCatalog{}, "SELECT * FROM t", sess, true); err != nil {
		t.Fatalf("describe err: %v", err)
	} else if kind != "result" {
		t.Fatalf("describe kind = %q, want result", kind)
	}
	if h.calls != 1 {
		t.Fatalf("read-only Describe should execute once, got %d", h.calls)
	}
}

func TestCountParamsClamp(t *testing.T) {
	cases := map[string]int{
		"SELECT $1, $2":        2,
		"SELECT $3":            3,
		"no params here":       0,
		"SELECT $900000000":    maxWireParams, // clamped, not 900M
		"WHERE a=$1 AND b=$10": 10,
	}
	for q, want := range cases {
		if got := countParams(q); got != want {
			t.Errorf("countParams(%q) = %d, want %d", q, got, want)
		}
	}
}
