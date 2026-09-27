// transaction/log/log_manager_test.go
package log_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yourname/go-rdbms/transaction/log"
)

func TestLogManager_AppendFlushRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.log")
	lm, err := log.NewLogManager(path)
	if err != nil {
		t.Fatal(err)
	}

	lsn1 := lm.Append(&log.LogRecord{TxID: 1, Type: log.RecordBegin})
	lsn2 := lm.Append(&log.LogRecord{TxID: 1, Type: log.RecordInsert, PageID: 3, SlotID: 7, Data: []byte("hello")})
	lsn3 := lm.Append(&log.LogRecord{TxID: 1, Type: log.RecordCommit})
	if lsn1 != 1 || lsn2 != 2 || lsn3 != 3 {
		t.Fatalf("LSNは1から順に振られるはず: %d, %d, %d", lsn1, lsn2, lsn3)
	}

	// Append しただけではファイルは空のまま
	if info, _ := os.Stat(path); info.Size() != 0 {
		t.Errorf("Append直後にファイルへ書かれている: %dバイト", info.Size())
	}
	if err := lm.FlushTo(lsn3); err != nil {
		t.Fatal(err)
	}
	lm.Close()

	// 開き直すと、レコードが読めて、LSNは続きから振られる
	lm2, err := log.NewLogManager(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lm2.Close()
	records, err := lm2.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("レコード数: want 3, got %d", len(records))
	}
	if r := records[1]; r.Type != log.RecordInsert || r.PageID != 3 || r.SlotID != 7 || string(r.Data) != "hello" {
		t.Errorf("INSERTレコードの中身が不正: %+v", r)
	}
	if lsn := lm2.Append(&log.LogRecord{TxID: 2, Type: log.RecordBegin}); lsn != 4 {
		t.Errorf("再オープン後のLSNは4から: got %d", lsn)
	}
}

func TestLogManager_TornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.log")
	lm, _ := log.NewLogManager(path)
	lm.Append(&log.LogRecord{TxID: 1, Type: log.RecordBegin})
	lm.Append(&log.LogRecord{TxID: 1, Type: log.RecordCommit})
	lm.Close()

	// 書き込みの途中でクラッシュしたように、末尾に中途半端なバイト列を足す
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.Write([]byte{0, 0, 0, 40, 1, 2, 3})
	f.Close()

	lm2, err := log.NewLogManager(path)
	if err != nil {
		t.Fatal(err)
	}
	lm2.Append(&log.LogRecord{TxID: 2, Type: log.RecordBegin})
	records, err := lm2.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	// 壊れた末尾は捨てられ、その位置から新しいレコードが続く
	if len(records) != 3 || records[2].LSN != 3 {
		t.Errorf("壊れた末尾が正しく切り捨てられていない: %d件", len(records))
	}
	lm2.Close()
}

func TestLogManager_Truncate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.log")
	lm, _ := log.NewLogManager(path)
	for i := 0; i < 10; i++ {
		lm.Append(&log.LogRecord{TxID: 1, Type: log.RecordBegin})
	}
	lm.Flush()
	if err := lm.Truncate(); err != nil {
		t.Fatal(err)
	}
	lsn := lm.Append(&log.LogRecord{TxID: 2, Type: log.RecordBegin})
	lm.Close()

	lm2, _ := log.NewLogManager(path)
	defer lm2.Close()
	records, _ := lm2.ReadAll()
	// CHECKPOINT(LSN=11) と BEGIN(LSN=12) だけが残り、LSNは巻き戻らない
	if len(records) != 2 || records[0].Type != log.RecordCheckpoint || records[0].LSN != 11 || lsn != 12 {
		t.Errorf("Truncate後のログが不正: %d件, 先頭=%+v, lsn=%d", len(records), records[0], lsn)
	}
}
