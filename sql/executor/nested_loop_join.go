// sql/executor/nested_loop_join.go
package executor

import "github.com/yourname/go-rdbms/sql/parser"

// NestedLoopJoin は外側の1行ごとに内側を最初から読み直し、条件を満たす組を返すオペレータ
type NestedLoopJoin struct {
	outer, inner Operator
	condition    parser.Expression
	columns      []OutputColumn
	outerTuple   *Tuple // 現在処理中の外側の行
}

func NewNestedLoopJoin(outer, inner Operator, condition parser.Expression) *NestedLoopJoin {
	cols := append(append([]OutputColumn{}, outer.Columns()...), inner.Columns()...)
	return &NestedLoopJoin{outer: outer, inner: inner, condition: condition, columns: cols}
}

func (j *NestedLoopJoin) Columns() []OutputColumn { return j.columns }

func (j *NestedLoopJoin) Open() error {
	j.outerTuple = nil
	if err := j.outer.Open(); err != nil {
		return err
	}
	return j.inner.Open()
}

func (j *NestedLoopJoin) Next() (*Tuple, error) {
	for {
		// 外側の行がなければ次の行を読む。外側が尽きたら結合も終わり
		if j.outerTuple == nil {
			t, err := j.outer.Next()
			if err != nil || t == nil {
				return nil, err
			}
			j.outerTuple = t
		}

		innerTuple, err := j.inner.Next()
		if err != nil {
			return nil, err
		}
		if innerTuple == nil {
			// 内側を読み終えたら、内側を先頭に戻して外側を1行進める
			if err := j.inner.Close(); err != nil {
				return nil, err
			}
			if err := j.inner.Open(); err != nil {
				return nil, err
			}
			j.outerTuple = nil
			continue
		}

		combined := &Tuple{Values: append(append([]Value{}, j.outerTuple.Values...), innerTuple.Values...)}
		ok, err := evalPredicate(j.condition, combined, j.columns)
		if err != nil {
			return nil, err
		}
		if ok {
			return combined, nil
		}
	}
}

func (j *NestedLoopJoin) Close() error {
	if err := j.outer.Close(); err != nil {
		return err
	}
	return j.inner.Close()
}
