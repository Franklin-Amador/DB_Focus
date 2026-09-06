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
	triggerDepth    int
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

// Execute dispatches statement execution to the handler registered for the
// statement's concrete AST type. Handlers register themselves via registerExec
// in the init() blocks of the executor_*.go files (see executor_registry.go),
// so new statement types do not require editing this function.
func (e *Executor) Execute(ctx context.Context, stmt ast.Statement) (*Result, error) {
	if stmt == nil {
		return &Result{Tag: constants.ResultOK}, nil
	}

	// Check context cancellation
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	if res, err, ok := e.dispatch(ctx, stmt); ok {
		return res, err
	}
	return nil, fmt.Errorf("unsupported statement type: %T", stmt)
}

func init() {
	registerExec((*Executor).executeSet)
}

// executeSet handles SET statements. They are accepted and ignored (the parser
// currently discards their payload); returning OK keeps client sessions happy.
func (e *Executor) executeSet(ctx context.Context, stmt *ast.Set) (*Result, error) {
	return &Result{Tag: constants.ResultOK}, nil
}

// Catalog returns the catalog (database) this executor runs against.
func (e *Executor) Catalog() *catalog.Catalog {
	return e.catalog
}

// SetTriggersEnabled enables or disables trigger execution
func (e *Executor) SetTriggersEnabled(enabled bool) {
	e.triggersEnabled = enabled
}
