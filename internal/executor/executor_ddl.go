package executor

import (
	"context"
	"fmt"
	"strconv"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/constants"
)

// executeCreateTable handles CREATE TABLE statements
func (e *Executor) executeCreateTable(ctx context.Context, stmt *ast.CreateTable) (*Result, error) {
	// Validate statement
	if err := e.validator.ValidateCreateTable(stmt); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
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

	// Create table in catalog
	if err := e.catalog.CreateTable(stmt.Table.Name, columns, constraints); err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	// Persist to storage
	table, err := e.catalog.GetTable(stmt.Table.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve created table: %w", err)
	}

	if e.storage != nil {
		if err := e.storage.SaveTable(table); err != nil {
			fmt.Printf("warning: failed to persist table %s: %v\n", stmt.Table.Name, err)
		}
	}

	return &Result{Tag: constants.ResultCreateTable}, nil
}

// executeCreateDatabase handles CREATE DATABASE statements
func (e *Executor) executeCreateDatabase(ctx context.Context, stmt *ast.CreateDatabase) (*Result, error) {
	dbName := stmt.Name.Name
	if dbName == "" {
		return nil, fmt.Errorf("database name cannot be empty")
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
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

	// Persist to storage
	if e.storage != nil {
		if err := e.storage.SaveTable(dbTable); err != nil {
			fmt.Printf("warning: failed to persist pg_database: %v\n", err)
		}
	}

	return &Result{Tag: constants.ResultCreateDatabase}, nil
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
