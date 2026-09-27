// sql/catalog/catalog.go
package catalog

import (
	"fmt"
	"sort"
	"sync"

	"github.com/yourname/go-rdbms/storage/btree"
	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/heap"
)

// TableInfo はテーブルの定義と、実データ・インデックスへの参照を持つ
type TableInfo struct {
	Name   string
	Schema *Schema
	Heap   *heap.HeapFile
	Index  *btree.BPlusTree // 主キーのインデックス（主キーがなければnil）
}

// Catalog はテーブル定義を管理する（第8章の時点ではメモリ上だけで管理する）
type Catalog struct {
	mu     sync.RWMutex
	bpm    *buffer.BufferPoolManager
	tables map[string]*TableInfo
}

// NewCatalog は空のカタログを作る
func NewCatalog(bpm *buffer.BufferPoolManager) *Catalog {
	return &Catalog{bpm: bpm, tables: make(map[string]*TableInfo)}
}

// CreateTable はテーブルを作成し、ヒープファイルと主キーのインデックスを用意する
func (c *Catalog) CreateTable(name string, columns []Column) (*TableInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.tables[name]; exists {
		return nil, fmt.Errorf("catalog: テーブル %s は既に存在します", name)
	}
	schema := &Schema{Columns: columns}
	if err := validateSchema(schema); err != nil {
		return nil, err
	}

	hf, err := heap.NewHeapFile(c.bpm)
	if err != nil {
		return nil, err
	}
	info := &TableInfo{Name: name, Schema: schema, Heap: hf}
	if schema.PrimaryKeyIndex() >= 0 {
		if info.Index, err = btree.NewBPlusTree(c.bpm); err != nil {
			return nil, err
		}
	}
	c.tables[name] = info
	return info, nil
}

// GetTable はテーブル名からテーブル情報を返す
func (c *Catalog) GetTable(name string) (*TableInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info, ok := c.tables[name]
	if !ok {
		return nil, fmt.Errorf("catalog: テーブル %s は存在しません", name)
	}
	return info, nil
}

// ListTables はテーブル名の一覧を名前順で返す
func (c *Catalog) ListTables() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	names := make([]string, 0, len(c.tables))
	for name := range c.tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// validateSchema はカラム定義の妥当性を確認する
func validateSchema(s *Schema) error {
	if len(s.Columns) == 0 {
		return fmt.Errorf("catalog: カラムが1つもありません")
	}
	seen := map[string]bool{}
	pkCount := 0
	for _, col := range s.Columns {
		if seen[col.Name] {
			return fmt.Errorf("catalog: カラム %s が重複しています", col.Name)
		}
		seen[col.Name] = true
		if col.PrimaryKey {
			pkCount++
			if col.Type != TypeInt {
				return fmt.Errorf("catalog: PRIMARY KEY は INT 型のカラムにだけ指定できます")
			}
		}
	}
	if pkCount > 1 {
		return fmt.Errorf("catalog: PRIMARY KEY は1つのカラムにだけ指定できます")
	}
	return nil
}
