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

type Index struct {
	Name        string
	ColumnNames []string
	Values      map[string][]int
}

type Table struct {
	Name        string
	Columns     []Column
	Constraints []Constraint
	Indexes     map[string]*Index
	Rows        [][]interface{}
	mu          sync.RWMutex
}

type View struct {
	Name    string
	Columns []Column
	Query   *ast.Select
	// QueryText is the original SELECT SQL. When present it is the canonical,
	// AST-independent definition used for persistence (re-parsed on load).
	QueryText string
}

type Procedure struct {
	Name       string
	Parameters []ast.Parameter
	Body       []ast.Statement
	BodyText   string // Canonical AST-independent body SQL (re-parsed on load)
	mu         sync.RWMutex
}

type Trigger struct {
	Name       string
	Timing     string
	Event      string
	Table      string
	ForEachRow bool
	Body       []ast.Statement
	BodyText   string // Canonical AST-independent body SQL (re-parsed on load)
}

type Job struct {
	Name     string
	Interval int
	Unit     string
	Body     []ast.Statement
	BodyText string // Canonical AST-independent body SQL (re-parsed on load)
	Enabled  bool
	LastRun  int64 // Unix timestamp of last execution
	Mu       sync.RWMutex
}

type Catalog struct {
	name       string                       // database this catalog belongs to
	cluster    *Cluster                     // owning cluster (nil when detached)
	tables     map[string]map[string]*Table // schema -> table -> *Table
	views      map[string]map[string]*View  // schema -> view -> *View
	procedures map[string]*Procedure
	triggers   map[string][]*Trigger // key: table name, value: triggers for that table
	jobs       map[string]*Job
	mu         sync.RWMutex
}
