// storage/heap/heap_file_test.go
package heap_test

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/disk"
	"github.com/yourname/go-rdbms/storage/heap"
	"github.com/yourname/go-rdbms/storage/page"
)

// setupHeapFile はテスト用の一時ファイル上にHeapFileを作る
func setupHeapFile(t *testing.T, poolSize int) (*heap.HeapFile, *buffer.BufferPoolManager, func()) {
	t.Helper()

	f, err := os.CreateTemp("", "test-heap-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	dm, err := disk.NewFileDiskManager(f.Name())
	if err != nil {
		os.Remove(f.Name())
		t.Fatal(err)
	}
	bpm := buffer.NewBufferPoolManager(dm, poolSize)
	hf, err := heap.NewHeapFile(bpm)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		dm.Close()
		os.Remove(f.Name())
	}
	return hf, bpm, cleanup
}

func TestHeapFile_InsertAndGet(t *testing.T) {
	hf, _, cleanup := setupHeapFile(t, 10)
	defer cleanup()

	data := []byte("hello, heap file!")
	rid, err := hf.InsertRecord(nil, data)
	if err != nil {
		t.Fatal(err)
	}

	got, err := hf.GetRecord(rid)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("データ不一致: want %q, got %q", data, got)
	}
}

func TestHeapFile_ScanAcrossPages(t *testing.T) {
	// プールサイズを小さくして、スキャン中にページの追い出しも起きるようにする
	hf, _, cleanup := setupHeapFile(t, 4)
	defer cleanup()

	const n = 1000
	for i := 0; i < n; i++ {
		rec := []byte(fmt.Sprintf("record-%04d-%s", i, bytes.Repeat([]byte("x"), 80)))
		if _, err := hf.InsertRecord(nil, rec); err != nil {
			t.Fatal(err)
		}
	}

	scanner := hf.Scan()
	count := 0
	pages := map[page.PageID]bool{}
	for {
		rid, data, ok, err := scanner.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		want := fmt.Sprintf("record-%04d-", count)
		if !bytes.HasPrefix(data, []byte(want)) {
			t.Fatalf("%d件目のデータ不一致: got %q", count, data[:12])
		}
		pages[rid.PageID] = true
		count++
	}
	if count != n {
		t.Errorf("件数不一致: want %d, got %d", n, count)
	}
	if len(pages) < 2 {
		t.Errorf("複数ページにまたがっていない: %dページ", len(pages))
	}
}

func TestHeapFile_Delete(t *testing.T) {
	hf, _, cleanup := setupHeapFile(t, 10)
	defer cleanup()

	rid1, _ := hf.InsertRecord(nil, []byte("Alice"))
	rid2, _ := hf.InsertRecord(nil, []byte("Bob"))

	if err := hf.DeleteRecord(nil, rid1); err != nil {
		t.Fatal(err)
	}
	if _, err := hf.GetRecord(rid1); err == nil {
		t.Error("削除済みレコードを取得できてしまった")
	}

	// スキャンでは削除済みレコードが読み飛ばされる
	rid, data, ok, err := hf.Scan().Next()
	if err != nil || !ok {
		t.Fatalf("スキャン失敗: ok=%v, err=%v", ok, err)
	}
	if rid != rid2 || string(data) != "Bob" {
		t.Errorf("削除されていないBobが返るはず: rid=%v, data=%q", rid, data)
	}
}

func TestHeapFile_Reopen(t *testing.T) {
	hf, bpm, cleanup := setupHeapFile(t, 10)
	defer cleanup()

	for i := 0; i < 200; i++ {
		if _, err := hf.InsertRecord(nil, bytes.Repeat([]byte("y"), 100)); err != nil {
			t.Fatal(err)
		}
	}

	// 先頭ページIDだけを手がかりに開き直すと、末尾ページが復元される
	reopened, err := heap.OpenHeapFile(bpm, hf.FirstPageID())
	if err != nil {
		t.Fatal(err)
	}
	rid, err := reopened.InsertRecord(nil, []byte("after reopen"))
	if err != nil {
		t.Fatal(err)
	}
	if rid.PageID == hf.FirstPageID() {
		t.Errorf("末尾ページに追記されるはずが先頭ページに入った: %v", rid)
	}
}

func TestHeapFile_TooLargeRecord(t *testing.T) {
	hf, _, cleanup := setupHeapFile(t, 10)
	defer cleanup()

	if _, err := hf.InsertRecord(nil, make([]byte, page.MaxRecordSize+1)); err == nil {
		t.Error("1ページに収まらないレコードがエラーにならなかった")
	}
	if _, err := hf.InsertRecord(nil, make([]byte, page.MaxRecordSize)); err != nil {
		t.Errorf("上限ちょうどのレコードは格納できるはず: %v", err)
	}
}

func TestRecord_EncodeDecode(t *testing.T) {
	fields := [][]byte{{0, 0, 0, 42}, []byte("Alice"), {}}
	data, err := heap.EncodeRecord(fields)
	if err != nil {
		t.Fatal(err)
	}
	got, err := heap.DecodeRecord(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(fields) {
		t.Fatalf("フィールド数不一致: want %d, got %d", len(fields), len(got))
	}
	for i := range fields {
		if !bytes.Equal(got[i], fields[i]) {
			t.Errorf("フィールド%d不一致: want %v, got %v", i, fields[i], got[i])
		}
	}
}
