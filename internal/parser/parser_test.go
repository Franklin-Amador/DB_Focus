package parser

import (
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
