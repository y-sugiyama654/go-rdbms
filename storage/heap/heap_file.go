// storage/heap/heap_file.go
package heap

import (
	"errors"
	"fmt"
	"sync"

	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/page"
)

// Logger はヒープへの変更をWALに記録する（第12章）
// 各メソッドは書き込んだログレコードのLSNを返す
type Logger interface {
	LogInsert(rid RID, data []byte) uint64
	LogDelete(rid RID) uint64
	LogNewPage(prevPageID, newPageID page.PageID) uint64
}

// HeapFile は1つのテーブルのレコードを、連結されたスロット付きページの列として管理する
type HeapFile struct {
	mu          sync.Mutex
	bpm         *buffer.BufferPoolManager
	firstPageID page.PageID
	lastPageID  page.PageID
}

// NewHeapFile は先頭ページを1つ割り当てて、空のHeapFileを作る
func NewHeapFile(bpm *buffer.BufferPoolManager) (*HeapFile, error) {
	pageID, p, err := bpm.NewPage()
	if err != nil {
		return nil, fmt.Errorf("heap: 先頭ページの割り当てに失敗しました: %w", err)
	}
	page.NewSlottedPage(p).Init()
	if err := bpm.UnpinPage(pageID, true); err != nil {
		return nil, err
	}
	return &HeapFile{bpm: bpm, firstPageID: pageID, lastPageID: pageID}, nil
}

// OpenHeapFile は先頭ページIDから既存のHeapFileを開く
// ページのつながりをたどって末尾ページを見つけておく
func OpenHeapFile(bpm *buffer.BufferPoolManager, firstPageID page.PageID) (*HeapFile, error) {
	last := firstPageID
	for {
		p, err := bpm.FetchPage(last)
		if err != nil {
			return nil, fmt.Errorf("heap: ページ %d を読めません: %w", last, err)
		}
		next := page.NewSlottedPage(p).NextPageID()
		if err := bpm.UnpinPage(last, false); err != nil {
			return nil, err
		}
		if next == page.InvalidPageID {
			break
		}
		last = next
	}
	return &HeapFile{bpm: bpm, firstPageID: firstPageID, lastPageID: last}, nil
}

// FirstPageID は先頭ページのPageIDを返す
func (hf *HeapFile) FirstPageID() page.PageID {
	return hf.firstPageID
}

// InsertRecord はレコードを格納してRIDを返す
// lgがnilでなければ、変更をWALに記録してページLSNを更新する
func (hf *HeapFile) InsertRecord(lg Logger, data []byte) (RID, error) {
	if len(data) > page.MaxRecordSize {
		return RID{}, fmt.Errorf("heap: レコードが大きすぎます（%dバイト、上限%dバイト）",
			len(data), page.MaxRecordSize)
	}

	hf.mu.Lock()
	defer hf.mu.Unlock()

	// まず末尾ページに入れてみる
	lastID := hf.lastPageID
	p, err := hf.bpm.FetchPage(lastID)
	if err != nil {
		return RID{}, err
	}
	sp := page.NewSlottedPage(p)
	slotID, err := sp.InsertRecord(data)
	if err == nil {
		rid := RID{PageID: lastID, SlotID: slotID}
		if lg != nil {
			p.SetLSN(lg.LogInsert(rid, data))
		}
		return rid, hf.bpm.UnpinPage(lastID, true)
	}
	if !errors.Is(err, page.ErrNoSpace) {
		hf.bpm.UnpinPage(lastID, false)
		return RID{}, err
	}

	// 末尾ページが満杯なら、新しいページを確保して末尾につなぐ
	newID, newP, err := hf.bpm.NewPage()
	if err != nil {
		hf.bpm.UnpinPage(lastID, false)
		return RID{}, fmt.Errorf("heap: 新規ページの割り当てに失敗しました: %w", err)
	}
	newSP := page.NewSlottedPage(newP)
	newSP.Init()
	sp.SetNextPageID(newID)
	if lg != nil {
		// ページの追加は、2つのページ（元の末尾と新しいページ）にまたがる変更
		lsn := lg.LogNewPage(lastID, newID)
		p.SetLSN(lsn)
		newP.SetLSN(lsn)
	}
	if err := hf.bpm.UnpinPage(lastID, true); err != nil {
		return RID{}, err
	}
	hf.lastPageID = newID

	slotID, err = newSP.InsertRecord(data)
	if err != nil {
		hf.bpm.UnpinPage(newID, true)
		return RID{}, err
	}
	rid := RID{PageID: newID, SlotID: slotID}
	if lg != nil {
		newP.SetLSN(lg.LogInsert(rid, data))
	}
	return rid, hf.bpm.UnpinPage(newID, true)
}

// GetRecord はRIDのレコードを返す
func (hf *HeapFile) GetRecord(rid RID) ([]byte, error) {
	hf.mu.Lock()
	defer hf.mu.Unlock()

	p, err := hf.bpm.FetchPage(rid.PageID)
	if err != nil {
		return nil, err
	}
	defer hf.bpm.UnpinPage(rid.PageID, false)
	return page.NewSlottedPage(p).GetRecord(rid.SlotID)
}

// DeleteRecord はRIDのレコードを削除する
func (hf *HeapFile) DeleteRecord(lg Logger, rid RID) error {
	hf.mu.Lock()
	defer hf.mu.Unlock()
	return DeleteAt(hf.bpm, lg, rid)
}

// DeleteAt はRIDのレコードを削除する（HeapFileを経由せずに使えるようにしたもの）
func DeleteAt(bpm *buffer.BufferPoolManager, lg Logger, rid RID) error {
	p, err := bpm.FetchPage(rid.PageID)
	if err != nil {
		return err
	}
	if err := page.NewSlottedPage(p).DeleteRecord(rid.SlotID); err != nil {
		bpm.UnpinPage(rid.PageID, false)
		return err
	}
	if lg != nil {
		p.SetLSN(lg.LogDelete(rid))
	}
	return bpm.UnpinPage(rid.PageID, true)
}

// Scan は全レコードを先頭から順に返すスキャナを作る
func (hf *HeapFile) Scan() *HeapScanner {
	return &HeapScanner{hf: hf, pageID: hf.firstPageID}
}

// HeapScanner はHeapFileのレコードを順番にたどるイテレータ
type HeapScanner struct {
	hf     *HeapFile
	pageID page.PageID // 次に読むページ
	slotID uint16      // 次に読むスロット
}

// Next は次の有効なレコードを返す。最後まで読み終えたら ok=false を返す
func (s *HeapScanner) Next() (rid RID, data []byte, ok bool, err error) {
	s.hf.mu.Lock()
	defer s.hf.mu.Unlock()

	for s.pageID != page.InvalidPageID {
		p, err := s.hf.bpm.FetchPage(s.pageID)
		if err != nil {
			return RID{}, nil, false, err
		}
		sp := page.NewSlottedPage(p)

		for s.slotID < sp.NumSlots() {
			slotID := s.slotID
			s.slotID++
			data, err := sp.GetRecord(slotID)
			if errors.Is(err, page.ErrRecordDeleted) {
				continue // 削除済みスロットは読み飛ばす
			}
			if err != nil {
				s.hf.bpm.UnpinPage(s.pageID, false)
				return RID{}, nil, false, err
			}
			rid := RID{PageID: s.pageID, SlotID: slotID}
			return rid, data, true, s.hf.bpm.UnpinPage(s.pageID, false)
		}

		// このページを読み終えたら次のページへ
		next := sp.NextPageID()
		if err := s.hf.bpm.UnpinPage(s.pageID, false); err != nil {
			return RID{}, nil, false, err
		}
		s.pageID = next
		s.slotID = 0
	}
	return RID{}, nil, false, nil
}
