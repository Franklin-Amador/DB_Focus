package parser

import (
	"strings"
	"unicode"
)

type Lexer struct {
	input string
	pos   int
	ch    byte
}

func NewLexer(input string) *Lexer {
	l := &Lexer{input: input, pos: -1}
	l.readChar()
	return l
}

func (l *Lexer) readQuotedIdent() string {
	l.readChar() // consume "
	start := l.pos
	for l.ch != '"' && l.ch != 0 {
		l.readChar()
	}
	s := l.input[start:l.pos]
	if l.ch == '"' {
		l.readChar()
	}
	return s
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	tok := Token{Pos: l.pos}

	switch l.ch {
	case 0:
		tok.Type = TokenEOF
	case ',':
		tok.Type = TokenComma
		tok.Literal = ","
		l.readChar()
	case '(':
		tok.Type = TokenLParen
		tok.Literal = "("
		l.readChar()
	case ')':
		tok.Type = TokenRParen
		tok.Literal = ")"
		l.readChar()
	case '*':
		tok.Type = TokenStar
		tok.Literal = "*"
		l.readChar()
	case ';':
		tok.Type = TokenSemicolon
		tok.Literal = ";"
		l.readChar()
	case '=':
		tok.Type = TokenEq
		tok.Literal = "="
		l.readChar()
	case '$':
		// Dollar-quoting: $$...$$ or tagged $tag$...$tag$ (PostgreSQL / DBeaver).
		if lit, ok := l.readDollarQuoted(); ok {
			tok.Type = TokenDollarString
			tok.Literal = lit
			return tok
		}
		// Not a valid dollar-quote (e.g. a lone $): skip it and get next token.
		l.readChar()
		return l.NextToken()
	case '\'':
		tok.Type = TokenString
		tok.Literal = l.readString()
	case '.':
		tok.Type = TokenDot
		tok.Literal = "."
		l.readChar()
	case ':':
		if l.peekChar() == ':' {
			tok.Type = TokenCast
			tok.Literal = "::"
			l.readChar()
			l.readChar()
			return tok
		}
		// Single : is not valid SQL operator - skip it
		l.readChar()
		return l.NextToken()
	case '<':
		if l.pos+1 < len(l.input) && l.input[l.pos+1] == '>' {
			l.readChar()
			l.readChar()
			tok.Type = TokenNotEq
			tok.Literal = "<>"
		} else if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
			l.readChar()
			l.readChar()
			tok.Type = TokenLte
			tok.Literal = "<="
		} else {
			l.readChar()
			tok.Type = TokenLt
			tok.Literal = "<"
		}
	case '>':
		if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
			l.readChar()
			l.readChar()
			tok.Type = TokenGte
			tok.Literal = ">="
		} else {
			l.readChar()
			tok.Type = TokenGt
			tok.Literal = ">"
		}
	case '!':
		if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
			l.readChar()
			l.readChar()
			tok.Type = TokenNotEq
			tok.Literal = "!="
		} else {
			// Bare '!' is not a valid operator - skip it
			l.readChar()
			return l.NextToken()
		}
	case '"':
		tok.Type = TokenIdent
		tok.Literal = l.readQuotedIdent()
		return tok
	case '-':
		// A '-' immediately before a digit is a negative numeric literal
		// (e.g. -5). The engine has no arithmetic operators, so a bare '-' in
		// any other position is not meaningful and is skipped. ('--' line
		// comments are already consumed by skipWhitespace.)
		if isDigit(l.peekChar()) {
			start := l.pos
			l.readChar() // consume '-'
			for isDigit(l.ch) {
				l.readChar()
			}
			tok.Type = TokenNumber
			tok.Literal = l.input[start:l.pos]
			return tok
		}
		l.readChar()
		return l.NextToken()
	default:
		if isLetter(l.ch) {
			literal := l.readIdentifier()
			upper := strings.ToUpper(literal)
			if kw, ok := keywords[upper]; ok {
				tok.Type = kw
				tok.Literal = upper
			} else {
				tok.Type = TokenIdent
				tok.Literal = literal
			}
			return tok
		}
		if isDigit(l.ch) {
			tok.Type = TokenNumber
			tok.Literal = l.readNumber()
			return tok
		}
		tok.Type = TokenEOF
		l.readChar()
	}

	return tok
}

func (l *Lexer) readChar() {
	l.pos++
	if l.pos >= len(l.input) {
		l.ch = 0
		return
	}
	l.ch = l.input[l.pos]
}

func (l *Lexer) peekChar() byte {
	if l.pos+1 >= len(l.input) {
		return 0
	}
	return l.input[l.pos+1]
}

func (l *Lexer) skipWhitespace() {
	for {
		if l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
			l.readChar()
		} else if l.ch == '-' && l.peekChar() == '-' {
			// Skip -- line comment until end of line or EOF
			for l.ch != '\n' && l.ch != 0 {
				l.readChar()
			}
		} else {
			break
		}
	}
}

func (l *Lexer) readIdentifier() string {
	start := l.pos
	for isLetter(l.ch) || isDigit(l.ch) || l.ch == '_' || l.ch == '.' {
		l.readChar()
	}
	return l.input[start:l.pos]
}

func (l *Lexer) readNumber() string {
	start := l.pos
	for isDigit(l.ch) {
		l.readChar()
	}
	return l.input[start:l.pos]
}

func (l *Lexer) readString() string {
	l.readChar()
	start := l.pos
	for l.ch != '\'' && l.ch != 0 {
		l.readChar()
	}
	s := l.input[start:l.pos]
	if l.ch == '\'' {
		l.readChar()
	}
	return s
}

// readDollarQuoted reads a dollar-quoted string. The current char is '$'. It
// supports both the untagged form ($$...$$) and the PostgreSQL/DBeaver tagged
// form ($tag$...$tag$), where tag is an identifier (letters, digits, underscore;
// not starting with a digit). The returned literal is the content between the
// opening and matching closing delimiter. It reports ok=false when the '$' does
// not begin a valid dollar-quote delimiter, leaving the lexer position unchanged
// so the caller can skip the lone '$'.
func (l *Lexer) readDollarQuoted() (string, bool) {
	// Scan the opening delimiter: '$' [tag] '$'. The tag, if present, must be a
	// valid identifier (so "$5" is not treated as a delimiter).
	i := l.pos + 1
	if i < len(l.input) && l.input[i] != '$' {
		if !isLetter(l.input[i]) {
			return "", false
		}
		for i < len(l.input) && (isLetter(l.input[i]) || isDigit(l.input[i]) || l.input[i] == '_') {
			i++
		}
	}
	if i >= len(l.input) || l.input[i] != '$' {
		return "", false
	}
	delim := l.input[l.pos : i+1] // "$$" or "$tag$"
	contentStart := i + 1

	idx := strings.Index(l.input[contentStart:], delim)
	if idx < 0 {
		// Unterminated: take the remainder and advance to EOF.
		content := l.input[contentStart:]
		l.pos = len(l.input)
		l.ch = 0
		return content, true
	}
	contentEnd := contentStart + idx
	content := l.input[contentStart:contentEnd]
	l.pos = contentEnd + len(delim)
	if l.pos < len(l.input) {
		l.ch = l.input[l.pos]
	} else {
		l.ch = 0
	}
	return content, true
}

func isLetter(ch byte) bool {
	return ch != 0 && (unicode.IsLetter(rune(ch)) || ch == '_')
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}
