package ast

type Statement interface {
	stmtNode()
}

type Identifier struct {
	Name  string
	Alias string
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

type CreateDatabase struct {
	Name Identifier
}

func (CreateDatabase) stmtNode() {}

type CreateSchema struct {
	Name string
}

func (CreateSchema) stmtNode() {}

type DropTable struct {
	Table Identifier
}

func (DropTable) stmtNode() {}

type DropSchema struct {
	Name string
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
	Join         *JoinClause
	Where        *WhereClause
	GroupBy      []Identifier
	OrderBy      []OrderByClause
	Limit        int
	Offset       int
	Star         bool
	Distinct     bool
	AllowMissing bool
}

func (Select) stmtNode() {}

type JoinClause struct {
	Type  string // "INNER", "LEFT", "RIGHT"
	Table Identifier
	Left  Identifier
	Right Identifier
}

type SelectFunction struct {
	Name string
}

func (SelectFunction) stmtNode() {}

type WhereClause struct {
	Column Identifier
	Value  Literal
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
