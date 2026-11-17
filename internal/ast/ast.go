package ast

type Statement interface {
	stmtNode()
}

type Identifier struct {
	Name  string
	Alias string
	// Schema qualifies a SELECT table reference (FROM/JOIN): set by the parser
	// for "schema.table" and by ApplyDefaultSchema for the session's default
	// schema. Alias is then purely the table alias. DDL/DML object references
	// keep the older convention of carrying the schema in Alias.
	Schema string
	// Window is set when the identifier denotes a window-function call
	// (e.g. ROW_NUMBER() OVER (...)). Name is then empty and Alias carries the
	// output column name.
	Window *WindowFunc
	// IsCall marks a FROM reference that is a set-returning function call
	// (e.g. FROM fn_listar_cursos()); Args carries the call arguments.
	IsCall bool
	Args   []Literal
	// Subquery is set when the reference is a derived table (FROM (SELECT ...) x);
	// Name is then empty and Alias carries the derived-table alias.
	Subquery *Select
}

// WindowFunc is a window-function call: FUNC(arg) OVER (PARTITION BY ... ORDER BY ...).
// Func is upper-case (ROW_NUMBER, RANK, DENSE_RANK, COUNT, SUM, AVG, MIN, MAX).
// Arg is "" for ranking functions, "*" for COUNT(*), a column reference, or the
// canonical text of an aggregate ("SUM(monto)") when used over grouped rows.
type WindowFunc struct {
	Func        string
	Arg         string
	PartitionBy []Identifier
	OrderBy     []OrderByClause
}

type Literal struct {
	Kind  string
	Value string
}

// Constraint types
type Constraint interface {
	constraintNode()
}

type PrimaryKeyConstraint struct {
	ColumnName string
}

func (PrimaryKeyConstraint) constraintNode() {}

type ForeignKeyConstraint struct {
	ColumnName      string
	ReferencedTable string
	ReferencedCol   string
	// OnDelete and OnUpdate hold the referential action ("CASCADE", "SET NULL",
	// "RESTRICT", "NO ACTION"). An empty string means the default, RESTRICT.
	OnDelete string
	OnUpdate string
}

func (ForeignKeyConstraint) constraintNode() {}

type UniqueConstraint struct {
	ColumnName string
}

func (UniqueConstraint) constraintNode() {}

type NotNullConstraint struct {
	ColumnName string
}

func (NotNullConstraint) constraintNode() {}

// CheckConstraint is a CHECK (predicate) constraint, column- or table-level.
// Expr is the parsed predicate; Text is its verbatim source, persisted and
// re-parsed on load (AST-independent, like views). Name is optional.
type CheckConstraint struct {
	Name string
	Expr *WhereClause
	Text string
}

func (CheckConstraint) constraintNode() {}

type ColumnDef struct {
	Name        Identifier
	Type        string
	Constraints []Constraint
	NotNull     bool
	DefaultVal  *Literal
	Identity    bool
}

type CreateTable struct {
	Table       Identifier
	Columns     []ColumnDef
	Constraints []Constraint
}

func (CreateTable) stmtNode() {}

type CreateIndex struct {
	Name    Identifier
	Table   Identifier
	Columns []Identifier
	Unique  bool
}

func (CreateIndex) stmtNode() {}

type CreateView struct {
	Name        Identifier
	ColumnNames []string // Optional: explicit column names for view
	Query       *Select
	QueryText   string // Original SQL text of the SELECT, for stable persistence
	Replace     bool
	IfNotExists bool
}

func (CreateView) stmtNode() {}

type DropView struct {
	Name     Identifier
	IfExists bool
	Behavior string // "CASCADE", "RESTRICT", or "" (default)
}

func (DropView) stmtNode() {}

type DropIndex struct {
	Name  Identifier
	Table Identifier
}

func (DropIndex) stmtNode() {}

// Describe is `DESCRIBE table` (or its `DESC table` alias): it lists the
// columns of a table with their type, nullability, key role, and identity
// flag. Following the DDL convention (see AGENTS.md gotcha #5), the schema
// is carried in Table.Alias.
type Describe struct {
	Table Identifier
}

func (Describe) stmtNode() {}

type CreateDatabase struct {
	Name Identifier
}

func (CreateDatabase) stmtNode() {}

type CreateSchema struct {
	Name        string
	IfNotExists bool
}

func (CreateSchema) stmtNode() {}

type DropTable struct {
	Table    Identifier
	IfExists bool
	Behavior string // "CASCADE", "RESTRICT", or "" (default)
}

func (DropTable) stmtNode() {}

type DropSchema struct {
	Name     string
	IfExists bool
	Behavior string // "CASCADE", "RESTRICT", or "" (default)
}

func (DropSchema) stmtNode() {}

type DropDatabase struct {
	Name string
}

func (DropDatabase) stmtNode() {}

type Insert struct {
	Table   Identifier
	Columns []Identifier
	// Values holds the first row's literals. Kept for backward compatibility
	// (procedure parameter substitution, validation, gob-decoded old data).
	Values []Literal
	// Rows holds every VALUES tuple for multi-row inserts
	// (INSERT ... VALUES (...), (...), ...). The parser always fills it and
	// sets Values = Rows[0]; executor iterates Rows (falling back to [Values]).
	Rows [][]Literal
}

func (Insert) stmtNode() {}

type CTE struct {
	Name   Identifier
	Select *Select
}

type OrderByClause struct {
	Column    Identifier
	Direction string // "ASC" or "DESC" (default "ASC")
}

type Select struct {
	With         []CTE
	Columns      []Identifier
	Table        Identifier
	Join         *JoinClause   // first join (kept for backward-compat; mirrors Joins[0])
	Joins        []*JoinClause // full chain of joins (supports N-way joins)
	Where        *WhereClause
	GroupBy      []Identifier
	Having       *WhereClause // HAVING predicate, evaluated over the grouped rows
	Qualify      *WhereClause // QUALIFY predicate, evaluated after window functions
	OrderBy      []OrderByClause
	Limit        int
	Offset       int
	Star         bool
	Distinct     bool
	AllowMissing bool
	// Snowflake-style SELECT * modifiers (only meaningful when Star is true).
	StarExclude []string      // SELECT * EXCLUDE (a, b): columns to drop
	StarRename  []StarRename  // SELECT * RENAME (a AS x): columns to rename in the output
	StarILike   string        // SELECT * ILIKE 'pattern': keep only columns matching the pattern
	// GroupByAll is set by GROUP BY ALL: group by every non-aggregate select item.
	GroupByAll bool
}

// StarRename is one `old AS new` pair of a SELECT * RENAME clause.
type StarRename struct {
	From string
	To   string
}

func (Select) stmtNode() {}

// SetOp is a chain of set operations over SELECT operands, folded left to right.
// All[i] reports whether the operator between Selects[i] and Selects[i+1] is
// UNION ALL (keep duplicates) rather than UNION (deduplicate). OrderBy/Limit/
// Offset apply to the combined result. Column names come from the first operand.
type SetOp struct {
	Selects []*Select
	All     []bool
	OrderBy []OrderByClause
	Limit   int
	Offset  int
}

func (SetOp) stmtNode() {}

type JoinClause struct {
	Type    string // "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "ASOF"
	Table   Identifier
	Left    Identifier
	Right   Identifier
	Natural bool     // NATURAL JOIN: join on all common column names (no ON)
	Using   []string // JOIN ... USING (col, ...): join on the named common columns
	// ASOF JOIN fields (Type == "ASOF"): for each left row, the nearest right row
	// satisfying MATCH_CONDITION (MatchLeft MatchOp MatchRight) and the optional
	// ON equality (Left = Right). MatchOp is one of >, >=, <, <=.
	Asof       bool
	MatchLeft  Identifier
	MatchRight Identifier
	MatchOp    string
}

type SelectFunction struct {
	Name string
}

func (SelectFunction) stmtNode() {}

// WhereClause is a boolean predicate tree. A leaf node is a single comparison
// (Column Operator Value) and has Conj == "". A compound node combines two
// sub-predicates with Conj ("AND" or "OR") and leaves the leaf fields unused.
type WhereClause struct {
	// Leaf fields (used when Conj == "").
	Column   Identifier
	Operator string // "=", "<>", "<", ">", "<=", ">=", "BETWEEN" (default "=")
	Value    Literal
	// Value2 is the upper bound of a BETWEEN predicate (Operator == "BETWEEN");
	// Value is then the lower bound, both inclusive. Adding this field is
	// gob-backward-compatible (old blobs decode it to the zero Literal).
	Value2 Literal

	// Subquery fields. Sub is the right-hand SELECT for a scalar subquery
	// (col OP (SELECT ...)), an IN subquery (Operator == "IN") or an EXISTS
	// predicate (Operator == "EXISTS", Column empty). ValueList is the literal
	// list of an IN predicate (col IN (v1, v2, ...)). Negate marks NOT IN /
	// NOT EXISTS.
	Sub       *Select
	ValueList []Literal
	Negate    bool

	// Compound fields (used when Conj != "").
	Left  *WhereClause
	Conj  string // "AND" or "OR"
	Right *WhereClause
}

// IsLeaf reports whether the clause is a single comparison predicate rather
// than an AND/OR combination.
func (w *WhereClause) IsLeaf() bool {
	return w != nil && w.Conj == ""
}

// LeafColumns returns the column names referenced by every leaf predicate in
// the tree, in left-to-right order. Used for validation.
func (w *WhereClause) LeafColumns() []string {
	if w == nil {
		return nil
	}
	if w.Conj == "" {
		// EXISTS predicates have no left column; subquery/IN-list leaves resolve
		// their inner columns separately. Only validate a real left column name.
		if w.Column.Name == "" {
			return nil
		}
		return []string{w.Column.Name}
	}
	return append(w.Left.LeafColumns(), w.Right.LeafColumns()...)
}

// Leaves returns every leaf predicate of the tree in left-to-right order. The
// executor uses it to find window-function calls embedded in QUALIFY.
func (w *WhereClause) Leaves() []*WhereClause {
	if w == nil {
		return nil
	}
	if w.Conj == "" {
		return []*WhereClause{w}
	}
	return append(w.Left.Leaves(), w.Right.Leaves()...)
}

// Assignment is one `column = value` pair of an UPDATE's SET clause.
type Assignment struct {
	Column Identifier
	Value  Literal
}

type Update struct {
	Table Identifier
	// Column and Value mirror the first assignment, kept for backward
	// compatibility (gob-decoded statements, single-column callers).
	Column Identifier
	Value  Literal
	// Assignments holds every `col = value` pair (UPDATE t SET a = 1, b = 2).
	// The parser always fills it and sets Column/Value = Assignments[0].
	Assignments []Assignment
	Where       *WhereClause
}

func (Update) stmtNode() {}

type Delete struct {
	Table Identifier
	Where *WhereClause
}

func (Delete) stmtNode() {}

// Set is a SET statement. Name is the variable (e.g. "search_path") and
// Values the tokens of the value list ("tienda", "public"). The executor
// ignores it; the wire server uses search_path for the session schema.
type Set struct {
	Name   string
	Values []string
}

func (Set) stmtNode() {}

type Parameter struct {
	Name Identifier
	Type string
}

type CreateProcedure struct {
	Name       Identifier
	Parameters []Parameter
	Body       []Statement
	BodyText   string // Verbatim SQL of the body, for stable persistence
}

func (CreateProcedure) stmtNode() {}

// CreateFunction is a practical subset of PostgreSQL's CREATE FUNCTION: a
// set-returning (RETURNS TABLE / SETOF) or scalar function whose body is a
// single RETURN QUERY <SELECT> (or a bare SELECT). The SELECT is captured as
// Query (+ QueryText for stable persistence); the engine runs it when the
// function is referenced in a FROM clause. General plpgsql is not supported.
type CreateFunction struct {
	Name        Identifier // schema carried in Alias (DDL convention)
	Parameters  []Parameter
	Returns     []Parameter // columns of RETURNS TABLE(...); empty for scalar/SETOF
	ReturnsType string      // RETURNS <type> / SETOF <type> (when not TABLE)
	SetOf       bool
	Query       *Select // the SELECT the function returns (set-returning functions)
	QueryText   string  // verbatim SELECT text, for stable persistence
	// Body / BodyText hold a statement body for a trigger or side-effecting
	// function (BEGIN INSERT ...; RETURN NEW; END) when it is not a single
	// RETURN QUERY <SELECT>. Query and Body are mutually exclusive.
	Body        []Statement
	BodyText    string
	Replace     bool
	IfNotExists bool
	Language    string
}

func (CreateFunction) stmtNode() {}

// Return is a `RETURN [expression]` statement inside a function/trigger body.
// The engine does not use the returned value (triggers ignore it; scalar return
// values are not evaluated), so Value holds the raw expression text and the
// statement executes as a no-op. It exists so bodies containing RETURN parse.
type Return struct {
	Value string
}

func (Return) stmtNode() {}

type DropFunction struct {
	Name     Identifier // schema in Alias
	IfExists bool
}

func (DropFunction) stmtNode() {}

type CallProcedure struct {
	Name      Identifier
	Arguments []Literal
}

func (CallProcedure) stmtNode() {}

type DropProcedure struct {
	Name Identifier
}

func (DropProcedure) stmtNode() {}

type CreateTrigger struct {
	Name   Identifier
	Timing string // "BEFORE", "AFTER", "INSTEAD OF"
	// Event is the first trigger event (kept for backward compatibility).
	// Events holds all of them for a multi-event trigger
	// (INSERT OR UPDATE OR DELETE).
	Event  string
	Events []string
	// UpdateColumns are the columns of an `UPDATE OF col, ...` trigger (parsed
	// and stored; the trigger still fires on any UPDATE of the table).
	UpdateColumns []string
	Table         Identifier
	ForEachRow    bool
	// ExecRoutine names the function/procedure of a PostgreSQL-style
	// `EXECUTE FUNCTION fn()` / `EXECUTE PROCEDURE fn()` trigger (schema in
	// Alias). Empty when the trigger has an inline BEGIN ... END body.
	ExecRoutine Identifier
	Body        []Statement
	BodyText    string // Verbatim SQL of the body, for stable persistence
}

func (CreateTrigger) stmtNode() {}

type DropTrigger struct {
	Name  Identifier
	Table Identifier
}

func (DropTrigger) stmtNode() {}

type CreateJob struct {
	Name     Identifier
	Interval int    // Number of units (1, 5, 10, etc.)
	Unit     string // "MINUTE", "HOUR", "DAY"
	Body     []Statement
	BodyText string // Verbatim SQL of the body, for stable persistence
	Enabled  bool
}

func (CreateJob) stmtNode() {}

type DropJob struct {
	Name Identifier
}

func (DropJob) stmtNode() {}

type AlterJob struct {
	Name   Identifier
	Action string // "ENABLE", "DISABLE"
}

func (AlterJob) stmtNode() {}

// AlterTable representa una sentencia ALTER TABLE
type AlterTable struct {
	Table  Identifier
	Action AlterAction
}

func (AlterTable) stmtNode() {}

// AlterAction es la interfaz para las acciones de ALTER TABLE
type AlterAction interface {
	alterActionNode()
}

// AddColumn representa ADD COLUMN
type AddColumn struct {
	Column ColumnDef
}

func (AddColumn) alterActionNode() {}

// DropColumn representa DROP COLUMN
type DropColumn struct {
	ColumnName string
}

func (DropColumn) alterActionNode() {}

// AlterColumn representa ALTER COLUMN (cambiar tipo)
type AlterColumn struct {
	ColumnName string
	NewType    string
}

func (AlterColumn) alterActionNode() {}

// RenameColumn representa RENAME COLUMN
type RenameColumn struct {
	OldName string
	NewName string
}

func (RenameColumn) alterActionNode() {}
