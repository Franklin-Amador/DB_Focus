package main

import (
	"context"
	"fmt"
	"strings"

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
	fmt.Printf("expected RESTRICT error: %v\n", err)

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

	// recreate child and test CASCADE behavior
	_ = cat.CreateTable("child", []catalog.Column{{Name: "id", Type: "INTEGER"}, {Name: "parent_id", Type: "INTEGER"}, {Name: "label", Type: "TEXT"}}, []catalog.Constraint{{Type: constants.ConstraintForeignKey, ColumnName: "parent_id", ReferencedTable: "parent", ReferencedCol: "id"}}, "fran")

	p3 := parser.NewParser("DROP TABLE fran.parent CASCADE;")
	stmt3, err := p3.ParseStatement()
	if err != nil {
		panic(err)
	}
	if _, err := exe.Execute(context.Background(), stmt3); err != nil {
		fmt.Printf("unexpected drop parent CASCADE error: %v\n", err)
		return
	}
	fmt.Println("drop parent cascade ok")

	child, err := cat.GetTable("child", "fran")
	if err != nil {
		fmt.Printf("unexpected get child error: %v\n", err)
		return
	}
	for _, c := range child.Constraints {
		if c.Type == constants.ConstraintForeignKey && (c.ReferencedTable == "parent" || strings.HasSuffix(c.ReferencedTable, ".parent")) {
			fmt.Println("unexpected: foreign key still references dropped table")
			return
		}
	}
	fmt.Println("cascade cleaned foreign key dependencies")
}
