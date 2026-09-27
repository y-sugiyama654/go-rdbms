// storage/disk/file_disk_manager.go
package disk

import (
	"fmt"
	"os"
	"sync"

	"github.com/yourname/go-rdbms/storage/page"
)

// FileDiskManager は DiskManager インターフェースのファイルベース実装
type FileDiskManager struct {
	file     *os.File
	numPages page.PageID
	mu       sync.RWMutex
}

// NewFileDiskManager は指定されたパスのファイルを開き、FileDiskManagerを返す
// ファイルが存在しない場合は新規作成する
func NewFileDiskManager(filePath string) (*FileDiskManager, error) {
	f, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("disk: ファイルを開けませんでした(%s): %w", filePath, err)
	}

	// 既存ファイルのページ数を計算
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("disk: ファイル情報の取得に失敗しました: %w", err)
	}
	numPages := page.PageID(info.Size() / page.PageSize)

	return &FileDiskManager{
		file:     f,
		numPages: numPages,
	}, nil
}

// ReadPage は指定されたPageIDのページを読み込む
func (dm *FileDiskManager) ReadPage(pageID page.PageID, p *page.Page) error {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	if pageID >= dm.numPages {
		return fmt.Errorf("disk: PageID %d は範囲外です（総ページ数: %d）", pageID, dm.numPages)
	}

	offset := int64(pageID) * page.PageSize
	n, err := dm.file.ReadAt(p[:], offset)
	if err != nil {
		return fmt.Errorf("disk: ページ %d の読み込みに失敗しました: %w", pageID, err)
	}
	if n != page.PageSize {
		return fmt.Errorf("disk: ページ %d の読み込みサイズが不正です（期待: %d, 実際: %d）",
			pageID, page.PageSize, n)
	}
	return nil
}

// WritePage は指定されたPageIDの位置にページを書き込む
func (dm *FileDiskManager) WritePage(pageID page.PageID, p *page.Page) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	offset := int64(pageID) * page.PageSize
	n, err := dm.file.WriteAt(p[:], offset)
	if err != nil {
		return fmt.Errorf("disk: ページ %d の書き込みに失敗しました: %w", pageID, err)
	}
	if n != page.PageSize {
		return fmt.Errorf("disk: ページ %d の書き込みサイズが不正です（期待: %d, 実際: %d）",
			pageID, page.PageSize, n)
	}

	// 書き込んだページIDが現在の末尾を超えていればページ数を更新
	if pageID >= dm.numPages {
		dm.numPages = pageID + 1
	}
	return nil
}

// AllocatePage は新しいページを末尾に割り当てる
func (dm *FileDiskManager) AllocatePage() (page.PageID, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	newPageID := dm.numPages

	// 新しいページ領域をゼロで初期化して書き込む
	var emptyPage page.Page
	offset := int64(newPageID) * page.PageSize
	if _, err := dm.file.WriteAt(emptyPage[:], offset); err != nil {
		return page.InvalidPageID, fmt.Errorf("disk: 新規ページの割り当てに失敗しました: %w", err)
	}

	dm.numPages++
	return newPageID, nil
}

// NumPages は現在のページ数を返す
func (dm *FileDiskManager) NumPages() page.PageID {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.numPages
}

// Sync はOSバッファをディスクにフラッシュする
func (dm *FileDiskManager) Sync() error {
	if err := dm.file.Sync(); err != nil {
		return fmt.Errorf("disk: fsync に失敗しました: %w", err)
	}
	return nil
}

// Close はファイルを閉じる
func (dm *FileDiskManager) Close() error {
	return dm.file.Close()
}
