package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
	"dbf/internal/storage"
)

func mustExec(ctx context.Context, exe *executor.Executor, sql string) {
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for %q: %v", sql, err)
	}
	if _, err := exe.Execute(ctx, stmt); err != nil {
		log.Fatalf("execute error for %q: %v", sql, err)
	}
}

func main() {
	testDir := "./data_test_self_fk"
	defer os.RemoveAll(testDir)

	cat := catalog.New()
	st, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}
	defer st.Close()

	exe := executor.New(cat, st)
	ctx := context.Background()

	fmt.Println("=== Self Foreign Key Test ===")

	mustExec(ctx, exe, `CREATE TABLE categorias (
		id INT PRIMARY KEY,
		nombre TEXT,
		parent_id INT,
		FOREIGN KEY (parent_id) REFERENCES categorias(id)
	)`)
	fmt.Println("✓ Created table with self-referencing FK")

	mustExec(ctx, exe, "INSERT INTO categorias (id, nombre) VALUES (1, 'raiz')")
	fmt.Println("✓ Inserted root category")

	mustExec(ctx, exe, "INSERT INTO categorias (id, nombre, parent_id) VALUES (2, 'hijo', 1)")
	fmt.Println("✓ Inserted child category referencing parent id=1")

	p := parser.NewParser("INSERT INTO categorias (id, nombre, parent_id) VALUES (3, 'huerfano', 99)")
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error for orphan insert: %v", err)
	}
	if _, err := exe.Execute(ctx, stmt); err != nil {
		fmt.Printf("✓ Correctly rejected orphan insert: %v\n", err)
	} else {
		log.Fatal("expected foreign key violation for orphan insert")
	}

	fmt.Println("=== Self FK behavior verified ===")
}
