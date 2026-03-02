package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"net"
)

func main() {
	conn, err := net.Dial("tcp", "localhost:4445")
	if err != nil {
		log.Fatalf("connect error: %v", err)
	}
	defer conn.Close()

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	// Authenticate
	if err := authenticate(rw); err != nil {
		log.Fatalf("auth error: %v", err)
	}

	// Query users table
	log.Println("[USERS] Sending: SELECT * FROM focus.users;")
	query := buildSimpleQuery("SELECT * FROM focus.users;")
	if _, err := rw.Write(query); err != nil {
		log.Fatalf("write error: %v", err)
	}
	if err := rw.Flush(); err != nil {
		log.Fatalf("flush error: %v", err)
	}

	// Read response
	rowCount := 0
	for {
		msg, err := readMessage(rw)
		if err != nil {
			log.Fatalf("read error: %v", err)
		}
		if msg.Type == 'T' {
			log.Printf("[USERS] RowDescription received")
		}
		if msg.Type == 'D' {
			rowCount++
			log.Printf("[USERS] DataRow %d received: %v", rowCount, msg.Payload)
		}
		if msg.Type == 'C' {
			log.Printf("[USERS] Response: %s", string(msg.Payload[:len(msg.Payload)-1]))
		}
		if msg.Type == 'Z' {
			break
		}
	}
	log.Printf("[USERS] ✓ Found %d users in catalog (postgres, admin pre-registered)", rowCount)
}

func authenticate(rw *bufio.ReadWriter) error {
	// Send startup
	startup := buildStartup("admin", "postgres")
	if _, err := rw.Write(startup); err != nil {
		return err
	}
	if err := rw.Flush(); err != nil {
		return err
	}

	// Read AuthRequest
	msg, err := readMessage(rw)
	if err != nil {
		return err
	}
	if msg.Type != 'R' {
		return fmt.Errorf("expected R, got %c", msg.Type)
	}

	// Send password
	pwd := buildPasswordMessage("4444")
	if _, err := rw.Write(pwd); err != nil {
		return err
	}
	if err := rw.Flush(); err != nil {
		return err
	}

	// Read responses until ready
	for {
		msg, err := readMessage(rw)
		if err != nil {
			return err
		}
		if msg.Type == 'Z' {
			break
		}
	}

	return nil
}

type Message struct {
	Type    byte
	Payload []byte
}

func readMessage(rw *bufio.ReadWriter) (*Message, error) {
	msgTypeBuf := make([]byte, 1)
	if _, err := rw.Read(msgTypeBuf); err != nil {
		return nil, err
	}
	msgType := msgTypeBuf[0]

	lenBuf := make([]byte, 4)
	if _, err := rw.Read(lenBuf); err != nil {
		return nil, err
	}
	msgLen := binary.BigEndian.Uint32(lenBuf) - 4

	payload := make([]byte, msgLen)
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
