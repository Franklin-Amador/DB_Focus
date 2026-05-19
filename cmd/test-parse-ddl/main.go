package main

import (
	"dbf/internal/ast"
	"dbf/internal/parser"
	"fmt"
)

func main() {
	sql := `CREATE TABLE accounts (
    user_id SERIAL PRIMARY KEY,        -- Auto-incrementing unique ID
    username VARCHAR(50) UNIQUE NOT NULL, -- String up to 50 chars, must be unique
    email VARCHAR(255) UNIQUE NOT NULL,  -- String up to 255 chars, must be unique
    is_active BOOLEAN DEFAULT true,      -- Boolean with a default value
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP -- Automatically sets current time
);`

	p := parser.NewParser(sql)
	stmt, err := p.ParseStatement()
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		return
	}
	ct, ok := stmt.(*ast.CreateTable)
	if !ok {
		fmt.Printf("FAIL: expected CreateTable, got %T\n", stmt)
		return
	}
	fmt.Printf("OK - Table: %s\n", ct.Table.Name)
	for _, col := range ct.Columns {
		defVal := "<nil>"
		if col.DefaultVal != nil {
			defVal = col.DefaultVal.Value
		}
		fmt.Printf("  %-20s type=%-12s identity=%-5v notNull=%-5v default=%s constraints=%d\n",
			col.Name.Name, col.Type, col.Identity, col.NotNull, defVal, len(col.Constraints))
	}
}
