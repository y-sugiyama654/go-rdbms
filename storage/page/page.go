// storage/page/page.go
package page

import "encoding/binary"

const PageSize = 4096

// Page はディスクI/Oの最小単位（4KBの固定長バイト配列）
type Page [PageSize]byte

// PageID はページを一意に識別するID（0始まり）
type PageID uint32

// InvalidPageID は無効なページIDを表す定数
const InvalidPageID PageID = ^PageID(0) // 0xFFFFFFFF

type PageType uint32

const (
	PageTypeUnknown  PageType = 0
	PageTypeHeap     PageType = 1 // テーブルデータ
	PageTypeBTree    PageType = 2 // B+木ノード
	PageTypeOverflow PageType = 3 // 大きなデータの続き
)

const HeaderSize = 16

// ヘッダのオフセット定数
const (
	offsetPageID    = 0
	offsetPageType  = 4
	offsetFreeSpace = 8
	offsetNumSlots  = 12
)

func (p *Page) GetPageID() PageID {
	return PageID(binary.BigEndian.Uint32(p[offsetPageID : offsetPageID+4]))
}

func (p *Page) SetPageID(id PageID) {
	binary.BigEndian.PutUint32(p[offsetPageID:offsetPageID+4], uint32(id))
}

func (p *Page) GetPageType() PageType {
	return PageType(binary.BigEndian.Uint32(p[offsetPageType : offsetPageType+4]))
}

func (p *Page) SetPageType(t PageType) {
	binary.BigEndian.PutUint32(p[offsetPageType:offsetPageType+4], uint32(t))
}

func (p *Page) GetFreeSpaceOffset() uint32 {
	return binary.BigEndian.Uint32(p[offsetFreeSpace : offsetFreeSpace+4])
}

func (p *Page) SetFreeSpaceOffset(offset uint32) {
	binary.BigEndian.PutUint32(p[offsetFreeSpace:offsetFreeSpace+4], offset)
}

func (p *Page) GetNumSlots() uint32 {
	return binary.BigEndian.Uint32(p[offsetNumSlots : offsetNumSlots+4])
}

func (p *Page) SetNumSlots(n uint32) {
	binary.BigEndian.PutUint32(p[offsetNumSlots:offsetNumSlots+4], n)
}

// Data はヘッダを除いたデータ領域のスライスを返す
func (p *Page) Data() []byte {
	return p[HeaderSize:]
}
