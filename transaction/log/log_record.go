// transaction/log/log_record.go
package log

import (
	"encoding/binary"
	"fmt"

	"github.com/yourname/go-rdbms/storage/page"
	"github.com/yourname/go-rdbms/transaction/txid"
)

// LSN (Log Sequence Number) はログレコードに振る、1から単調増加する番号
type LSN uint64

// RecordType はログレコードの種類
type RecordType uint8

const (
	RecordBegin      RecordType = iota + 1 // トランザクション開始
	RecordCommit                           // コミット
	RecordAbort                            // ロールバック完了
	RecordInsert                           // レコードの挿入（REDO・UNDO用）
	RecordDelete                           // レコードの削除（REDO用。ロールバックの取り消し操作で出る）
	RecordNewPage                          // ヒープへのページ追加（REDO用）
	RecordCheckpoint                       // チェックポイント
)

func (t RecordType) String() string {
	names := map[RecordType]string{
		RecordBegin: "BEGIN", RecordCommit: "COMMIT", RecordAbort: "ABORT",
		RecordInsert: "INSERT", RecordDelete: "DELETE", RecordNewPage: "NEWPAGE",
		RecordCheckpoint: "CHECKPOINT",
	}
	return names[t]
}

// LogRecord は1件のログレコード
type LogRecord struct {
	LSN        LSN
	TxID       txid.TxID
	Type       RecordType
	PageID     page.PageID // INSERT/DELETE: 対象のページ, NEWPAGE: 追加したページ
	SlotID     uint16      // INSERT/DELETE: 対象のスロット
	PrevPageID page.PageID // NEWPAGE: 新しいページをつないだ元のページ
	Data       []byte      // INSERT: 挿入したレコード
}

// 固定長部分: LSN(8) + TxID(4) + Type(1) + PageID(4) + SlotID(2) + PrevPageID(4) + Dataの長さ(4)
const recordHeaderSize = 27

// encode はログレコードをバイト列にする
func (r *LogRecord) encode() []byte {
	buf := make([]byte, recordHeaderSize, recordHeaderSize+len(r.Data))
	binary.BigEndian.PutUint64(buf[0:], uint64(r.LSN))
	binary.BigEndian.PutUint32(buf[8:], uint32(r.TxID))
	buf[12] = byte(r.Type)
	binary.BigEndian.PutUint32(buf[13:], uint32(r.PageID))
	binary.BigEndian.PutUint16(buf[17:], r.SlotID)
	binary.BigEndian.PutUint32(buf[19:], uint32(r.PrevPageID))
	binary.BigEndian.PutUint32(buf[23:], uint32(len(r.Data)))
	return append(buf, r.Data...)
}

// decodeRecord はバイト列をログレコードに戻す
func decodeRecord(buf []byte) (*LogRecord, error) {
	if len(buf) < recordHeaderSize {
		return nil, fmt.Errorf("log: レコードが短すぎます")
	}
	dataLen := int(binary.BigEndian.Uint32(buf[23:]))
	if len(buf) != recordHeaderSize+dataLen {
		return nil, fmt.Errorf("log: レコードの長さが不正です")
	}
	return &LogRecord{
		LSN:        LSN(binary.BigEndian.Uint64(buf[0:])),
		TxID:       txid.TxID(binary.BigEndian.Uint32(buf[8:])),
		Type:       RecordType(buf[12]),
		PageID:     page.PageID(binary.BigEndian.Uint32(buf[13:])),
		SlotID:     binary.BigEndian.Uint16(buf[17:]),
		PrevPageID: page.PageID(binary.BigEndian.Uint32(buf[19:])),
		Data:       append([]byte(nil), buf[recordHeaderSize:]...),
	}, nil
}
