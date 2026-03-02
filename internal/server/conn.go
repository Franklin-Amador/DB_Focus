package server

import (
	"bufio"
	"errors"
	"log"
	"net"
	"strings"

	"dbf/internal/catalog"
)

func ListenAndServe(addr string, handler QueryHandler, cat *catalog.Catalog) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go handleConn(conn, handler, cat)
	}
}

func handleConn(conn net.Conn, handler QueryHandler, cat *catalog.Catalog) {
	log.Printf("[conn] new connection from %s", conn.RemoteAddr())
	defer conn.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	if err := rw.Flush(); err != nil {
		log.Printf("[conn] flush error: %v", err)
		return
	}

	// Authenticate
	user, err := authenticate(rw)
	if err != nil {
		log.Printf("[conn] authentication failed: %v", err)
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
			log.Printf("[conn] message read error: %v", err)
			return
		}
		log.Printf("[conn] received message type=%c payload_len=%d", msgType, len(payload))

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
			log.Printf("[msg B] bind")
			writeBindComplete(rw)
		case 'D':
			log.Printf("[msg D] describe")
			handleDescribe(rw, lastPrepared)
		case 'E':
			log.Printf("[msg E] execute")
			handleExecute(rw, handler, lastPrepared)
		case 'S':
			log.Printf("[msg S] sync")
			if err := writeReady(rw); err != nil {
				log.Printf("[msg S] writeReady error: %v", err)
				return
			}
		case 'H':
			log.Printf("[msg H] flush")
			rw.Flush()
		case 'X':
			log.Printf("[conn] close")
			log.Printf("[conn] close requested")
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
	log.Printf("[msg Q] query: %q", query)

	if strings.TrimSpace(query) == "" {
		writeMessage(rw, 'I', nil)
		writeReady(rw)
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
	return nil
}

func handleParse(rw *bufio.ReadWriter, payload []byte, lastPrepared *string) error {
	log.Printf("[msg P] parse")
	query, err := parseParseMessage(payload)
	if err != nil {
		log.Printf("[msg P] error: %v", err)
		writeError(rw, err.Error())
		return nil
	}
	log.Printf("[msg P] prepared query: %q", query)
	*lastPrepared = query
	writeParseComplete(rw)
	return nil
}

func handleDescribe(rw *bufio.ReadWriter, lastPrepared string) {
	if isSelectOne(lastPrepared) {
		writeRowDescriptionSelectOne(rw)
	} else {
		writeNoData(rw)
	}
}

func handleExecute(rw *bufio.ReadWriter, handler QueryHandler, lastPrepared string) {
	log.Printf("[msg E] executing: %q", lastPrepared) // agrega esto

	if lastPrepared == "" {
		writeError(rw, "no prepared query")
		writeReady(rw)
		return
	}

	if isSelectOne(lastPrepared) {
		writeDataRowSelectOne(rw)
		writeCommandComplete(rw, "SELECT 1")
		return
	}

	if result := checkBypass(lastPrepared); result != nil {
		log.Printf("[msg E] bypass query")
		handleBypass(rw, result)
		return
	}

	result, err := handler.Handle(lastPrepared)
	if err != nil {
		log.Printf("[msg E] handler error: %v", err)
		writeError(rw, err.Error())
		writeReady(rw) // ← importante para resincronizar
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
		return
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
	} else if len(result.Rows) > 0 {
		// Hay filas pero no columnas — problema en el executor
		log.Printf("[msg E] WARNING: rows without columns, skipping data")
	}
	writeCommandComplete(rw, result.Tag)
}

func authenticate(rw *bufio.ReadWriter) (string, error) {
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
