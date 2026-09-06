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
	Values  []Literal
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
}

func (Select) stmtNode() {}

type JoinClause struct {
	Type    string // "INNER", "LEFT", "RIGHT", "FULL", "CROSS"
	Table   Identifier
	Left    Identifier
	Right   Identifier
	Natural bool     // NATURAL JOIN: join on all common column names (no ON)
	Using   []string // JOIN ... USING (col, ...): join on the named common columns
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
	Operator string // "=", "<>", "<", ">", "<=", ">=" (default "=")
	Value    Literal

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

type Update struct {
	Table  Identifier
	Column Identifier
	Value  Literal
	Where  *WhereClause
}

func (Update) stmtNode() {}

type Delete struct {
	Table Identifier
	Where *WhereClause
}

func (Delete) stmtNode() {}

type Set struct {
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
	Name       Identifier
	Timing     string // "BEFORE", "AFTER", "INSTEAD OF"
	Event      string // "INSERT", "UPDATE", "DELETE"
	Table      Identifier
	ForEachRow bool
	Body       []Statement
	BodyText   string // Verbatim SQL of the body, for stable persistence
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
