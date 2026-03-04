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
	fmt.Println("=== Testing ALTER TABLE Constraint Validations ===\n")

	// Create catalog and storage
	cat := catalog.New()
	st, err := storage.NewPebbleStorage("data_test_constraints")
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer st.Close()

	exec := executor.New(cat, st)
	ctx := context.Background()

	// Test 1: Create table with PRIMARY KEY
	fmt.Println("Test 1: Create table with PRIMARY KEY")
	sql := "CREATE TABLE users (id INT PRIMARY KEY, name TEXT)"
	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		log.Fatalf("Execute error: %v", err)
	}
	fmt.Println("✓ Created table 'users' with PRIMARY KEY on 'id'\n")

	// Test 2: Try to add another PRIMARY KEY (should fail)
	fmt.Println("Test 2: Try to add another PRIMARY KEY (should fail)")
	sql = "ALTER TABLE users ADD COLUMN email TEXT PRIMARY KEY"
	p = parser.NewParser(sql)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		fmt.Printf("✓ Correctly rejected: %v\n\n", err)
	} else {
		log.Fatalf("ERROR: Should have rejected duplicate PRIMARY KEY!\n")
	}

	// Test 3: Create referenced table and FK
	fmt.Println("Test 3: Create orders table with FOREIGN KEY")
	sql = "CREATE TABLE orders (order_id INT PRIMARY KEY, user_id INT, FOREIGN KEY (user_id) REFERENCES users(id))"
	p = parser.NewParser(sql)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		log.Fatalf("Execute error: %v", err)
	}
	fmt.Println("✓ Created table 'orders' with FK referencing users(id)\n")

	// Test 4: Try to drop column referenced by FK (should fail)
	fmt.Println("Test 4: Try to drop column 'id' from users (should fail)")
	sql = "ALTER TABLE users DROP COLUMN id"
	p = parser.NewParser(sql)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		fmt.Printf("✓ Correctly rejected: %v\n\n", err)
	} else {
		log.Fatalf("ERROR: Should have rejected dropping FK-referenced column!\n")
	}

	// Test 5: Try to drop PRIMARY KEY column (should fail)
	fmt.Println("Test 5: Try to drop PRIMARY KEY column from orders (should fail)")
	sql = "ALTER TABLE orders DROP COLUMN order_id"
	p = parser.NewParser(sql)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		fmt.Printf("✓ Correctly rejected: %v\n\n", err)
	} else {
		log.Fatalf("ERROR: Should have rejected dropping PRIMARY KEY column!\n")
	}

	// Test 6: Drop non-constrained column (should succeed)
	fmt.Println("Test 6: Drop non-constrained column 'name' from users (should succeed)")
	sql = "ALTER TABLE users DROP COLUMN name"
	p = parser.NewParser(sql)
	stmt, err = p.ParseStatement()
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	if _, err := exec.Execute(ctx, stmt); err != nil {
		log.Fatalf("Failed to drop column: %v", err)
	}

	// Verify column was dropped
	table, _ := cat.GetTable("users")
	fmt.Print("✓ Successfully dropped 'name'. Remaining columns: ")
	for i, col := range table.Columns {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Print(col.Name)
	}
	fmt.Println("\n")

	fmt.Println("=== All constraint validation tests passed! ===")
}
