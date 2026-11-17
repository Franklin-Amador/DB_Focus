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
	// DefaultKind/DefaultValue describe a column DEFAULT. DefaultKind is "" when
	// there is no default; otherwise it is the literal kind ("string", "number",
	// "bool", "null") or "ident" for a marker such as CURRENT_TIMESTAMP, with the
	// text in DefaultValue. Adding these fields is gob/JSON backward-compatible.
	DefaultKind  string
	DefaultValue string
}

type Constraint struct {
	Type            string
	ColumnName      string
	ReferencedTable string
	ReferencedCol   string
	// OnDelete and OnUpdate hold the FK referential action ("CASCADE",
	// "SET NULL", "RESTRICT", "NO ACTION"). Empty means the default, RESTRICT.
	// Adding these fields is gob/JSON backward-compatible (old data decodes to
	// the empty string, i.e. RESTRICT — the prior behavior).
	OnDelete string
	OnUpdate string
	// Name is the optional constraint name (CONSTRAINT fk_x ...), for messages.
	Name string
	// CheckExpr holds the raw predicate text of a CHECK constraint
	// (Type == "CHECK"), e.g. "edad >= 18". It is re-parsed at enforcement time.
	CheckExpr string
}

type Index struct {
	Name        string
	ColumnNames []string
	Unique      bool
	Values      map[string][]int
	// sortedKeys holds the distinct keys of Values in ascending canonical order
	// (compareIndexKeys), enabling range lookups by binary search. It is derived
	// state, kept in sync with Values on insert and rebuild; it is never
	// persisted (rebuilt from rows on load).
	sortedKeys []string
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
	Event      string   // first event (backward compatibility)
	Events     []string // all events for a multi-event trigger
	Table      string
	ForEachRow bool
	// UpdateColumns are the columns of an `UPDATE OF col, ...` trigger (stored;
	// the trigger fires on any UPDATE of the table).
	UpdateColumns []string
	// ExecRoutine / ExecSchema name the function or procedure of a PostgreSQL
	// `EXECUTE FUNCTION fn()` trigger. When set, the trigger runs that routine's
	// body on fire; the inline Body is empty.
	ExecRoutine string
	ExecSchema  string
	Body        []ast.Statement
	BodyText    string // Canonical AST-independent body SQL (re-parsed on load)
}

// MatchesEvent reports whether the trigger fires for the given event, honoring a
// multi-event trigger (Events) and falling back to the single Event field.
func (t *Trigger) MatchesEvent(event string) bool {
	if len(t.Events) > 0 {
		for _, e := range t.Events {
			if e == event {
				return true
			}
		}
		return false
	}
	return t.Event == event
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

// Function is a practical subset of a PostgreSQL function: a set-returning or
// scalar function whose body is a single SELECT (captured as Query/QueryText).
// It is run when the function is referenced in a FROM clause.
type Function struct {
	Name        string
	Schema      string
	Parameters  []ast.Parameter
	Returns     []Column // RETURNS TABLE columns (output schema)
	SetOf       bool
	ReturnsType string
	Query       *ast.Select
	QueryText   string // canonical SELECT SQL, re-parsed on load
}

type Catalog struct {
	name       string                       // database this catalog belongs to
	cluster    *Cluster                     // owning cluster (nil when detached)
	tables     map[string]map[string]*Table // schema -> table -> *Table
	views      map[string]map[string]*View  // schema -> view -> *View
	procedures map[string]*Procedure
	functions  map[string]*Function // key: "schema.name"
	triggers   map[string][]*Trigger // key: table name, value: triggers for that table
	jobs       map[string]*Job
	mu         sync.RWMutex
}
