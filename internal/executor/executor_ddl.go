package executor

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/constants"
)

func init() {
	registerExec((*Executor).executeCreateTable)
	registerExec((*Executor).executeCreateView)
	registerExec((*Executor).executeCreateIndex)
	registerExec((*Executor).executeDropIndex)
	registerExec((*Executor).executeDropView)
	registerExec((*Executor).executeDropTable)
	registerExec((*Executor).executeAlterTable)
	registerExec((*Executor).executeCreateDatabase)
	registerExec((*Executor).executeDropDatabase)
	registerExec((*Executor).executeCreateSchema)
	registerExec((*Executor).executeDropSchema)
}

// executeCreateTable handles CREATE TABLE statements
func (e *Executor) executeCreateTable(ctx context.Context, stmt *ast.CreateTable) (*Result, error) {
	// Validate statement
	if err := e.validator.ValidateCreateTable(stmt); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Check context cancellation
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	// Convert AST columns to catalog columns
	columns := make([]catalog.Column, len(stmt.Columns))
	for i, col := range stmt.Columns {
		columns[i] = catalog.Column{
			Name:          col.Name.Name,
			Type:          col.Type,
			NotNull:       col.NotNull,
			Identity:      col.Identity,
			IdentityValue: 0,
		}
	}

	// Convert AST constraints to catalog constraints
	constraints := []catalog.Constraint{}

	// Add column-level constraints
	for _, col := range stmt.Columns {
		for _, constraint := range col.Constraints {
			switch c := constraint.(type) {
			case *ast.PrimaryKeyConstraint:
				constraints = append(constraints, catalog.Constraint{
					Type:       constants.ConstraintPrimaryKey,
					ColumnName: col.Name.Name,
				})
			case *ast.UniqueConstraint:
				constraints = append(constraints, catalog.Constraint{
					Type:       constants.ConstraintUnique,
					ColumnName: col.Name.Name,
				})
			case *ast.ForeignKeyConstraint:
				constraints = append(constraints, catalog.Constraint{
					Type:            constants.ConstraintForeignKey,
					ColumnName:      col.Name.Name,
					ReferencedTable: c.ReferencedTable,
					ReferencedCol:   c.ReferencedCol,
				})
			}
		}
	}

	// Add table-level constraints
	for _, constraint := range stmt.Constraints {
		switch c := constraint.(type) {
		case *ast.PrimaryKeyConstraint:
			constraints = append(constraints, catalog.Constraint{
				Type:       constants.ConstraintPrimaryKey,
				ColumnName: c.ColumnName,
			})
		case *ast.UniqueConstraint:
			constraints = append(constraints, catalog.Constraint{
				Type:       constants.ConstraintUnique,
				ColumnName: c.ColumnName,
			})
		case *ast.ForeignKeyConstraint:
			constraints = append(constraints, catalog.Constraint{
				Type:            constants.ConstraintForeignKey,
				ColumnName:      c.ColumnName,
				ReferencedTable: c.ReferencedTable,
				ReferencedCol:   c.ReferencedCol,
			})
		}
	}

	// Determine schema (may be in Table.Alias)
	schema := ""
	if stmt.Table.Alias != "" {
		schema = stmt.Table.Alias
	}

	// Create table in catalog (pass schema if provided)
	if err := e.catalog.CreateTable(stmt.Table.Name, columns, constraints, schema); err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	// Persist to storage
	table, err := e.catalog.GetTable(stmt.Table.Name, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve created table: %w", err)
	}

	e.persistTableWarn(table, schema)

	return &Result{Tag: constants.ResultCreateTable}, nil
}

func (e *Executor) executeCreateIndex(ctx context.Context, stmt *ast.CreateIndex) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	schema := schemaOrPublic(stmt.Table.Alias)

	columnNames := make([]string, 0, len(stmt.Columns))
	for _, col := range stmt.Columns {
		columnNames = append(columnNames, col.Name)
	}

	if err := e.catalog.CreateIndex(stmt.Table.Name, stmt.Name.Name, columnNames, schema); err != nil {
		return nil, fmt.Errorf("failed to create index: %w", err)
	}

	if table, err := e.catalog.GetTable(stmt.Table.Name, schema); err == nil {
		if err := e.persistTable(table, schema); err != nil {
			fmt.Printf("warning: failed to persist table %s.%s after CREATE INDEX: %v\n", schema, stmt.Table.Name, err)
		}
	}

	return &Result{Tag: constants.ResultCreateIndex}, nil
}

func (e *Executor) executeDropIndex(ctx context.Context, stmt *ast.DropIndex) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	schema := schemaOrPublic(stmt.Table.Alias)

	if err := e.catalog.DropIndex(stmt.Table.Name, stmt.Name.Name, schema); err != nil {
		return nil, fmt.Errorf("failed to drop index: %w", err)
	}

	if table, err := e.catalog.GetTable(stmt.Table.Name, schema); err == nil {
		if err := e.persistTable(table, schema); err != nil {
			fmt.Printf("warning: failed to persist table %s.%s after DROP INDEX: %v\n", schema, stmt.Table.Name, err)
		}
	}

	return &Result{Tag: constants.ResultDropIndex}, nil
}

func (e *Executor) executeCreateView(ctx context.Context, stmt *ast.CreateView) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	if stmt.Query == nil {
		return nil, fmt.Errorf("CREATE VIEW requires a SELECT query")
	}

	result, err := e.executeSelect(ctx, stmt.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to validate view query: %w", err)
	}

	// Validate explicit column list if provided
	if len(stmt.ColumnNames) > 0 {
		if len(stmt.ColumnNames) != len(result.Columns) {
			return nil, fmt.Errorf("column list has %d columns but SELECT query returns %d columns",
				len(stmt.ColumnNames), len(result.Columns))
		}
		// Validate uniqueness of explicit column names
		seenNames := make(map[string]struct{})
		for _, colName := range stmt.ColumnNames {
			normalized := strings.ToLower(strings.TrimSpace(colName))
			if normalized == "" {
				return nil, fmt.Errorf("view column name cannot be empty")
			}
			if _, exists := seenNames[normalized]; exists {
				return nil, fmt.Errorf("duplicate column name in view column list: %s", colName)
			}
			seenNames[normalized] = struct{}{}
		}
	}

	// Validate implicit column list (from SELECT)
	seenCols := make(map[string]struct{}, len(result.Columns))
	for _, colName := range result.Columns {
		normalized := strings.ToLower(strings.TrimSpace(colName))
		if normalized == "" {
			return nil, fmt.Errorf("view query produced an empty column name")
		}
		if _, exists := seenCols[normalized]; exists {
			return nil, fmt.Errorf("view query has duplicate output column: %s", colName)
		}
		seenCols[normalized] = struct{}{}
	}

	schema := stmt.Name.Alias
	if schema == "" {
		schema = "public"
	}

	if stmt.IfNotExists && !stmt.Replace {
		if _, err := e.catalog.GetView(stmt.Name.Name, schema); err == nil {
			return &Result{Tag: constants.ResultCreateView}, nil
		}
	}

	if stmt.Replace {
		if err := e.catalog.DropView(stmt.Name.Name, schema); err != nil {
			if !strings.Contains(err.Error(), "does not exist") {
				return nil, fmt.Errorf("failed to replace view: %w", err)
			}
		}
		if e.storage != nil {
			if err := e.storage.DeleteView(stmt.Name.Name, schema); err != nil {
				fmt.Printf("warning: failed to delete old view %s.%s during replace: %v\n", schema, stmt.Name.Name, err)
			}
		}
	}

	// Use explicit column names if provided, otherwise use queried column names
	columnNames := result.Columns
	if len(stmt.ColumnNames) > 0 {
		columnNames = stmt.ColumnNames
	}

	cols := make([]catalog.Column, len(columnNames))
	for i, name := range columnNames {
		cols[i] = catalog.Column{Name: name, Type: "TEXT"}
	}

	if err := e.catalog.CreateView(stmt.Name.Name, cols, stmt.Query, schema); err != nil {
		return nil, fmt.Errorf("failed to create view: %w", err)
	}

	if e.storage != nil {
		if err := e.storage.SaveView(&catalog.View{Name: stmt.Name.Name, Columns: cols, Query: stmt.Query, QueryText: stmt.QueryText}, schema); err != nil {
			fmt.Printf("warning: failed to persist view %s.%s: %v\n", schema, stmt.Name.Name, err)
		}
	}

	return &Result{Tag: constants.ResultCreateView}, nil
}

func (e *Executor) executeDropView(ctx context.Context, stmt *ast.DropView) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	schema := stmt.Name.Alias
	if schema == "" {
		schema = "public"
	}

	// Find dependent views before dropping
	dependentViews := e.catalog.FindDependentViews(stmt.Name.Name, schema)

	// Use DropViewWithBehavior for CASCADE/RESTRICT handling
	if err := e.catalog.DropViewWithBehavior(stmt.Name.Name, stmt.Behavior, schema); err != nil {
		if stmt.IfExists && strings.Contains(err.Error(), "does not exist") {
			return &Result{Tag: constants.ResultDropView}, nil
		}
		// If error is about dependencies and we're not using CASCADE, pass it through
		if strings.Contains(err.Error(), "cannot drop view") {
			return nil, err
		}
		return nil, fmt.Errorf("failed to drop view: %w", err)
	}

	// Delete persisted view and dependent views from storage
	if e.storage != nil {
		if err := e.storage.DeleteView(stmt.Name.Name, schema); err != nil {
			fmt.Printf("warning: failed to delete persisted view %s.%s: %v\n", schema, stmt.Name.Name, err)
		}

		// If CASCADE, also delete dependent views from storage
		if stmt.Behavior == "CASCADE" {
			for _, depName := range dependentViews {
				parts := strings.Split(depName, ".")
				if len(parts) == 2 {
					depSchema := parts[0]
					depViewName := parts[1]
					if err := e.storage.DeleteView(depViewName, depSchema); err != nil {
						fmt.Printf("warning: failed to delete dependent view %s from storage: %v\n", depName, err)
					}
				}
			}
		}
	}

	return &Result{Tag: constants.ResultDropView}, nil
}

// executeCreateDatabase handles CREATE DATABASE statements
func (e *Executor) executeCreateDatabase(ctx context.Context, stmt *ast.CreateDatabase) (*Result, error) {
	dbName := stmt.Name.Name
	if dbName == "" {
		return nil, fmt.Errorf("database name cannot be empty")
	}

	// Check context cancellation
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	// Check if database already exists
	dbTable, err := e.catalog.GetTable(constants.CatalogDatabase)
	if err != nil {
		return nil, fmt.Errorf("failed to access catalog: %w", err)
	}

	if rows, err := dbTable.SelectWhere("datname", dbName); err == nil && len(rows) > 0 {
		return nil, fmt.Errorf("database %s already exists", dbName)
	}

	// Create database entry
	nextOid := calculateNextDatabaseOID(dbTable)
	row := []interface{}{
		nextOid,                // oid
		dbName,                 // datname
		constants.DefaultOwner, // datdba (owner)
		6,                      // encoding (UTF8)
		"C",                    // datcollate
		"C",                    // datctype
		"c",                    // datlocprovider
		"",                     // daticulocale
		"",                     // daticurules
		"",                     // datacl
		"",                     // datcollversion
		true,                   // datallowconn
		false,                  // datistemplate
	}

	if err := dbTable.InsertRowUnsafe(row); err != nil {
		return nil, fmt.Errorf("failed to insert database: %w", err)
	}

	// Keep user objects isolated per database by mapping each DB to its own schema namespace.
	if dbName != "postgres" {
		if err := e.catalog.CreateSchema(dbName); err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				return nil, fmt.Errorf("failed to create schema for database %s: %w", dbName, err)
			}
		}
		if e.storage != nil {
			if err := e.storage.CreateSchema(dbName); err != nil && !strings.Contains(err.Error(), "already exists") {
				fmt.Printf("warning: failed to persist schema for database %s: %v\n", dbName, err)
			}
		}
	}

	// Persist to storage
	if e.storage != nil {
		if err := e.storage.SaveTable(dbTable); err != nil {
			fmt.Printf("warning: failed to persist pg_database: %v\n", err)
		}
	}

	return &Result{Tag: constants.ResultCreateDatabase}, nil
}

// executeDropDatabase handles DROP DATABASE statements
func (e *Executor) executeDropDatabase(ctx context.Context, stmt *ast.DropDatabase) (*Result, error) {
	dbName := stmt.Name
	if dbName == "" {
		return nil, fmt.Errorf("database name cannot be empty")
	}

	// Check context cancellation
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	// Check if database exists
	dbTable, err := e.catalog.GetTable(constants.CatalogDatabase)
	if err != nil {
		return nil, fmt.Errorf("failed to access catalog: %w", err)
	}

	rows, err := dbTable.SelectWhere("datname", dbName)
	if err != nil || len(rows) == 0 {
		return nil, fmt.Errorf("database %s does not exist", dbName)
	}

	// Delete the database entry
	// Note: This is a simplistic delete that only removes from pg_database catalog
	// A real implementation would handle cascading deletes, active connections, etc.
	if err := dbTable.DeleteWhere("datname", dbName); err != nil {
		return nil, fmt.Errorf("failed to delete database: %w", err)
	}

	// Persist to storage
	if e.storage != nil {
		if err := e.storage.SaveTable(dbTable); err != nil {
			fmt.Printf("warning: failed to persist pg_database after DELETE: %v\n", err)
		}
	}

	return &Result{Tag: constants.ResultDropDatabase}, nil
}

// calculateNextDatabaseOID calculates the next available OID for a database
func calculateNextDatabaseOID(table *catalog.Table) int {
	maxOID := 0
	rows := table.SelectAll()

	for _, row := range rows {
		if len(row) == 0 {
			continue
		}

		switch v := row[0].(type) {
		case int:
			if v > maxOID {
				maxOID = v
			}
		case int64:
			if int(v) > maxOID {
				maxOID = int(v)
			}
		case string:
			if parsed, err := strconv.Atoi(v); err == nil && parsed > maxOID {
				maxOID = parsed
			}
		}
	}

	if maxOID < 1 {
		maxOID = 1
	}

	return maxOID + 1
}

// executeCreateSchema handles CREATE SCHEMA statements
func (e *Executor) executeCreateSchema(ctx context.Context, stmt *ast.CreateSchema) (*Result, error) {
	schemaName := stmt.Name
	if schemaName == "" {
		return nil, fmt.Errorf("schema name cannot be empty")
	}

	// Check context cancellation
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	if err := e.catalog.CreateSchema(schemaName); err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	// Persist schema metadata if storage supports it
	if e.storage != nil {
		if err := e.storage.CreateSchema(schemaName); err != nil {
			// If schema already exists in persistent metadata, ignore the error
			// but surface other errors as warnings.
			// Detect duplicate schema error by message match
			if !strings.Contains(err.Error(), "already exists") {
				fmt.Printf("warning: failed to persist schema %s: %v\n", schemaName, err)
			}
		}
	}

	return &Result{Tag: constants.ResultCreateTable}, nil
}

// executeDropTable handles DROP TABLE statements with FK dependency checks.
func (e *Executor) executeDropTable(ctx context.Context, stmt *ast.DropTable) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	schema := schemaOrPublic(stmt.Table.Alias)
	tableName := stmt.Table.Name
	behavior := stmt.Behavior
	if behavior == "" {
		behavior = "RESTRICT"
	}

	if !e.catalog.TableExists(tableName, schema) {
		if stmt.IfExists {
			return &Result{Tag: constants.ResultDropTable}, nil
		}
		return nil, fmt.Errorf("failed to drop table: table %s.%s does not exist", schema, tableName)
	}

	fkDependents := e.catalog.FindForeignKeyDependents(schema, tableName)
	viewDependents := e.catalog.FindViewsUsingSource(tableName, schema)

	if behavior != "CASCADE" && (len(fkDependents) > 0 || len(viewDependents) > 0) {
		if len(fkDependents) > 0 {
			return nil, fmt.Errorf("cannot drop table %s.%s: referenced by foreign key in table(s) %v", schema, tableName, fkDependents)
		}
		return nil, fmt.Errorf("cannot drop table %s.%s: dependent view(s) exist %v", schema, tableName, viewDependents)
	}

	if behavior == "CASCADE" {
		updatedTables, err := e.catalog.RemoveForeignKeyReferencesToTable(schema, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed to drop table: %w", err)
		}

		for _, depView := range viewDependents {
			parts := strings.Split(depView, ".")
			if len(parts) != 2 {
				continue
			}
			depSchema := parts[0]
			depName := parts[1]
			if err := e.catalog.DropView(depName, depSchema); err != nil {
				return nil, fmt.Errorf("failed to drop dependent view %s during CASCADE: %w", depView, err)
			}
			if e.storage != nil {
				if err := e.storage.DeleteView(depName, depSchema); err != nil {
					fmt.Printf("warning: failed to delete dependent view %s from storage: %v\n", depView, err)
				}
			}
		}

		if e.storage != nil {
			for _, depTable := range updatedTables {
				parts := strings.Split(depTable, ".")
				if len(parts) != 2 {
					continue
				}
				depSchema := parts[0]
				depName := parts[1]
				tbl, err := e.catalog.GetTable(depName, depSchema)
				if err != nil {
					continue
				}
				if err := e.storage.SaveTableWithSchema(tbl, depSchema); err != nil {
					fmt.Printf("warning: failed to persist dependent table %s after CASCADE DROP TABLE: %v\n", depTable, err)
				}
			}
		}
	}

	if err := e.catalog.DropTable(tableName, schema); err != nil {
		if stmt.IfExists && strings.Contains(err.Error(), "does not exist") {
			return &Result{Tag: constants.ResultDropTable}, nil
		}
		return nil, fmt.Errorf("failed to drop table: %w", err)
	}

	if e.storage != nil {
		if err := e.storage.DeleteTable(tableName, schema); err != nil {
			fmt.Printf("warning: failed to delete persisted table %s.%s: %v\n", schema, tableName, err)
		}
	}

	return &Result{Tag: constants.ResultDropTable}, nil
}

// executeDropSchema handles DROP SCHEMA statements
func (e *Executor) executeDropSchema(ctx context.Context, stmt *ast.DropSchema) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	schemaName := stmt.Name
	if schemaName == "" {
		return nil, fmt.Errorf("schema name cannot be empty")
	}
	behavior := stmt.Behavior
	if behavior == "" {
		behavior = "RESTRICT"
	}

	isEmpty, err := e.catalog.SchemaIsEmpty(schemaName)
	if err != nil {
		if stmt.IfExists {
			return &Result{Tag: constants.ResultDropTable}, nil
		}
		return nil, fmt.Errorf("failed to drop schema: %w", err)
	}

	if behavior != "CASCADE" && !isEmpty {
		return nil, fmt.Errorf("cannot drop schema %s: schema is not empty (use CASCADE)", schemaName)
	}

	if err := e.catalog.DropSchema(schemaName); err != nil {
		if stmt.IfExists && strings.Contains(err.Error(), "does not exist") {
			return &Result{Tag: constants.ResultDropTable}, nil
		}
		return nil, fmt.Errorf("failed to drop schema: %w", err)
	}

	// Clean up persistent storage
	if e.storage != nil {
		if err := e.storage.DeleteSchema(schemaName); err != nil {
			fmt.Printf("warning: failed to delete persisted schema %s: %v\n", schemaName, err)
		}
	}

	return &Result{Tag: constants.ResultDropTable}, nil
}

// executeAlterTable handles ALTER TABLE statements
func (e *Executor) executeAlterTable(ctx context.Context, stmt *ast.AlterTable) (*Result, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	schema := schemaOrPublic(stmt.Table.Alias)
	tableName := stmt.Table.Name

	// Verify table exists
	_, err := e.catalog.GetTable(tableName, schema)
	if err != nil {
		return nil, fmt.Errorf("table %s.%s does not exist", schema, tableName)
	}

	// Execute action based on type
	switch action := stmt.Action.(type) {
	case *ast.AddColumn:
		return e.executeAddColumn(ctx, schema, tableName, action)
	case *ast.DropColumn:
		return e.executeDropColumn(ctx, schema, tableName, action)
	case *ast.AlterColumn:
		return e.executeAlterColumn(ctx, schema, tableName, action)
	case *ast.RenameColumn:
		return e.executeRenameColumn(ctx, schema, tableName, action)
	default:
		return nil, fmt.Errorf("unsupported ALTER TABLE action: %T", action)
	}
}

func (e *Executor) executeAddColumn(ctx context.Context, schema string, tableName string, action *ast.AddColumn) (*Result, error) {
	// Create new column from AST definition
	newCol := catalog.Column{
		Name:     action.Column.Name.Name,
		Type:     action.Column.Type,
		NotNull:  action.Column.NotNull,
		Identity: action.Column.Identity,
	}

	// Check if column has PRIMARY KEY constraint
	var pkConstraint *catalog.Constraint
	for _, astConstraint := range action.Column.Constraints {
		if pk, ok := astConstraint.(*ast.PrimaryKeyConstraint); ok {
			pkConstraint = &catalog.Constraint{
				Type:       constants.ConstraintPrimaryKey,
				ColumnName: pk.ColumnName,
			}
			break
		}
	}

	// Add column to catalog (with constraint validation if applicable)
	var err error
	if pkConstraint != nil {
		err = e.catalog.AddColumnWithConstraint(tableName, &newCol, pkConstraint, schema)
	} else {
		err = e.catalog.AddColumn(tableName, &newCol, schema)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to add column: %w", err)
	}

	// Persist change in storage
	table, err := e.catalog.GetTable(tableName, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to get table for persistence: %w", err)
	}

	if err := e.persistTable(table, schema); err != nil {
		return nil, fmt.Errorf("failed to persist table schema: %w", err)
	}

	return &Result{Tag: "ALTER TABLE (ADD COLUMN)"}, nil
}

func (e *Executor) executeDropColumn(ctx context.Context, schema string, tableName string, action *ast.DropColumn) (*Result, error) {
	// Drop column from catalog
	if err := e.catalog.DropColumn(tableName, action.ColumnName, schema); err != nil {
		return nil, fmt.Errorf("failed to drop column: %w", err)
	}

	// Persist change in storage
	table, err := e.catalog.GetTable(tableName, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to get table for persistence: %w", err)
	}

	if e.storage != nil {
		if err := e.storage.SaveTableWithSchema(table, schema); err != nil {
			return nil, fmt.Errorf("failed to persist table schema: %w", err)
		}

		// Also remove column data from all rows
		if err := e.storage.DropColumnData(tableName, action.ColumnName, schema); err != nil {
			fmt.Printf("warning: failed to drop column data: %v\n", err)
		}
	}

	return &Result{Tag: "ALTER TABLE (DROP COLUMN)"}, nil
}

func (e *Executor) executeAlterColumn(ctx context.Context, schema string, tableName string, action *ast.AlterColumn) (*Result, error) {
	// Modify column type in catalog
	if err := e.catalog.AlterColumnType(tableName, action.ColumnName, action.NewType, schema); err != nil {
		return nil, fmt.Errorf("failed to alter column: %w", err)
	}

	// Persist change in storage
	table, err := e.catalog.GetTable(tableName, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to get table for persistence: %w", err)
	}

	if err := e.persistTable(table, schema); err != nil {
		return nil, fmt.Errorf("failed to persist table schema: %w", err)
	}

	return &Result{Tag: "ALTER TABLE (ALTER COLUMN)"}, nil
}

func (e *Executor) executeRenameColumn(ctx context.Context, schema string, tableName string, action *ast.RenameColumn) (*Result, error) {
	// Rename column in catalog
	if err := e.catalog.RenameColumn(tableName, action.OldName, action.NewName, schema); err != nil {
		return nil, fmt.Errorf("failed to rename column: %w", err)
	}

	// Persist change in storage
	table, err := e.catalog.GetTable(tableName, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to get table for persistence: %w", err)
	}

	if e.storage != nil {
		if err := e.storage.SaveTableWithSchema(table, schema); err != nil {
			return nil, fmt.Errorf("failed to persist table schema: %w", err)
		}

		// Also rename column in data
		if err := e.storage.RenameColumnData(tableName, action.OldName, action.NewName, schema); err != nil {
			fmt.Printf("warning: failed to rename column data: %v\n", err)
		}
	}

	return &Result{Tag: "ALTER TABLE (RENAME COLUMN)"}, nil
}
