package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"net"
)

func main() {
	conn, err := net.Dial("tcp", "localhost:4444")
	if err != nil {
		log.Fatalf("connect error: %v", err)
	}
	defer conn.Close()

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	// Startup
	log.Println("=== Sending Startup ===")
	startup := buildStartup("postgres", "postgres")
	if _, err := rw.Write(startup); err != nil {
		log.Fatalf("startup write error: %v", err)
	}
	if err := rw.Flush(); err != nil {
		log.Fatalf("startup flush error: %v", err)
	}

	// Read AuthRequest
	log.Println("=== Reading AuthRequest ===")
	msg, err := readMessage(rw)
	if err != nil {
		log.Fatalf("read error: %v", err)
	}
	log.Printf("AuthRequest type=%c payload=%v", msg.Type, msg.Payload)

	// Send Password
	log.Println("=== Sending Password ===")
	pwd := buildPasswordMessage("4444")
	if _, err := rw.Write(pwd); err != nil {
		log.Fatalf("password write error: %v", err)
	}
	if err := rw.Flush(); err != nil {
		log.Fatalf("password flush error: %v", err)
	}

	// Read AuthOK + params
	log.Println("=== Reading AuthOK & Params ===")
	for {
		msg, err := readMessage(rw)
		if err != nil {
			log.Fatalf("read error: %v", err)
		}
		log.Printf("Message: type=%c payload_len=%d", msg.Type, len(msg.Payload))
		if msg.Type == 'Z' {
			log.Printf("Ready for query!")
			break
		}
	}

	// Send SIMPLE QUERY (Q)
	log.Println("\n=== Sending Simple Query (Q) ===")
	query := buildSimpleQuery(`SELECT CASE
	WHEN (SELECT count(extname) FROM pg_catalog.pg_extension WHERE extname='bdr') > 0
	THEN 'pgd'
	WHEN (SELECT COUNT(*) FROM pg_replication_slots) > 0
	THEN 'log'
	ELSE NULL
END as type;`)
	if _, err := rw.Write(query); err != nil {
		log.Fatalf("query write error: %v", err)
	}
	if err := rw.Flush(); err != nil {
		log.Fatalf("query flush error: %v", err)
	}
	log.Printf("Query sent, bytes: %d", len(query))

	// Read response
	log.Println("=== Reading Query Response ===")
	for i := 0; i < 10; i++ {
		msg, err := readMessage(rw)
		if err != nil {
			log.Fatalf("read error: %v", err)
		}
		log.Printf("Response %d: type=%c payload_len=%d", i, msg.Type, len(msg.Payload))
		if msg.Type == 'D' {
			log.Printf("  DataRow bytes: %v", msg.Payload)
		}
		if msg.Type == 'C' {
			log.Printf("  CommandComplete: %s", string(msg.Payload[:len(msg.Payload)-1]))
		}
		if msg.Type == 'Z' {
			log.Printf("  ReadyForQuery: %c", msg.Payload[0])
			break
		}
	}

	log.Println("Done!")
}

type Message struct {
	Type    byte
	Payload []byte
}

func readMessage(rw *bufio.ReadWriter) (*Message, error) {
	msgType, err := rw.ReadByte()
	if err != nil {
		return nil, err
	}
	var lengthBuf [4]byte
	if _, err := rw.Read(lengthBuf[:]); err != nil {
		return nil, err
	}
	length := int32(binary.BigEndian.Uint32(lengthBuf[:]))
	if length < 4 {
		return nil, fmt.Errorf("invalid length: %d", length)
	}
	payload := make([]byte, length-4)
	if _, err := rw.Read(payload); err != nil {
		return nil, err
	}
	return &Message{Type: msgType, Payload: payload}, nil
}

func buildStartup(user, database string) []byte {
	var buf []byte

	var params []byte
	params = append(params, []byte("user")...)
	params = append(params, 0)
	params = append(params, []byte(user)...)
	params = append(params, 0)
	params = append(params, []byte("database")...)
	params = append(params, 0)
	params = append(params, []byte(database)...)
	params = append(params, 0)
	params = append(params, 0)

	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(params)+8))
	buf = append(buf, lenBuf[:]...)
	binary.BigEndian.PutUint16(lenBuf[:2], 3)
	buf = append(buf, lenBuf[:4]...)
	buf = append(buf, params...)

	return buf
}

func buildPasswordMessage(password string) []byte {
	payload := []byte(password)
	payload = append(payload, 0)
	return buildMsg('p', payload)
}

func buildSimpleQuery(query string) []byte {
	payload := []byte(query)
	payload = append(payload, 0)
	return buildMsg('Q', payload)
}

func buildMsg(msgType byte, payload []byte) []byte {
	var buf []byte
	buf = append(buf, msgType)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)+4))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, payload...)
	return buf
}
