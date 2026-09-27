// transaction/log/recovery.go
package log

import (
	"errors"
	"fmt"

	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/heap"
	"github.com/yourname/go-rdbms/storage/page"
	"github.com/yourname/go-rdbms/transaction/txid"
)

// RecoveryManager は起動時にWALを読み、ヒープを一貫した状態に戻す
type RecoveryManager struct {
	bpm *buffer.BufferPoolManager
	lm  *LogManager
}

func NewRecoveryManager(bpm *buffer.BufferPoolManager, lm *LogManager) *RecoveryManager {
	return &RecoveryManager{bpm: bpm, lm: lm}
}

// Recover はリカバリを行う。前回が正常終了でなかった
// （ログにCHECKPOINT以外のレコードが残っていた）場合は true を返す
func (rm *RecoveryManager) Recover() (bool, error) {
	records, err := rm.lm.ReadAll()
	if err != nil {
		return false, err
	}

	// 1. 分析: 最後まで終わらなかったトランザクション（loser）を洗い出す
	losers := map[txid.TxID]bool{}
	unclean := false
	for _, rec := range records {
		switch rec.Type {
		case RecordCheckpoint:
			continue
		case RecordCommit, RecordAbort:
			delete(losers, rec.TxID)
		default:
			losers[rec.TxID] = true
		}
		unclean = true
	}
	if !unclean {
		return false, nil
	}

	// 2. REDO: コミットしたかどうかに関係なく、すべての変更を先頭から再適用し、
	//    クラッシュ直前のバッファプールの状態を再現する
	for _, rec := range records {
		if err := rm.redo(rec); err != nil {
			return true, fmt.Errorf("recovery: LSN %d のREDOに失敗しました: %w", rec.LSN, err)
		}
	}

	// 3. UNDO: loserが挿入したレコードを、後ろから順に削除する。
	//    削除もログに記録するので、リカバリ中に再びクラッシュしてもやり直せる
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		if rec.Type != RecordInsert || !losers[rec.TxID] {
			continue
		}
		rid := heap.RID{PageID: rec.PageID, SlotID: rec.SlotID}
		err := heap.DeleteAt(rm.bpm, NewTxLogger(rm.lm, rec.TxID), rid)
		if errors.Is(err, page.ErrRecordDeleted) {
			continue // クラッシュ前のロールバックで既に取り消されていた
		}
		if err != nil {
			return true, fmt.Errorf("recovery: LSN %d のUNDOに失敗しました: %w", rec.LSN, err)
		}
	}
	for tx := range losers {
		rm.lm.Append(&LogRecord{TxID: tx, Type: RecordAbort})
	}
	return true, rm.lm.Flush()
}

func (rm *RecoveryManager) redo(rec *LogRecord) error {
	lsn := uint64(rec.LSN)
	switch rec.Type {
	case RecordInsert:
		return heap.RedoInsert(rm.bpm, heap.RID{PageID: rec.PageID, SlotID: rec.SlotID}, rec.Data, lsn)
	case RecordDelete:
		return heap.RedoDelete(rm.bpm, heap.RID{PageID: rec.PageID, SlotID: rec.SlotID}, lsn)
	case RecordNewPage:
		return heap.RedoNewPage(rm.bpm, rec.PrevPageID, rec.PageID, lsn)
	}
	return nil // BEGIN / COMMIT / ABORT / CHECKPOINT はページを変更しない
}

// Checkpoint はダーティページをすべてデータファイルに書き出してから、ログを空にする
// 実行中のトランザクションがない状態で呼ぶこと（TransactionManager.Checkpoint 経由で呼ぶ）
func Checkpoint(bpm *buffer.BufferPoolManager, lm *LogManager) error {
	if err := bpm.FlushAllPages(); err != nil {
		return err
	}
	return lm.Truncate()
}
