// storage/buffer/lru_replacer.go
package buffer

import (
	"container/list"
	"sync"
)

// LRUReplacer はLeast Recently Usedポリシーで追い出し対象を選ぶReplacer実装
type LRUReplacer struct {
	mu       sync.Mutex
	list     *list.List // 先頭=最も長く使われていない, 末尾=最も最近使われた
	elements map[FrameID]*list.Element
}

// NewLRUReplacer は空のLRUReplacerを返す
func NewLRUReplacer() *LRUReplacer {
	return &LRUReplacer{
		list:     list.New(),
		elements: make(map[FrameID]*list.Element),
	}
}

// Victim は最も長く使われていないフレームを取り出す
func (r *LRUReplacer) Victim() (FrameID, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	front := r.list.Front()
	if front == nil {
		return 0, false
	}
	id := front.Value.(FrameID)
	r.list.Remove(front)
	delete(r.elements, id)
	return id, true
}

// Pin はフレームを追い出し候補から除外する
func (r *LRUReplacer) Pin(id FrameID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if elem, ok := r.elements[id]; ok {
		r.list.Remove(elem)
		delete(r.elements, id)
	}
}

// Unpin はフレームを追い出し候補（リスト末尾）に加える
func (r *LRUReplacer) Unpin(id FrameID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.elements[id]; ok {
		return // 既に追い出し候補に入っている
	}
	r.elements[id] = r.list.PushBack(id)
}

// Size は現在追い出し候補になっているフレーム数を返す
func (r *LRUReplacer) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.list.Len()
}
