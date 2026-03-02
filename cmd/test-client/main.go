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
	startup := buildStartup("postgres", "4444", "postgres")
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

	// Send extended query: Parse
	log.Println("\n=== Sending Parse ===")
	parse := buildParseMessage("", "SELECT 1")
	if _, err := rw.Write(parse); err != nil {
		log.Fatalf("parse write error: %v", err)
	}
	if err := rw.Flush(); err != nil {
		log.Fatalf("parse flush error: %v", err)
	}

	// Read ParseComplete
	msg, err = readMessage(rw)
	if err != nil {
		log.Fatalf("read error: %v", err)
	}
	log.Printf("ParseComplete: type=%c", msg.Type)

	// Send Bind
	log.Println("=== Sending Bind ===")
	bind := buildBindMessage("", "")
	if _, err := rw.Write(bind); err != nil {
		log.Fatalf("bind write error: %v", err)
	}
	if err := rw.Flush(); err != nil {
		log.Fatalf("bind flush error: %v", err)
	}

	// Read BindComplete
	msg, err = readMessage(rw)
	if err != nil {
		log.Fatalf("read error: %v", err)
	}
	log.Printf("BindComplete: type=%c", msg.Type)

	// Send Describe
	log.Println("=== Sending Describe ===")
	describe := buildDescribeMessage('P', "")
	if _, err := rw.Write(describe); err != nil {
		log.Fatalf("describe write error: %v", err)
	}
	if err := rw.Flush(); err != nil {
		log.Fatalf("describe flush error: %v", err)
	}

	// Read RowDescription or NoData
	msg, err = readMessage(rw)
	if err != nil {
		log.Fatalf("read error: %v", err)
	}
	log.Printf("Describe response: type=%c payload_len=%d", msg.Type, len(msg.Payload))

	// Send Execute
	log.Println("=== Sending Execute ===")
	execute := buildExecuteMessage("", 0)
	if _, err := rw.Write(execute); err != nil {
		log.Fatalf("execute write error: %v", err)
	}
	if err := rw.Flush(); err != nil {
		log.Fatalf("execute flush error: %v", err)
	}

	// Read results
	log.Println("=== Reading Execute Results ===")
	for i := 0; i < 10; i++ {
		msg, err := readMessage(rw)
		if err != nil {
			log.Fatalf("read error: %v", err)
		}
		log.Printf("Response %d: type=%c payload_len=%d", i, msg.Type, len(msg.Payload))
		if msg.Type == 'C' || msg.Type == 'I' {
			log.Printf("  Command: %s", string(msg.Payload))
		}
		if msg.Type == 'Z' {
			break
		}
	}

	// Send Sync
	log.Println("=== Sending Sync ===")
	sync := buildSyncMessage()
	if _, err := rw.Write(sync); err != nil {
		log.Fatalf("sync write error: %v", err)
	}
	if err := rw.Flush(); err != nil {
		log.Fatalf("sync flush error: %v", err)
	}

	// Read ready
	msg, err = readMessage(rw)
	if err != nil {
		log.Fatalf("read error: %v", err)
	}
	log.Printf("Final: type=%c", msg.Type)

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

func buildStartup(user, password, database string) []byte {
	var buf []byte

	// Build params
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

	// Protocol version 3.0
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

func buildParseMessage(stmtName, query string) []byte {
	var params []byte
	params = append(params, []byte(stmtName)...)
	params = append(params, 0)
	params = append(params, []byte(query)...)
	params = append(params, 0)
	var zero [2]byte
	params = append(params, zero[:]...)
	return buildMsg('P', params)
}

func buildBindMessage(portalName, stmtName string) []byte {
	var params []byte
	params = append(params, []byte(portalName)...)
	params = append(params, 0)
	params = append(params, []byte(stmtName)...)
	params = append(params, 0)
	var zero [2]byte
	params = append(params, zero[:]...)
	var numFormats [2]byte
	binary.BigEndian.PutUint16(numFormats[:], 0)
	params = append(params, numFormats[:]...)
	var numValues [2]byte
	binary.BigEndian.PutUint16(numValues[:], 0)
	params = append(params, numValues[:]...)
	var resultFormats [2]byte
	binary.BigEndian.PutUint16(resultFormats[:], 0)
	params = append(params, resultFormats[:]...)
	return buildMsg('B', params)
}

func buildDescribeMessage(kind byte, name string) []byte {
	var params []byte
	params = append(params, kind)
	params = append(params, []byte(name)...)
	params = append(params, 0)
	return buildMsg('D', params)
}

func buildExecuteMessage(portalName string, maxRows int32) []byte {
	var params []byte
	params = append(params, []byte(portalName)...)
	params = append(params, 0)
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(maxRows))
	params = append(params, buf[:]...)
	return buildMsg('E', params)
}

func buildSyncMessage() []byte {
	return buildMsg('S', nil)
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
