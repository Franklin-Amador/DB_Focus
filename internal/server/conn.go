package server

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net"
	"strings"

	"dbf/internal/catalog"
)

// Default max connections to prevent OOM on limited memory systems
const defaultMaxConnections = 20

func ListenAndServe(addr string, handler QueryHandler, cat *catalog.Catalog) error {
	return ListenAndServeWithConfig(addr, handler, cat, defaultMaxConnections, 4096)
}

func ListenAndServeWithConfig(addr string, handler QueryHandler, cat *catalog.Catalog, maxConns int, bufSize int) error {
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

func handleConnWithBufSize(conn net.Conn, handler QueryHandler, cat *catalog.Catalog, bufSize int) {
	defer conn.Close()
	log.Printf("[conn] new connection from %s", conn.RemoteAddr())
	rw := bufio.NewReadWriter(
		bufio.NewReaderSize(conn, bufSize),
		bufio.NewWriterSize(conn, bufSize),
	)

	if err := rw.Flush(); err != nil {
		log.Printf("[conn] flush error: %v", err)
		return
	}

	// Authenticate (and validate database)
	user, err := authenticate(rw, cat)
	if err != nil {
		// Hosted platforms often probe open ports and close immediately.
		// Treat EOF-style auth failures as expected and keep logs clean.
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			log.Printf("[conn] authentication failed: %v", err)
		}
		return
	}
	log.Printf("[conn] authenticated as user: %s", user)

	// Register user in catalog
	if err := cat.RegisterUser(user, true); err != nil {
		log.Printf("[conn] warning: failed to register user: %v", err)
	}

	lastPrepared := ""

	for {
		msgType, payload, err := readMessage(rw)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				log.Printf("[conn] message read error: %v", err)
			}
			return
		}

		switch msgType {
		case 'Q':
			if err := handleSimpleQuery(rw, handler, cat, payload); err != nil {
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
			handleExecute(rw, handler, lastPrepared)
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

func handleSimpleQuery(rw *bufio.ReadWriter, handler QueryHandler, cat *catalog.Catalog, payload []byte) error {
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

		if sysResult, ok := cat.HandleSystemQuery(stmt); ok {
			log.Printf("[msg Q] system query handled")
			writeSystemResult(rw, sysResult)
			continue
		}

		result, err := handler.Handle(stmt)
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

func handleExecute(rw *bufio.ReadWriter, handler QueryHandler, lastPrepared string) {
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

	log.Printf("[msg E] calling handler.Handle()...")
	result, err := handler.Handle(lastPrepared)
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

func authenticate(rw *bufio.ReadWriter, cat *catalog.Catalog) (string, error) {
	params, err := readStartup(rw)
	if err != nil {
		return "", err
	}

	if err := writeAuthRequestCleartext(rw); err != nil {
		return "", err
	}
	if err := rw.Flush(); err != nil {
		return "", err
	}

	password, err := readPassword(rw)
	if err != nil {
		return "", err
	}

	if password != "4444" {
		writeError(rw, "invalid password")
		rw.Flush()
		return "", errors.New("invalid password")
	}

	// Validate database if specified
	if database, ok := params["database"]; ok && database != "" {
		if !cat.DatabaseExists(database) {
			log.Printf("[conn] database %q does not exist", database)
			writeError(rw, "database \""+database+"\" does not exist")
			rw.Flush()
			return "", errors.New("database does not exist")
		}
		log.Printf("[conn] database %q validated", database)
	}

	if err := writeAuthOK(rw); err != nil {
		return "", err
	}

	user := params["user"]
	if user == "" {
		user = "focus"
	}

	if err := writeStartupResponse(rw, user); err != nil {
		return "", err
	}
	if err := rw.Flush(); err != nil {
		return "", err
	}

	return user, nil
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

// fmtError is a tiny local helper to avoid importing fmt in this file.
// end of file
