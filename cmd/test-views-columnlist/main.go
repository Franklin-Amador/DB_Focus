package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

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
	fmt.Println("=== Test CREATE VIEW with explicit column list ===\n")

	// Setup storage and catalog
	testDir := "./data_test_views_columnlist"
	defer os.RemoveAll(testDir)

	cat := catalog.New()
	st, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}
	defer st.Close()

	exe := executor.New(cat, st)
	ctx := context.Background()

	// Phase 1: Create table
	fmt.Println("Phase 1: Creating test table...")
	mustExec(ctx, exe, `
		CREATE TABLE products (
			id INT PRIMARY KEY,
			product_name TEXT,
			price INT
		)
	`)
	fmt.Println("✓ Created table: products")

	// Phase 2: Insert test data
	fmt.Println("\nPhase 2: Inserting test data...")
	mustExec(ctx, exe, "INSERT INTO products VALUES (1, 'Laptop', 999)")
	mustExec(ctx, exe, "INSERT INTO products VALUES (2, 'Mouse', 29)")
	mustExec(ctx, exe, "INSERT INTO products VALUES (3, 'Keyboard', 79)")
	fmt.Println("✓ Inserted 3 products")

	// Phase 3: Create view with explicit column names
	fmt.Println("\nPhase 3: Creating view with explicit column names...")
	mustExec(ctx, exe, `
		CREATE VIEW v_products (prod_id, prod_name, prod_price) AS 
		SELECT id, product_name, price FROM products
	`)
	fmt.Println("✓ Created view with explicit columns: v_products (prod_id, prod_name, prod_price)")

	// Phase 4: Query the view and verify column names
	fmt.Println("\nPhase 4: Querying view and verifying column names...")
	p := parser.NewParser("SELECT * FROM v_products")
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}
	result, err := exe.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("execute error: %v", err)
	}
	if len(result.Columns) != 3 {
		log.Fatalf("Expected 3 columns, got %d", len(result.Columns))
	}
	expectedCols := []string{"prod_id", "prod_name", "prod_price"}
	for i, col := range result.Columns {
		if col != expectedCols[i] {
			log.Fatalf("Column %d: expected %s, got %s", i, expectedCols[i], col)
		}
	}
	fmt.Printf("✓ View columns: %v\n", result.Columns)
	fmt.Printf("✓ View has %d rows\n", len(result.Rows))

	// Phase 5: Test CREATE VIEW with mismatched column count
	fmt.Println("\nPhase 5: Testing error handling for mismatched column count...")
	p = parser.NewParser(`CREATE VIEW v_bad (col1, col2) AS SELECT id, product_name, price FROM products`)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}
	_, err = exe.Execute(ctx, stmt)
	if err == nil {
		log.Fatalf("Expected error for mismatched column count, but succeeded")
	}
	if !strings.Contains(err.Error(), "column list has") {
		log.Fatalf("Expected column mismatch error, got: %v", err)
	}
	fmt.Printf("✓ Correctly rejected mismatched column count\n")

	// Phase 6: Test CREATE VIEW with duplicate column names
	fmt.Println("\nPhase 6: Testing error handling for duplicate column names...")
	p = parser.NewParser(`CREATE VIEW v_dup (col1, col2, col1) AS SELECT id, product_name, price FROM products`)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}
	_, err = exe.Execute(ctx, stmt)
	if err == nil {
		log.Fatalf("Expected error for duplicate column names, but succeeded")
	}
	if !strings.Contains(err.Error(), "duplicate column name") {
		log.Fatalf("Expected duplicate column error, got: %v", err)
	}
	fmt.Printf("✓ Correctly rejected duplicate column names\n")

	// Phase 7: Test CREATE OR REPLACE with column list
	fmt.Println("\nPhase 7: Testing CREATE OR REPLACE VIEW with column list...")
	mustExec(ctx, exe, `
		CREATE OR REPLACE VIEW v_products (product_id, product_title, product_cost) AS 
		SELECT id, product_name, price FROM products
	`)
	fmt.Println("✓ Replaced view with different column names")

	// Verify replaced columns
	p = parser.NewParser("SELECT * FROM v_products")
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}
	result, err = exe.Execute(ctx, stmt)
	if err != nil {
		log.Fatalf("execute error: %v", err)
	}
	expectedCols = []string{"product_id", "product_title", "product_cost"}
	for i, col := range result.Columns {
		if col != expectedCols[i] {
			log.Fatalf("Column %d: expected %s, got %s", i, expectedCols[i], col)
		}
	}
	fmt.Printf("✓ Replaced view has correct columns: %v\n", result.Columns)

	// Phase 8: Test persistence
	fmt.Println("\nPhase 8: Testing persistence across restart...")
	cat2 := catalog.New()
	if err := st.LoadAll(cat2); err != nil {
		log.Fatalf("Failed to load catalog on restart: %v", err)
	}
	view, err := cat2.GetView("v_products", "public")
	if err != nil {
		log.Fatalf("Failed to get persisted view: %v", err)
	}
	fmt.Printf("✓ View persisted with %d columns\n", len(view.Columns))
	if len(view.Columns) == 3 {
		fmt.Printf("  Columns: %s, %s, %s\n", view.Columns[0].Name, view.Columns[1].Name, view.Columns[2].Name)
	}

	fmt.Println("\n=== All tests passed! ===")
}
