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
