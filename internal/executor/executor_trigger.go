package executor

import (
	"context"
	"fmt"

	"dbf/internal/ast"
	"dbf/internal/constants"
)

const maxTriggerRecursionDepth = 16

func init() {
	registerExec((*Executor).executeCreateTrigger)
	registerExec((*Executor).executeDropTrigger)
}

// executeCreateTrigger creates a new trigger in the catalog.
func (e *Executor) executeCreateTrigger(ctx context.Context, stmt *ast.CreateTrigger) (*Result, error) {
	// Check context cancellation
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	if err := e.catalog.CreateTrigger(
		stmt.Name.Name,
		stmt.Timing,
		stmt.Event,
		stmt.Table.Name,
		stmt.ForEachRow,
		stmt.Body,
		stmt.BodyText,
	); err != nil {
		return nil, fmt.Errorf("failed to create trigger %s: %w", stmt.Name.Name, err)
	}

	// Persist trigger to storage
	if e.storage != nil {
		// Get trigger from catalog to persist
		if triggers := e.catalog.GetTriggers(stmt.Table.Name, stmt.Timing, stmt.Event); len(triggers) > 0 {
			if err := e.storage.SaveTrigger(triggers[len(triggers)-1]); err != nil {
				fmt.Printf("warning: failed to persist trigger %s: %v\n", stmt.Name.Name, err)
			}
		}
	}

	return &Result{Tag: constants.ResultCreateTrigger}, nil
}

// executeDropTrigger removes a trigger from the catalog.
func (e *Executor) executeDropTrigger(ctx context.Context, stmt *ast.DropTrigger) (*Result, error) {
	// Check context cancellation
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	if err := e.catalog.DropTrigger(stmt.Name.Name, stmt.Table.Name); err != nil {
		return nil, fmt.Errorf("failed to drop trigger %s: %w", stmt.Name.Name, err)
	}

	// Clean up from storage
	if e.storage != nil {
		if err := e.storage.DeleteTrigger(stmt.Name.Name); err != nil {
			fmt.Printf("warning: failed to delete persisted trigger %s: %v\n", stmt.Name.Name, err)
		}
	}

	return &Result{Tag: constants.ResultDropTrigger}, nil
}

// executeTriggers executes all triggers matching the given table, timing, and event.
// Recursive trigger chains are supported with a bounded depth to avoid infinite loops.
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
	if !e.triggersEnabled {
		return nil
	}

	if e.triggerDepth >= maxTriggerRecursionDepth {
		return fmt.Errorf("trigger recursion depth exceeded (%d) while executing %s %s on %s", maxTriggerRecursionDepth, timing, event, table)
	}

	// Check context cancellation
	if err := checkCtx(ctx); err != nil {
		return err
	}

	e.triggerDepth++
	defer func() {
		e.triggerDepth--
	}()

	triggers := e.catalog.GetTriggers(table, timing, event)
	for _, trigger := range triggers {
		// Execute each statement in the trigger body
		for _, stmt := range trigger.Body {
			// Check context before each statement
			if err := checkCtx(ctx); err != nil {
				return err
			}

			// TODO: Support OLD and NEW row references
			// For now, execute statements as-is
			if _, err := e.Execute(ctx, stmt); err != nil {
				return fmt.Errorf("trigger %s failed at depth %d: %w", trigger.Name, e.triggerDepth, err)
			}
		}
	}

	return nil
}
