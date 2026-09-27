// transaction/transaction_manager.go
package transaction

import (
	"errors"
	"fmt"
	"sync"

	"github.com/yourname/go-rdbms/storage/btree"
	"github.com/yourname/go-rdbms/transaction/lock"
	"github.com/yourname/go-rdbms/transaction/log"
	"github.com/yourname/go-rdbms/transaction/txid"
)

// TransactionManager はトランザクションの開始・コミット・ロールバックを管理する
type TransactionManager struct {
	mu            sync.Mutex
	cond          *sync.Cond // トランザクションの終了・チェックポイントの完了を知らせる
	nextID        txid.TxID
	active        map[txid.TxID]*Transaction
	lockMgr       *lock.LockManager
	logMgr        *log.LogManager
	checkpointing bool
}

func NewTransactionManager(lockMgr *lock.LockManager, logMgr *log.LogManager) *TransactionManager {
	tm := &TransactionManager{
		nextID:  1,
		active:  make(map[txid.TxID]*Transaction),
		lockMgr: lockMgr,
		logMgr:  logMgr,
	}
	tm.cond = sync.NewCond(&tm.mu)
	return tm
}

// Begin は新しいトランザクションを開始する
func (tm *TransactionManager) Begin() *Transaction {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for tm.checkpointing {
		tm.cond.Wait() // チェックポイント中は新しいトランザクションを始めない
	}
	tx := &Transaction{id: tm.nextID, status: StatusActive}
	tx.logger = log.NewTxLogger(tm.logMgr, tx.id)
	tm.nextID++
	tm.active[tx.id] = tx
	tm.logMgr.Append(&log.LogRecord{TxID: tx.id, Type: log.RecordBegin})
	return tx
}

// Commit はトランザクションをコミットする
func (tm *TransactionManager) Commit(tx *Transaction) error {
	if tx.status != StatusActive {
		return fmt.Errorf("transaction: TX %d は既に終了しています", tx.id)
	}
	// COMMITログをfsyncし終えた時点で、コミットが確定する（Durability）
	lsn := tm.logMgr.Append(&log.LogRecord{TxID: tx.id, Type: log.RecordCommit})
	// 何も書き込んでいないトランザクションは、fsyncを待たなくてよい。
	// COMMITログが失われても、取り消すべき変更がないので結果は変わらない
	if len(tx.writes) > 0 {
		if err := tm.logMgr.FlushTo(lsn); err != nil {
			return fmt.Errorf("transaction: COMMITログを永続化できません: %w", err)
		}
	}
	if err := tm.finish(tx, StatusCommitted); err != nil {
		return err
	}
	// 厳格2PL: ロックはトランザクションの終了時にまとめて解放する
	tm.lockMgr.ReleaseAll(tx.id)
	return nil
}

// Rollback はトランザクションの書き込みをすべて取り消す
func (tm *TransactionManager) Rollback(tx *Transaction) error {
	if tx.status != StatusActive {
		return fmt.Errorf("transaction: TX %d は既に終了しています", tx.id)
	}

	// 後に行った操作から順に取り消す
	for i := len(tx.writes) - 1; i >= 0; i-- {
		w := tx.writes[i]
		if w.HasKey {
			// インデックスへの登録前に失敗した場合はキーがないので、見つからなくてもよい
			if err := w.Table.Index.Delete(w.Key); err != nil && !errors.Is(err, btree.ErrKeyNotFound) {
				return err
			}
		}
		// 取り消し操作もWALに記録する（クラッシュ後のREDOで再現できるように）
		if err := w.Table.Heap.DeleteRecord(tx.logger, w.RID); err != nil {
			return err
		}
	}
	tm.logMgr.Append(&log.LogRecord{TxID: tx.id, Type: log.RecordAbort})
	if err := tm.finish(tx, StatusAborted); err != nil {
		return err
	}
	// 取り消しが終わるまではXロックを持ち続け、最後に解放する
	tm.lockMgr.ReleaseAll(tx.id)
	return nil
}

// finish はトランザクションを終了状態にして、実行中の一覧から外す
func (tm *TransactionManager) finish(tx *Transaction, status Status) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tx.status != StatusActive {
		return fmt.Errorf("transaction: TX %d は既に終了しています", tx.id)
	}
	tx.status = status
	delete(tm.active, tx.id)
	tm.cond.Broadcast()
	return nil
}

// Checkpoint は新しいトランザクションの開始を止め、実行中のトランザクションが
// すべて終わるのを待ってから fn（ページの書き出しとログの切り詰め）を実行する
func (tm *TransactionManager) Checkpoint(fn func() error) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.checkpointing = true
	for len(tm.active) > 0 {
		tm.cond.Wait()
	}
	err := fn()
	tm.checkpointing = false
	tm.cond.Broadcast()
	return err
}
