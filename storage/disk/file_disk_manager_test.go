// storage/disk/file_disk_manager_test.go
package disk_test

import (
	"os"
	"testing"

	"github.com/yourname/go-rdbms/storage/disk"
	"github.com/yourname/go-rdbms/storage/page"
)

// setupDiskManager はテスト用の一時ファイルでDiskManagerを作成する
func setupDiskManager(t *testing.T) (disk.DiskManager, func()) {
	t.Helper()

	f, err := os.CreateTemp("", "test-disk-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	dm, err := disk.NewFileDiskManager(f.Name())
	if err != nil {
		os.Remove(f.Name())
		t.Fatal(err)
	}

	cleanup := func() {
		dm.Close()
		os.Remove(f.Name())
	}
	return dm, cleanup
}

func TestFileDiskManager_AllocateAndWrite(t *testing.T) {
	dm, cleanup := setupDiskManager(t)
	defer cleanup()

	// 最初はページ数が0
	if dm.NumPages() != 0 {
		t.Fatalf("初期ページ数は0のはず: got %d", dm.NumPages())
	}

	// ページを割り当てる
	pageID, err := dm.AllocatePage()
	if err != nil {
		t.Fatal(err)
	}
	if pageID != 0 {
		t.Fatalf("最初のPageIDは0のはず: got %d", pageID)
	}
	if dm.NumPages() != 1 {
		t.Fatalf("AllocatePage後のページ数は1のはず: got %d", dm.NumPages())
	}

	// ページに書き込む
	var p page.Page
	p.SetPageID(pageID)
	p.SetPageType(page.PageTypeHeap)
	copy(p.Data(), []byte("hello, disk manager!"))

	if err := dm.WritePage(pageID, &p); err != nil {
		t.Fatal(err)
	}

	// ページを読み込む
	var readPage page.Page
	if err := dm.ReadPage(pageID, &readPage); err != nil {
		t.Fatal(err)
	}

	// 内容を検証
	if readPage.GetPageID() != pageID {
		t.Errorf("PageID不一致: want %d, got %d", pageID, readPage.GetPageID())
	}
	if readPage.GetPageType() != page.PageTypeHeap {
		t.Errorf("PageType不一致: want %d, got %d", page.PageTypeHeap, readPage.GetPageType())
	}
	if string(readPage.Data()[:20]) != "hello, disk manager!" {
		t.Errorf("データ不一致: got %q", readPage.Data()[:20])
	}
}

func TestFileDiskManager_MultiplePages(t *testing.T) {
	dm, cleanup := setupDiskManager(t)
	defer cleanup()

	const numPages = 10

	// 10ページを割り当てて書き込む
	for i := 0; i < numPages; i++ {
		pageID, err := dm.AllocatePage()
		if err != nil {
			t.Fatal(err)
		}

		var p page.Page
		p.SetPageID(pageID)
		// データ領域にページIDを書き込む
		p.Data()[0] = byte(i)

		if err := dm.WritePage(pageID, &p); err != nil {
			t.Fatal(err)
		}
	}

	// 各ページを読み込んで検証
	for i := 0; i < numPages; i++ {
		var p page.Page
		if err := dm.ReadPage(page.PageID(i), &p); err != nil {
			t.Fatal(err)
		}
		if p.Data()[0] != byte(i) {
			t.Errorf("ページ%d: データ不一致: want %d, got %d", i, i, p.Data()[0])
		}
	}
}

func TestFileDiskManager_ReadOutOfRange(t *testing.T) {
	dm, cleanup := setupDiskManager(t)
	defer cleanup()

	// ページを割り当てずに読もうとするとエラー
	var p page.Page
	err := dm.ReadPage(0, &p)
	if err == nil {
		t.Error("範囲外のページを読み込もうとしてもエラーにならなかった")
	}
}

func TestFileDiskManager_PersistAfterReopen(t *testing.T) {
	f, err := os.CreateTemp("", "test-persist-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	filePath := f.Name()
	defer os.Remove(filePath)

	// 1回目: 書き込んで閉じる
	{
		dm, err := disk.NewFileDiskManager(filePath)
		if err != nil {
			t.Fatal(err)
		}

		pageID, _ := dm.AllocatePage()
		var p page.Page
		copy(p.Data(), []byte("persist test"))
		dm.WritePage(pageID, &p)
		dm.Sync()
		dm.Close()
	}

	// 2回目: 再度開いて読む
	{
		dm, err := disk.NewFileDiskManager(filePath)
		if err != nil {
			t.Fatal(err)
		}
		defer dm.Close()

		if dm.NumPages() != 1 {
			t.Fatalf("再オープン後のページ数が不正: want 1, got %d", dm.NumPages())
		}

		var p page.Page
		if err := dm.ReadPage(0, &p); err != nil {
			t.Fatal(err)
		}
		if string(p.Data()[:12]) != "persist test" {
			t.Errorf("データが永続化されていない: got %q", p.Data()[:12])
		}
	}
}
