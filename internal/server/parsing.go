package server

import (
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
)

func parseStartupParams(payload []byte) map[string]string {
	params := make(map[string]string)
	start := 0
	parts := []string{}
	for i, b := range payload {
		if b == 0 {
			if i > start {
				parts = append(parts, string(payload[start:i]))
			}
			start = i + 1
		}
	}
	for i := 0; i+1 < len(parts); i += 2 {
		params[strings.ToLower(parts[i])] = parts[i+1]
	}
	return params
}

func parseParseMessage(payload []byte) (string, error) {
	idx := 0
	_, next, ok := readCString(payload, idx)
	if !ok {
		return "", errors.New("invalid parse message")
	}
	query, next, ok := readCString(payload, next)
	if !ok {
		return "", errors.New("invalid parse message")
	}
	if len(query) == 0 {
		return "", errors.New("empty query")
	}
	return query, nil
}

func readCString(payload []byte, start int) (string, int, bool) {
	for i := start; i < len(payload); i++ {
		if payload[i] == 0 {
			return string(payload[start:i]), i + 1, true
		}
	}
	return "", start, false
}

// parseBindParams extracts the parameter values from a Bind ('B') message so
// they can be inlined into the prepared query (the engine has no real plan for
// placeholders). Parameters are read as text; the format codes are skipped.
func parseBindParams(payload []byte) []string {
	idx := 0
	if _, next, ok := readCString(payload, idx); ok { // destination portal
		idx = next
	} else {
		return nil
	}
	if _, next, ok := readCString(payload, idx); ok { // source statement
		idx = next
	} else {
		return nil
	}
	if idx+2 > len(payload) {
		return nil
	}
	fmtCount := int(binary.BigEndian.Uint16(payload[idx:]))
	idx += 2 + fmtCount*2
	if idx+2 > len(payload) {
		return nil
	}
	paramCount := int(binary.BigEndian.Uint16(payload[idx:]))
	idx += 2
	params := make([]string, 0, paramCount)
	for i := 0; i < paramCount; i++ {
		if idx+4 > len(payload) {
			break
		}
		length := int32(binary.BigEndian.Uint32(payload[idx:]))
		idx += 4
		if length < 0 { // SQL NULL
			params = append(params, "")
			continue
		}
		if idx+int(length) > len(payload) {
			break
		}
		params = append(params, string(payload[idx:idx+int(length)]))
		idx += int(length)
	}
	return params
}

// substituteParams inlines bound parameters ($1, $2, ...) into a query. A value
// that looks numeric is inserted bare; otherwise it is single-quoted (with the
// SQL '' escape), so the system-catalog routes can read the real values of
// introspection queries such as "... WHERE typname = $1".
func substituteParams(query string, params []string) string {
	if len(params) == 0 {
		return query
	}
	for i := len(params); i >= 1; i-- { // high-to-low so $1 does not match $10
		val := params[i-1]
		rep := "'" + strings.ReplaceAll(val, "'", "''") + "'"
		if isNumericLiteral(val) {
			rep = val
		}
		query = strings.ReplaceAll(query, "$"+strconv.Itoa(i), rep)
	}
	return query
}

func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != '-' {
			return false
		}
	}
	return true
}

func isSelectOne(query string) bool {
	q := strings.TrimSpace(query)
	q = strings.TrimRight(q, ";")
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return false
	}
	normalized := strings.ToLower(strings.Join(fields, " "))
	return normalized == "select 1"
}
