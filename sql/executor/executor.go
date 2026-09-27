// sql/executor/executor.go
package executor

import (
	"fmt"

	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/sql/parser"
	"github.com/yourname/go-rdbms/sql/planner"
	"github.com/yourname/go-rdbms/transaction"
)

// Result はSQL文の実行結果
type Result struct {
	Columns      []string  // SELECTの列名
	Rows         [][]Value // SELECTの結果行
	RowsAffected int       // INSERTで追加した行数
	Message      string    // CREATE TABLEなどの結果メッセージ
}

// Executor はSQL文（AST）を実行する
type Executor struct {
	catalog *catalog.Catalog
	planner *planner.Planner
}

func New(cat *catalog.Catalog) *Executor {
	return &Executor{catalog: cat, planner: planner.New(cat)}
}

// Execute はトランザクションtxの中でSQL文を1つ実行する
func (e *Executor) Execute(tx *transaction.Transaction, stmt parser.Statement) (*Result, error) {
	switch s := stmt.(type) {
	case *parser.SelectStatement:
		return e.executeSelect(s)
	case *parser.InsertStatement:
		return e.executeInsert(tx, s)
	case *parser.CreateTableStatement:
		return e.executeCreateTable(s)
	default:
		return nil, fmt.Errorf("executor: 未対応の文です: %T", stmt)
	}
}

// ---- SELECT ----

func (e *Executor) executeSelect(stmt *parser.SelectStatement) (*Result, error) {
	plan, err := e.planner.PlanSelect(stmt)
	if err != nil {
		return nil, err
	}
	op, err := e.build(plan)
	if err != nil {
		return nil, err
	}
	if err := op.Open(); err != nil {
		return nil, err
	}
	defer op.Close()

	result := &Result{}
	for _, c := range op.Columns() {
		if len(stmt.Joins) > 0 {
			result.Columns = append(result.Columns, c.Table+"."+c.Name)
		} else {
			result.Columns = append(result.Columns, c.Name)
		}
	}
	for {
		tuple, err := op.Next()
		if err != nil {
			return nil, err
		}
		if tuple == nil {
			break
		}
		result.Rows = append(result.Rows, tuple.Values)
	}
	return result, nil
}

// build は実行計画の木から、同じ形のオペレータの木を組み立てる
func (e *Executor) build(plan planner.Plan) (Operator, error) {
	switch p := plan.(type) {
	case *planner.SeqScanPlan:
		return NewSeqScan(p.Table), nil
	case *planner.IndexScanPlan:
		return NewIndexScan(p.Table, p.Min, p.Max), nil
	case *planner.FilterPlan:
		child, err := e.build(p.Child)
		if err != nil {
			return nil, err
		}
		return NewFilter(child, p.Predicate), nil
	case *planner.ProjectionPlan:
		child, err := e.build(p.Child)
		if err != nil {
			return nil, err
		}
		return NewProjection(child, p.Columns)
	case *planner.NestedLoopJoinPlan:
		outer, err := e.build(p.Outer)
		if err != nil {
			return nil, err
		}
		inner, err := e.build(p.Inner)
		if err != nil {
			return nil, err
		}
		return NewNestedLoopJoin(outer, inner, p.Condition), nil
	default:
		return nil, fmt.Errorf("executor: 未対応の実行計画です: %T", plan)
	}
}

// ---- INSERT ----

func (e *Executor) executeInsert(tx *transaction.Transaction, stmt *parser.InsertStatement) (*Result, error) {
	table, err := e.catalog.GetTable(stmt.Table)
	if err != nil {
		return nil, err
	}
	values, err := insertValues(stmt, table.Schema)
	if err != nil {
		return nil, err
	}
	data, err := EncodeTuple(values, table.Schema)
	if err != nil {
		return nil, err
	}

	// 主キーの重複はヒープに書く前に確かめる
	pk := table.Schema.PrimaryKeyIndex()
	if pk >= 0 {
		if _, found, err := table.Index.Search(values[pk].Int); err != nil {
			return nil, err
		} else if found {
			return nil, fmt.Errorf("executor: 主キー %d は既に存在します", values[pk].Int)
		}
	}

	rid, err := table.Heap.InsertRecord(data)
	if err != nil {
		return nil, err
	}
	// ロールバックに備えて、インデックスを更新する前に書き込みを記録しておく
	w := transaction.WriteRecord{Table: table, RID: rid}
	if pk >= 0 {
		w.Key, w.HasKey = values[pk].Int, true
	}
	tx.AddWrite(w)

	if pk >= 0 {
		if err := table.Index.Insert(values[pk].Int, rid); err != nil {
			return nil, err
		}
	}
	return &Result{RowsAffected: 1}, nil
}

// insertValues はVALUESのリテラルを、テーブルのカラム順に並んだ値に変換する
// NULLは扱わないので、すべてのカラムに値を指定する必要がある
func insertValues(stmt *parser.InsertStatement, schema *catalog.Schema) ([]Value, error) {
	order := make([]int, len(stmt.Values)) // i番目の値を入れるカラム位置
	if stmt.Columns == nil {
		for i := range order {
			order[i] = i
		}
	} else {
		if len(stmt.Columns) != len(stmt.Values) {
			return nil, fmt.Errorf("executor: カラム数(%d)と値の数(%d)が一致しません", len(stmt.Columns), len(stmt.Values))
		}
		for i, name := range stmt.Columns {
			order[i] = -1
			for j, col := range schema.Columns {
				if col.Name == name {
					order[i] = j
				}
			}
			if order[i] < 0 {
				return nil, fmt.Errorf("executor: カラム %s は存在しません", name)
			}
		}
	}
	if len(stmt.Values) != len(schema.Columns) {
		return nil, fmt.Errorf("executor: すべてのカラム(%d個)に値を指定してください", len(schema.Columns))
	}

	values := make([]Value, len(schema.Columns))
	filled := make([]bool, len(schema.Columns))
	for i, expr := range stmt.Values {
		idx := order[i]
		if filled[idx] {
			return nil, fmt.Errorf("executor: カラム %s が重複しています", schema.Columns[idx].Name)
		}
		filled[idx] = true
		switch lit := expr.(type) {
		case *parser.IntLiteral:
			values[idx] = NewInt(lit.Value)
		case *parser.StringLiteral:
			values[idx] = NewString(lit.Value)
		default:
			return nil, fmt.Errorf("executor: VALUESにはリテラルを指定してください: %s", expr)
		}
	}
	return values, nil
}

// ---- CREATE TABLE ----

func (e *Executor) executeCreateTable(stmt *parser.CreateTableStatement) (*Result, error) {
	columns := make([]catalog.Column, len(stmt.Columns))
	for i, def := range stmt.Columns {
		col := catalog.Column{Name: def.Name, Length: def.Length, PrimaryKey: def.PrimaryKey}
		if def.Type == "INT" {
			col.Type = catalog.TypeInt
		} else {
			col.Type = catalog.TypeVarchar
		}
		columns[i] = col
	}
	if _, err := e.catalog.CreateTable(stmt.Table, columns); err != nil {
		return nil, err
	}
	return &Result{Message: fmt.Sprintf("テーブル %s を作成しました", stmt.Table)}, nil
}
