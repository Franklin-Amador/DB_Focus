# AGENTS.md - FocusDB Coding Agent Guidelines

This document provides guidelines for AI coding agents working in the FocusDB codebase.

## Project Overview

FocusDB is a SQL database engine written in Go with PostgreSQL Wire Protocol compatibility (psql-wire).
- **Module name:** `dbf`
- **Go version:** 1.22+ (tested with 1.25.2)
- **Storage backend:** Pebble (CockroachDB's embedded key-value store with WAL)
- **Wire protocol:** PostgreSQL-compatible (works with psql, pgAdmin, DBeaver)

## Directory Structure

```
DB_F/
├── cmd/                    # Application entry points
│   ├── focusd/            # Main database server (main.go, handler.go)
│   └── test-*/            # Integration test programs
├── internal/               # Private packages
│   ├── ast/               # Abstract Syntax Tree definitions
│   ├── catalog/           # Table/schema metadata management
│   ├── constants/         # Shared constants
│   ├── executor/          # SQL execution engine
│   ├── parser/            # SQL lexer and parser
│   ├── queryutil/         # Query helper utilities
│   ├── server/            # PostgreSQL wire protocol server
│   ├── storage/           # Persistence layer (Pebble backend)
│   └── validator/         # SQL statement validation
├── data/                   # Runtime data directory (Pebble DB)
└── go.mod / go.sum        # Go module files
```

## Build/Lint/Test Commands

### Build
```bash
go build ./cmd/focusd                    # Build server
go build -o focusd ./cmd/focusd          # Build with custom binary name
```

### Run
```bash
go run ./cmd/focusd                      # Run database server
```

### Test Commands
```bash
go test ./...                            # Run all tests
go test -v ./...                         # Run all tests with verbose output
go test ./internal/parser                # Test a specific package
go test -v ./internal/executor           # Test package with verbose output
```

### Running a Single Test
```bash
go test -run TestFunctionName ./internal/package
go test -v -run TestMultipleStatements ./internal/parser
go test -v -run TestSimpleSelect ./internal/parser
go test -run "TestPrefix.*" ./internal/executor  # Pattern matching
```

### Code Quality
```bash
go fmt ./...                             # Format all code
go vet ./...                             # Run static analysis
```

## Code Style Guidelines

### Import Organization
Group imports in this order with blank lines between groups:
1. Standard library
2. External dependencies
3. Internal project packages

```go
import (
    "context"
    "fmt"
    "strings"

    "github.com/cockroachdb/pebble"

    "dbf/internal/ast"
    "dbf/internal/catalog"
)
```

### Naming Conventions

| Element | Convention | Example |
|---------|------------|---------|
| Packages | lowercase, single word | `parser`, `executor`, `catalog` |
| Files | snake_case | `executor_select.go`, `parser_test.go` |
| Exported types | PascalCase | `Parser`, `Executor`, `Table` |
| Exported functions | PascalCase | `ParseSQL`, `Execute` |
| Private functions | camelCase | `parseExpression`, `handleError` |
| Constants | PascalCase | `MaxConnections`, `DefaultPort` |
| Interfaces | PascalCase, often -er suffix | `Statement`, `Backend`, `QueryHandler` |

### Interface Design
- Use marker methods for interface implementation:
```go
type Statement interface {
    stmtNode()  // Marker method
}
```
- Keep interfaces small and focused
- Define interfaces where they are consumed, not where implemented

### Type Patterns
- Use `context.Context` as first parameter for cancellable operations
- Use `sync.RWMutex` for thread-safe data structures
- Return concrete types, accept interfaces
- Use pointer receivers for methods that modify state

### Error Handling
- Always check and handle errors explicitly
- Wrap errors with context using `fmt.Errorf("operation failed: %w", err)`
- Check context cancellation at function entry points:
```go
func (e *Executor) Execute(ctx context.Context, stmt ast.Statement) (*Result, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    // ... implementation
}
```
- Validate inputs before processing
- Return early on errors

### Documentation
- Add doc comments to all exported types, functions, and methods
- Explain "why" not "what" in implementation comments
- Use complete sentences in doc comments

```go
// ParseSQL parses a SQL string and returns the AST representation.
// It supports multiple statements separated by semicolons.
func ParseSQL(input string) ([]ast.Statement, error) {
```

### Testing
- Test files go in same package: `parser_test.go`
- Use `t.Fatalf()` for fatal errors, `t.Errorf()` for non-fatal
- Name tests descriptively: `TestParseSelectWithJoin`
- Integration tests go in `cmd/test-*` directories

```go
func TestParseSimpleSelect(t *testing.T) {
    input := "SELECT * FROM users"
    stmts, err := ParseSQL(input)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(stmts) != 1 {
        t.Errorf("expected 1 statement, got %d", len(stmts))
    }
}
```

### Concurrency
- Use `sync.RWMutex` for read-heavy shared state
- Lock for shortest duration possible
- Use defer for unlock when appropriate:
```go
func (c *Catalog) GetTable(name string) *Table {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.tables[name]
}
```

### Project-Specific Patterns

#### AST Nodes
All statement types implement the `Statement` interface with a marker method:
```go
type SelectStatement struct { /* fields */ }
func (s *SelectStatement) stmtNode() {}
```

#### Executor Pattern
SQL execution follows: Parse -> Validate -> Execute
```go
stmts, err := parser.ParseSQL(sql)
if err != nil { return err }
if err := validator.Validate(stmt); err != nil { return err }
result, err := executor.Execute(ctx, stmt)
```

#### Storage Keys
Pebble storage uses prefixed keys for different data types.
Check `internal/constants/` for key prefix conventions.

## Common Tasks

### Adding a New SQL Statement Type
1. Define AST node in `internal/ast/`
2. Add parser support in `internal/parser/`
3. Add validation in `internal/validator/`
4. Implement execution in `internal/executor/`
5. Add tests at each layer

### Adding a New Feature
1. Understand existing patterns in related code
2. Write tests first when possible
3. Follow the layered architecture (parser -> validator -> executor)
4. Ensure thread safety for shared state

## Work Log - Feature Integration Backlog

### Session: March 2, 2026 - Integration Pass

**Objective**: Integrate orphaned features (parsed but not fully implemented/persisted).

**Status**: ?? IN PROGRESS

**Gaps Identified** (from codebase audit):
1. ? **DROP SCHEMA** - Parsed but no executor/catalog handler
2. ? **Trigger Persistence** - Created/executed in memory, lost on restart
3. ? **Jobs Persistence** - Scheduled but never saved to disk

**Implementation Plan** (priority order):
1. **HIGH**: Drop Schema - Add case to executor, method to catalog/storage
2. **MEDIUM**: Trigger Persistence - Add SaveTrigger/LoadTrigger following procedure pattern
3. **MEDIUM**: Jobs Persistence - Add SaveJob/LoadJob following procedure pattern

**Code Patterns to Follow**:
- Procedure persistence: internal/executor/executor_procedure.go
- Storage pattern: internal/storage/pebble_storage.go (gob encoding)
- Catalog delete: catalog.DropProcedure() in procedures.go

**Implementation Status Updated**:
- [x] 1. DROP SCHEMA executor handler (completed)
- [x] 2. DropSchema() in catalog.go (completed)
- [x] 3. DeleteSchema() in storage/pebble_storage.go (completed)
- [x] 4. Trigger SaveTrigger/DeleteTrigger methods (completed)
- [x] 5. Jobs SaveJob/DeleteJob methods (completed)
- [x] 6. Register gob types for triggers/jobs (already in registerGobTypes)
- [x] 7. Update LoadAll() to reload triggers/jobs on startup (completed)
- [ ] 8. Integration tests for persistence (can be done parallel)

**Completed Changes**:
1. **executor.go**: Added case for *ast.DropSchema dispatch
2. **executor_ddl.go**: Implemented executeDropSchema() with catalog + storage cleanup
3. **catalog_tables.go**: Added DropSchema() method
4. **storage.go**: Extended Backend interface with DeleteSchema, SaveTrigger, DeleteTrigger, SaveJob, DeleteJob
5. **pebble_storage.go**: 
   - Defined TriggerData{} and JobData{} types for gob serialization
   - Implemented SaveTrigger/DeleteTrigger with trig: key prefix
   - Implemented SaveJob/DeleteJob with job: key prefix
   - Updated LoadAll() to iterate and load triggers/jobs from pebble
6. **executor_trigger.go**: Updated executeCreateTrigger/executeDropTrigger to call storage
7. **executor_job.go**: Updated executeCreateJob/executeDropJob to call storage
8. **catalog/triggers.go**: Added LoadTrigger() for restart rehydration
9. **catalog/jobs.go**: Added LoadJob() for restart rehydration

**Result**: All three features now have complete persistence:
- DROP SCHEMA ? works and cleans up persisted data
- Triggers ? persisted and reloaded on restart
- Jobs ? persisted and reloaded on restart  
- All gob-encoded in Pebble with proper key prefixes

**Technical Details**:
- Trigger keys: trig:<name>
- Job keys: job:<name>  
- Schema keys: existing metadata.json approach
- All use gob for binary serialization in Pebble
- LoadAll() handles graceful decode failures with logging

**Status**: ?? COMPLETED - Ready for testing

---

### Bug Fix During Implementation

During persistence testing, discovered a parsing bug in parseCreateJob():
- **Issue**: Job unit tokens (MINUTE, HOUR, DAY) are defined as TokenMinute/TokenHour/TokenDay but parser only checked for TokenIdent
- **Location**: internal/parser/parser.go line 995
- **Fix**: Added switch to handle TokenMinute, TokenHour, TokenDay in addition to TokenIdent
- **Result**: JOB INTERVAL syntax now correctly parses:
  \CREATE JOB name INTERVAL 5 UNIT MINUTE BEGIN ... END;\

### Integration Test Results

All persistence tests **PASS**:
- ? DROP SCHEMA creates, persists, and destroys schema + tables
- ? Triggers created, persisted to disk, and reloaded on startup with correct timing/event
- ? Jobs created, persisted to disk, and reloaded on startup with enabled status preserved

**Test Output Summary**:
\\\
Phase 1: Creating schema, table, trigger, and job...
  ? Created schema: test_schema
  ? Created table: test_schema.test_table
  ? Created trigger: test_trigger
  ? Created job: test_job

Phase 2: Reopening storage and verifying persistence...
  ? Schema persisted: test_schema
  ? Table persisted: test_schema.test_table
  ? Trigger persisted: test_trigger
  ? Job persisted: test_job

Phase 3: Testing DROP SCHEMA...
  ? Dropped schema: test_schema
  ? Schema removed from catalog

=== All tests passed! ===
\\\

### Known Limitations (Not Yet Addressed)

1. **Trigger OLD/NEW references**: Trigger bodies can't access OLD/NEW row values (requires context injection)
2. **Jobs execution history**: No logging/audit trail of job execution times and results
3. **DROP TABLE/SCHEMA CASCADE/RESTRICT**: Only basic DROP implemented, no modifiers for cascade behavior
4. **DROP IF EXISTS**: Not yet implemented for idempotent drops (always errors if not found)

**All scheduled work items COMPLETED. System is production-ready for basic DDL operations with full persistence.**

---

### Session: March 4, 2026 - ALTER Command Implementation

**Objective**: Implement comprehensive ALTER functionality for TABLE and JOB statements.

**Status**: ✅ COMPLETED

**Features Implemented**:
1. ✅ **ALTER TABLE ADD COLUMN** - Parse, execute, persist new columns
2. ✅ **ALTER TABLE DROP COLUMN** - Parse, execute, remove column + data
3. ✅ **ALTER TABLE ALTER COLUMN TYPE** - Parse, execute, change column type
4. ✅ **ALTER TABLE RENAME COLUMN** - Parse, execute, rename column + data
5. ✅ **ALTER JOB ENABLE/DISABLE** - Parse, execute, persist job state changes

**Code Changes**:
- **internal/ast/ast.go**: Added AlterTable, AlterAction interface, AddColumn, DropColumn, AlterColumn, RenameColumn structs
- **internal/parser/token.go**: Added TokenAdd, TokenColumn, TokenRename, TokenDataType, TokenTo
- **internal/parser/parser.go**: Implemented parseAlterTable() with 4 sub-parsers + parseAlterJob() for ENABLE/DISABLE
- **internal/executor/executor.go**: Added *ast.AlterTable dispatch case
- **internal/executor/executor_ddl.go**: Implemented executeAlterTable() + 4 action executors with persistence
- **internal/executor/executor_job.go**: Modified executeAlterJob() to persist state via storage.SaveJob()
- **internal/catalog/catalog_tables.go**: Added AddColumn(), DropColumn(), AlterColumnType(), RenameColumn() methods
- **internal/storage/storage.go**: Extended Backend interface with DropColumnData(), RenameColumnData()
- **internal/storage/pebble_storage.go**: Implemented column data persistence methods
- **cmd/test-alter/main.go**: Created comprehensive 6-phase integration test
- **README.md**: Updated with ALTER TABLE and ALTER JOB syntax documentation

**Bug Fixes During Implementation**:
1. **Token naming conflict**: Renamed TokenType → TokenDataType to avoid Go type system collision
2. **Column mutation bug**: Changed AlterColumnType/RenameColumn to use index-based iteration (for i := range) instead of value iteration (for _, col := range) to properly modify Column structs in-place
3. **Unused variable**: Removed unused `enabled` variable after changing CreateJob default to Enabled: true
4. **Test persistence**: Added LoadAll() call in test to properly reload catalog from storage

**Test Results**: All tests passing (exit code 0)
- ✅ CREATE TABLE + ADD COLUMN with persistence
- ✅ RENAME COLUMN with data updates
- ✅ ALTER COLUMN TYPE with schema updates
- ✅ DROP COLUMN with data cleanup
- ✅ CREATE JOB (defaults to enabled=true)
- ✅ ALTER JOB ENABLE/DISABLE with persistence
- ✅ Full reload from storage preserves all changes

**Technical Implementation Notes**:
- Column operations modify catalog in-memory then persist via storage.SaveTableWithSchema()
- DROP COLUMN removes column from schema AND deletes data via storage.DropColumnData()
- RENAME COLUMN updates schema AND data keys via storage.RenameColumnData()
- ALTER COLUMN TYPE only updates schema (no data migration)
- ALTER JOB state changes now persist via storage.SaveJob()
- All changes survive restart (verified via LoadAll() test)

**Known Design Decisions**:
- ALTER COLUMN TYPE does not validate/migrate existing data - assumes user responsibility
- DROP/RENAME operations work on column data using Pebble key iteration
- CREATE JOB now defaults to enabled=true (changed from false)
- No CASCADE/RESTRICT support yet for DROP operations

**Status**: ✅ COMPLETED - Full ALTER support with persistence for TABLE and JOB operations

---

### Session: March 4, 2026 - ALTER Constraint Validation

**Objective**: Add constraint validation to ALTER TABLE operations.

**Status**: ✅ COMPLETED

**Features Implemented**:
1. ✅ **PRIMARY KEY validation** - Prevent adding duplicate PRIMARY KEY to table
2. ✅ **FOREIGN KEY validation** - Prevent dropping columns referenced by foreign keys
3. ✅ **PRIMARY KEY drop protection** - Prevent dropping PRIMARY KEY columns

**Code Changes**:
- **internal/catalog/catalog_tables.go**: 
  - Added `AddColumnWithConstraint()` method - validates no duplicate PRIMARY KEY exists
  - Added `checkColumnForeignKeyReferences()` helper - checks if column is FK-referenced
  - Modified `DropColumn()` to validate PRIMARY KEY and FK constraints before dropping
- **internal/executor/executor_ddl.go**: 
  - Modified `executeAddColumn()` to detect PRIMARY KEY constraint in AST and call `AddColumnWithConstraint()`
- **cmd/test-alter-constraints/main.go**: Created comprehensive validation test suite

**Validations Implemented**:
1. **ADD COLUMN with PRIMARY KEY**: Rejects if table already has a PRIMARY KEY
   - Example: `ALTER TABLE users ADD COLUMN email TEXT PRIMARY KEY` when `id` is already PK → ERROR
2. **DROP COLUMN with FK reference**: Rejects if other tables reference the column via FOREIGN KEY
   - Example: `ALTER TABLE users DROP COLUMN id` when `orders.user_id` references it → ERROR
3. **DROP COLUMN that is PRIMARY KEY**: Rejects dropping PRIMARY KEY columns
   - Example: `ALTER TABLE orders DROP COLUMN order_id` where `order_id` is PK → ERROR
4. **DROP COLUMN without constraints**: Allows dropping non-constrained columns
   - Example: `ALTER TABLE users DROP COLUMN name` → SUCCESS

**Test Results**: All constraint validation tests passing
- ✅ Correctly rejects duplicate PRIMARY KEY
- ✅ Correctly rejects dropping FK-referenced column
- ✅ Correctly rejects dropping PRIMARY KEY column
- ✅ Successfully drops non-constrained column

**Technical Implementation Notes**:
- `checkColumnForeignKeyReferences()` iterates all tables to find FK constraints pointing to the column
- PRIMARY KEY validation checks both table-level and column-level constraints
- `DropColumn()` performs FK check BEFORE acquiring lock to avoid deadlock
- Constraint validation happens before catalog modification (fail-fast pattern)

**Status**: ✅ COMPLETED - ALTER TABLE operations now properly validate constraints
