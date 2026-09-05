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
│   ├── focusd/            # Main database server
│   │   ├── main.go        # Bootstrap + flags (-addr, -gui, -data, -query-timeout)
│   │   ├── handler.go     # SQL execution (wire + GUI): HandleWithDatabaseCtx, HandleScript
│   │   ├── gui_server.go  # HTTP mux, static embed, middlewares (recover/method/cache)
│   │   ├── gui_api.go     # GUI API handlers + DTOs
│   │   └── static/        # GUI "FocusDB Studio" (embebida con go:embed)
│   │       ├── index.html # Solo markup + <link>/<script>
│   │       ├── css/       # app.css (design system "Botánico nocturno")
│   │       ├── js/        # Módulos ES (ver abajo)
│   │       └── vendor/    # CodeMirror 5.65.16 vendoreado (offline, MIT)
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

### GUI frontend (`cmd/focusd/static/js/`)

Módulos ES sin bundler (`<script type="module">`). CodeMirror se carga antes como script
clásico y se usa vía el global `window.CodeMirror`.

| Módulo | Responsabilidad |
|---|---|
| `app.js` | Bootstrap, cambio de vista (query/diagram/explorer), **puente `window.*`** |
| `state.js` | Estado compartido (`state.editor`); mutar propiedades, nunca reasignar |
| `api.js` | Cliente HTTP de los endpoints (+ `AbortController`, `maxRows`) |
| `dom.js` | `render()` (DOM-safe), `escHtml/escAttr`, `showToast`, `spawnMotes`, `downloadBlob` |
| `icons.js` | `ICON_PATHS` + `icon()` — iconografía SVG de línea |
| `editor.js` | CodeMirror, ejecución, validación, historial persistente, autocompletado |
| `tabs.js` | Pestañas de consulta (`CodeMirror.Doc` + `swapDoc`) |
| `results.js` | Resultados: filtro/orden/paginación, export CSV-JSON, modal de celda, script runner |
| `sidebar.js` | Árbol de objetos |
| `explorer.js` | Grilla CRUD (`/api/table-data` + escrituras por `/api/query`) |
| `diagram.js` | Diagrama ER (layout, drag, minimapa, export) |

**Convenciones del frontend (importantes)**
- El markup usa handlers inline (`onclick="..."`). Toda función referenciada desde HTML
  **debe** exportarse y registrarse en el `Object.assign(window, {...})` de `app.js`.
  Al agregar una, verificá con `grep -o 'on[a-z]*="[a-zA-Z_]*' static/index.html`.
- **Nunca usar `innerHTML` directo** (hay un hook de seguridad que lo bloquea): usar el helper
  `render(el, html)` de `dom.js` (DOMParser + `replaceChildren`).
- Estado persistido en `localStorage` con claves versionadas: `focusdb.tabs.v1`,
  `focusdb.history.v1`, `focusdb.diagram.v1.<sig>`. Todo `JSON.parse` va con try/catch.
- Los colores del diagrama están hardcodeados a propósito (const `C` en `diagram.js`) para que
  el SVG exportado sea autocontenido; el resto de la UI usa las CSS vars del tema.

## Build/Lint/Test Commands

> **Windows / entorno sin toolchain C**: anteponer `CGO_ENABLED=0` a todos los comandos de
> build/test/run (Pebble tiene fallback puro-Go). Sin esto, la compilación de
> `pebble/internal/manual` y `DataDog/zstd` falla con `cgo.exe: exit status 2`.

### Build
```bash
go build ./cmd/focusd                    # Build server
go build -o focusd ./cmd/focusd          # Build with custom binary name
```

### Run
```bash
go run ./cmd/focusd                      # Run database server (wire :4444, GUI :9011)
go run ./cmd/focusd -gui :9011 -data ./data -query-timeout 60s
```

**Nota de Windows**: el binario queda bloqueado mientras corre. Antes de recompilar,
`taskkill /F /IM focusd.exe`. Pebble además es de **un solo escritor**: solo un proceso
puede tener abierto el mismo `-data` a la vez.

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

Desde el refactor (Fase 2) el dispatch es **por datos, con auto-registro**: no hay que editar
`executor.go` ni el `switch` de `parser.go`.

1. **Token** en `internal/parser/token.go`: la constante + la entrada en el mapa `keywords`.
2. **Nodo AST** en `internal/ast/ast.go` (con su marker `stmtNode()`).
3. **Parser**: escribir `parseXxx()` en `parser.go`/`helpers.go` y registrarlo en el `init()` de
   `parser_registry.go` (`topLevelParsers`, o `createParsers`/`dropParsers`/`alterParsers`
   según corresponda). Los mapas se pueblan en `init()` a propósito, para evitar el
   "initialization cycle" estático.
4. **Executor**: escribir el handler en su propio `executor_*.go` y auto-registrarlo con
   `registerExec((*Executor).executeXxx)` dentro del `init()` de ese mismo archivo.
5. **Validación** en `internal/validator/` si aplica.
6. **Tag de resultado** en `internal/constants/`.
7. **Tests**: unit en `parser_test.go` + integración en `cmd/test-<feature>/`.
8. **Docs**: sintaxis en `README.md` + entrada en el Work Log de este archivo.

**Helpers a reutilizar** (no re-implementar):
- Executor: `checkCtx`, `schemaOrPublic`, `qualifiedName`, `persistTable`, `persistTableWarn`
  (`executor_helpers.go`).
- **Pipeline de SELECT** (`executor/select_pipeline.go`): toda etapa posterior a FROM/WHERE
  (GROUP BY, ventanas, QUALIFY, ORDER BY, proyección, DISTINCT, LIMIT) vive en `finishSelect`
  sobre un `rowset` (`cols []joinColumn` + `rows`). Resolver columnas con `rowset.resolve` /
  `resolveWithAliases` (acepta `ref.col`, alias del SELECT y texto de agregado `SUM(x)`); no
  volver a duplicar post-procesado por ruta. Funciones de ventana en `window.go`
  (`evalWindow`), comparación de orden en `queryutil.CompareByKeys`/`SortRowsByKeys`.
- Parser: `parseColumnRef` (columna o `AGG(col)` como texto) y `parseWindowCall`
  (`FUNC(arg) [OVER (...)]`) para cualquier cláusula que acepte expresiones de columna.
- Parser: `parseIdentRequired`, `parseOptionalIfExists`, `parseOptionalCascadeRestrict`,
  `parseBeginEndBlock`, `parseWhereClause` (`helpers.go`).
- Comparación de valores: `catalog.ValuesEqual` / `executor.compareCells` — **nunca `==` crudo**
  (ver "Gotchas" abajo).

### Adding a New Feature
1. Understand existing patterns in related code
2. Write tests first when possible
3. Follow the layered architecture (parser -> validator -> executor)
4. Ensure thread safety for shared state

### Gotchas del motor (aprendidos a golpes)

1. **Tipos de celda heterogéneos.** Las filas son `[]interface{}` con tipos Go mezclados: los
   literales de SQL se guardan como **string**, los valores generados por `IDENTITY` como
   **int**. Comparar con `==` crudo falla en silencio (`int(1) != "1"`). Usar siempre
   `catalog.ValuesEqual` (FK, igualdad) o `compareCells` (orden). Los índices ya son coherentes
   porque sus claves son strings canónicas (`normalizeIndexValue`).
   Esto causó dos bugs reales: FK contra PK `IDENTITY`, y `UPDATE/DELETE ... WHERE id = 1`
   afectando 0 filas (el `SELECT` "funcionaba" solo por el fast-path del índice).
2. **Sin transacciones.** Un script que falla a mitad deja aplicados los statements previos;
   `/api/script` reporta `failedIndex` pero no hace rollback.
3. **Persistencia desacoplada del AST.** Vistas y rutinas guardan su SQL canónico
   (`QueryText`/`BodyText`) y se **re-parsean** al cargar. Al tocar esos structs hay que
   propagar el texto en las tres capas: `ast` → `catalog` → `storage`, y también en los
   `Load*` de la recarga (un `Get*` que olvide copiar `BodyText` lo pierde al persistir).
4. **`gob` acopla el formato en disco.** Agregar campos a un struct persistido es
   backward-compatible (decodifican a cero); renombrarlos o cambiar su tipo, no.

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

---

### Session: March 30, 2026 - IDENTITY INSERT Positional Mapping Fix

**Objective**: Fix incorrect value mapping when inserting into tables with `IDENTITY` columns using `INSERT ... VALUES (...)` without explicit column list.

**Status**: ✅ COMPLETED

**Issue**:
- In tables like `(id IDENTITY, name TEXT)`, statements such as `INSERT INTO test VALUES ('Oscar')` returned success but failed to persist `name` correctly.
- Root cause: positional mapping used the first provided value against table index `0`, then IDENTITY generation overwrote/shifted expected semantics.

**Fix Implemented**:
1. ✅ **Executor INSERT mapping corrected**:
   - Supports full positional insert when values count equals total columns.
   - Supports "non-IDENTITY-only" positional insert when values count equals non-IDENTITY columns.
   - Auto-generates IDENTITY values while preserving provided non-IDENTITY values in correct columns.
2. ✅ **Validation path kept consistent** with existing insert validator expectations.
3. ✅ **Integration regression test added** for real scenario.

**Code Changes**:
- **internal/executor/executor_dml.go**:
  - Updated `executeInsert()` positional mapping logic for IDENTITY-aware inserts without explicit column list.

- **cmd/test-identity-insert/main.go**:
  - Added integration test validating:
    - auto-incremented IDs
    - correct persistence of `name` values
    - stable row ordering and values

- **README.md**:
  - Added behavior note and examples for `INSERT` without column list in IDENTITY tables.

**Validation Results**:
- ✅ `go run ./cmd/test-identity-insert`
- ✅ `go build ./cmd/focusd`
- ✅ `go test -vet=off ./internal/...`

**Status**: ✅ COMPLETED - IDENTITY positional INSERT now preserves non-IDENTITY values correctly

---

### Session: August 14, 2026 - Modularization Refactor (Roadmap + Fase 1)

**Objective**: Reducir la fricción de agregar features. Diagnóstico completo de acoplamiento y
plan por fases documentado en `REFACTOR.md`. Arranque de la Fase 1.

**Status**: ⏳ IN PROGRESS

**Diagnóstico (resumen, detalle en `REFACTOR.md`)**:
- 🔴 Boilerplate copiado en cada handler del executor (resolución de schema, persistencia con
  warning, ctx-check) — ~13 sitios, con default de schema inconsistente (`""` vs `"public"`) y
  manejo de error de persistencia inconsistente (unos warning+éxito, otros error).
- 🔴 Capa pg/system-catalog triplicada: `server/bypass.go` + `catalog/system_handler.go`
  (~400 líneas muertas) + `catalog/system_catalog.go`, toda por match de strings.
- 🔴 `storage.Backend` de 18 métodos con implementación legacy toda-stubs.
- 🟡 `Catalog` god object (5 responsabilidades bajo un solo lock, acoplado al AST).
- 🟡 Nodos AST anémicos: `WhereClause` = un solo `col = valor` (sin AND/OR ni operadores);
  agregados detectados por prefijo de string; `gob` de nodos AST acopla persistencia al parseo.
- 🟡 `validator` desconectado del parser; lógica de validación triplicada; 3 listas de tipos.

**Plan por fases**: ver `REFACTOR.md`.
1. Fase 1: matar boilerplate (helpers executor/parser) — bajo riesgo. ⏳
2. Fase 2: registry de statements (dispatch por datos).
3. Fase 3: consolidar capa pg/system-catalog.
4. Fase 4: segregar `storage.Backend` y `Catalog`.
5. Fase 5: AST expresivo (WHERE compuesto, operadores, agregados, HAVING).

**Fase 1 — Cambios**:
- [x] Helpers en executor (`internal/executor/executor_helpers.go`): `checkCtx`, `schemaOrPublic`,
      `qualifiedName`, `persistTable`, `persistTableWarn`. Aplicados en `executor_dml.go`,
      `executor_ddl.go`, `executor_select.go`, `executor_procedure.go`, `executor_trigger.go`,
      `executor_job.go`. Eliminados: ~13 bloques `select { <-ctx.Done() }` / `if ctx.Err()`,
      ~11 resoluciones de schema duplicadas, y el bloque `if e.storage != nil { SaveTableWithSchema
      + warning }` en los sitios de tabla. Mensajes de warning contextuales preservados textualmente.
- [x] Helpers en parser (`internal/parser/helpers.go`): `parseOptionalIfExists`,
      `parseOptionalCascadeRestrict`. Aplicados en `parseDropTable`, `parseDropView`, `parseDropSchema`.
- [x] `parseBeginEndBlock` (procedure/trigger/job comparten `BEGIN … END`) y barrido de
      `parseIdentRequired` (parseDropIndex, parseDropProcedure, parseDropTrigger, parseDropJob,
      parseAlterJob, parseCreateIndex). Fase 1 completa. Integración adicional ✅:
      `test-job-persistence`, `test-drop-index`, `test-parse-proc`.
- [x] Verificación (CGO_ENABLED=0, por límite de toolchain C del entorno; Pebble usa fallback puro-Go):
      `go build ./internal/...` ✅, `go vet ./internal/{executor,parser}` ✅,
      `go test ./internal/...` ✅. Integración ✅: `test-identity-insert`, `test-alter`,
      `test-trigger-recursion`, `test-drop-table-fk`, `test-drop-schema`, `test-views-cascade`.
      Cero cambio de comportamiento observable.

**Nota de entorno**: `go build ./...` completo falla al compilar Pebble vía CGO (`cgo.exe: exit
status 2` en `pebble/internal/manual` y `DataDog/zstd`) porque el toolchain C no está en el PATH
usado por `go`. Workaround verificado: `CGO_ENABLED=0` (Pebble tiene implementación puro-Go).

**Fase 2 — Registry de statements (dispatch por datos)** ✅ COMPLETED:
- Executor (`internal/executor/executor_registry.go`): `registerExec[T ast.Statement]` genérico +
  `execHandlers map[reflect.Type]execHandler`. `Execute` reemplaza el type-switch de ~30 casos por
  lookup+dispatch. Handlers auto-registrados en `init()` co-localizado en cada `executor_*.go`
  (ddl/dml/select/procedure/trigger/job) + `executeSet` en `executor.go`. Agregar un statement ya
  no requiere tocar `executor.go`.
- Parser (`internal/parser/parser_registry.go`): `topLevelParsers`/`createParsers`/`dropParsers`/
  `alterParsers` (`map[TokenType]stmtParseFn`). Poblados en `init()` para evitar el "initialization
  cycle" estático (los parsers referencian de vuelta a los mapas). `ParseStatement`,
  `parseCreate`, `parseDrop`, `parseAlter` colapsados a lookup + fallback de error. `parseNoop`
  para tokens no-op (`;`, END, dollar-string) al top level.
- Fix colateral: `cmd/test-persistence-integration` usaba `DROP SCHEMA` sin CASCADE sobre schema
  no vacío (fallaba ya en el baseline por RESTRICT-default) → corregido a CASCADE.
- Verificación (CGO_ENABLED=0): `go build/vet/test ./internal/...` ✅; **20/20** tests de
  integración del motor ✅; servidor + `test-client` protocolo extendido ✅.

**Fase 3 — Consolidar capa pg/system-catalog** ⏳ PARCIAL:
- Borradas ~400 líneas de código muerto (versión antigua comentada) en `catalog/system_handler.go`
  → 1023 → 622 líneas. Verificado con build/vet/test + tests cliente wire-protocol (`test-client`,
  `test-information-schema`, `test-users` en :4445, `test-simple-query`).
- Reachability documentada: `checkBypass` precede siempre a `HandleSystemQueryForDatabase`
  (`conn.go:155`→`:161`); el path extendido (`:281`) usa `checkBypass`+`executeQuery`, nunca
  `HandleSystemQuery`. Los casos inline de `system_handler` que también matchea `bypass` quedan
  sombreados/inalcanzables (duplicación latente, no divergencia).
- PENDIENTE (gateado por riesgo de wire-compat, requiere validar contra pgAdmin/DBeaver/psql real):
  unificar `bypass.go` + switch de `system_handler.go` en una sola tabla patrón→respuesta sirviendo
  desde `system_catalog.go`. (Decisión del usuario: no tocarla ahora.)

**Fase 4 — Segregar `storage.Backend` y `Catalog`** ✅ COMPLETED (núcleo):
- Borrada la implementación legacy `Storage` (backend JSON, toda-stubs, nunca instanciada — no hay
  `storage.New` en el código): `internal/storage/storage.go` 417 → 141 líneas. Preservados los
  helpers compartidos con Pebble (`indexColumnsFromData`, `syncIdentityValues`) y recortados los
  imports huérfanos (`fmt`, `io/ioutil`, `os`, `path/filepath`, `strings`, `sync`).
- `Backend` (18 métodos planos) segregada en interfaces por responsabilidad: `TableStore`,
  `ViewStore`, `ProcedureStore`, `TriggerStore`, `JobStore`, `SchemaStore`, compuestas en `Backend`
  (+ `LoadAll`/`Close`). `PebbleStorage` y `Executor` sin cambios → comportamiento idéntico.
- Resta (más invasivo, gateado): mover mutación de filas de `DropColumnData`/`RenameColumnData` al
  catálogo; sub-catálogos con locks propios.
- Verificación (CGO_ENABLED=0): build/vet/test ✅; **20/20** integración del motor (con ciclos de
  persistencia+reload) ✅.

**Fase 5 — AST expresivo (Paso 1: operadores de comparación en WHERE)** ✅:
- Nuevos operadores en WHERE: `=`, `<>`/`!=`, `<`, `>`, `<=`, `>=` (SELECT/UPDATE/DELETE).
- Cambios: `internal/parser/token.go` (`TokenLte`, `TokenGte`); `internal/parser/lexer.go` (lexeo de
  `>`, `>=`, `<=`, `!=`); `internal/ast/ast.go` (`WhereClause.Operator`); `internal/parser/helpers.go`
  (`parseWhereClause` lee operador + helper `whereOperator`); `internal/executor/where.go` (nuevo,
  comparador type-aware `whereMatches`/`compareCells`: numérico si ambos parsean, si no lexicográfico);
  y los 4 sitios de evaluación (`fetchRows` — índice preservado para `=`, scan+comparador para el
  resto —, `filterJoinedRows`, `performUpdate`, `performDelete`).
- Equality (`=`) preserva EXACTAMENTE la semántica previa (índice + `==`). Aún un solo predicado
  (sin AND/OR — Paso 2 pendiente).
- Test nuevo: `cmd/test-where-operators` (10 asserts). Verificación: build/vet/test ✅; **21/21**
  integración del motor ✅. README actualizado.
**Fase 5 — Paso 4: desacoplar persistencia del AST (vistas)** ✅:
- Vistas ahora persisten el texto SQL del SELECT y se re-parsean al cargar (formato en disco estable
  e independiente de la forma del AST). Cambios: `ast.CreateView.QueryText`; captura en el parser vía
  `Token.Pos` + helper `sliceFrom`; `catalog.View.QueryText`; `storage.ViewData.QueryText` (guardado
  en `SaveView`); en `LoadAll`, si `QueryText != ""` se re-parsea con `parseViewQuery` (nuevo import
  `storage → parser`, sin ciclo), con **fallback** al AST `gob` para datos previos.
- Test nuevo: `cmd/test-view-persistence` (round-trip create→close→reopen→LoadAll→query).
  Verificación: build/vet/test ✅; **23/23** integración ✅.
- Pendiente: aplicar el mismo patrón (capturar texto de `BEGIN…END`) a procedures/triggers/jobs, que
  aún serializan `[]ast.Statement` vía `gob`.

**Fase 3 — Consolidar capa pg/system-catalog (unificación)** ✅ COMPLETED:
- `catalog/system_handler.go`: el switch de ~22 casos `strings.Contains` en
  `HandleSystemQueryForDatabase` reescrito como tabla ordenada `systemRoutes []systemRoute`
  (`{match func(string)bool, handle func(*Catalog,upper,db)*SystemResult}`), primer match gana,
  orden/predicados idénticos (1:1). Helpers `hasAny` (multi-substring) y `canned` (respuesta fija).
  Sin ciclo de init (var literal; las closures referencian métodos que no referencian la tabla).
- No se fusionó con `server/bypass.go` entre paquetes (mecánicas distintas: bypass escribe wire
  directo; system_handler arma `SystemResult`); bypass ya es tabla limpia y corre primero. Layering
  documentado en el código (fallback defensivo para patrones solapados).
- Verificación: build/vet/test + `catalog` unit tests (system_handler_test.go) + tests cliente
  wire-protocol (test-client, test-information-schema, test-users@:4445, test-simple-query,
  test-multi-client) todos ✅. **Roadmap de modularización (Fases 1–5) completo.**

**Fase 5 — Paso 4 (resto): desacoplar persistencia del AST en procedures/triggers/jobs** ✅:
- `parseBeginEndBlock` ahora devuelve `(body, bodyText, err)` capturando el texto verbatim entre
  BEGIN y END (`Token.Pos`+`sliceFrom`). Propagado a `ast.Create{Procedure,Trigger,Job}.BodyText`,
  `catalog.{Procedure,Trigger,Job}.BodyText` (Create* del catálogo reciben `bodyText`), y
  `storage.{Procedure,Trigger,Job}Data.BodyText`. Cubre también el path dollar-quoted de procedures.
- En `LoadAll`, si `BodyText != ""` se re-parsea con `parseBody` (nuevo helper) hacia `[]ast.Statement`,
  con fallback al AST `gob` para datos previos. Fase 5 completa (vistas + rutinas desacopladas).
- Test `cmd/test-routine-persistence`: proc+trigger creados, cerrados, reabiertos y recargados; el
  CALL del proc re-parseado inserta y dispara el trigger re-parseado. Regresión **25/25** ✅.

### Session: August 14, 2026 - Rediseño web (Botánico nocturno) + fix ORDER BY

**Rediseño de `cmd/focusd/static/index.html`** (solo la piel; funcionalidad intacta):
- Identidad "Botánico nocturno": paleta bosque profundo (`#0f1a14`) + savia (`#7bb661`) + arena
  cálida + floración ámbar. Reemplazados TODOS los literales Catppuccin morado/azul (incl. los
  hexes del render SVG del diagrama).
- Capa viva: fondo con luz filtrada (radial-gradients), grano SVG sutil, transiciones lentas
  orgánicas, wordmark `🌿 focusdb` con hoja que se mece (`@keyframes sway`), badge de conexión que
  respira (`breathe`), sintaxis del editor CodeMirror retematizada (keywords savia, strings ámbar).
- Verificado en vivo (server :9011): paleta computada correcta, sin errores de consola, queries y
  sidebar funcionando. (Screenshot no disponible: el panel del navegador no estaba visible.)

**Bug fix (preexistente) — ORDER BY en columnas proyectadas**: `SELECT col1, col2 ... ORDER BY x`
no ordenaba (sí funcionaba `SELECT *`). Causa en `executor_select.go` `projectColumns`: aplicaba
`ApplyOrderBy` sobre filas ya proyectadas pasando `table.Columns` (esquema completo) → índice de la
columna de orden fuera de rango. Fix: aplicar ORDER BY sobre las filas completas ANTES de proyectar
(además ahora permite `ORDER BY` por columnas no seleccionadas, como SQL estándar). Verificado;
regresión **23/23** ✅.

**Fase 5 — Paso 3: agregados SUM/AVG/MIN/MAX** ✅:
- Parser (`helpers.go`): detección genérica `isAggregateFunc` (COUNT/SUM/AVG/MIN/MAX); el bloque de
  agregados en `parseSelectItem` ya no es solo-COUNT. Fix en `parser.go`: el fast-path de
  `SELECT func()` sin argumentos excluye agregados (antes `SUM(monto)` fallaba con "expected )").
- Executor (`aggregate.go` nuevo): `parseAggregate` (extrae función + columna arg), `computeAggregate`
  (COUNT/SUM/AVG/MIN/MAX; SUM/AVG numéricos vía `toFloat`, MIN/MAX numérico o lexicográfico vía
  `compareCells`, NULLs ignorados salvo COUNT(*)), helpers `toFloat`/`numberValue`. `hasAggregates`
  y el parseo de columnas en `executeGroupedSelect` ahora usan `parseAggregate`; `aggregateAllRows`
  y `aggregateByGroups` usan `computeAggregate` (antes solo COUNT).
- Test nuevo: `cmd/test-aggregates` (12 asserts: scalar, agregado+WHERE, y GROUP BY con las 5
  funciones). Verificación: build/vet/test ✅; **22/22** integración del motor ✅. README actualizado.

**Fase 5 — Paso 2: predicados compuestos AND/OR** ✅:
- `ast.WhereClause` ahora es un árbol: leaf (`Column`/`Operator`/`Value`, `Conj==""`) o nodo
  compuesto (`Left`/`Conj`/`Right`). Helpers `IsLeaf()` y `LeafColumns()`.
- Parser (`helpers.go`): `parseWhereClause` → `parseWhereOr`/`parseWhereAnd`/`parseWherePredicate`
  con precedencia estándar (OR<AND<comparación) y paréntesis. Token `TokenAnd` agregado a
  `token.go` (const + keywords map).
- Evaluador recursivo `evalWhere` (`executor/where.go`) con short-circuit AND/OR; usado en los 4
  sitios: `fetchRows` (fast-path de índice preservado solo para leaf `=`), `filterJoinedRows`
  (JOIN, resolver combinado), `performUpdate`, `performDelete`. Helper `columnResolver`.
- Validator recorre el árbol: `validateWhereColumns` sobre `WhereClause.LeafColumns()`.
- Compat gob: agregar campos a `WhereClause` es backward-compatible (decode de blobs viejos → cero).
- Test `cmd/test-where-operators` ampliado a 16 asserts (AND/OR, precedencia, paréntesis, UPDATE/
  DELETE compuestos, JOIN+WHERE compuesto). Verificación: build/vet/test ✅; 21/21 integración ✅.
  README actualizado.

### Session: August 14, 2026 - Fixes del motor: FK vs IDENTITY + JOINs N-way

**Objetivo**: dejar el motor "al 100%" tras detectar dos defectos probando la GUI.

**Status**: ✅ COMPLETED

**Bug 1 — FK contra columna IDENTITY (correctitud)**:
- Síntoma: `INSERT` en una tabla hija con FK a una PK `IDENTITY` fallaba con "foreign key
  violation: value N not found" aunque el padre existía. Con PK no-IDENTITY (valores literales)
  sí funcionaba.
- Causa raíz: el motor almacena valores con tipos Go heterogéneos — los literales como **string**
  (`stmt.Values[i].Value`), los IDENTITY como **int** (`Column.IdentityValue`). La comprobación FK
  usaba `==` sobre `interface{}`, sensible al tipo: `int(1) == string("1")` es `false`.
- Fix: comparación tolerante al tipo por forma canónica (mismo criterio que el matching de JOIN,
  que ya usaba `fmt.Sprintf("%v", …)`). Nuevo helper exportado `catalog.ValuesEqual(a,b)`.
  Aplicado en `catalog/table.go` (`InsertRow` FK check) y `executor/executor_dml.go`
  (`validateForeignKey`, ruta de UPDATE). Los huérfanos se siguen rechazando.

**Bug 2 — JOINs de 3+ tablas (feature/limitación)**:
- Síntoma: `FROM a JOIN b ON … JOIN c ON …` daba "unknown table qualifier c". El parser parseaba
  **un solo** JOIN (`if`, no loop) y el AST/executor estaban cableados a exactamente dos tablas
  (`stmt.Table` + `stmt.Join`).
- Fix parser (`parser/helpers.go`): `parseFromAndJoin` ahora devuelve `[]*ast.JoinClause` y hace
  loop sobre joins consecutivos (`parseSingleJoin` extraído). `parser.go` puebla `stmt.Joins`
  (+ `stmt.Join = Joins[0]` para compat con `catalog/views.go` y blobs gob viejos).
- Fix AST (`ast.go`): `Select.Joins []*JoinClause` (se conserva `Join *JoinClause`, backward-compat).
- Fix executor (`executor/executor_select.go`): `executeJoinSelect` reescrito como **fold
  left-deep** sobre las tablas. Se acumulan filas + columnas como `[]joinColumn{ref,name,typ}`;
  `foldJoin` maneja INNER/LEFT/RIGHT/FULL/CROSS con relleno de NULLs por lado; `resolveJoinColumn`
  resuelve refs calificadas/inequívocas contra el esquema acumulado; WHERE/GROUP BY/agregados/
  ORDER BY/proyección/`SELECT *` operan sobre el conjunto acumulado (columnas `ref.col`).
  Eliminadas las funciones de 2 tablas (performJoin/perform{Inner,Left,Right,Full}Join,
  filterJoinedRows, createVirtualTable, projectJoin{Star,Columns}, getRowValue).

**Verificación**: `go build/vet/test ./internal/...` ✅. Regresión de integración ✅
(`test-self-fk`, `test-drop-table-fk`, `test-where-operators`, `test-aggregates`,
`test-persistence-integration`, `test-views-cascade`, `test-routine-persistence`, `test-multi-stmt`).
Nuevo test `cmd/test-multi-join` (FK-IDENTITY + INNER/LEFT 3-way + WHERE compuesto + agregado +
`SELECT *` calificado + errores de qualifier). Test dedicado `cmd/test-fk-identity` con el repro
exacto (dos tablas IDENTITY + FK: INSERT hijo válido, rechazo de huérfano, ruta de UPDATE, y
control con PK no-IDENTITY). E2E vía API confirmado. README actualizado.

### Session: August 14, 2026 - NATURAL JOIN y JOIN ... USING

**Objetivo**: soportar `NATURAL JOIN` y `JOIN ... USING (cols)` sobre el motor de joins N-way.

**Status**: ✅ COMPLETED

**Cambios**:
- **token.go**: tokens `NATURAL` y `USING` (const + keywords).
- **ast.go**: `JoinClause.Natural bool` y `JoinClause.Using []string` (backward-compat gob).
- **parser/helpers.go**: `parseSingleJoin` acepta prefijo opcional `NATURAL` (INNER/LEFT/RIGHT/FULL,
  no CROSS) y, tras la tabla, resuelve `USING (col, ...)` o `ON …` o nada (natural). Loop de joins
  incluye `TokenNatural`.
- **executor/executor_select.go**: `foldJoin` reescrito con detección de pares de columnas:
  NATURAL → todas las columnas de igual nombre entre el lado acumulado y la tabla nueva; USING →
  las columnas nombradas (validadas en ambos lados). Semántica **coalesce**: la columna común
  aparece una sola vez (se descarta la copia del lado derecho) y en outer joins el valor coalescido
  toma el lado no nulo (`COALESCE`). La columna común sobrevive con el ref del lado izquierdo.
  `ON` explícito y `CROSS` conservan comportamiento idéntico (sin coalesce/drop). NATURAL sin
  columnas comunes degrada a CROSS (estándar SQL).

**Verificación**: build/vet/test ✅. Nuevos tests: `cmd/test-natural-join` (NATURAL INNER/LEFT/RIGHT
con coalesce desde ambos lados, USING simple y multi-columna, error de columna USING inexistente,
NATURAL sin comunes = CROSS, natural encadenado con join explícito) y `parser_test.go`
(`TestParseNaturalJoin`, `TestParseJoinUsing`). Regresión de joins con `ON` sin cambios
(`test-multi-join`, `test-where-operators`) + barrido de integración ✅. README actualizado.

### Session: August 22, 2026 - FocusDB Studio v2 (GUI integral) + fixes de motor

**Objetivo**: mejora mayor de la GUI en 4 frentes (plan aprobado en
`~/.claude/plans/curious-baking-sunrise.md`): Editor pro, Resultados pro, Explorador de datos,
Diagrama ER pro. CodeMirror offline. Monolito → módulos ES.

**Status**: ✅ COMPLETED (Fases 0–6)

**F0 — Refactor de base**: `static/index.html` (monolito 2156 líneas) partido en
`index.html` + `css/app.css` + `js/{state,icons,dom,api,editor,tabs,results,sidebar,explorer,
diagram,app}.js` (módulos ES; CodeMirror sigue como script clásico global; puente
`Object.assign(window, {...})` para los handlers inline). CodeMirror 5.65.16 **vendoreado** en
`static/vendor/codemirror/` (core+dracula+sql+addons hint/edit/search/dialog+LICENSE, 286KB) —
la GUI funciona sin red. Middleware `withStaticHeaders` (vendor immutable 24h, resto no-cache).

**F1 — Backend API** (`gui_api.go` nuevo; `gui_server.go` adelgazado a mux+middlewares):
- `POST /api/script`: un resultado por statement `{index,sql,tag,columns,rows,elapsedMs,truncated}`
  + `failedIndex`/`failedSql`/`error`; el SQL por statement sale del parser (nuevo accessor
  `Parser.Pos()`, offsets exactos — cubre dollar-quoted).
- Cancelación real: `handler.go: HandleWithDatabaseCtx(ctx,…)` pasa `r.Context()` +
  `context.WithTimeout` (flag `-query-timeout`, default 60s) a `executor.Execute`. El path wire
  (`Handle`/`HandleWithDatabase`) delega con `context.Background()` — sin cambios.
- `/api/schema` reconstruido sobre `GetTablesForDiagram` + `GetAllViewsInSchema`: arregla el bug
  `notNull` siempre-false; añade `identity/isPK/isFK/isUnique/ordinal` por columna, `rowCount`,
  `indexes[]`, `viewDefinition`; orden determinista (`sort.Slice`) — el árbol ya no se reordena.
- `/api/objects`: + `bodyText` (triggers/jobs/procedures), `forEachRow`, `lastRun`/`nextRun`.
- `/api/diagram`: + `pk[]` (PK compuesta), `rowCount`, `indexes`, `notNull/identity/isUnique`.
- `GET /api/table-data?table&offset&limit` (cap 500): página + `total` + `pk` + metadata de
  columnas; 400 vistas, 404 inexistente. Escrituras del explorador van por `/api/query`.
- `maxRows`+`truncated` opcionales en query/script (compat total sin ellos). `withRecover`,
  `withMethod`, `http.Server{ReadHeaderTimeout}`. Borrado `userInfoResult` (muerto).

**F2 — Editor pro**: pestañas con `CodeMirror.Doc`+`swapDoc` (persisten en
`focusdb.tabs.v1`, Ctrl+T nueva); historial persistente (`focusdb.history.v1`, dedup);
Ctrl+F búsqueda (addons vendoreados, diálogo tematizado); autocompletado con alias del
statement (regex FROM/JOIN) + snippets; `runAll` → `/api/script` con acordeón por statement,
subrayado del fallido (marcas separadas de las de sintaxis para que el debounce de validación
no las borre) y botón **Cancelar** (AbortController).

**F3 — Resultados pro**: pipeline filtro (debounce 150ms) → sort → render paginado (bloques de
500 + append sin re-render); export CSV (RFC 4180 + BOM) y JSON del conjunto filtrado; dblclick
→ modal de celda (pretty-print JSON); copiar fila como INSERT (tabla por heurística del FROM);
badge `truncated`; `cmpCell` numérico para strings de `sanitizeRows`.

**F4 — Explorador de datos** (`js/explorer.js` + vista overlay): grilla server-side paginada
(100/página), edición inline por PK (dblclick→input→`UPDATE … WHERE pk=…`), alta con formulario
generado de columnas (IDENTITY deshabilitada, NOT NULL marcado), borrado con confirmación; sin
PK → solo lectura. Badges PK/ID/NN en encabezados; rowCount + acceso desde el árbol lateral.

**F5 — Diagrama v2**: persistencia `focusdb.diagram.v1.<sig>` (posiciones+zoom+compacto por
firma djb2 del schema; reentrar no re-organiza; `resetDiagramLayout` limpia storage); drag
O(aristas incidentes) — `moveTable` actualiza solo transform + paths (`edgesByTable`), la
búsqueda/selección sobreviven; **Pointer Events** unificados (pan/drag/pinch-zoom táctil,
`touch-action:none`); self-FK como bucle lateral; cardinalidad `1`/`N`; rowCount en header;
badges `ix`/`u` por columna; **minimapa** con viewport navegable; modo compacto real (W 210→150,
re-render); zoom con clamp también en rueda [0.1,3]; export SVG/PNG sin `.fk-label` fantasma y
con try/catch+toast; layout AABB consciente del alto de cajas, troceado en rAF si N>25;
`fitDiagram`/bounds unificados; tooltip sin params muertos.

**Fixes de motor descubiertos durante la fase** (misma familia de tipos mixtos que FK-IDENTITY):
- `executor/where.go: whereMatches` — `=`/`<>` usaban `==` estricto de Go: `UPDATE/DELETE …
  WHERE id = 1` sobre PK IDENTITY (int) con literal ("1") afectaba **0 filas** (SELECT solo
  funcionaba por el fast-path del índice, que compara claves string). Ahora todos los operadores
  comparan por forma canónica vía `compareCells` (consistente con índices, JOIN ON y FK).
- `catalog.GetProcedure` no copiaba `BodyText` → los procedures se **persistían sin cuerpo
  canónico** (SaveProcedure serializa la copia). Corregido + `Load{Procedure,Trigger,Job}` ahora
  reciben y conservan `bodyText` en la recarga (antes memoria lo perdía tras reinicio).

**Verificación**: build/vet/test ✅; suite de integración **14/14** ✅; E2E browser: offline
(0 requests CDN), pestañas+historial sobreviven reload, script con error localizado en editor,
explorador CRUD completo (update/insert/delete reales), diagrama con persistencia/self-FK/
minimapa/compacto/clamp, 700 filas → primer render 127ms + paginación, consola limpia.
README actualizado (sección GUI completa + flags).

### Session: September 5, 2026 - Funciones de ventana + QUALIFY (pipeline de SELECT unificado)

**Objetivo**: agregar `QUALIFY` al motor. Como no existían funciones de ventana (ni `HAVING`),
la entrega incluye `ROW_NUMBER/RANK/DENSE_RANK` y `COUNT/SUM/AVG/MIN/MAX ... OVER (PARTITION BY …
ORDER BY …)`, y QUALIFY por alias, inline y sobre `GROUP BY`.

**Status**: ✅ COMPLETED

**Cambios**:
- **token.go**: `QUALIFY`, `OVER`, `PARTITION`. `ROW_NUMBER/RANK/DENSE_RANK` siguen siendo
  identificadores (como `SUM`), reconocidos por nombre (`isRankingFunc`).
- **ast.go**: `WindowFunc{Func, Arg, PartitionBy, OrderBy}`; `Identifier.Window *WindowFunc`
  (item de ventana = `Name:""`, `Alias: salida`); `Select.Qualify *WhereClause`;
  `WhereClause.Leaves()`. Campos nuevos → gob backward-compat.
- **parser**: `parseWindowCall` (`FUNC(arg) [OVER (...)]`, anidado `SUM(SUM(x)) OVER ()`),
  `parseColumnRef`/`parseColumnRefList` (columna o texto de agregado; usados por GROUP BY,
  ORDER BY, PARTITION BY y hojas de QUALIFY) y `parseQualifyClause` (flag `p.inQualify` que
  permite ventanas/agregados en `parseWherePredicate`; errores dicen `QUALIFY`). En
  `parseSelectItem` la rama de agregados delega en `parseWindowCall`. Beneficio colateral:
  `ORDER BY SUM(monto) DESC` ahora parsea (antes rompía).
- **executor — pipeline unificado** (`select_pipeline.go`, nuevo): `rowset{cols,rows,qualified}`
  + `finishSelect` = `groupRows` → `computeWindows` → QUALIFY (`evalWhere`) → ORDER BY
  (`queryutil.SortRowsByKeys`) → `projectRows` → DISTINCT → LIMIT. Las rutas tabla-simple y JOIN
  solo producen el rowset post-WHERE y delegan. Eliminadas `executeSelectStar`, `projectColumns`,
  `executeGroupedSelect`, `aggregateAllRows`, `aggregateByGroups`, `joinVirtualTable`,
  `applyJoinOrderBy`, `hasAggregates`, `colSpec` (salda la duplicación de `REFACTOR.md`).
  `groupRows` emite las columnas del select-list (name=alias, `expr`=texto fuente) más columnas
  **ocultas** para agregados/columnas que ventanas, QUALIFY u ORDER BY referencian sin proyectar.
- **executor/window.go** (nuevo): `evalWindow` particiona por clave `%v` (mismo criterio que
  GROUP BY), ordena índices con `queryutil.CompareByKeys`, detecta pares; ranking + agregados
  con frame por defecto (`RANGE UNBOUNDED PRECEDING → CURRENT ROW`, acumulado con pares).
  Las ventanas inline de QUALIFY se materializan como columnas ocultas `__qualify_N` y el
  predicado se **copia** con `substituteLeaves` (el AST de una vista es compartido: no mutar).
  Filas ampliadas con slices nuevos (las de origen pueden compartir backing array).
- **queryutil**: `OrderKey`, `CompareByKeys`, `SortRowsByKeys` (estable); `ApplyOrderBy` delega.
- **GUI** (`editor.js`): modo SQL con keywords extra (`sqlMode()` sobre `resolveMode`, sin
  tocar el vendor), snippets `ROW_NUMBER() OVER (...)`/`QUALIFY`, keywords en `aliasTables`.

**Cambios de comportamiento a conocer**:
- `ORDER BY` de columna inexistente ahora **falla** (`ORDER BY: column X not found`); antes se
  ignoraba en silencio. Acepta alias del SELECT y texto de agregado.
- `resolveJoinColumn` sin calificar: mensaje `column X not found` (antes "…in either table").
- Ventanas en `WHERE` no están permitidas (error de parseo), como en el estándar.

**Verificación**: `go build/vet/test ./internal/...` ✅; `parser_test.go` +8 tests;
`cmd/test-qualify` (40 casos: ranking/pares, agregados OVER con y sin ORDER BY, QUALIFY alias/
inline/compuesto/`SELECT *`/DISTINCT/LIMIT, GROUP BY con agregado proyectado y no proyectado,
`SUM(SUM(x)) OVER ()`, JOIN calificado, CTE, vista re-ejecutada, errores) ✅; regresión de
integración de SELECT ✅.

**Revisión de código (8 ángulos) y correcciones aplicadas antes del commit**:
- Proyección **posicional**: `selectItemIndexes` liga cada item del SELECT a su índice antes de
  anexar ventanas (un alias igual a una columna base ya no la sombrea; `SELECT monto,
  ROW_NUMBER() ... AS monto` devolvía el número de fila en ambas columnas). Las ventanas de
  QUALIFY se resuelven por índice (`hidden` map), no por nombre `__qualify_N`.
- Reglas estrictas: un agregado solo en ORDER BY/QUALIFY/OVER sin GROUP BY ni agregado en el
  SELECT es error (antes colapsaba la consulta a una fila); una columna no agrupada referenciada
  fuera del SELECT en consulta agrupada es error `must appear in GROUP BY` (antes tomaba el
  primer valor del grupo y `SUM(monto) OVER (PARTITION BY categoria)` sobre GROUP BY daba
  cifras falsas). La leniencia "primer valor" se conserva solo para el select-list.
- `errAmbiguousColumn` (sentinel): la ambigüedad en JOIN se reporta aunque otro item haya
  activado `AllowMissing`; `GROUP BY` envuelve el error real con `%w`.
- `ROW_NUMBER() OVER () * 2` → placeholder NULL (antes descartaba el `* 2` en silencio);
  `ORDER BY` sobre alias de placeholder se ignora en vez de fallar.
- `evalWindow` con acumulador incremental (`windowAccumulator`): lineal en vez de O(n²).
- Memoización del `resolve` de QUALIFY por consulta; proyección identidad sin copiar filas.
- GUI: MIME propio `text/x-focusdb` (`CodeMirror.defineMIME`) compartido por `editor.js` y
  `tabs.js` — antes las pestañas nuevas perdían el resaltado de QUALIFY/OVER.
- Limpieza: `queryutil.ApplyOrderBy` eliminado (sin llamadores); `parseSelect` usa
  `isFunctionCallStart`. Documentado en README que QUALIFY/OVER/PARTITION son reservadas.

**Fuera de alcance / follow-ups**: `HAVING` (filtro post-`groupRows`, misma infraestructura),
`LAG/LEAD/NTILE/FIRST_VALUE`, frames explícitos `ROWS/RANGE BETWEEN`, `SELECT *, ventana`
(limitación previa del parser con `*`), cláusula `WINDOW w AS (...)`.
