package parser

import (
	"fmt"
	"log"
	"strings"

	"dbf/internal/ast"
)

type Parser struct {
	l    *Lexer
	cur  Token
	peek Token
}

func NewParser(input string) *Parser {
	l := NewLexer(input)
	p := &Parser{l: l}
	p.cur = l.NextToken()
	p.peek = l.NextToken()
	return p
}

func (p *Parser) next() {
	p.cur = p.peek
	p.peek = p.l.NextToken()
}

func (p *Parser) AtEOF() bool {
	return p.cur.Type == TokenEOF
}

func (p *Parser) ParseStatement() (ast.Statement, error) {
	switch p.cur.Type {
	case TokenWith:
		// WITH starts a SELECT statement with CTEs
		return p.parseSelect()
	case TokenSelect:
		return p.parseSelect()
	case TokenCreate:
		return p.parseCreate()
	case TokenInsert:
		return p.parseInsert()
	case TokenUpdate:
		return p.parseUpdate()
	case TokenDelete:
		return p.parseDelete()
	case TokenSet:
		return p.parseSet()
	case TokenCall:
		return p.parseCall()
	case TokenDrop:
		return p.parseDrop()
	case TokenAlter:
		return p.parseAlter()
	case TokenSemicolon:
		p.next()
		return nil, nil
	case TokenEnd:
		// Ignore stray END tokens at top-level (can appear with some clients after dollar-quoted bodies)
		p.next()
		return nil, nil
	case TokenDollarString:
		// Ignore stray dollar-quoted blocks at top-level (client-side chunking can leave this token alone)
		p.next()
		return nil, nil
	default:
		return nil, p.errorf("unexpected token %s", p.cur.Type)
	}
}

func (p *Parser) parseSelect() (ast.Statement, error) {
	// Handle both WITH and SELECT starting tokens
	if p.cur.Type != TokenWith && p.cur.Type != TokenSelect {
		return nil, p.errorf("expected WITH or SELECT, got %s", p.cur.Type)
	}

	// Parse WITH clause (CTEs)
	var ctes []ast.CTE
	if p.cur.Type == TokenWith {
		p.next()
		for {
			// Parse CTE name
			if p.cur.Type != TokenIdent {
				return nil, p.errorf("expected CTE name")
			}
			cteName := ast.Identifier{Name: p.cur.Literal}
			p.next()

			// Expect AS
			if !p.expect(TokenAs) {
				return nil, p.errorf("expected AS after CTE name")
			}

			// Expect (
			if !p.expect(TokenLParen) {
				return nil, p.errorf("expected ( after AS")
			}

			// Parse SELECT statement
			if p.cur.Type != TokenSelect {
				return nil, p.errorf("expected SELECT in CTE")
			}
			cteSelect, err := p.parseSelect()
			if err != nil {
				return nil, err
			}
			selectStmt, ok := cteSelect.(*ast.Select)
			if !ok {
				return nil, p.errorf("CTE must be a SELECT statement")
			}

			// Expect )
			if !p.expect(TokenRParen) {
				return nil, p.errorf("expected ) after CTE SELECT")
			}

			ctes = append(ctes, ast.CTE{Name: cteName, Select: selectStmt})

			// Check for more CTEs
			if p.cur.Type == TokenComma {
				p.next()
				continue
			}
			break
		}
	}

	// Now we should be at SELECT
	if p.cur.Type != TokenSelect {
		return nil, p.errorf("expected SELECT after WITH clause")
	}
	p.next()

	if p.cur.Type == TokenIdent && p.peek.Type == TokenLParen {
		name := p.cur.Literal
		p.next()
		if !p.expect(TokenLParen) {
			return nil, p.errorf("expected (")
		}
		if !p.expect(TokenRParen) {
			return nil, p.errorf("expected )")
		}
		return &ast.SelectFunction{Name: name}, nil
	}

	stmt := &ast.Select{With: ctes}

	// Check for DISTINCT
	if p.cur.Type == TokenDistinct {
		stmt.Distinct = true
		p.next()
	}

	if p.cur.Type == TokenStar {
		stmt.Star = true
		p.next()
	} else {
		cols, allowMissing, err := p.parseSelectList()
		if err != nil {
			return nil, err
		}
		stmt.Columns = cols
		stmt.AllowMissing = allowMissing
	}

	if p.cur.Type != TokenFrom {
		return stmt, nil
	}

	tbl, join, err := p.parseFromAndJoin()
	if err != nil {
		return nil, err
	}
	stmt.Table = tbl
	if join != nil {
		stmt.Join = join
	}

	where, err := p.parseWhereClause()
	if err != nil {
		return nil, err
	}
	if where != nil {
		stmt.Where = where
	}

	grp, err := p.parseGroupByClause()
	if err != nil {
		return nil, err
	}
	stmt.GroupBy = grp

	ob, err := p.parseOrderByClause()
	if err != nil {
		return nil, err
	}
	stmt.OrderBy = ob

	lim, off, err := p.parseLimitOffset()
	if err != nil {
		return nil, err
	}
	stmt.Limit = lim
	stmt.Offset = off

	return stmt, nil
}

func (p *Parser) parseSelectList() ([]ast.Identifier, bool, error) {
	var cols []ast.Identifier
	allowMissing := false
	depth := 0
	exprIdx := 1

	for {
		id, allowDelta, stop, err := p.parseSelectItem(&depth, exprIdx)
		if err != nil {
			return nil, false, err
		}
		cols = append(cols, id)
		if allowDelta {
			allowMissing = true
		}
		exprIdx++
		if stop {
			return cols, allowMissing, nil
		}
	}
}

func trimQualifier(name string) string {
	idx := strings.LastIndex(name, ".")
	if idx == -1 {
		return name
	}
	return name[idx+1:]
}

func (p *Parser) parseCreateDatabase() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected database name")
	}
	stmt := &ast.CreateDatabase{Name: ast.Identifier{Name: p.cur.Literal}}
	p.next()

	// Parse optional WITH clause and database options
	if p.cur.Type == TokenWith {
		p.next()
		// Parse database options: OWNER, ENCODING, CONNECTION LIMIT, IS_TEMPLATE, etc.
		for p.cur.Type != TokenSemicolon && p.cur.Type != TokenEOF {
			switch p.cur.Type {
			case TokenOwner:
				p.next()
				if p.cur.Type == TokenEq {
					p.next()
				}
				// Skip owner value (ident or string)
				p.next()
			case TokenEncoding:
				p.next()
				if p.cur.Type == TokenEq {
					p.next()
				}
				// Skip encoding value
				p.next()
			case TokenConnection:
				p.next()
				if p.cur.Type == TokenLimit {
					p.next()
				}
				if p.cur.Type == TokenEq {
					p.next()
				}
				// Skip connection limit value
				p.next()
			case TokenIs:
				p.next()
				if p.cur.Type == TokenTemplate {
					p.next()
				}
				if p.cur.Type == TokenEq {
					p.next()
				}
				// Skip TRUE/FALSE or value
				if p.cur.Type == TokenTrue || p.cur.Type == TokenFalse || p.cur.Type == TokenIdent {
					p.next()
				}
			default:
				p.next()
			}
		}
	}

	return stmt, nil
}

func (p *Parser) parseCreateProcedure() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected procedure name")
	}
	stmt := &ast.CreateProcedure{Name: ast.Identifier{Name: p.cur.Literal}}
	p.next()

	// Parse parameters
	if !p.expect(TokenLParen) {
		return nil, p.errorf("expected ( after procedure name")
	}

	// Parse parameter list (name TYPE, name TYPE, ...)
	for p.cur.Type != TokenRParen && p.cur.Type != TokenEOF {
		if p.cur.Type != TokenIdent {
			return nil, p.errorf("expected parameter name")
		}
		paramName := ast.Identifier{Name: p.cur.Literal}
		p.next()

		if p.cur.Type != TokenIdent {
			return nil, p.errorf("expected parameter type")
		}
		paramType := p.cur.Literal
		p.next()

		stmt.Parameters = append(stmt.Parameters, ast.Parameter{
			Name: paramName,
			Type: paramType,
		})

		if p.cur.Type == TokenComma {
			p.next()
			continue
		}
		break
	}

	if !p.expect(TokenRParen) {
		return nil, p.errorf("expected ) after parameters")
	}

	if !p.expect(TokenAs) {
		return nil, p.errorf("expected AS after parameters")
	}

	// Support two body styles:
	// 1) Inline: AS BEGIN ... END
	// 2) Dollar-quoted: AS $$ BEGIN ... END; $$
	if p.cur.Type == TokenDollarString {
		// Parse the quoted content with an inner parser
		content := p.cur.Literal
		inner := NewParser(content)

		// Expect BEGIN inside the dollar-quoted content
		if !inner.expect(TokenBegin) {
			return nil, p.errorf("expected BEGIN inside dollar-quoted procedure body")
		}
		body, err := inner.parseStatementsBlock()
		if err != nil {
			return nil, err
		}
		stmt.Body = append(stmt.Body, body...)
		if inner.cur.Type == TokenEnd {
			inner.next()
			if inner.cur.Type == TokenSemicolon {
				inner.next()
			}
		} else if inner.cur.Type != TokenEOF {
			return nil, p.errorf("expected END inside dollar-quoted procedure body")
		}
		// consume the outer dollar string token
		p.next()
		return stmt, nil
	}

	if !p.expect(TokenBegin) {
		return nil, p.errorf("expected BEGIN")
	}

	// Parse body statements
	body, err := p.parseStatementsBlock()
	if err != nil {
		return nil, err
	}
	stmt.Body = append(stmt.Body, body...)

	if !p.expect(TokenEnd) {
		return nil, p.errorf("expected END")
	}

	return stmt, nil
}

func (p *Parser) parseCreateTrigger() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected trigger name")
	}
	stmt := &ast.CreateTrigger{Name: ast.Identifier{Name: p.cur.Literal}}
	p.next()

	// Parse timing (BEFORE/AFTER/INSTEAD)
	switch p.cur.Type {
	case TokenBefore:
		stmt.Timing = "BEFORE"
		p.next()
	case TokenAfter:
		stmt.Timing = "AFTER"
		p.next()
	case TokenInstead:
		p.next()
		if !p.expect(TokenOf) {
			return nil, p.errorf("expected OF after INSTEAD")
		}
		stmt.Timing = "INSTEAD OF"
	default:
		return nil, p.errorf("expected BEFORE, AFTER, or INSTEAD OF")
	}

	// Parse event (INSERT/UPDATE/DELETE)
	switch p.cur.Type {
	case TokenInsert:
		stmt.Event = "INSERT"
	case TokenUpdate:
		stmt.Event = "UPDATE"
	case TokenDelete:
		stmt.Event = "DELETE"
	default:
		return nil, p.errorf("expected INSERT, UPDATE, or DELETE")
	}
	p.next()

	// Expect ON
	if !p.expect(TokenOn) {
		return nil, p.errorf("expected ON")
	}

	// Parse table name
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected table name")
	}
	stmt.Table = ast.Identifier{Name: p.cur.Literal}
	p.next()

	// Parse FOR EACH ROW (optional, defaults to true)
	stmt.ForEachRow = true
	if p.cur.Type == TokenFor {
		p.next()
		if !p.expect(TokenEach) {
			return nil, p.errorf("expected EACH after FOR")
		}
		if !p.expect(TokenRow) {
			return nil, p.errorf("expected ROW after EACH")
		}
	}

	// Expect BEGIN
	if !p.expect(TokenBegin) {
		return nil, p.errorf("expected BEGIN")
	}

	// Parse body statements
	body, err := p.parseStatementsBlock()
	if err != nil {
		return nil, err
	}
	stmt.Body = append(stmt.Body, body...)

	if !p.expect(TokenEnd) {
		return nil, p.errorf("expected END")
	}

	return stmt, nil
}

func (p *Parser) parseDrop() (ast.Statement, error) {
	p.next()
	switch p.cur.Type {
	case TokenTable:
		return p.parseDropTable()
	case TokenSchema:
		return p.parseDropSchema()
	case TokenDatabase:
		return p.parseDropDatabase()
	case TokenProcedure:
		return p.parseDropProcedure()
	case TokenTrigger:
		return p.parseDropTrigger()
	case TokenJob:
		return p.parseDropJob()
	default:
		return nil, p.errorf("expected TABLE, SCHEMA, DATABASE, PROCEDURE, TRIGGER o JOB after DROP")
	}
}

// DROP PROCEDURE procedure_name[()]
func (p *Parser) parseDropProcedure() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected procedure name")
	}
	procName := ast.Identifier{Name: p.cur.Literal}
	p.next()

	// Optional parentheses for PostgreSQL-like syntax: DROP PROCEDURE name()
	if p.cur.Type == TokenLParen {
		p.next()
		if !p.expect(TokenRParen) {
			return nil, p.errorf("expected ) after procedure name")
		}
	}

	return &ast.DropProcedure{Name: procName}, nil
}

// DROP TABLE [schema.]table
func (p *Parser) parseDropTable() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected table name")
	}
	tableIdent := p.parseQualifiedIdent()
	return &ast.DropTable{Table: tableIdent}, nil
}

// DROP SCHEMA schema
func (p *Parser) parseDropSchema() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected schema name")
	}
	schema := p.cur.Literal
	p.next()
	return &ast.DropSchema{Name: schema}, nil
}

// DROP DATABASE database
func (p *Parser) parseDropDatabase() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected database name")
	}
	database := p.cur.Literal
	p.next()
	return &ast.DropDatabase{Name: database}, nil
}

// DROP TRIGGER trigger_name ON table_name
func (p *Parser) parseDropTrigger() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected trigger name")
	}
	triggerName := ast.Identifier{Name: p.cur.Literal}
	p.next()

	if !p.expect(TokenOn) {
		return nil, p.errorf("expected ON after trigger name")
	}
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected table name after ON")
	}
	tableName := ast.Identifier{Name: p.cur.Literal}
	p.next()

	return &ast.DropTrigger{Name: triggerName, Table: tableName}, nil
}

// DROP JOB job_name
func (p *Parser) parseDropJob() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected job name")
	}
	jobName := ast.Identifier{Name: p.cur.Literal}
	p.next()
	return &ast.DropJob{Name: jobName}, nil
}

func (p *Parser) parseCreate() (ast.Statement, error) {
	p.next()
	switch p.cur.Type {
	case TokenTable:
		return p.parseCreateTable()
	case TokenSchema:
		return p.parseCreateSchema()
	case TokenDatabase:
		return p.parseCreateDatabase()
	case TokenProcedure:
		return p.parseCreateProcedure()
	case TokenTrigger:
		return p.parseCreateTrigger()
	case TokenJob:
		return p.parseCreateJob()
	default:
		return nil, p.errorf("expected TABLE, SCHEMA, DATABASE, PROCEDURE, TRIGGER or JOB after CREATE")
	}
}

// CREATE SCHEMA schema
func (p *Parser) parseCreateSchema() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected schema name")
	}
	schema := p.cur.Literal
	p.next()
	return &ast.CreateSchema{Name: schema}, nil
}

func (p *Parser) parseCreateTable() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected table name")
	}
	tableIdent := p.parseQualifiedIdent()
	stmt := &ast.CreateTable{Table: tableIdent}
	// parseQualifiedIdent already advances past the identifier

	if !p.expect(TokenLParen) {
		return nil, p.errorf("expected (")
	}

	for {
		// Check for table-level constraints (FOREIGN KEY, PRIMARY KEY, UNIQUE, etc.)
		if p.cur.Type == TokenPrimary || p.cur.Type == TokenForeign || p.cur.Type == TokenUnique {
			constraint, err := p.parseTableLevelConstraint()
			if err != nil {
				return nil, err
			}
			stmt.Constraints = append(stmt.Constraints, constraint)
			if p.cur.Type == TokenComma {
				p.next()
				continue
			}
			break
		}

		// Parse column definition
		colDef, err := p.parseColumnDef()
		if err != nil {
			return nil, err
		}
		stmt.Columns = append(stmt.Columns, colDef)

		if p.cur.Type == TokenComma {
			p.next()
			continue
		}
		break
	}

	if !p.expect(TokenRParen) {
		return nil, p.errorf("expected )")
	}

	return stmt, nil
}

func (p *Parser) parseTableLevelConstraint() (ast.Constraint, error) {
	switch p.cur.Type {
	case TokenPrimary:
		p.next()
		if err := p.expectOrError(TokenKey, "expected KEY after PRIMARY"); err != nil {
			return nil, err
		}
		if err := p.expectOrError(TokenLParen, "expected ( after PRIMARY KEY"); err != nil {
			return nil, err
		}
		id, err := p.parseIdentRequired("expected column name")
		if err != nil {
			return nil, err
		}
		if err := p.expectOrError(TokenRParen, "expected ) after column name"); err != nil {
			return nil, err
		}
		return &ast.PrimaryKeyConstraint{ColumnName: id.Name}, nil

	case TokenForeign:
		p.next()
		if err := p.expectOrError(TokenKey, "expected KEY after FOREIGN"); err != nil {
			return nil, err
		}
		if err := p.expectOrError(TokenLParen, "expected ( after FOREIGN KEY"); err != nil {
			return nil, err
		}
		id, err := p.parseIdentRequired("expected column name")
		if err != nil {
			return nil, err
		}
		if err := p.expectOrError(TokenRParen, "expected ) after column name"); err != nil {
			return nil, err
		}
		if err := p.expectOrError(TokenReferences, "expected REFERENCES after FOREIGN KEY"); err != nil {
			return nil, err
		}
		refTable, err := p.parseIdentRequired("expected referenced table name")
		if err != nil {
			return nil, err
		}
		if err := p.expectOrError(TokenLParen, "expected ( after table name"); err != nil {
			return nil, err
		}
		refCol, err := p.parseIdentRequired("expected referenced column name")
		if err != nil {
			return nil, err
		}
		if err := p.expectOrError(TokenRParen, "expected ) after referenced column"); err != nil {
			return nil, err
		}
		return &ast.ForeignKeyConstraint{
			ColumnName:      id.Name,
			ReferencedTable: refTable.Name,
			ReferencedCol:   refCol.Name,
		}, nil

	case TokenUnique:
		p.next()
		if err := p.expectOrError(TokenLParen, "expected ( after UNIQUE"); err != nil {
			return nil, err
		}
		id, err := p.parseIdentRequired("expected column name")
		if err != nil {
			return nil, err
		}
		if err := p.expectOrError(TokenRParen, "expected ) after column name"); err != nil {
			return nil, err
		}
		return &ast.UniqueConstraint{ColumnName: id.Name}, nil

	default:
		return nil, p.errorf("unexpected constraint")
	}
}

func (p *Parser) parseInsert() (ast.Statement, error) {
	p.next()
	if !p.expect(TokenInto) {
		return nil, p.errorf("expected INTO")
	}
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected table name")
	}
	tableIdent := p.parseQualifiedIdent()
	stmt := &ast.Insert{Table: tableIdent}
	// parseQualifiedIdent already advances past the identifier

	if p.cur.Type == TokenLParen {
		p.next()
		cols, err := p.parseIdentList()
		if err != nil {
			return nil, err
		}
		stmt.Columns = cols
		if !p.expect(TokenRParen) {
			return nil, p.errorf("expected ) after columns")
		}
	}

	if !p.expect(TokenValues) {
		return nil, p.errorf("expected VALUES")
	}
	if !p.expect(TokenLParen) {
		return nil, p.errorf("expected (")
	}

	for {
		lit, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		stmt.Values = append(stmt.Values, lit)
		if p.cur.Type == TokenComma {
			p.next()
			continue
		}
		break
	}

	if !p.expect(TokenRParen) {
		return nil, p.errorf("expected ) after values")
	}

	return stmt, nil
}

func (p *Parser) parseIdentList() ([]ast.Identifier, error) {
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected identifier")
	}
	var ids []ast.Identifier
	for {
		if p.cur.Type != TokenIdent {
			return nil, p.errorf("expected identifier")
		}
		ids = append(ids, ast.Identifier{Name: p.cur.Literal})
		p.next()
		if p.cur.Type == TokenComma {
			p.next()
			continue
		}
		break
	}
	return ids, nil
}

func (p *Parser) parseLiteral() (ast.Literal, error) {
	switch p.cur.Type {
	case TokenString:
		lit := ast.Literal{Kind: "string", Value: p.cur.Literal}
		p.next()
		return lit, nil
	case TokenNumber:
		lit := ast.Literal{Kind: "number", Value: p.cur.Literal}
		p.next()
		return lit, nil
	case TokenIdent:
		// Allow identifiers for procedure parameters
		lit := ast.Literal{Kind: "identifier", Value: p.cur.Literal}
		p.next()
		return lit, nil
	default:
		return ast.Literal{}, p.errorf("expected literal")
	}
}

func (p *Parser) expect(tt TokenType) bool {
	if p.cur.Type != tt {
		return false
	}
	p.next()
	return true
}

func (p *Parser) errorf(format string, args ...any) error {
	return fmt.Errorf("parse error at %d: %s", p.cur.Pos, fmt.Sprintf(format, args...))
}

func (p *Parser) parseSet() (ast.Statement, error) {
	// SET variable_name = value
	// or SET variable_name TO value
	log.Printf("[parser] parseSet called, cur=%v", p.cur.Type)

	// Move past SET keyword
	p.next()

	// Expect variable name (identifier)
	if p.cur.Type != TokenIdent {
		return nil, fmt.Errorf("SET: expected variable name, got %v", p.cur.Type)
	}
	varName := p.cur.Literal
	log.Printf("[parser] parseSet variable: %s", varName)
	p.next()

	// Expect = or TO
	if p.cur.Type != TokenEq && !strings.EqualFold(p.cur.Literal, "TO") {
		return nil, fmt.Errorf("SET: expected = or TO, got %v (%s)", p.cur.Type, p.cur.Literal)
	}
	p.next()

	// Consume value and anything until ; or EOF
	// The value could be complex (e.g., "on", "off", "3", etc.)
	// No limit - just consume until we hit statement boundary
	for p.cur.Type != TokenSemicolon && p.cur.Type != TokenEOF {
		log.Printf("[parser] parseSet consuming token: %v (%s)", p.cur.Type, p.cur.Literal)
		p.next()
	}

	log.Printf("[parser] parseSet complete, now at: %v", p.cur.Type)
	return &ast.Set{}, nil
}

func (p *Parser) parseCall() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected procedure name")
	}
	stmt := &ast.CallProcedure{Name: ast.Identifier{Name: p.cur.Literal}}
	p.next()

	if !p.expect(TokenLParen) {
		return nil, p.errorf("expected ( after procedure name")
	}

	// Parse arguments
	for p.cur.Type != TokenRParen && p.cur.Type != TokenEOF {
		lit, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		stmt.Arguments = append(stmt.Arguments, lit)

		if p.cur.Type == TokenComma {
			p.next()
			continue
		}
		break
	}

	if !p.expect(TokenRParen) {
		return nil, p.errorf("expected ) after arguments")
	}

	return stmt, nil
}

func (p *Parser) parseUpdate() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected table name")
	}
	tableIdent := p.parseQualifiedIdent()
	stmt := &ast.Update{Table: tableIdent}
	// parseQualifiedIdent already advances past the identifier

	if !p.expect(TokenSet) {
		return nil, p.errorf("expected SET")
	}

	col, err := p.parseIdentRequired("expected column name")
	if err != nil {
		return nil, err
	}

	if err := p.expectOrError(TokenEq, "expected ="); err != nil {
		return nil, err
	}

	lit, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	stmt.Column = col
	stmt.Value = lit

	where, err := p.parseWhereClause()
	if err != nil {
		return nil, err
	}
	if where != nil {
		stmt.Where = where
	}

	return stmt, nil
}

func (p *Parser) parseDelete() (ast.Statement, error) {
	p.next()
	if !p.expect(TokenFrom) {
		return nil, p.errorf("expected FROM")
	}

	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected table name")
	}
	tableIdent := p.parseQualifiedIdent()
	stmt := &ast.Delete{Table: tableIdent}
	// parseQualifiedIdent already advances past the identifier

	where, err := p.parseWhereClause()
	if err != nil {
		return nil, err
	}
	if where != nil {
		stmt.Where = where
	}

	return stmt, nil
}

// parseQualifiedIdent parsea un identificador calificado: schema.tabla o solo tabla
func (p *Parser) parseQualifiedIdent() ast.Identifier {
	var schema, name string
	if p.cur.Type == TokenIdent {
		// Handle cases where lexer returned a dotted identifier as a single token (e.g. "schema.table")
		if strings.Contains(p.cur.Literal, ".") {
			parts := strings.SplitN(p.cur.Literal, ".", 2)
			schema = parts[0]
			name = parts[1]
			p.next()
			return ast.Identifier{Name: name, Alias: schema}
		}

		if p.peek.Type == TokenDot {
			schema = p.cur.Literal
			p.next() // ident
			p.next() // dot
			if p.cur.Type == TokenIdent {
				name = p.cur.Literal
				p.next()
			}
		} else {
			name = p.cur.Literal
			p.next()
		}
	}
	return ast.Identifier{Name: name, Alias: schema}
}

// CREATE JOB job_name INTERVAL n UNIT unit [ENABLED] BEGIN ... END
func (p *Parser) parseCreateJob() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected job name")
	}
	jobName := ast.Identifier{Name: p.cur.Literal}
	p.next()

	if p.cur.Type != TokenInterval {
		return nil, p.errorf("expected INTERVAL after job name")
	}
	p.next()

	if p.cur.Type != TokenNumber {
		return nil, p.errorf("expected interval number")
	}
	interval := 0
	_, err := fmt.Sscanf(p.cur.Literal, "%d", &interval)
	if err != nil {
		return nil, p.errorf("invalid interval number")
	}
	p.next()

	if p.cur.Type != TokenUnit {
		return nil, p.errorf("expected UNIT after interval")
	}
	p.next()

	// Parse unit (MINUTE, HOUR, DAY as tokens or identifier)
	var unit string
	switch p.cur.Type {
	case TokenMinute:
		unit = "MINUTE"
	case TokenHour:
		unit = "HOUR"
	case TokenDay:
		unit = "DAY"
	case TokenIdent:
		unit = strings.ToUpper(p.cur.Literal)
	default:
		return nil, p.errorf("expected unit name (MINUTE, HOUR, DAY)")
	}
	p.next()

	enabled := false
	if p.cur.Type == TokenEnabled {
		enabled = true
		p.next()
	}

	if !p.expect(TokenBegin) {
		return nil, p.errorf("expected BEGIN for job body")
	}

	body, err := p.parseStatementsBlock()
	if err != nil {
		return nil, err
	}

	if !p.expect(TokenEnd) {
		return nil, p.errorf("expected END after job body")
	}

	return &ast.CreateJob{
		Name:     jobName,
		Interval: interval,
		Unit:     unit,
		Body:     body,
		Enabled:  enabled,
	}, nil
}

// ALTER [TABLE|JOB] ... (stub implementation)
func (p *Parser) parseAlter() (ast.Statement, error) {
	p.next()
	switch p.cur.Type {
	case TokenTable:
		// Aquí podrías llamar a parseAlterTable si lo implementas
		return nil, p.errorf("ALTER TABLE no implementado aún")
	case TokenJob:
		return p.parseAlterJob()
	default:
		return nil, p.errorf("expected TABLE or JOB after ALTER")
	}
}

// ALTER JOB job_name ... (stub)
func (p *Parser) parseAlterJob() (ast.Statement, error) {
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected job name after ALTER JOB")
	}
	jobName := ast.Identifier{Name: p.cur.Literal}
	p.next()
	// Aquí deberías parsear las opciones específicas de ALTER JOB
	return &ast.AlterJob{Name: jobName}, nil
}
