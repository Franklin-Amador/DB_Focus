package main

import (
	"fmt"
	"os"
	"path/filepath"

	"context"
	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
	"dbf/internal/storage"
)

func runPhase(dataDir string) error {
	// ensure data dir exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}

	st, err := storage.NewPebbleStorage(dataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	cat := catalog.New()
	if err := st.LoadAll(cat); err != nil {
		return err
	}

	exe := executor.New(cat, st)

	queries := []string{
		"CREATE SCHEMA myschema;",
		"CREATE TABLE myschema.test (id INT PRIMARY KEY, name TEXT);",
		"INSERT INTO myschema.test (id, name) VALUES (1, 'a');",
	}

	for _, q := range queries {
		p := parser.NewParser(q)
		for !p.AtEOF() {
			stmt, err := p.ParseStatement()
			if err != nil {
				return fmt.Errorf("parse error for '%s': %w", q, err)
			}
			if stmt == nil {
				continue
			}
			// debug: show parsed statement info
			switch s := stmt.(type) {
			case *ast.CreateTable:
				fmt.Printf("Parsed CREATE TABLE: name=%s schema=%s\n", s.Table.Name, s.Table.Alias)
			case *ast.CreateSchema:
				fmt.Printf("Parsed CREATE SCHEMA: name=%s\n", s.Name)
			}
			if _, err := exe.Execute(context.Background(), stmt); err != nil {
				return fmt.Errorf("exec error for '%s': %w", q, err)
			}
		}
	}

	// Inspect persisted metadata
	fmt.Println("Persisted metadata schemas:")
	for s, tbls := range st.Meta().Tables {
		fmt.Printf(" - %s: %d tables\n", s, len(tbls))
		for t := range tbls {
			fmt.Printf("    - %s\n", t)
		}
	}

	return nil
}

func verifyPhase(dataDir string) error {
	st, err := storage.NewPebbleStorage(dataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	cat := catalog.New()
	if err := st.LoadAll(cat); err != nil {
		return err
	}

	// Try to get table myschema.test
	table, err := cat.GetTable("test", "myschema")
	if err != nil {
		return fmt.Errorf("table not found after reload: %w", err)
	}
	rows := table.SelectAll()
	fmt.Printf("Loaded table %s.%s with %d rows\n", "myschema", table.Name, len(rows))
	for _, r := range rows {
		fmt.Printf(" row: %v\n", r)
	}
	return nil
}

func main() {
	dataDir := filepath.Join("..", "data_test_run")
	// remove previous test data
	_ = os.RemoveAll(dataDir)

	fmt.Println("Phase 1: create schema/table/insert")
	if err := runPhase(dataDir); err != nil {
		fmt.Printf("phase1 error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Phase 2: reopen and verify")
	if err := verifyPhase(dataDir); err != nil {
		fmt.Printf("verify error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Done")
}
