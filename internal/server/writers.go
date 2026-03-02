package server

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"

	"dbf/internal/catalog"
)

func writeRowDescriptionSelectOne(rw *bufio.ReadWriter) error {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, int16(1))
	buf.WriteString("?column?")
	buf.WriteByte(0)
	_ = binary.Write(&buf, binary.BigEndian, int32(0))
	_ = binary.Write(&buf, binary.BigEndian, int16(0))
	_ = binary.Write(&buf, binary.BigEndian, int32(23))
	_ = binary.Write(&buf, binary.BigEndian, int16(4))
	_ = binary.Write(&buf, binary.BigEndian, int32(-1))
	_ = binary.Write(&buf, binary.BigEndian, int16(0))
	return writeMessage(rw, 'T', buf.Bytes())
}

func writeSystemResult(rw *bufio.ReadWriter, result *catalog.SystemResult) {
	if len(result.Columns) > 0 {
		var sample []interface{}
		if len(result.Rows) > 0 {
			sample = result.Rows[0]
		}
		writeRowDescriptionForResult(rw, result.Columns, sample)
		for _, row := range result.Rows {
			writeDataRow(rw, row)
		}
	} else {
		writeEmptyRowDescription(rw)
	}
	writeCommandComplete(rw, result.Tag)
	writeReady(rw)
	rw.Flush()
}

func writeDataRowSelectOne(rw *bufio.ReadWriter) error {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, int16(1))
	_ = binary.Write(&buf, binary.BigEndian, int32(1))
	buf.WriteString("1")
	return writeMessage(rw, 'D', buf.Bytes())
}

func writeEmptyRowDescription(rw *bufio.ReadWriter) error {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, int16(0))
	return writeMessage(rw, 'T', buf.Bytes())
}

func writeRowDescriptionForResult(rw *bufio.ReadWriter, columns []string, sampleRow []interface{}) error {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, int16(len(columns)))
	for i, col := range columns {
		var oid int32 = 25
		if sampleRow != nil && i < len(sampleRow) {
			oid = oidForValue(sampleRow[i])
		}
		buf.WriteString(col)
		buf.WriteByte(0)
		_ = binary.Write(&buf, binary.BigEndian, int32(0))
		_ = binary.Write(&buf, binary.BigEndian, int16(0))
		_ = binary.Write(&buf, binary.BigEndian, oid)
		_ = binary.Write(&buf, binary.BigEndian, int16(4))
		_ = binary.Write(&buf, binary.BigEndian, int32(-1))
		_ = binary.Write(&buf, binary.BigEndian, int16(0))
	}
	return writeMessage(rw, 'T', buf.Bytes())
}

func writeDataRow(rw *bufio.ReadWriter, values []interface{}) error {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, int16(len(values)))
	for _, val := range values {
		if val == nil {
			_ = binary.Write(&buf, binary.BigEndian, int32(-1)) // NULL
			continue
		}
		str := fmt.Sprintf("%v", val)
		_ = binary.Write(&buf, binary.BigEndian, int32(len(str)))
		buf.WriteString(str)
	}
	return writeMessage(rw, 'D', buf.Bytes())
}

func oidForValue(val interface{}) int32 {
	switch val.(type) {
	case int, int32, int64:
		return 23 // int4
	case bool:
		return 16 // bool
	case float32, float64:
		return 701 // float8
	default:
		return 25 // text
	}
}
