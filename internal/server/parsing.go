package server

import (
	"errors"
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
