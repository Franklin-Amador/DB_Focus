package server

import "testing"

func TestSearchPathTarget(t *testing.T) {
	cases := map[string]string{
		"SET search_path TO tienda":                  "tienda",
		"set search_path = tienda;":                  "tienda",
		"SET search_path TO tienda, public":          "tienda",
		"SET search_path TO \"Ventas 2026\", public": "Ventas 2026",
		"SET SEARCH_PATH TO '$user', public":         "public",
		"SET search_path TO public":                  "public",
		"SET client_min_messages TO warning":         "",
		"SELECT * FROM search_path":                  "",
		"SET search_path":                            "",
	}
	for stmt, want := range cases {
		if got := searchPathTarget(stmt); got != want {
			t.Errorf("searchPathTarget(%q) = %q, want %q", stmt, got, want)
		}
	}
}
