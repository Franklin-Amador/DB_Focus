package catalog

import (
	"dbf/internal/ast"
	"sync"
)

type Column struct {
	Name          string
	Type          string
	NotNull       bool
	Identity      bool
	IdentityValue int
}

type Constraint struct {
	Type            string
	ColumnName      string
	ReferencedTable string
	ReferencedCol   string
}

type Table struct {
	Name        string
	Columns     []Column
	Constraints []Constraint
	Rows        [][]interface{}
	mu          sync.RWMutex
}

type Procedure struct {
	Name       string
	Parameters []ast.Parameter
	Body       []ast.Statement
	mu         sync.RWMutex
}

type Trigger struct {
	Name       string
	Timing     string
	Event      string
	Table      string
	ForEachRow bool
	Body       []ast.Statement
}

type Job struct {
	Name     string
	Interval int
	Unit     string
	Body     []ast.Statement
	Enabled  bool
	LastRun  int64 // Unix timestamp of last execution
	Mu       sync.RWMutex
}

type Catalog struct {
	tables     map[string]map[string]*Table // schema -> table -> *Table
	procedures map[string]*Procedure
	triggers   map[string][]*Trigger // key: table name, value: triggers for that table
	jobs       map[string]*Job
	mu         sync.RWMutex
}
