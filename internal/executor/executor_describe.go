package executor

import (
	"context"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/constants"
)

func init() {
	registerExec((*Executor).executeDescribe)
}

// executeDescribe handles `DESCRIBE table` (and its `DESC table` alias),
// returning one row per column with its type, nullability, key role
// (PRI/UNI/MUL), and identity flag — the shape clients expect from a
// MySQL-style DESCRIBE. The schema is read from Table.Alias (DDL convention,
// see AGENTS.md gotcha #5).
func (e *Executor) executeDescribe(ctx context.Context, stmt *ast.Describe) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	schema := schemaOrPublic(stmt.Table.Alias)
	table, err := e.catalog.GetTable(stmt.Table.Name, schema)
	if err != nil {
		return nil, err
	}

	keyRole := columnKeyRoles(table)

	rows := make([][]interface{}, 0, len(table.Columns))
	for _, col := range table.Columns {
		nullable := "YES"
		if col.NotNull {
			nullable = "NO"
		}
		identity := "NO"
		if col.Identity {
			identity = "YES"
		}
		rows = append(rows, []interface{}{
			col.Name,
			col.Type,
			nullable,
			keyRole[col.Name],
			identity,
		})
	}

	return &Result{
		Columns: []string{"Column", "Type", "Null", "Key", "Identity"},
		Rows:    rows,
		Tag:     constants.ResultDescribe,
	}, nil
}

// keyRolePriority ranks the key annotations so a column that is both a
// primary key and a foreign key is reported as the stronger role (PRI).
var keyRolePriority = map[string]int{"": 0, "MUL": 1, "UNI": 2, "PRI": 3}

// columnKeyRoles maps each column of a table to its key annotation:
// "PRI" for a primary-key column, "UNI" for a unique column, and "MUL" for a
// column that participates in a foreign key (it references another table).
// The highest-priority role wins. Columns with no key role map to "".
func columnKeyRoles(table *catalog.Table) map[string]string {
	roles := make(map[string]string)
	assign := func(col, role string) {
		if keyRolePriority[role] > keyRolePriority[roles[col]] {
			roles[col] = role
		}
	}
	for _, c := range table.Constraints {
		switch c.Type {
		case constants.ConstraintForeignKey:
			assign(c.ColumnName, "MUL")
		case constants.ConstraintUnique:
			assign(c.ColumnName, "UNI")
		case constants.ConstraintPrimaryKey:
			assign(c.ColumnName, "PRI")
		}
	}
	return roles
}
