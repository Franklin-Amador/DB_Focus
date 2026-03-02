// server/bypass.go
package server

import (
	"bufio"
	"log"
	"strings"
)

var bypassPatterns = []struct {
	pattern string
	result  BypassResult
}{
	{
		pattern: "pg_is_in_recovery",
		result: BypassResult{
			Columns: []string{"inrecovery", "isreplaypaused"},
			Rows:    [][]interface{}{{false, false}},
			Tag:     "SELECT 1",
		},
	},
	{
		pattern: "extname='bdr'",
		result: BypassResult{
			Columns: []string{"type"},
			Rows:    [][]interface{}{{nil}},
			Tag:     "SELECT 1",
		},
	},
	{
		pattern: "pg_replication_slots",
		result: BypassResult{
			Columns: []string{"count"},
			Rows:    [][]interface{}{{0}},
			Tag:     "SELECT 1",
		},
	},
	{
		pattern: "pg_catalog.pg_extension",
		result: BypassResult{
			Columns: []string{"extname"},
			Rows:    [][]interface{}{},
			Tag:     "SELECT 0",
		},
	},
	{
		pattern: "pg_is_wal_replay_paused",
		result: BypassResult{
			Columns: []string{"result"},
			Rows:    [][]interface{}{{false}},
			Tag:     "SELECT 1",
		},
	},
	{
		pattern: "pg_encoding_to_char",
		result: BypassResult{
			Columns: []string{"did", "datname", "datallowconn", "serverencoding", "cancreate", "datistemplate"},
			Rows: [][]interface{}{
				{1, "focusdb", true, "UTF8", true, false},
			},
			Tag: "SELECT 1",
		},
	},
	{
		pattern: "pg_show_all_settings",
		result: BypassResult{
			Columns: []string{"set_config"},
			Rows:    [][]interface{}{{"hex"}},
			Tag:     "SELECT 1",
		},
	},
	{
		pattern: "client_min_messages",
		result: BypassResult{
			Columns: []string{},
			Rows:    [][]interface{}{},
			Tag:     "SET",
		},
	},
	{
		pattern: "pg_stat_gssapi",
		result: BypassResult{
			Columns: []string{"gss_authenticated", "encrypted"},
			Rows:    [][]interface{}{{false, false}},
			Tag:     "SELECT 1",
		},
	},
	{
		pattern: "can_signal_backend",
		result: BypassResult{
			Columns: []string{"id", "name", "is_superuser", "can_create_role", "can_create_db", "can_signal_backend"},
			Rows: [][]interface{}{
				{int32(10), "postgres", true, true, true, false},
			},
			Tag: "SELECT 1",
		},
	},
	{
		pattern: "WHERE 1<>1",
		result: BypassResult{
			Columns: []string{},
			Rows:    [][]interface{}{},
			Tag:     "SELECT 0",
		},
	},
	{
		pattern: "pg_catalog.pg_type",
		result: BypassResult{
			Columns: []string{"oid", "typname", "typtype", "typlen"},
			Rows:    [][]interface{}{},
			Tag:     "SELECT 0",
		},
	},
	{
		pattern: "pg_catalog.pg_enum",
		result: BypassResult{
			Columns: []string{"oid", "enumtypid", "enumlabel"},
			Rows:    [][]interface{}{},
			Tag:     "SELECT 0",
		},
	},
	{
		pattern: "format_type(nullif",
		result: BypassResult{
			Columns: []string{"oid", "typname", "typtype", "typlen", "relkind", "base_type_name", "description"},
			Rows:    [][]interface{}{},
			Tag:     "SELECT 1",
		},
	},
}

func checkBypass(query string) *BypassResult {
	upper := strings.ToUpper(query)
	for _, bp := range bypassPatterns {
		if strings.Contains(upper, strings.ToUpper(bp.pattern)) {
			r := bp.result
			return &r
		}
	}
	return nil
}

func handleBypass(rw *bufio.ReadWriter, result *BypassResult) {
	log.Printf("[bypass] columns: %v rows: %v", result.Columns, result.Rows)
	if len(result.Columns) > 0 {
		var sample []interface{}
		if len(result.Rows) > 0 {
			sample = result.Rows[0]
		}
		writeRowDescriptionForResult(rw, result.Columns, sample)
		for _, row := range result.Rows {
			writeDataRow(rw, row)
		}
	} else {
		writeEmptyRowDescription(rw)
	}
	writeCommandComplete(rw, result.Tag)
	writeReady(rw)
}
