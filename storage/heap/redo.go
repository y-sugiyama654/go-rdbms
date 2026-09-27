// storage/heap/redo.go
package heap

import (
	"fmt"

	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/page"
)

// ここにあるのは、リカバリ（第12章）でWALを再適用するための関数。
// どれも「ページLSN >= ログのLSN なら、そのページには反映済み」と判断して何もしない。
// そのため同じログを何度適用しても結果は変わらない（冪等）。

// RedoInsert はINSERTのログを再適用する
func RedoInsert(bpm *buffer.BufferPoolManager, rid RID, data []byte, lsn uint64) error {
	p, err := bpm.FetchPage(rid.PageID)
	if err != nil {
		return err
	}
	if p.GetLSN() >= lsn {
		return bpm.UnpinPage(rid.PageID, false)
	}
	sp := page.NewSlottedPage(p)
	// ページはこのログの直前の状態のはずなので、次に割り当てられるスロットが一致する
	if sp.NumSlots() != rid.SlotID {
		bpm.UnpinPage(rid.PageID, false)
		return fmt.Errorf("heap: REDO先のスロットが一致しません（ページ %d: 期待 %d, 実際 %d）",
			rid.PageID, rid.SlotID, sp.NumSlots())
	}
	if _, err := sp.InsertRecord(data); err != nil {
		bpm.UnpinPage(rid.PageID, false)
		return err
	}
	p.SetLSN(lsn)
	return bpm.UnpinPage(rid.PageID, true)
}

// RedoDelete はDELETEのログを再適用する
func RedoDelete(bpm *buffer.BufferPoolManager, rid RID, lsn uint64) error {
	p, err := bpm.FetchPage(rid.PageID)
	if err != nil {
		return err
	}
	if p.GetLSN() >= lsn {
		return bpm.UnpinPage(rid.PageID, false)
	}
	if err := page.NewSlottedPage(p).DeleteRecord(rid.SlotID); err != nil {
		bpm.UnpinPage(rid.PageID, false)
		return err
	}
	p.SetLSN(lsn)
	return bpm.UnpinPage(rid.PageID, true)
}

// RedoNewPage はページ追加のログを再適用する（新しいページの初期化と、元の末尾ページからのリンク）
func RedoNewPage(bpm *buffer.BufferPoolManager, prevPageID, newPageID page.PageID, lsn uint64) error {
	newP, err := bpm.FetchPage(newPageID)
	if err != nil {
		return err
	}
	dirty := false
	if newP.GetLSN() < lsn {
		page.NewSlottedPage(newP).Init()
		newP.SetLSN(lsn)
		dirty = true
	}
	if err := bpm.UnpinPage(newPageID, dirty); err != nil {
		return err
	}

	prevP, err := bpm.FetchPage(prevPageID)
	if err != nil {
		return err
	}
	dirty = false
	if prevP.GetLSN() < lsn {
		page.NewSlottedPage(prevP).SetNextPageID(newPageID)
		prevP.SetLSN(lsn)
		dirty = true
	}
	return bpm.UnpinPage(prevPageID, dirty)
}
