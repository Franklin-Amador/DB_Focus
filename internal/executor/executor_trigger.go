package executor

import (
	"context"
	"fmt"
	"strings"

	"dbf/internal/ast"
	"dbf/internal/catalog"
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

	trig := &catalog.Trigger{
		Name:          stmt.Name.Name,
		Timing:        stmt.Timing,
		Event:         stmt.Event,
		Events:        stmt.Events,
		Table:         stmt.Table.Name,
		ForEachRow:    stmt.ForEachRow,
		UpdateColumns: stmt.UpdateColumns,
		ExecRoutine:   stmt.ExecRoutine.Name,
		ExecSchema:    stmt.ExecRoutine.Alias,
		Body:          stmt.Body,
		BodyText:      stmt.BodyText,
	}
	if err := e.catalog.CreateTrigger(trig); err != nil {
		return nil, fmt.Errorf("failed to create trigger %s: %w", stmt.Name.Name, err)
	}

	// Persist trigger to storage
	if e.storage != nil {
		if err := e.storage.SaveTrigger(trig); err != nil {
			fmt.Printf("warning: failed to persist trigger %s: %v\n", stmt.Name.Name, err)
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
	if len(triggers) == 0 {
		return nil
	}

	// Bind NEW.<col> / OLD.<col> to the affected row's values so a trigger body
	// can reference them, e.g. INSERT INTO audit VALUES (NEW.id, NEW.nombre).
	// NEW is the post-image (INSERT/UPDATE), OLD the pre-image (UPDATE/DELETE).
	rowRefs := e.triggerRowRefs(table, oldRow, newRow)

	for _, trigger := range triggers {
		// Resolve the statements to run: an inline BEGIN...END body, or the body
		// of the routine named by a PostgreSQL `EXECUTE FUNCTION/PROCEDURE fn()`
		// trigger. A referenced routine that is not a runnable procedure (e.g. a
		// trigger function, not yet supported) leaves the trigger inert.
		body := trigger.Body
		if trigger.ExecRoutine != "" {
			proc, err := e.catalog.GetProcedure(trigger.ExecRoutine)
			if err != nil {
				continue // referenced routine not runnable yet — inert, not an error
			}
			body = proc.Body
		}

		// Execute each statement in the trigger body
		for _, stmt := range body {
			// Check context before each statement
			if err := checkCtx(ctx); err != nil {
				return err
			}

			bound := stmt
			if len(rowRefs) > 0 {
				bound = e.substituteParameters(stmt, rowRefs)
			}
			if _, err := e.Execute(ctx, bound); err != nil {
				return fmt.Errorf("trigger %s failed at depth %d: %w", trigger.Name, e.triggerDepth, err)
			}
		}
	}

	return nil
}

// triggerRowRefs builds the NEW.<col>/OLD.<col> substitution map for a trigger
// body from the affected row(s). Keys are generated for the common case
// spellings (NEW/new prefix; column name as defined, lower, and upper) so a
// body referencing NEW.nombre resolves regardless of case. A nil row (e.g. OLD
// on INSERT, NEW on DELETE) contributes no keys.
func (e *Executor) triggerRowRefs(table string, oldRow, newRow []interface{}) map[string]string {
	tbl, err := e.catalog.GetTable(table)
	if err != nil {
		return nil
	}
	refs := make(map[string]string)
	bind := func(prefix string, row []interface{}) {
		if row == nil {
			return
		}
		for i, col := range tbl.Columns {
			if i >= len(row) {
				break
			}
			val := ""
			if row[i] != nil {
				val = fmt.Sprintf("%v", row[i])
			}
			for _, pfx := range []string{strings.ToUpper(prefix), strings.ToLower(prefix)} {
				for _, name := range []string{col.Name, strings.ToLower(col.Name), strings.ToUpper(col.Name)} {
					refs[pfx+"."+name] = val
				}
			}
		}
	}
	bind("NEW", newRow)
	bind("OLD", oldRow)
	return refs
}
