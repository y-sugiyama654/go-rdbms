// transaction/lock/lock_manager.go
package lock

import (
	"errors"
	"sync"

	"github.com/yourname/go-rdbms/transaction/txid"
)

// Mode はロックの種類
type Mode int

const (
	Shared    Mode = iota // Sロック（読み取り用）
	Exclusive             // Xロック（書き込み用）
)

func (m Mode) String() string {
	if m == Shared {
		return "S"
	}
	return "X"
}

// ErrDeadlock はデッドロックを検出し、このロック要求を諦めたことを表す
// 受け取った側はトランザクションをロールバックしなければならない
var ErrDeadlock = errors.New("lock: デッドロックを検出しました")

// compatible は2つのロックが同時に成立するかを返す（S同士だけが両立する）
func compatible(a, b Mode) bool {
	return a == Shared && b == Shared
}

// resource は1つの資源（本シリーズではテーブル）のロック状態
type resource struct {
	holders map[txid.TxID]Mode // ロックを持っているトランザクションとそのモード
}

// waitInfo はロック待ちをしているトランザクションが、何を待っているか
type waitInfo struct {
	resource string
	mode     Mode
}

// LockManager はロックの付与・解放と、デッドロックの検出を行う
type LockManager struct {
	mu        sync.Mutex
	cond      *sync.Cond // ロックが解放されたことを待機中のゴルーチンに知らせる
	resources map[string]*resource
	waiting   map[txid.TxID]waitInfo
}

func NewLockManager() *LockManager {
	lm := &LockManager{
		resources: make(map[string]*resource),
		waiting:   make(map[txid.TxID]waitInfo),
	}
	lm.cond = sync.NewCond(&lm.mu)
	return lm
}

// Lock はtxに資源nameのロックを与える。すぐに与えられなければ、与えられるまで待つ
// 待っている間にデッドロックになった場合は ErrDeadlock を返す
func (lm *LockManager) Lock(tx txid.TxID, name string, mode Mode) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for {
		// 眠っている間にReleaseAllで資源が片付けられていることがあるので、毎回引き直す
		r, ok := lm.resources[name]
		if !ok {
			r = &resource{holders: make(map[txid.TxID]Mode)}
			lm.resources[name] = r
		}

		// 既に同じか強いロックを持っていれば何もしない
		if held, ok := r.holders[tx]; ok && (held == Exclusive || mode == Shared) {
			delete(lm.waiting, tx)
			return nil
		}
		if lm.grantable(r, tx, mode) {
			delete(lm.waiting, tx)
			r.holders[tx] = mode // SからXへのアップグレードの場合は上書きになる
			return nil
		}

		lm.waiting[tx] = waitInfo{resource: name, mode: mode}
		if lm.hasCycle(tx) {
			delete(lm.waiting, tx)
			return ErrDeadlock
		}
		lm.cond.Wait() // 誰かがロックを解放するまで眠る（待機中はlm.muを手放す）
	}
}

// grantable はtxにmodeのロックを今すぐ与えられるかを返す
func (lm *LockManager) grantable(r *resource, tx txid.TxID, mode Mode) bool {
	for holder, held := range r.holders {
		if holder == tx {
			continue // 自分が持っているSロックは、Xへのアップグレードを妨げない
		}
		if !compatible(held, mode) {
			return false
		}
	}
	return true
}

// ReleaseAll はtxが持つロックをすべて解放する（コミット・ロールバック時に呼ぶ）
func (lm *LockManager) ReleaseAll(tx txid.TxID) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for name, r := range lm.resources {
		delete(r.holders, tx)
		if len(r.holders) == 0 {
			delete(lm.resources, name)
		}
	}
	delete(lm.waiting, tx)
	lm.cond.Broadcast() // 待っているゴルーチンをすべて起こし、ロックを取れるか確かめさせる
}

// waitsFor はtxがロック解放を待っている相手（待機グラフの辺の行き先）を返す
func (lm *LockManager) waitsFor(tx txid.TxID) []txid.TxID {
	w, ok := lm.waiting[tx]
	if !ok {
		return nil
	}
	r, ok := lm.resources[w.resource]
	if !ok {
		return nil // 資源が片付けられた = 待っている相手はもういない
	}
	var result []txid.TxID
	for holder, held := range r.holders {
		if holder != tx && !compatible(held, w.mode) {
			result = append(result, holder)
		}
	}
	return result
}

// hasCycle は待機グラフをstartからたどり、startに戻ってくる閉路があるかを調べる
// グラフは現在のロック状態から毎回作り直すので、古い辺が残ることはない
func (lm *LockManager) hasCycle(start txid.TxID) bool {
	visited := map[txid.TxID]bool{}
	var visit func(tx txid.TxID) bool
	visit = func(tx txid.TxID) bool {
		for _, next := range lm.waitsFor(tx) {
			if next == start {
				return true
			}
			if !visited[next] {
				visited[next] = true
				if visit(next) {
					return true
				}
			}
		}
		return false
	}
	return visit(start)
}
