// storage/disk/disk_manager.go
package disk

import "github.com/yourname/go-rdbms/storage/page"

// DiskManager はデータベースファイルへのページ単位の読み書きを担う
type DiskManager interface {
	// ReadPage は指定されたPageIDのページを読み込みpに格納する
	ReadPage(pageID page.PageID, p *page.Page) error
	// WritePage は指定されたPageIDの位置にpの内容を書き込む
	WritePage(pageID page.PageID, p *page.Page) error
	// AllocatePage は新しいページを割り当て、そのPageIDを返す
	AllocatePage() (page.PageID, error)
	// NumPages は現在のページ数を返す
	NumPages() page.PageID
	// Sync はOSバッファをディスクにフラッシュする
	Sync() error
	// Close はファイルを閉じる
	Close() error
}
