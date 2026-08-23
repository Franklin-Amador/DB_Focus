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

// runAll parses and executes every statement in sql (multi-statement aware),
// returning the last statement's result — matching the server's Handle().
func runAll(ctx context.Context, e *executor.Executor, sql string) (*executor.Result, error) {
	p := parser.NewParser(sql)
	var last *executor.Result
	for !p.AtEOF() {
		st, err := p.ParseStatement()
		if err != nil {
			return nil, err
		}
		if st == nil {
			continue
		}
		r, err := e.Execute(ctx, st)
		if err != nil {
			return nil, err
		}
		last = r
	}
	return last, nil
}

func fresh(ctx context.Context) *executor.Executor {
	e := executor.New(catalog.New(), nil)
	setup := "CREATE TABLE jardin (id INT IDENTITY PRIMARY KEY, planta TEXT, altura INT);\n" +
		"INSERT INTO jardin (planta,altura) VALUES ('Helecho',40);\n" +
		"INSERT INTO jardin (planta,altura) VALUES ('Monstera',120);\n" +
		"INSERT INTO jardin (planta,altura) VALUES ('Pothos',25);\n"
	if _, err := runAll(ctx, e, setup); err != nil {
		log.Fatalf("setup failed: %v", err)
	}
	return e
}

func expectRows(ctx context.Context, name, sql string, want int) {
	e := fresh(ctx)
	r, err := runAll(ctx, e, sql)
	if err != nil {
		log.Fatalf("[FAIL] %s: %v", name, err)
	}
	got := 0
	if r != nil {
		got = len(r.Rows)
	}
	if got != want {
		log.Fatalf("[FAIL] %s: expected %d rows, got %d (%+v)", name, want, got, r)
	}
	fmt.Printf("OK  %-46s -> %d rows\n", name, got)
}

func main() {
	fmt.Println("=== Testing SQL with line breaks (LF & CRLF) ===")
	ctx := context.Background()

	// A single statement split across LF lines.
	expectRows(ctx, "single stmt multiline (LF)",
		"SELECT planta, altura\nFROM jardin\nWHERE altura > 30\nORDER BY altura DESC", 2)

	// Same statement with Windows CRLF line endings.
	expectRows(ctx, "single stmt multiline (CRLF)",
		"SELECT planta, altura\r\nFROM jardin\r\nWHERE altura > 30\r\nORDER BY altura DESC", 2)

	// Multiple statements separated by newline + semicolon.
	expectRows(ctx, "multi-stmt via newline",
		"UPDATE jardin SET altura = 200 WHERE planta = 'Pothos';\nSELECT planta FROM jardin WHERE altura > 30", 3)

	// Newline immediately after keywords + irregular indentation.
	expectRows(ctx, "newline after keyword + indent",
		"SELECT\n    planta,\n    altura\nFROM\n    jardin\nWHERE altura >= 40", 2)

	// Blank lines between clauses.
	expectRows(ctx, "blank lines between clauses",
		"SELECT planta, altura\n\n\nFROM jardin\n\nORDER BY altura ASC", 3)

	// Whole multi-statement script in CRLF, ending in an aggregate.
	expectRows(ctx, "aggregate over CRLF script",
		strings.ReplaceAll("SELECT COUNT(*)\nFROM jardin", "\n", "\r\n"), 1)

	fmt.Println("=== Line-break SQL test passed ===")
}
