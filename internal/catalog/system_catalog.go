package catalog

import "dbf/internal/constants"

func (c *Catalog) initSystemCatalog() {
	// pg_catalog.pg_namespace
	_ = c.CreateTable(constants.CatalogNamespace, []Column{
		{Name: "oid", Type: constants.DataTypeInteger},
		{Name: "nspname", Type: constants.DataTypeText},
		{Name: "nspowner", Type: constants.DataTypeInteger},
	}, nil)

	if ns, err := c.GetTable(constants.CatalogNamespace); err == nil {
		_ = ns.InsertRowUnsafe([]interface{}{11, "pg_catalog", 10})
		_ = ns.InsertRowUnsafe([]interface{}{2200, "public", 10})
	}

	// pg_catalog.pg_roles
	_ = c.CreateTable(constants.CatalogRoles, []Column{
		{Name: "oid", Type: constants.DataTypeInteger},
		{Name: "rolname", Type: constants.DataTypeText},
		{Name: "rolsuper", Type: constants.DataTypeBoolean},
		{Name: "rolinherit", Type: constants.DataTypeBoolean},
		{Name: "rolcreaterole", Type: constants.DataTypeBoolean},
		{Name: "rolcreatedb", Type: constants.DataTypeBoolean},
		{Name: "rolcanlogin", Type: constants.DataTypeBoolean},
	}, nil)

	if roles, err := c.GetTable(constants.CatalogRoles); err == nil {
		_ = roles.InsertRowUnsafe([]interface{}{10, "postgres", true, true, true, true, true})
	}

	// pg_catalog.pg_database
	_ = c.CreateTable(constants.CatalogDatabase, []Column{
		{Name: "oid", Type: constants.DataTypeInteger},
		{Name: "datname", Type: constants.DataTypeText},
		{Name: "datdba", Type: constants.DataTypeInteger},
		{Name: "encoding", Type: constants.DataTypeInteger},
		{Name: "datcollate", Type: constants.DataTypeText},
		{Name: "datctype", Type: constants.DataTypeText},
		{Name: "datlocprovider", Type: constants.DataTypeText},
		{Name: "daticulocale", Type: constants.DataTypeText},
		{Name: "daticurules", Type: constants.DataTypeText},
		{Name: "datacl", Type: constants.DataTypeText},
		{Name: "datcollversion", Type: constants.DataTypeText},
		{Name: "datallowconn", Type: constants.DataTypeBoolean},
		{Name: "datistemplate", Type: constants.DataTypeBoolean},
	}, nil)

	if dbs, err := c.GetTable(constants.CatalogDatabase); err == nil {
		_ = dbs.InsertRowUnsafe([]interface{}{1, "postgres", 10, 6, "C", "C", "c", "", "", "", "", true, false})
	}

	// pg_catalog.pg_extension
	_ = c.CreateTable("pg_catalog.pg_extension", []Column{
		{Name: "oid", Type: constants.DataTypeInteger},
		{Name: "extname", Type: constants.DataTypeText},
		{Name: "extnamespace", Type: constants.DataTypeInteger},
		{Name: "extversion", Type: constants.DataTypeText},
	}, nil)

	// pg_catalog.pg_replication_slots
	_ = c.CreateTable("pg_replication_slots", []Column{
		{Name: "slot_name", Type: constants.DataTypeText},
		{Name: "plugin", Type: constants.DataTypeText},
		{Name: "slot_type", Type: constants.DataTypeText},
		{Name: "datoid", Type: constants.DataTypeInteger},
		{Name: "database", Type: constants.DataTypeText},
		{Name: "active", Type: constants.DataTypeBoolean},
	}, nil)

	// pg_catalog.pg_stat_gssapi
	_ = c.CreateTable("pg_catalog.pg_stat_gssapi", []Column{
		{Name: "gss_authenticated", Type: constants.DataTypeBoolean},
		{Name: "encrypted", Type: constants.DataTypeBoolean},
		{Name: "pid", Type: constants.DataTypeInteger},
	}, nil)

	if gss, err := c.GetTable("pg_catalog.pg_stat_gssapi"); err == nil {
		_ = gss.InsertRowUnsafe([]interface{}{false, false, 0})
	}

	// pg_catalog.pg_class (tables and indexes)
	_ = c.CreateTable("pg_catalog.pg_class", []Column{
		{Name: "oid", Type: constants.DataTypeInteger},
		{Name: "relname", Type: constants.DataTypeText},
		{Name: "relnamespace", Type: constants.DataTypeInteger},
		{Name: "relkind", Type: constants.DataTypeText},
		{Name: "relowner", Type: constants.DataTypeInteger},
	}, nil)

	// pg_catalog.pg_attribute (columns)
	_ = c.CreateTable("pg_catalog.pg_attribute", []Column{
		{Name: "attrelid", Type: constants.DataTypeInteger},
		{Name: "attname", Type: constants.DataTypeText},
		{Name: "atttypid", Type: constants.DataTypeInteger},
		{Name: "attnum", Type: constants.DataTypeInteger},
		{Name: "attnotnull", Type: constants.DataTypeBoolean},
	}, nil)

	// pg_catalog.pg_settings
	_ = c.CreateTable("pg_catalog.pg_settings", []Column{
		{Name: "name", Type: constants.DataTypeText},
		{Name: "setting", Type: constants.DataTypeText},
	}, nil)

	if settings, err := c.GetTable("pg_catalog.pg_settings"); err == nil {
		_ = settings.InsertRowUnsafe([]interface{}{"server_version", "16.1"})
		_ = settings.InsertRowUnsafe([]interface{}{"server_encoding", "UTF8"})
		_ = settings.InsertRowUnsafe([]interface{}{"client_encoding", "UTF8"})
		_ = settings.InsertRowUnsafe([]interface{}{"DateStyle", "ISO, MDY"})
		_ = settings.InsertRowUnsafe([]interface{}{"TimeZone", "UTC"})
		_ = settings.InsertRowUnsafe([]interface{}{"integer_datetimes", "on"})
		_ = settings.InsertRowUnsafe([]interface{}{"standard_conforming_strings", "on"})
	}

	// pg_catalog.pg_type - data types catalog
	_ = c.CreateTable("pg_catalog.pg_type", []Column{
		{Name: "oid", Type: constants.DataTypeInteger},
		{Name: "typname", Type: constants.DataTypeText},
		{Name: "typtype", Type: constants.DataTypeText},
		{Name: "typlen", Type: constants.DataTypeInteger},
		{Name: "typbasetype", Type: constants.DataTypeInteger},
		{Name: "typtypmod", Type: constants.DataTypeInteger},
		{Name: "typelem", Type: constants.DataTypeInteger},
		{Name: "typrelid", Type: constants.DataTypeInteger},
		{Name: "typcategory", Type: constants.DataTypeText},
		{Name: "typnamespace", Type: constants.DataTypeInteger},
	}, nil)

	if types, err := c.GetTable("pg_catalog.pg_type"); err == nil {
		// Standard data types
		_ = types.InsertRowUnsafe([]interface{}{16, "bool", "b", 1, 0, -1, 0, 0, "B", 11})
		_ = types.InsertRowUnsafe([]interface{}{20, "int8", "b", 8, 0, -1, 0, 0, "N", 11})
		_ = types.InsertRowUnsafe([]interface{}{21, "int2", "b", 2, 0, -1, 0, 0, "N", 11})
		_ = types.InsertRowUnsafe([]interface{}{23, "int4", "b", 4, 0, -1, 0, 0, "N", 11})
		_ = types.InsertRowUnsafe([]interface{}{25, "text", "b", -1, 0, -1, 0, 0, "S", 11})
		_ = types.InsertRowUnsafe([]interface{}{700, "float4", "b", 4, 0, -1, 0, 0, "N", 11})
		_ = types.InsertRowUnsafe([]interface{}{701, "float8", "b", 8, 0, -1, 0, 0, "N", 11})
		_ = types.InsertRowUnsafe([]interface{}{1043, "varchar", "b", -1, 0, -1, 0, 0, "S", 11})
		_ = types.InsertRowUnsafe([]interface{}{1082, "date", "b", 4, 0, -1, 0, 0, "D", 11})
		_ = types.InsertRowUnsafe([]interface{}{1114, "timestamp", "b", 8, 0, -1, 0, 0, "D", 11})
	}

	// pg_catalog.pg_proc - stored procedures catalog
	_ = c.CreateTable(constants.CatalogProc, []Column{
		{Name: "oid", Type: constants.DataTypeInteger},
		{Name: "proname", Type: constants.DataTypeText},
		{Name: "pronamespace", Type: constants.DataTypeInteger},
		{Name: "proowner", Type: constants.DataTypeInteger},
		{Name: "prolang", Type: constants.DataTypeInteger},
		{Name: "pronargs", Type: constants.DataTypeInteger},
		{Name: "proargtypes", Type: constants.DataTypeText},
		{Name: "prosrc", Type: constants.DataTypeText},
	}, nil)

	// pg_catalog.pg_trigger - triggers catalog
	_ = c.CreateTable(constants.CatalogTrigger, []Column{
		{Name: "oid", Type: constants.DataTypeInteger},
		{Name: "tgname", Type: constants.DataTypeText},
		{Name: "tgrelid", Type: constants.DataTypeInteger},
		{Name: "tgtype", Type: constants.DataTypeInteger},
		{Name: "tgenabled", Type: constants.DataTypeText},
		{Name: "tgisinternal", Type: constants.DataTypeBoolean},
		{Name: "tgconstrrelid", Type: constants.DataTypeInteger},
		{Name: "tgfoid", Type: constants.DataTypeInteger},
		{Name: "tgargs", Type: constants.DataTypeText},
	}, nil)

	// pg_catalog.pg_job - scheduled jobs catalog (FocusDB extension)
	_ = c.CreateTable(constants.CatalogJob, []Column{
		{Name: "oid", Type: constants.DataTypeInteger},
		{Name: "jobname", Type: constants.DataTypeText},
		{Name: "jobinterval", Type: constants.DataTypeInteger},
		{Name: "jobunit", Type: constants.DataTypeText},
		{Name: "jobenabled", Type: constants.DataTypeBoolean},
		{Name: "joblastrun", Type: constants.DataTypeText},
		{Name: "jobowner", Type: constants.DataTypeInteger},
	}, nil)

	// focus.users - internal user registry for pgAdmin compatibility
	_ = c.CreateTable(focusUsersTable, []Column{
		{Name: "username", Type: constants.DataTypeText},
		{Name: "superuser", Type: constants.DataTypeBoolean},
		{Name: "created_at", Type: constants.DataTypeText},
	}, nil)

	if users, err := c.GetTable(focusUsersTable); err == nil {
		// Register default postgres user
		_ = users.InsertRowUnsafe([]interface{}{"postgres", true, "2026-02-22T16:00:00Z"})
		_ = users.InsertRowUnsafe([]interface{}{"admin", true, "2026-02-22T16:00:00Z"})
	}
}
