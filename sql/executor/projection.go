// sql/executor/projection.go
package executor

import "github.com/yourname/go-rdbms/sql/parser"

// Projection は指定されたカラムだけを取り出すオペレータ
type Projection struct {
	child   Operator
	indexes []int // 子の出力列のうち、何番目を取り出すか
	columns []OutputColumn
}

// NewProjection はカラム参照を子の出力列の位置に解決してから作る
func NewProjection(child Operator, refs []*parser.ColumnRef) (*Projection, error) {
	childCols := child.Columns()
	p := &Projection{child: child}
	for _, ref := range refs {
		idx, err := resolveColumn(childCols, ref)
		if err != nil {
			return nil, err
		}
		p.indexes = append(p.indexes, idx)
		p.columns = append(p.columns, childCols[idx])
	}
	return p, nil
}

func (p *Projection) Open() error             { return p.child.Open() }
func (p *Projection) Close() error            { return p.child.Close() }
func (p *Projection) Columns() []OutputColumn { return p.columns }

func (p *Projection) Next() (*Tuple, error) {
	tuple, err := p.child.Next()
	if err != nil || tuple == nil {
		return nil, err
	}
	values := make([]Value, len(p.indexes))
	for i, idx := range p.indexes {
		values[i] = tuple.Values[idx]
	}
	return &Tuple{Values: values}, nil
}
