package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dbf/internal/catalog"
	"dbf/internal/executor"
)

// newTestAPI wires an in-memory catalog + executor behind the GUI mux.
func newTestAPI(t *testing.T) *httptest.Server {
	t.Helper()
	cat := catalog.New()
	exe := executor.New(cat, nil)
	h := executeHandler{executor: exe, catalog: cat}
	srv := httptest.NewServer(withRecover(newGUIMux(h, cat, 0, nil)))
	t.Cleanup(srv.Close)
	return srv
}

func postQuery(t *testing.T, srv *httptest.Server, path string, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	raw, _ := json.Marshal(body)
	res, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer res.Body.Close()
	var out map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("POST %s: decode: %v", path, err)
	}
	return out
}

func getJSON(t *testing.T, srv *httptest.Server, path string, into interface{}) int {
	t.Helper()
	res, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(into); err != nil {
		t.Fatalf("GET %s: decode: %v", path, err)
	}
	return res.StatusCode
}

func mustOK(t *testing.T, srv *httptest.Server, sql, schema string) map[string]interface{} {
	t.Helper()
	body := map[string]interface{}{"sql": sql}
	if schema != "" {
		body["schema"] = schema
	}
	out := postQuery(t, srv, "/api/query", body)
	if e, _ := out["error"].(string); e != "" {
		t.Fatalf("%q (schema %q): unexpected error %s", sql, schema, e)
	}
	return out
}

func TestAPISchemasListAndActiveSchema(t *testing.T) {
	srv := newTestAPI(t)

	var schemas []apiSchemaInfo
	getJSON(t, srv, "/api/schemas", &schemas)
	if len(schemas) != 1 || schemas[0].Name != "public" || !schemas[0].IsDefault {
		t.Fatalf("fresh catalog should list only public, got %+v", schemas)
	}

	out := mustOK(t, srv, "CREATE SCHEMA tienda", "")
	if out["tag"] != "CREATE SCHEMA" {
		t.Errorf("expected CREATE SCHEMA tag, got %v", out["tag"])
	}

	// Unqualified DDL/DML inside the active schema.
	mustOK(t, srv, "CREATE TABLE productos (id INT IDENTITY PRIMARY KEY, nombre TEXT, precio INT)", "tienda")
	mustOK(t, srv, "INSERT INTO productos (nombre, precio) VALUES ('lapiz', 5)", "tienda")
	mustOK(t, srv, "INSERT INTO productos (nombre, precio) VALUES ('tinta', 20)", "tienda")
	mustOK(t, srv, "CREATE VIEW caros AS SELECT nombre, precio FROM productos WHERE precio > 10", "tienda")

	getJSON(t, srv, "/api/schemas", &schemas)
	if len(schemas) != 2 || schemas[1].Name != "tienda" || schemas[1].Tables != 1 || schemas[1].Views != 1 {
		t.Fatalf("expected tienda with 1 table + 1 view, got %+v", schemas)
	}

	// Metadata is scoped by ?schema=.
	var tables []apiTableInfo
	getJSON(t, srv, "/api/schema?schema=tienda", &tables)
	names := map[string]string{}
	for _, tb := range tables {
		names[tb.Name] = tb.Kind
		if tb.Schema != "tienda" {
			t.Errorf("table %s reported schema %q", tb.Name, tb.Schema)
		}
	}
	if names["productos"] != "BASE TABLE" || names["caros"] != "VIEW" {
		t.Fatalf("expected productos + caros in tienda, got %v", names)
	}
	getJSON(t, srv, "/api/schema", &tables)
	if len(tables) != 0 {
		t.Fatalf("public should be empty, got %+v", tables)
	}

	// Queries resolve inside the active schema; the view was stored with it.
	out = mustOK(t, srv, "SELECT nombre FROM caros", "tienda")
	if rows := out["rows"].([]interface{}); len(rows) != 1 {
		t.Fatalf("expected 1 row from caros, got %v", rows)
	}
	out = postQuery(t, srv, "/api/query", map[string]interface{}{"sql": "SELECT * FROM productos"})
	if e, _ := out["error"].(string); !strings.Contains(e, "not found") {
		t.Fatalf("productos must not resolve in public, got %v", out)
	}
	out = mustOK(t, srv, "SELECT p.nombre FROM tienda.productos AS p WHERE p.precio > 10", "")
	if rows := out["rows"].([]interface{}); len(rows) != 1 {
		t.Fatalf("qualified query from public failed: %v", out)
	}

	// Script path honours the schema too.
	script := postQuery(t, srv, "/api/script", map[string]interface{}{
		"sql": "INSERT INTO productos (nombre, precio) VALUES ('goma', 3); SELECT COUNT(*) FROM productos;", "schema": "tienda",
	})
	if e, _ := script["error"].(string); e != "" {
		t.Fatalf("script error: %s", e)
	}
	results := script["results"].([]interface{})
	last := results[len(results)-1].(map[string]interface{})
	if rows := last["rows"].([]interface{}); len(rows) != 1 || rows[0].([]interface{})[0] != float64(3) {
		t.Fatalf("expected COUNT(*) = 3 in tienda, got %v", rows)
	}

	// Table data explorer is scoped as well.
	var td apiTableDataResponse
	if code := getJSON(t, srv, "/api/table-data?table=productos&schema=tienda", &td); code != 200 || td.Total != 3 {
		t.Fatalf("table-data in tienda: code=%d resp=%+v", code, td)
	}
	if code := getJSON(t, srv, "/api/table-data?table=productos", &td); code != http.StatusNotFound {
		t.Fatalf("table-data in public should be 404, got %d", code)
	}
	var diag diagramDTO
	if code := getJSON(t, srv, "/api/diagram?schema=tienda", &diag); code != 200 || len(diag.Tables) != 1 {
		t.Fatalf("diagram in tienda: code=%d tables=%d", code, len(diag.Tables))
	}
	var errResp map[string]string
	if code := getJSON(t, srv, "/api/diagram?schema=nope", &errResp); code != http.StatusNotFound {
		t.Fatalf("unknown schema should be 404, got %d", code)
	}

	// Protected schemas and non-empty drops.
	out = postQuery(t, srv, "/api/query", map[string]interface{}{"sql": "DROP SCHEMA public"})
	if e, _ := out["error"].(string); !strings.Contains(e, "system schema") {
		t.Fatalf("DROP SCHEMA public must be rejected, got %v", out)
	}
	out = postQuery(t, srv, "/api/query", map[string]interface{}{"sql": "DROP SCHEMA tienda"})
	if e, _ := out["error"].(string); !strings.Contains(e, "not empty") {
		t.Fatalf("DROP SCHEMA tienda without CASCADE must be rejected, got %v", out)
	}
	out = mustOK(t, srv, "DROP SCHEMA tienda CASCADE", "")
	if out["tag"] != "DROP SCHEMA" {
		t.Errorf("expected DROP SCHEMA tag, got %v", out["tag"])
	}
	getJSON(t, srv, "/api/schemas", &schemas)
	if len(schemas) != 1 {
		t.Fatalf("tienda should be gone, got %+v", schemas)
	}
}

func TestAPIDropDatabaseRemovesSchema(t *testing.T) {
	srv := newTestAPI(t)
	mustOK(t, srv, "CREATE DATABASE analytics", "")
	mustOK(t, srv, "CREATE TABLE hechos (id INT)", "analytics")
	var schemas []apiSchemaInfo
	getJSON(t, srv, "/api/schemas", &schemas)
	if len(schemas) != 2 || schemas[1].Name != "analytics" {
		t.Fatalf("CREATE DATABASE should create the schema, got %+v", schemas)
	}
	mustOK(t, srv, "DROP DATABASE analytics", "")
	getJSON(t, srv, "/api/schemas", &schemas)
	if len(schemas) != 1 {
		t.Fatalf("DROP DATABASE should remove the schema, got %+v", schemas)
	}
	out := postQuery(t, srv, "/api/query", map[string]interface{}{"sql": "DROP DATABASE postgres"})
	if e, _ := out["error"].(string); !strings.Contains(e, "default database") {
		t.Fatalf("DROP DATABASE postgres must be rejected, got %v", out)
	}
}
