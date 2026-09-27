// sql/planner/planner.go
package planner

import (
	"math"

	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/sql/parser"
)

// Planner はSELECT文のASTから実行計画を組み立てる
type Planner struct {
	catalog *catalog.Catalog
}

func New(cat *catalog.Catalog) *Planner {
	return &Planner{catalog: cat}
}

// PlanSelect はSELECT文の実行計画を作る
//
//	Projection        ← SELECT句（* のときは付けない）
//	  Filter          ← WHERE句（あるとき）
//	    NestedLoopJoin ← JOIN句（あるとき、左から順に積み上げる）
//	      Scan
func (p *Planner) PlanSelect(stmt *parser.SelectStatement) (Plan, error) {
	from, err := p.catalog.GetTable(stmt.From)
	if err != nil {
		return nil, err
	}

	var plan Plan
	if len(stmt.Joins) == 0 {
		plan = chooseScan(from, stmt.Where)
	} else {
		plan = &SeqScanPlan{Table: from}
		for _, join := range stmt.Joins {
			inner, err := p.catalog.GetTable(join.Table)
			if err != nil {
				return nil, err
			}
			plan = &NestedLoopJoinPlan{Outer: plan, Inner: &SeqScanPlan{Table: inner}, Condition: join.On}
		}
	}

	// インデックスで範囲を絞った場合も、WHERE句全体のFilterは必ず残す。
	// インデックスが扱うのは主キーの条件だけなので、残りの条件をここで確かめる
	if stmt.Where != nil {
		plan = &FilterPlan{Child: plan, Predicate: stmt.Where}
	}
	if stmt.Columns != nil {
		plan = &ProjectionPlan{Child: plan, Columns: stmt.Columns}
	}
	return plan, nil
}

// chooseScan はWHERE句に主キーの条件があればIndexScanを、なければSeqScanを選ぶ
func chooseScan(table *catalog.TableInfo, where parser.Expression) Plan {
	if lo, hi, ok := indexRange(table, where); ok {
		return &IndexScanPlan{Table: table, Min: lo, Max: hi}
	}
	return &SeqScanPlan{Table: table}
}

// indexRange はANDでつながった条件のうち「主キー 比較演算子 整数」の形のものを集め、
// 主キーが取りうる範囲 [lo, hi] を求める
func indexRange(table *catalog.TableInfo, where parser.Expression) (lo, hi int64, ok bool) {
	pk := table.Schema.PrimaryKeyIndex()
	if where == nil || pk < 0 || table.Index == nil {
		return 0, 0, false
	}
	pkName := table.Schema.Columns[pk].Name

	lo, hi = math.MinInt64, math.MaxInt64
	for _, cond := range splitAnd(where) {
		op, v, isPK := pkComparison(cond, table.Name, pkName)
		if !isPK {
			continue
		}
		switch op {
		case "=":
			lo, hi = max(lo, v), min(hi, v)
		case ">":
			lo = max(lo, v+1)
		case ">=":
			lo = max(lo, v)
		case "<":
			hi = min(hi, v-1)
		case "<=":
			hi = min(hi, v)
		default: // <> はインデックスで絞れない
			continue
		}
		ok = true
	}
	return lo, hi, ok
}

// splitAnd は A AND B AND C を [A, B, C] に分解する
func splitAnd(expr parser.Expression) []parser.Expression {
	if b, ok := expr.(*parser.BinaryExpr); ok && b.Op == "AND" {
		return append(splitAnd(b.Left), splitAnd(b.Right)...)
	}
	return []parser.Expression{expr}
}

// pkComparison は条件が「主キー op 整数」または「整数 op 主キー」の形なら、
// 主キーを左辺にそろえた演算子と整数を返す
func pkComparison(cond parser.Expression, tableName, pkName string) (string, int64, bool) {
	b, ok := cond.(*parser.BinaryExpr)
	if !ok {
		return "", 0, false
	}
	isPK := func(e parser.Expression) bool {
		col, ok := e.(*parser.ColumnRef)
		return ok && col.Name == pkName && (col.Table == "" || col.Table == tableName)
	}
	if lit, ok := b.Right.(*parser.IntLiteral); ok && isPK(b.Left) {
		return b.Op, lit.Value, true
	}
	if lit, ok := b.Left.(*parser.IntLiteral); ok && isPK(b.Right) {
		// 5 < id は id > 5 と同じ
		flipped := map[string]string{"=": "=", "<>": "<>", "<": ">", ">": "<", "<=": ">=", ">=": "<="}
		return flipped[b.Op], lit.Value, true
	}
	return "", 0, false
}
