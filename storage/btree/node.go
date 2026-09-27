// storage/btree/node.go
package btree

import (
	"encoding/binary"

	"github.com/yourname/go-rdbms/storage/heap"
	"github.com/yourname/go-rdbms/storage/page"
)

// ノードのレイアウト
//
//	[0:16]  共通ページヘッダ（ページ種別 = PageTypeBTree）
//	[16:24] ページLSN（第12章のために予約。第5章のヒープページと同じ位置）
//	[24]    ノード種別（1 = 葉、0 = 内部）
//	[26:28] キーの数
//	[28:32] 葉: 右隣の葉のPageID / 内部: 最も左の子のPageID
//	[32:]   エントリの配列
//
//	葉のエントリ   : キー(8B) + RID.PageID(4B) + RID.SlotID(2B) = 14B
//	内部のエントリ : キー(8B) + 右側の子のPageID(4B)          = 12B
const (
	offsetIsLeaf   = 24
	offsetNumKeys  = 26
	offsetLink     = 28
	nodeHeaderSize = 32

	leafEntrySize     = 14
	internalEntrySize = 12

	maxLeafKeys     = (page.PageSize - nodeHeaderSize) / leafEntrySize     // 290
	maxInternalKeys = (page.PageSize - nodeHeaderSize) / internalEntrySize // 338
)

// leafEntry は葉ノードの1エントリ
type leafEntry struct {
	key int64
	rid heap.RID
}

// node はページをB+木のノードとして読み書きするためのラッパー
type node struct {
	p *page.Page
}

func (n node) initLeaf() {
	n.p.SetPageType(page.PageTypeBTree)
	n.p[offsetIsLeaf] = 1
	n.setNumKeys(0)
	n.setLink(page.InvalidPageID)
}

func (n node) initInternal(leftmostChild page.PageID) {
	n.p.SetPageType(page.PageTypeBTree)
	n.p[offsetIsLeaf] = 0
	n.setNumKeys(0)
	n.setLink(leftmostChild)
}

func (n node) isLeaf() bool { return n.p[offsetIsLeaf] == 1 }

func (n node) numKeys() int {
	return int(binary.BigEndian.Uint16(n.p[offsetNumKeys:]))
}

func (n node) setNumKeys(k int) {
	binary.BigEndian.PutUint16(n.p[offsetNumKeys:], uint16(k))
}

func (n node) link() page.PageID {
	return page.PageID(binary.BigEndian.Uint32(n.p[offsetLink:]))
}

func (n node) setLink(id page.PageID) {
	binary.BigEndian.PutUint32(n.p[offsetLink:], uint32(id))
}

func (n node) entrySize() int {
	if n.isLeaf() {
		return leafEntrySize
	}
	return internalEntrySize
}

func (n node) entryPos(i int) int {
	return nodeHeaderSize + i*n.entrySize()
}

// key はi番目のキーを返す（葉・内部共通）
func (n node) key(i int) int64 {
	return int64(binary.BigEndian.Uint64(n.p[n.entryPos(i):]))
}

// ---- 葉ノード ----

// next は右隣の葉のPageIDを返す
func (n node) next() page.PageID { return n.link() }

func (n node) setNext(id page.PageID) { n.setLink(id) }

func (n node) rid(i int) heap.RID {
	pos := n.entryPos(i) + 8
	return heap.RID{
		PageID: page.PageID(binary.BigEndian.Uint32(n.p[pos:])),
		SlotID: binary.BigEndian.Uint16(n.p[pos+4:]),
	}
}

func (n node) setLeafEntry(i int, e leafEntry) {
	pos := n.entryPos(i)
	binary.BigEndian.PutUint64(n.p[pos:], uint64(e.key))
	binary.BigEndian.PutUint32(n.p[pos+8:], uint32(e.rid.PageID))
	binary.BigEndian.PutUint16(n.p[pos+12:], e.rid.SlotID)
}

func (n node) leafEntries() []leafEntry {
	entries := make([]leafEntry, n.numKeys())
	for i := range entries {
		entries[i] = leafEntry{key: n.key(i), rid: n.rid(i)}
	}
	return entries
}

func (n node) setLeafEntries(entries []leafEntry) {
	for i, e := range entries {
		n.setLeafEntry(i, e)
	}
	n.setNumKeys(len(entries))
}

// insertLeafEntry はi番目にエントリを挿入する（i以降は1つ後ろへずらす）
func (n node) insertLeafEntry(i int, e leafEntry) {
	k := n.numKeys()
	copy(n.p[n.entryPos(i+1):n.entryPos(k+1)], n.p[n.entryPos(i):n.entryPos(k)])
	n.setLeafEntry(i, e)
	n.setNumKeys(k + 1)
}

// removeLeafEntry はi番目のエントリを取り除く（i+1以降を1つ前へ詰める）
func (n node) removeLeafEntry(i int) {
	k := n.numKeys()
	copy(n.p[n.entryPos(i):n.entryPos(k-1)], n.p[n.entryPos(i+1):n.entryPos(k)])
	n.setNumKeys(k - 1)
}

// lowerBound はkey以上となる最初のエントリ位置を返す（二分探索）
func (n node) lowerBound(key int64) int {
	lo, hi := 0, n.numKeys()
	for lo < hi {
		mid := (lo + hi) / 2
		if n.key(mid) < key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// ---- 内部ノード ----
// キーがk個ある内部ノードは、子をk+1個持つ。
// child(0) はヘッダのlink、child(i+1) はi番目のエントリに入っている。

func (n node) child(i int) page.PageID {
	if i == 0 {
		return n.link()
	}
	pos := n.entryPos(i-1) + 8
	return page.PageID(binary.BigEndian.Uint32(n.p[pos:]))
}

// setInternalEntry はi番目のキーと、その右側の子（child(i+1)）を書き込む
func (n node) setInternalEntry(i int, key int64, rightChild page.PageID) {
	pos := n.entryPos(i)
	binary.BigEndian.PutUint64(n.p[pos:], uint64(key))
	binary.BigEndian.PutUint32(n.p[pos+8:], uint32(rightChild))
}

// insertInternalEntry はi番目にキーと右側の子を挿入する
func (n node) insertInternalEntry(i int, key int64, rightChild page.PageID) {
	k := n.numKeys()
	copy(n.p[n.entryPos(i+1):n.entryPos(k+1)], n.p[n.entryPos(i):n.entryPos(k)])
	n.setInternalEntry(i, key, rightChild)
	n.setNumKeys(k + 1)
}

// internalContents はキーの一覧と子の一覧（キー数+1個）を返す
func (n node) internalContents() ([]int64, []page.PageID) {
	k := n.numKeys()
	keys := make([]int64, k)
	children := make([]page.PageID, k+1)
	children[0] = n.child(0)
	for i := 0; i < k; i++ {
		keys[i] = n.key(i)
		children[i+1] = n.child(i + 1)
	}
	return keys, children
}

func (n node) setInternalContents(keys []int64, children []page.PageID) {
	n.setLink(children[0])
	for i, key := range keys {
		n.setInternalEntry(i, key, children[i+1])
	}
	n.setNumKeys(len(keys))
}

// childIndex はkeyをたどるべき子の位置を返す
// 「key以下のキーの個数」が、そのまま子の位置になる
func (n node) childIndex(key int64) int {
	lo, hi := 0, n.numKeys()
	for lo < hi {
		mid := (lo + hi) / 2
		if n.key(mid) <= key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}
