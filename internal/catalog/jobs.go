package catalog

import (
	"dbf/internal/ast"
	"dbf/internal/constants"
	"fmt"
	"sync/atomic"
)

// jobOIDCounter generates unique OIDs for jobs
var jobOIDCounter int64 = 49152 // Start above trigger OIDs

func (c *Catalog) CreateJob(name string, interval int, unit string, body []ast.Statement, enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.jobs[name]; exists {
		return fmt.Errorf("job %s already exists", name)
	}

	c.jobs[name] = &Job{
		Name:     name,
		Interval: interval,
		Unit:     unit,
		Body:     body,
		Enabled:  enabled,
		LastRun:  0,
	}

	// Register in pg_catalog.pg_job
	if err := c.registerJobInCatalog(name, interval, unit, enabled); err != nil {
		// Log but don't fail - job is still created in memory
		fmt.Printf("warning: failed to register job in catalog: %v\n", err)
	}

	return nil
}

// registerJobInCatalog adds job to pg_catalog.pg_job
// Caller must hold the write lock on c.mu
func (c *Catalog) registerJobInCatalog(name string, interval int, unit string, enabled bool) error {
	jobTable, err := c.getTableUnlocked(constants.CatalogJob)
	if err != nil {
		return err
	}

	oid := atomic.AddInt64(&jobOIDCounter, 1)

	// oid, jobname, jobinterval, jobunit, jobenabled, joblastrun, jobowner
	row := []interface{}{
		int(oid),
		name,
		interval,
		unit,
		enabled,
		"", // never run yet
		constants.DefaultOwner,
	}

	return jobTable.InsertRowUnsafe(row)
}

func (c *Catalog) GetJob(name string) (*Job, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	job, exists := c.jobs[name]
	if !exists {
		return nil, fmt.Errorf("job %s not found", name)
	}

	return job, nil
}

func (c *Catalog) GetAllJobs() []*Job {
	c.mu.RLock()
	defer c.mu.RUnlock()

	jobs := make([]*Job, 0, len(c.jobs))
	for _, job := range c.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (c *Catalog) DropJob(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.jobs[name]; !exists {
		return fmt.Errorf("job %s not found", name)
	}

	delete(c.jobs, name)
	return nil
}

func (c *Catalog) AlterJob(name, action string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	job, exists := c.jobs[name]
	if !exists {
		return fmt.Errorf("job %s not found", name)
	}

	switch action {
	case constants.JobActionEnable:
		job.Enabled = true
	case constants.JobActionDisable:
		job.Enabled = false
	default:
		return fmt.Errorf("invalid job action: %s", action)
	}

	return nil
}

// LoadJob loads a persisted job into the catalog (used on restart).
// Unlike CreateJob, this does not re-register in pg_catalog.
func (c *Catalog) LoadJob(name string, interval int, unit string, body []ast.Statement, enabled bool) error {
c.mu.Lock()
defer c.mu.Unlock()

if _, exists := c.jobs[name]; exists {
return nil // Already loaded
}

c.jobs[name] = &Job{
Name:     name,
Interval: interval,
Unit:     unit,
Body:     body,
Enabled:  enabled,
LastRun:  0,
}
return nil
}
