package server

import (
	"bufio"
	"errors"
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
			if err := handleParse(rw, payload, &lastPrepared); err != nil {
				log.Printf("[conn] parse error: %v", err)
			}
		case 'B':
			writeBindComplete(rw)
			rw.Flush()
		case 'D':
			handleDescribe(rw, lastPrepared)
		case 'E':
			handleExecute(rw, handler, lastPrepared, sess)
		case 'S':
			if err := writeReady(rw); err != nil {
				log.Printf("[msg S] writeReady error: %v", err)
				return
			}
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
			handleSelectOne(rw)
			continue
		}

		if result := checkBypass(stmt); result != nil {
			log.Printf("[msg Q] bypass query")
			handleBypass(rw, result)
			continue
		}

		// SET search_path changes the session schema (like PostgreSQL).
		if target := searchPathTarget(stmt); target != "" {
			sess.schema = target
			log.Printf("[msg Q] search_path -> %s", target)
			writeEmptyRowDescription(rw)
			writeCommandComplete(rw, "SET")
			continue
		}

		if sysResult, ok := cat.HandleSystemQueryForSession(stmt, sess.database, sess.schema); ok {
			log.Printf("[msg Q] system query handled")
			writeSystemResult(rw, sysResult)
			continue
		}

		result, err := executeQuery(handler, stmt, sess)
		if err != nil {
			log.Printf("[msg Q] handler error: %v", err)
			writeError(rw, err.Error())
			writeReady(rw)
			rw.Flush()
			return nil
		}
		if result == nil {
			writeEmptyRowDescription(rw)
			writeCommandComplete(rw, "OK")
			continue
		}
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

func handleDescribe(rw *bufio.ReadWriter, lastPrepared string) {
	if isSelectOne(lastPrepared) {
		writeRowDescriptionSelectOne(rw)
		log.Printf("[msg D] sent RowDescription")
	} else {
		writeNoData(rw)
		log.Printf("[msg D] sent NoData")
	}
	rw.Flush()
}

func handleExecuteWithDatabase(rw *bufio.ReadWriter, handler QueryHandler, lastPrepared string, sess *session) {
	log.Printf("[msg E] executing: %q", lastPrepared) // agrega esto

	if lastPrepared == "" {
		writeError(rw, "no prepared query")
		// NO writeReady() aquí - esperar Sync
		rw.Flush()
		return
	}

	log.Printf("[msg E] checking if SELECT 1...")
	if isSelectOne(lastPrepared) {
		writeDataRowSelectOne(rw)
		writeCommandComplete(rw, "SELECT 1")
		log.Printf("[msg E] sent CommandComplete: SELECT 1")
		rw.Flush()
		return
	}

	log.Printf("[msg E] checking bypass...")
	if result := checkBypass(lastPrepared); result != nil {
		log.Printf("[msg E] bypass query")
		handleBypass(rw, result)
		rw.Flush()
		return
	}

	if target := searchPathTarget(lastPrepared); target != "" {
		sess.schema = target
		writeCommandComplete(rw, "SET")
		rw.Flush()
		return
	}

	log.Printf("[msg E] calling handler.Handle()...")
	result, err := executeQuery(handler, lastPrepared, sess)
	log.Printf("[msg E] handler.Handle() returned")
	if err != nil {
		log.Printf("[msg E] handler error: %v", err)
		writeError(rw, err.Error())
		writeReady(rw) // ← importante para resincronizar
		rw.Flush()
		return
	}

	// if err != nil {
	// 	log.Printf("[msg E] handler error: %v", err)
	// 	// Devolver vacío en vez de error para no matar la conexión
	// 	writeCommandComplete(rw, "SELECT 0")
	// 	return
	// }
	if result == nil {
		writeCommandComplete(rw, "OK")
		log.Printf("[msg E] sent CommandComplete: OK")
		rw.Flush()
		return
	}
	log.Printf("[msg E] result is not nil, tag=%q, cols=%d, rows=%d", result.Tag, len(result.Columns), len(result.Rows))
	if len(result.Columns) > 0 {
		var sample []interface{}
		if len(result.Rows) > 0 {
			sample = result.Rows[0]
		}
		writeRowDescriptionForResult(rw, result.Columns, sample)
		for _, row := range result.Rows {
			writeDataRow(rw, row)
		}
	} else if len(result.Rows) > 0 {
		// Hay filas pero no columnas — problema en el executor
		log.Printf("[msg E] WARNING: rows without columns, skipping data")
	}
	log.Printf("[msg E] about to send CommandComplete with tag: %q", result.Tag)
	writeCommandComplete(rw, result.Tag)
	log.Printf("[msg E] sent CommandComplete: %s", result.Tag)
	rw.Flush()
	log.Printf("[msg E] flushed buffer")
}

func handleExecute(rw *bufio.ReadWriter, handler QueryHandler, lastPrepared string, sess *session) {
	handleExecuteWithDatabase(rw, handler, lastPrepared, sess)
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
