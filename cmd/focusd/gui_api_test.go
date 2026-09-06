package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dbf/internal/catalog"
)

// newTestAPI wires an in-memory cluster + handler behind the GUI mux.
func newTestAPI(t *testing.T) *httptest.Server {
	t.Helper()
	cl := catalog.NewCluster()
	h := newExecuteHandler(nil, cl, nil)
	srv := httptest.NewServer(withRecover(newGUIMux(h, cl, 0, nil)))
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

// mustOKIn runs a statement inside a database (and schema) via /api/query.
func mustOKIn(t *testing.T, srv *httptest.Server, sql, database, schema string) map[string]interface{} {
	t.Helper()
	out := postQuery(t, srv, "/api/query", map[string]interface{}{"sql": sql, "database": database, "schema": schema})
	if e, _ := out["error"].(string); e != "" {
		t.Fatalf("%q (db %q, schema %q): unexpected error %s", sql, database, schema, e)
	}
	return out
}

func TestAPIDatabasesAreIsolatedContainers(t *testing.T) {
	srv := newTestAPI(t)

	var dbs []apiDatabaseInfo
	getJSON(t, srv, "/api/databases", &dbs)
	if len(dbs) != 1 || dbs[0].Name != "postgres" || !dbs[0].IsDefault {
		t.Fatalf("fresh cluster should list only postgres, got %+v", dbs)
	}

	out := mustOK(t, srv, "CREATE DATABASE ventas", "")
	if out["tag"] != "CREATE DATABASE" {
		t.Errorf("expected CREATE DATABASE tag, got %v", out["tag"])
	}
	// Objects live inside the database: schema, table, view, procedure.
	mustOKIn(t, srv, "CREATE SCHEMA reportes", "ventas", "")
	mustOKIn(t, srv, "CREATE TABLE pedidos (id INT IDENTITY PRIMARY KEY, total INT)", "ventas", "reportes")
	mustOKIn(t, srv, "INSERT INTO pedidos (total) VALUES (10)", "ventas", "reportes")
	mustOKIn(t, srv, "CREATE TABLE clientes (id INT)", "ventas", "")

	getJSON(t, srv, "/api/databases", &dbs)
	if len(dbs) != 2 || dbs[1].Name != "ventas" || dbs[1].Schemas != 2 || dbs[1].Tables != 2 {
		t.Fatalf("expected ventas with 2 schemas + 2 tables, got %+v", dbs)
	}
	var schemas []apiSchemaInfo
	getJSON(t, srv, "/api/schemas?database=ventas", &schemas)
	if len(schemas) != 2 || schemas[1].Name != "reportes" || schemas[1].Tables != 1 {
		t.Fatalf("expected [public reportes] in ventas, got %+v", schemas)
	}
	getJSON(t, srv, "/api/schemas", &schemas)
	if len(schemas) != 1 {
		t.Fatalf("schemas of ventas must not leak into postgres, got %+v", schemas)
	}

	// Metadata endpoints are scoped by database + schema.
	var tables []apiTableInfo
	getJSON(t, srv, "/api/schema?database=ventas&schema=reportes", &tables)
	if len(tables) != 1 || tables[0].Name != "pedidos" {
		t.Fatalf("expected pedidos in ventas.reportes, got %+v", tables)
	}
	var errResp map[string]string
	if code := getJSON(t, srv, "/api/schema?schema=reportes", &errResp); code != http.StatusNotFound {
		t.Fatalf("reportes must not exist in postgres, got %d", code)
	}
	if code := getJSON(t, srv, "/api/schema?database=nope", &errResp); code != http.StatusNotFound {
		t.Fatalf("unknown database should be 404, got %d", code)
	}
	var td apiTableDataResponse
	if code := getJSON(t, srv, "/api/table-data?database=ventas&schema=reportes&table=pedidos", &td); code != 200 || td.Total != 1 {
		t.Fatalf("table-data in ventas.reportes: code=%d resp=%+v", code, td)
	}

	// Isolation: the same names do not resolve from another database.
	out = postQuery(t, srv, "/api/query", map[string]interface{}{"sql": "SELECT * FROM clientes"})
	if e, _ := out["error"].(string); !strings.Contains(e, "not found") {
		t.Fatalf("clientes must not resolve in postgres, got %v", out)
	}
	out = mustOKIn(t, srv, "SELECT total FROM reportes.pedidos", "ventas", "")
	if rows := out["rows"].([]interface{}); len(rows) != 1 {
		t.Fatalf("qualified query inside ventas failed: %v", out)
	}
	// information_schema reports the request's database as table_catalog.
	out = mustOKIn(t, srv, "SELECT * FROM information_schema.tables", "ventas", "")
	rows := out["rows"].([]interface{})
	if len(rows) != 1 || rows[0].([]interface{})[0] != "ventas" || rows[0].([]interface{})[2] != "clientes" {
		t.Fatalf("information_schema.tables in ventas should list clientes with catalog ventas, got %v", rows)
	}

	// Script path honours the database too.
	script := postQuery(t, srv, "/api/script", map[string]interface{}{
		"sql": "INSERT INTO clientes (id) VALUES (1); SELECT COUNT(*) FROM clientes;", "database": "ventas",
	})
	if e, _ := script["error"].(string); e != "" {
		t.Fatalf("script error: %s", e)
	}

	// Protection and drop.
	out = postQuery(t, srv, "/api/query", map[string]interface{}{"sql": "DROP DATABASE postgres"})
	if e, _ := out["error"].(string); !strings.Contains(e, "default database") {
		t.Fatalf("DROP DATABASE postgres must be rejected, got %v", out)
	}
	out = postQuery(t, srv, "/api/query", map[string]interface{}{"sql": "DROP DATABASE ventas", "database": "ventas"})
	if e, _ := out["error"].(string); !strings.Contains(e, "currently open") {
		t.Fatalf("dropping the open database must be rejected, got %v", out)
	}
	out = mustOK(t, srv, "DROP DATABASE ventas", "")
	if out["tag"] != "DROP DATABASE" {
		t.Errorf("expected DROP DATABASE tag, got %v", out["tag"])
	}
	getJSON(t, srv, "/api/databases", &dbs)
	if len(dbs) != 1 {
		t.Fatalf("ventas should be gone, got %+v", dbs)
	}
	if code := getJSON(t, srv, "/api/schemas?database=ventas", &errResp); code != http.StatusNotFound {
		t.Fatalf("dropped database should be 404, got %d", code)
	}
}
