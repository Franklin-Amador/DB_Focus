package server

import (
	"encoding/binary"
	"testing"

	"dbf/internal/catalog"
)

func TestDecodeBindValue(t *testing.T) {
	if got := decodeBindValue([]byte("490133"), false); got != "490133" {
		t.Errorf("text decode = %q, want 490133", got)
	}
	b4 := make([]byte, 4)
	binary.BigEndian.PutUint32(b4, 490133)
	if got := decodeBindValue(b4, true); got != "490133" {
		t.Errorf("binary int4 decode = %q, want 490133", got)
	}
	b8 := make([]byte, 8)
	binary.BigEndian.PutUint64(b8, 1234567890)
	if got := decodeBindValue(b8, true); got != "1234567890" {
		t.Errorf("binary int8 decode = %q, want 1234567890", got)
	}
	b2 := make([]byte, 2)
	binary.BigEndian.PutUint16(b2, 42)
	if got := decodeBindValue(b2, true); got != "42" {
		t.Errorf("binary int2 decode = %q, want 42", got)
	}
}

// TestParseBindParamsBinary is the regression for DBeaver/pgjdbc sending an oid
// filter value in binary format: the 4-byte int must decode to its decimal text
// so "WHERE c.oid = $1" matches instead of returning every table's columns.
func TestParseBindParamsBinary(t *testing.T) {
	var p []byte
	p = append(p, 0)    // portal "" (cstring)
	p = append(p, 0)    // statement "" (cstring)
	p = append(p, 0, 1) // 1 format code
	p = append(p, 0, 1) // format = binary
	p = append(p, 0, 1) // 1 param
	val := make([]byte, 4)
	binary.BigEndian.PutUint32(val, 490133)
	p = append(p, 0, 0, 0, 4) // length 4
	p = append(p, val...)
	p = append(p, 0, 0) // result format codes

	params := parseBindParams(p)
	if len(params) != 1 || params[0] != "490133" {
		t.Fatalf("parseBindParams = %q, want [490133]", params)
	}
}

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
