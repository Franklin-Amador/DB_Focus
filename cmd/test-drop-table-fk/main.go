package main

import (
	"context"
	"fmt"

	"dbf/internal/catalog"
	"dbf/internal/constants"
	"dbf/internal/executor"
	"dbf/internal/parser"
)

func main() {
	cat := catalog.New()
	exe := executor.New(cat, nil)

	_ = cat.CreateSchema("fran")
	_ = cat.CreateTable("parent", []catalog.Column{{Name: "id", Type: "INTEGER"}}, []catalog.Constraint{{Type: constants.ConstraintPrimaryKey, ColumnName: "id"}}, "fran")
	_ = cat.CreateTable("child", []catalog.Column{{Name: "id", Type: "INTEGER"}, {Name: "parent_id", Type: "INTEGER"}}, []catalog.Constraint{{Type: constants.ConstraintForeignKey, ColumnName: "parent_id", ReferencedTable: "parent", ReferencedCol: "id"}}, "fran")

	p := parser.NewParser("DROP TABLE fran.parent;")
	stmt, err := p.ParseStatement()
	if err != nil {
		panic(err)
	}

	_, err = exe.Execute(context.Background(), stmt)
	if err == nil {
		fmt.Println("unexpected: drop succeeded")
		return
	}
	fmt.Printf("expected error: %v\n", err)

	// drop non-referenced table should succeed
	p2 := parser.NewParser("DROP TABLE fran.child;")
	stmt2, err := p2.ParseStatement()
	if err != nil {
		panic(err)
	}
	if _, err := exe.Execute(context.Background(), stmt2); err != nil {
		fmt.Printf("unexpected drop child error: %v\n", err)
		return
	}
	fmt.Println("drop child ok")
}
