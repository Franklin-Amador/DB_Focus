package ast

import "testing"

func TestApplyDefaultSchemaSelect(t *testing.T) {
	join := &JoinClause{Type: "INNER", Table: Identifier{Name: "ventas", Schema: "public"}}
	join2 := &JoinClause{Type: "LEFT", Table: Identifier{Name: "cats", Alias: "c"}}
	sel := &Select{
		With:  []CTE{{Name: Identifier{Name: "top"}, Select: &Select{Star: true, Table: Identifier{Name: "productos"}}}},
		Table: Identifier{Name: "top"},
		Join:  join,
		Joins: []*JoinClause{join, join2},
	}
	ApplyDefaultSchema(sel, "tienda")

	if sel.Table.Schema != "" {
		t.Errorf("CTE reference must stay unqualified, got %q", sel.Table.Schema)
	}
	if sel.With[0].Select.Table.Schema != "tienda" {
		t.Errorf("CTE body table should resolve in tienda, got %q", sel.With[0].Select.Table.Schema)
	}
	if join.Table.Schema != "public" {
		t.Errorf("explicit schema must be preserved, got %q", join.Table.Schema)
	}
	if join2.Table.Schema != "tienda" || join2.Table.Alias != "c" {
		t.Errorf("join table should get tienda and keep its alias, got %+v", join2.Table)
	}

	// Idempotent.
	ApplyDefaultSchema(sel, "otro")
	if join2.Table.Schema != "tienda" {
		t.Errorf("second application must not overwrite, got %q", join2.Table.Schema)
	}
}

func TestApplyDefaultSchemaDDL(t *testing.T) {
	ct := &CreateTable{Table: Identifier{Name: "t"}}
	ApplyDefaultSchema(ct, "tienda")
	if ct.Table.Alias != "tienda" {
		t.Errorf("DDL keeps schema in Alias, got %+v", ct.Table)
	}
	ins := &Insert{Table: Identifier{Name: "t", Alias: "public"}}
	ApplyDefaultSchema(ins, "tienda")
	if ins.Table.Alias != "public" {
		t.Errorf("explicit DML schema must be preserved, got %+v", ins.Table)
	}
	cv := &CreateView{Name: Identifier{Name: "v"}, Query: &Select{Table: Identifier{Name: "t"}}}
	ApplyDefaultSchema(cv, "tienda")
	if cv.Name.Alias != "tienda" || cv.Query.Table.Schema != "tienda" {
		t.Errorf("view name and body should be qualified, got %+v / %+v", cv.Name, cv.Query.Table)
	}
}
