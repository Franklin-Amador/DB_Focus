package parser

import (
	"strings"
	"testing"

	"dbf/internal/ast"
)

func TestMultipleStatements(t *testing.T) {
	input := "SET DateStyle=ISO; SELECT version();"
	p := NewParser(input)

	var statements []ast.Statement

	// Parse all statements
	for !p.AtEOF() {
		stmt, err := p.ParseStatement()
		if err != nil {
			t.Fatalf("Failed to parse statement: %v", err)
		}
		if stmt != nil {
			statements = append(statements, stmt)
		}
	}

	// We should have 2 statements
	if len(statements) != 2 {
		t.Fatalf("Expected 2 statements, got %d", len(statements))
	}

	// First should be SET
	if _, ok := statements[0].(*ast.Set); !ok {
		t.Errorf("Expected first statement to be *ast.Set, got %T", statements[0])
	}

	// Second should be SELECT
	if _, ok := statements[1].(*ast.SelectFunction); !ok {
		t.Errorf("Expected second statement to be *ast.SelectFunction, got %T", statements[1])
	}
}

func TestSimpleSelect(t *testing.T) {
	input := "SELECT * FROM users;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse SELECT: %v", err)
	}
	if stmt == nil {
		t.Fatal("Expected statement to be non-nil")
	}
	sel, ok := stmt.(*ast.Select)
	if !ok {
		t.Fatalf("Expected *ast.Select, got %T", stmt)
	}
	if !sel.Star {
		t.Errorf("Expected Star=true, got false")
	}
	if sel.Table.Name != "users" {
		t.Errorf("Expected table 'users', got '%s'", sel.Table.Name)
	}
}

func TestParseNaturalJoin(t *testing.T) {
	p := NewParser("SELECT * FROM a NATURAL LEFT JOIN b;")
	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse NATURAL JOIN: %v", err)
	}
	sel := stmt.(*ast.Select)
	if len(sel.Joins) != 1 {
		t.Fatalf("Expected 1 join, got %d", len(sel.Joins))
	}
	j := sel.Joins[0]
	if !j.Natural {
		t.Errorf("Expected Natural=true")
	}
	if j.Type != "LEFT" {
		t.Errorf("Expected Type=LEFT, got %q", j.Type)
	}
	if j.Table.Name != "b" {
		t.Errorf("Expected joined table 'b', got %q", j.Table.Name)
	}
}

func TestParseJoinUsing(t *testing.T) {
	p := NewParser("SELECT * FROM a JOIN b USING (k1, k2);")
	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse JOIN USING: %v", err)
	}
	j := stmt.(*ast.Select).Joins[0]
	if j.Natural {
		t.Errorf("Expected Natural=false for USING join")
	}
	if len(j.Using) != 2 || j.Using[0] != "k1" || j.Using[1] != "k2" {
		t.Errorf("Expected Using=[k1 k2], got %v", j.Using)
	}
}

func TestCreateTable(t *testing.T) {
	input := "CREATE TABLE test (id INT PRIMARY KEY, name TEXT);"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse CREATE TABLE: %v", err)
	}
	if stmt == nil {
		t.Fatal("Expected statement to be non-nil")
	}
	ct, ok := stmt.(*ast.CreateTable)
	if !ok {
		t.Fatalf("Expected *ast.CreateTable, got %T", stmt)
	}
	if ct.Table.Name != "test" {
		t.Errorf("Expected table 'test', got '%s'", ct.Table.Name)
	}
	if len(ct.Columns) != 2 {
		t.Errorf("Expected 2 columns, got %d", len(ct.Columns))
	}
}

// findFK returns the first ForeignKeyConstraint declared anywhere in a
// CREATE TABLE (column-level or table-level).
func findFK(ct *ast.CreateTable) *ast.ForeignKeyConstraint {
	for _, col := range ct.Columns {
		for _, c := range col.Constraints {
			if fk, ok := c.(*ast.ForeignKeyConstraint); ok {
				return fk
			}
		}
	}
	for _, c := range ct.Constraints {
		if fk, ok := c.(*ast.ForeignKeyConstraint); ok {
			return fk
		}
	}
	return nil
}

func TestForeignKeyReferentialActions(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantDelete string
		wantUpdate string
	}{
		{
			name:       "inline cascade both",
			input:      "CREATE TABLE c (id INT PRIMARY KEY, p INT REFERENCES par(id) ON DELETE CASCADE ON UPDATE CASCADE)",
			wantDelete: "CASCADE",
			wantUpdate: "CASCADE",
		},
		{
			name:       "inline set null on delete only",
			input:      "CREATE TABLE c (id INT PRIMARY KEY, p INT REFERENCES par(id) ON DELETE SET NULL)",
			wantDelete: "SET NULL",
			wantUpdate: "",
		},
		{
			name:       "inline update restrict only",
			input:      "CREATE TABLE c (id INT PRIMARY KEY, p INT REFERENCES par(id) ON UPDATE RESTRICT)",
			wantDelete: "",
			wantUpdate: "RESTRICT",
		},
		{
			name:       "table-level no action + cascade (reversed order)",
			input:      "CREATE TABLE c (id INT PRIMARY KEY, p INT, FOREIGN KEY (p) REFERENCES par(id) ON UPDATE CASCADE ON DELETE NO ACTION)",
			wantDelete: "NO ACTION",
			wantUpdate: "CASCADE",
		},
		{
			name:       "no actions defaults to empty",
			input:      "CREATE TABLE c (id INT PRIMARY KEY, p INT REFERENCES par(id))",
			wantDelete: "",
			wantUpdate: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stmt, err := NewParser(tc.input).ParseStatement()
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			ct, ok := stmt.(*ast.CreateTable)
			if !ok {
				t.Fatalf("expected *ast.CreateTable, got %T", stmt)
			}
			fk := findFK(ct)
			if fk == nil {
				t.Fatal("no foreign key constraint found")
			}
			if fk.ReferencedTable != "par" || fk.ReferencedCol != "id" {
				t.Errorf("expected REFERENCES par(id), got %s(%s)", fk.ReferencedTable, fk.ReferencedCol)
			}
			if fk.OnDelete != tc.wantDelete {
				t.Errorf("OnDelete: expected %q, got %q", tc.wantDelete, fk.OnDelete)
			}
			if fk.OnUpdate != tc.wantUpdate {
				t.Errorf("OnUpdate: expected %q, got %q", tc.wantUpdate, fk.OnUpdate)
			}
		})
	}
}

func TestParseStarModifiers(t *testing.T) {
	stmt, err := NewParser("SELECT * EXCLUDE (ssn, loaded_at) RENAME (nombre AS name) FROM t").ParseStatement()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	s := stmt.(*ast.Select)
	if !s.Star {
		t.Fatal("expected Star")
	}
	if len(s.StarExclude) != 2 || s.StarExclude[0] != "ssn" || s.StarExclude[1] != "loaded_at" {
		t.Errorf("StarExclude = %v", s.StarExclude)
	}
	if len(s.StarRename) != 1 || s.StarRename[0].From != "nombre" || s.StarRename[0].To != "name" {
		t.Errorf("StarRename = %v", s.StarRename)
	}
	if s.Table.Name != "t" {
		t.Errorf("expected FROM t, got %q", s.Table.Name)
	}

	s2 := mustParseSelect(t, "SELECT * ILIKE '%_amount' FROM payments")
	if s2.StarILike != "%_amount" {
		t.Errorf("StarILike = %q", s2.StarILike)
	}
}

func TestParseGroupByAll(t *testing.T) {
	s := mustParseSelect(t, "SELECT a, COUNT(*) FROM t GROUP BY ALL")
	if !s.GroupByAll {
		t.Fatal("expected GroupByAll")
	}
	if len(s.GroupBy) != 0 {
		t.Errorf("GroupBy should be empty, got %v", s.GroupBy)
	}
}

func TestParseMaxBy(t *testing.T) {
	s := mustParseSelect(t, "SELECT MAX_BY(status, updated_at) FROM t")
	if len(s.Columns) != 1 || s.Columns[0].Name != "MAX_BY(status, updated_at)" {
		t.Fatalf("expected MAX_BY text column, got %+v", s.Columns)
	}
	if _, err := NewParser("SELECT MAX_BY(status) FROM t").ParseStatement(); err == nil {
		t.Error("expected error for MAX_BY with one argument")
	}
}

func TestParseAsofJoin(t *testing.T) {
	s := mustParseSelect(t, "SELECT * FROM trades t ASOF JOIN quotes q MATCH_CONDITION (t.ts >= q.ts) ON t.sym = q.sym")
	if len(s.Joins) != 1 {
		t.Fatalf("expected 1 join, got %d", len(s.Joins))
	}
	j := s.Joins[0]
	if !j.Asof || j.Type != "ASOF" {
		t.Errorf("expected ASOF join, got Asof=%v Type=%q", j.Asof, j.Type)
	}
	if j.MatchLeft.Name != "t.ts" || j.MatchRight.Name != "q.ts" || j.MatchOp != ">=" {
		t.Errorf("MATCH_CONDITION = %q %q %q", j.MatchLeft.Name, j.MatchOp, j.MatchRight.Name)
	}
	if j.Left.Name != "t.sym" || j.Right.Name != "q.sym" {
		t.Errorf("ON = %q = %q", j.Left.Name, j.Right.Name)
	}
	// ON is optional.
	s2 := mustParseSelect(t, "SELECT * FROM trades t ASOF JOIN quotes q MATCH_CONDITION (t.ts >= q.ts)")
	if s2.Joins[0].Left.Name != "" {
		t.Errorf("expected no ON, got %q", s2.Joins[0].Left.Name)
	}
	// MATCH_CONDITION must use an inequality.
	if _, err := NewParser("SELECT * FROM a t ASOF JOIN b q MATCH_CONDITION (t.x = q.x)").ParseStatement(); err == nil {
		t.Error("expected error for = in MATCH_CONDITION")
	}
}

func mustParseSelect(t *testing.T, sql string) *ast.Select {
	t.Helper()
	stmt, err := NewParser(sql).ParseStatement()
	if err != nil {
		t.Fatalf("parse error for %q: %v", sql, err)
	}
	s, ok := stmt.(*ast.Select)
	if !ok {
		t.Fatalf("expected *ast.Select, got %T", stmt)
	}
	return s
}

func TestParseBetween(t *testing.T) {
	stmt, err := NewParser("SELECT * FROM t WHERE n BETWEEN 5 AND 10").ParseStatement()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	sel, ok := stmt.(*ast.Select)
	if !ok {
		t.Fatalf("expected *ast.Select, got %T", stmt)
	}
	w := sel.Where
	if w == nil || !w.IsLeaf() {
		t.Fatalf("expected a leaf WHERE, got %+v", w)
	}
	if w.Operator != "BETWEEN" {
		t.Errorf("expected operator BETWEEN, got %q", w.Operator)
	}
	if w.Column.Name != "n" {
		t.Errorf("expected column n, got %q", w.Column.Name)
	}
	if w.Value.Value != "5" || w.Value2.Value != "10" {
		t.Errorf("expected bounds 5..10, got %q..%q", w.Value.Value, w.Value2.Value)
	}
}

func TestParseBetweenMissingAnd(t *testing.T) {
	if _, err := NewParser("SELECT * FROM t WHERE n BETWEEN 5 10").ParseStatement(); err == nil {
		t.Fatal("expected parse error for BETWEEN without AND")
	}
}

func TestForeignKeyInvalidAction(t *testing.T) {
	if _, err := NewParser("CREATE TABLE c (id INT, p INT REFERENCES par(id) ON DELETE BOGUS)").ParseStatement(); err == nil {
		t.Fatal("expected parse error for unknown referential action")
	}
}

func TestCreateIndex(t *testing.T) {
	input := "CREATE INDEX idx_users_name ON users (name);"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse CREATE INDEX: %v", err)
	}
	ci, ok := stmt.(*ast.CreateIndex)
	if !ok {
		t.Fatalf("Expected *ast.CreateIndex, got %T", stmt)
	}
	if ci.Name.Name != "idx_users_name" {
		t.Fatalf("Expected index name idx_users_name, got %s", ci.Name.Name)
	}
	if ci.Table.Name != "users" {
		t.Fatalf("Expected table users, got %s", ci.Table.Name)
	}
	if len(ci.Columns) != 1 || ci.Columns[0].Name != "name" {
		t.Fatalf("Expected indexed columns [name], got %+v", ci.Columns)
	}
}

func TestCreateCompositeIndex(t *testing.T) {
	input := "CREATE INDEX idx_users_email_age ON users (email, age);"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse CREATE INDEX: %v", err)
	}
	ci, ok := stmt.(*ast.CreateIndex)
	if !ok {
		t.Fatalf("Expected *ast.CreateIndex, got %T", stmt)
	}
	if ci.Name.Name != "idx_users_email_age" {
		t.Fatalf("Expected index name idx_users_email_age, got %s", ci.Name.Name)
	}
	if ci.Table.Name != "users" {
		t.Fatalf("Expected table users, got %s", ci.Table.Name)
	}
	if len(ci.Columns) != 2 || ci.Columns[0].Name != "email" || ci.Columns[1].Name != "age" {
		t.Fatalf("Expected indexed columns [email age], got %+v", ci.Columns)
	}
}

func TestDropIndex(t *testing.T) {
	input := "DROP INDEX idx_users_age ON users;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse DROP INDEX: %v", err)
	}
	di, ok := stmt.(*ast.DropIndex)
	if !ok {
		t.Fatalf("Expected *ast.DropIndex, got %T", stmt)
	}
	if di.Name.Name != "idx_users_age" {
		t.Fatalf("Expected index name idx_users_age, got %s", di.Name.Name)
	}
	if di.Table.Name != "users" {
		t.Fatalf("Expected table users, got %s", di.Table.Name)
	}
}

func TestCreateFunctionReturnsTable(t *testing.T) {
	input := `CREATE OR REPLACE FUNCTION fn_listar_cursos()
RETURNS TABLE (id INTEGER, titulo VARCHAR, creditos INTEGER)
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    SELECT c.id, c.titulo, c.creditos FROM cursos c;
END;
$$`
	stmt, err := NewParser(input).ParseStatement()
	if err != nil {
		t.Fatalf("parse CREATE FUNCTION: %v", err)
	}
	fn, ok := stmt.(*ast.CreateFunction)
	if !ok {
		t.Fatalf("expected *ast.CreateFunction, got %T", stmt)
	}
	if !fn.Replace {
		t.Fatalf("expected Replace=true")
	}
	if fn.Name.Name != "fn_listar_cursos" {
		t.Fatalf("expected name fn_listar_cursos, got %s", fn.Name.Name)
	}
	if len(fn.Returns) != 3 || fn.Returns[0].Name.Name != "id" || fn.Returns[2].Name.Name != "creditos" {
		t.Fatalf("expected 3 RETURNS TABLE columns, got %+v", fn.Returns)
	}
	if fn.Query == nil {
		t.Fatalf("expected a parsed body SELECT")
	}
	if fn.QueryText == "" {
		t.Fatalf("expected captured QueryText")
	}
}

func TestCreateFunctionWithParam(t *testing.T) {
	input := `CREATE FUNCTION fn_cursos(min_cred INTEGER)
RETURNS TABLE (titulo VARCHAR) AS $$
BEGIN RETURN QUERY SELECT c.titulo FROM cursos c WHERE c.creditos >= min_cred; END;
$$`
	stmt, err := NewParser(input).ParseStatement()
	if err != nil {
		t.Fatalf("parse CREATE FUNCTION with param: %v", err)
	}
	fn := stmt.(*ast.CreateFunction)
	if len(fn.Parameters) != 1 || fn.Parameters[0].Name.Name != "min_cred" || fn.Parameters[0].Type != "INTEGER" {
		t.Fatalf("expected param min_cred INTEGER, got %+v", fn.Parameters)
	}
}

func TestFromFunctionCall(t *testing.T) {
	stmt, err := NewParser("SELECT * FROM fn_listar_cursos()").ParseStatement()
	if err != nil {
		t.Fatalf("parse FROM function call: %v", err)
	}
	sel := stmt.(*ast.Select)
	if !sel.Table.IsCall {
		t.Fatalf("expected Table.IsCall=true for fn_listar_cursos()")
	}
	if sel.Table.Name != "fn_listar_cursos" {
		t.Fatalf("expected function name fn_listar_cursos, got %s", sel.Table.Name)
	}
}

func TestFromFunctionCallWithArgs(t *testing.T) {
	stmt, err := NewParser("SELECT * FROM fn_cursos(5) f").ParseStatement()
	if err != nil {
		t.Fatalf("parse FROM function call with args: %v", err)
	}
	sel := stmt.(*ast.Select)
	if !sel.Table.IsCall || len(sel.Table.Args) != 1 || sel.Table.Args[0].Value != "5" {
		t.Fatalf("expected one arg 5, got %+v", sel.Table.Args)
	}
	if sel.Table.Alias != "f" {
		t.Fatalf("expected alias f, got %s", sel.Table.Alias)
	}
}

func TestDropFunction(t *testing.T) {
	stmt, err := NewParser("DROP FUNCTION IF EXISTS fn_cursos(int)").ParseStatement()
	if err != nil {
		t.Fatalf("parse DROP FUNCTION: %v", err)
	}
	df, ok := stmt.(*ast.DropFunction)
	if !ok {
		t.Fatalf("expected *ast.DropFunction, got %T", stmt)
	}
	if !df.IfExists || df.Name.Name != "fn_cursos" {
		t.Fatalf("expected IF EXISTS fn_cursos, got %+v", df)
	}
}

func TestDescribe(t *testing.T) {
	for _, input := range []string{"DESCRIBE users;", "DESC users;"} {
		p := NewParser(input)
		stmt, err := p.ParseStatement()
		if err != nil {
			t.Fatalf("Failed to parse %q: %v", input, err)
		}
		d, ok := stmt.(*ast.Describe)
		if !ok {
			t.Fatalf("%q: expected *ast.Describe, got %T", input, stmt)
		}
		if d.Table.Name != "users" {
			t.Fatalf("%q: expected table users, got %s", input, d.Table.Name)
		}
	}
}

func TestDescribeQualified(t *testing.T) {
	p := NewParser("DESCRIBE sales.orders;")
	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse DESCRIBE sales.orders: %v", err)
	}
	d, ok := stmt.(*ast.Describe)
	if !ok {
		t.Fatalf("Expected *ast.Describe, got %T", stmt)
	}
	if d.Table.Name != "orders" || d.Table.Alias != "sales" {
		t.Fatalf("Expected schema sales / table orders, got %s / %s", d.Table.Alias, d.Table.Name)
	}
}

func TestDropIndexQualifiedTable(t *testing.T) {
	input := "DROP INDEX idx_users_age ON public.users;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse qualified DROP INDEX: %v", err)
	}
	di, ok := stmt.(*ast.DropIndex)
	if !ok {
		t.Fatalf("Expected *ast.DropIndex, got %T", stmt)
	}
	if di.Table.Alias != "public" || di.Table.Name != "users" {
		t.Fatalf("Expected table public.users, got %s.%s", di.Table.Alias, di.Table.Name)
	}
}

func TestDropTableCascade(t *testing.T) {
	input := "DROP TABLE public.users CASCADE;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse DROP TABLE CASCADE: %v", err)
	}
	dt, ok := stmt.(*ast.DropTable)
	if !ok {
		t.Fatalf("Expected *ast.DropTable, got %T", stmt)
	}
	if dt.Table.Alias != "public" || dt.Table.Name != "users" {
		t.Fatalf("Expected table public.users, got %s.%s", dt.Table.Alias, dt.Table.Name)
	}
	if dt.Behavior != "CASCADE" {
		t.Fatalf("Expected Behavior=CASCADE, got %s", dt.Behavior)
	}
}

func TestDropTableIfExistsRestrict(t *testing.T) {
	input := "DROP TABLE IF EXISTS users RESTRICT;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse DROP TABLE IF EXISTS RESTRICT: %v", err)
	}
	dt, ok := stmt.(*ast.DropTable)
	if !ok {
		t.Fatalf("Expected *ast.DropTable, got %T", stmt)
	}
	if !dt.IfExists {
		t.Fatalf("Expected IfExists=true")
	}
	if dt.Behavior != "RESTRICT" {
		t.Fatalf("Expected Behavior=RESTRICT, got %s", dt.Behavior)
	}
}

func TestDropSchemaCascade(t *testing.T) {
	input := "DROP SCHEMA myschema CASCADE;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse DROP SCHEMA CASCADE: %v", err)
	}
	ds, ok := stmt.(*ast.DropSchema)
	if !ok {
		t.Fatalf("Expected *ast.DropSchema, got %T", stmt)
	}
	if ds.Name != "myschema" {
		t.Fatalf("Expected schema myschema, got %s", ds.Name)
	}
	if ds.Behavior != "CASCADE" {
		t.Fatalf("Expected Behavior=CASCADE, got %s", ds.Behavior)
	}
}

func TestDropSchemaIfExistsRestrict(t *testing.T) {
	input := "DROP SCHEMA IF EXISTS myschema RESTRICT;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse DROP SCHEMA IF EXISTS RESTRICT: %v", err)
	}
	ds, ok := stmt.(*ast.DropSchema)
	if !ok {
		t.Fatalf("Expected *ast.DropSchema, got %T", stmt)
	}
	if !ds.IfExists {
		t.Fatalf("Expected IfExists=true")
	}
	if ds.Behavior != "RESTRICT" {
		t.Fatalf("Expected Behavior=RESTRICT, got %s", ds.Behavior)
	}
}

func TestCreateView(t *testing.T) {
	input := "CREATE VIEW v_users AS SELECT id, name FROM users;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse CREATE VIEW: %v", err)
	}
	cv, ok := stmt.(*ast.CreateView)
	if !ok {
		t.Fatalf("Expected *ast.CreateView, got %T", stmt)
	}
	if cv.Name.Name != "v_users" {
		t.Fatalf("Expected view name v_users, got %s", cv.Name.Name)
	}
	if cv.Query == nil {
		t.Fatalf("Expected query in CREATE VIEW")
	}
	if cv.Query.Table.Name != "users" {
		t.Fatalf("Expected source table users, got %s", cv.Query.Table.Name)
	}
}

func TestCreateOrReplaceView(t *testing.T) {
	input := "CREATE OR REPLACE VIEW v_users AS SELECT id FROM users;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse CREATE OR REPLACE VIEW: %v", err)
	}
	cv, ok := stmt.(*ast.CreateView)
	if !ok {
		t.Fatalf("Expected *ast.CreateView, got %T", stmt)
	}
	if !cv.Replace {
		t.Fatalf("Expected Replace=true for CREATE OR REPLACE VIEW")
	}
	if cv.Name.Name != "v_users" {
		t.Fatalf("Expected view name v_users, got %s", cv.Name.Name)
	}
}

func TestCreateViewIfNotExists(t *testing.T) {
	input := "CREATE VIEW IF NOT EXISTS v_users AS SELECT id FROM users;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse CREATE VIEW IF NOT EXISTS: %v", err)
	}
	cv, ok := stmt.(*ast.CreateView)
	if !ok {
		t.Fatalf("Expected *ast.CreateView, got %T", stmt)
	}
	if !cv.IfNotExists {
		t.Fatalf("Expected IfNotExists=true")
	}
	if cv.Replace {
		t.Fatalf("Expected Replace=false")
	}
}

func TestDropView(t *testing.T) {
	input := "DROP VIEW v_users;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse DROP VIEW: %v", err)
	}
	dv, ok := stmt.(*ast.DropView)
	if !ok {
		t.Fatalf("Expected *ast.DropView, got %T", stmt)
	}
	if dv.Name.Name != "v_users" {
		t.Fatalf("Expected view name v_users, got %s", dv.Name.Name)
	}
}

func TestDropViewIfExists(t *testing.T) {
	input := "DROP VIEW IF EXISTS v_users;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse DROP VIEW IF EXISTS: %v", err)
	}
	dv, ok := stmt.(*ast.DropView)
	if !ok {
		t.Fatalf("Expected *ast.DropView, got %T", stmt)
	}
	if !dv.IfExists {
		t.Fatalf("Expected IfExists=true")
	}
	if dv.Name.Name != "v_users" {
		t.Fatalf("Expected view name v_users, got %s", dv.Name.Name)
	}
}

func TestCreateViewWithColumnList(t *testing.T) {
	input := "CREATE VIEW v_users (user_id, user_name) AS SELECT id, name FROM users;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse CREATE VIEW with column list: %v", err)
	}
	cv, ok := stmt.(*ast.CreateView)
	if !ok {
		t.Fatalf("Expected *ast.CreateView, got %T", stmt)
	}
	if cv.Name.Name != "v_users" {
		t.Fatalf("Expected view name v_users, got %s", cv.Name.Name)
	}
	if len(cv.ColumnNames) != 2 {
		t.Fatalf("Expected 2 column names, got %d", len(cv.ColumnNames))
	}
	if cv.ColumnNames[0] != "user_id" || cv.ColumnNames[1] != "user_name" {
		t.Fatalf("Expected column names [user_id, user_name], got %v", cv.ColumnNames)
	}
	if cv.Query == nil {
		t.Fatalf("Expected query in CREATE VIEW")
	}
}

func TestCreateViewWithColumnListAndReplace(t *testing.T) {
	input := "CREATE OR REPLACE VIEW v_users (user_id, user_name, user_email) AS SELECT id, name, email FROM users;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse CREATE OR REPLACE VIEW with column list: %v", err)
	}
	cv, ok := stmt.(*ast.CreateView)
	if !ok {
		t.Fatalf("Expected *ast.CreateView, got %T", stmt)
	}
	if !cv.Replace {
		t.Fatalf("Expected Replace=true")
	}
	if len(cv.ColumnNames) != 3 {
		t.Fatalf("Expected 3 column names, got %d", len(cv.ColumnNames))
	}
	if cv.ColumnNames[0] != "user_id" || cv.ColumnNames[1] != "user_name" || cv.ColumnNames[2] != "user_email" {
		t.Fatalf("Expected column names [user_id, user_name, user_email], got %v", cv.ColumnNames)
	}
}

func TestDropViewCascade(t *testing.T) {
	input := "DROP VIEW v_users CASCADE;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse DROP VIEW CASCADE: %v", err)
	}
	dv, ok := stmt.(*ast.DropView)
	if !ok {
		t.Fatalf("Expected *ast.DropView, got %T", stmt)
	}
	if dv.Name.Name != "v_users" {
		t.Fatalf("Expected view name v_users, got %s", dv.Name.Name)
	}
	if dv.Behavior != "CASCADE" {
		t.Fatalf("Expected Behavior=CASCADE, got %s", dv.Behavior)
	}
}

func TestDropViewRestrict(t *testing.T) {
	input := "DROP VIEW v_users RESTRICT;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse DROP VIEW RESTRICT: %v", err)
	}
	dv, ok := stmt.(*ast.DropView)
	if !ok {
		t.Fatalf("Expected *ast.DropView, got %T", stmt)
	}
	if dv.Name.Name != "v_users" {
		t.Fatalf("Expected view name v_users, got %s", dv.Name.Name)
	}
	if dv.Behavior != "RESTRICT" {
		t.Fatalf("Expected Behavior=RESTRICT, got %s", dv.Behavior)
	}
}

func TestDropViewIfExistsCascade(t *testing.T) {
	input := "DROP VIEW IF EXISTS v_users CASCADE;"
	p := NewParser(input)

	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("Failed to parse DROP VIEW IF EXISTS CASCADE: %v", err)
	}
	dv, ok := stmt.(*ast.DropView)
	if !ok {
		t.Fatalf("Expected *ast.DropView, got %T", stmt)
	}
	if !dv.IfExists {
		t.Fatalf("Expected IfExists=true")
	}
	if dv.Behavior != "CASCADE" {
		t.Fatalf("Expected Behavior=CASCADE, got %s", dv.Behavior)
	}
}

// parseSelectT parses a single SELECT and fails the test on error.
func parseSelectT(t *testing.T, input string) *ast.Select {
	t.Helper()
	p := NewParser(input)
	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("parse %q: %v", input, err)
	}
	sel, ok := stmt.(*ast.Select)
	if !ok {
		t.Fatalf("expected *ast.Select, got %T", stmt)
	}
	return sel
}

func TestParseWindowFunction(t *testing.T) {
	sel := parseSelectT(t, "SELECT categoria, monto, ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) AS rn FROM ventas")
	if len(sel.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(sel.Columns))
	}
	w := sel.Columns[2]
	if w.Window == nil {
		t.Fatalf("expected window column, got %+v", w)
	}
	if w.Alias != "rn" || w.Name != "" {
		t.Errorf("expected alias rn and empty name, got name=%q alias=%q", w.Name, w.Alias)
	}
	if w.Window.Func != "ROW_NUMBER" || w.Window.Arg != "" {
		t.Errorf("unexpected window func %+v", w.Window)
	}
	if len(w.Window.PartitionBy) != 1 || w.Window.PartitionBy[0].Name != "categoria" {
		t.Errorf("unexpected partition by %+v", w.Window.PartitionBy)
	}
	if len(w.Window.OrderBy) != 1 || w.Window.OrderBy[0].Column.Name != "monto" || w.Window.OrderBy[0].Direction != "DESC" {
		t.Errorf("unexpected order by %+v", w.Window.OrderBy)
	}
	if sel.Table.Name != "ventas" {
		t.Errorf("expected table ventas, got %q", sel.Table.Name)
	}
	if sel.AllowMissing {
		t.Errorf("window columns must not enable AllowMissing")
	}
}

func TestParseAggregateOver(t *testing.T) {
	sel := parseSelectT(t, "SELECT SUM(monto) OVER () total, COUNT(*) OVER (PARTITION BY categoria) AS n, SUM(monto) AS plain FROM ventas")
	if len(sel.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(sel.Columns))
	}
	if sel.Columns[0].Window == nil || sel.Columns[0].Window.Func != "SUM" || sel.Columns[0].Window.Arg != "monto" || sel.Columns[0].Alias != "total" {
		t.Errorf("unexpected first column %+v", sel.Columns[0])
	}
	if sel.Columns[1].Window == nil || sel.Columns[1].Window.Func != "COUNT" || sel.Columns[1].Window.Arg != "*" || len(sel.Columns[1].Window.PartitionBy) != 1 {
		t.Errorf("unexpected second column %+v", sel.Columns[1])
	}
	if sel.Columns[2].Window != nil || sel.Columns[2].Name != "SUM(monto)" || sel.Columns[2].Alias != "plain" {
		t.Errorf("plain aggregate must stay textual, got %+v", sel.Columns[2])
	}
}

func TestParseQualifyAlias(t *testing.T) {
	sel := parseSelectT(t, "SELECT categoria, ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) AS rn FROM ventas QUALIFY rn <= 2 ORDER BY categoria LIMIT 5")
	if sel.Qualify == nil || !sel.Qualify.IsLeaf() {
		t.Fatalf("expected leaf QUALIFY, got %+v", sel.Qualify)
	}
	if sel.Qualify.Column.Name != "rn" || sel.Qualify.Operator != "<=" || sel.Qualify.Value.Value != "2" {
		t.Errorf("unexpected QUALIFY leaf %+v", sel.Qualify)
	}
	if len(sel.OrderBy) != 1 || sel.Limit != 5 {
		t.Errorf("clauses after QUALIFY not parsed: order=%+v limit=%d", sel.OrderBy, sel.Limit)
	}
}

func TestParseQualifyInlineWindow(t *testing.T) {
	sel := parseSelectT(t, "SELECT categoria, monto FROM ventas WHERE monto > 0 QUALIFY ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) = 1 AND monto > 10")
	if sel.Where == nil {
		t.Fatalf("WHERE lost")
	}
	if sel.Qualify == nil || sel.Qualify.Conj != "AND" {
		t.Fatalf("expected AND QUALIFY, got %+v", sel.Qualify)
	}
	left := sel.Qualify.Left
	if left.Column.Window == nil || left.Column.Window.Func != "ROW_NUMBER" || left.Operator != "=" || left.Value.Value != "1" {
		t.Errorf("unexpected inline window leaf %+v", left)
	}
	if sel.Qualify.Right.Column.Name != "monto" {
		t.Errorf("unexpected right leaf %+v", sel.Qualify.Right)
	}
}

func TestParseQualifyWithGroupBy(t *testing.T) {
	sel := parseSelectT(t, "SELECT categoria, SUM(monto) AS total, RANK() OVER (ORDER BY SUM(monto) DESC) AS pos FROM ventas GROUP BY categoria QUALIFY pos <= 3")
	if len(sel.GroupBy) != 1 || sel.GroupBy[0].Name != "categoria" {
		t.Errorf("unexpected group by %+v", sel.GroupBy)
	}
	w := sel.Columns[2].Window
	if w == nil || w.Func != "RANK" || len(w.OrderBy) != 1 || w.OrderBy[0].Column.Name != "SUM(monto)" || w.OrderBy[0].Direction != "DESC" {
		t.Errorf("unexpected window over aggregate %+v", w)
	}
	if sel.Qualify == nil || sel.Qualify.Column.Name != "pos" {
		t.Errorf("unexpected qualify %+v", sel.Qualify)
	}
}

func TestParseOrderByAggregate(t *testing.T) {
	sel := parseSelectT(t, "SELECT categoria, SUM(monto) FROM ventas GROUP BY categoria ORDER BY SUM(monto) DESC, categoria")
	if len(sel.OrderBy) != 2 || sel.OrderBy[0].Column.Name != "SUM(monto)" || sel.OrderBy[0].Direction != "DESC" || sel.OrderBy[1].Column.Name != "categoria" {
		t.Errorf("unexpected order by %+v", sel.OrderBy)
	}
}

func TestParseQualifyAggregateLeaf(t *testing.T) {
	sel := parseSelectT(t, "SELECT categoria FROM ventas GROUP BY categoria QUALIFY SUM(monto) > 100")
	if sel.Qualify == nil || sel.Qualify.Column.Name != "SUM(monto)" || sel.Qualify.Column.Window != nil {
		t.Errorf("unexpected qualify %+v", sel.Qualify)
	}
}

func TestParseHaving(t *testing.T) {
	sel := parseSelectT(t, "SELECT categoria, COUNT(*) AS n FROM ventas WHERE monto > 0 GROUP BY categoria HAVING SUM(monto) > 100 AND n >= 2 QUALIFY ROW_NUMBER() OVER (ORDER BY n DESC) <= 5 ORDER BY categoria")
	if sel.Having == nil || sel.Having.Conj != "AND" {
		t.Fatalf("expected AND HAVING, got %+v", sel.Having)
	}
	if sel.Having.Left.Column.Name != "SUM(monto)" || sel.Having.Left.Operator != ">" || sel.Having.Left.Value.Value != "100" {
		t.Errorf("unexpected HAVING left leaf %+v", sel.Having.Left)
	}
	if sel.Having.Right.Column.Name != "n" || sel.Having.Right.Operator != ">=" {
		t.Errorf("unexpected HAVING right leaf %+v", sel.Having.Right)
	}
	if sel.Where == nil || sel.Qualify == nil || len(sel.OrderBy) != 1 || len(sel.GroupBy) != 1 {
		t.Errorf("surrounding clauses lost: where=%v qualify=%v order=%v group=%v", sel.Where != nil, sel.Qualify != nil, sel.OrderBy, sel.GroupBy)
	}
	// HAVING without GROUP BY is valid (whole table is one group).
	sel = parseSelectT(t, "SELECT COUNT(*) FROM ventas HAVING COUNT(*) > 3")
	if sel.Having == nil || sel.Having.Column.Name != "COUNT(*)" {
		t.Errorf("unexpected HAVING %+v", sel.Having)
	}
	// Window functions are rejected in HAVING.
	p := NewParser("SELECT categoria FROM ventas GROUP BY categoria HAVING ROW_NUMBER() OVER () = 1")
	if _, err := p.ParseStatement(); err == nil || !strings.Contains(err.Error(), "HAVING") {
		t.Errorf("expected HAVING error for window function, got %v", err)
	}
}

func TestParseWindowInExpressionIsPlaceholder(t *testing.T) {
	sel := parseSelectT(t, "SELECT ROW_NUMBER() OVER (ORDER BY id) * 2 AS x, monto FROM ventas")
	if len(sel.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(sel.Columns))
	}
	first := sel.Columns[0]
	if first.Window != nil || first.Name != "" || first.Alias != "x" {
		t.Errorf("expected NULL placeholder for wrapped window, got %+v", first)
	}
	if !sel.AllowMissing {
		t.Errorf("placeholder item must enable AllowMissing")
	}
	if sel.Columns[1].Name != "monto" {
		t.Errorf("following item lost: %+v", sel.Columns[1])
	}
}

func TestParseWindowErrors(t *testing.T) {
	bad := []string{
		"SELECT ROW_NUMBER() FROM ventas",                             // ranking without OVER
		"SELECT ROW_NUMBER() OVER PARTITION BY x FROM ventas",         // missing parens
		"SELECT RANK(monto) OVER () FROM ventas",                      // ranking with argument
		"SELECT categoria FROM ventas QUALIFY",                        // empty predicate
		"SELECT categoria FROM ventas QUALIFY rn",                     // missing operator
		"SELECT categoria FROM ventas WHERE ROW_NUMBER() OVER () = 1", // window in WHERE
		"SELECT SUM(monto) OVER (ORDER BY monto FROM ventas",          // unclosed OVER
	}
	for _, sql := range bad {
		p := NewParser(sql)
		if _, err := p.ParseStatement(); err == nil && p.AtEOF() {
			t.Errorf("expected parse error for %q", sql)
		}
	}
}

func TestParseCreateSchemaIfNotExists(t *testing.T) {
	p := NewParser("CREATE SCHEMA IF NOT EXISTS tienda")
	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cs, ok := stmt.(*ast.CreateSchema)
	if !ok || cs.Name != "tienda" || !cs.IfNotExists {
		t.Fatalf("unexpected statement %+v", stmt)
	}
	p = NewParser("CREATE SCHEMA tienda")
	stmt, _ = p.ParseStatement()
	if cs := stmt.(*ast.CreateSchema); cs.IfNotExists {
		t.Errorf("IfNotExists must default to false")
	}
}

func TestParseTableRefSchemaAndAlias(t *testing.T) {
	sel := parseSelectT(t, "SELECT p.nombre, v.monto FROM tienda.productos AS p INNER JOIN public.ventas v ON p.id = v.id LEFT JOIN cats ON cats.id = v.id")
	if sel.Table.Schema != "tienda" || sel.Table.Name != "productos" || sel.Table.Alias != "p" {
		t.Errorf("unexpected FROM table %+v", sel.Table)
	}
	if len(sel.Joins) != 2 {
		t.Fatalf("expected 2 joins, got %d", len(sel.Joins))
	}
	if j := sel.Joins[0].Table; j.Schema != "public" || j.Name != "ventas" || j.Alias != "v" {
		t.Errorf("unexpected first join table %+v", j)
	}
	if j := sel.Joins[1].Table; j.Schema != "" || j.Name != "cats" || j.Alias != "" {
		t.Errorf("unexpected second join table %+v", j)
	}
	// Bare alias on a simple FROM.
	sel = parseSelectT(t, "SELECT v.monto FROM ventas v WHERE v.monto > 1")
	if sel.Table.Name != "ventas" || sel.Table.Alias != "v" || sel.Table.Schema != "" {
		t.Errorf("unexpected bare alias parse %+v", sel.Table)
	}
}

func TestParseSetKeepsPayload(t *testing.T) {
	p := NewParser("SET search_path TO tienda, public")
	stmt, err := p.ParseStatement()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	set, ok := stmt.(*ast.Set)
	if !ok || set.Name != "search_path" || len(set.Values) != 2 || set.Values[0] != "tienda" || set.Values[1] != "public" {
		t.Fatalf("unexpected SET payload %+v", stmt)
	}
	p = NewParser("SET DateStyle = 'ISO'")
	stmt, _ = p.ParseStatement()
	if set := stmt.(*ast.Set); set.Name != "DateStyle" || len(set.Values) != 1 || set.Values[0] != "ISO" {
		t.Fatalf("unexpected SET payload %+v", stmt)
	}
}

func mustParseStmt(t *testing.T, sql string) ast.Statement {
	t.Helper()
	stmt, err := NewParser(sql).ParseStatement()
	if err != nil {
		t.Fatalf("parse %q: %v", sql, err)
	}
	return stmt
}

func mustFailParse(t *testing.T, sql string) {
	t.Helper()
	if _, err := NewParser(sql).ParseStatement(); err == nil {
		t.Fatalf("expected parse error for %q", sql)
	}
}

func findCheck(cs []ast.Constraint) *ast.CheckConstraint {
	for _, c := range cs {
		if chk, ok := c.(*ast.CheckConstraint); ok {
			return chk
		}
	}
	return nil
}

func TestParseCheckConstraint(t *testing.T) {
	// Column-level CHECK.
	ct := mustParseStmt(t, "CREATE TABLE t (id INT, edad INTEGER CHECK (edad >= 18))").(*ast.CreateTable)
	chk := findCheck(ct.Columns[1].Constraints)
	if chk == nil {
		t.Fatal("expected a column-level CHECK constraint")
	}
	if chk.Expr == nil || chk.Expr.Column.Name != "edad" || chk.Expr.Operator != ">=" {
		t.Fatalf("unexpected CHECK expr %+v", chk.Expr)
	}

	// Table-level named CHECK.
	ct = mustParseStmt(t, "CREATE TABLE t (id INT, precio INT, CONSTRAINT chk_p CHECK (precio > 0))").(*ast.CreateTable)
	chk = findCheck(ct.Constraints)
	if chk == nil || chk.Name != "chk_p" {
		t.Fatalf("expected a named table-level CHECK, got %+v", chk)
	}
}

func TestParseNamedConstraintForeignKey(t *testing.T) {
	ct := mustParseStmt(t, "CREATE TABLE hijo (id INT, pid INT, CONSTRAINT fk FOREIGN KEY (pid) REFERENCES padre(id))").(*ast.CreateTable)
	fk, ok := ct.Constraints[0].(*ast.ForeignKeyConstraint)
	if !ok || fk.ReferencedTable != "padre" || fk.ReferencedCol != "id" || fk.ColumnName != "pid" {
		t.Fatalf("unexpected FK constraint %+v", ct.Constraints[0])
	}
}

func TestParseCreateUniqueIndexAndDesc(t *testing.T) {
	ci := mustParseStmt(t, "CREATE UNIQUE INDEX ix ON t(email)").(*ast.CreateIndex)
	if !ci.Unique || ci.Name.Name != "ix" || ci.Columns[0].Name != "email" {
		t.Fatalf("unexpected unique index %+v", ci)
	}
	ci = mustParseStmt(t, "CREATE INDEX ix ON t(n DESC)").(*ast.CreateIndex)
	if ci.Unique || ci.Columns[0].Name != "n" {
		t.Fatalf("unexpected index with DESC %+v", ci)
	}
}

func TestParseMultiColumnUpdate(t *testing.T) {
	up := mustParseStmt(t, "UPDATE t SET a = 1, b = 'x', c = NULL WHERE id = 1").(*ast.Update)
	if len(up.Assignments) != 3 {
		t.Fatalf("expected 3 assignments, got %d", len(up.Assignments))
	}
	if up.Assignments[0].Column.Name != "a" || up.Assignments[1].Column.Name != "b" || up.Assignments[2].Column.Name != "c" {
		t.Fatalf("unexpected assignment columns %+v", up.Assignments)
	}
	if up.Assignments[2].Value.Kind != "null" {
		t.Fatalf("expected NULL literal, got %+v", up.Assignments[2].Value)
	}
}

func TestParseInsertNull(t *testing.T) {
	ins := mustParseStmt(t, "INSERT INTO t (x) VALUES (NULL)").(*ast.Insert)
	if ins.Rows[0][0].Kind != "null" {
		t.Fatalf("expected null literal, got %+v", ins.Rows[0][0])
	}
}

func TestParseUnion(t *testing.T) {
	so := mustParseStmt(t, "SELECT v FROM a UNION SELECT v FROM b UNION ALL SELECT v FROM c ORDER BY v").(*ast.SetOp)
	if len(so.Selects) != 3 {
		t.Fatalf("expected 3 operands, got %d", len(so.Selects))
	}
	if len(so.All) != 2 || so.All[0] != false || so.All[1] != true {
		t.Fatalf("unexpected UNION ALL flags %+v", so.All)
	}
	if len(so.OrderBy) != 1 || so.OrderBy[0].Column.Name != "v" {
		t.Fatalf("expected trailing ORDER BY v, got %+v", so.OrderBy)
	}
}

func TestParseSubqueries(t *testing.T) {
	// Scalar subquery in WHERE.
	sel := parseSelectT(t, "SELECT x FROM t WHERE x = (SELECT MAX(y) FROM u)")
	if sel.Where == nil || sel.Where.Sub == nil || sel.Where.Operator != "=" {
		t.Fatalf("expected scalar subquery, got %+v", sel.Where)
	}
	// IN subquery and IN list.
	sel = parseSelectT(t, "SELECT x FROM t WHERE x IN (SELECT y FROM u)")
	if sel.Where.Operator != "IN" || sel.Where.Sub == nil {
		t.Fatalf("expected IN subquery, got %+v", sel.Where)
	}
	sel = parseSelectT(t, "SELECT x FROM t WHERE x NOT IN (1, 2, 3)")
	if sel.Where.Operator != "IN" || !sel.Where.Negate || len(sel.Where.ValueList) != 3 {
		t.Fatalf("expected NOT IN list, got %+v", sel.Where)
	}
	// EXISTS.
	sel = parseSelectT(t, "SELECT x FROM t e WHERE EXISTS (SELECT 1 FROM u x WHERE x.id = e.id)")
	if sel.Where.Operator != "EXISTS" || sel.Where.Sub == nil {
		t.Fatalf("expected EXISTS, got %+v", sel.Where)
	}
	// Derived table.
	sel = parseSelectT(t, "SELECT s.x FROM (SELECT x FROM t) s")
	if sel.Table.Subquery == nil || sel.Table.Alias != "s" {
		t.Fatalf("expected derived table, got %+v", sel.Table)
	}
}

func TestParseCheckMissingParen(t *testing.T) {
	mustFailParse(t, "CREATE TABLE t (id INT, edad INT CHECK edad >= 18)")
}

func TestParseNegativeNumberLiteral(t *testing.T) {
	// In a VALUES tuple.
	ins := mustParseStmt(t, "INSERT INTO t (x) VALUES (-5)").(*ast.Insert)
	if ins.Rows[0][0].Kind != "number" || ins.Rows[0][0].Value != "-5" {
		t.Fatalf("expected number -5, got %+v", ins.Rows[0][0])
	}
	// In a WHERE comparison.
	sel := parseSelectT(t, "SELECT x FROM t WHERE x < -40")
	if sel.Where == nil || sel.Where.Value.Value != "-40" {
		t.Fatalf("expected WHERE x < -40, got %+v", sel.Where)
	}
	// In a CHECK expression.
	ct := mustParseStmt(t, "CREATE TABLE t (id INT, temp INT CHECK (temp >= -40))").(*ast.CreateTable)
	chk := findCheck(ct.Columns[1].Constraints)
	if chk == nil || chk.Expr.Value.Value != "-40" {
		t.Fatalf("expected CHECK temp >= -40, got %+v", chk)
	}
}
