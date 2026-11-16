# Indexing Guide

This document describes how indexing works in FocusDB, when it helps, and current limits.

## Supported Syntax

```sql
CREATE INDEX index_name ON table_name (column_name);
CREATE INDEX index_name_composite ON table_name (column_a, column_b);
DROP INDEX index_name ON table_name;
```

## What Is Optimized Today

Index usage covers equality **and range** filters:

- `WHERE column = value` (equality)
- `WHERE column > value`, `<`, `>=`, `<=` (range), and `WHERE column BETWEEN a AND b` (bounded range, inclusive) — on a single-column index over that column.
- Composite indexes are supported for equality on the first indexed column (prefix).

When a matching index exists, the engine uses it to resolve row positions instead of scanning all rows. Range lookups binary-search a per-index **sorted key list** kept in sync on insert and rebuild, so a range costs `O(log k + matches)` instead of a full scan.

Ordering is numeric when both operands parse as numbers, lexicographic otherwise — the exact comparator the executor uses, so the index path returns the same rows a scan would (e.g. `n > 9` yields `10..20`, not the string order).

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

### 3) Range queries also use the index

```sql
SELECT * FROM users WHERE age > 30;
SELECT * FROM users WHERE age BETWEEN 20 AND 40;
```

Both use `idx_users_age` via binary search over the sorted keys (single-column index).

## Current Limitations

- Range optimization applies to **single-column** indexes; on a composite index only equality on the first-column prefix is optimized.

## Regression Check

After index-related changes, run:

```bash
go run ./cmd/test-index
go run ./cmd/test-drop-index
go run ./cmd/test-range-index
go test -vet=off ./internal/...
go test -vet=off ./...
```
