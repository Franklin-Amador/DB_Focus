package executor

import (
	"context"
	"fmt"
	"strings"

	"dbf/internal/ast"
	"dbf/internal/catalog"
	"dbf/internal/constants"
	"dbf/internal/parser"
)

func init() {
	registerExec((*Executor).executeCreateFunction)
	registerExec((*Executor).executeDropFunction)
	registerExec((*Executor).executeReturn)
}

// executeReturn is a no-op: the engine does not use a routine's return value
// (triggers ignore it, scalar returns are not evaluated). It exists so that a
// function/trigger body containing RETURN executes rather than erroring.
func (e *Executor) executeReturn(ctx context.Context, stmt *ast.Return) (*Result, error) {
	return &Result{Tag: "RETURN"}, nil
}

// executeCreateFunction stores a (subset) SQL function in the catalog and
// persists it. The function body is a single SELECT captured at parse time.
func (e *Executor) executeCreateFunction(ctx context.Context, stmt *ast.CreateFunction) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	if stmt.Query == nil && len(stmt.Body) == 0 {
		return nil, fmt.Errorf("CREATE FUNCTION body must contain a SELECT or statements")
	}

	schema := stmt.Name.Alias // DDL convention: schema carried in Alias
	if schema == "" {
		schema = "public"
	}

	// Validate the body SELECT runs (surfaces bad column/table references now).
	// A statement body (trigger / side-effecting function) is not run here.
	if stmt.Query != nil {
		if _, err := e.executeSelect(ctx, stmt.Query); err != nil {
			return nil, fmt.Errorf("failed to validate function body: %w", err)
		}
	}

	returns := make([]catalog.Column, len(stmt.Returns))
	for i, p := range stmt.Returns {
		returns[i] = catalog.Column{Name: p.Name.Name, Type: p.Type}
	}

	fn := &catalog.Function{
		Name:        stmt.Name.Name,
		Schema:      schema,
		Parameters:  stmt.Parameters,
		Returns:     returns,
		SetOf:       stmt.SetOf,
		ReturnsType: stmt.ReturnsType,
		Query:       stmt.Query,
		QueryText:   stmt.QueryText,
		Body:        stmt.Body,
		BodyText:    stmt.BodyText,
	}

	if err := e.catalog.CreateFunction(fn, stmt.Replace, stmt.IfNotExists); err != nil {
		return nil, fmt.Errorf("failed to create function %s: %w", stmt.Name.Name, err)
	}

	if e.storage != nil {
		if err := e.storage.SaveFunction(fn, schema); err != nil {
			fmt.Printf("warning: failed to persist function %s.%s: %v\n", schema, stmt.Name.Name, err)
		}
	}

	return &Result{Tag: constants.ResultCreateFunction}, nil
}

func (e *Executor) executeDropFunction(ctx context.Context, stmt *ast.DropFunction) (*Result, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	schema := stmt.Name.Alias
	if schema == "" {
		schema = "public"
	}

	if err := e.catalog.DropFunction(stmt.Name.Name, stmt.IfExists, schema); err != nil {
		return nil, fmt.Errorf("failed to drop function %s: %w", stmt.Name.Name, err)
	}

	if e.storage != nil {
		if err := e.storage.DeleteFunction(stmt.Name.Name, schema); err != nil {
			fmt.Printf("warning: failed to delete persisted function %s.%s: %v\n", schema, stmt.Name.Name, err)
		}
	}

	return &Result{Tag: constants.ResultDropFunction}, nil
}

// maxFunctionCallDepth bounds nested set-returning function calls so a function
// whose body references itself (directly or through a cycle) cannot recurse into
// a stack overflow and crash the server.
const maxFunctionCallDepth = 32

// resolveFunctionTable runs a set-returning function referenced in a FROM clause
// and returns its result as a virtual table (same shape the view path uses).
func (e *Executor) resolveFunctionTable(ctx context.Context, call ast.Identifier) (*catalog.Table, error) {
	if e.funcDepth >= maxFunctionCallDepth {
		return nil, fmt.Errorf("function call depth exceeded (%d) resolving %s; recursive function?", maxFunctionCallDepth, call.Name)
	}
	e.funcDepth++
	defer func() { e.funcDepth-- }()

	fn, err := e.catalog.GetFunction(call.Name, call.Schema)
	if err != nil {
		return nil, err
	}
	if len(call.Args) != len(fn.Parameters) {
		return nil, fmt.Errorf("function %s expects %d argument(s), got %d", fn.Name, len(fn.Parameters), len(call.Args))
	}

	query := fn.Query
	if len(fn.Parameters) > 0 {
		q, err := substituteFunctionArgs(fn, call.Args)
		if err != nil {
			return nil, err
		}
		query = q
	}
	if query == nil {
		return nil, fmt.Errorf("function %s has no query body", fn.Name)
	}

	res, err := e.executeSelect(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("error running function %s: %w", fn.Name, err)
	}

	cols := fn.Returns
	// Fall back to the SELECT's own output columns when RETURNS gave no column
	// list (SETOF / scalar) or the counts disagree.
	if len(cols) != len(res.Columns) {
		cols = make([]catalog.Column, len(res.Columns))
		for i, name := range res.Columns {
			cols[i] = catalog.Column{Name: name, Type: "TEXT"}
		}
	}
	return &catalog.Table{Name: fn.Name, Columns: cols, Rows: res.Rows}, nil
}

// substituteFunctionArgs binds a function call's arguments to the parameter
// names referenced in the function's SELECT text, then re-parses it. Covers
// parameters referenced by name; general plpgsql is out of scope.
//
// Binding is a single left-to-right pass (bindParamsInSQL) so a substituted
// value is never rescanned, string-literal contents are never touched, and a
// qualified reference like "c.min_cred" is left alone — addressing the
// text-substitution hazards an engine with no prepared-parameter protocol would
// otherwise have. Argument values are validated before insertion.
func substituteFunctionArgs(fn *catalog.Function, args []ast.Literal) (*ast.Select, error) {
	text := fn.QueryText
	if text == "" {
		return fn.Query, nil
	}

	values := make(map[string]string, len(fn.Parameters))
	for i, p := range fn.Parameters {
		if p.Name.Name == "" {
			continue // unnamed parameter: not referenceable by name
		}
		val, err := renderLiteral(args[i])
		if err != nil {
			return nil, fmt.Errorf("argument %d (%s): %w", i+1, p.Name.Name, err)
		}
		values[strings.ToLower(p.Name.Name)] = val
	}

	bound := bindParamsInSQL(text, values)

	node, err := parser.NewParser(bound).ParseStatement()
	if err != nil {
		return nil, fmt.Errorf("error binding function arguments: %w", err)
	}
	sel, ok := node.(*ast.Select)
	if !ok {
		return nil, fmt.Errorf("function body did not re-parse to a SELECT")
	}
	return sel, nil
}

// bindParamsInSQL replaces, in a single pass, every bare identifier that names a
// parameter with its bound value. It copies single-quoted string literals
// verbatim (honoring '' escapes), skips an identifier used as a member access
// (preceded by '.', e.g. a qualified column) and one used as a function name
// (followed by '('), and never rescans text it has already emitted — so a value
// can never be interpreted as a parameter name.
func bindParamsInSQL(text string, values map[string]string) string {
	var b strings.Builder
	b.Grow(len(text))
	prevSignificant := byte(0) // last non-space byte emitted from the source
	i := 0
	for i < len(text) {
		c := text[i]
		if c == '\'' {
			b.WriteByte(c)
			i++
			for i < len(text) {
				b.WriteByte(text[i])
				if text[i] == '\'' {
					if i+1 < len(text) && text[i+1] == '\'' { // escaped quote
						b.WriteByte(text[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			prevSignificant = '\''
			continue
		}
		if isIdentStart(c) {
			j := i + 1
			for j < len(text) && isIdentPart(text[j]) {
				j++
			}
			word := text[i:j]
			k := j
			for k < len(text) && (text[k] == ' ' || text[k] == '\t' || text[k] == '\r' || text[k] == '\n') {
				k++
			}
			isMember := prevSignificant == '.'
			isCall := k < len(text) && text[k] == '('
			if val, ok := values[strings.ToLower(word)]; ok && !isMember && !isCall {
				b.WriteString(val)
			} else {
				b.WriteString(word)
			}
			prevSignificant = word[len(word)-1]
			i = j
			continue
		}
		b.WriteByte(c)
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			prevSignificant = c
		}
		i++
	}
	return b.String()
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// renderLiteral renders a validated call argument as SQL text for substitution.
// String values are single-quoted with embedded quotes doubled; numbers are
// checked to be well-formed; identifiers must be a plain word (the lexer already
// guarantees this). This keeps a crafted argument from reshaping the query.
func renderLiteral(lit ast.Literal) (string, error) {
	switch lit.Kind {
	case "string":
		return "'" + strings.ReplaceAll(lit.Value, "'", "''") + "'", nil
	case "number":
		if !isNumericLiteral(lit.Value) {
			return "", fmt.Errorf("invalid numeric argument %q", lit.Value)
		}
		return lit.Value, nil
	case "identifier":
		if !isPlainIdentifier(lit.Value) {
			return "", fmt.Errorf("invalid identifier argument %q", lit.Value)
		}
		return lit.Value, nil
	default:
		return "", fmt.Errorf("unsupported argument kind %q", lit.Kind)
	}
}

func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '+' || s[0] == '-' {
		i++
	}
	digits, dot := false, false
	for ; i < len(s); i++ {
		switch {
		case s[i] >= '0' && s[i] <= '9':
			digits = true
		case s[i] == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return digits
}

func isPlainIdentifier(s string) bool {
	if s == "" || !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentPart(s[i]) {
			return false
		}
	}
	return true
}
