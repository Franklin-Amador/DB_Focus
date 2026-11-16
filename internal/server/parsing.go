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
// placeholders). The per-parameter format codes ARE honoured: a text parameter
// is taken verbatim, but a BINARY parameter is decoded — pgjdbc (DBeaver) sends
// integer filter values such as a table oid in binary (a 4-byte int), and
// reading those raw bytes as a string made predicates like "c.oid = $1" never
// match, so a metadata query returned every table's columns instead of one
// table's.
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
	idx += 2
	// A format code of 1 means binary, 0 text. The array may be empty (all
	// text), a single code (applies to every parameter) or one code per param.
	formats := make([]uint16, fmtCount)
	for i := 0; i < fmtCount; i++ {
		if idx+2 > len(payload) {
			return nil
		}
		formats[i] = binary.BigEndian.Uint16(payload[idx:])
		idx += 2
	}
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
		raw := payload[idx : idx+int(length)]
		idx += int(length)

		binaryFmt := false
		switch {
		case fmtCount == 1:
			binaryFmt = formats[0] == 1
		case i < fmtCount:
			binaryFmt = formats[i] == 1
		}
		params = append(params, decodeBindValue(raw, binaryFmt))
	}
	return params
}

// decodeBindValue renders a bound parameter as text for inlining. A text-format
// value is used verbatim; a binary integer (1/2/4/8 bytes, big-endian signed) is
// rendered in decimal so numeric predicates match. Any other binary payload is
// passed through as raw bytes (best effort — the engine stores values as text).
func decodeBindValue(raw []byte, binaryFmt bool) string {
	if !binaryFmt {
		return string(raw)
	}
	switch len(raw) {
	case 1:
		return strconv.FormatInt(int64(int8(raw[0])), 10)
	case 2:
		return strconv.FormatInt(int64(int16(binary.BigEndian.Uint16(raw))), 10)
	case 4:
		return strconv.FormatInt(int64(int32(binary.BigEndian.Uint32(raw))), 10)
	case 8:
		return strconv.FormatInt(int64(binary.BigEndian.Uint64(raw)), 10)
	default:
		return string(raw)
	}
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

// countParams returns the highest placeholder number ($1, $2, ...) in a query,
// i.e. how many parameters a prepared statement declares. The result is clamped
// to the wire protocol's ceiling (a ParameterDescription count is a uint16), so
// a pathological placeholder such as "$900000000" cannot drive a huge downstream
// allocation. Digit runs are also capped so Atoi never scans an unbounded span.
func countParams(query string) int {
	max := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '$' && i+1 < len(query) && query[i+1] >= '1' && query[i+1] <= '9' {
			j := i + 1
			for j < len(query) && query[j] >= '0' && query[j] <= '9' && j-i <= 6 {
				j++
			}
			if n, err := strconv.Atoi(query[i+1 : j]); err == nil && n > max {
				max = n
			}
			i = j - 1
		}
	}
	if max > maxWireParams {
		max = maxWireParams
	}
	return max
}

func quoteParam(val string) string {
	if isStrictNumeric(val) {
		return val
	}
	return "'" + strings.ReplaceAll(val, "'", "''") + "'"
}

// isStrictNumeric reports whether s is an unsigned decimal literal
// (`[0-9]+(\.[0-9]+)?`), so inlining it bare cannot inject syntax. Signs,
// exponents, hex, NaN and Inf (all accepted by strconv) are deliberately
// rejected and get quoted instead.
func isStrictNumeric(s string) bool {
	if s == "" {
		return false
	}
	seenDot := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == '.' && !seenDot:
			seenDot = true
		default:
			return false
		}
	}
	return s != "."
}

// isReadOnlyQuery reports whether a query only reads (so running it twice — once
// to resolve the Describe field structure, once at Execute — cannot change data).
// Used to keep Describe from executing a state-changing statement (INSERT/UPDATE/
// DELETE/DDL/CALL): those are skipped at Describe and run exactly once at Execute.
// The engine has no data-modifying CTEs, so WITH is read-only here.
func isReadOnlyQuery(query string) bool {
	q := strings.TrimLeft(query, " \t\r\n(")
	end := 0
	for end < len(q) {
		c := q[end]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			end++
			continue
		}
		break
	}
	switch strings.ToUpper(q[:end]) {
	case "SELECT", "WITH", "VALUES", "TABLE", "SHOW", "EXPLAIN", "DESCRIBE", "DESC":
		return true
	}
	return false
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
