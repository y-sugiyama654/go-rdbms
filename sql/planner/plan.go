// sql/planner/plan.go
package planner

import (
	"fmt"
	"math"
	"strings"

	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/sql/parser"
)

// Plan は実行計画の木のノード
// 「何をどの順に行うか」だけを表すデータで、実行はExecutorが担当する
type Plan interface {
	planNode()
}

// SeqScanPlan はテーブルの全件走査
type SeqScanPlan struct {
	Table *catalog.TableInfo
}

// IndexScanPlan は主キーのインデックスを使った範囲 [Min, Max] の走査
type IndexScanPlan struct {
	Table    *catalog.TableInfo
	Min, Max int64
}

// FilterPlan は条件による絞り込み
type FilterPlan struct {
	Child     Plan
	Predicate parser.Expression
}

// ProjectionPlan は出力カラムの選択
type ProjectionPlan struct {
	Child   Plan
	Columns []*parser.ColumnRef
}

// NestedLoopJoinPlan は2つの入力の結合
type NestedLoopJoinPlan struct {
	Outer, Inner Plan
	Condition    parser.Expression
}

func (*SeqScanPlan) planNode()        {}
func (*IndexScanPlan) planNode()      {}
func (*FilterPlan) planNode()         {}
func (*ProjectionPlan) planNode()     {}
func (*NestedLoopJoinPlan) planNode() {}

// Explain は実行計画の木を、字下げしたテキストにする
func Explain(p Plan) string {
	var sb strings.Builder
	explain(&sb, p, 0)
	return sb.String()
}

func explain(sb *strings.Builder, p Plan, depth int) {
	sb.WriteString(strings.Repeat("  ", depth))
	switch n := p.(type) {
	case *SeqScanPlan:
		fmt.Fprintf(sb, "SeqScan %s\n", n.Table.Name)
	case *IndexScanPlan:
		fmt.Fprintf(sb, "IndexScan %s [%s, %s]\n", n.Table.Name, bound(n.Min), bound(n.Max))
	case *FilterPlan:
		fmt.Fprintf(sb, "Filter %s\n", n.Predicate)
		explain(sb, n.Child, depth+1)
	case *ProjectionPlan:
		names := make([]string, len(n.Columns))
		for i, c := range n.Columns {
			names[i] = c.String()
		}
		fmt.Fprintf(sb, "Projection %s\n", strings.Join(names, ", "))
		explain(sb, n.Child, depth+1)
	case *NestedLoopJoinPlan:
		fmt.Fprintf(sb, "NestedLoopJoin %s\n", n.Condition)
		explain(sb, n.Outer, depth+1)
		explain(sb, n.Inner, depth+1)
	}
}

func bound(v int64) string {
	switch v {
	case math.MinInt64:
		return "-inf"
	case math.MaxInt64:
		return "+inf"
	}
	return fmt.Sprint(v)
}
