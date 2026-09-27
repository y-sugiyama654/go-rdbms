// sql/executor/value.go
package executor

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/storage/heap"
)

// Value は1つのカラムの値
type Value struct {
	Type catalog.DataType
	Int  int64  // TypeInt のときに使う
	Str  string // TypeVarchar のときに使う
}

func NewInt(v int64) Value     { return Value{Type: catalog.TypeInt, Int: v} }
func NewString(s string) Value { return Value{Type: catalog.TypeVarchar, Str: s} }

func (v Value) String() string {
	if v.Type == catalog.TypeInt {
		return strconv.FormatInt(v.Int, 10)
	}
	return v.Str
}

// Compare はaとbを比較し、a<bなら負、a==bなら0、a>bなら正を返す
// 型が異なる値どうしは比較できない
func Compare(a, b Value) (int, error) {
	if a.Type != b.Type {
		return 0, fmt.Errorf("executor: %s と %s は比較できません", a.Type, b.Type)
	}
	if a.Type == catalog.TypeInt {
		switch {
		case a.Int < b.Int:
			return -1, nil
		case a.Int > b.Int:
			return 1, nil
		}
		return 0, nil
	}
	switch {
	case a.Str < b.Str:
		return -1, nil
	case a.Str > b.Str:
		return 1, nil
	}
	return 0, nil
}

// EncodeTuple はスキーマに従って値の並びをレコードのバイト列にする
// INT は4バイトのビッグエンディアン（符号付き）、VARCHAR はUTF-8のバイト列
func EncodeTuple(values []Value, schema *catalog.Schema) ([]byte, error) {
	if len(values) != len(schema.Columns) {
		return nil, fmt.Errorf("executor: 値の数(%d)がカラム数(%d)と一致しません", len(values), len(schema.Columns))
	}
	fields := make([][]byte, len(values))
	for i, col := range schema.Columns {
		v := values[i]
		if v.Type != col.Type {
			return nil, fmt.Errorf("executor: カラム %s は %s 型です", col.Name, col.Type)
		}
		switch col.Type {
		case catalog.TypeInt:
			if v.Int < math.MinInt32 || v.Int > math.MaxInt32 {
				return nil, fmt.Errorf("executor: カラム %s の値 %d はINTの範囲外です", col.Name, v.Int)
			}
			fields[i] = binary.BigEndian.AppendUint32(nil, uint32(int32(v.Int)))
		case catalog.TypeVarchar:
			if n := utf8.RuneCountInString(v.Str); n > col.Length {
				return nil, fmt.Errorf("executor: カラム %s は最大%d文字です（%d文字）", col.Name, col.Length, n)
			}
			fields[i] = []byte(v.Str)
		}
	}
	return heap.EncodeRecord(fields)
}

// DecodeTuple はレコードのバイト列をスキーマに従って値の並びに戻す
func DecodeTuple(data []byte, schema *catalog.Schema) ([]Value, error) {
	fields, err := heap.DecodeRecord(data)
	if err != nil {
		return nil, err
	}
	if len(fields) != len(schema.Columns) {
		return nil, fmt.Errorf("executor: レコードのフィールド数(%d)がカラム数(%d)と一致しません", len(fields), len(schema.Columns))
	}
	values := make([]Value, len(fields))
	for i, col := range schema.Columns {
		switch col.Type {
		case catalog.TypeInt:
			if len(fields[i]) != 4 {
				return nil, fmt.Errorf("executor: カラム %s のデータ長が不正です", col.Name)
			}
			// int32 を経由することで負の値も正しく復元できる
			values[i] = NewInt(int64(int32(binary.BigEndian.Uint32(fields[i]))))
		case catalog.TypeVarchar:
			values[i] = NewString(string(fields[i]))
		}
	}
	return values, nil
}
