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
	case '$':
		// Support simple dollar-quoting using $$...$$ (no tag support yet)
		if l.peekChar() == '$' {
			tok.Type = TokenDollarString
			tok.Literal = l.readDollarString()
			return tok
		}
		tok.Type = TokenEOF
		l.readChar()
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
		tok.Type = TokenEOF
	case '<':
		if l.pos+1 < len(l.input) && l.input[l.pos+1] == '>' {
			l.readChar()
			l.readChar()
			tok.Type = TokenNotEq
			tok.Literal = "<>"
		} else {
			l.readChar()
			tok.Type = TokenLt
			tok.Literal = "<"
		}
	case '"':
		tok.Type = TokenIdent
		tok.Literal = l.readQuotedIdent()
		return tok
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
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		l.readChar()
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

// readDollarString reads a $$...$$ quoted string (no tag support).
func (l *Lexer) readDollarString() string {
	// consume the opening $$
	l.readChar() // consume first $
	l.readChar() // consume second $
	start := l.pos
	for {
		if l.ch == 0 {
			// unterminated
			break
		}
		if l.ch == '$' && l.peekChar() == '$' {
			// end found
			end := l.pos
			// consume closing $$
			l.readChar()
			l.readChar()
			return l.input[start:end]
		}
		l.readChar()
	}
	return l.input[start:l.pos]
}

func isLetter(ch byte) bool {
	return ch != 0 && (unicode.IsLetter(rune(ch)) || ch == '_')
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}
