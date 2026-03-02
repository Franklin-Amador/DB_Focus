package constants

import "fmt"

// Constraint types
const (
	ConstraintPrimaryKey = "PRIMARY_KEY"
	ConstraintForeignKey = "FOREIGN_KEY"
	ConstraintUnique     = "UNIQUE"
	ConstraintNotNull    = "NOT_NULL"
)

// Trigger timing
const (
	TriggerBefore    = "BEFORE"
	TriggerAfter     = "AFTER"
	TriggerInsteadOf = "INSTEAD OF"
)

// Trigger events
const (
	TriggerInsert = "INSERT"
	TriggerUpdate = "UPDATE"
	TriggerDelete = "DELETE"
)

// Job time units
const (
	JobUnitMinute = "MINUTE"
	JobUnitHour   = "HOUR"
	JobUnitDay    = "DAY"
)

// Job actions
const (
	JobActionEnable  = "ENABLE"
	JobActionDisable = "DISABLE"
)

// Join types
const (
	JoinInner = "INNER"
	JoinLeft  = "LEFT"
	JoinRight = "RIGHT"
	JoinFull  = "FULL"
	JoinCross = "CROSS"
)

// Order direction
const (
	OrderAsc  = "ASC"
	OrderDesc = "DESC"
)

// Data types
const (
	DataTypeInteger = "INTEGER"
	DataTypeText    = "TEXT"
	DataTypeBoolean = "BOOLEAN"
)

// Result tags
const (
	ResultOK              = "OK"
	ResultInsert          = "INSERT 0 1"
	ResultUpdate          = "UPDATE 1"
	ResultDelete          = "DELETE 1"
	ResultCreateTable     = "CREATE TABLE"
	ResultDropTable       = "DROP TABLE"
	ResultCreateDatabase  = "CREATE DATABASE"
	ResultCreateProcedure = "CREATE PROCEDURE"
	ResultDropProcedure   = "DROP PROCEDURE"
	ResultCreateTrigger   = "CREATE TRIGGER"
	ResultDropTrigger     = "DROP TRIGGER"
	ResultCreateJob       = "CREATE JOB"
	ResultDropJob         = "DROP JOB"
	ResultCall            = "CALL"
)

// ResultSelectTag returns a SELECT result tag with the row count.
func ResultSelectTag(rowCount int) string {
	return fmt.Sprintf("SELECT %d", rowCount)
}

// System catalog tables
const (
	CatalogNamespace = "pg_catalog.pg_namespace"
	CatalogRoles     = "pg_catalog.pg_roles"
	CatalogDatabase  = "pg_catalog.pg_database"
	CatalogTables    = "pg_catalog.pg_tables"
	CatalogProc      = "pg_catalog.pg_proc"
	CatalogTrigger   = "pg_catalog.pg_trigger"
	CatalogJob       = "pg_catalog.pg_job"
)

// Default values
const (
	DefaultOwner = 10
)
