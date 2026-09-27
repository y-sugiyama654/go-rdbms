// storage/btree/btree_test.go
package btree_test

import (
	"errors"
	"math/rand"
	"os"
	"testing"

	"github.com/yourname/go-rdbms/storage/btree"
	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/disk"
	"github.com/yourname/go-rdbms/storage/heap"
	"github.com/yourname/go-rdbms/storage/page"
)

func setupBTree(t *testing.T) (*btree.BPlusTree, func()) {
	t.Helper()

	f, err := os.CreateTemp("", "test-btree-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	dm, err := disk.NewFileDiskManager(f.Name())
	if err != nil {
		os.Remove(f.Name())
		t.Fatal(err)
	}
	bpm := buffer.NewBufferPoolManager(dm, 64)
	bt, err := btree.NewBPlusTree(bpm)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		dm.Close()
		os.Remove(f.Name())
	}
	return bt, cleanup
}

// ridFor はテスト用に、キーから決まるダミーのRIDを作る
func ridFor(key int64) heap.RID {
	return heap.RID{PageID: page.PageID(key / 100), SlotID: uint16(key % 100)}
}

func TestBPlusTree_InsertAndSearch(t *testing.T) {
	bt, cleanup := setupBTree(t)
	defer cleanup()

	if err := bt.Insert(42, ridFor(42)); err != nil {
		t.Fatal(err)
	}
	rid, found, err := bt.Search(42)
	if err != nil {
		t.Fatal(err)
	}
	if !found || rid != ridFor(42) {
		t.Errorf("検索結果が不正: found=%v, rid=%v", found, rid)
	}

	if _, found, _ := bt.Search(999); found {
		t.Error("存在しないキーが見つかった")
	}
}

func TestBPlusTree_DuplicateKey(t *testing.T) {
	bt, cleanup := setupBTree(t)
	defer cleanup()

	bt.Insert(1, ridFor(1))
	if err := bt.Insert(1, ridFor(1)); !errors.Is(err, btree.ErrDuplicateKey) {
		t.Errorf("重複キーはErrDuplicateKeyになるはず: got %v", err)
	}
}

func TestBPlusTree_ManyKeysGrowTree(t *testing.T) {
	bt, cleanup := setupBTree(t)
	defer cleanup()

	const n = 100000
	for i := int64(0); i < n; i++ {
		if err := bt.Insert(i, ridFor(i)); err != nil {
			t.Fatalf("キー%dの挿入に失敗: %v", i, err)
		}
	}

	height, err := bt.Height()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d件挿入後の木の高さ: %d", n, height)
	if height < 3 {
		t.Errorf("内部ノードが多段になっていない: height=%d", height)
	}

	for i := int64(0); i < n; i++ {
		rid, found, err := bt.Search(i)
		if err != nil {
			t.Fatal(err)
		}
		if !found || rid != ridFor(i) {
			t.Fatalf("キー%dが正しく引けない: found=%v, rid=%v", i, found, rid)
		}
	}
}

func TestBPlusTree_RandomOrder(t *testing.T) {
	bt, cleanup := setupBTree(t)
	defer cleanup()

	const n = 20000
	keys := rand.New(rand.NewSource(1)).Perm(n)
	for _, k := range keys {
		if err := bt.Insert(int64(k), ridFor(int64(k))); err != nil {
			t.Fatal(err)
		}
	}

	// 全範囲をスキャンすると、キー順（= RID順）に全件が返る
	rids, err := bt.RangeScan(0, n-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rids) != n {
		t.Fatalf("件数不一致: want %d, got %d", n, len(rids))
	}
	for i, rid := range rids {
		if rid != ridFor(int64(i)) {
			t.Fatalf("%d番目がキー順になっていない: %v", i, rid)
		}
	}
}

func TestBPlusTree_RangeScan(t *testing.T) {
	bt, cleanup := setupBTree(t)
	defer cleanup()

	for i := int64(1); i <= 1000; i++ {
		bt.Insert(i*10, ridFor(i*10)) // 10, 20, ..., 10000
	}

	tests := []struct {
		min, max int64
		want     int
	}{
		{20, 40, 3},       // 20, 30, 40
		{15, 45, 3},       // 境界がキーと一致しない場合
		{1, 10000, 1000},  // 全件（複数の葉にまたがる）
		{5000, 5000, 1},   // 1点
		{10001, 20000, 0}, // 範囲外
		{40, 20, 0},       // min > max
	}
	for _, tt := range tests {
		rids, err := bt.RangeScan(tt.min, tt.max)
		if err != nil {
			t.Fatal(err)
		}
		if len(rids) != tt.want {
			t.Errorf("RangeScan(%d, %d): want %d件, got %d件", tt.min, tt.max, tt.want, len(rids))
		}
	}
}

func TestBPlusTree_Delete(t *testing.T) {
	bt, cleanup := setupBTree(t)
	defer cleanup()

	const n = 2000
	for i := int64(0); i < n; i++ {
		bt.Insert(i, ridFor(i))
	}
	// 偶数のキーを削除する
	for i := int64(0); i < n; i += 2 {
		if err := bt.Delete(i); err != nil {
			t.Fatal(err)
		}
	}

	for i := int64(0); i < n; i++ {
		_, found, _ := bt.Search(i)
		if want := i%2 == 1; found != want {
			t.Fatalf("キー%d: want found=%v, got %v", i, want, found)
		}
	}
	if err := bt.Delete(0); !errors.Is(err, btree.ErrKeyNotFound) {
		t.Errorf("削除済みキーの削除はErrKeyNotFoundになるはず: got %v", err)
	}
	rids, _ := bt.RangeScan(0, n)
	if len(rids) != n/2 {
		t.Errorf("削除後の件数不一致: want %d, got %d", n/2, len(rids))
	}
}
