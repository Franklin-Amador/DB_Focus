package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/parser"
	"dbf/internal/storage"
)

func main() {
	testDir := "./data_job_test"
	defer os.RemoveAll(testDir)

	fmt.Println("=== Job Persistence Test ===\n")

	// Phase 1: Create job
	fmt.Println("Phase 1: Creating job...")
	cat1 := catalog.New()
	st1, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}
	exe1 := executor.New(cat1, st1)

	ctx := context.Background()

	jobSQL := "CREATE JOB test_job INTERVAL 5 UNIT MINUTE BEGIN SELECT 1; END;"
	p := parser.NewParser(jobSQL)
	stmt, err := p.ParseStatement()
	if err != nil {
		log.Fatalf("failed to parse: %v", err)
	}
	fmt.Printf("Parsed statement type: %T\n", stmt)

	if _, err := exe1.Execute(ctx, stmt); err != nil {
		log.Fatalf("failed to create job: %v", err)
	}
	fmt.Println("✓ Created job: test_job")

	// Verify job in memory
	if job, err := cat1.GetJob("test_job"); err == nil && job != nil {
		fmt.Printf("✓ Job in memory: %s (enabled=%v)\n", job.Name, job.Enabled)
	} else {
		fmt.Printf("✗ Job NOT in memory: %v\n", err)
	}

	st1.Close()

	// Phase 2: Reopen storage and verify persistence
	fmt.Println("\nPhase 2: Reopening storage and verifying persistence...")
	cat2 := catalog.New()
	st2, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("failed to reopen storage: %v", err)
	}

	if err := st2.LoadAll(cat2); err != nil {
		log.Fatalf("failed to load all: %v", err)
	}

	// Verify job exists
	if job, err := cat2.GetJob("test_job"); err == nil && job != nil {
		fmt.Printf("✓ Job persisted: %s (enabled=%v)\n", job.Name, job.Enabled)
	} else {
		fmt.Printf("✗ Job NOT persisted: %v\n", err)
	}

	st2.Close()
}
