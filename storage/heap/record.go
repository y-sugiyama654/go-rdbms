// storage/heap/record.go
package heap

import (
	"encoding/binary"
	"fmt"
	"math"
)

// EncodeRecord は各フィールドのバイト列を「長さ(2バイト) + データ」の形式で連結する
func EncodeRecord(fields [][]byte) ([]byte, error) {
	size := 0
	for _, f := range fields {
		if len(f) > math.MaxUint16 {
			return nil, fmt.Errorf("record: フィールドが長すぎます（%dバイト）", len(f))
		}
		size += 2 + len(f)
	}

	buf := make([]byte, 0, size)
	for _, f := range fields {
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(f)))
		buf = append(buf, f...)
	}
	return buf, nil
}

// DecodeRecord はEncodeRecordで作ったバイト列をフィールドごとに分解する
func DecodeRecord(data []byte) ([][]byte, error) {
	var fields [][]byte
	offset := 0
	for offset < len(data) {
		if offset+2 > len(data) {
			return nil, fmt.Errorf("record: 長さフィールドが途中で切れています")
		}
		length := int(binary.BigEndian.Uint16(data[offset:]))
		offset += 2
		if offset+length > len(data) {
			return nil, fmt.Errorf("record: フィールドのデータが途中で切れています")
		}
		fields = append(fields, data[offset:offset+length])
		offset += length
	}
	return fields, nil
}
