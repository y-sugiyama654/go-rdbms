// sql/executor/scan.go
package executor

import (
	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/storage/heap"
)

// tableColumns はテーブルの全カラムを出力列の並びに変換する
func tableColumns(table *catalog.TableInfo) []OutputColumn {
	cols := make([]OutputColumn, len(table.Schema.Columns))
	for i, c := range table.Schema.Columns {
		cols[i] = OutputColumn{Table: table.Name, Name: c.Name}
	}
	return cols
}

// SeqScan はヒープファイルを先頭から全件読むオペレータ
type SeqScan struct {
	table   *catalog.TableInfo
	scanner *heap.HeapScanner
}

func NewSeqScan(table *catalog.TableInfo) *SeqScan {
	return &SeqScan{table: table}
}

func (s *SeqScan) Open() error {
	s.scanner = s.table.Heap.Scan()
	return nil
}

func (s *SeqScan) Next() (*Tuple, error) {
	_, data, ok, err := s.scanner.Next()
	if err != nil || !ok {
		return nil, err
	}
	values, err := DecodeTuple(data, s.table.Schema)
	if err != nil {
		return nil, err
	}
	return &Tuple{Values: values}, nil
}

func (s *SeqScan) Close() error            { return nil }
func (s *SeqScan) Columns() []OutputColumn { return tableColumns(s.table) }

// IndexScan は主キーのB+木で範囲 [lo, hi] のRIDを求め、そのレコードだけを読むオペレータ
type IndexScan struct {
	table  *catalog.TableInfo
	lo, hi int64
	rids   []heap.RID
	pos    int
}

func NewIndexScan(table *catalog.TableInfo, lo, hi int64) *IndexScan {
	return &IndexScan{table: table, lo: lo, hi: hi}
}

func (s *IndexScan) Open() error {
	rids, err := s.table.Index.RangeScan(s.lo, s.hi)
	if err != nil {
		return err
	}
	s.rids, s.pos = rids, 0
	return nil
}

func (s *IndexScan) Next() (*Tuple, error) {
	if s.pos >= len(s.rids) {
		return nil, nil
	}
	rid := s.rids[s.pos]
	s.pos++
	data, err := s.table.Heap.GetRecord(rid)
	if err != nil {
		return nil, err
	}
	values, err := DecodeTuple(data, s.table.Schema)
	if err != nil {
		return nil, err
	}
	return &Tuple{Values: values}, nil
}

func (s *IndexScan) Close() error            { return nil }
func (s *IndexScan) Columns() []OutputColumn { return tableColumns(s.table) }
