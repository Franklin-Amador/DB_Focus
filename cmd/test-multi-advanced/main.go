package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

func main() {
	connStr := "host=localhost port=4444 user=postgres password=4444 dbname=postgres sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping: %v", err)
	}

	fmt.Println("✅ Connected to FocusD")

	// Drop table if exists
	fmt.Println("Cleaning up test_multi table...")
	db.Exec("DROP TABLE test_multi")

	// Test 1: Multiple SETs and SELECT
	test1 := "SET DateStyle=ISO; SET TimeZone=UTC; SELECT version();"
	fmt.Printf("Test 1: Multiple configurations + SELECT\n")
	fmt.Printf("Query: %s\n", test1)
	if err := executeAndPrint(db, test1); err != nil {
		log.Printf("❌ Test 1 failed: %v\n", err)
	} else {
		fmt.Println("✅ Test 1 passed")
	}

	// Test 2: CREATE TABLE and INSERT
	test2 := `CREATE TABLE test_multi (id INT, name TEXT);
	          INSERT INTO test_multi (id, name) VALUES (1, 'test');`
	fmt.Printf("Test 2: CREATE + INSERT\n")
	fmt.Printf("Query: %s\n", test2)
	if err := executeAndPrint(db, test2); err != nil {
		log.Printf("❌ Test 2 failed: %v\n", err)
	} else {
		fmt.Println("✅ Test 2 passed")
	}

	// Test 3: INSERT and SELECT
	test3 := `INSERT INTO test_multi (id, name) VALUES (2, 'another');
	          SELECT * FROM test_multi;`
	fmt.Printf("Test 3: INSERT + SELECT\n")
	fmt.Printf("Query: %s\n", test3)
	if err := executeAndPrint(db, test3); err != nil {
		log.Printf("❌ Test 3 failed: %v\n", err)
	} else {
		fmt.Println("✅ Test 3 passed")
	}

	// Test 4: UPDATE and SELECT
	test4 := `UPDATE test_multi SET name='updated' WHERE id=1;
	          SELECT * FROM test_multi WHERE id=1;`
	fmt.Printf("Test 4: UPDATE + SELECT\n")
	fmt.Printf("Query: %s\n", test4)
	if err := executeAndPrint(db, test4); err != nil {
		log.Printf("❌ Test 4 failed: %v\n", err)
	} else {
		fmt.Println("✅ Test 4 passed")
	}

	fmt.Println("\n🎉 All multi-statement tests completed!")
}

func executeAndPrint(db *sql.DB, query string) error {
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	if len(cols) > 0 {
		fmt.Printf("Columns: %v\n", cols)
	}

	count := 0
	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return err
		}

		count++
		fmt.Printf("  Row %d: ", count)
		for i, col := range cols {
			fmt.Printf("%s=%v ", col, values[i])
		}
		fmt.Println()
	}

	if count > 0 {
		fmt.Printf("Rows returned: %d\n", count)
	}

	return rows.Err()
}
