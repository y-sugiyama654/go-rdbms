// transaction/transaction.go
package transaction

import (
	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/storage/heap"
	"github.com/yourname/go-rdbms/transaction/log"
	"github.com/yourname/go-rdbms/transaction/txid"
)

// Status はトランザクションの状態
type Status int

const (
	StatusActive    Status = iota // 実行中
	StatusCommitted               // コミット済み
	StatusAborted                 // ロールバック済み
)

// WriteRecord はロールバックのために覚えておく書き込み操作（本シリーズではINSERTだけ）
type WriteRecord struct {
	Table  *catalog.TableInfo
	RID    heap.RID // 挿入したレコードの位置
	Key    int64    // 挿入した主キー（HasKeyがtrueのときだけ有効）
	HasKey bool
}

// Transaction は1つのトランザクション
// 1つのトランザクションを複数のゴルーチンから同時に使うことはない前提
type Transaction struct {
	id     txid.TxID
	status Status
	writes []WriteRecord
	logger *log.TxLogger
}

func (tx *Transaction) ID() txid.TxID  { return tx.id }
func (tx *Transaction) Status() Status { return tx.status }

// Logger はこのトランザクションのヒープ操作をWALに記録するLoggerを返す
func (tx *Transaction) Logger() heap.Logger { return tx.logger }

// AddWrite は書き込み操作を記録する（ロールバック時に逆順で取り消す）
func (tx *Transaction) AddWrite(w WriteRecord) {
	tx.writes = append(tx.writes, w)
}
