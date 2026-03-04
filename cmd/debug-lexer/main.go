package main

import (
	"fmt"

	"dbf/internal/parser"
)

func main() {
	sql := "CREATE JOB test_job INTERVAL 5 UNIT MINUTE BEGIN SELECT 1; END;"
	p := parser.NewParser(sql)

	for i := 0; i < 20; i++ {
		fmt.Printf("Token %d: Type=%v\n", i, p.CurToken().Type)
		if p.CurToken().Type == parser.TokenEOF {
			break
		}
		p.Next()
	}
}
