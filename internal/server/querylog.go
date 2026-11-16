package server

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Query logging is an opt-in diagnostic that records every query the server
// receives, together with how it was resolved (bypass, system route, executed
// with N rows, or an executor error). It exists to capture exactly what a client
// such as DBeaver sends during introspection, so the system-catalog gap can be
// measured instead of guessed. It is off unless EnableQueryLog is called.

var (
	qlMu   sync.Mutex
	qlFile *os.File
)

// EnableQueryLog opens (appending) the file queries are logged to. Passing an
// empty path disables logging. Safe to call once at startup.
func EnableQueryLog(path string) error {
	qlMu.Lock()
	defer qlMu.Unlock()
	if qlFile != nil {
		_ = qlFile.Close()
		qlFile = nil
	}
	if path == "" {
		return nil
	}
	// Owner-only: the log records query shapes and may otherwise capture values.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_ = f.Chmod(0o600) // tighten a pre-existing file too
	qlFile = f
	fmt.Fprintf(f, "\n=== query log opened %s ===\n", time.Now().Format(time.RFC3339))
	_ = f.Sync()
	return nil
}

// logQuery records one query and its disposition. It collapses whitespace so
// each (often multi-line) introspection query occupies a single, greppable line.
func logQuery(disposition, query string) {
	qlMu.Lock()
	defer qlMu.Unlock()
	if qlFile == nil {
		return
	}
	// Redact both the query and the disposition (executor errors can echo a
	// literal value, e.g. "duplicate value '...'").
	oneLine := redactLiterals(strings.Join(strings.Fields(query), " "))
	disp := redactLiterals(disposition)
	fmt.Fprintf(qlFile, "%s\t%-16s\t%s\n", time.Now().Format("15:04:05.000"), disp, oneLine)
	_ = qlFile.Sync()
}

// redactLiterals replaces string values with '?' so the diagnostic log captures
// query shape without persisting data (passwords, PII). It covers single-quoted
// literals (with the '' escape) and dollar-quoted bodies ($$...$$ / $tag$...$tag$,
// used by procedure bodies). It is deliberately conservative: on anything it
// cannot pair it over-redacts rather than leaking. This is defense-in-depth for a
// local, opt-in, owner-only (0600) diagnostic file, not a security boundary, so
// it does not aim to match the engine lexer byte-for-byte.
func redactLiterals(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		switch {
		case s[i] == '\'':
			b.WriteString("'?'")
			i++ // opening quote
			for i < len(s) {
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						i += 2 // escaped '' — stay inside the literal
						continue
					}
					i++ // closing quote
					break
				}
				i++
			}
		case s[i] == '$':
			if tag, ok := dollarQuoteTag(s, i); ok {
				b.WriteString(tag + "?" + tag)
				i += len(tag)
				if end := strings.Index(s[i:], tag); end >= 0 {
					i += end + len(tag)
				} else {
					i = len(s) // unterminated — redact to end
				}
				continue
			}
			b.WriteByte(s[i])
			i++
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// dollarQuoteTag returns the dollar-quote tag ($$ or $name$) starting at i, if
// s[i:] opens one.
func dollarQuoteTag(s string, i int) (string, bool) {
	j := i + 1
	for j < len(s) {
		c := s[j]
		if c == '$' {
			return s[i : j+1], true
		}
		if c != '_' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return "", false
		}
		j++
	}
	return "", false
}
