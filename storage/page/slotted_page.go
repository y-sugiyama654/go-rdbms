// storage/page/slotted_page.go
package page

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// スロット付きページのレイアウト
//
//	[0:16]  共通ページヘッダ（第3章）
//	[16:24] ページLSN（第12章のWALで使うため予約）
//	[24:28] 次のヒープページのPageID
//	[28:]   スロット配列（1スロット4バイト、先頭から後ろへ伸びる）
//	  ...   空き領域
//	[..:4096] レコードデータ（末尾から前へ伸びる）
const (
	offsetNextPageID  = 24
	slottedHeaderSize = 28
	slotSize          = 4 // offset(2B) + length(2B)

	// MaxRecordSize は1ページに格納できるレコードの最大サイズ
	MaxRecordSize = PageSize - slottedHeaderSize - slotSize
)

var (
	// ErrNoSpace はページに空き領域が足りないことを表す
	ErrNoSpace = errors.New("slotted_page: ページに空き領域がありません")
	// ErrRecordDeleted は削除済みのスロットを参照したことを表す
	ErrRecordDeleted = errors.New("slotted_page: レコードは削除済みです")
)

// SlottedPage はPageをスロット付きページとして操作するためのラッパー
type SlottedPage struct {
	p *Page
}

// NewSlottedPage はpをスロット付きページとして扱うSlottedPageを返す
func NewSlottedPage(p *Page) *SlottedPage {
	return &SlottedPage{p: p}
}

// Init はページを空のスロット付きページとして初期化する
func (sp *SlottedPage) Init() {
	sp.p.SetPageType(PageTypeHeap)
	sp.p.SetNumSlots(0)
	sp.p.SetFreeSpaceOffset(PageSize) // レコード領域の先頭 = ページ末尾
	sp.SetNextPageID(InvalidPageID)
}

// NumSlots はスロット数（削除済みを含む）を返す
func (sp *SlottedPage) NumSlots() uint16 {
	return uint16(sp.p.GetNumSlots())
}

// FreeSpace はスロット配列とレコード領域の間の空きバイト数を返す
func (sp *SlottedPage) FreeSpace() int {
	slotArrayEnd := slottedHeaderSize + int(sp.NumSlots())*slotSize
	return int(sp.p.GetFreeSpaceOffset()) - slotArrayEnd
}

// InsertRecord はレコードを格納し、割り当てたスロット番号を返す
func (sp *SlottedPage) InsertRecord(data []byte) (uint16, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("slotted_page: 空のレコードは格納できません")
	}
	// レコード本体に加えて、新しいスロット1つ分の領域が必要
	if sp.FreeSpace() < len(data)+slotSize {
		return 0, ErrNoSpace
	}

	// レコードをレコード領域の先頭（末尾側から詰める）に書く
	offset := int(sp.p.GetFreeSpaceOffset()) - len(data)
	copy(sp.p[offset:], data)
	sp.p.SetFreeSpaceOffset(uint32(offset))

	// スロット配列の末尾にスロットを追加する
	slotID := sp.NumSlots()
	sp.setSlot(slotID, uint16(offset), uint16(len(data)))
	sp.p.SetNumSlots(uint32(slotID) + 1)
	return slotID, nil
}

// GetRecord はスロット番号のレコードのコピーを返す
func (sp *SlottedPage) GetRecord(slotID uint16) ([]byte, error) {
	if slotID >= sp.NumSlots() {
		return nil, fmt.Errorf("slotted_page: スロット %d は範囲外です", slotID)
	}
	offset, length := sp.getSlot(slotID)
	if length == 0 {
		return nil, ErrRecordDeleted
	}
	data := make([]byte, length)
	copy(data, sp.p[offset:int(offset)+int(length)])
	return data, nil
}

// DeleteRecord はスロットを削除済みにする
// レコード本体の領域は詰め直さず、スロット番号も再利用しない
func (sp *SlottedPage) DeleteRecord(slotID uint16) error {
	if slotID >= sp.NumSlots() {
		return fmt.Errorf("slotted_page: スロット %d は範囲外です", slotID)
	}
	if _, length := sp.getSlot(slotID); length == 0 {
		return ErrRecordDeleted
	}
	sp.setSlot(slotID, 0, 0) // length=0 が削除済みの印
	return nil
}

// NextPageID は次のヒープページのPageIDを返す
func (sp *SlottedPage) NextPageID() PageID {
	return PageID(binary.BigEndian.Uint32(sp.p[offsetNextPageID:]))
}

// SetNextPageID は次のヒープページのPageIDを設定する
func (sp *SlottedPage) SetNextPageID(id PageID) {
	binary.BigEndian.PutUint32(sp.p[offsetNextPageID:], uint32(id))
}

func (sp *SlottedPage) slotPos(slotID uint16) int {
	return slottedHeaderSize + int(slotID)*slotSize
}

func (sp *SlottedPage) getSlot(slotID uint16) (offset, length uint16) {
	pos := sp.slotPos(slotID)
	offset = binary.BigEndian.Uint16(sp.p[pos:])
	length = binary.BigEndian.Uint16(sp.p[pos+2:])
	return offset, length
}

func (sp *SlottedPage) setSlot(slotID uint16, offset, length uint16) {
	pos := sp.slotPos(slotID)
	binary.BigEndian.PutUint16(sp.p[pos:], offset)
	binary.BigEndian.PutUint16(sp.p[pos+2:], length)
}
