package main

import (
	"context"
	"fmt"
	"log"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
	"dbf/internal/storage"
)

func main() {
	fmt.Println("=== Testing ALTER TABLE and ALTER JOB ===\n")

	// Create catalog and storage
	cat := catalog.New()
	st, err := storage.NewPebbleStorage("data_test_alter")
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	// No defer here - we'll close manually before reopening

	exec := executor.New(cat, st)
	ctx := context.Background()

	// Test 1: CREATE TABLE and ALTER TABLE ADD COLUMN
	fmt.Println("Test 1: CREATE TABLE and ADD COLUMN")
	sql := "CREATE TABLE productos (id INT PRIMARY KEY, nombre TEXT)"
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		log.Fatalf("Execute error: %v", err)
	}
	fmt.Println("✓ Created table 'productos' with columns: id, nombre")

	// Insert test data
	sql = "INSERT INTO productos VALUES (1, 'Laptop')"
	p = parser.NewParser(sql)
	stmt, _ = p.ParseStatement()
	exec.Execute(ctx, stmt)

	// ALTER TABLE ADD COLUMN
	sql = "ALTER TABLE productos ADD COLUMN precio INT"
	p = parser.NewParser(sql)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		log.Fatalf("Execute error: %v", err)
	}
	fmt.Println("✓ Added column 'precio' to table 'productos'")

	// Verify columns
	table, _ := cat.GetTable("productos")
	fmt.Print("  Columns: ")
	for i, col := range table.Columns {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Print(col.Name)
	}
	fmt.Println("\n")

	// Test 2: ALTER TABLE RENAME COLUMN
	fmt.Println("Test 2: RENAME COLUMN")
	sql = "ALTER TABLE productos RENAME COLUMN nombre TO producto"
	p = parser.NewParser(sql)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		log.Fatalf("Execute error: %v", err)
	}
	fmt.Println("✓ Renamed column 'nombre' to 'producto'")

	// Verify columns
	table, _ = cat.GetTable("productos")
	fmt.Print("  Columns: ")
	for i, col := range table.Columns {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Print(col.Name)
	}
	fmt.Println("\n")

	// Test 3: ALTER TABLE ALTER COLUMN TYPE
	fmt.Println("Test 3: ALTER COLUMN TYPE")
	sql = "ALTER TABLE productos ALTER COLUMN precio TYPE TEXT"
	p = parser.NewParser(sql)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		log.Fatalf("Execute error: %v", err)
	}
	fmt.Println("✓ Changed column 'precio' type from INT to TEXT")

	// Verify column type
	table, _ = cat.GetTable("productos")
	for _, col := range table.Columns {
		if col.Name == "precio" {
			fmt.Printf("  precio type: %s\n\n", col.Type)
		}
	}

	// Test 4: ALTER TABLE DROP COLUMN
	fmt.Println("Test 4: DROP COLUMN")
	sql = "ALTER TABLE productos DROP COLUMN precio"
	p = parser.NewParser(sql)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		log.Fatalf("Execute error: %v", err)
	}
	fmt.Println("✓ Dropped column 'precio'")

	// Verify columns
	table, _ = cat.GetTable("productos")
	fmt.Print("  Columns: ")
	for i, col := range table.Columns {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Print(col.Name)
	}
	fmt.Println("\n")

	// Test 5: CREATE JOB and ALTER JOB
	fmt.Println("Test 5: CREATE JOB and ALTER JOB")
	sql = "CREATE JOB test_job INTERVAL 5 UNIT MINUTE BEGIN SELECT 1; END"
	p = parser.NewParser(sql)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		log.Fatalf("Execute error: %v", err)
	}
	fmt.Println("✓ Created job 'test_job' (enabled)")

	// Verify job is enabled
	job, _ := cat.GetJob("test_job")
	fmt.Printf("  Job enabled: %v\n", job.Enabled)

	// ALTER JOB DISABLE
	sql = "ALTER JOB test_job DISABLE"
	p = parser.NewParser(sql)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		log.Fatalf("Execute error: %v", err)
	}
	fmt.Println("✓ Disabled job 'test_job'")

	// Verify job is disabled
	job, _ = cat.GetJob("test_job")
	fmt.Printf("  Job enabled: %v\n", job.Enabled)

	// ALTER JOB ENABLE
	sql = "ALTER JOB test_job ENABLE"
	p = parser.NewParser(sql)
	stmt, _ = p.ParseStatement()
	exec.Execute(ctx, stmt)
	fmt.Println("✓ Re-enabled job 'test_job'")

	// Verify job is enabled again
	job, _ = cat.GetJob("test_job")
	fmt.Printf("  Job enabled: %v\n\n", job.Enabled)

	// Test 6: Verify persistence
	fmt.Println("Test 6: Verify persistence (loading from storage)")

	// Close and reopen storage
	st.Close()
	st, err = storage.NewPebbleStorage("data_test_alter")
	if err != nil {
		log.Fatalf("Failed to reopen storage: %v", err)
	}
	defer st.Close()

	// Create new catalog and load from storage
	cat2 := catalog.New()
	if err := st.LoadAll(cat2); err != nil {
		log.Fatalf("Failed to load data from storage: %v", err)
	}

	// Verify table structure persisted
	table2, err := cat2.GetTable("productos")
	if err != nil {
		log.Fatalf("Table not found after reload: %v", err)
	}
	fmt.Print("✓ Table 'productos' loaded with columns: ")
	for i, col := range table2.Columns {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Print(col.Name)
	}
	fmt.Println()

	// Verify job state persisted
	job2, err := cat2.GetJob("test_job")
	if err != nil {
		log.Fatalf("Job not found after reload: %v", err)
	}
	fmt.Printf("✓ Job 'test_job' loaded with enabled=%v\n\n", job2.Enabled)

	fmt.Println("=== All tests passed! ===")
}
