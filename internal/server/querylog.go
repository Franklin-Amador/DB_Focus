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
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
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
	oneLine := strings.Join(strings.Fields(query), " ")
	fmt.Fprintf(qlFile, "%s\t%-16s\t%s\n", time.Now().Format("15:04:05.000"), disposition, oneLine)
	_ = qlFile.Sync()
}
