// transaction/log/tx_logger.go
package log

import (
	"github.com/yourname/go-rdbms/storage/heap"
	"github.com/yourname/go-rdbms/storage/page"
	"github.com/yourname/go-rdbms/transaction/txid"
)

// TxLogger は1つのトランザクションのヒープ操作をWALに記録する（heap.Loggerの実装）
type TxLogger struct {
	lm   *LogManager
	txID txid.TxID
}

var _ heap.Logger = (*TxLogger)(nil)

func NewTxLogger(lm *LogManager, txID txid.TxID) *TxLogger {
	return &TxLogger{lm: lm, txID: txID}
}

func (l *TxLogger) LogInsert(rid heap.RID, data []byte) uint64 {
	return uint64(l.lm.Append(&LogRecord{
		TxID: l.txID, Type: RecordInsert, PageID: rid.PageID, SlotID: rid.SlotID, Data: data,
	}))
}

func (l *TxLogger) LogDelete(rid heap.RID) uint64 {
	return uint64(l.lm.Append(&LogRecord{
		TxID: l.txID, Type: RecordDelete, PageID: rid.PageID, SlotID: rid.SlotID,
	}))
}

func (l *TxLogger) LogNewPage(prevPageID, newPageID page.PageID) uint64 {
	return uint64(l.lm.Append(&LogRecord{
		TxID: l.txID, Type: RecordNewPage, PageID: newPageID, PrevPageID: prevPageID,
	}))
}
