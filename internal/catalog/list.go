package catalog

import (
	"dbf/internal/ast"
	"strings"
)

type TriggerInfo struct {
	Name       string
	Timing     string
	Event      string
	Table      string
	ForEachRow bool
}

type ProcedureInfo struct {
	Name       string
	Parameters []ast.Parameter
}

// isSystemTable reports whether a table name (short, within its schema) belongs
// to an internal catalog and should be hidden from user-facing views.
func isSystemTable(name string) bool {
	return strings.HasPrefix(name, "pg_") ||
		strings.HasPrefix(name, "pg_catalog.") ||
		strings.HasPrefix(name, "information_schema.")
}

// GetAllTriggers returns a flat list of all triggers across all tables.
func (c *Catalog) GetAllTriggers() []TriggerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []TriggerInfo
	for _, triggers := range c.triggers {
		for _, t := range triggers {
			result = append(result, TriggerInfo{
				Name:       t.Name,
				Timing:     t.Timing,
				Event:      t.Event,
				Table:      t.Table,
				ForEachRow: t.ForEachRow,
			})
		}
	}
	return result
}

// DiagramTable holds all data needed to render a table in the schema diagram.
type DiagramTable struct {
	Schema      string
	Name        string
	Columns     []Column
	Constraints []Constraint
}

// GetTablesForDiagram returns tables with their full column + constraint info for a given schema.
func (c *Catalog) GetTablesForDiagram(schema string) []DiagramTable {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if schema == "" {
		schema = "public"
	}
	tablesInSchema, ok := c.tables[schema]
	if !ok {
		return nil
	}

	result := make([]DiagramTable, 0, len(tablesInSchema))
	for name, t := range tablesInSchema {
		if isSystemTable(name) {
			continue
		}
		result = append(result, DiagramTable{
			Schema:      schema,
			Name:        name,
			Columns:     append([]Column(nil), t.Columns...),
			Constraints: append([]Constraint(nil), t.Constraints...),
		})
	}
	return result
}

// GetAllProcedures returns a list of all stored procedures.
func (c *Catalog) GetAllProcedures() []ProcedureInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]ProcedureInfo, 0, len(c.procedures))
	for _, p := range c.procedures {
		result = append(result, ProcedureInfo{
			Name:       p.Name,
			Parameters: append([]ast.Parameter(nil), p.Parameters...),
		})
	}
	return result
}
