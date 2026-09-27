// sql/catalog/catalog.go
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"sync"

	"github.com/yourname/go-rdbms/storage/btree"
	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/heap"
	"github.com/yourname/go-rdbms/storage/page"
)

// TableInfo はテーブルの定義と、実データ・インデックスへの参照を持つ
type TableInfo struct {
	Name            string      `json:"name"`
	Schema          *Schema     `json:"schema"`
	FirstPageID     page.PageID `json:"first_page_id"`      // ヒープファイルの先頭ページ
	IndexRootPageID page.PageID `json:"index_root_page_id"` // 主キーのB+木のルート（なければInvalidPageID）

	// 以下は起動時にページIDから復元するので、JSONには保存しない
	Heap  *heap.HeapFile   `json:"-"`
	Index *btree.BPlusTree `json:"-"` // 主キーがなければnil
}

// catalogFile はカタログファイル（JSON）の中身
type catalogFile struct {
	Tables []*TableInfo `json:"tables"`
}

// Catalog はテーブル定義を管理し、JSONファイルに永続化する
type Catalog struct {
	mu     sync.RWMutex
	bpm    *buffer.BufferPoolManager
	path   string
	tables map[string]*TableInfo
}

// NewCatalog はカタログファイルを読み込んでカタログを作る
// ファイルがまだなければ、空のカタログとして始める
func NewCatalog(bpm *buffer.BufferPoolManager, path string) (*Catalog, error) {
	c := &Catalog{bpm: bpm, path: path, tables: make(map[string]*TableInfo)}
	if err := c.load(); err != nil {
		return nil, fmt.Errorf("catalog: %s を読み込めません: %w", path, err)
	}
	return c, nil
}

// load はカタログファイルを読み、各テーブルのヒープファイルとインデックスを開き直す
func (c *Catalog) load() error {
	data, err := os.ReadFile(c.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var cf catalogFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return err
	}
	for _, info := range cf.Tables {
		if info.Heap, err = heap.OpenHeapFile(c.bpm, info.FirstPageID); err != nil {
			return err
		}
		if info.IndexRootPageID != page.InvalidPageID {
			info.Index = btree.OpenBPlusTree(c.bpm, info.IndexRootPageID)
		}
		c.tables[info.Name] = info
	}
	return nil
}

// save はカタログ全体をJSONファイルに書き出す
// 一時ファイルに書いてfsyncしてからリネームするので、書き込み途中で落ちても
// 「古いカタログ」か「新しいカタログ」のどちらかが必ず残る
func (c *Catalog) save() error {
	cf := catalogFile{}
	for _, name := range c.tableNames() {
		cf.Tables = append(cf.Tables, c.tables[name])
	}
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}

	tmp := c.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
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
	info := &TableInfo{
		Name:            name,
		Schema:          schema,
		FirstPageID:     hf.FirstPageID(),
		IndexRootPageID: page.InvalidPageID,
		Heap:            hf,
	}
	if schema.PrimaryKeyIndex() >= 0 {
		if info.Index, err = btree.NewBPlusTree(c.bpm); err != nil {
			return nil, err
		}
		info.IndexRootPageID = info.Index.RootPageID()
	}

	// カタログが指すページを先にディスクへ書いてから、カタログを保存する。
	// 逆の順序だと、カタログだけ残って中身が初期化されていないページを指す恐れがある
	if err := c.bpm.FlushAllPages(); err != nil {
		return nil, err
	}
	c.tables[name] = info
	if err := c.save(); err != nil {
		delete(c.tables, name)
		return nil, err
	}
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
	return c.tableNames()
}

func (c *Catalog) tableNames() []string {
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
