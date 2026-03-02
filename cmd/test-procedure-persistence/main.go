package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
	"dbf/internal/storage"
)

func execSQL(exe *executor.Executor, sql string) error {
	p := parser.NewParser(sql)
	for !p.AtEOF() {
		stmt, err := p.ParseStatement()
		if err != nil {
			return err
		}
		if stmt == nil {
			continue
		}
		if _, err := exe.Execute(context.Background(), stmt); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	dataDir := filepath.Join(".", "data_proc_test")
	_ = os.RemoveAll(dataDir)
	defer os.RemoveAll(dataDir)

	// Phase 1: create table + procedure
	st1, err := storage.NewPebbleStorage(dataDir)
	if err != nil {
		panic(err)
	}
	cat1 := catalog.New()
	if err := st1.LoadAll(cat1); err != nil {
		panic(err)
	}
	exe1 := executor.New(cat1, st1)

	if err := execSQL(exe1, "CREATE SCHEMA fran;"); err != nil {
		panic(err)
	}
	if err := execSQL(exe1, "CREATE TABLE fran.test (id INTEGER IDENTITY PRIMARY KEY, name TEXT);"); err != nil {
		panic(err)
	}
	if err := execSQL(exe1, "CREATE PROCEDURE agg_reg() AS $$ BEGIN INSERT INTO fran.test (name) VALUES ('Estiven'); END; $$;"); err != nil {
		panic(err)
	}
	if err := st1.Close(); err != nil {
		panic(err)
	}

	// Phase 2: reopen + call persisted procedure
	st2, err := storage.NewPebbleStorage(dataDir)
	if err != nil {
		panic(err)
	}
	defer st2.Close()
	cat2 := catalog.New()
	if err := st2.LoadAll(cat2); err != nil {
		panic(err)
	}
	exe2 := executor.New(cat2, st2)

	if err := execSQL(exe2, "CALL agg_reg();"); err != nil {
		panic(err)
	}

	tbl, err := cat2.GetTable("test", "fran")
	if err != nil {
		panic(err)
	}
	rows := tbl.SelectAll()
	fmt.Printf("rows after restart+call: %d\n", len(rows))
	for _, r := range rows {
		fmt.Printf("row: %v\n", r)
	}
}
