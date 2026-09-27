// transaction/transaction_manager.go
package transaction

import (
	"errors"
	"fmt"
	"sync"

	"github.com/yourname/go-rdbms/storage/btree"
	"github.com/yourname/go-rdbms/transaction/txid"
)

// TransactionManager はトランザクションの開始・コミット・ロールバックを管理する
type TransactionManager struct {
	mu     sync.Mutex
	nextID txid.TxID
	active map[txid.TxID]*Transaction
}

func NewTransactionManager() *TransactionManager {
	return &TransactionManager{
		nextID: 1,
		active: make(map[txid.TxID]*Transaction),
	}
}

// Begin は新しいトランザクションを開始する
func (tm *TransactionManager) Begin() *Transaction {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tx := &Transaction{id: tm.nextID, status: StatusActive}
	tm.nextID++
	tm.active[tx.id] = tx
	return tx
}

// Commit はトランザクションをコミットする
func (tm *TransactionManager) Commit(tx *Transaction) error {
	if err := tm.finish(tx, StatusCommitted); err != nil {
		return err
	}
	// 第12章でここにCOMMITログの書き込みとfsyncが入る
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
		if err := w.Table.Heap.DeleteRecord(w.RID); err != nil {
			return err
		}
	}
	return tm.finish(tx, StatusAborted)
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
	return nil
}
