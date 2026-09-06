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

// parseFromAndJoin parses FROM table and an optional chain of JOIN clauses. It
// assumes the current token is TokenFrom and advances the parser accordingly.
// Multiple joins (N-way) are supported: each JOIN is appended to the returned slice.
func (p *Parser) parseFromAndJoin() (ast.Identifier, []*ast.JoinClause, error) {
	// consume FROM
	p.next()

	if p.cur.Type != TokenIdent {
		return ast.Identifier{}, nil, p.errorf("expected table name")
	}
	table, err := p.parseTableRef()
	if err != nil {
		return ast.Identifier{}, nil, err
	}

	// Optional chain of JOINs
	var joins []*ast.JoinClause
	for p.cur.Type == TokenNatural || p.cur.Type == TokenInner || p.cur.Type == TokenLeft || p.cur.Type == TokenRight || p.cur.Type == TokenFull || p.cur.Type == TokenCross || p.cur.Type == TokenJoin {
		join, err := p.parseSingleJoin()
		if err != nil {
			return ast.Identifier{}, nil, err
		}
		joins = append(joins, join)
	}

	return table, joins, nil
}

// parseSingleJoin parses one JOIN clause (the current token is a join keyword or
// NATURAL). Supports explicit ON, USING (col, ...), and NATURAL joins.
func (p *Parser) parseSingleJoin() (*ast.JoinClause, error) {
	// Optional NATURAL prefix.
	natural := false
	if p.cur.Type == TokenNatural {
		natural = true
		p.next()
	}

	var joinType string
	switch p.cur.Type {
	case TokenInner, TokenJoin:
		joinType = "INNER"
		p.next()
		if p.cur.Type == TokenJoin {
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
		return nil, p.errorf("expected table name after JOIN")
	}
	joinTable, err := p.parseTableRef()
	if err != nil {
		return nil, err
	}

	if natural && joinType == "CROSS" {
		return nil, p.errorf("NATURAL CROSS JOIN is not allowed")
	}

	// NATURAL join: no ON/USING; common columns are matched at execution time.
	if natural {
		return &ast.JoinClause{Type: joinType, Table: joinTable, Natural: true}, nil
	}

	// CROSS join has no join condition.
	if joinType == "CROSS" {
		return &ast.JoinClause{Type: joinType, Table: joinTable}, nil
	}

	// USING (col, ...): join on the named common columns.
	if p.cur.Type == TokenUsing {
		p.next()
		if !p.expect(TokenLParen) {
			return nil, p.errorf("expected ( after USING")
		}
		var cols []string
		for {
			if p.cur.Type != TokenIdent {
				return nil, p.errorf("expected column name in USING list")
			}
			cols = append(cols, p.cur.Literal)
			p.next()
			if p.cur.Type == TokenComma {
				p.next()
				continue
			}
			break
		}
		if !p.expect(TokenRParen) {
			return nil, p.errorf("expected ) after USING column list")
		}
		return &ast.JoinClause{Type: joinType, Table: joinTable, Using: cols}, nil
	}

	// Explicit ON condition.
	if !p.expect(TokenOn) {
		return nil, p.errorf("expected ON, USING or NATURAL for JOIN")
	}
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected column in JOIN condition")
	}
	left := ast.Identifier{Name: p.cur.Literal}
	p.next()
	if !p.expect(TokenEq) {
		return nil, p.errorf("expected = in JOIN condition")
	}
	if p.cur.Type != TokenIdent {
		return nil, p.errorf("expected column in JOIN condition")
	}
	right := ast.Identifier{Name: p.cur.Literal}
	p.next()

	return &ast.JoinClause{Type: joinType, Table: joinTable, Left: left, Right: right}, nil
}

// parseTableRef parses a FROM/JOIN table reference: "[schema.]table [[AS] alias]".
// The current token must be the table identifier. A dotted name is split into
// Identifier.Schema + Name so that Alias is purely the table alias.
func (p *Parser) parseTableRef() (ast.Identifier, error) {
	id := ast.Identifier{Name: p.cur.Literal}
	if parts := strings.SplitN(id.Name, ".", 2); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		id.Schema, id.Name = parts[0], parts[1]
	}
	p.next()
	switch {
	case p.cur.Type == TokenAs:
		p.next()
		if p.cur.Type != TokenIdent {
			return ast.Identifier{}, p.errorf("expected alias after AS")
		}
		id.Alias = p.cur.Literal
		p.next()
	case p.cur.Type == TokenIdent:
		// Bare alias: FROM ventas v
		id.Alias = p.cur.Literal
		p.next()
	}
	return id, nil
}

func (p *Parser) parseWhereClause() (*ast.WhereClause, error) {
	if p.cur.Type != TokenWhere {
		return nil, nil
	}
	p.next()
	return p.parseWhereOr()
}

// parseWhereOr parses OR-separated conjunctions (OR has the lowest precedence).
func (p *Parser) parseWhereOr() (*ast.WhereClause, error) {
	left, err := p.parseWhereAnd()
	if err != nil {
		return nil, err
	}
	for p.cur.Type == TokenOr {
		p.next()
		right, err := p.parseWhereAnd()
		if err != nil {
			return nil, err
		}
		left = &ast.WhereClause{Conj: "OR", Left: left, Right: right}
	}
	return left, nil
}

// parseWhereAnd parses AND-separated predicates (AND binds tighter than OR).
func (p *Parser) parseWhereAnd() (*ast.WhereClause, error) {
	left, err := p.parseWherePredicate()
	if err != nil {
		return nil, err
	}
	for p.cur.Type == TokenAnd {
		p.next()
		right, err := p.parseWherePredicate()
		if err != nil {
			return nil, err
		}
		left = &ast.WhereClause{Conj: "AND", Left: left, Right: right}
	}
	return left, nil
}

// parseWherePredicate parses a parenthesized sub-expression or a single
// `column OP literal` comparison (the leaf). Inside QUALIFY the left operand
// may also be an aggregate expression (SUM(x)) or a window-function call
// (ROW_NUMBER() OVER (...)).
func (p *Parser) parseWherePredicate() (*ast.WhereClause, error) {
	clause := p.predicateClauseName()
	if p.cur.Type == TokenLParen {
		p.next()
		inner, err := p.parseWhereOr()
		if err != nil {
			return nil, err
		}
		if !p.expect(TokenRParen) {
			return nil, p.errorf("expected ) in %s", clause)
		}
		return inner, nil
	}

	var col ast.Identifier
	switch {
	case p.predClause != "" && p.isFunctionCallStart():
		w, text, err := p.parseWindowCall()
		if err != nil {
			return nil, err
		}
		switch {
		case w != nil && clause != "QUALIFY":
			return nil, p.errorf("window functions are not allowed in %s", clause)
		case w != nil:
			col = ast.Identifier{Window: w}
		default:
			col = ast.Identifier{Name: text}
		}
	case p.cur.Type == TokenIdent:
		col = ast.Identifier{Name: p.cur.Literal}
		p.next()
	default:
		return nil, p.errorf("expected column in %s", clause)
	}
	op, ok := whereOperator(p.cur.Type)
	if !ok {
		return nil, p.errorf("expected comparison operator (=, <>, <, >, <=, >=) in %s", clause)
	}
	p.next()
	lit, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	return &ast.WhereClause{Column: col, Operator: op, Value: lit}, nil
}

// predicateClauseName names the clause whose predicate is being parsed, for
// error messages.
func (p *Parser) predicateClauseName() string {
	if p.predClause != "" {
		return p.predClause
	}
	return "WHERE"
}

// parsePredicateClause parses an optional predicate introduced by keyword
// (HAVING, QUALIFY): same tree as WHERE, with the leaf grammar widened
// according to the clause name.
func (p *Parser) parsePredicateClause(keyword TokenType, clause string) (*ast.WhereClause, error) {
	if p.cur.Type != keyword {
		return nil, nil
	}
	p.next()
	p.predClause = clause
	defer func() { p.predClause = "" }()
	return p.parseWhereOr()
}

// parseHavingClause parses an optional HAVING predicate, whose leaves may be
// columns, select-list aliases or aggregate expressions (SUM(x) > 10).
func (p *Parser) parseHavingClause() (*ast.WhereClause, error) {
	return p.parsePredicateClause(TokenHaving, "HAVING")
}

// parseQualifyClause parses an optional QUALIFY predicate, whose leaves may
// additionally be window-function calls (inline or by alias).
func (p *Parser) parseQualifyClause() (*ast.WhereClause, error) {
	return p.parsePredicateClause(TokenQualify, "QUALIFY")
}

// isFunctionCallStart reports whether the current token starts an aggregate or
// ranking function call: FUNC followed by "(".
func (p *Parser) isFunctionCallStart() bool {
	if p.peek.Type != TokenLParen {
		return false
	}
	if p.cur.Type == TokenCount {
		return true
	}
	if p.cur.Type != TokenIdent {
		return false
	}
	upper := strings.ToUpper(p.cur.Literal)
	return isAggregateFunc(upper) || isRankingFunc(upper)
}

// parseColumnRef parses a column reference usable in ORDER BY / PARTITION BY /
// GROUP BY: either a plain (possibly qualified) identifier or an aggregate call
// such as SUM(monto), which is kept as canonical text so the executor can match
// it against the select list.
func (p *Parser) parseColumnRef(clause string) (ast.Identifier, error) {
	if p.isFunctionCallStart() {
		w, text, err := p.parseWindowCall()
		if err != nil {
			return ast.Identifier{}, err
		}
		if w != nil {
			return ast.Identifier{}, p.errorf("window functions are not allowed in %s", clause)
		}
		return ast.Identifier{Name: text}, nil
	}
	if p.cur.Type != TokenIdent {
		return ast.Identifier{}, p.errorf("expected column name in %s", clause)
	}
	id := ast.Identifier{Name: p.cur.Literal}
	p.next()
	return id, nil
}

// parseColumnRefList parses a comma-separated list of column references.
func (p *Parser) parseColumnRefList(clause string) ([]ast.Identifier, error) {
	var out []ast.Identifier
	for {
		id, err := p.parseColumnRef(clause)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
		if p.cur.Type != TokenComma {
			break
		}
		p.next()
	}
	return out, nil
}

// parseWindowCall parses FUNC(arg) and, when followed by OVER, the window
// specification. The current token must be the function name and peek "(".
//
// It returns (window, text, err): window is non-nil for a window-function call;
// otherwise text holds the canonical aggregate expression ("SUM(monto)",
// "COUNT(*)") for a plain aggregate.
func (p *Parser) parseWindowCall() (*ast.WindowFunc, string, error) {
	funcName := p.cur.Literal
	upper := strings.ToUpper(funcName)
	p.next() // function name
	if !p.expect(TokenLParen) {
		return nil, "", p.errorf("expected ( after %s", funcName)
	}

	arg := ""
	switch {
	case p.cur.Type == TokenStar:
		arg = "*"
		p.next()
	case p.cur.Type == TokenRParen:
		// no argument (ranking functions)
	case p.isFunctionCallStart():
		// nested aggregate, e.g. SUM(SUM(monto)) OVER () on grouped rows
		w, text, err := p.parseWindowCall()
		if err != nil {
			return nil, "", err
		}
		if w != nil {
			return nil, "", p.errorf("window function calls cannot be nested")
		}
		arg = text
	case p.cur.Type == TokenIdent:
		arg = p.cur.Literal
		p.next()
	default:
		return nil, "", p.errorf("unexpected %s in %s(...) argument", p.cur.Type, funcName)
	}
	if !p.expect(TokenRParen) {
		return nil, "", p.errorf("expected ) after %s argument", funcName)
	}

	if isRankingFunc(upper) && arg != "" {
		return nil, "", p.errorf("%s takes no arguments", upper)
	}

	text := funcName + "(" + arg + ")"
	if p.cur.Type != TokenOver {
		if isRankingFunc(upper) {
			return nil, "", p.errorf("%s requires an OVER clause", upper)
		}
		return nil, text, nil
	}
	p.next() // OVER

	w := &ast.WindowFunc{Func: upper, Arg: arg}
	if !p.expect(TokenLParen) {
		return nil, "", p.errorf("expected ( after OVER")
	}
	if p.cur.Type == TokenPartition {
		p.next()
		if !p.expect(TokenBy) {
			return nil, "", p.errorf("expected BY after PARTITION")
		}
		parts, err := p.parseColumnRefList("PARTITION BY")
		if err != nil {
			return nil, "", err
		}
		w.PartitionBy = parts
	}
	ob, err := p.parseOrderByClause()
	if err != nil {
		return nil, "", err
	}
	w.OrderBy = ob
	if !p.expect(TokenRParen) {
		return nil, "", p.errorf("expected ) to close OVER clause")
	}
	return w, "", nil
}

// sliceFrom returns the original source text from byte offset start up to the
// start of the current token, clamped to valid bounds. Used to capture the
// verbatim SQL of a sub-clause (e.g. a view's SELECT) for stable persistence.
func (p *Parser) sliceFrom(start int) string {
	src := p.l.input
	end := p.cur.Pos
	if start < 0 || start > len(src) {
		return ""
	}
	if end < start || end > len(src) {
		end = len(src)
	}
	return src[start:end]
}

// isAggregateFunc reports whether an (already upper-cased) identifier is a
// supported aggregate function name.
func isAggregateFunc(upper string) bool {
	switch upper {
	case "COUNT", "SUM", "AVG", "MIN", "MAX":
		return true
	}
	return false
}

// isRankingFunc reports whether an (already upper-cased) identifier is a
// ranking window function (one that only makes sense with OVER).
func isRankingFunc(upper string) bool {
	switch upper {
	case "ROW_NUMBER", "RANK", "DENSE_RANK":
		return true
	}
	return false
}

// whereOperator maps a comparison token to its canonical operator string,
// reporting whether the token is a recognized comparison operator.
func whereOperator(t TokenType) (string, bool) {
	switch t {
	case TokenEq:
		return "=", true
	case TokenNotEq:
		return "<>", true
	case TokenLt:
		return "<", true
	case TokenGt:
		return ">", true
	case TokenLte:
		return "<=", true
	case TokenGte:
		return ">=", true
	}
	return "", false
}

func (p *Parser) parseGroupByClause() ([]ast.Identifier, error) {
	if p.cur.Type != TokenGroup {
		return nil, nil
	}
	p.next()
	if !p.expect(TokenBy) {
		return nil, p.errorf("expected BY after GROUP")
	}
	return p.parseColumnRefList("GROUP BY")
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
		col, err := p.parseColumnRef("ORDER BY")
		if err != nil {
			return nil, err
		}
		orderCol := ast.OrderByClause{Column: col, Direction: "ASC"}
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
	var window *ast.WindowFunc

	appendItem := func() ast.Identifier {
		// A window call wrapped in a larger expression (ROW_NUMBER() OVER () * 2)
		// is not supported: fall through to the NULL placeholder like any other
		// unsupported expression instead of silently projecting the bare window.
		if window != nil && itemHasComplexExpr {
			window = nil
		}
		if window != nil {
			outputName := alias
			if outputName == "" {
				outputName = fmt.Sprintf("expr%d", exprIdx)
			}
			return ast.Identifier{Alias: outputName, Window: window}
		}
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

			// Aggregate calls (COUNT(*), SUM(col), ...) and window-function calls
			// (ROW_NUMBER() OVER (...), SUM(col) OVER (...)).
			if p.isFunctionCallStart() {
				w, text, err := p.parseWindowCall()
				if err != nil {
					return ast.Identifier{}, false, false, err
				}
				if w != nil {
					window = w
					itemHasExpr = true
					continue
				}
				lastIdent = text
				itemHasExpr = true
				itemHasColumn = true
				continue
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
	isSerial := strings.EqualFold(colType, "SERIAL")
	if isSerial {
		colType = "INTEGER"
	}
	p.next()

	// Skip optional type length/precision: VARCHAR(50), CHAR(10), NUMERIC(10,2), etc.
	if p.cur.Type == TokenLParen {
		p.next()
		depth := 1
		for depth > 0 && p.cur.Type != TokenEOF {
			if p.cur.Type == TokenLParen {
				depth++
			} else if p.cur.Type == TokenRParen {
				depth--
			}
			p.next()
		}
	}

	colDef := ast.ColumnDef{Name: colName, Type: colType, Constraints: []ast.Constraint{}, Identity: isSerial}

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
		case TokenNull:
			// explicit NULL — no-op, just consume
			p.next()
		case TokenPrimary:
			p.next()
			if !p.expect(TokenKey) {
				return ast.ColumnDef{}, p.errorf("expected KEY after PRIMARY")
			}
			colDef.Constraints = append(colDef.Constraints, &ast.PrimaryKeyConstraint{ColumnName: colName.Name})
		case TokenUnique:
			p.next()
			colDef.Constraints = append(colDef.Constraints, &ast.UniqueConstraint{ColumnName: colName.Name})
		case TokenIdent:
			// DEFAULT keyword (not yet a dedicated token)
			if strings.EqualFold(p.cur.Literal, "DEFAULT") {
				p.next()
				lit, err := p.parseDefaultValue()
				if err != nil {
					return ast.ColumnDef{}, err
				}
				colDef.DefaultVal = lit
			} else {
				return colDef, nil
			}
		default:
			return colDef, nil
		}
	}
}

// parseDefaultValue consumes the default expression after DEFAULT.
// Supports: literals (string, number), TRUE/FALSE/NULL, identifiers (e.g. CURRENT_TIMESTAMP).
func (p *Parser) parseDefaultValue() (*ast.Literal, error) {
	switch p.cur.Type {
	case TokenString:
		lit := &ast.Literal{Kind: "string", Value: p.cur.Literal}
		p.next()
		return lit, nil
	case TokenNumber:
		lit := &ast.Literal{Kind: "number", Value: p.cur.Literal}
		p.next()
		return lit, nil
	case TokenTrue:
		lit := &ast.Literal{Kind: "bool", Value: "true"}
		p.next()
		return lit, nil
	case TokenFalse:
		lit := &ast.Literal{Kind: "bool", Value: "false"}
		p.next()
		return lit, nil
	case TokenNull:
		lit := &ast.Literal{Kind: "null", Value: "NULL"}
		p.next()
		return lit, nil
	case TokenIdent:
		// e.g. CURRENT_TIMESTAMP or any function-like identifier
		val := p.cur.Literal
		p.next()
		// Handle function call syntax: NOW() etc.
		if p.cur.Type == TokenLParen {
			p.next()
			if p.cur.Type == TokenRParen {
				p.next()
			}
		}
		return &ast.Literal{Kind: "ident", Value: val}, nil
	default:
		return &ast.Literal{Kind: "ident", Value: ""}, nil
	}
}

// parseBeginEndBlock parses a `BEGIN <statements> END` block and returns the
// body statements together with their verbatim source text (between BEGIN and
// END). The current token must be BEGIN; both BEGIN and the closing END are
// consumed. The body text lets callers persist a stable, AST-independent
// definition. Shared by CREATE PROCEDURE/TRIGGER/JOB bodies.
func (p *Parser) parseBeginEndBlock() ([]ast.Statement, string, error) {
	if !p.expect(TokenBegin) {
		return nil, "", p.errorf("expected BEGIN")
	}
	bodyStart := p.cur.Pos
	body, err := p.parseStatementsBlock()
	if err != nil {
		return nil, "", err
	}
	bodyText := strings.TrimSpace(p.sliceFrom(bodyStart))
	if !p.expect(TokenEnd) {
		return nil, "", p.errorf("expected END")
	}
	return body, bodyText, nil
}

// parseOptionalIfExists consumes an optional "IF EXISTS" prefix and reports
// whether it was present. It errors if IF is seen without a following EXISTS.
func (p *Parser) parseOptionalIfExists() (bool, error) {
	if p.cur.Type != TokenIf {
		return false, nil
	}
	p.next()
	if p.cur.Type != TokenExists {
		return false, p.errorf("expected EXISTS after IF")
	}
	p.next()
	return true, nil
}

// parseOptionalCascadeRestrict consumes an optional CASCADE or RESTRICT
// modifier and returns "CASCADE", "RESTRICT", or "" when neither is present.
func (p *Parser) parseOptionalCascadeRestrict() string {
	switch p.cur.Type {
	case TokenCascade:
		p.next()
		return "CASCADE"
	case TokenRestrict:
		p.next()
		return "RESTRICT"
	default:
		return ""
	}
}
