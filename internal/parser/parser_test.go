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
