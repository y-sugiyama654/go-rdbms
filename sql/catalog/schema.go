// sql/catalog/schema.go
package catalog

import "fmt"

// DataType はカラムのデータ型
type DataType int

const (
	TypeInt     DataType = iota // 32ビット符号付き整数
	TypeVarchar                 // 可変長文字列
)

func (t DataType) String() string {
	if t == TypeInt {
		return "INT"
	}
	return "VARCHAR"
}

// MarshalText はJSONに "INT" / "VARCHAR" という文字列で保存するためのメソッド
func (t DataType) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

// UnmarshalText はJSONの "INT" / "VARCHAR" をDataTypeに戻す
func (t *DataType) UnmarshalText(b []byte) error {
	switch string(b) {
	case "INT":
		*t = TypeInt
	case "VARCHAR":
		*t = TypeVarchar
	default:
		return fmt.Errorf("catalog: 未知の型です: %s", b)
	}
	return nil
}

// Column はカラムの定義
type Column struct {
	Name       string   `json:"name"`
	Type       DataType `json:"type"`
	Length     int      `json:"length,omitempty"` // VARCHARの最大文字数
	PrimaryKey bool     `json:"primary_key,omitempty"`
}

// Schema はテーブルのカラム定義の並び
type Schema struct {
	Columns []Column `json:"columns"`
}

// PrimaryKeyIndex は主キーのカラム位置を返す（主キーがなければ-1）
func (s *Schema) PrimaryKeyIndex() int {
	for i, c := range s.Columns {
		if c.PrimaryKey {
			return i
		}
	}
	return -1
}
