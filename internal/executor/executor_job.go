package executor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/constants"
)

// executeCreateJob creates a new scheduled job in the catalog.
func (e *Executor) executeCreateJob(ctx context.Context, stmt *ast.CreateJob) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := e.catalog.CreateJob(
		stmt.Name.Name,
		stmt.Interval,
		stmt.Unit,
		stmt.Body,
		stmt.Enabled,
	); err != nil {
		return nil, fmt.Errorf("failed to create job %s: %w", stmt.Name.Name, err)
	}

	// Persist job to storage
	if e.storage != nil {
		if job, err := e.catalog.GetJob(stmt.Name.Name); err == nil {
			if err := e.storage.SaveJob(job); err != nil {
				fmt.Printf("warning: failed to persist job %s: %v\n", stmt.Name.Name, err)
			}
		}
	}

	return &Result{Tag: constants.ResultCreateJob}, nil
}

// executeDropJob removes a scheduled job from the catalog.
func (e *Executor) executeDropJob(ctx context.Context, stmt *ast.DropJob) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := e.catalog.DropJob(stmt.Name.Name); err != nil {
		return nil, fmt.Errorf("failed to drop job %s: %w", stmt.Name.Name, err)
	}

	// Clean up from storage
	if e.storage != nil {
		if err := e.storage.DeleteJob(stmt.Name.Name); err != nil {
			fmt.Printf("warning: failed to delete persisted job %s: %v\n", stmt.Name.Name, err)
		}
	}

	return &Result{Tag: constants.ResultDropJob}, nil
}

// executeAlterJob modifies a scheduled job (enable/disable) and persists the change.
func (e *Executor) executeAlterJob(ctx context.Context, stmt *ast.AlterJob) (*Result, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Modify job state in catalog
	if err := e.catalog.AlterJob(stmt.Name.Name, stmt.Action); err != nil {
		return nil, fmt.Errorf("failed to alter job %s: %w", stmt.Name.Name, err)
	}

	// Get updated job to persist
	job, err := e.catalog.GetJob(stmt.Name.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get updated job: %w", err)
	}

	// Persist change to storage
	if e.storage != nil {
		if err := e.storage.SaveJob(job); err != nil {
			return nil, fmt.Errorf("failed to persist job state: %w", err)
		}
	}

	action := strings.ToLower(stmt.Action)
	return &Result{Tag: fmt.Sprintf("ALTER JOB (%s)", action)}, nil
}

// StartJobScheduler starts a background goroutine that executes scheduled jobs.
// The scheduler checks for jobs to run every minute.
func (e *Executor) StartJobScheduler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// Context cancelled, stop scheduler
				return
			case <-ticker.C:
				e.checkAndExecuteJobs(ctx)
			}
		}
	}()
}

// checkAndExecuteJobs checks all jobs and executes those that are due.
func (e *Executor) checkAndExecuteJobs(ctx context.Context) {
	// Check context cancellation
	if ctx.Err() != nil {
		return
	}

	jobs := e.catalog.GetAllJobs()
	now := time.Now().Unix()

	for _, job := range jobs {
		// Skip disabled jobs
		if !job.Enabled {
			continue
		}

		// Calculate interval in seconds
		intervalSeconds := e.calculateIntervalSeconds(job.Interval, job.Unit)
		if intervalSeconds == 0 {
			continue
		}

		// Check if it's time to run the job (with proper locking)
		job.Mu.Lock()
		shouldRun := job.LastRun == 0 || now-job.LastRun >= intervalSeconds
		if shouldRun {
			// Update LastRun BEFORE executing to prevent duplicate runs
			job.LastRun = now
		}
		job.Mu.Unlock()

		if shouldRun {
			e.executeJob(ctx, job)
		}
	}
}

// calculateIntervalSeconds converts a job interval to seconds.
func (e *Executor) calculateIntervalSeconds(interval int, unit string) int64 {
	switch unit {
	case constants.JobUnitMinute:
		return int64(interval * 60)
	case constants.JobUnitHour:
		return int64(interval * 3600)
	case constants.JobUnitDay:
		return int64(interval * 86400)
	default:
		return 0
	}
}

// executeJob executes all statements in a job's body.
func (e *Executor) executeJob(ctx context.Context, job *catalog.Job) {
	fmt.Printf("[JOB] Executing job %s\n", job.Name)

	// Create a derived context with timeout to prevent jobs from running forever
	jobCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	for _, stmt := range job.Body {
		// Check context before each statement
		if jobCtx.Err() != nil {
			fmt.Printf("[JOB] Job %s cancelled: %v\n", job.Name, jobCtx.Err())
			return
		}

		if _, err := e.Execute(jobCtx, stmt); err != nil {
			fmt.Printf("[JOB] Job %s failed: %v\n", job.Name, err)
			return
		}
	}

	fmt.Printf("[JOB] Job %s completed successfully\n", job.Name)
}
