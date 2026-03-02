// package catalog

// import (
// 	"dbf/internal/constants"
// 	"fmt"
// 	"strings"
// )

// // SystemResult represents a result from a system catalog query
// type SystemResult struct {
// 	Columns []string
// 	Rows    [][]interface{}
// 	Tag     string
// }

// // HandleSystemQuery intercepts system catalog queries from DBeaver/pgAdmin
// // and returns real data from the catalog. Returns (result, true) if handled,
// // (nil, false) if the query should be passed to the normal parser.
// func (c *Catalog) HandleSystemQuery(query string) (*SystemResult, bool) {
// 	upper := strings.ToUpper(strings.TrimSpace(query))

// 	switch {
// 	// ── information_schema ──────────────────────────────────────────
// 	case strings.Contains(upper, "INFORMATION_SCHEMA.TABLES"):
// 		return c.getISchemaTables(), true

// 	case strings.Contains(upper, "INFORMATION_SCHEMA.COLUMNS"):
// 		return c.getISchemaColumns(), true

// 	case strings.Contains(upper, "INFORMATION_SCHEMA."):
// 		// cualquier otra query de information_schema → vacío seguro
// 		return emptyResult("SELECT 0"), true

// 	// ── pg_type (debe ir antes que pg_catalog genérico) ─────────────
// 	case strings.Contains(upper, "FORMAT_TYPE(NULLIF"):
// 		return c.getPgTypeFull(), true

// 	case strings.Contains(upper, "PG_CATALOG.PG_TYPE") ||
// 		strings.Contains(upper, "PG_TYPE T"):
// 		return c.getPgTypeBasic(), true

// 	// ── pg_class ────────────────────────────────────────────────────
// 	case strings.Contains(upper, "PG_CATALOG.PG_CLASS") ||
// 		strings.Contains(upper, "FROM PG_CLASS"):
// 		return c.getPgClass(), true

// 	// ── pg_attribute ────────────────────────────────────────────────
// 	case strings.Contains(upper, "PG_CATALOG.PG_ATTRIBUTE") ||
// 		strings.Contains(upper, "FROM PG_ATTRIBUTE"):
// 		return c.getPgAttribute(), true

// 	// ── pg_namespace ─────────────────────────────────────────────────
// 	case strings.Contains(upper, "PG_CATALOG.PG_NAMESPACE") ||
// 		strings.Contains(upper, "FROM PG_NAMESPACE"):
// 		return c.getPgNamespace(), true

// 	// ── pg_roles / pg_user ──────────────────────────────────────────
// 	case strings.Contains(upper, "PG_CATALOG.PG_ROLES") ||
// 		strings.Contains(upper, "PG_CATALOG.PG_USER") ||
// 		strings.Contains(upper, "CAN_SIGNAL_BACKEND"):
// 		return c.getPgRoles(), true

// 	// ── pg_database ──────────────────────────────────────────────────
// 	case strings.Contains(upper, "PG_CATALOG.PG_DATABASE") ||
// 		strings.Contains(upper, "PG_ENCODING_TO_CHAR"):
// 		return c.getPgDatabase(), true

// 	// ── pg_constraint ────────────────────────────────────────────────
// 	case strings.Contains(upper, "PG_CATALOG.PG_CONSTRAINT") ||
// 		strings.Contains(upper, "FROM PG_CONSTRAINT"):
// 		return c.getPgConstraint(), true

// 	// ── pg_index ─────────────────────────────────────────────────────
// 	case strings.Contains(upper, "PG_CATALOG.PG_INDEX") ||
// 		strings.Contains(upper, "FROM PG_INDEX"):
// 		return emptyResultWithCols(
// 			[]string{"indexrelid", "indrelid", "indisprimary", "indisunique"},
// 			"SELECT 0",
// 		), true

// 	// ── pg_enum ──────────────────────────────────────────────────────
// 	case strings.Contains(upper, "PG_CATALOG.PG_ENUM") ||
// 		strings.Contains(upper, "FROM PG_ENUM"):
// 		return emptyResultWithCols(
// 			[]string{"oid", "enumtypid", "enumsortorder", "enumlabel"},
// 			"SELECT 0",
// 		), true

// 	// ── pg_proc / funciones ──────────────────────────────────────────
// 	case strings.Contains(upper, "PG_CATALOG.PG_PROC") ||
// 		strings.Contains(upper, "FROM PG_PROC"):
// 		return emptyResultWithCols(
// 			[]string{"oid", "proname", "pronamespace", "prorettype"},
// 			"SELECT 0",
// 		), true

// 	// ── pg_stat_gssapi ───────────────────────────────────────────────
// 	case strings.Contains(upper, "PG_STAT_GSSAPI"):
// 		return &SystemResult{
// 			Columns: []string{"gss_authenticated", "encrypted"},
// 			Rows:    [][]interface{}{{false, false}},
// 			Tag:     "SELECT 1",
// 		}, true

// 	// ── recovery / replicación ───────────────────────────────────────
// 	case strings.Contains(upper, "PG_IS_IN_RECOVERY") ||
// 		strings.Contains(upper, "PG_IS_WAL_REPLAY_PAUSED"):
// 		return &SystemResult{
// 			Columns: []string{"inrecovery", "isreplaypaused"},
// 			Rows:    [][]interface{}{{false, false}},
// 			Tag:     "SELECT 1",
// 		}, true

// 	case strings.Contains(upper, "PG_REPLICATION_SLOTS"):
// 		return &SystemResult{
// 			Columns: []string{"count"},
// 			Rows:    [][]interface{}{{int32(0)}},
// 			Tag:     "SELECT 1",
// 		}, true

// 	// ── bdr / replication type ───────────────────────────────────────
// 	case strings.Contains(upper, "EXTNAME='BDR'"):
// 		return &SystemResult{
// 			Columns: []string{"type"},
// 			Rows:    [][]interface{}{{""}},
// 			Tag:     "SELECT 1",
// 		}, true

// 	// ── pg_show_all_settings / set_config ────────────────────────────
// 	case strings.Contains(upper, "PG_SHOW_ALL_SETTINGS") ||
// 		strings.Contains(upper, "SET_CONFIG"):
// 		return &SystemResult{
// 			Columns: []string{"set_config"},
// 			Rows:    [][]interface{}{{"hex"}},
// 			Tag:     "SELECT 1",
// 		}, true

// 	// ── pg_description ───────────────────────────────────────────────
// 	case strings.Contains(upper, "PG_CATALOG.PG_DESCRIPTION") ||
// 		strings.Contains(upper, "FROM PG_DESCRIPTION"):
// 		return emptyResultWithCols(
// 			[]string{"objoid", "classoid", "objsubid", "description"},
// 			"SELECT 0",
// 		), true

// 	// ── pg_settings ──────────────────────────────────────────────────
// 	case strings.Contains(upper, "PG_CATALOG.PG_SETTINGS") ||
// 		strings.Contains(upper, "FROM PG_SETTINGS"):
// 		return c.getPgSettings(), true

// 	// ── WHERE 1<>1 (introspección de tipos de DBeaver) ───────────────
// 	case strings.Contains(upper, "WHERE 1<>1") ||
// 		strings.Contains(upper, "WHERE 1 <> 1"):
// 		return emptyResult("SELECT 0"), true

// 	case strings.Contains(upper, "PG_GET_KEYWORDS"):
// 		return &SystemResult{
// 			Columns: []string{"string_agg"},
// 			Rows:    [][]interface{}{{""}},
// 			Tag:     "SELECT 1",
// 		}, true

// 	case strings.Contains(upper, "FOCUS.USERS"):
// 		// delegar al handler de usuarios — retorna false para que lo maneje el executor
// 		return nil, false

// 	case strings.Contains(upper, "$1") && strings.Contains(upper, "PG_CATALOG"):
// 		// prepared query con parámetro bind contra pg_catalog → vacío seguro
// 		return emptyResult("SELECT 0"), true

// 	case strings.Contains(upper, "CURRENT_SCHEMA()"):
// 		return &SystemResult{
// 			Columns: []string{"current_schema", "session_user"},
// 			Rows:    [][]interface{}{{"public", "postgres"}},
// 			Tag:     "SELECT 1",
// 		}, true

// 	case strings.Contains(upper, "FORMAT_TYPE(NULLIF"):
// 		return c.getPgTypeFull(), true
// 	}

// 	return nil, false
// }

// // ── Implementaciones reales ──────────────────────────────────────────────────

// func (c *Catalog) getISchemaTables() *SystemResult {
// 	rows := c.GetInformationSchemaTables()
// 	return &SystemResult{
// 		Columns: []string{"table_catalog", "table_schema", "table_name", "table_type"},
// 		Rows:    rows,
// 		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
// 	}
// }

// func (c *Catalog) getISchemaColumns() *SystemResult {
// 	rows := c.GetInformationSchemaColumns()
// 	return &SystemResult{
// 		Columns: []string{"table_catalog", "table_schema", "table_name", "column_name", "ordinal_position", "data_type"},
// 		Rows:    rows,
// 		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
// 	}
// }

// func (c *Catalog) getPgClass() *SystemResult {
// 	tables := c.GetAllTables()
// 	var rows [][]interface{}
// 	oid := int32(1000)
// 	for name := range tables {
// 		if strings.HasPrefix(name, "pg_catalog.") {
// 			continue
// 		}
// 		rows = append(rows, []interface{}{
// 			oid,         // oid
// 			name,        // relname
// 			int32(2200), // relnamespace (public)
// 			"r",         // relkind (table)
// 			int32(10),   // relowner
// 			int32(0),    // reltype
// 			int32(0),    // reloftype
// 		})
// 		oid++
// 	}
// 	return &SystemResult{
// 		Columns: []string{"oid", "relname", "relnamespace", "relkind", "relowner", "reltype", "reloftype"},
// 		Rows:    rows,
// 		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
// 	}
// }

// func (c *Catalog) getPgAttribute() *SystemResult {
// 	tables := c.GetAllTables()
// 	var rows [][]interface{}
// 	tableOid := int32(1000)
// 	for tName, table := range tables {
// 		if strings.HasPrefix(tName, "pg_catalog.") {
// 			continue
// 		}
// 		for i, col := range table.Columns {
// 			rows = append(rows, []interface{}{
// 				tableOid,     // attrelid
// 				col.Name,     // attname
// 				int32(25),    // atttypid (text por defecto)
// 				int32(i + 1), // attnum
// 				col.NotNull,  // attnotnull
// 				int32(-1),    // atttypmod
// 				false,        // attisdropped
// 			})
// 		}
// 		tableOid++
// 	}
// 	return &SystemResult{
// 		Columns: []string{"attrelid", "attname", "atttypid", "attnum", "attnotnull", "atttypmod", "attisdropped"},
// 		Rows:    rows,
// 		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
// 	}
// }

// func (c *Catalog) getPgNamespace() *SystemResult {
// 	return &SystemResult{
// 		Columns: []string{"oid", "nspname", "nspowner"},
// 		Rows: [][]interface{}{
// 			{int32(11), "pg_catalog", int32(10)},
// 			{int32(2200), "public", int32(10)},
// 		},
// 		Tag: "SELECT 2",
// 	}
// }

// func (c *Catalog) getPgRoles() *SystemResult {
// 	rolesTable, err := c.GetTable(constants.CatalogRoles)
// 	if err != nil {
// 		return emptyResultWithCols(
// 			[]string{"id", "name", "is_superuser", "can_create_role", "can_create_db", "rolcanlogin", "can_signal_backend"},
// 			"SELECT 0",
// 		)
// 	}

// 	allRows := rolesTable.SelectAll()
// 	result := make([][]interface{}, 0, len(allRows))
// 	for _, r := range allRows {
// 		// pg_catalog.pg_roles columns: oid, rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb, rolcanlogin
// 		if len(r) < 7 {
// 			continue
// 		}
// 		result = append(result, []interface{}{
// 			r[0],  // oid
// 			r[1],  // rolname
// 			r[2],  // rolsuper
// 			r[4],  // rolcreaterole
// 			r[5],  // rolcreatedb
// 			r[6],  // rolcanlogin
// 			false, // can_signal_backend
// 		})
// 	}

// 	return &SystemResult{
// 		Columns: []string{"id", "name", "is_superuser", "can_create_role", "can_create_db", "rolcanlogin", "can_signal_backend"},
// 		Rows:    result,
// 		Tag:     fmt.Sprintf("SELECT %d", len(result)),
// 	}
// }

// func (c *Catalog) getPgDatabase() *SystemResult {
// 	return &SystemResult{
// 		Columns: []string{"did", "datname", "datallowconn", "serverencoding", "cancreate", "datistemplate"},
// 		Rows: [][]interface{}{
// 			{int32(1), "focusdb", true, "UTF8", true, false},
// 		},
// 		Tag: "SELECT 1",
// 	}
// }

// func (c *Catalog) getPgConstraint() *SystemResult {
// 	tables := c.GetAllTables()
// 	var rows [][]interface{}
// 	oid := int32(3000)
// 	tableOid := int32(1000)
// 	for tName, table := range tables {
// 		if strings.HasPrefix(tName, "pg_catalog.") {
// 			continue
// 		}
// 		for _, constraint := range table.Constraints {
// 			contype := "c"
// 			switch constraint.Type {
// 			case "PRIMARY_KEY":
// 				contype = "p"
// 			case "FOREIGN_KEY":
// 				contype = "f"
// 			case "UNIQUE":
// 				contype = "u"
// 			}
// 			rows = append(rows, []interface{}{
// 				oid,
// 				constraint.ColumnName + "_" + contype,
// 				tableOid,
// 				contype,
// 				constraint.ReferencedTable,
// 			})
// 			oid++
// 		}
// 		tableOid++
// 	}
// 	return &SystemResult{
// 		Columns: []string{"oid", "conname", "conrelid", "contype", "confrelid"},
// 		Rows:    rows,
// 		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
// 	}
// }

// func (c *Catalog) getPgTypeBasic() *SystemResult {
// 	return &SystemResult{
// 		Columns: []string{"oid", "typname", "typtype", "typlen", "typbasetype", "typtypmod", "typelem", "typrelid", "typcategory", "typnamespace"},
// 		Rows: [][]interface{}{
// 			{int32(16), "bool", "b", int32(1), int32(0), int32(-1), int32(0), int32(0), "B", int32(11)},
// 			{int32(23), "int4", "b", int32(4), int32(0), int32(-1), int32(0), int32(0), "N", int32(11)},
// 			{int32(25), "text", "b", int32(-1), int32(0), int32(-1), int32(0), int32(0), "S", int32(11)},
// 			{int32(701), "float8", "b", int32(8), int32(0), int32(-1), int32(0), int32(0), "N", int32(11)},
// 			{int32(1043), "varchar", "b", int32(-1), int32(0), int32(-1), int32(0), int32(0), "S", int32(11)},
// 			{int32(1082), "date", "b", int32(4), int32(0), int32(-1), int32(0), int32(0), "D", int32(11)},
// 			{int32(1114), "timestamp", "b", int32(8), int32(0), int32(-1), int32(0), int32(0), "D", int32(11)},
// 		},
// 		Tag: "SELECT 7",
// 	}
// }

// func (c *Catalog) getPgTypeFull() *SystemResult {
// 	// Mismos datos que basic pero con columnas extra que pide DBeaver
// 	basic := c.getPgTypeBasic()
// 	basic.Columns = append(basic.Columns, "relkind", "base_type_name", "description")
// 	for i := range basic.Rows {
// 		basic.Rows[i] = append(basic.Rows[i], nil, nil, nil)
// 	}
// 	return basic
// }

// func (c *Catalog) getPgSettings() *SystemResult {
// 	return &SystemResult{
// 		Columns: []string{"name", "setting"},
// 		Rows: [][]interface{}{
// 			{"server_version", "17.0"},
// 			{"server_encoding", "UTF8"},
// 			{"client_encoding", "UTF8"},
// 			{"bytea_output", "hex"},
// 			{"standard_conforming_strings", "on"},
// 			{"integer_datetimes", "on"},
// 		},
// 		Tag: "SELECT 6",
// 	}
// }

// // ── Helpers ──────────────────────────────────────────────────────────────────

// func emptyResult(tag string) *SystemResult {
// 	return &SystemResult{Columns: []string{}, Rows: [][]interface{}{}, Tag: tag}
// }

// func emptyResultWithCols(cols []string, tag string) *SystemResult {
// 	return &SystemResult{Columns: cols, Rows: [][]interface{}{}, Tag: tag}
// }

package catalog

import (
	"fmt"
	"strings"
)

// SystemResult represents a result from a system catalog query
type SystemResult struct {
	Columns []string
	Rows    [][]interface{}
	Tag     string
}

// HandleSystemQuery intercepts system catalog queries — generated from real PostgreSQL 17 responses
func (c *Catalog) HandleSystemQuery(query string) (*SystemResult, bool) {
	upper := strings.ToUpper(strings.TrimSpace(query))
	switch {
	case strings.Contains(upper, "CURRENT_SCHEMA()"):
		return c.getCurrentSchema(), true

	case strings.Contains(upper, "SHOW SEARCH_PATH"):
		return c.getSearchPath(), true

	case strings.Contains(upper, "SELECT VERSION()"):
		return c.getVersion(), true

	case strings.Contains(upper, "FORMAT_TYPE(NULLIF"):
		return c.getPgTypeFull(), true

	case strings.Contains(upper, "PG_GET_KEYWORDS"):
		return c.getPgKeywords(), true

	case strings.Contains(upper, "PG_CATALOG.PG_DATABASE"):
		return c.getPgDatabase(), true

	case strings.Contains(upper, "PG_CATALOG.PG_SETTINGS"):
		return c.getPgSettings(), true

	case strings.Contains(upper, "PG_CATALOG.PG_NAMESPACE"):
		return c.getPgNamespace(), true

	case strings.Contains(upper, "PG_CATALOG.PG_ENUM"):
		return c.getPgEnum(), true

	case strings.Contains(upper, "PG_CATALOG.PG_CLASS"):
		return c.getPgClass(), true

	case strings.Contains(upper, "PG_CATALOG.PG_ATTRIBUTE"):
		return c.getPgAttribute(), true

	case strings.Contains(upper, "PG_CATALOG.PG_CONSTRAINT"):
		return c.getPgConstraint(), true

	case strings.Contains(upper, "PG_IS_IN_RECOVERY") ||
		strings.Contains(upper, "PG_IS_WAL_REPLAY_PAUSED"):
		return &SystemResult{
			Columns: []string{"inrecovery", "isreplaypaused"},
			Rows:    [][]interface{}{{false, false}},
			Tag:     "SELECT 1",
		}, true

	case strings.Contains(upper, "PG_REPLICATION_SLOTS"):
		return &SystemResult{
			Columns: []string{"count"},
			Rows:    [][]interface{}{{int32(0)}},
			Tag:     "SELECT 1",
		}, true

	case strings.Contains(upper, "EXTNAME='BDR'"):
		return &SystemResult{
			Columns: []string{"type"},
			Rows:    [][]interface{}{{""}},
			Tag:     "SELECT 1",
		}, true

	case strings.Contains(upper, "PG_STAT_GSSAPI"):
		return &SystemResult{
			Columns: []string{"gss_authenticated", "encrypted"},
			Rows:    [][]interface{}{{false, false}},
			Tag:     "SELECT 1",
		}, true

	case strings.Contains(upper, "INFORMATION_SCHEMA.TABLES"):
		return c.getISchemaTables(), true

	case strings.Contains(upper, "INFORMATION_SCHEMA.COLUMNS"):
		return c.getISchemaColumns(), true

	case strings.Contains(upper, "INFORMATION_SCHEMA."):
		return emptyResult("SELECT 0"), true

	case strings.Contains(upper, "WHERE 1<>1"):
		return emptyResult("SELECT 0"), true

	case strings.Contains(upper, "PG_CATALOG."):
		return emptyResult("SELECT 0"), true
	}
	return nil, false
}

func (c *Catalog) getCurrentSchema() *SystemResult {
	return &SystemResult{
		Columns: []string{"current_schema", "session_user"},
		Rows: [][]interface{}{
			{"public", "postgres"},
		},
		Tag: "SELECT 1",
	}
}

func (c *Catalog) getPgNamespace() *SystemResult {
	return &SystemResult{
		Columns: []string{"oid", "oid_1", "nspname", "nspowner", "nspacl", "description"},
		Rows: [][]interface{}{
			{"14704", "14704", "information_schema", "10", "{postgres=UC/postgres,=U/postgres}", nil},
			{"11", "11", "pg_catalog", "10", "{postgres=UC/postgres,=U/postgres}", "system catalog schema"},
			{"99", "99", "pg_toast", "10", nil, "reserved schema for TOAST tables"},
			{"2200", "2200", "public", "6171", "{pg_database_owner=UC/pg_database_owner,=U/pg_database_owner}", "standard public schema"},
		},
		Tag: "SELECT 4",
	}
}

func (c *Catalog) getSearchPath() *SystemResult {
	return &SystemResult{
		Columns: []string{"search_path"},
		Rows: [][]interface{}{
			{"\"$user\", public"},
		},
		Tag: "SELECT 1",
	}
}

func (c *Catalog) getPgDatabase() *SystemResult {
	// Try to get real data from pg_catalog.pg_database
	table, err := c.GetTable("pg_catalog.pg_database")
	if err != nil {
		// Fallback to static data
		return &SystemResult{
			Columns: []string{"oid", "datname", "datdba", "encoding", "datcollate", "datctype", "datlocprovider", "daticulocale", "daticurules", "datacl", "datcollversion", "datallowconn", "datistemplate"},
			Rows: [][]interface{}{
				{1, "postgres", 10, 6, "C", "C", "c", "", "", "", "", true, false},
			},
			Tag: "SELECT 1",
		}
	}

	rows := table.SelectAll()
	return &SystemResult{
		Columns: []string{"oid", "datname", "datdba", "encoding", "datcollate", "datctype", "datlocprovider", "daticulocale", "daticurules", "datacl", "datcollversion", "datallowconn", "datistemplate"},
		Rows:    rows,
		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
	}
}

func (c *Catalog) getPgSettings() *SystemResult {
	return &SystemResult{
		Columns: []string{"name", "setting", "unit", "category", "short_desc", "extra_desc", "context", "vartype", "source", "min_val", "max_val", "enumvals", "boot_val", "reset_val", "sourcefile", "sourceline", "pending_restart"},
		Rows: [][]interface{}{
			{"standard_conforming_strings", "on", nil, "Compatibilidad de Versión y Plataforma / Versiones Anteriores de PostgreSQL", "Provoca que las cadenas '...' traten las barras inclinadas inversas (\\) en forma literal.", nil, "user", "bool", "default", nil, nil, nil, "on", "on", nil, nil, "f"},
		},
		Tag: "SELECT 1",
	}
}

func (c *Catalog) getPgKeywords() *SystemResult {
	return &SystemResult{
		Columns: []string{"string_agg"},
		Rows: [][]interface{}{
			{"abort,absent,access,aggregate,also,analyse,analyze,attach,backward,bit,cache,checkpoint,class,cluster,columns,comment,comments,compression,concurrently,conditional,configuration,conflict,connection,content,conversion,copy,cost,csv,current_catalog,current_schema,database,delimiter,delimiters,depends,detach,dictionary,disable,discard,do,document,empty,enable,encoding,encrypted,enum,error,event,exclusive,explain,expression,extension,family,finalize,force,format,forward,freeze,functions,generated,greatest,groups,handler,header,if,ilike,immutable,implicit,import,include,indent,index,indexes,inherit,inherits,inline,instead,isnull,json,json_array,json_arrayagg,json_exists,json_object,json_objectagg,json_query,json_scalar,json_serialize,json_table,json_value,keep,keys,label,leakproof,least,limit,listen,load,location,lock,locked,logged,mapping,materialized,merge_action,mode,move,nested,nfc,nfd,nfkc,nfkd,nothing,notify,notnull,nowait,off,offset,oids,omit,operator,owned,owner,parallel,parser,passing,password,plan,plans,policy,prepared,procedural,procedures,program,publication,quote,quotes,reassign,recheck,refresh,reindex,rename,replace,replica,reset,restrict,returning,routines,rule,scalar,schemas,sequences,server,setof,share,show,skip,snapshot,stable,standalone,statistics,stdin,stdout,storage,stored,strict,string,strip,subscription,support,sysid,tables,tablespace,target,temp,template,text,truncate,trusted,types,unconditional,unencrypted,unlisten,unlogged,until,vacuum,valid,validate,validator,variadic,verbose,version,views,volatile,whitespace,wrapper,xml,xmlattributes,xmlconcat,xmlelement,xmlexists,xmlforest,xmlnamespaces,xmlparse,xmlpi,xmlroot,xmlserialize,xmltable,yes"},
		},
		Tag: "SELECT 1",
	}
}

func (c *Catalog) getVersion() *SystemResult {
	return &SystemResult{
		Columns: []string{"version"},
		Rows: [][]interface{}{
			{"PostgreSQL 17.8 on x86_64-windows, compiled by msvc-19.44.35222, 64-bit"},
		},
		Tag: "SELECT 1",
	}
}

func (c *Catalog) getPgEnum() *SystemResult {
	return &SystemResult{
		Columns: []string{"oid", "enumtypid", "enumsortorder", "enumlabel"},
		Rows:    [][]interface{}{},
		Tag:     "SELECT 0",
	}
}

func (c *Catalog) getPgTypeFull() *SystemResult {
	return &SystemResult{
		Columns: []string{"oid", "oid_1", "typname", "typnamespace", "typowner", "typlen", "typbyval", "typtype", "typcategory", "typispreferred", "typisdefined", "typdelim", "typrelid", "typsubscript", "typelem", "typarray", "typinput", "typoutput", "typreceive", "typsend", "typmodin", "typmodout", "typanalyze", "typalign", "typstorage", "typnotnull", "typbasetype", "typtypmod", "typndims", "typcollation", "typdefaultbin", "typdefault", "typacl", "relkind", "base_type_name", "description"},
		Rows: [][]interface{}{
			{"16", "16", "bool", "11", "10", "1", "t", "b", "B", "t", "t", ",", "0", "-", "0", "1000", "boolin", "boolout", "boolrecv", "boolsend", "-", "-", "-", "c", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "boolean, format 't'/'f'"},
			{"17", "17", "bytea", "11", "10", "-1", "f", "b", "U", "f", "t", ",", "0", "-", "0", "1001", "byteain", "byteaout", "bytearecv", "byteasend", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "variable-length string, binary values escaped"},
			{"18", "18", "char", "11", "10", "1", "t", "b", "Z", "f", "t", ",", "0", "-", "0", "1002", "charin", "charout", "charrecv", "charsend", "-", "-", "-", "c", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "single character"},
			{"19", "19", "name", "11", "10", "64", "f", "b", "S", "f", "t", ",", "0", "raw_array_subscript_handler", "18", "1003", "namein", "nameout", "namerecv", "namesend", "-", "-", "-", "c", "p", "f", "0", "-1", "0", "950", nil, nil, nil, nil, nil, "63-byte type for storing system identifiers"},
			{"20", "20", "int8", "11", "10", "8", "t", "b", "N", "f", "t", ",", "0", "-", "0", "1016", "int8in", "int8out", "int8recv", "int8send", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "~18 digit integer, 8-byte storage"},
			{"21", "21", "int2", "11", "10", "2", "t", "b", "N", "f", "t", ",", "0", "-", "0", "1005", "int2in", "int2out", "int2recv", "int2send", "-", "-", "-", "s", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "-32 thousand to 32 thousand, 2-byte storage"},
			{"22", "22", "int2vector", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "21", "1006", "int2vectorin", "int2vectorout", "int2vectorrecv", "int2vectorsend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "array of int2, used in system tables"},
			{"23", "23", "int4", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "1007", "int4in", "int4out", "int4recv", "int4send", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "-2 billion to 2 billion integer, 4-byte storage"},
			{"24", "24", "regproc", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "1008", "regprocin", "regprocout", "regprocrecv", "regprocsend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "registered procedure"},
			{"25", "25", "text", "11", "10", "-1", "f", "b", "S", "t", "t", ",", "0", "-", "0", "1009", "textin", "textout", "textrecv", "textsend", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, "variable-length string, no limit specified"},
			{"26", "26", "oid", "11", "10", "4", "t", "b", "N", "t", "t", ",", "0", "-", "0", "1028", "oidin", "oidout", "oidrecv", "oidsend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "object identifier(oid), maximum 4 billion"},
			{"27", "27", "tid", "11", "10", "6", "f", "b", "U", "f", "t", ",", "0", "-", "0", "1010", "tidin", "tidout", "tidrecv", "tidsend", "-", "-", "-", "s", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "tuple physical location, format '(block,offset)'"},
			{"28", "28", "xid", "11", "10", "4", "t", "b", "U", "f", "t", ",", "0", "-", "0", "1011", "xidin", "xidout", "xidrecv", "xidsend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "transaction id"},
			{"29", "29", "cid", "11", "10", "4", "t", "b", "U", "f", "t", ",", "0", "-", "0", "1012", "cidin", "cidout", "cidrecv", "cidsend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "command identifier type, sequence in transaction id"},
			{"30", "30", "oidvector", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "26", "1013", "oidvectorin", "oidvectorout", "oidvectorrecv", "oidvectorsend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "array of oids, used in system tables"},
			{"114", "114", "json", "11", "10", "-1", "f", "b", "U", "f", "t", ",", "0", "-", "0", "199", "json_in", "json_out", "json_recv", "json_send", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "JSON stored as text"},
			{"142", "142", "xml", "11", "10", "-1", "f", "b", "U", "f", "t", ",", "0", "-", "0", "143", "xml_in", "xml_out", "xml_recv", "xml_send", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "XML content"},
			{"194", "194", "pg_node_tree", "11", "10", "-1", "f", "b", "Z", "f", "t", ",", "0", "-", "0", "0", "pg_node_tree_in", "pg_node_tree_out", "pg_node_tree_recv", "pg_node_tree_send", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, "string representing an internal node tree"},
			{"3361", "3361", "pg_ndistinct", "11", "10", "-1", "f", "b", "Z", "f", "t", ",", "0", "-", "0", "0", "pg_ndistinct_in", "pg_ndistinct_out", "pg_ndistinct_recv", "pg_ndistinct_send", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, "multivariate ndistinct coefficients"},
			{"3402", "3402", "pg_dependencies", "11", "10", "-1", "f", "b", "Z", "f", "t", ",", "0", "-", "0", "0", "pg_dependencies_in", "pg_dependencies_out", "pg_dependencies_recv", "pg_dependencies_send", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, "multivariate dependencies"},
			{"5017", "5017", "pg_mcv_list", "11", "10", "-1", "f", "b", "Z", "f", "t", ",", "0", "-", "0", "0", "pg_mcv_list_in", "pg_mcv_list_out", "pg_mcv_list_recv", "pg_mcv_list_send", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, "multivariate MCV list"},
			{"32", "32", "pg_ddl_command", "11", "10", "8", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "pg_ddl_command_in", "pg_ddl_command_out", "pg_ddl_command_recv", "pg_ddl_command_send", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "internal type for passing CollectedCommand"},
			{"5069", "5069", "xid8", "11", "10", "8", "t", "b", "U", "f", "t", ",", "0", "-", "0", "271", "xid8in", "xid8out", "xid8recv", "xid8send", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "full transaction id"},
			{"600", "600", "point", "11", "10", "16", "f", "b", "G", "f", "t", ",", "0", "raw_array_subscript_handler", "701", "1017", "point_in", "point_out", "point_recv", "point_send", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "geometric point, format '(x,y)'"},
			{"601", "601", "lseg", "11", "10", "32", "f", "b", "G", "f", "t", ",", "0", "raw_array_subscript_handler", "600", "1018", "lseg_in", "lseg_out", "lseg_recv", "lseg_send", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "geometric line segment, format '[point1,point2]'"},
			{"602", "602", "path", "11", "10", "-1", "f", "b", "G", "f", "t", ",", "0", "-", "0", "1019", "path_in", "path_out", "path_recv", "path_send", "-", "-", "-", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "geometric path, format '(point1,...)'"},
			{"603", "603", "box", "11", "10", "32", "f", "b", "G", "f", "t", ";", "0", "raw_array_subscript_handler", "600", "1020", "box_in", "box_out", "box_recv", "box_send", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "geometric box, format 'lower left point,upper right point'"},
			{"604", "604", "polygon", "11", "10", "-1", "f", "b", "G", "f", "t", ",", "0", "-", "0", "1027", "poly_in", "poly_out", "poly_recv", "poly_send", "-", "-", "-", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "geometric polygon, format '(point1,...)'"},
			{"628", "628", "line", "11", "10", "24", "f", "b", "G", "f", "t", ",", "0", "raw_array_subscript_handler", "701", "629", "line_in", "line_out", "line_recv", "line_send", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "geometric line, formats '{A,B,C}'/'[point1,point2]'"},
			{"700", "700", "float4", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "1021", "float4in", "float4out", "float4recv", "float4send", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "single-precision floating point number, 4-byte storage"},
			{"701", "701", "float8", "11", "10", "8", "t", "b", "N", "t", "t", ",", "0", "-", "0", "1022", "float8in", "float8out", "float8recv", "float8send", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "double-precision floating point number, 8-byte storage"},
			{"705", "705", "unknown", "11", "10", "-2", "f", "p", "X", "f", "t", ",", "0", "-", "0", "0", "unknownin", "unknownout", "unknownrecv", "unknownsend", "-", "-", "-", "c", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing an undetermined type"},
			{"718", "718", "circle", "11", "10", "24", "f", "b", "G", "f", "t", ",", "0", "-", "0", "719", "circle_in", "circle_out", "circle_recv", "circle_send", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "geometric circle, format '<center point,radius>'"},
			{"790", "790", "money", "11", "10", "8", "t", "b", "N", "f", "t", ",", "0", "-", "0", "791", "cash_in", "cash_out", "cash_recv", "cash_send", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "monetary amounts, $d,ddd.cc"},
			{"829", "829", "macaddr", "11", "10", "6", "f", "b", "U", "f", "t", ",", "0", "-", "0", "1040", "macaddr_in", "macaddr_out", "macaddr_recv", "macaddr_send", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "XX:XX:XX:XX:XX:XX, MAC address"},
			{"869", "869", "inet", "11", "10", "-1", "f", "b", "I", "t", "t", ",", "0", "-", "0", "1041", "inet_in", "inet_out", "inet_recv", "inet_send", "-", "-", "-", "i", "m", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "IP address/netmask, host address, netmask optional"},
			{"650", "650", "cidr", "11", "10", "-1", "f", "b", "I", "f", "t", ",", "0", "-", "0", "651", "cidr_in", "cidr_out", "cidr_recv", "cidr_send", "-", "-", "-", "i", "m", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "network IP address/netmask, network address"},
			{"774", "774", "macaddr8", "11", "10", "8", "f", "b", "U", "f", "t", ",", "0", "-", "0", "775", "macaddr8_in", "macaddr8_out", "macaddr8_recv", "macaddr8_send", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "XX:XX:XX:XX:XX:XX:XX:XX, MAC address"},
			{"1033", "1033", "aclitem", "11", "10", "16", "f", "b", "U", "f", "t", ",", "0", "-", "0", "1034", "aclitemin", "aclitemout", "-", "-", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "access control list"},
			{"1042", "1042", "bpchar", "11", "10", "-1", "f", "b", "S", "f", "t", ",", "0", "-", "0", "1014", "bpcharin", "bpcharout", "bpcharrecv", "bpcharsend", "bpchartypmodin", "bpchartypmodout", "-", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, "'char(length)' blank-padded string, fixed storage length"},
			{"1043", "1043", "varchar", "11", "10", "-1", "f", "b", "S", "f", "t", ",", "0", "-", "0", "1015", "varcharin", "varcharout", "varcharrecv", "varcharsend", "varchartypmodin", "varchartypmodout", "-", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, "'varchar(length)' non-blank-padded string, variable storage length"},
			{"1082", "1082", "date", "11", "10", "4", "t", "b", "D", "f", "t", ",", "0", "-", "0", "1182", "date_in", "date_out", "date_recv", "date_send", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "date"},
			{"1083", "1083", "time", "11", "10", "8", "t", "b", "D", "f", "t", ",", "0", "-", "0", "1183", "time_in", "time_out", "time_recv", "time_send", "timetypmodin", "timetypmodout", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "time of day"},
			{"1114", "1114", "timestamp", "11", "10", "8", "t", "b", "D", "f", "t", ",", "0", "-", "0", "1115", "timestamp_in", "timestamp_out", "timestamp_recv", "timestamp_send", "timestamptypmodin", "timestamptypmodout", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "date and time"},
			{"1184", "1184", "timestamptz", "11", "10", "8", "t", "b", "D", "t", "t", ",", "0", "-", "0", "1185", "timestamptz_in", "timestamptz_out", "timestamptz_recv", "timestamptz_send", "timestamptztypmodin", "timestamptztypmodout", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "date and time with time zone"},
			{"1186", "1186", "interval", "11", "10", "16", "f", "b", "T", "t", "t", ",", "0", "-", "0", "1187", "interval_in", "interval_out", "interval_recv", "interval_send", "intervaltypmodin", "intervaltypmodout", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "time interval, format 'number units ...'"},
			{"1266", "1266", "timetz", "11", "10", "12", "f", "b", "D", "f", "t", ",", "0", "-", "0", "1270", "timetz_in", "timetz_out", "timetz_recv", "timetz_send", "timetztypmodin", "timetztypmodout", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "time of day with time zone"},
			{"1560", "1560", "bit", "11", "10", "-1", "f", "b", "V", "f", "t", ",", "0", "-", "0", "1561", "bit_in", "bit_out", "bit_recv", "bit_send", "bittypmodin", "bittypmodout", "-", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "fixed-length bit string"},
			{"1562", "1562", "varbit", "11", "10", "-1", "f", "b", "V", "t", "t", ",", "0", "-", "0", "1563", "varbit_in", "varbit_out", "varbit_recv", "varbit_send", "varbittypmodin", "varbittypmodout", "-", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "variable-length bit string"},
			{"1700", "1700", "numeric", "11", "10", "-1", "f", "b", "N", "f", "t", ",", "0", "-", "0", "1231", "numeric_in", "numeric_out", "numeric_recv", "numeric_send", "numerictypmodin", "numerictypmodout", "-", "i", "m", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "'numeric(precision, scale)' arbitrary precision number"},
			{"1790", "1790", "refcursor", "11", "10", "-1", "f", "b", "U", "f", "t", ",", "0", "-", "0", "2201", "textin", "textout", "textrecv", "textsend", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "reference to cursor (portal name)"},
			{"2202", "2202", "regprocedure", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "2207", "regprocedurein", "regprocedureout", "regprocedurerecv", "regproceduresend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "registered procedure (with args)"},
			{"2203", "2203", "regoper", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "2208", "regoperin", "regoperout", "regoperrecv", "regopersend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "registered operator"},
			{"2204", "2204", "regoperator", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "2209", "regoperatorin", "regoperatorout", "regoperatorrecv", "regoperatorsend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "registered operator (with args)"},
			{"2205", "2205", "regclass", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "2210", "regclassin", "regclassout", "regclassrecv", "regclasssend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "registered class"},
			{"4191", "4191", "regcollation", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "4192", "regcollationin", "regcollationout", "regcollationrecv", "regcollationsend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "registered collation"},
			{"2206", "2206", "regtype", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "2211", "regtypein", "regtypeout", "regtyperecv", "regtypesend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "registered type"},
			{"4096", "4096", "regrole", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "4097", "regrolein", "regroleout", "regrolerecv", "regrolesend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "registered role"},
			{"4089", "4089", "regnamespace", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "4090", "regnamespacein", "regnamespaceout", "regnamespacerecv", "regnamespacesend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "registered namespace"},
			{"2950", "2950", "uuid", "11", "10", "16", "f", "b", "U", "f", "t", ",", "0", "-", "0", "2951", "uuid_in", "uuid_out", "uuid_recv", "uuid_send", "-", "-", "-", "c", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "UUID"},
			{"3220", "3220", "pg_lsn", "11", "10", "8", "t", "b", "U", "f", "t", ",", "0", "-", "0", "3221", "pg_lsn_in", "pg_lsn_out", "pg_lsn_recv", "pg_lsn_send", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "PostgreSQL LSN"},
			{"3614", "3614", "tsvector", "11", "10", "-1", "f", "b", "U", "f", "t", ",", "0", "-", "0", "3643", "tsvectorin", "tsvectorout", "tsvectorrecv", "tsvectorsend", "-", "-", "ts_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "text representation for text search"},
			{"3642", "3642", "gtsvector", "11", "10", "-1", "f", "b", "U", "f", "t", ",", "0", "-", "0", "3644", "gtsvectorin", "gtsvectorout", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "GiST index internal text representation for text search"},
			{"3615", "3615", "tsquery", "11", "10", "-1", "f", "b", "U", "f", "t", ",", "0", "-", "0", "3645", "tsqueryin", "tsqueryout", "tsqueryrecv", "tsquerysend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "query representation for text search"},
			{"3734", "3734", "regconfig", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "3735", "regconfigin", "regconfigout", "regconfigrecv", "regconfigsend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "registered text search configuration"},
			{"3769", "3769", "regdictionary", "11", "10", "4", "t", "b", "N", "f", "t", ",", "0", "-", "0", "3770", "regdictionaryin", "regdictionaryout", "regdictionaryrecv", "regdictionarysend", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "registered text search dictionary"},
			{"3802", "3802", "jsonb", "11", "10", "-1", "f", "b", "U", "f", "t", ",", "0", "jsonb_subscript_handler", "0", "3807", "jsonb_in", "jsonb_out", "jsonb_recv", "jsonb_send", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "Binary JSON"},
			{"4072", "4072", "jsonpath", "11", "10", "-1", "f", "b", "U", "f", "t", ",", "0", "-", "0", "4073", "jsonpath_in", "jsonpath_out", "jsonpath_recv", "jsonpath_send", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "JSON path"},
			{"2970", "2970", "txid_snapshot", "11", "10", "-1", "f", "b", "U", "f", "t", ",", "0", "-", "0", "2949", "txid_snapshot_in", "txid_snapshot_out", "txid_snapshot_recv", "txid_snapshot_send", "-", "-", "-", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "transaction snapshot"},
			{"5038", "5038", "pg_snapshot", "11", "10", "-1", "f", "b", "U", "f", "t", ",", "0", "-", "0", "5039", "pg_snapshot_in", "pg_snapshot_out", "pg_snapshot_recv", "pg_snapshot_send", "-", "-", "-", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "transaction snapshot"},
			{"3904", "3904", "int4range", "11", "10", "-1", "f", "r", "R", "f", "t", ",", "0", "-", "0", "3905", "range_in", "range_out", "range_recv", "range_send", "-", "-", "range_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "range of integers"},
			{"3906", "3906", "numrange", "11", "10", "-1", "f", "r", "R", "f", "t", ",", "0", "-", "0", "3907", "range_in", "range_out", "range_recv", "range_send", "-", "-", "range_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "range of numerics"},
			{"3908", "3908", "tsrange", "11", "10", "-1", "f", "r", "R", "f", "t", ",", "0", "-", "0", "3909", "range_in", "range_out", "range_recv", "range_send", "-", "-", "range_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "range of timestamps without time zone"},
			{"3910", "3910", "tstzrange", "11", "10", "-1", "f", "r", "R", "f", "t", ",", "0", "-", "0", "3911", "range_in", "range_out", "range_recv", "range_send", "-", "-", "range_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "range of timestamps with time zone"},
			{"3912", "3912", "daterange", "11", "10", "-1", "f", "r", "R", "f", "t", ",", "0", "-", "0", "3913", "range_in", "range_out", "range_recv", "range_send", "-", "-", "range_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "range of dates"},
			{"3926", "3926", "int8range", "11", "10", "-1", "f", "r", "R", "f", "t", ",", "0", "-", "0", "3927", "range_in", "range_out", "range_recv", "range_send", "-", "-", "range_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "range of bigints"},
			{"4451", "4451", "int4multirange", "11", "10", "-1", "f", "m", "R", "f", "t", ",", "0", "-", "0", "6150", "multirange_in", "multirange_out", "multirange_recv", "multirange_send", "-", "-", "multirange_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "multirange of integers"},
			{"4532", "4532", "nummultirange", "11", "10", "-1", "f", "m", "R", "f", "t", ",", "0", "-", "0", "6151", "multirange_in", "multirange_out", "multirange_recv", "multirange_send", "-", "-", "multirange_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "multirange of numerics"},
			{"4533", "4533", "tsmultirange", "11", "10", "-1", "f", "m", "R", "f", "t", ",", "0", "-", "0", "6152", "multirange_in", "multirange_out", "multirange_recv", "multirange_send", "-", "-", "multirange_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "multirange of timestamps without time zone"},
			{"4534", "4534", "tstzmultirange", "11", "10", "-1", "f", "m", "R", "f", "t", ",", "0", "-", "0", "6153", "multirange_in", "multirange_out", "multirange_recv", "multirange_send", "-", "-", "multirange_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "multirange of timestamps with time zone"},
			{"4535", "4535", "datemultirange", "11", "10", "-1", "f", "m", "R", "f", "t", ",", "0", "-", "0", "6155", "multirange_in", "multirange_out", "multirange_recv", "multirange_send", "-", "-", "multirange_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "multirange of dates"},
			{"4536", "4536", "int8multirange", "11", "10", "-1", "f", "m", "R", "f", "t", ",", "0", "-", "0", "6157", "multirange_in", "multirange_out", "multirange_recv", "multirange_send", "-", "-", "multirange_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "multirange of bigints"},
			{"2249", "2249", "record", "11", "10", "-1", "f", "p", "P", "f", "t", ",", "0", "-", "0", "2287", "record_in", "record_out", "record_recv", "record_send", "-", "-", "-", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing any composite type"},
			{"2287", "2287", "_record", "11", "10", "-1", "f", "p", "P", "f", "t", ",", "0", "array_subscript_handler", "2249", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"2275", "2275", "cstring", "11", "10", "-2", "f", "p", "P", "f", "t", ",", "0", "-", "0", "1263", "cstring_in", "cstring_out", "cstring_recv", "cstring_send", "-", "-", "-", "c", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "C-style string"},
			{"2276", "2276", "any", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "any_in", "any_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing any type"},
			{"2277", "2277", "anyarray", "11", "10", "-1", "f", "p", "P", "f", "t", ",", "0", "-", "0", "0", "anyarray_in", "anyarray_out", "anyarray_recv", "anyarray_send", "-", "-", "-", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing a polymorphic array type"},
			{"2278", "2278", "void", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "void_in", "void_out", "void_recv", "void_send", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type for the result of a function with no real result"},
			{"2279", "2279", "trigger", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "trigger_in", "trigger_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type for the result of a trigger function"},
			{"3838", "3838", "event_trigger", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "event_trigger_in", "event_trigger_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type for the result of an event trigger function"},
			{"2280", "2280", "language_handler", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "language_handler_in", "language_handler_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type for the result of a language handler function"},
			{"2281", "2281", "internal", "11", "10", "8", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "internal_in", "internal_out", "-", "-", "-", "-", "-", "d", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing an internal data structure"},
			{"2283", "2283", "anyelement", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "anyelement_in", "anyelement_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing a polymorphic base type"},
			{"2776", "2776", "anynonarray", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "anynonarray_in", "anynonarray_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing a polymorphic base type that is not an array"},
			{"3500", "3500", "anyenum", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "anyenum_in", "anyenum_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing a polymorphic base type that is an enum"},
			{"3115", "3115", "fdw_handler", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "fdw_handler_in", "fdw_handler_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type for the result of an FDW handler function"},
			{"325", "325", "index_am_handler", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "index_am_handler_in", "index_am_handler_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type for the result of an index AM handler function"},
			{"3310", "3310", "tsm_handler", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "tsm_handler_in", "tsm_handler_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type for the result of a tablesample method function"},
			{"269", "269", "table_am_handler", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "table_am_handler_in", "table_am_handler_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type for the result of a table AM handler function"},
			{"3831", "3831", "anyrange", "11", "10", "-1", "f", "p", "P", "f", "t", ",", "0", "-", "0", "0", "anyrange_in", "anyrange_out", "-", "-", "-", "-", "-", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing a range over a polymorphic base type"},
			{"5077", "5077", "anycompatible", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "anycompatible_in", "anycompatible_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing a polymorphic common type"},
			{"5078", "5078", "anycompatiblearray", "11", "10", "-1", "f", "p", "P", "f", "t", ",", "0", "-", "0", "0", "anycompatiblearray_in", "anycompatiblearray_out", "anycompatiblearray_recv", "anycompatiblearray_send", "-", "-", "-", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing an array of polymorphic common type elements"},
			{"5079", "5079", "anycompatiblenonarray", "11", "10", "4", "t", "p", "P", "f", "t", ",", "0", "-", "0", "0", "anycompatiblenonarray_in", "anycompatiblenonarray_out", "-", "-", "-", "-", "-", "i", "p", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing a polymorphic common type that is not an array"},
			{"5080", "5080", "anycompatiblerange", "11", "10", "-1", "f", "p", "P", "f", "t", ",", "0", "-", "0", "0", "anycompatiblerange_in", "anycompatiblerange_out", "-", "-", "-", "-", "-", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing a range over a polymorphic common type"},
			{"4537", "4537", "anymultirange", "11", "10", "-1", "f", "p", "P", "f", "t", ",", "0", "-", "0", "0", "anymultirange_in", "anymultirange_out", "-", "-", "-", "-", "-", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing a polymorphic base type that is a multirange"},
			{"4538", "4538", "anycompatiblemultirange", "11", "10", "-1", "f", "p", "P", "f", "t", ",", "0", "-", "0", "0", "anycompatiblemultirange_in", "anycompatiblemultirange_out", "-", "-", "-", "-", "-", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, "pseudo-type representing a multirange over a polymorphic common type"},
			{"4600", "4600", "pg_brin_bloom_summary", "11", "10", "-1", "f", "b", "Z", "f", "t", ",", "0", "-", "0", "0", "brin_bloom_summary_in", "brin_bloom_summary_out", "brin_bloom_summary_recv", "brin_bloom_summary_send", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, "pseudo-type representing BRIN bloom summary"},
			{"4601", "4601", "pg_brin_minmax_multi_summary", "11", "10", "-1", "f", "b", "Z", "f", "t", ",", "0", "-", "0", "0", "brin_minmax_multi_summary_in", "brin_minmax_multi_summary_out", "brin_minmax_multi_summary_recv", "brin_minmax_multi_summary_send", "-", "-", "-", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, "pseudo-type representing BRIN minmax-multi summary"},
			{"1000", "1000", "_bool", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "16", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1001", "1001", "_bytea", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "17", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1002", "1002", "_char", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "18", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1003", "1003", "_name", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "19", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "950", nil, nil, nil, nil, nil, nil},
			{"1016", "1016", "_int8", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "20", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1005", "1005", "_int2", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "21", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1006", "1006", "_int2vector", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "22", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1007", "1007", "_int4", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "23", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1008", "1008", "_regproc", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "24", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1009", "1009", "_text", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "25", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, nil},
			{"1028", "1028", "_oid", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "26", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1010", "1010", "_tid", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "27", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1011", "1011", "_xid", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "28", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1012", "1012", "_cid", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "29", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1013", "1013", "_oidvector", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "30", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"199", "199", "_json", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "114", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"143", "143", "_xml", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "142", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"271", "271", "_xid8", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "5069", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1017", "1017", "_point", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "600", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1018", "1018", "_lseg", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "601", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1019", "1019", "_path", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "602", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1020", "1020", "_box", "11", "10", "-1", "f", "b", "A", "f", "t", ";", "0", "array_subscript_handler", "603", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1027", "1027", "_polygon", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "604", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"629", "629", "_line", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "628", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1021", "1021", "_float4", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "700", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1022", "1022", "_float8", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "701", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"719", "719", "_circle", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "718", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"791", "791", "_money", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "790", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1040", "1040", "_macaddr", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "829", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1041", "1041", "_inet", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "869", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"651", "651", "_cidr", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "650", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"775", "775", "_macaddr8", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "774", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1034", "1034", "_aclitem", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1033", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1014", "1014", "_bpchar", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1042", "0", "array_in", "array_out", "array_recv", "array_send", "bpchartypmodin", "bpchartypmodout", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, nil},
			{"1015", "1015", "_varchar", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1043", "0", "array_in", "array_out", "array_recv", "array_send", "varchartypmodin", "varchartypmodout", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "100", nil, nil, nil, nil, nil, nil},
			{"1182", "1182", "_date", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1082", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1183", "1183", "_time", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1083", "0", "array_in", "array_out", "array_recv", "array_send", "timetypmodin", "timetypmodout", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1115", "1115", "_timestamp", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1114", "0", "array_in", "array_out", "array_recv", "array_send", "timestamptypmodin", "timestamptypmodout", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1185", "1185", "_timestamptz", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1184", "0", "array_in", "array_out", "array_recv", "array_send", "timestamptztypmodin", "timestamptztypmodout", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1187", "1187", "_interval", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1186", "0", "array_in", "array_out", "array_recv", "array_send", "intervaltypmodin", "intervaltypmodout", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1270", "1270", "_timetz", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1266", "0", "array_in", "array_out", "array_recv", "array_send", "timetztypmodin", "timetztypmodout", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1561", "1561", "_bit", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1560", "0", "array_in", "array_out", "array_recv", "array_send", "bittypmodin", "bittypmodout", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1563", "1563", "_varbit", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1562", "0", "array_in", "array_out", "array_recv", "array_send", "varbittypmodin", "varbittypmodout", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1231", "1231", "_numeric", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1700", "0", "array_in", "array_out", "array_recv", "array_send", "numerictypmodin", "numerictypmodout", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"2201", "2201", "_refcursor", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "1790", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"2207", "2207", "_regprocedure", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "2202", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"2208", "2208", "_regoper", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "2203", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"2209", "2209", "_regoperator", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "2204", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"2210", "2210", "_regclass", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "2205", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"4192", "4192", "_regcollation", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "4191", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"2211", "2211", "_regtype", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "2206", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"4097", "4097", "_regrole", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "4096", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"4090", "4090", "_regnamespace", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "4089", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"2951", "2951", "_uuid", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "2950", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3221", "3221", "_pg_lsn", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3220", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3643", "3643", "_tsvector", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3614", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3644", "3644", "_gtsvector", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3642", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3645", "3645", "_tsquery", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3615", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3735", "3735", "_regconfig", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3734", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3770", "3770", "_regdictionary", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3769", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3807", "3807", "_jsonb", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3802", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"4073", "4073", "_jsonpath", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "4072", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"2949", "2949", "_txid_snapshot", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "2970", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"5039", "5039", "_pg_snapshot", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "5038", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3905", "3905", "_int4range", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3904", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3907", "3907", "_numrange", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3906", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3909", "3909", "_tsrange", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3908", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3911", "3911", "_tstzrange", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3910", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3913", "3913", "_daterange", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3912", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"3927", "3927", "_int8range", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "3926", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"6150", "6150", "_int4multirange", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "4451", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"6151", "6151", "_nummultirange", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "4532", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"6152", "6152", "_tsmultirange", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "4533", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"6153", "6153", "_tstzmultirange", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "4534", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"6155", "6155", "_datemultirange", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "4535", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"6157", "6157", "_int8multirange", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "4536", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"1263", "1263", "_cstring", "11", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "2275", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"14718", "14718", "cardinal_number", "14704", "10", "4", "t", "d", "N", "f", "t", ",", "0", "-", "0", "14717", "domain_in", "int4out", "domain_recv", "int4send", "-", "-", "-", "i", "p", "f", "23", "-1", "0", "0", nil, nil, nil, nil, "integer", nil},
			{"14717", "14717", "_cardinal_number", "14704", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "14718", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"14721", "14721", "character_data", "14704", "10", "-1", "f", "d", "S", "f", "t", ",", "0", "-", "0", "14720", "domain_in", "varcharout", "domain_recv", "varcharsend", "-", "-", "-", "i", "x", "f", "1043", "-1", "0", "950", nil, nil, nil, nil, "character varying", nil},
			{"14720", "14720", "_character_data", "14704", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "14721", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "950", nil, nil, nil, nil, nil, nil},
			{"14723", "14723", "sql_identifier", "14704", "10", "64", "f", "d", "S", "f", "t", ",", "0", "-", "0", "14722", "domain_in", "nameout", "domain_recv", "namesend", "-", "-", "-", "c", "p", "f", "19", "-1", "0", "950", nil, nil, nil, nil, "name", nil},
			{"14722", "14722", "_sql_identifier", "14704", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "14723", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "950", nil, nil, nil, nil, nil, nil},
			{"14729", "14729", "time_stamp", "14704", "10", "8", "t", "d", "D", "f", "t", ",", "0", "-", "0", "14728", "domain_in", "timestamptz_out", "domain_recv", "timestamptz_send", "-", "-", "-", "d", "p", "f", "1184", "2", "0", "0", "{SQLVALUEFUNCTION :op 4 :type 1184 :typmod 2 :location -1}", "CURRENT_TIMESTAMP(2)", nil, nil, "timestamp(2) with time zone", nil},
			{"14728", "14728", "_time_stamp", "14704", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "14729", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "d", "x", "f", "0", "-1", "0", "0", nil, nil, nil, nil, nil, nil},
			{"14731", "14731", "yes_or_no", "14704", "10", "-1", "f", "d", "S", "f", "t", ",", "0", "-", "0", "14730", "domain_in", "varcharout", "domain_recv", "varcharsend", "-", "-", "-", "i", "x", "f", "1043", "7", "0", "950", nil, nil, nil, nil, "character varying(3)", nil},
			{"14730", "14730", "_yes_or_no", "14704", "10", "-1", "f", "b", "A", "f", "t", ",", "0", "array_subscript_handler", "14731", "0", "array_in", "array_out", "array_recv", "array_send", "-", "-", "array_typanalyze", "i", "x", "f", "0", "-1", "0", "950", nil, nil, nil, nil, nil, nil},
		},
		Tag: "SELECT 195",
	}
}

func (c *Catalog) getISchemaTables() *SystemResult {
	rows := c.GetInformationSchemaTables()
	return &SystemResult{
		Columns: []string{"table_catalog", "table_schema", "table_name", "table_type"},
		Rows:    rows,
		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
	}
}

func (c *Catalog) getISchemaColumns() *SystemResult {
	rows := c.GetInformationSchemaColumns()
	return &SystemResult{
		Columns: []string{"table_catalog", "table_schema", "table_name", "column_name", "ordinal_position", "data_type"},
		Rows:    rows,
		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
	}
}

func (c *Catalog) getPgClass() *SystemResult {
	tables := c.GetAllTables()
	var rows [][]interface{}
	oid := int32(1000)
	for name := range tables {
		if strings.HasPrefix(name, "pg_catalog.") {
			continue
		}
		rows = append(rows, []interface{}{oid, oid, name, int32(2200), "r", int32(10), int32(0)})
		oid++
	}
	return &SystemResult{
		Columns: []string{"oid", "oid2", "relname", "relnamespace", "relkind", "relowner", "reltype"},
		Rows:    rows,
		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
	}
}

func (c *Catalog) getPgAttribute() *SystemResult {
	tables := c.GetAllTables()
	var rows [][]interface{}
	tableOid := int32(1000)
	for tName, table := range tables {
		if strings.HasPrefix(tName, "pg_catalog.") {
			continue
		}
		for i, col := range table.Columns {
			rows = append(rows, []interface{}{tableOid, col.Name, int32(25), int32(i + 1), col.NotNull, int32(-1), false})
		}
		tableOid++
	}
	return &SystemResult{
		Columns: []string{"attrelid", "attname", "atttypid", "attnum", "attnotnull", "atttypmod", "attisdropped"},
		Rows:    rows,
		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
	}
}

func (c *Catalog) getPgConstraint() *SystemResult {
	tables := c.GetAllTables()
	var rows [][]interface{}
	oid := int32(3000)
	tableOid := int32(1000)
	for tName, table := range tables {
		if strings.HasPrefix(tName, "pg_catalog.") {
			continue
		}
		for _, con := range table.Constraints {
			contype := "c"
			switch con.Type {
			case "PRIMARY_KEY":
				contype = "p"
			case "FOREIGN_KEY":
				contype = "f"
			case "UNIQUE":
				contype = "u"
			}
			rows = append(rows, []interface{}{oid, con.ColumnName + "_" + contype, tableOid, contype, con.ReferencedTable})
			oid++
		}
		tableOid++
	}
	return &SystemResult{
		Columns: []string{"oid", "conname", "conrelid", "contype", "confrelid"},
		Rows:    rows,
		Tag:     fmt.Sprintf("SELECT %d", len(rows)),
	}
}

func emptyResult(tag string) *SystemResult {
	return &SystemResult{Columns: []string{}, Rows: [][]interface{}{}, Tag: tag}
}
