package catalog

import (
	"testing"

	"dbf/internal/ast"
)

func TestListSchemasHidesSystemAndCounts(t *testing.T) {
	c := New()
	if err := c.CreateSchema("ventas"); err != nil {
		t.Fatal(err)
	}
	cols := []Column{{Name: "id", Type: "INT"}}
	if err := c.CreateTable("pedidos", cols, nil, "ventas"); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateView("v", cols, &ast.Select{Star: true, Table: ast.Identifier{Name: "pedidos"}}, "ventas"); err != nil {
		t.Fatal(err)
	}

	list := c.ListSchemas()
	if len(list) != 2 || list[0].Name != "public" || list[1].Name != "ventas" {
		t.Fatalf("expected [public ventas], got %+v", list)
	}
	if list[1].Tables != 1 || list[1].Views != 1 {
		t.Errorf("expected 1 table + 1 view in ventas, got %+v", list[1])
	}
	for _, s := range list {
		if IsSystemSchema(s.Name) {
			t.Errorf("system schema %s leaked into the listing", s.Name)
		}
	}
}

func TestDropSchemaProtectsSystemSchemas(t *testing.T) {
	c := New()
	for _, name := range []string{"public", "pg_catalog", "information_schema", "focus"} {
		if err := c.DropSchema(name); err == nil {
			t.Errorf("DropSchema(%s) must fail", name)
		}
	}
	if !c.SchemaExists("public") || !c.SchemaExists("pg_catalog") {
		t.Fatal("protected schemas were removed")
	}
}

func TestPgNamespaceListsUserSchemas(t *testing.T) {
	c := New()
	if err := c.CreateSchema("tienda"); err != nil {
		t.Fatal(err)
	}
	res, ok := c.HandleSystemQueryForDatabase("SELECT * FROM pg_catalog.pg_namespace", "postgres")
	if !ok {
		t.Fatal("pg_namespace query was not handled")
	}
	found := false
	for _, row := range res.Rows {
		if row[2] == "tienda" {
			found = true
		}
		if row[2] == "focus" {
			t.Errorf("internal schema focus must not be listed")
		}
	}
	if !found {
		t.Fatalf("tienda missing from pg_namespace: %v", res.Rows)
	}
}

func TestLoadViewAppliesOwnSchemaToQuery(t *testing.T) {
	c := New()
	if err := c.CreateSchema("tienda"); err != nil {
		t.Fatal(err)
	}
	query := &ast.Select{Star: true, Table: ast.Identifier{Name: "productos"}}
	if err := c.LoadView("caros", []Column{{Name: "id", Type: "INT"}}, query, "tienda"); err != nil {
		t.Fatal(err)
	}
	v, err := c.GetView("caros", "tienda")
	if err != nil {
		t.Fatal(err)
	}
	if v.Query.Table.Schema != "tienda" {
		t.Fatalf("view query table should resolve in tienda, got schema %q", v.Query.Table.Schema)
	}
}
