// storage/buffer/buffer_pool_manager.go
package buffer

import (
	"fmt"
	"sync"

	"github.com/yourname/go-rdbms/storage/disk"
	"github.com/yourname/go-rdbms/storage/page"
)

// frame はバッファプール内の1スロットを表す
type frame struct {
	page     page.Page
	pageID   page.PageID
	pinCount int
	isDirty  bool
}

// BufferPoolManager はページのメモリキャッシュを管理する
type BufferPoolManager struct {
	mu          sync.Mutex
	diskManager disk.DiskManager
	frames      []frame
	pageTable   map[page.PageID]FrameID
	freeList    []FrameID
	replacer    Replacer
}

// NewBufferPoolManager は指定サイズのフレームを持つBufferPoolManagerを返す
func NewBufferPoolManager(diskManager disk.DiskManager, poolSize int) *BufferPoolManager {
	freeList := make([]FrameID, poolSize)
	for i := range freeList {
		freeList[i] = FrameID(i)
	}

	return &BufferPoolManager{
		diskManager: diskManager,
		frames:      make([]frame, poolSize),
		pageTable:   make(map[page.PageID]FrameID),
		freeList:    freeList,
		replacer:    NewLRUReplacer(),
	}
}

// FetchPage はpageIDのページをバッファプール上で返す
// すでにキャッシュされていればそれを、なければディスクから読み込む
func (bpm *BufferPoolManager) FetchPage(pageID page.PageID) (*page.Page, error) {
	bpm.mu.Lock()
	defer bpm.mu.Unlock()

	if frameID, ok := bpm.pageTable[pageID]; ok {
		bpm.frames[frameID].pinCount++
		bpm.replacer.Pin(frameID)
		return &bpm.frames[frameID].page, nil
	}

	frameID, err := bpm.allocateFrame()
	if err != nil {
		return nil, err
	}

	f := &bpm.frames[frameID]
	if err := bpm.diskManager.ReadPage(pageID, &f.page); err != nil {
		// 読み込みに失敗したら、確保したフレームを空きリストに戻す
		bpm.freeList = append(bpm.freeList, frameID)
		return nil, fmt.Errorf("buffer: ページ %d の読み込みに失敗しました: %w", pageID, err)
	}

	f.pageID = pageID
	f.pinCount = 1
	f.isDirty = false
	bpm.pageTable[pageID] = frameID
	bpm.replacer.Pin(frameID)

	return &f.page, nil
}

// NewPage はディスク上に新しいページを割り当て、バッファプールに読み込んだ状態で返す
func (bpm *BufferPoolManager) NewPage() (page.PageID, *page.Page, error) {
	bpm.mu.Lock()
	defer bpm.mu.Unlock()

	// 先にフレームを確保する。ディスク上のページを先に割り当てると、
	// フレームが確保できなかったときに、どこからも使われないページが残ってしまう
	frameID, err := bpm.allocateFrame()
	if err != nil {
		return page.InvalidPageID, nil, err
	}

	pageID, err := bpm.diskManager.AllocatePage()
	if err != nil {
		bpm.freeList = append(bpm.freeList, frameID)
		return page.InvalidPageID, nil, fmt.Errorf("buffer: 新規ページの割り当てに失敗しました: %w", err)
	}

	f := &bpm.frames[frameID]
	f.page = page.Page{}
	f.page.SetPageID(pageID)
	f.pageID = pageID
	f.pinCount = 1
	f.isDirty = true
	bpm.pageTable[pageID] = frameID
	bpm.replacer.Pin(frameID)

	return pageID, &f.page, nil
}

// UnpinPage はページの利用終了を通知する
// isDirtyがtrueの場合、ページが変更されたものとしてマークする
func (bpm *BufferPoolManager) UnpinPage(pageID page.PageID, isDirty bool) error {
	bpm.mu.Lock()
	defer bpm.mu.Unlock()

	frameID, ok := bpm.pageTable[pageID]
	if !ok {
		return fmt.Errorf("buffer: ページ %d はバッファプールにありません", pageID)
	}

	f := &bpm.frames[frameID]
	if f.pinCount <= 0 {
		return fmt.Errorf("buffer: ページ %d は既にアンピンされています", pageID)
	}

	if isDirty {
		f.isDirty = true
	}
	f.pinCount--
	if f.pinCount == 0 {
		bpm.replacer.Unpin(frameID)
	}
	return nil
}

// FlushPage は指定ページをディスクに書き戻す
func (bpm *BufferPoolManager) FlushPage(pageID page.PageID) error {
	bpm.mu.Lock()
	defer bpm.mu.Unlock()
	return bpm.flushFrame(pageID)
}

func (bpm *BufferPoolManager) flushFrame(pageID page.PageID) error {
	frameID, ok := bpm.pageTable[pageID]
	if !ok {
		return fmt.Errorf("buffer: ページ %d はバッファプールにありません", pageID)
	}

	f := &bpm.frames[frameID]
	if !f.isDirty {
		return nil
	}
	if err := bpm.diskManager.WritePage(pageID, &f.page); err != nil {
		return fmt.Errorf("buffer: ページ %d の書き戻しに失敗しました: %w", pageID, err)
	}
	f.isDirty = false
	return nil
}

// allocateFrame は空きフレームを確保する
// 空きがなければreplacerに追い出しを依頼する
func (bpm *BufferPoolManager) allocateFrame() (FrameID, error) {
	if len(bpm.freeList) > 0 {
		frameID := bpm.freeList[len(bpm.freeList)-1]
		bpm.freeList = bpm.freeList[:len(bpm.freeList)-1]
		return frameID, nil
	}

	victimID, ok := bpm.replacer.Victim()
	if !ok {
		return 0, fmt.Errorf("buffer: 追い出し可能なページがありません（全ページがピン中です）")
	}

	victim := &bpm.frames[victimID]
	if victim.isDirty {
		if err := bpm.diskManager.WritePage(victim.pageID, &victim.page); err != nil {
			// 書き戻せなかったページは追い出さず、追い出し候補に戻しておく
			bpm.replacer.Unpin(victimID)
			return 0, fmt.Errorf("buffer: 追い出し対象ページ %d の書き戻しに失敗しました: %w", victim.pageID, err)
		}
	}
	delete(bpm.pageTable, victim.pageID)

	return victimID, nil
}
