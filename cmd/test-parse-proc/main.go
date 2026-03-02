package main

import (
	"dbf/internal/parser"
	"fmt"
)

func main() {
	q := `CREATE PROCEDURE agg_laptop() AS $$
BEGIN
	INSERT INTO fran.prods (nombre, precio) VALUES ('MSI PV', 900);
END;
$$;`
	p := parser.NewParser(q)
	stmt, err := p.ParseStatement()
	if err != nil {
		fmt.Printf("parse error: %v\n", err)
		return
	}
	fmt.Printf("parsed statement type: %T\n", stmt)
}
