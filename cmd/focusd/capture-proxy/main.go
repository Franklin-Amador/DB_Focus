package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type CapturedExchange struct {
	Timestamp string     `json:"timestamp"`
	Query     string     `json:"query"`
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	Tag       string     `json:"tag"`
	Error     string     `json:"error,omitempty"`
}

var (
	captures []CapturedExchange
	mu       sync.Mutex
	outFile  *os.File
)

func main() {
	listenAddr := ":5433"
	pgAddr := "localhost:5432"
	if len(os.Args) > 1 {
		pgAddr = os.Args[1]
	}

	var err error
	outFile, err = os.Create("captured_queries.json")
	if err != nil {
		log.Fatalf("failed to create output file: %v", err)
	}
	defer flushCaptures()

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	log.Printf("proxy listening on %s → forwarding to %s", listenAddr, pgAddr)
	log.Printf("output → captured_queries.json")

	for {
		client, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleConnection(client, pgAddr)
	}
}

func handleConnection(client net.Conn, pgAddr string) {
	defer client.Close()

	pg, err := net.Dial("tcp", pgAddr)
	if err != nil {
		log.Printf("failed to connect to postgres: %v", err)
		return
	}
	defer pg.Close()

	cr := bufio.NewReader(client)
	pr := bufio.NewReader(pg)
	cw := bufio.NewWriter(client)
	pw := bufio.NewWriter(pg)

	if err := forwardStartup(cr, pw, pr, cw); err != nil {
		log.Printf("startup error: %v", err)
		return
	}

	var currentQuery string
	var mu sync.Mutex

	// pg → client: captura respuestas en goroutine separada
	go func() {
		var columns []string
		var rows [][]string

		for {
			msgType, payload, err := readMsg(pr)
			if err != nil {
				return
			}
			writeMsg(cw, msgType, payload)
			cw.Flush()

			mu.Lock()
			q := currentQuery
			mu.Unlock()

			switch msgType {
			case 'T':
				columns = parseRowDescription(payload)
				rows = nil
			case 'D':
				rows = append(rows, parseDataRow(payload))
			case 'C':
				tag := strings.TrimRight(string(payload), "\x00")
				if q != "" {
					saveCapture(CapturedExchange{
						Timestamp: time.Now().Format(time.RFC3339),
						Query:     q,
						Columns:   columns,
						Rows:      rows,
						Tag:       tag,
					})
				}
				columns = nil
				rows = nil
			case 'E':
				if q != "" {
					saveCapture(CapturedExchange{
						Timestamp: time.Now().Format(time.RFC3339),
						Query:     q,
						Error:     parseError(payload),
					})
				}
			}
		}
	}()

	// client → pg: captura queries y reenvía
	for {
		msgType, payload, err := readMsg(cr)
		if err != nil {
			return
		}

		switch msgType {
		case 'Q':
			mu.Lock()
			currentQuery = strings.TrimRight(string(payload), "\x00")
			mu.Unlock()
		case 'P':
			null := strings.IndexByte(string(payload), 0)
			if null >= 0 && null+1 < len(payload) {
				q := string(payload[null+1:])
				if idx := strings.IndexByte(q, 0); idx >= 0 {
					q = q[:idx]
				}
				mu.Lock()
				currentQuery = q
				mu.Unlock()
			}
		case 'X':
			writeMsg(pw, msgType, payload)
			pw.Flush()
			return
		}

		writeMsg(pw, msgType, payload)
		pw.Flush()
	}
}

// ── Wire protocol ────────────────────────────────────────────────────────────

func readMsg(r *bufio.Reader) (byte, []byte, error) {
	msgType, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return 0, nil, err
	}
	length := int(binary.BigEndian.Uint32(lenBuf[:])) - 4
	if length < 0 {
		return msgType, nil, nil
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return msgType, payload, nil
}

func writeMsg(w *bufio.Writer, msgType byte, payload []byte) {
	w.WriteByte(msgType)
	length := uint32(len(payload) + 4)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], length)
	w.Write(lenBuf[:])
	if len(payload) > 0 {
		w.Write(payload)
	}
}

func forwardStartup(cr *bufio.Reader, pw *bufio.Writer, pr *bufio.Reader, cw *bufio.Writer) error {
	for {
		var lenBuf [4]byte
		if _, err := io.ReadFull(cr, lenBuf[:]); err != nil {
			return err
		}
		totalLen := int(binary.BigEndian.Uint32(lenBuf[:]))
		if totalLen < 4 {
			return fmt.Errorf("invalid startup length: %d", totalLen)
		}

		payload := make([]byte, totalLen-4)
		if len(payload) > 0 {
			if _, err := io.ReadFull(cr, payload); err != nil {
				return err
			}
		}

		// Necesitamos al menos 4 bytes para el código de protocolo
		if len(payload) < 4 {
			return fmt.Errorf("startup payload too short")
		}

		code := binary.BigEndian.Uint32(payload[0:4])

		switch code {
		case 80877103: // SSLRequest
			log.Printf("[startup] SSL request → responding N to client")
			cw.WriteByte('N')
			cw.Flush()
			// Leer el siguiente mensaje (startup real)
			continue

		case 80877102: // CancelRequest
			log.Printf("[startup] cancel request → forwarding to postgres")
			pw.Write(lenBuf[:])
			pw.Write(payload)
			pw.Flush()
			return fmt.Errorf("cancel request")

		default: // Startup message real
			log.Printf("[startup] real startup, protocol code: %d", code)
			pw.Write(lenBuf[:])
			pw.Write(payload)
			pw.Flush()

			// Leer respuestas de postgres hasta ReadyForQuery
			// Leer respuestas de postgres hasta ReadyForQuery
			for {
				msgType, msgPayload, err := readMsg(pr)
				if err != nil {
					return fmt.Errorf("postgres startup response error: %v", err)
				}
				log.Printf("[startup] postgres → client: %c", msgType)
				writeMsg(cw, msgType, msgPayload)
				cw.Flush()

				if msgType == 'Z' {
					return nil
				}
				if msgType == 'E' {
					return fmt.Errorf("postgres auth error: %s", parseError(msgPayload))
				}

				// Si postgres pide password (R con código != 0), leer respuesta del cliente
				if msgType == 'R' && len(msgPayload) >= 4 {
					authCode := binary.BigEndian.Uint32(msgPayload[0:4])
					log.Printf("[startup] R authCode=%d", authCode)
					// 10=SASLInitial, 11=SASLContinue necesitan respuesta
					// 12=SASLFinal y 0=AuthOK NO necesitan respuesta del cliente
					if authCode == 10 || authCode == 11 {
						clientMsgType, clientPayload, err := readMsg(cr)
						if err != nil {
							return fmt.Errorf("client auth response error: %v", err)
						}
						log.Printf("[startup] client → postgres: %c", clientMsgType)
						writeMsg(pw, clientMsgType, clientPayload)
						pw.Flush()
					}
				}
			}
		}
	}
}

// ── Parsers ──────────────────────────────────────────────────────────────────

func parseRowDescription(payload []byte) []string {
	if len(payload) < 2 {
		return nil
	}
	count := int(binary.BigEndian.Uint16(payload[0:2]))
	cols := make([]string, 0, count)
	pos := 2
	for i := 0; i < count; i++ {
		if pos >= len(payload) {
			break
		}
		end := pos
		for end < len(payload) && payload[end] != 0 {
			end++
		}
		cols = append(cols, string(payload[pos:end]))
		pos = end + 1 + 18 // null terminator + 18 bytes of field metadata
	}
	return cols
}

func parseDataRow(payload []byte) []string {
	if len(payload) < 2 {
		return nil
	}
	count := int(binary.BigEndian.Uint16(payload[0:2]))
	row := make([]string, count)
	pos := 2
	for i := 0; i < count; i++ {
		if pos+4 > len(payload) {
			break
		}
		valLen := int(int32(binary.BigEndian.Uint32(payload[pos : pos+4])))
		pos += 4
		if valLen == -1 {
			row[i] = "NULL"
			continue
		}
		if pos+valLen > len(payload) {
			break
		}
		row[i] = string(payload[pos : pos+valLen])
		pos += valLen
	}
	return row
}

func parseError(payload []byte) string {
	for i := 0; i < len(payload)-1; i++ {
		if payload[i] == 'M' {
			end := i + 1
			for end < len(payload) && payload[end] != 0 {
				end++
			}
			return string(payload[i+1 : end])
		}
	}
	return "unknown error"
}

// ── Storage ──────────────────────────────────────────────────────────────────

func saveCapture(exchange CapturedExchange) {
	mu.Lock()
	defer mu.Unlock()

	// Deduplicate by query
	for _, existing := range captures {
		if existing.Query == exchange.Query {
			return
		}
	}

	captures = append(captures, exchange)
	log.Printf("[captured] %s → %d cols %d rows tag=%s",
		truncate(exchange.Query, 60),
		len(exchange.Columns),
		len(exchange.Rows),
		exchange.Tag,
	)
	flushCaptures()
}

func flushCaptures() {
	if outFile == nil {
		return
	}
	outFile.Seek(0, 0)
	outFile.Truncate(0)
	enc := json.NewEncoder(outFile)
	enc.SetIndent("", "  ")
	enc.Encode(captures)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
