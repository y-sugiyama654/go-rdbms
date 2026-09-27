// sql/catalog/catalog_test.go
package catalog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/disk"
)

// openCatalog はdir内のデータファイルとカタログファイルを開く
func openCatalog(t *testing.T, dir string) (*catalog.Catalog, *buffer.BufferPoolManager, disk.DiskManager) {
	t.Helper()
	dm, err := disk.NewFileDiskManager(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	bpm := buffer.NewBufferPoolManager(dm, 32)
	cat, err := catalog.NewCatalog(bpm, filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	return cat, bpm, dm
}

var userColumns = []catalog.Column{
	{Name: "ID", Type: catalog.TypeInt, PrimaryKey: true},
	{Name: "NAME", Type: catalog.TypeVarchar, Length: 50},
}

func TestCatalog_CreateAndGet(t *testing.T) {
	cat, _, dm := openCatalog(t, t.TempDir())
	defer dm.Close()

	if _, err := cat.CreateTable("USERS", userColumns); err != nil {
		t.Fatal(err)
	}
	info, err := cat.GetTable("USERS")
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Schema.Columns) != 2 || info.Heap == nil || info.Index == nil {
		t.Errorf("テーブル情報が不完全: %+v", info)
	}
	if _, err := cat.GetTable("NOTHING"); err == nil {
		t.Error("存在しないテーブルでエラーにならなかった")
	}
}

func TestCatalog_InvalidDefinitions(t *testing.T) {
	cat, _, dm := openCatalog(t, t.TempDir())
	defer dm.Close()

	cat.CreateTable("USERS", userColumns)
	tests := []struct {
		table string
		cols  []catalog.Column
	}{
		{"USERS", userColumns}, // 同名のテーブル
		{"T1", []catalog.Column{{Name: "A", Type: catalog.TypeInt}, {Name: "A", Type: catalog.TypeInt}}},
		{"T2", []catalog.Column{{Name: "A", Type: catalog.TypeVarchar, Length: 5, PrimaryKey: true}}},
		{"T3", []catalog.Column{
			{Name: "A", Type: catalog.TypeInt, PrimaryKey: true},
			{Name: "B", Type: catalog.TypeInt, PrimaryKey: true},
		}},
	}
	for _, tt := range tests {
		if _, err := cat.CreateTable(tt.table, tt.cols); err == nil {
			t.Errorf("%s: エラーになるはず", tt.table)
		}
	}
}

func TestCatalog_Persistence(t *testing.T) {
	dir := t.TempDir()

	// 1回目: テーブルを作ってデータを入れ、ページを書き出して閉じる
	{
		cat, bpm, dm := openCatalog(t, dir)
		info, err := cat.CreateTable("USERS", userColumns)
		if err != nil {
			t.Fatal(err)
		}
		for i := int64(1); i <= 300; i++ {
			rid, err := info.Heap.InsertRecord([]byte(strings.Repeat("x", 50)))
			if err != nil {
				t.Fatal(err)
			}
			if err := info.Index.Insert(i, rid); err != nil {
				t.Fatal(err)
			}
		}
		if err := bpm.FlushAllPages(); err != nil {
			t.Fatal(err)
		}
		dm.Close()
	}

	// カタログファイルの中身はJSON
	data, _ := os.ReadFile(filepath.Join(dir, "catalog.json"))
	t.Logf("catalog.json:\n%s", data)

	// 2回目: 開き直すと、テーブル定義・ヒープ・インデックスが復元される
	cat, _, dm := openCatalog(t, dir)
	defer dm.Close()

	info, err := cat.GetTable("USERS")
	if err != nil {
		t.Fatalf("再オープン後にテーブルが見つからない: %v", err)
	}
	if info.Schema.Columns[1].Type != catalog.TypeVarchar || info.Schema.Columns[1].Length != 50 {
		t.Errorf("スキーマが復元されていない: %+v", info.Schema.Columns[1])
	}

	count := 0
	for scanner := info.Heap.Scan(); ; count++ {
		_, _, ok, err := scanner.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
	}
	if count != 300 {
		t.Errorf("ヒープのレコード数: want 300, got %d", count)
	}
	if _, found, _ := info.Index.Search(150); !found {
		t.Error("インデックスからキー150が引けない")
	}

	// 開き直した後も、ヒープの末尾に追記できる
	if _, err := info.Heap.InsertRecord([]byte("after reopen")); err != nil {
		t.Fatal(err)
	}
}
