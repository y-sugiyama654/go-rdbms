// storage/btree/btree.go
package btree

import (
	"errors"
	"fmt"
	"sync"

	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/heap"
	"github.com/yourname/go-rdbms/storage/page"
)

var (
	// ErrDuplicateKey は既に存在するキーを挿入しようとしたことを表す
	ErrDuplicateKey = errors.New("btree: キーが重複しています")
	// ErrKeyNotFound は存在しないキーを削除しようとしたことを表す
	ErrKeyNotFound = errors.New("btree: キーが見つかりません")
)

// BPlusTree はキー（int64）からRIDを引くB+木インデックス
// キーの重複は許さない（主キー用のユニークインデックス）
type BPlusTree struct {
	mu         sync.RWMutex
	bpm        *buffer.BufferPoolManager
	rootPageID page.PageID // ルートのPageIDは作成後に変わらない
}

// NewBPlusTree は空の葉ノード1つをルートとする新しいB+木を作る
func NewBPlusTree(bpm *buffer.BufferPoolManager) (*BPlusTree, error) {
	rootID, p, err := bpm.NewPage()
	if err != nil {
		return nil, fmt.Errorf("btree: ルートページの割り当てに失敗しました: %w", err)
	}
	node{p}.initLeaf()
	if err := bpm.UnpinPage(rootID, true); err != nil {
		return nil, err
	}
	return &BPlusTree{bpm: bpm, rootPageID: rootID}, nil
}

// OpenBPlusTree はルートのPageIDから既存のB+木を開く
func OpenBPlusTree(bpm *buffer.BufferPoolManager, rootPageID page.PageID) *BPlusTree {
	return &BPlusTree{bpm: bpm, rootPageID: rootPageID}
}

// RootPageID はルートノードのPageIDを返す
func (t *BPlusTree) RootPageID() page.PageID {
	return t.rootPageID
}

// ---- 検索 ----

// Search はkeyに対応するRIDを返す。見つからなければ found=false
func (t *BPlusTree) Search(key int64) (rid heap.RID, found bool, err error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	leafID, err := t.findLeaf(key)
	if err != nil {
		return heap.RID{}, false, err
	}
	p, err := t.bpm.FetchPage(leafID)
	if err != nil {
		return heap.RID{}, false, err
	}
	defer t.bpm.UnpinPage(leafID, false)

	n := node{p}
	i := n.lowerBound(key)
	if i < n.numKeys() && n.key(i) == key {
		return n.rid(i), true, nil
	}
	return heap.RID{}, false, nil
}

// findLeaf はルートから内部ノードをたどり、keyが属する葉のPageIDを返す
func (t *BPlusTree) findLeaf(key int64) (page.PageID, error) {
	id := t.rootPageID
	for {
		p, err := t.bpm.FetchPage(id)
		if err != nil {
			return page.InvalidPageID, err
		}
		n := node{p}
		if n.isLeaf() {
			return id, t.bpm.UnpinPage(id, false)
		}
		next := n.child(n.childIndex(key))
		if err := t.bpm.UnpinPage(id, false); err != nil {
			return page.InvalidPageID, err
		}
		id = next
	}
}

// RangeScan は minKey 以上 maxKey 以下のキーに対応するRIDをキー順に返す
func (t *BPlusTree) RangeScan(minKey, maxKey int64) ([]heap.RID, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if minKey > maxKey {
		return nil, nil
	}
	leafID, err := t.findLeaf(minKey)
	if err != nil {
		return nil, err
	}

	var result []heap.RID
	for leafID != page.InvalidPageID {
		p, err := t.bpm.FetchPage(leafID)
		if err != nil {
			return nil, err
		}
		n := node{p}
		done := false
		for i := n.lowerBound(minKey); i < n.numKeys(); i++ {
			if n.key(i) > maxKey {
				done = true
				break
			}
			result = append(result, n.rid(i))
		}
		next := n.next()
		if err := t.bpm.UnpinPage(leafID, false); err != nil {
			return nil, err
		}
		if done {
			break
		}
		leafID = next // 右隣の葉へ
	}
	return result, nil
}

// ---- 挿入 ----

// splitResult は子ノードが分割されたときに親へ伝える情報
type splitResult struct {
	key   int64       // 親に追加する区切りキー
	right page.PageID // 分割で新しくできた右側のノード
}

// Insert はキーとRIDの組を挿入する
func (t *BPlusTree) Insert(key int64, rid heap.RID) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	split, err := t.insert(t.rootPageID, key, rid)
	if err != nil || split == nil {
		return err
	}
	// ルートが分割された場合は木を1段高くする
	return t.growRoot(split)
}

// insert はpageIDをルートとする部分木にエントリを挿入する
// このノードが分割された場合は、親に追加すべき区切りキーを返す
func (t *BPlusTree) insert(pageID page.PageID, key int64, rid heap.RID) (*splitResult, error) {
	p, err := t.bpm.FetchPage(pageID)
	if err != nil {
		return nil, err
	}
	n := node{p}

	if n.isLeaf() {
		i := n.lowerBound(key)
		if i < n.numKeys() && n.key(i) == key {
			t.bpm.UnpinPage(pageID, false)
			return nil, ErrDuplicateKey
		}
		if n.numKeys() < maxLeafKeys {
			n.insertLeafEntry(i, leafEntry{key, rid})
			return nil, t.bpm.UnpinPage(pageID, true)
		}
		split, err := t.splitLeaf(n, i, leafEntry{key, rid})
		t.bpm.UnpinPage(pageID, true)
		return split, err
	}

	// 内部ノード: 子をたどって再帰的に挿入する
	idx := n.childIndex(key)
	childID := n.child(idx)
	if err := t.bpm.UnpinPage(pageID, false); err != nil {
		return nil, err
	}
	childSplit, err := t.insert(childID, key, rid)
	if err != nil || childSplit == nil {
		return nil, err
	}

	// 子が分割されたので、区切りキーと新しい子をこのノードに追加する
	p, err = t.bpm.FetchPage(pageID)
	if err != nil {
		return nil, err
	}
	n = node{p}
	if n.numKeys() < maxInternalKeys {
		n.insertInternalEntry(idx, childSplit.key, childSplit.right)
		return nil, t.bpm.UnpinPage(pageID, true)
	}
	split, err := t.splitInternal(n, idx, childSplit.key, childSplit.right)
	t.bpm.UnpinPage(pageID, true)
	return split, err
}

// splitLeaf は満杯の葉に新しいエントリを加えたうえで、左右2つの葉に分ける
func (t *BPlusTree) splitLeaf(left node, pos int, e leafEntry) (*splitResult, error) {
	entries := left.leafEntries()
	entries = append(entries[:pos], append([]leafEntry{e}, entries[pos:]...)...)
	mid := len(entries) / 2

	rightID, rp, err := t.bpm.NewPage()
	if err != nil {
		return nil, err
	}
	right := node{rp}
	right.initLeaf()
	right.setLeafEntries(entries[mid:])
	right.setNext(left.next()) // 右の葉は、元の右隣を引き継ぐ
	left.setLeafEntries(entries[:mid])
	left.setNext(rightID)

	if err := t.bpm.UnpinPage(rightID, true); err != nil {
		return nil, err
	}
	// 右の葉の最小キーを区切りキーとして親にコピーする
	return &splitResult{key: entries[mid].key, right: rightID}, nil
}

// splitInternal は満杯の内部ノードに区切りキーを加えたうえで、左右2つに分ける
func (t *BPlusTree) splitInternal(left node, pos int, key int64, rightChild page.PageID) (*splitResult, error) {
	keys, children := left.internalContents()
	keys = append(keys[:pos], append([]int64{key}, keys[pos:]...)...)
	children = append(children[:pos+1], append([]page.PageID{rightChild}, children[pos+1:]...)...)
	mid := len(keys) / 2

	rightID, rp, err := t.bpm.NewPage()
	if err != nil {
		return nil, err
	}
	right := node{rp}
	right.initInternal(children[mid+1])
	right.setInternalContents(keys[mid+1:], children[mid+1:])
	left.setInternalContents(keys[:mid], children[:mid+1])

	if err := t.bpm.UnpinPage(rightID, true); err != nil {
		return nil, err
	}
	// 真ん中のキーは左右どちらにも残さず、親へ移動する
	return &splitResult{key: keys[mid], right: rightID}, nil
}

// growRoot はルートが分割されたときに木を1段高くする
// ルートの中身を新しいページへ移し、ルート自身は2つの子を持つ内部ノードに作り直す。
// こうするとルートのPageIDが変わらないので、カタログを書き換えずに済む
func (t *BPlusTree) growRoot(split *splitResult) error {
	rootPage, err := t.bpm.FetchPage(t.rootPageID)
	if err != nil {
		return err
	}
	leftID, leftPage, err := t.bpm.NewPage()
	if err != nil {
		t.bpm.UnpinPage(t.rootPageID, false)
		return err
	}
	*leftPage = *rootPage // ページ全体をコピーしてから、PageIDだけ付け替える
	leftPage.SetPageID(leftID)

	root := node{rootPage}
	root.initInternal(leftID)
	root.insertInternalEntry(0, split.key, split.right)

	if err := t.bpm.UnpinPage(leftID, true); err != nil {
		return err
	}
	return t.bpm.UnpinPage(t.rootPageID, true)
}

// ---- 削除 ----

// Delete はkeyのエントリを削除する
// 本シリーズではノードのマージ・再分配は行わない（葉が空になっても残す）
func (t *BPlusTree) Delete(key int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	leafID, err := t.findLeaf(key)
	if err != nil {
		return err
	}
	p, err := t.bpm.FetchPage(leafID)
	if err != nil {
		return err
	}
	n := node{p}
	i := n.lowerBound(key)
	if i >= n.numKeys() || n.key(i) != key {
		t.bpm.UnpinPage(leafID, false)
		return ErrKeyNotFound
	}
	n.removeLeafEntry(i)
	return t.bpm.UnpinPage(leafID, true)
}

// Reset はルートを空の葉ノードに戻して、木を空にする（第12章のインデックス再構築で使う）
// 古いノードのページは解放せずにそのまま残る（空きページの管理は実装していない）
func (t *BPlusTree) Reset() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	p, err := t.bpm.FetchPage(t.rootPageID)
	if err != nil {
		return err
	}
	node{p}.initLeaf()
	return t.bpm.UnpinPage(t.rootPageID, true)
}

// Height は木の高さ（ルートから葉までのノード数）を返す
func (t *BPlusTree) Height() (int, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	height := 1
	id := t.rootPageID
	for {
		p, err := t.bpm.FetchPage(id)
		if err != nil {
			return 0, err
		}
		n := node{p}
		leaf, next := n.isLeaf(), n.child(0)
		if err := t.bpm.UnpinPage(id, false); err != nil {
			return 0, err
		}
		if leaf {
			return height, nil
		}
		height++
		id = next
	}
}
