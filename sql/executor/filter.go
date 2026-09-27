// sql/executor/filter.go
package executor

import "github.com/yourname/go-rdbms/sql/parser"

// Filter は条件を満たす行だけを上に流すオペレータ
type Filter struct {
	child     Operator
	predicate parser.Expression
}

func NewFilter(child Operator, predicate parser.Expression) *Filter {
	return &Filter{child: child, predicate: predicate}
}

func (f *Filter) Open() error             { return f.child.Open() }
func (f *Filter) Close() error            { return f.child.Close() }
func (f *Filter) Columns() []OutputColumn { return f.child.Columns() }

func (f *Filter) Next() (*Tuple, error) {
	for {
		tuple, err := f.child.Next()
		if err != nil || tuple == nil {
			return nil, err
		}
		ok, err := evalPredicate(f.predicate, tuple, f.child.Columns())
		if err != nil {
			return nil, err
		}
		if ok {
			return tuple, nil
		}
	}
}
