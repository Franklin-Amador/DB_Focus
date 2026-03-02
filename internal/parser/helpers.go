package parser

import (
	"dbf/internal/ast"
	"fmt"
	"strconv"
	"strings"
)

// expectOrError wraps expect and returns a formatted parse error when the
// expected token is not present. This reduces repeated boilerplate checks.
func (p *Parser) expectOrError(tt TokenType, msg string) error {
	if !p.expect(tt) {
		return p.errorf(msg)
	}
	return nil
}

// parseIdentRequired parses and returns the current token as an identifier,
// advancing the parser. Returns an error if the current token is not an ident.
func (p *Parser) parseIdentRequired(msg string) (ast.Identifier, error) {
	if p.cur.Type != TokenIdent {
		return ast.Identifier{}, p.errorf(msg)
	}
	id := ast.Identifier{Name: p.cur.Literal}
	p.next()
	return id, nil
}

// parseStatementsBlock parses a sequence of statements until an END or EOF
// token is encountered. It does not consume the END token; callers should
// call expect(TokenEnd) when appropriate.
func (p *Parser) parseStatementsBlock() ([]ast.Statement, error) {
	var stmts []ast.Statement
	for p.cur.Type != TokenEnd && p.cur.Type != TokenEOF {
		bodyStmt, err := p.ParseStatement()
		if err != nil {
			return nil, err
		}
		if bodyStmt != nil {
			stmts = append(stmts, bodyStmt)
		}
	}
	return stmts, nil
}

// parseFromAndJoin parses FROM table and optional JOIN clause. It assumes
// the current token is TokenFrom and advances the parser accordingly.
func (p *Parser) parseFromAndJoin() (ast.Identifier, *ast.JoinClause, error) {
	// consume FROM
	p.next()

	if p.cur.Type != TokenIdent {
		return ast.Identifier{}, nil, p.errorf("expected table name")
	}
	table := ast.Identifier{Name: p.cur.Literal}
	p.next()
	// Optional AS alias for table
	if p.cur.Type == TokenAs {
		p.next()
		if p.cur.Type != TokenIdent {
			return ast.Identifier{}, nil, p.errorf("expected alias after AS")
		}
		table.Alias = p.cur.Literal
		p.next()
	}

	// Optional JOIN
	var join *ast.JoinClause
	if p.cur.Type == TokenInner || p.cur.Type == TokenLeft || p.cur.Type == TokenRight || p.cur.Type == TokenFull || p.cur.Type == TokenCross || p.cur.Type == TokenJoin {
		var joinType string
		switch p.cur.Type {
		case TokenInner, TokenJoin:
			joinType = "INNER"
			p.next()
			if p.cur.Type == TokenJoin { // when explicit JOIN token followed
				// already advanced
			}
			if p.cur.Type == TokenJoin {
				// consume JOIN
				p.next()
			}
		case TokenLeft:
			joinType = "LEFT"
			p.next()
			if p.cur.Type == TokenOuter {
				p.next()
			}
			if p.cur.Type == TokenJoin {
				p.next()
			}
		case TokenRight:
			joinType = "RIGHT"
			p.next()
			if p.cur.Type == TokenOuter {
				p.next()
			}
			if p.cur.Type == TokenJoin {
				p.next()
			}
		case TokenFull:
			joinType = "FULL"
			p.next()
			if p.cur.Type == TokenOuter {
				p.next()
			}
			if p.cur.Type == TokenJoin {
				p.next()
			}
		case TokenCross:
			joinType = "CROSS"
			p.next()
			if p.cur.Type == TokenJoin {
				p.next()
			}
		}

		if p.cur.Type != TokenIdent {
			return ast.Identifier{}, nil, p.errorf("expected table name after JOIN")
		}
		joinTable := ast.Identifier{Name: p.cur.Literal}
		p.next()
		if p.cur.Type == TokenAs {
			p.next()
			if p.cur.Type != TokenIdent {
				return ast.Identifier{}, nil, p.errorf("expected alias after AS")
			}
			joinTable.Alias = p.cur.Literal
			p.next()
		}

		if joinType == "CROSS" {
			join = &ast.JoinClause{Type: joinType, Table: joinTable}
		} else {
			if !p.expect(TokenOn) {
				return ast.Identifier{}, nil, p.errorf("expected ON after JOIN table")
			}
			if p.cur.Type != TokenIdent {
				return ast.Identifier{}, nil, p.errorf("expected column in JOIN condition")
			}
			left := ast.Identifier{Name: p.cur.Literal}
			p.next()
			if !p.expect(TokenEq) {
				return ast.Identifier{}, nil, p.errorf("expected = in JOIN condition")
			}
			if p.cur.Type != TokenIdent {
				return ast.Identifier{}, nil, p.errorf("expected column in JOIN condition")
			}
			right := ast.Identifier{Name: p.cur.Literal}
			p.next()

			join = &ast.JoinClause{Type: joinType, Table: joinTable, Left: left, Right: right}
		}
	}

	return table, join, nil
}

func (p *Parser) parseWhereClause() (*ast.WhereClause, error) {
	if p.cur.Type != TokenWhere {
		return nil, nil
	}
	p.next()
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected column in WHERE")
	}
	col := ast.Identifier{Name: p.cur.Literal}
	p.next()
	if !p.expect(TokenEq) {
		return nil, p.errorf("expected = in WHERE")
	}
	lit, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	return &ast.WhereClause{Column: col, Value: lit}, nil
}

func (p *Parser) parseGroupByClause() ([]ast.Identifier, error) {
	var groupBy []ast.Identifier
	if p.cur.Type != TokenGroup {
		return groupBy, nil
	}
	p.next()
	if !p.expect(TokenBy) {
		return nil, p.errorf("expected BY after GROUP")
	}
	for {
		if p.cur.Type != TokenIdent {
			return nil, p.errorf("expected column name in GROUP BY")
		}
		groupBy = append(groupBy, ast.Identifier{Name: p.cur.Literal})
		p.next()
		if p.cur.Type != TokenComma {
			break
		}
		p.next()
	}
	return groupBy, nil
}

func (p *Parser) parseOrderByClause() ([]ast.OrderByClause, error) {
	var orderBy []ast.OrderByClause
	if p.cur.Type != TokenOrder {
		return orderBy, nil
	}
	p.next()
	if !p.expect(TokenBy) {
		return nil, p.errorf("expected BY after ORDER")
	}
	for {
		if p.cur.Type != TokenIdent {
			return nil, p.errorf("expected column name in ORDER BY")
		}
		orderCol := ast.OrderByClause{Column: ast.Identifier{Name: p.cur.Literal}, Direction: "ASC"}
		p.next()
		switch p.cur.Type {
		case TokenAsc:
			orderCol.Direction = "ASC"
			p.next()
		case TokenDesc:
			orderCol.Direction = "DESC"
			p.next()
		}
		orderBy = append(orderBy, orderCol)
		if p.cur.Type != TokenComma {
			break
		}
		p.next()
	}
	return orderBy, nil
}

func (p *Parser) parseLimitOffset() (int, int, error) {
	limit := 0
	offset := 0
	if p.cur.Type == TokenLimit {
		p.next()
		if p.cur.Type != TokenNumber {
			return 0, 0, p.errorf("expected number after LIMIT")
		}
		v, err := strconv.Atoi(p.cur.Literal)
		if err != nil {
			return 0, 0, p.errorf("invalid LIMIT value: %s", p.cur.Literal)
		}
		limit = v
		p.next()
	}
	if p.cur.Type == TokenOffset {
		p.next()
		if p.cur.Type != TokenNumber {
			return 0, 0, p.errorf("expected number after OFFSET")
		}
		v, err := strconv.Atoi(p.cur.Literal)
		if err != nil {
			return 0, 0, p.errorf("invalid OFFSET value: %s", p.cur.Literal)
		}
		offset = v
		p.next()
	}
	return limit, offset, nil
}

// parseSelectItem parses a single select list item. It updates the provided
// depth pointer for parenthesis nesting and returns the parsed Identifier,
// whether this item causes allowMissing to be set, a boolean indicating the
// caller should stop parsing further items (e.g., reached FROM/EOF), and an
// error if any.
func (p *Parser) parseSelectItem(depth *int, exprIdx int) (ast.Identifier, bool, bool, error) {
	allowMissingDelta := false
	lastIdent := ""
	alias := ""

	itemHasExpr := false
	itemHasColumn := false
	itemHasComplexExpr := false
	expectAlias := false

	appendItem := func() ast.Identifier {
		sourceName := ""
		if itemHasColumn {
			sourceName = lastIdent
		}
		outputName := alias
		if outputName == "" {
			outputName = trimQualifier(sourceName)
		}
		if outputName == "" {
			outputName = fmt.Sprintf("expr%d", exprIdx)
			itemHasExpr = true
		}
		id := ast.Identifier{Name: sourceName}
		if outputName != sourceName {
			id.Alias = outputName
		}
		if itemHasComplexExpr {
			itemHasColumn = false
		}
		if itemHasExpr && !itemHasColumn {
			allowMissingDelta = true
		}
		return id
	}

	for {
		if p.cur.Type == TokenEOF || p.cur.Type == TokenSemicolon || (p.cur.Type == TokenFrom && *depth == 0) {
			return appendItem(), allowMissingDelta, true, nil
		}

		switch p.cur.Type {
		case TokenCast:
			p.next()
			if p.cur.Type == TokenIdent {
				p.next()
			}
			itemHasExpr = true
			itemHasComplexExpr = true
		case TokenCase, TokenWhen, TokenThen, TokenElse:
			itemHasExpr = true
			itemHasComplexExpr = true
			p.next()
		case TokenEnd:
			itemHasExpr = true
			itemHasComplexExpr = true
			p.next()
		case TokenLParen:
			*depth++
			itemHasExpr = true
			itemHasComplexExpr = true
			p.next()
		case TokenRParen:
			if *depth > 0 {
				*depth--
			}
			itemHasComplexExpr = true
			p.next()
		case TokenComma:
			if *depth == 0 {
				id := appendItem()
				p.next()
				return id, allowMissingDelta, false, nil
			}
			p.next()
		case TokenAs:
			if *depth == 0 {
				expectAlias = true
			}
			p.next()
		case TokenIdent, TokenCount:
			lit := p.cur.Literal
			upper := strings.ToUpper(lit)
			if upper == "CASE" || upper == "WHEN" || upper == "THEN" || upper == "ELSE" {
				itemHasExpr = true
				itemHasComplexExpr = true
				p.next()
				continue
			}
			if expectAlias {
				alias = lit
				expectAlias = false
				itemHasExpr = true
				p.next()
				continue
			}
			if upper == "AS" && *depth == 0 {
				expectAlias = true
				p.next()
				continue
			}
			if *depth == 0 && !expectAlias && (itemHasExpr || itemHasColumn) {
				if strings.Contains(lit, " ") || p.peek.Type == TokenComma || p.peek.Type == TokenFrom || p.peek.Type == TokenSemicolon || p.peek.Type == TokenEOF {
					alias = lit
					itemHasExpr = true
					p.next()
					continue
				}
			}

			// Handle aggregate functions like COUNT(*)
			if p.peek.Type == TokenLParen && (upper == "COUNT" || p.cur.Type == TokenCount) {
				funcName := lit
				p.next() // consume function name
				p.next() // consume (
				*depth++

				// Capture arguments
				argStart := ""
				switch p.cur.Type {
				case TokenStar:
					argStart = "*"
					p.next()
				case TokenIdent:
					argStart = p.cur.Literal
					p.next()
				}

				if p.cur.Type == TokenRParen {
					*depth--
					p.next()
					lastIdent = funcName + "(" + argStart + ")"
					itemHasExpr = true
					itemHasColumn = true
					continue
				}
			}

			lastIdent = lit
			if p.peek.Type == TokenLParen {
				itemHasExpr = true
			} else {
				itemHasColumn = true
			}
			p.next()
		default:
			itemHasExpr = true
			itemHasComplexExpr = true
			p.next()
		}
	}
}

// parseColumnDef parses a column definition inside CREATE TABLE and advances
// the parser to the next token after the column definition and its
// constraints.
func (p *Parser) parseColumnDef() (ast.ColumnDef, error) {
	if p.cur.Type != TokenIdent {
		return ast.ColumnDef{}, p.errorf("expected column name")
	}
	colName := ast.Identifier{Name: p.cur.Literal}
	p.next()

	if p.cur.Type != TokenIdent {
		return ast.ColumnDef{}, p.errorf("expected type name")
	}
	colType := p.cur.Literal
	p.next()

	colDef := ast.ColumnDef{Name: colName, Type: colType, Constraints: []ast.Constraint{}}

	for {
		switch p.cur.Type {
		case TokenIdentity:
			p.next()
			colDef.Identity = true
		case TokenNot:
			p.next()
			if !p.expect(TokenNull) {
				return ast.ColumnDef{}, p.errorf("expected NULL after NOT")
			}
			colDef.NotNull = true
		case TokenPrimary:
			p.next()
			if !p.expect(TokenKey) {
				return ast.ColumnDef{}, p.errorf("expected KEY after PRIMARY")
			}
			colDef.Constraints = append(colDef.Constraints, &ast.PrimaryKeyConstraint{ColumnName: colName.Name})
		case TokenUnique:
			p.next()
			colDef.Constraints = append(colDef.Constraints, &ast.UniqueConstraint{ColumnName: colName.Name})
		default:
			return colDef, nil
		}
	}
}
