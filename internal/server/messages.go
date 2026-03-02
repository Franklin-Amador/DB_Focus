package server

import (
	"encoding/binary"
	"errors"
	"io"
	"strings"

	"bufio"
)

func readMessage(rw *bufio.ReadWriter) (byte, []byte, error) {
	msgType, err := rw.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	length, err := readInt32(rw)
	if err != nil {
		return 0, nil, err
	}
	if length < 4 {
		return 0, nil, errors.New("invalid message length")
	}
	payload := make([]byte, length-4)
	if _, err := io.ReadFull(rw, payload); err != nil {
		return 0, nil, err
	}
	return msgType, payload, nil
}

func readInt32(rw *bufio.ReadWriter) (int32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(rw, buf[:]); err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(buf[:])), nil
}

func writeMessage(rw *bufio.ReadWriter, msgType byte, payload []byte) error {
	if err := rw.WriteByte(msgType); err != nil {
		return err
	}
	length := int32(len(payload) + 4)
	if err := binary.Write(rw, binary.BigEndian, length); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := rw.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func int32ToBytes(v int32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	return b[:]
}

func writeParameterStatus(rw *bufio.ReadWriter, key, value string) error {
	payload := append(append([]byte(key), 0), []byte(value)...)
	payload = append(payload, 0)
	return writeMessage(rw, 'S', payload)
}

func writeBackendKeyData(rw *bufio.ReadWriter, pid, key int32) error {
	payload := append(int32ToBytes(pid), int32ToBytes(key)...)
	return writeMessage(rw, 'K', payload)
}

func writeReady(rw *bufio.ReadWriter) error {
	return writeMessage(rw, 'Z', []byte{'I'})
}

func writeError(rw *bufio.ReadWriter, msg string) error {
	fields := []byte{'S'}
	fields = append(fields, []byte("ERROR")...)
	fields = append(fields, 0)
	fields = append(fields, 'M')
	fields = append(fields, []byte(msg)...)
	fields = append(fields, 0, 0)
	return writeMessage(rw, 'E', fields)
}

func writeCommandComplete(rw *bufio.ReadWriter, tag string) error {
	payload := append([]byte(tag), 0)
	return writeMessage(rw, 'C', payload)
}

func writeParseComplete(rw *bufio.ReadWriter) error {
	return writeMessage(rw, '1', nil)
}

func writeBindComplete(rw *bufio.ReadWriter) error {
	return writeMessage(rw, '2', nil)
}

func writeNoData(rw *bufio.ReadWriter) error {
	return writeMessage(rw, 'n', nil)
}

func writeAuthRequestCleartext(rw *bufio.ReadWriter) error {
	return writeMessage(rw, 'R', int32ToBytes(3))
}

func writeAuthOK(rw *bufio.ReadWriter) error {
	return writeMessage(rw, 'R', int32ToBytes(0))
}

func readPassword(rw *bufio.ReadWriter) (string, error) {
	msgType, payload, err := readMessage(rw)
	if err != nil {
		return "", err
	}
	if msgType != 'p' {
		return "", errors.New("expected password message")
	}
	password := strings.TrimRight(string(payload), "\x00")
	return password, nil
}

func readStartup(rw *bufio.ReadWriter) (map[string]string, error) {
	for {
		length, err := readInt32(rw)
		if err != nil {
			return nil, err
		}
		if length < 8 {
			return nil, errors.New("invalid startup length")
		}
		payload := make([]byte, length-4)
		if _, err := io.ReadFull(rw, payload); err != nil {
			return nil, err
		}
		protocol := int32(binary.BigEndian.Uint32(payload[0:4]))
		if protocol == 80877103 {
			if _, err := rw.Write([]byte("N")); err != nil {
				return nil, err
			}
			if err := rw.Flush(); err != nil {
				return nil, err
			}
			continue
		}
		params := parseStartupParams(payload[4:])
		return params, nil
	}
}
