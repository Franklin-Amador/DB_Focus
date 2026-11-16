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

// maxSubstitutedQueryLen caps the size of a query after parameter inlining, so a
// few large parameters cannot amplify it without bound.
const maxSubstitutedQueryLen = 1 << 20 // 1 MiB

// substituteParams inlines bound parameters ($1, $2, ...) into a query in a
// SINGLE left-to-right pass, so an inserted value is never re-scanned (a value
// that itself contains "$1" cannot trigger a second substitution). A value that
// is strictly numeric is inserted bare; every other value is single-quoted with
// the SQL '' escape, so it cannot break out of its string literal. This lets the
// system-catalog routes read the real values of introspection queries such as
// "... WHERE typname = $1".
//
// It is a compatibility shim for an engine with no placeholder planning, not a
// trust boundary: a client that can Bind parameters can already run arbitrary
// SQL over the simple-query protocol.
func substituteParams(query string, params []string) string {
	if len(params) == 0 || !strings.ContainsRune(query, '$') {
		return query
	}
	var b strings.Builder
	for i := 0; i < len(query); {
		if query[i] == '$' && i+1 < len(query) && query[i+1] >= '1' && query[i+1] <= '9' {
			j := i + 1
			for j < len(query) && query[j] >= '0' && query[j] <= '9' {
				j++
			}
			if n, err := strconv.Atoi(query[i+1 : j]); err == nil && n >= 1 && n <= len(params) {
				b.WriteString(quoteParam(params[n-1]))
				if b.Len() > maxSubstitutedQueryLen {
					return query // refuse to amplify; the original will fail naturally
				}
				i = j
				continue
			}
		}
		b.WriteByte(query[i])
		i++
	}
	return b.String()
}

func quoteParam(val string) string {
	if isStrictNumeric(val) {
		return val
	}
	return "'" + strings.ReplaceAll(val, "'", "''") + "'"
}

// isStrictNumeric reports whether s is a well-formed number, so inlining it bare
// cannot inject syntax (e.g. "--" or "1 OR 1=1" are rejected and get quoted).
func isStrictNumeric(s string) bool {
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
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
