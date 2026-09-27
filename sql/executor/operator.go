// sql/executor/operator.go
package executor

import (
	"fmt"

	"github.com/yourname/go-rdbms/sql/parser"
)

// Tuple はオペレータ間を流れる1行
type Tuple struct {
	Values []Value
}

// OutputColumn はオペレータが出力する列の情報
type OutputColumn struct {
	Table string
	Name  string
}

// Operator はイテレータモデル（Volcano Model）のオペレータ
type Operator interface {
	Open() error
	// Next は次の1行を返す。行がもうなければ nil を返す
	Next() (*Tuple, error)
	Close() error
	// Columns は出力する列の並びを返す
	Columns() []OutputColumn
}

// resolveColumn はカラム参照が列の並びの何番目かを求める
func resolveColumn(cols []OutputColumn, ref *parser.ColumnRef) (int, error) {
	found := -1
	for i, c := range cols {
		if c.Name != ref.Name || (ref.Table != "" && c.Table != ref.Table) {
			continue
		}
		if found >= 0 {
			return -1, fmt.Errorf("executor: カラム %s があいまいです（テーブル名で修飾してください）", ref)
		}
		found = i
	}
	if found < 0 {
		return -1, fmt.Errorf("executor: カラム %s が見つかりません", ref)
	}
	return found, nil
}
