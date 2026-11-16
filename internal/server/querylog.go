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
	oneLine := redactLiterals(strings.Join(strings.Fields(query), " "))
	fmt.Fprintf(qlFile, "%s\t%-16s\t%s\n", time.Now().Format("15:04:05.000"), disposition, oneLine)
	_ = qlFile.Sync()
}

// redactLiterals replaces every single-quoted string literal with '?', so the
// diagnostic log captures query shape without persisting values (passwords,
// PII). SQL's doubled-quote escape ('') inside a literal is handled.
func redactLiterals(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\'' {
			b.WriteByte(s[i])
			i++
			continue
		}
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
	}
	return b.String()
}
