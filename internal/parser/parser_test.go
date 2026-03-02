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
