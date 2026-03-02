package executor

import (
	"context"
	"fmt"

	"dbf/internal/ast"
	"dbf/internal/constants"
)

// executeCreateProcedure creates a new stored procedure in the catalog.
func (e *Executor) executeCreateProcedure(ctx context.Context, stmt *ast.CreateProcedure) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := e.catalog.CreateProcedure(stmt.Name.Name, stmt.Parameters, stmt.Body); err != nil {
		return nil, fmt.Errorf("failed to create procedure %s: %w", stmt.Name.Name, err)
	}

	if e.storage != nil {
		proc, err := e.catalog.GetProcedure(stmt.Name.Name)
		if err == nil {
			if err := e.storage.SaveProcedure(proc); err != nil {
				fmt.Printf("warning: failed to persist procedure %s: %v\n", stmt.Name.Name, err)
			}
		}
	}

	return &Result{Tag: constants.ResultCreateProcedure}, nil
}

// executeCallProcedure executes a stored procedure with the given arguments.
func (e *Executor) executeCallProcedure(ctx context.Context, stmt *ast.CallProcedure) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	proc, err := e.catalog.GetProcedure(stmt.Name.Name)
	if err != nil {
		return nil, fmt.Errorf("procedure %s not found: %w", stmt.Name.Name, err)
	}

	// Validate argument count
	if len(stmt.Arguments) != len(proc.Parameters) {
		return nil, fmt.Errorf("procedure %s expects %d arguments, got %d",
			stmt.Name.Name, len(proc.Parameters), len(stmt.Arguments))
	}

	// Build parameter substitution map
	paramValues := make(map[string]string)
	for i, param := range proc.Parameters {
		paramValues[param.Name.Name] = stmt.Arguments[i].Value
	}

	// Execute each statement in the procedure body
	var lastResult *Result
	for _, bodyStmt := range proc.Body {
		// Check context cancellation before each statement
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Substitute parameter values in the statement
		substitutedStmt := e.substituteParameters(bodyStmt, paramValues)

		result, err := e.Execute(ctx, substitutedStmt)
		if err != nil {
			return nil, fmt.Errorf("error in procedure %s: %w", stmt.Name.Name, err)
		}
		lastResult = result
	}

	if lastResult == nil {
		return &Result{Tag: constants.ResultCall}, nil
	}
	return lastResult, nil
}

// executeDropProcedure removes a stored procedure from the catalog.
func (e *Executor) executeDropProcedure(ctx context.Context, stmt *ast.DropProcedure) (*Result, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := e.catalog.DropProcedure(stmt.Name.Name); err != nil {
		return nil, fmt.Errorf("failed to drop procedure %s: %w", stmt.Name.Name, err)
	}

	if e.storage != nil {
		if err := e.storage.DeleteProcedure(stmt.Name.Name); err != nil {
			fmt.Printf("warning: failed to delete persisted procedure %s: %v\n", stmt.Name.Name, err)
		}
	}

	return &Result{Tag: constants.ResultDropProcedure}, nil
}

// substituteParameters replaces parameter references in a statement with their actual values.
// Handles INSERT, UPDATE, and DELETE statements.
func (e *Executor) substituteParameters(stmt ast.Statement, paramValues map[string]string) ast.Statement {
	switch s := stmt.(type) {
	case *ast.Insert:
		return e.substituteInsertParameters(s, paramValues)
	case *ast.Update:
		return e.substituteUpdateParameters(s, paramValues)
	case *ast.Delete:
		return e.substituteDeleteParameters(s, paramValues)
	}
	return stmt
}

// substituteInsertParameters substitutes parameters in an INSERT statement.
func (e *Executor) substituteInsertParameters(stmt *ast.Insert, paramValues map[string]string) *ast.Insert {
	newStmt := *stmt
	newValues := make([]ast.Literal, len(stmt.Values))

	for i, val := range stmt.Values {
		// Check if it's an identifier (parameter reference)
		if val.Kind == "identifier" {
			if paramVal, exists := paramValues[val.Value]; exists {
				newValues[i] = inferLiteralType(paramVal)
			} else {
				newValues[i] = val
			}
		} else if paramVal, exists := paramValues[val.Value]; exists {
			newValues[i] = ast.Literal{Kind: val.Kind, Value: paramVal}
		} else {
			newValues[i] = val
		}
	}

	newStmt.Values = newValues
	return &newStmt
}

// substituteUpdateParameters substitutes parameters in an UPDATE statement.
func (e *Executor) substituteUpdateParameters(stmt *ast.Update, paramValues map[string]string) *ast.Update {
	newStmt := *stmt

	// Substitute value
	if stmt.Value.Kind == "identifier" {
		if paramVal, exists := paramValues[stmt.Value.Value]; exists {
			newStmt.Value = inferLiteralType(paramVal)
		}
	} else if paramVal, exists := paramValues[stmt.Value.Value]; exists {
		newStmt.Value = ast.Literal{Kind: stmt.Value.Kind, Value: paramVal}
	}

	// Substitute WHERE clause
	if stmt.Where != nil {
		newWhere := e.substituteWhereClause(stmt.Where, paramValues)
		newStmt.Where = newWhere
	}

	return &newStmt
}

// substituteDeleteParameters substitutes parameters in a DELETE statement.
func (e *Executor) substituteDeleteParameters(stmt *ast.Delete, paramValues map[string]string) *ast.Delete {
	newStmt := *stmt

	// Substitute WHERE clause
	if stmt.Where != nil {
		newWhere := e.substituteWhereClause(stmt.Where, paramValues)
		newStmt.Where = newWhere
	}

	return &newStmt
}

// substituteWhereClause substitutes parameters in a WHERE clause.
func (e *Executor) substituteWhereClause(where *ast.WhereClause, paramValues map[string]string) *ast.WhereClause {
	if where == nil {
		return nil
	}

	newWhere := *where

	// Substitute value in WHERE clause
	if where.Value.Kind == "identifier" {
		if paramVal, exists := paramValues[where.Value.Value]; exists {
			newWhere.Value = inferLiteralType(paramVal)
		}
	} else if paramVal, exists := paramValues[where.Value.Value]; exists {
		newWhere.Value = ast.Literal{Kind: where.Value.Kind, Value: paramVal}
	}

	return &newWhere
}

// inferLiteralType attempts to infer the type of a literal value.
// Returns a literal with the appropriate type (number or string).
func inferLiteralType(value string) ast.Literal {
	// Try to parse as integer
	var intVal int
	if _, err := fmt.Sscanf(value, "%d", &intVal); err == nil {
		return ast.Literal{Kind: "number", Value: value}
	}

	// Default to string
	return ast.Literal{Kind: "string", Value: value}
}
