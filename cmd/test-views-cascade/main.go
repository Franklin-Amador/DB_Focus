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
	fmt.Println("=== Test DROP VIEW CASCADE/RESTRICT ===\n")

	// Setup storage and catalog
	testDir := "./data_test_views_cascade"
	defer os.RemoveAll(testDir)

	cat := catalog.New()
	st, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}
	defer st.Close()

	exe := executor.New(cat, st)
	ctx := context.Background()

	// Phase 1: Create base table and views
	fmt.Println("Phase 1: Creating table and view hierarchy...")
	mustExec(ctx, exe, "CREATE TABLE products (id INT PRIMARY KEY, name TEXT)")
	mustExec(ctx, exe, "INSERT INTO products VALUES (1, 'Laptop')")
	mustExec(ctx, exe, "INSERT INTO products VALUES (2, 'Mouse')")
	mustExec(ctx, exe, "CREATE VIEW v_products AS SELECT id, name FROM products")
	fmt.Println("✓ Created base table: products")
	fmt.Println("✓ Created base view: v_products")

	// Phase 2: Create dependent views
	fmt.Println("\nPhase 2: Creating dependent (nested) views...")
	mustExec(ctx, exe, "CREATE VIEW v_products_readonly AS SELECT id FROM v_products")
	mustExec(ctx, exe, "CREATE VIEW v_products_info (pid, pname) AS SELECT id, name FROM v_products")
	fmt.Println("✓ Created dependent view: v_products_readonly")
	fmt.Println("✓ Created dependent view: v_products_info")

	// Phase 3: Test RESTRICT (should fail with dependencies)
	fmt.Println("\nPhase 3: Testing DROP VIEW RESTRICT with dependencies...")
	p := parser.NewParser("DROP VIEW v_products RESTRICT")
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}
	_, err = exe.Execute(ctx, stmt)
	if err == nil {
		log.Fatalf("Expected error when dropping view with dependencies using RESTRICT, but succeeded")
	}
	if !strings.Contains(err.Error(), "cannot drop view") {
		log.Fatalf("Expected 'cannot drop view' error, got: %v", err)
	}
	fmt.Printf("✓ Correctly rejected: %v\n", err)

	// Phase 4: Test CASCADE (should drop dependent views)
	fmt.Println("\nPhase 4: Testing DROP VIEW CASCADE...")
	mustExec(ctx, exe, "DROP VIEW v_products CASCADE")
	fmt.Println("✓ Dropped v_products with CASCADE (should also drop v_products_readonly and v_products_info)")

	// Phase 5: Verify dependent views were dropped
	fmt.Println("\nPhase 5: Verifying dependent views were dropped...")
	p = parser.NewParser("SELECT * FROM v_products_readonly")
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}
	_, err = exe.Execute(ctx, stmt)
	if err == nil {
		log.Fatalf("Expected error selecting from dropped view v_products_readonly, but succeeded")
	}
	fmt.Println("✓ v_products_readonly was dropped (selecting from it fails)")

	// Phase 6: Create a new hierarchy
	fmt.Println("\nPhase 6: Creating new view hierarchy for IF EXISTS CASCADE test...")
	mustExec(ctx, exe, "CREATE VIEW v_products2 AS SELECT id FROM products")
	mustExec(ctx, exe, "CREATE VIEW v_products2_derived AS SELECT id FROM v_products2")
	fmt.Println("✓ Created v_products2")
	fmt.Println("✓ Created v_products2_derived")

	// Phase 7: Test IF EXISTS CASCADE on non-existent view (should not error)
	fmt.Println("\nPhase 7: Testing DROP VIEW IF EXISTS CASCADE on non-existent view...")
	mustExec(ctx, exe, "DROP VIEW IF EXISTS non_existent CASCADE")
	fmt.Println("✓ No error when dropping non-existent view with IF EXISTS CASCADE")

	// Phase 8: Test DROP VIEW without CASCADE/RESTRICT (defaults to RESTRICT)
	fmt.Println("\nPhase 8: Testing DROP VIEW without CASCADE/RESTRICT (defaults to RESTRICT)...")
	p = parser.NewParser("DROP VIEW v_products2")
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}
	_, err = exe.Execute(ctx, stmt)
	if err == nil {
		log.Fatalf("Expected error when dropping view with dependencies (no CASCADE specified), but succeeded")
	}
	fmt.Printf("✓ Correctly rejected (default behavior is RESTRICT): %v\n", err)

	// Phase 9: Drop with no dependencies
	fmt.Println("\nPhase 9: Testing DROP VIEW on view with no dependencies...")
	mustExec(ctx, exe, "CREATE VIEW v_standalone AS SELECT id FROM products")
	mustExec(ctx, exe, "DROP VIEW v_standalone")
	fmt.Println("✓ Successfully dropped standalone view")

	fmt.Println("\n=== All CASCADE/RESTRICT tests passed! ===")
}
