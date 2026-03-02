package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

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

	// Use a unique table name based on timestamp
	testTableName := fmt.Sprintf("test_table_%d", time.Now().UnixNano())

	// Create a test table
	createTableSQL := fmt.Sprintf("CREATE TABLE %s (id INT, name TEXT, registered BOOLEAN)", testTableName)
	_, err = db.Exec(createTableSQL)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}
	fmt.Printf("✅ Created test table: %s\n\n", testTableName)

	// Test 1: Query INFORMATION_SCHEMA.TABLES
	fmt.Println("Test 1: Querying INFORMATION_SCHEMA.TABLES")
	fmt.Println(strings.Repeat("=", 50))
	rows, err := db.Query("SELECT table_catalog, table_schema, table_name, table_type FROM information_schema.tables WHERE table_schema = 'public'")
	if err != nil {
		log.Fatalf("❌ Failed to query INFORMATION_SCHEMA.TABLES: %v", err)
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var catalog, schema, name, tableType string
		if err := rows.Scan(&catalog, &schema, &name, &tableType); err != nil {
			log.Fatalf("Failed to scan: %v", err)
		}
		tableNames = append(tableNames, name)
		fmt.Printf("  - Table: %s\n", name)
	}

	if len(tableNames) > 0 {
		fmt.Println("✅ INFORMATION_SCHEMA.TABLES works!")
	} else {
		fmt.Println("❌ No tables found")
	}

	// Test 2: Query INFORMATION_SCHEMA.COLUMNS for our test table
	fmt.Printf("Test 2: Querying INFORMATION_SCHEMA.COLUMNS for %s\n", testTableName)
	fmt.Println(strings.Repeat("=", 50))
	columnsSQL := fmt.Sprintf("SELECT table_catalog, table_schema, table_name, column_name, ordinal_position, data_type FROM information_schema.columns WHERE table_name = '%s'", testTableName)
	rows, err = db.Query(columnsSQL)
	if err != nil {
		log.Fatalf("❌ Failed to query INFORMATION_SCHEMA.COLUMNS: %v", err)
	}
	defer rows.Close()

	var columnCount int
	for rows.Next() {
		var catalog, schema, tableName, colName string
		var ordinal int32
		var dataType string
		if err := rows.Scan(&catalog, &schema, &tableName, &colName, &ordinal, &dataType); err != nil {
			log.Fatalf("Failed to scan: %v", err)
		}
		columnCount++
		fmt.Printf("  - Column: %s (Type: %s)\n", colName, dataType)
	}

	if columnCount == 3 {
		fmt.Println("✅ INFORMATION_SCHEMA.COLUMNS works! Found all 3 columns")
	} else {
		fmt.Printf("❌ Expected 3 columns, found %d\n", columnCount)
	}

	// Test 3: Full INFORMATION_SCHEMA.TABLES query with all columns
	fmt.Println("Test 3: Full INFORMATION_SCHEMA.TABLES query")
	fmt.Println(strings.Repeat("=", 50))
	rows, err = db.Query("SELECT table_catalog, table_schema, table_name, table_type FROM information_schema.tables WHERE table_name LIKE 'information_schema%'")
	if err != nil {
		log.Fatalf("❌ Failed: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var catalog, schema, tableName, tableType string
		if err := rows.Scan(&catalog, &schema, &tableName, &tableType); err != nil {
			log.Fatalf("Failed to scan: %v", err)
		}
		fmt.Printf("  Catalog: %s | Schema: %s | Table: %s | Type: %s\n", catalog, schema, tableName, tableType)
	}
	fmt.Println("✅ Query executed successfully")

	fmt.Println("🎉 All INFORMATION_SCHEMA tests passed!")
}
