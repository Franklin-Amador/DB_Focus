package ast

import "strings"

// ApplyDefaultSchema qualifies every unqualified table reference in stmt with
// schema, the way a session's default schema (PostgreSQL's search_path) does.
// It is applied by the wire protocol (connection database), by the GUI (active
// schema) and by the catalog when a view is stored, so a view keeps resolving
// its tables inside its own schema after being re-parsed from text.
//
// SELECT tables (FROM/JOIN, recursively through CTEs and view bodies) receive
// Identifier.Schema; CTE names are left untouched because they are temporary
// and always unqualified. DDL and DML statements keep the parser's convention
// of carrying the schema in Identifier.Alias. Fields already set are never
// overwritten, so applying the same schema twice is a no-op.
func ApplyDefaultSchema(stmt Statement, schema string) {
	if schema == "" {
		return
	}
	switch s := stmt.(type) {
	case *Select:
		ApplyDefaultSchemaToSelect(s, schema)
	case *Insert:
		setAliasSchema(&s.Table, schema)
	case *Update:
		setAliasSchema(&s.Table, schema)
	case *Delete:
		setAliasSchema(&s.Table, schema)
	case *CreateTable:
		setAliasSchema(&s.Table, schema)
	case *DropTable:
		setAliasSchema(&s.Table, schema)
	case *AlterTable:
		setAliasSchema(&s.Table, schema)
	case *CreateIndex:
		setAliasSchema(&s.Table, schema)
	case *CreateView:
		setAliasSchema(&s.Name, schema)
		ApplyDefaultSchemaToSelect(s.Query, schema)
	case *DropView:
		setAliasSchema(&s.Name, schema)
	case *CreateTrigger:
		setAliasSchema(&s.Table, schema)
	case *DropTrigger:
		setAliasSchema(&s.Table, schema)
	}
}

// ApplyDefaultSchemaToSelect qualifies the FROM/JOIN tables of a SELECT (and
// of its CTEs) that carry no explicit schema.
func ApplyDefaultSchemaToSelect(sel *Select, schema string) {
	applySelectSchema(sel, schema, nil)
}

func applySelectSchema(sel *Select, schema string, outer map[string]bool) {
	if sel == nil || schema == "" {
		return
	}
	ctes := make(map[string]bool, len(outer)+len(sel.With))
	for k := range outer {
		ctes[k] = true
	}
	for i := range sel.With {
		ctes[strings.ToLower(sel.With[i].Name.Name)] = true
		applySelectSchema(sel.With[i].Select, schema, ctes)
	}
	setTableSchema(&sel.Table, schema, ctes)
	if sel.Join != nil {
		setTableSchema(&sel.Join.Table, schema, ctes)
	}
	for _, j := range sel.Joins {
		if j != nil {
			setTableSchema(&j.Table, schema, ctes)
		}
	}
}

// setTableSchema fills Identifier.Schema for a SELECT table reference unless
// it is already qualified (Schema set or dotted Name) or names a CTE.
func setTableSchema(id *Identifier, schema string, ctes map[string]bool) {
	if id == nil || id.Name == "" || id.Schema != "" || strings.Contains(id.Name, ".") {
		return
	}
	if ctes[strings.ToLower(id.Name)] {
		return
	}
	id.Schema = schema
}

// setAliasSchema fills the schema of a DDL/DML object reference, which the
// parser stores in Identifier.Alias.
func setAliasSchema(id *Identifier, schema string) {
	if id != nil && id.Name != "" && id.Alias == "" {
		id.Alias = schema
	}
}
