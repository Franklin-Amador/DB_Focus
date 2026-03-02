package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
)

func main() {
	testName := "persistence"
	if len(os.Args) > 1 {
		testName = os.Args[1]
	}

	conn, err := net.Dial("tcp", "localhost:4444")
	if err != nil {
		log.Fatalf("connect error: %v", err)
	}
	defer conn.Close()

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	// Authenticate
	if err := authenticate(rw); err != nil {
		log.Fatalf("auth error: %v", err)
	}

	// Test case
	switch testName {
	case "create":
		testCreateTable(rw)
	case "insert":
		testInsertData(rw)
	case "check":
		testCheckData(rw)
	default:
		testCreateTable(rw)
		testInsertData(rw)
		testCheckData(rw)
	}
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

func testCreateTable(rw *bufio.ReadWriter) {
	log.Println("[CREATE TABLE] Sending: CREATE TABLE test_users (id INT PRIMARY KEY, name TEXT, email TEXT);")
	query := buildSimpleQuery("CREATE TABLE test_users (id INT PRIMARY KEY, name TEXT, email TEXT);")
	if _, err := rw.Write(query); err != nil {
		log.Fatalf("write error: %v", err)
	}
	if err := rw.Flush(); err != nil {
		log.Fatalf("flush error: %v", err)
	}

	// Read response
	for {
		msg, err := readMessage(rw)
		if err != nil {
			log.Fatalf("read error: %v", err)
		}
		if msg.Type == 'C' {
			log.Printf("[CREATE TABLE] Response: %s", string(msg.Payload[:len(msg.Payload)-1]))
		}
		if msg.Type == 'Z' {
			break
		}
	}
	log.Println("[CREATE TABLE] Table created successfully")
}

func testInsertData(rw *bufio.ReadWriter) {
	queries := []string{
		"INSERT INTO test_users VALUES (1, 'Alice', 'alice@example.com');",
		"INSERT INTO test_users VALUES (2, 'Bob', 'bob@example.com');",
		"INSERT INTO test_users VALUES (3, 'Charlie', 'charlie@example.com');",
	}

	for _, q := range queries {
		log.Printf("[INSERT] Sending: %s\n", q)
		query := buildSimpleQuery(q)
		if _, err := rw.Write(query); err != nil {
			log.Fatalf("write error: %v", err)
		}
		if err := rw.Flush(); err != nil {
			log.Fatalf("flush error: %v", err)
		}

		// Read response
		for {
			msg, err := readMessage(rw)
			if err != nil {
				log.Fatalf("read error: %v", err)
			}
			if msg.Type == 'C' {
				log.Printf("[INSERT] Response: %s", string(msg.Payload[:len(msg.Payload)-1]))
			}
			if msg.Type == 'Z' {
				break
			}
		}
	}
	log.Println("[INSERT] All rows inserted successfully")
}

func testCheckData(rw *bufio.ReadWriter) {
	log.Println("[SELECT] Sending: SELECT * FROM test_users;")
	query := buildSimpleQuery("SELECT * FROM test_users;")
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
		if msg.Type == 'D' {
			rowCount++
			log.Printf("[SELECT] DataRow %d: %v", rowCount, msg.Payload)
		}
		if msg.Type == 'C' {
			log.Printf("[SELECT] Response: %s", string(msg.Payload[:len(msg.Payload)-1]))
		}
		if msg.Type == 'Z' {
			break
		}
	}
	log.Printf("[SELECT] Found %d rows\n", rowCount)
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
	payload := password + "\x00"
	msg := make([]byte, 1+4+len(payload))
	msg[0] = 'p'
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(payload)))
	copy(msg[5:], payload)
	return msg
}

func buildSimpleQuery(query string) []byte {
	payload := query + "\x00"
	msg := make([]byte, 1+4+len(payload))
	msg[0] = 'Q'
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(payload)))
	copy(msg[5:], payload)
	return msg
}
