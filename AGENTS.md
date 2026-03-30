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

---

### Session: March 29, 2026 - Trigger Recursion, Self-FK, and Wire Protocol Stability

**Objective**: Add controlled trigger recursion, validate self-referential foreign keys, and fix extended protocol hangs in integration clients.

**Status**: ✅ COMPLETED

**Features Implemented**:
1. ✅ **Controlled trigger recursion** with max depth guard
2. ✅ **Self-referencing foreign key integration test** (`categorias.parent_id -> categorias.id`)
3. ✅ **Extended protocol fix for `SELECT 1`** in test client flow
4. ✅ **System query response synchronization fix** (removed duplicate `ReadyForQuery`)

**Code Changes**:
- **internal/executor/executor.go**:
  - Added `triggerDepth int` field in `Executor`
- **internal/executor/executor_trigger.go**:
  - Added `maxTriggerRecursionDepth = 16`
  - Enabled recursive trigger execution (removed temporary trigger disable during body execution)
  - Added explicit recursion depth error message with context
- **cmd/test-trigger-recursion/main.go**:
  - Added integration scenario to verify recursive execution + recursion guard stop
- **cmd/test-self-fk/main.go**:
  - Added integration scenario for self-FK creation, valid parent-child insert, and orphan rejection
- **cmd/test-client/main.go**:
  - Fixed extended protocol read loop to stop on `CommandComplete/ErrorResponse`
  - Ensured `Sync` is sent before waiting for `ReadyForQuery`
- **internal/server/writers.go**:
  - Removed duplicate `writeReady()` in `writeSystemResult()` to prevent protocol desynchronization
- **cmd/test-information-schema/main.go**:
  - Fixed result-set handling and scanning logic for robust validation of `information_schema`
- **README.md**:
  - Updated behavior notes and added regression command sets

**Regression Results**:
- ✅ `go run ./cmd/test-trigger-recursion`
- ✅ `go run ./cmd/test-self-fk`
- ✅ Retried previously failing client scenarios after server startup:
  - `test-client`, `test-information-schema`, `test-multi-advanced`, `test-multi-client`, `test-persistence`, `test-simple-query`, `test-users`
  - All passed with exit code `0`

**Technical Notes**:
- Trigger recursion is now supported but bounded for safety.
- The current recursion ceiling is a constant (`16`), not yet runtime-configurable.
- Self-FK insertion rules are enforced (orphan insert rejected).
- The protocol fix addressed a real stream-order issue, not only test code behavior.

**Known Remaining Limitations**:
1. `OLD`/`NEW` row bindings in trigger body statements are still TODO.
2. Trigger recursion limit is static (no config flag/env yet).
3. Some `cmd/test-*` programs are stateful against persisted data directories and may require cleanup for fully idempotent reruns.

**Status**: ✅ COMPLETED - System stable for recursive triggers, self-FK insertion validation, and extended protocol query flow

---

### Session: March 29, 2026 - Indexing Support and Persistence

**Objective**: Add basic indexing support to improve equality-filter performance on large tables.

**Status**: ✅ COMPLETED

**Features Implemented**:
1. ✅ **CREATE INDEX** parser/AST/executor support
2. ✅ **Index-aware SELECT WHERE** path for equality filters
3. ✅ **Index consistency maintenance** on INSERT/UPDATE/DELETE and ALTER TABLE rename/drop column
4. ✅ **Index metadata persistence** across Pebble and JSON storage backends
5. ✅ **Index integration test** with reload validation

**Code Changes**:
- **internal/ast/ast.go**: Added `CreateIndex` statement node
- **internal/parser/token.go**: Added `TokenIndex` keyword support
- **internal/parser/parser.go**: Added `parseCreateIndex()` and CREATE dispatcher integration
- **internal/executor/executor.go**: Added `*ast.CreateIndex` dispatch
- **internal/executor/executor_ddl.go**: Implemented `executeCreateIndex()` + persistence call
- **internal/catalog/types.go**: Added `Index` type and `Table.Indexes` map
- **internal/catalog/table.go**:
  - Added index creation/rebuild helpers
  - Added index lookup path in `SelectWhere()`
  - Rebuild indexes after row replacement/deletion paths
- **internal/catalog/catalog_tables.go**:
  - Added `Catalog.CreateIndex()`
  - Fixed `DropColumn()` row-removal index calculation bug
  - Keep index metadata coherent on drop/rename column
- **internal/storage/storage.go** + **internal/storage/pebble_storage.go**:
  - Added index definition serialization/deserialization in `TableData`/`TableSchema`
  - Reload index definitions on startup
  - Keep persisted index metadata coherent in column rename/drop helpers
- **cmd/test-index/main.go**: New end-to-end index behavior + persistence test
- **internal/parser/parser_test.go**: Added `TestCreateIndex`

**Regression Results**:
- ✅ `go test -vet=off ./internal/...`
- ✅ `go test -vet=off ./...`
- ✅ `go run ./cmd/test-index`
- ✅ `go run ./cmd/test-alter`
- ✅ `go run ./cmd/test-persistence-integration`
- ✅ `go run ./cmd/test-trigger-recursion`
- ✅ `go run ./cmd/test-self-fk`

**Technical Notes**:
- Current index implementation is single-column and optimized for equality predicates (`col = value`).
- Index values are rebuilt when row positions can shift (e.g., delete/update bulk effects).
- Persisted index definitions are reconstructed at load time, then populated from rows.

**Known Limits**:
1. No composite indexes yet.
2. No range-scan index optimization (`>`, `<`, `BETWEEN`) yet.
3. No `DROP INDEX` syntax yet.

**Status**: ✅ COMPLETED - Basic index lifecycle is supported with persistence and regression coverage.

---

### Session: March 30, 2026 - View Features: Explicit Column List and CASCADE/RESTRICT

**Objective**: Extend view functionality with explicit column renaming and PostgreSQL-standard dependency handling.

**Status**: ✅ COMPLETED

**Features Implemented**:
1. ✅ **CREATE VIEW with explicit column list** - Rename output columns without SELECT aliases
2. ✅ **CREATE OR REPLACE VIEW with explicit column list** - Maintain columns on replacement
3. ✅ **DROP VIEW CASCADE** - Delete view + all dependent views atomically
4. ✅ **DROP VIEW RESTRICT** (default) - Prevent drop if other views depend on it
5. ✅ **Dependency tracking and validation** - Multi-level view chain support

**Code Changes**:
- **internal/ast/ast.go**:
  - Added ColumnNames []string field to CreateView struct
  - Added Behavior string field to DropView struct ("CASCADE", "RESTRICT", or "")

- **internal/parser/token.go**:
  - Added TokenCascade keyword token
  - Added TokenRestrict keyword token

- **internal/parser/parser.go**:
  - Extended parseCreateView() to parse optional (col1, col2, ...) syntax with validation
  - Extended parseDropView() to parse CASCADE/RESTRICT modifiers
  - Added column list validation: parenthesis matching, non-empty identifiers, no trailing commas, uniqueness check

- **internal/catalog/views.go**:
  - Added FindDependentViews(viewName, schema string) []string - detects views that depend on given view
  - Added DropViewWithBehavior(name, behavior, schema) error - handles RESTRICT/CASCADE logic
  - Supports multi-level dependency chains (v1 -> v2 -> v3)

- **internal/executor/executor_ddl.go**:
  - Enhanced executeCreateView():
    - Validates explicit column list count matches SELECT result columns
    - Detects duplicate column names (case-insensitive)
    - Stores explicit names in View.Columns for projection
    - Persists to storage
  - Enhanced executeDropView():
    - Calls FindDependentViews() to detect dependencies
    - Calls DropViewWithBehavior() with CASCADE/RESTRICT behavior
    - ON CASCADE: Deletes dependent views from catalog and storage
    - ON RESTRICT + dependencies: Returns error with clear dependency list messaging

- **internal/executor/executor_select.go**:
  - Fixed view column resolution: Changed from recreating columns from query results to using `view.Columns` directly
  - Enables explicit column names to appear in SELECT output

- **internal/parser/parser_test.go**:
  - Added TestCreateViewWithColumnList() - Verify parsing of column list syntax
  - Added TestCreateViewWithColumnListAndReplace() - Verify OR REPLACE with columns
  - Added TestDropViewCascade() - Verify CASCADE keyword parsing
  - Added TestDropViewRestrict() - Verify RESTRICT keyword parsing
  - Added TestDropViewIfExistsCascade() - Verify IF EXISTS with CASCADE

- **cmd/test-views-columnlist/main.go**:
  - 8-phase integration test validating explicit column list feature
  - Tests: create table, insert rows, CREATE VIEW with column names, multirow results, column count validation, duplicate detection, CREATE OR REPLACE, persistence

- **cmd/test-views-cascade/main.go**:
  - 9-phase integration test validating CASCADE/RESTRICT feature
  - Tests: view hierarchy creation, RESTRICT rejection, CASCADE success, dependent view verification, IF EXISTS CASCADE, default behavior tests

**Test Results**: ✅ All tests passing
- Parser tests: 5 new tests (2 column list, 3 cascade/restrict)
  - `go test -v ./internal/parser -run TestCreateView` → 3 tests pass
  - `go test -v ./internal/parser -run TestDropView` → 5 tests pass
- Integration test-views-columnlist: 8/8 phases ✅
- Integration test-views-cascade: 9/9 phases ✅
- E2E psql validation: All syntaxes confirmed working
- Zero regressions: All existing tests passing

**E2E Wire Protocol Validation**:
- ✅ CREATE VIEW v (col1, col2, col3) AS SELECT ... syntax
- ✅ \dv shows column names in view definition
- ✅ Column count mismatch properly rejected
- ✅ DROP VIEW CASCADE removes dependent views
- ✅ DROP VIEW RESTRICT shows dependency list in error message

**Technical Implementation Notes**:
- View columns stored directly in catalog, not derived from query results
- Dependency detection scans all schemas for views with matching references
- CASCADE deletion removes views from both catalog maps and Pebble storage
- Column validation: count matching + uniqueness (case-insensitive)
- RESTRICT is default behavior when no modifier specified

**Known Limitations**:
1. No ALTER VIEW ... RENAME support yet
2. No CREATE VIEW IF NOT EXISTS yet (only CREATE OR REPLACE)
3. No materialized views (MATERIALIZED VIEW, REFRESH)
4. No view-level permissions/security policies
5. No system catalog views (pg_views, view_definitions)

**Status**: ✅ COMPLETED - Full view lifecycle support with explicit columns and dependency management

---

### Session: March 30, 2026 - DROP INDEX (Priority 1)

**Objective**: Implement the missing DROP INDEX operation to complete index lifecycle support.

**Status**: ✅ COMPLETED

**Features Implemented**:
1. ✅ **DROP INDEX parser support** - Added syntax `DROP INDEX index_name ON [schema.]table`
2. ✅ **Catalog/Table index removal** - Index metadata can be removed safely from table definitions
3. ✅ **Executor support with persistence** - DROP INDEX updates in-memory catalog and persists table metadata
4. ✅ **Parser tests and integration test** - Added validation coverage for syntax and persistence behavior

**Code Changes**:
- **internal/ast/ast.go**:
  - Added `DropIndex` statement node with `Name` and `Table`

- **internal/parser/parser.go**:
  - Added DROP dispatcher support for `INDEX`
  - Implemented `parseDropIndex()` for `DROP INDEX idx ON table`
  - Updated DROP error message to include INDEX

- **internal/catalog/table.go**:
  - Added `DropIndex(name string) error`

- **internal/catalog/catalog_tables.go**:
  - Added `Catalog.DropIndex(tableName, indexName, schema...)`

- **internal/executor/executor.go**:
  - Added `*ast.DropIndex` dispatch case

- **internal/executor/executor_ddl.go**:
  - Implemented `executeDropIndex()`
  - Persists table metadata via `SaveTableWithSchema()` after drop

- **internal/constants/constants.go**:
  - Added result tag `ResultDropIndex = "DROP INDEX"`

- **internal/parser/parser_test.go**:
  - Added `TestDropIndex()`
  - Added `TestDropIndexQualifiedTable()`

- **cmd/test-drop-index/main.go**:
  - Added end-to-end integration test for create/drop/reload behavior of indexes

- **README.md**:
  - Added syntax line: `DROP INDEX indice ON tabla`
  - Added note explaining DROP INDEX persistence behavior

**Status**: ✅ COMPLETED - Index lifecycle now supports both CREATE INDEX and DROP INDEX with persistence

---

### Session: March 30, 2026 - DROP TABLE CASCADE/RESTRICT (Priority 2)

**Objective**: Extend DROP TABLE with dependency-aware behavior (`RESTRICT` default and `CASCADE`) plus idempotent `IF EXISTS`.

**Status**: ✅ COMPLETED

**Features Implemented**:
1. ✅ **DROP TABLE parser support** for `IF EXISTS`, `CASCADE`, and `RESTRICT`
2. ✅ **RESTRICT mode (default)** blocks drop when FK or view dependencies exist
3. ✅ **CASCADE mode** cleans FK dependencies and drops dependent views before dropping table
4. ✅ **IF EXISTS mode** returns success when target table does not exist

**Code Changes**:
- **internal/ast/ast.go**:
  - Extended `DropTable` with `IfExists bool` and `Behavior string`

- **internal/parser/parser.go**:
  - Extended `parseDropTable()` to handle `DROP TABLE IF EXISTS ... [CASCADE|RESTRICT]`

- **internal/catalog/catalog_tables.go**:
  - Added `FindForeignKeyDependents(schema, tableName) []string`
  - Added `RemoveForeignKeyReferencesToTable(schema, tableName) ([]string, error)`

- **internal/catalog/views.go**:
  - Added `FindViewsUsingSource(sourceName, schema) []string` for DROP TABLE dependency checks

- **internal/executor/executor_ddl.go**:
  - Enhanced `executeDropTable()` with:
    - RESTRICT dependency checks
    - CASCADE cleanup of FK constraints + dependent views
    - IF EXISTS idempotence handling
    - Storage persistence of modified dependent tables/views

- **internal/parser/parser_test.go**:
  - Added `TestDropTableCascade()`
  - Added `TestDropTableIfExistsRestrict()`

- **cmd/test-drop-table-fk/main.go**:
  - Extended integration test to validate:
    - RESTRICT failure on FK dependency
    - CASCADE success and FK cleanup on child table constraints

- **README.md**:
  - Added syntax: `DROP TABLE [IF EXISTS] tabla [CASCADE | RESTRICT]`
  - Added behavior notes and examples

**Status**: ✅ COMPLETED - DROP TABLE now supports dependency-aware lifecycle management with RESTRICT/CASCADE

---

### Session: March 30, 2026 - DROP SCHEMA CASCADE/RESTRICT (Priority 3)

**Objective**: Extend DROP SCHEMA with `RESTRICT`/`CASCADE` behavior and idempotent `IF EXISTS`.

**Status**: ✅ COMPLETED

**Features Implemented**:
1. ✅ **DROP SCHEMA parser support** for `IF EXISTS`, `CASCADE`, and `RESTRICT`
2. ✅ **RESTRICT mode (default)** blocks schema drop when schema is not empty
3. ✅ **CASCADE mode** drops non-empty schemas (tables/views) safely
4. ✅ **IF EXISTS mode** allows idempotent schema drops

**Code Changes**:
- **internal/ast/ast.go**:
  - Extended `DropSchema` with `IfExists bool` and `Behavior string`

- **internal/parser/parser.go**:
  - Extended `parseDropSchema()` to support `DROP SCHEMA IF EXISTS ... [CASCADE|RESTRICT]`

- **internal/catalog/catalog_tables.go**:
  - Added `SchemaIsEmpty(name) (bool, error)` helper for RESTRICT semantics

- **internal/executor/executor_ddl.go**:
  - Enhanced `executeDropSchema()` with:
    - default `RESTRICT` behavior
    - explicit CASCADE handling
    - IF EXISTS idempotence

- **internal/parser/parser_test.go**:
  - Added `TestDropSchemaCascade()`
  - Added `TestDropSchemaIfExistsRestrict()`

- **cmd/test-drop-schema/main.go**:
  - Added integration test for:
    - RESTRICT failure on non-empty schema
    - CASCADE success on non-empty schema
    - IF EXISTS idempotent behavior

- **README.md**:
  - Added syntax and examples for `DROP SCHEMA [IF EXISTS] ... [CASCADE|RESTRICT]`

**Status**: ✅ COMPLETED - DROP SCHEMA now supports explicit lifecycle behavior with RESTRICT/CASCADE

---

### Session: March 30, 2026 - CREATE VIEW IF NOT EXISTS (Priority 4)

**Objective**: Add idempotent view creation support with `CREATE VIEW IF NOT EXISTS`.

**Status**: ✅ COMPLETED

**Features Implemented**:
1. ✅ **Parser support** for `CREATE VIEW IF NOT EXISTS`
2. ✅ **Executor idempotence**: returns success when view already exists
3. ✅ **Validation rule**: rejects invalid `CREATE OR REPLACE VIEW IF NOT EXISTS`
4. ✅ **Parser + integration tests** for creation and idempotent behavior

**Code Changes**:
- **internal/ast/ast.go**:
  - Extended `CreateView` with `IfNotExists bool`

- **internal/parser/parser.go**:
  - Extended `parseCreateView()` to parse `IF NOT EXISTS`
  - Added guard: `CREATE OR REPLACE VIEW` cannot be combined with `IF NOT EXISTS`

- **internal/executor/executor_ddl.go**:
  - Enhanced `executeCreateView()` to return success when target view already exists and `IfNotExists` is set

- **internal/parser/parser_test.go**:
  - Added `TestCreateViewIfNotExists()`

- **cmd/test-view-if-not-exists/main.go**:
  - Added integration test validating:
    - no-error behavior when view exists
    - successful creation when view does not exist

- **README.md**:
  - Added syntax and examples for `CREATE VIEW IF NOT EXISTS`

**Status**: ✅ COMPLETED - View creation now supports idempotent `IF NOT EXISTS` behavior
