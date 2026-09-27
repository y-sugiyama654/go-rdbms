// storage/buffer/buffer_pool_manager_test.go
package buffer_test

import (
	"os"
	"testing"

	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/disk"
)

// setupBufferPoolManager はテスト用の一時ファイルでBufferPoolManagerを作成する
func setupBufferPoolManager(t *testing.T, poolSize int) (*buffer.BufferPoolManager, func()) {
	t.Helper()

	f, err := os.CreateTemp("", "test-buffer-*.db")
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
	cleanup := func() {
		dm.Close()
		os.Remove(f.Name())
	}
	return bpm, cleanup
}

func TestBufferPoolManager_NewPageAndFetch(t *testing.T) {
	bpm, cleanup := setupBufferPoolManager(t, 3)
	defer cleanup()

	pageID, p, err := bpm.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	copy(p.Data(), []byte("hello, buffer pool!"))
	if err := bpm.UnpinPage(pageID, true); err != nil {
		t.Fatal(err)
	}

	fetched, err := bpm.FetchPage(pageID)
	if err != nil {
		t.Fatal(err)
	}
	if string(fetched.Data()[:19]) != "hello, buffer pool!" {
		t.Errorf("データ不一致: got %q", fetched.Data()[:19])
	}
	bpm.UnpinPage(pageID, false)
}

func TestBufferPoolManager_EvictsLeastRecentlyUsed(t *testing.T) {
	bpm, cleanup := setupBufferPoolManager(t, 2)
	defer cleanup()

	id1, p1, _ := bpm.NewPage()
	copy(p1.Data(), []byte("page1"))
	bpm.UnpinPage(id1, true)

	id2, p2, _ := bpm.NewPage()
	copy(p2.Data(), []byte("page2"))
	bpm.UnpinPage(id2, true)

	// id1を使うことで最近使った扱いにする（id2がLRUの先頭に残る）
	bpm.FetchPage(id1)
	bpm.UnpinPage(id1, false)

	// 3ページ目を確保すると、直近アクセスのないid2が追い出される
	id3, p3, _ := bpm.NewPage()
	copy(p3.Data(), []byte("page3"))
	bpm.UnpinPage(id3, true)

	// id1は引き続きバッファプール上にあるはず
	if _, err := bpm.FetchPage(id1); err != nil {
		t.Fatalf("id1はバッファプールに残っているはず: %v", err)
	}
	bpm.UnpinPage(id1, false)

	// id2はディスクから再読込されるが、追い出し時に書き戻されているのでデータは保持されている
	fetched2, err := bpm.FetchPage(id2)
	if err != nil {
		t.Fatal(err)
	}
	if string(fetched2.Data()[:5]) != "page2" {
		t.Errorf("id2のデータが失われている: got %q", fetched2.Data()[:5])
	}
	bpm.UnpinPage(id2, false)
}

func TestBufferPoolManager_AllPinnedReturnsError(t *testing.T) {
	bpm, cleanup := setupBufferPoolManager(t, 1)
	defer cleanup()

	if _, _, err := bpm.NewPage(); err != nil {
		t.Fatal(err)
	}

	// プールサイズ1で、既存ページをアンピンせずに新規ページを確保しようとするとエラー
	if _, _, err := bpm.NewPage(); err == nil {
		t.Error("全フレームがピン中なのにエラーにならなかった")
	}
}

func TestBufferPoolManager_FlushPage(t *testing.T) {
	bpm, cleanup := setupBufferPoolManager(t, 2)
	defer cleanup()

	pageID, p, _ := bpm.NewPage()
	copy(p.Data(), []byte("flush test"))
	bpm.UnpinPage(pageID, true)

	if err := bpm.FlushPage(pageID); err != nil {
		t.Fatal(err)
	}
}

func TestBufferPoolManager_FailedFetchReleasesFrame(t *testing.T) {
	bpm, cleanup := setupBufferPoolManager(t, 1)
	defer cleanup()

	// 存在しないページのFetchは失敗する
	if _, err := bpm.FetchPage(99); err == nil {
		t.Fatal("存在しないページのFetchがエラーにならなかった")
	}

	// 失敗したFetchで確保したフレームは空きに戻っているので、新しいページを作れる
	pageID, _, err := bpm.NewPage()
	if err != nil {
		t.Fatalf("フレームがリークしている: %v", err)
	}
	bpm.UnpinPage(pageID, false)
}
