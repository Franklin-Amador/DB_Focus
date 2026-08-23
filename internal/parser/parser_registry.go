package parser

import "dbf/internal/ast"

// stmtParseFn parses one statement (or statement fragment) beginning at the
// current token and returns the resulting AST node.
type stmtParseFn func(*Parser) (ast.Statement, error)

// parseNoop consumes the current token and yields no statement. It is used for
// tokens that are valid but carry no statement at the top level: bare
// semicolons and stray END / dollar-quoted blocks left by some client chunking.
func (p *Parser) parseNoop() (ast.Statement, error) {
	p.next()
	return nil, nil
}

// These dispatch tables are populated in init() rather than as literal
// initializers: the parse functions they reference transitively refer back to
// the tables (parseCreate -> createParsers, ParseStatement -> topLevelParsers),
// which the compiler would otherwise reject as a static initialization cycle.
var (
	// topLevelParsers maps a statement's leading keyword to its parser.
	// Registering a new top-level statement is one entry here plus its parse
	// function — no central switch to edit.
	topLevelParsers map[TokenType]stmtParseFn

	// createParsers maps the object keyword after CREATE to its parser. VIEW
	// uses a wrapper because parseCreateView takes an explicit "replace" flag
	// (the CREATE OR REPLACE path is handled separately in parseCreate).
	createParsers map[TokenType]stmtParseFn

	// dropParsers maps the object keyword after DROP to its parser.
	dropParsers map[TokenType]stmtParseFn

	// alterParsers maps the object keyword after ALTER to its parser.
	alterParsers map[TokenType]stmtParseFn
)

func init() {
	topLevelParsers = map[TokenType]stmtParseFn{
		TokenWith:         (*Parser).parseSelect,
		TokenSelect:       (*Parser).parseSelect,
		TokenCreate:       (*Parser).parseCreate,
		TokenInsert:       (*Parser).parseInsert,
		TokenUpdate:       (*Parser).parseUpdate,
		TokenDelete:       (*Parser).parseDelete,
		TokenSet:          (*Parser).parseSet,
		TokenCall:         (*Parser).parseCall,
		TokenDrop:         (*Parser).parseDrop,
		TokenAlter:        (*Parser).parseAlter,
		TokenSemicolon:    (*Parser).parseNoop,
		TokenEnd:          (*Parser).parseNoop,
		TokenDollarString: (*Parser).parseNoop,
	}

	createParsers = map[TokenType]stmtParseFn{
		TokenTable:     (*Parser).parseCreateTable,
		TokenView:      func(p *Parser) (ast.Statement, error) { return p.parseCreateView(false) },
		TokenIndex:     (*Parser).parseCreateIndex,
		TokenSchema:    (*Parser).parseCreateSchema,
		TokenDatabase:  (*Parser).parseCreateDatabase,
		TokenProcedure: (*Parser).parseCreateProcedure,
		TokenTrigger:   (*Parser).parseCreateTrigger,
		TokenJob:       (*Parser).parseCreateJob,
	}

	dropParsers = map[TokenType]stmtParseFn{
		TokenTable:     (*Parser).parseDropTable,
		TokenIndex:     (*Parser).parseDropIndex,
		TokenView:      (*Parser).parseDropView,
		TokenSchema:    (*Parser).parseDropSchema,
		TokenDatabase:  (*Parser).parseDropDatabase,
		TokenProcedure: (*Parser).parseDropProcedure,
		TokenTrigger:   (*Parser).parseDropTrigger,
		TokenJob:       (*Parser).parseDropJob,
	}

	alterParsers = map[TokenType]stmtParseFn{
		TokenTable: (*Parser).parseAlterTable,
		TokenJob:   (*Parser).parseAlterJob,
	}
}
