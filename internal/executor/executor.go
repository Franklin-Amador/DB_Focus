package executor

import (
	"context"
	"fmt"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/constants"
	"dbf/internal/storage"
	"dbf/internal/validator"
)

// Result represents the result of a query execution
type Result struct {
	Columns []string
	Rows    [][]interface{}
	Tag     string
}

// Executor is the main SQL statement executor
type Executor struct {
	catalog         *catalog.Catalog
	storage         storage.Backend
	validator       *validator.Validator
	triggersEnabled bool
}

// New creates a new Executor instance
func New(cat *catalog.Catalog, st storage.Backend) *Executor {
	return &Executor{
		catalog:         cat,
		storage:         st,
		validator:       validator.New(),
		triggersEnabled: true,
	}
}

// Execute dispatches statement execution to appropriate handlers
func (e *Executor) Execute(ctx context.Context, stmt ast.Statement) (*Result, error) {
	if stmt == nil {
		return &Result{Tag: constants.ResultOK}, nil
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	switch s := stmt.(type) {
	// DDL statements
	case *ast.CreateTable:
		return e.executeCreateTable(ctx, s)
	case *ast.DropTable:
		return e.executeDropTable(ctx, s)
	case *ast.CreateDatabase:
		return e.executeCreateDatabase(ctx, s)

	case *ast.CreateSchema:
		return e.executeCreateSchema(ctx, s)
	case *ast.DropSchema:
		return e.executeDropSchema(ctx, s)

	// DML statements
	case *ast.Insert:
		return e.executeInsert(ctx, s)
	case *ast.Update:
		return e.executeUpdate(ctx, s)
	case *ast.Delete:
		return e.executeDelete(ctx, s)

	// Query statements
	case *ast.Select:
		return e.executeSelect(ctx, s)
	case *ast.SelectFunction:
		return e.executeSelectFunction(ctx, s)

	// Procedure statements
	case *ast.CreateProcedure:
		return e.executeCreateProcedure(ctx, s)
	case *ast.CallProcedure:
		return e.executeCallProcedure(ctx, s)
	case *ast.DropProcedure:
		return e.executeDropProcedure(ctx, s)

	// Trigger statements
	case *ast.CreateTrigger:
		return e.executeCreateTrigger(ctx, s)
	case *ast.DropTrigger:
		return e.executeDropTrigger(ctx, s)

	// Job statements
	case *ast.CreateJob:
		return e.executeCreateJob(ctx, s)
	case *ast.DropJob:
		return e.executeDropJob(ctx, s)
	case *ast.AlterJob:
		return e.executeAlterJob(ctx, s)

	// Utility statements
	case *ast.Set:
		return &Result{Tag: constants.ResultOK}, nil

	default:
		return nil, fmt.Errorf("unsupported statement type: %T", stmt)
	}
}

// SetTriggersEnabled enables or disables trigger execution
func (e *Executor) SetTriggersEnabled(enabled bool) {
	e.triggersEnabled = enabled
}
