package executor

import (
	"context"
	"fmt"

	"dbf/internal/ast"
	"dbf/internal/constants"
)

// executeCreateTrigger creates a new trigger in the catalog.
func (e *Executor) executeCreateTrigger(ctx context.Context, stmt *ast.CreateTrigger) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := e.catalog.CreateTrigger(
		stmt.Name.Name,
		stmt.Timing,
		stmt.Event,
		stmt.Table.Name,
		stmt.ForEachRow,
		stmt.Body,
	); err != nil {
		return nil, fmt.Errorf("failed to create trigger %s: %w", stmt.Name.Name, err)
	}

	return &Result{Tag: constants.ResultCreateTrigger}, nil
}

// executeDropTrigger removes a trigger from the catalog.
func (e *Executor) executeDropTrigger(ctx context.Context, stmt *ast.DropTrigger) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := e.catalog.DropTrigger(stmt.Name.Name, stmt.Table.Name); err != nil {
		return nil, fmt.Errorf("failed to drop trigger %s: %w", stmt.Name.Name, err)
	}

	return &Result{Tag: constants.ResultDropTrigger}, nil
}

// executeTriggers executes all triggers matching the given table, timing, and event.
// This function prevents recursive trigger execution by temporarily disabling triggers.
//
// Parameters:
//   - table: table name
//   - timing: BEFORE, AFTER, or INSTEAD OF
//   - event: INSERT, UPDATE, or DELETE
//   - oldRow: the row before the operation (for UPDATE/DELETE)
//   - newRow: the row after the operation (for INSERT/UPDATE)
//
// Returns an error if any trigger execution fails.
func (e *Executor) executeTriggers(ctx context.Context, table, timing, event string, oldRow, newRow []interface{}) error {
	// Prevent recursive trigger execution
	if !e.triggersEnabled {
		return nil
	}

	// Check context cancellation
	if ctx.Err() != nil {
		return ctx.Err()
	}

	triggers := e.catalog.GetTriggers(table, timing, event)
	for _, trigger := range triggers {
		// Disable triggers while executing trigger body to prevent recursion
		e.triggersEnabled = false

		// Execute each statement in the trigger body
		for _, stmt := range trigger.Body {
			// Check context before each statement
			if ctx.Err() != nil {
				e.triggersEnabled = true
				return ctx.Err()
			}

			// TODO: Support OLD and NEW row references
			// For now, execute statements as-is
			if _, err := e.Execute(ctx, stmt); err != nil {
				e.triggersEnabled = true
				return fmt.Errorf("trigger %s failed: %w", trigger.Name, err)
			}
		}

		// Re-enable triggers after trigger body completes
		e.triggersEnabled = true
	}

	return nil
}
