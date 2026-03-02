package parser

type TokenType string

const (
	TokenEOF          TokenType = "EOF"
	TokenIdent        TokenType = "IDENT"
	TokenNumber       TokenType = "NUMBER"
	TokenString       TokenType = "STRING"
	TokenDollarString TokenType = "DOLLAR_STRING"
	TokenComma        TokenType = ","
	TokenLParen       TokenType = "("
	TokenRParen       TokenType = ")"
	TokenStar         TokenType = "*"
	TokenSemicolon    TokenType = ";"
	TokenEq           TokenType = "="
	TokenDot          TokenType = "."
	TokenCast         TokenType = "::"
	TokenNotEq        TokenType = "<>"
	TokenLt           TokenType = "<"
	TokenGt           TokenType = ">"

	TokenAs         TokenType = "AS"
	TokenCount      TokenType = "COUNT"
	TokenCase       TokenType = "CASE"
	TokenWhen       TokenType = "WHEN"
	TokenThen       TokenType = "THEN"
	TokenElse       TokenType = "ELSE"
	TokenEnd        TokenType = "END"
	TokenJoin       TokenType = "JOIN"
	TokenOn         TokenType = "ON"
	TokenInner      TokenType = "INNER"
	TokenLeft       TokenType = "LEFT"
	TokenRight      TokenType = "RIGHT"
	TokenFull       TokenType = "FULL"
	TokenOuter      TokenType = "OUTER"
	TokenCross      TokenType = "CROSS"
	TokenSelect     TokenType = "SELECT"
	TokenDistinct   TokenType = "DISTINCT"
	TokenFrom       TokenType = "FROM"
	TokenWhere      TokenType = "WHERE"
	TokenGroup      TokenType = "GROUP"
	TokenBy         TokenType = "BY"
	TokenCreate     TokenType = "CREATE"
	TokenTable      TokenType = "TABLE"
	TokenDatabase   TokenType = "DATABASE"
	TokenInsert     TokenType = "INSERT"
	TokenInto       TokenType = "INTO"
	TokenValues     TokenType = "VALUES"
	TokenSet        TokenType = "SET"
	TokenUpdate     TokenType = "UPDATE"
	TokenDelete     TokenType = "DELETE"
	TokenPrimary    TokenType = "PRIMARY"
	TokenKey        TokenType = "KEY"
	TokenForeign    TokenType = "FOREIGN"
	TokenReferences TokenType = "REFERENCES"
	TokenUnique     TokenType = "UNIQUE"
	TokenNot        TokenType = "NOT"
	TokenNull       TokenType = "NULL"
	TokenWith       TokenType = "WITH"
	TokenOwner      TokenType = "OWNER"
	TokenEncoding   TokenType = "ENCODING"
	TokenConnection TokenType = "CONNECTION"
	TokenLimit      TokenType = "LIMIT"
	TokenIs         TokenType = "IS"
	TokenTemplate   TokenType = "TEMPLATE"
	TokenTrue       TokenType = "TRUE"
	TokenFalse      TokenType = "FALSE"
	TokenIdentity   TokenType = "IDENTITY"
	TokenProcedure  TokenType = "PROCEDURE"
	TokenBegin      TokenType = "BEGIN"
	TokenCall       TokenType = "CALL"
	TokenDeclare    TokenType = "DECLARE"
	TokenReturn     TokenType = "RETURN"
	TokenTrigger    TokenType = "TRIGGER"
	TokenBefore     TokenType = "BEFORE"
	TokenAfter      TokenType = "AFTER"
	TokenInstead    TokenType = "INSTEAD"
	TokenOf         TokenType = "OF"
	TokenFor        TokenType = "FOR"
	TokenEach       TokenType = "EACH"
	TokenRow        TokenType = "ROW"
	TokenDrop       TokenType = "DROP"
	TokenOld        TokenType = "OLD"
	TokenNew        TokenType = "NEW"
	TokenJob        TokenType = "JOB"
	TokenSchedule   TokenType = "SCHEDULE"
	TokenEvery      TokenType = "EVERY"
	TokenMinute     TokenType = "MINUTE"
	TokenHour       TokenType = "HOUR"
	TokenDay        TokenType = "DAY"
	TokenAt         TokenType = "AT"
	TokenEnable     TokenType = "ENABLE"
	TokenDisable    TokenType = "DISABLE"
	TokenAlter      TokenType = "ALTER"
	TokenOrder      TokenType = "ORDER"
	TokenAsc        TokenType = "ASC"
	TokenDesc       TokenType = "DESC"
	TokenOffset     TokenType = "OFFSET"
	TokenSchema     TokenType = "SCHEMA"
	TokenUnit       TokenType = "UNIT"
	TokenEnabled    TokenType = "ENABLED"
	TokenInterval   TokenType = "INTERVAL"
)

type Token struct {
	Type    TokenType
	Literal string
	Pos     int
}

var keywords = map[string]TokenType{
	"SELECT":     TokenSelect,
	"DISTINCT":   TokenDistinct,
	"FROM":       TokenFrom,
	"WHERE":      TokenWhere,
	"GROUP":      TokenGroup,
	"BY":         TokenBy,
	"CREATE":     TokenCreate,
	"TABLE":      TokenTable,
	"DATABASE":   TokenDatabase,
	"INSERT":     TokenInsert,
	"INTO":       TokenInto,
	"VALUES":     TokenValues,
	"SET":        TokenSet,
	"UPDATE":     TokenUpdate,
	"DELETE":     TokenDelete,
	"AS":         TokenAs,
	"COUNT":      TokenCount,
	"CASE":       TokenCase,
	"WHEN":       TokenWhen,
	"THEN":       TokenThen,
	"ELSE":       TokenElse,
	"END":        TokenEnd,
	"JOIN":       TokenJoin,
	"ON":         TokenOn,
	"INNER":      TokenInner,
	"LEFT":       TokenLeft,
	"RIGHT":      TokenRight,
	"FULL":       TokenFull,
	"OUTER":      TokenOuter,
	"CROSS":      TokenCross,
	"PRIMARY":    TokenPrimary,
	"KEY":        TokenKey,
	"FOREIGN":    TokenForeign,
	"REFERENCES": TokenReferences,
	"UNIQUE":     TokenUnique,
	"NOT":        TokenNot,
	"NULL":       TokenNull,
	"WITH":       TokenWith,
	"OWNER":      TokenOwner,
	"ENCODING":   TokenEncoding,
	"CONNECTION": TokenConnection,
	"LIMIT":      TokenLimit,
	"IS":         TokenIs,
	"TEMPLATE":   TokenTemplate,
	"TRUE":       TokenTrue,
	"FALSE":      TokenFalse,
	"IDENTITY":   TokenIdentity,
	"PROCEDURE":  TokenProcedure,
	"BEGIN":      TokenBegin,
	"CALL":       TokenCall,
	"DECLARE":    TokenDeclare,
	"RETURN":     TokenReturn,
	"TRIGGER":    TokenTrigger,
	"BEFORE":     TokenBefore,
	"AFTER":      TokenAfter,
	"INSTEAD":    TokenInstead,
	"OF":         TokenOf,
	"FOR":        TokenFor,
	"EACH":       TokenEach,
	"ROW":        TokenRow,
	"DROP":       TokenDrop,
	"OLD":        TokenOld,
	"NEW":        TokenNew,
	"JOB":        TokenJob,
	"SCHEDULE":   TokenSchedule,
	"EVERY":      TokenEvery,
	"MINUTE":     TokenMinute,
	"HOUR":       TokenHour,
	"DAY":        TokenDay,
	"AT":         TokenAt,
	"ENABLE":     TokenEnable,
	"DISABLE":    TokenDisable,
	"ALTER":      TokenAlter,
	"ORDER":      TokenOrder,
	"ASC":        TokenAsc,
	"DESC":       TokenDesc,
	"OFFSET":     TokenOffset,
	"SCHEMA":     TokenSchema,
	"UNIT":       TokenUnit,
	"ENABLED":    TokenEnabled,
	"INTERVAL":   TokenInterval,
}
