// sql/catalog/schema.go
package catalog

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

// Column はカラムの定義
type Column struct {
	Name       string
	Type       DataType
	Length     int // VARCHARの最大文字数
	PrimaryKey bool
}

// Schema はテーブルのカラム定義の並び
type Schema struct {
	Columns []Column
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
