package main

import (
	"dbf/internal/ast"
	"dbf/internal/parser"
	"fmt"
)

func main() {
	input := "SET DateStyle=ISO; SELECT version();"
	p := parser.NewParser(input)

	fmt.Printf("Input query: %s\n\n", input)

	var statements []ast.Statement
	stmtCount := 0

	for !p.AtEOF() {
		stmt, err := p.ParseStatement()
		if err != nil {
			fmt.Printf("❌ Error parsing statement %d: %v\n", stmtCount+1, err)
			return
		}
		if stmt != nil {
			stmtCount++
			statements = append(statements, stmt)
			fmt.Printf("✅ Statement %d: %T\n", stmtCount, stmt)
		}
	}

	fmt.Printf("\n📊 Summary: parsed %d statements successfully\n", len(statements))

	if len(statements) == 2 {
		fmt.Println("\n✅ Multi-statement parsing works correctly!")
		fmt.Println("   - First statement: SET")
		fmt.Println("   - Second statement: SELECT")
	} else {
		fmt.Printf("\n❌ Expected 2 statements but got %d\n", len(statements))
	}
}
