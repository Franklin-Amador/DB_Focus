package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strings"
)

// searchPathRe matches "SET search_path TO x[, y]" / "SET search_path = x".
var searchPathRe = regexp.MustCompile(`(?is)^\s*SET\s+search_path\s*(?:=|\bTO\b)\s*(.+?)\s*;?\s*$`)

// searchPathTarget extracts the first schema of a SET search_path statement,
// or "" when the statement is something else.
func searchPathTarget(stmt string) string {
	m := searchPathRe.FindStringSubmatch(stmt)
	if m == nil {
		return ""
	}
	first := strings.TrimSpace(strings.SplitN(m[1], ",", 2)[0])
	first = strings.Trim(first, "\"'")
	if first == "" || first == "$user" {
		return "public"
	}
	return first
}

// Default max connections to prevent OOM on limited memory systems
const defaultMaxConnections = 20

func ListenAndServe(addr string, handler QueryHandler, cat CatalogProvider) error {
	return ListenAndServeWithConfig(addr, handler, cat, defaultMaxConnections, 4096)
}

func ListenAndServeWithConfig(addr string, handler QueryHandler, cat CatalogProvider, maxConns int, bufSize int) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	sem := make(chan struct{}, maxConns)

	log.Printf("[server] listening on %s with max %d concurrent connections (buffer size: %d bytes)", addr, maxConns, bufSize)

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				log.Printf("[server] temporary accept error: %v", err)
				continue
			}
			return err
		}

		// Acquire before spawning goroutine to keep goroutine count bounded.
		sem <- struct{}{}
		go func(c net.Conn) {
			defer func() { <-sem }() // release semaphore
			handleConnWithBufSize(c, handler, cat, bufSize)
		}(conn)
	}
}

func handleConnWithBufSize(conn net.Conn, handler QueryHandler, cat CatalogProvider, bufSize int) {
	defer conn.Close()
	remoteAddr := conn.RemoteAddr().String()
	// A panic while serving one connection must not take down the server: recover
	// it, log it, and let this goroutine (and only this connection) end.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[conn] recovered panic from %s: %v", remoteAddr, r)
		}
	}()
	rw := bufio.NewReadWriter(
		bufio.NewReaderSize(conn, bufSize),
		bufio.NewWriterSize(conn, bufSize),
	)

	if err := rw.Flush(); err != nil {
		log.Printf("[conn] flush error: %v", err)
		return
	}

	// Authenticate (and validate database)
	user, currentDatabase, err := authenticate(rw, cat)
	if err != nil {
		if !isExpectedConnError(err) {
			log.Printf("[conn] authentication failed from %s: %v", remoteAddr, err)
		}
		return
	}
	log.Printf("[conn] authenticated from %s as user: %s (db=%s)", remoteAddr, user, currentDatabase)
	sess := &session{database: currentDatabase, schema: "public"}

	// Register user in catalog
	if err := cat.RegisterUser(user, true); err != nil {
		log.Printf("[conn] warning: failed to register user: %v", err)
	}

	lastPrepared := ""

	for {
		msgType, payload, err := readMessage(rw)
		if err != nil {
			if !isExpectedConnError(err) {
				log.Printf("[conn] message read error from %s: %v", remoteAddr, err)
			}
			return
		}

		switch msgType {
		case 'Q':
			if err := handleSimpleQuery(rw, handler, cat, payload, sess); err != nil {
				log.Printf("[conn] query handler error: %v", err)
				return
			}
		case 'P':
			sess.described = false
			sess.bindParams = nil
			if err := handleParse(rw, payload, &lastPrepared); err != nil {
				log.Printf("[conn] parse error: %v", err)
			}
		case 'B':
			sess.bindParams = parseBindParams(payload)
			writeBindComplete(rw)
			rw.Flush()
		case 'D':
			handleDescribe(rw, handler, cat, payload, lastPrepared, sess)
		case 'E':
			handleExecute(rw, handler, cat, lastPrepared, sess)
		case 'S':
			if err := writeReady(rw); err != nil {
				log.Printf("[msg S] writeReady error: %v", err)
				return
			}
			rw.Flush()
		case 'C':
			// Close a prepared statement or portal. pgjdbc (DBeaver) sends this
			// after Execute; replying CloseComplete instead of an error keeps the
			// extended-protocol response stream intact (an ErrorResponse here made
			// the driver discard the result set, so grids and the metadata tree
			// came back empty).
			sess.described = false
			writeCloseComplete(rw)
			rw.Flush()
		case 'H':
			rw.Flush()
		case 'X':
			return

		default:
			log.Printf("[conn] unknown message type %c", msgType)
			writeError(rw, "unsupported message")
			writeReady(rw)
		}

		if err := rw.Flush(); err != nil {
			log.Printf("[conn] flush error: %v", err)
			return
		}
	}
}

func handleSimpleQuery(rw *bufio.ReadWriter, handler QueryHandler, cat CatalogProvider, payload []byte, sess *session) error {
	query := strings.TrimRight(string(payload), "\x00")

	if strings.TrimSpace(query) == "" {
		writeMessage(rw, 'I', nil)
		writeReady(rw)
		rw.Flush()
		return nil
	}

	// Split múltiples statements
	statements := splitStatements(query)

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		if isSelectOne(stmt) {
			logQuery("Q:select1", stmt)
			handleSelectOne(rw)
			continue
		}

		if result := checkBypass(stmt); result != nil {
			logQuery("Q:bypass", stmt)
			log.Printf("[msg Q] bypass query")
			handleBypass(rw, result)
			continue
		}

		// SET search_path changes the session schema (like PostgreSQL).
		if target := searchPathTarget(stmt); target != "" {
			logQuery("Q:set-path", stmt)
			sess.schema = target
			log.Printf("[msg Q] search_path -> %s", target)
			writeEmptyRowDescription(rw)
			writeCommandComplete(rw, "SET")
			continue
		}

		if sysResult, ok := cat.HandleSystemQueryForSession(stmt, sess.database, sess.schema); ok {
			logQuery(fmt.Sprintf("Q:system:%drows", len(sysResult.Rows)), stmt)
			log.Printf("[msg Q] system query handled")
			writeSystemResult(rw, sysResult)
			continue
		}

		result, err := executeQuery(handler, stmt, sess)
		if err != nil {
			logQuery("Q:exec-ERROR: "+err.Error(), stmt)
			log.Printf("[msg Q] handler error: %v", err)
			writeError(rw, err.Error())
			writeReady(rw)
			rw.Flush()
			return nil
		}
		if result == nil {
			logQuery("Q:exec:ok-nil", stmt)
			writeEmptyRowDescription(rw)
			writeCommandComplete(rw, "OK")
			continue
		}
		logQuery(fmt.Sprintf("Q:exec:%drows", len(result.Rows)), stmt)
		if len(result.Columns) > 0 {
			var sample []interface{}
			if len(result.Rows) > 0 {
				sample = result.Rows[0]
			}
			writeRowDescriptionForResult(rw, result.Columns, sample)
			for _, row := range result.Rows {
				writeDataRow(rw, row)
			}
		}
		writeCommandComplete(rw, result.Tag)
	}

	writeReady(rw)
	rw.Flush()
	return nil
}

func splitStatements(query string) []string {
	var stmts []string
	var current strings.Builder
	inString := false

	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '\'' {
			inString = !inString
		}
		if ch == ';' && !inString {
			if s := strings.TrimSpace(current.String()); s != "" {
				stmts = append(stmts, s)
			}
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	if s := strings.TrimSpace(current.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}

func handleSelectOne(rw *bufio.ReadWriter) error {
	log.Printf("[msg Q] matched SELECT 1")
	writeRowDescriptionSelectOne(rw)
	writeDataRowSelectOne(rw)
	writeCommandComplete(rw, "SELECT 1")
	writeReady(rw)
	rw.Flush()
	return nil
}

func handleParse(rw *bufio.ReadWriter, payload []byte, lastPrepared *string) error {
	log.Printf("[msg P] parse")
	query, err := parseParseMessage(payload)
	if err != nil {
		log.Printf("[msg P] error: %v", err)
		writeError(rw, err.Error())
		rw.Flush()
		return nil
	}
	log.Printf("[msg P] prepared query: %q", query)
	*lastPrepared = query
	writeParseComplete(rw)
	log.Printf("[msg P] sent ParseComplete")
	rw.Flush()
	return nil
}

// computeExtended resolves an extended-protocol query through the same layers as
// the simple-query path (SELECT 1, bypass, SET search_path, system catalog, then
// the executor) WITHOUT writing to the wire. kind ("select1" | "set" | "result")
// tells the caller how to frame the Describe/Execute replies.
func computeExtended(handler QueryHandler, cat CatalogProvider, query string, sess *session, describe bool) (cols []string, rows [][]interface{}, tag string, kind string, err error) {
	// Route on the ORIGINAL prepared text so a parameter value cannot steer the
	// query onto a different fast path. Parameters are inlined only for the
	// system-catalog route and the executor, which need the real values.
	if isSelectOne(query) {
		return nil, nil, "SELECT 1", "select1", nil
	}
	if result := checkBypass(query); result != nil {
		return result.Columns, result.Rows, result.Tag, "result", nil
	}
	if target := searchPathTarget(query); target != "" {
		if describe {
			// Apply the search_path change only at Execute, so a Describe that
			// precedes it does not mutate session state twice.
			return nil, nil, "SET", "skip", nil
		}
		sess.schema = target
		return nil, nil, "SET", "set", nil
	}
	query = substituteParams(query, sess.bindParams)
	if sysResult, ok := cat.HandleSystemQueryForSession(query, sess.database, sess.schema); ok {
		return sysResult.Columns, sysResult.Rows, sysResult.Tag, "result", nil
	}
	// A Describe must not run a state-changing statement: Execute runs it, and
	// running it here too would duplicate the write. Skip the executor during
	// Describe for anything that is not a read-only query (the field structure of
	// an INSERT/UPDATE/DELETE/DDL/CALL is empty anyway → NoData).
	if describe && !isReadOnlyQuery(query) {
		return nil, nil, "", "skip", nil
	}
	result, err := executeQuery(handler, query, sess)
	if err != nil {
		return nil, nil, "", "", err
	}
	if result == nil {
		return nil, nil, "OK", "result", nil
	}
	return result.Columns, result.Rows, result.Tag, "result", nil
}

// handleDescribe answers a Describe ('S' statement or 'P' portal). It resolves
// the prepared query now and caches the outcome, then sends the real
// RowDescription so the client has a field structure for the rows Execute will
// send. This fixes the JDBC "received tuples, but no field structure" error that
// came from always replying NoData here.
// handleDescribe answers a Describe ('S' statement or 'P' portal). It computes
// the query once to emit the real RowDescription (field structure), so the
// client has it before Execute sends the rows. It does NOT cache the rows: the
// client may Bind parameters after Describe, so Execute recomputes to pick them
// up. This fixes the "received tuples, but no field structure" error.
func handleDescribe(rw *bufio.ReadWriter, handler QueryHandler, cat CatalogProvider, payload []byte, lastPrepared string, sess *session) {
	statement := len(payload) > 0 && payload[0] == 'S'

	cols, rows, _, kind, err := computeExtended(handler, cat, lastPrepared, sess, true)
	if err != nil {
		// Report nothing now; Execute re-runs and surfaces the error.
		sess.described = false
		if statement {
			writeParameterDescription(rw, countParams(lastPrepared))
		}
		writeNoData(rw)
		rw.Flush()
		return
	}
	sess.described = true
	logQuery(fmt.Sprintf("D:%s:%dcols", kind, len(cols)), lastPrepared)

	if statement {
		writeParameterDescription(rw, countParams(lastPrepared))
	}
	switch {
	case kind == "select1":
		writeRowDescriptionSelectOne(rw)
	case kind == "result" && len(cols) > 0:
		var sample []interface{}
		if len(rows) > 0 {
			sample = rows[0]
		}
		writeRowDescriptionForResult(rw, cols, sample)
	default: // SET / empty result
		writeNoData(rw)
	}
	rw.Flush()
}

// handleExecute runs the query with the parameters bound so far and sends the
// rows. It recomputes rather than replaying Describe's result, so parameters
// bound after Describe take effect; the RowDescription is sent here only when no
// Describe preceded this Execute.
func handleExecute(rw *bufio.ReadWriter, handler QueryHandler, cat CatalogProvider, lastPrepared string, sess *session) {
	described := sess.described
	sess.described = false

	if lastPrepared == "" {
		writeError(rw, "no prepared query")
		rw.Flush()
		return
	}
	cols, rows, tag, kind, err := computeExtended(handler, cat, lastPrepared, sess, false)
	if err != nil {
		logQuery("E:exec-ERROR: "+err.Error(), lastPrepared)
		writeError(rw, err.Error())
		writeReady(rw) // resync
		rw.Flush()
		return
	}
	switch kind {
	case "select1":
		writeDataRowSelectOne(rw)
		writeCommandComplete(rw, "SELECT 1")
	case "set":
		writeCommandComplete(rw, "SET")
	default:
		logQuery(fmt.Sprintf("E:exec:%drows", len(rows)), lastPrepared)
		if !described && len(cols) > 0 { // no Describe sent the structure yet
			var sample []interface{}
			if len(rows) > 0 {
				sample = rows[0]
			}
			writeRowDescriptionForResult(rw, cols, sample)
		}
		for _, row := range rows {
			writeDataRow(rw, row)
		}
		writeCommandComplete(rw, tag)
	}
	rw.Flush()
}

func executeQuery(handler QueryHandler, query string, sess *session) (*QueryResult, error) {
	if sh, ok := handler.(SessionQueryHandler); ok {
		return sh.HandleWithSession(query, sess.database, sess.schema)
	}
	if dbHandler, ok := handler.(DatabaseQueryHandler); ok {
		return dbHandler.HandleWithDatabase(query, sess.database)
	}
	return handler.Handle(query)
}

func authenticate(rw *bufio.ReadWriter, cat CatalogProvider) (string, string, error) {
	params, err := readStartup(rw)
	if err != nil {
		return "", "", err
	}

	if err := writeAuthRequestCleartext(rw); err != nil {
		return "", "", err
	}
	if err := rw.Flush(); err != nil {
		return "", "", err
	}

	password, err := readPassword(rw)
	if err != nil {
		return "", "", err
	}

	if password != "4444" {
		writeError(rw, "invalid password")
		rw.Flush()
		return "", "", errors.New("invalid password")
	}

	currentDatabase := "postgres"
	if database, ok := params["database"]; ok && database != "" {
		currentDatabase = database
	}

	// Validate database if specified
	if database, ok := params["database"]; ok && database != "" {
		if !cat.DatabaseExists(database) {
			log.Printf("[conn] database %q does not exist", database)
			writeError(rw, "database \""+database+"\" does not exist")
			rw.Flush()
			return "", "", errors.New("database does not exist")
		}
		log.Printf("[conn] database %q validated", database)
	}

	if err := writeAuthOK(rw); err != nil {
		return "", "", err
	}

	user := params["user"]
	if user == "" {
		user = "focus"
	}

	if err := writeStartupResponse(rw, user); err != nil {
		return "", "", err
	}
	if err := rw.Flush(); err != nil {
		return "", "", err
	}

	return user, currentDatabase, nil
}

func writeStartupResponse(rw *bufio.ReadWriter, user string) error {
	// Parameters clients commonly expect after startup/auth
	params := map[string]string{
		"server_version":              "16.1",
		"server_encoding":             "UTF8",
		"client_encoding":             "UTF8",
		"DateStyle":                   "ISO, MDY",
		"TimeZone":                    "UTC",
		"integer_datetimes":           "on",
		"standard_conforming_strings": "on",
		"application_name":            "",
		"session_authorization":       user,
	}

	for k, v := range params {
		if err := writeParameterStatus(rw, k, v); err != nil {
			return err
		}
	}

	if err := writeBackendKeyData(rw, 1, 1); err != nil {
		return err
	}

	return writeReady(rw)
}

func isExpectedConnError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "invalid startup length") ||
		strings.Contains(msg, "expected password message") ||
		strings.Contains(msg, "startup message too large") ||
		strings.Contains(msg, "message too large") ||
		strings.Contains(msg, "invalid message length") {
		return true
	}
	return false
}
