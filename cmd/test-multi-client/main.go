package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

func main() {
	// Connect to FocusD
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

	// Test multi-statement query
	query := "SET DateStyle=ISO; SELECT version();"
	fmt.Printf("\n📝 Executing query: %s\n\n", query)

	rows, err := db.Query(query)
	if err != nil {
		log.Fatalf("❌ Query failed: %v", err)
	}
	defer rows.Close()

	// Get column names
	cols, err := rows.Columns()
	if err != nil {
		log.Fatalf("Failed to get columns: %v", err)
	}

	fmt.Printf("📊 Columns: %v\n", cols)

	// Fetch results
	count := 0
	for rows.Next() {
		// Create a slice to hold the values
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			log.Fatalf("Failed to scan row: %v", err)
		}

		count++
		fmt.Printf("Row %d: ", count)
		for i, col := range cols {
			fmt.Printf("%s=%v ", col, values[i])
		}
		fmt.Println()
	}

	if err := rows.Err(); err != nil {
		log.Fatalf("Row iteration error: %v", err)
	}

	fmt.Printf("\n✅ Query executed successfully! Rows returned: %d\n", count)
	fmt.Println("\n🎉 Multi-statement execution works!")
}
