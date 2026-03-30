# Indexing Guide

This document describes how indexing works in FocusDB, when it helps, and current limits.

## Supported Syntax

```sql
CREATE INDEX index_name ON table_name (column_name);
CREATE INDEX index_name_composite ON table_name (column_a, column_b);
DROP INDEX index_name ON table_name;
```

## What Is Optimized Today

Current index usage is focused on equality filters:

- `WHERE column = value`
- Composite indexes are supported. With current SQL filter capabilities, optimization applies to equality filters on the first indexed column.

When a matching index exists, the engine uses it to resolve row positions instead of scanning all rows.

## Lifecycle and Consistency

Indexes are kept consistent automatically during:

- `INSERT`
- `UPDATE`
- `DELETE`
- `ALTER TABLE ... RENAME COLUMN ...`
- `ALTER TABLE ... DROP COLUMN ...`

Persistence behavior:

- Index definitions are stored in persistent metadata.
- On restart, definitions are loaded and index values are rebuilt from table rows.

## Practical Usage

### 1) Create an index for a frequent equality filter

```sql
CREATE TABLE users (
  id INT PRIMARY KEY,
  email TEXT,
  age INT
);

CREATE INDEX idx_users_age ON users (age);
```

### 2) Query using equality

```sql
SELECT * FROM users WHERE age = 30;
```

### 3) Keep queries aligned with current optimization

Recommended pattern:

```sql
SELECT * FROM users WHERE age = 30;
```

Not optimized by current index implementation:

```sql
SELECT * FROM users WHERE age > 30;
SELECT * FROM users WHERE age BETWEEN 20 AND 40;
```

## Current Limitations

- No range index optimization (`>`, `<`, `BETWEEN`) for indexed scans
- Multi-column optimization is currently limited to first-column prefix matching for equality lookups

## Regression Check

After index-related changes, run:

```bash
go run ./cmd/test-index
go run ./cmd/test-drop-index
go test -vet=off ./internal/...
go test -vet=off ./...
```
