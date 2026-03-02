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
	testDir := "./data_integration_test"
	defer os.RemoveAll(testDir)

	fmt.Println("=== Persistence Integration Test ===\n")

	// Phase 1: Create schema, table, trigger, job
	fmt.Println("Phase 1: Creating schema, table, trigger, and job...")
	cat1 := catalog.New()
	st1, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}
	exe1 := executor.New(cat1, st1)

	ctx := context.Background()

	// CREATE SCHEMA
	createSchemaSQL := "CREATE SCHEMA test_schema;"
	p := parser.NewParser(createSchemaSQL)
	stmt, _ := p.ParseStatement()
	if _, err := exe1.Execute(ctx, stmt); err != nil {
		log.Fatalf("failed to create schema: %v", err)
	}
	fmt.Println("✓ Created schema: test_schema")

	// CREATE TABLE
	createTableSQL := "CREATE TABLE test_schema.test_table (id INTEGER PRIMARY KEY, name TEXT);"
	p = parser.NewParser(createTableSQL)
	stmt, _ = p.ParseStatement()
	if _, err := exe1.Execute(ctx, stmt); err != nil {
		log.Fatalf("failed to create table: %v", err)
	}
	fmt.Println("✓ Created table: test_schema.test_table")

	// CREATE TRIGGER
	triggerSQL := `CREATE TRIGGER test_trigger
		BEFORE INSERT ON test_schema.test_table
		FOR EACH ROW
		BEGIN
			INSERT INTO test_schema.test_table VALUES (999, 'trigger_auto');
		END;`
	p = parser.NewParser(triggerSQL)
	stmt, _ = p.ParseStatement()
	if _, err := exe1.Execute(ctx, stmt); err != nil {
		log.Fatalf("failed to create trigger: %v", err)
	}
	fmt.Println("✓ Created trigger: test_trigger")

	// CREATE JOB
	jobSQL := `CREATE JOB test_job
		INTERVAL 5 UNIT MINUTE
		BEGIN
			INSERT INTO test_schema.test_table VALUES (888, 'job_auto');
		END;`
	p = parser.NewParser(jobSQL)
	stmt, _ = p.ParseStatement()
	if _, err := exe1.Execute(ctx, stmt); err != nil {
		log.Fatalf("failed to create job: %v", err)
	}
	fmt.Println("✓ Created job: test_job\n")

	st1.Close()

	// Phase 2: Reopen storage and verify persistence
	fmt.Println("Phase 2: Reopening storage and verifying persistence...")
	cat2 := catalog.New()
	st2, err := storage.NewPebbleStorage(testDir)
	if err != nil {
		log.Fatalf("failed to reopen storage: %v", err)
	}
	exe2 := executor.New(cat2, st2)

	if err := st2.LoadAll(cat2); err != nil {
		log.Fatalf("failed to load all: %v", err)
	}

	// Verify schema exists
	if table, _ := cat2.GetTable("test_table", "test_schema"); table != nil {
		fmt.Println("✓ Schema persisted: test_schema")
	} else {
		fmt.Println("✗ Schema NOT persisted")
	}

	// Verify table exists
	if table, _ := cat2.GetTable("test_table", "test_schema"); table != nil {
		fmt.Println("✓ Table persisted: test_schema.test_table")
	} else {
		fmt.Println("✗ Table NOT persisted")
	}

	// Verify trigger exists
	triggers := cat2.GetTriggers("test_schema.test_table", "BEFORE", "INSERT")
	if len(triggers) > 0 {
		fmt.Println("✓ Trigger persisted: test_trigger")
	} else {
		fmt.Println("✗ Trigger NOT persisted")
	}

	// Verify job exists
	if job, _ := cat2.GetJob("test_job"); job != nil {
		fmt.Println("✓ Job persisted: test_job")
	} else {
		fmt.Println("✗ Job NOT persisted")
	}

	// Phase 3: Test DROP SCHEMA
	fmt.Println("\nPhase 3: Testing DROP SCHEMA...")
	dropSchemaSQL := "DROP SCHEMA test_schema;"
	p = parser.NewParser(dropSchemaSQL)
	stmt, _ = p.ParseStatement()
	if _, err := exe2.Execute(ctx, stmt); err != nil {
		log.Fatalf("failed to drop schema: %v", err)
	}
	fmt.Println("✓ Dropped schema: test_schema")

	// Verify schema is gone
	if table, _ := cat2.GetTable("test_table", "test_schema"); table == nil {
		fmt.Println("✓ Schema removed from catalog")
	} else {
		fmt.Println("✗ Schema still in catalog")
	}

	st2.Close()

	fmt.Println("\n=== All tests passed! ===")
}
