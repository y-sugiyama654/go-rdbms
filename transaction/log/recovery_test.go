// transaction/log/recovery_test.go
package log_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/sql/executor"
	"github.com/yourname/go-rdbms/sql/parser"
	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/disk"
	"github.com/yourname/go-rdbms/transaction"
	"github.com/yourname/go-rdbms/transaction/lock"
	"github.com/yourname/go-rdbms/transaction/log"
)

// testDB はデータファイル・カタログ・WALを束ねたテスト用のデータベース
type testDB struct {
	t        *testing.T
	dm       *disk.FileDiskManager
	bpm      *buffer.BufferPoolManager
	lm       *log.LogManager
	tm       *transaction.TransactionManager
	executor *executor.Executor
}

// open は起動処理を行う: リカバリ → カタログの読み込み → インデックス再構築 → チェックポイント
func open(t *testing.T, dir string) *testDB {
	t.Helper()
	dm, err := disk.NewFileDiskManager(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	bpm := buffer.NewBufferPoolManager(dm, 16) // 小さめにして追い出しを起こりやすくする
	lm, err := log.NewLogManager(filepath.Join(dir, "wal.log"))
	if err != nil {
		t.Fatal(err)
	}
	bpm.SetLogFlusher(func(lsn uint64) error { return lm.FlushTo(log.LSN(lsn)) })

	recovered, err := log.NewRecoveryManager(bpm, lm).Recover()
	if err != nil {
		t.Fatal(err)
	}
	// カタログはリカバリの後に開く（REDOでヒープにページが増えている場合があるため）
	cat, err := catalog.NewCatalog(bpm, filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if recovered {
		if err := executor.RebuildIndexes(cat); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Checkpoint(bpm, lm); err != nil {
		t.Fatal(err)
	}

	lockMgr := lock.NewLockManager()
	return &testDB{
		t: t, dm: dm, bpm: bpm, lm: lm,
		tm:       transaction.NewTransactionManager(lockMgr, lm),
		executor: executor.New(cat, lockMgr),
	}
}

// crash はバッファプールの中身を書き出さずにデータファイルを閉じる（プロセスの異常終了を模す）
func (db *testDB) crash() {
	db.dm.Close()
}

func (db *testDB) exec(tx *transaction.Transaction, sql string) *executor.Result {
	db.t.Helper()
	stmt, err := parser.Parse(sql)
	if err != nil {
		db.t.Fatal(err)
	}
	res, err := db.executor.Execute(tx, stmt)
	if err != nil {
		db.t.Fatalf("%s: %v", sql, err)
	}
	return res
}

// query は1文だけのトランザクションでSELECTを実行する
func (db *testDB) query(sql string) *executor.Result {
	db.t.Helper()
	tx := db.tm.Begin()
	defer db.tm.Commit(tx)
	return db.exec(tx, sql)
}

func (db *testDB) createUsers() {
	tx := db.tm.Begin()
	db.exec(tx, "CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(100))")
	db.tm.Commit(tx)
}

func TestRecovery_CommittedDataSurvivesCrash(t *testing.T) {
	dir := t.TempDir()
	db := open(t, dir)
	db.createUsers()

	tx := db.tm.Begin()
	for i := 1; i <= 300; i++ {
		db.exec(tx, fmt.Sprintf("INSERT INTO users VALUES (%d, 'user%03d-padding-padding-padding')", i, i))
	}
	if err := db.tm.Commit(tx); err != nil {
		t.Fatal(err)
	}
	db.crash() // コミット済みの変更の多くはまだバッファプールの中にしかない

	db2 := open(t, dir)
	if got := len(db2.query("SELECT * FROM users").Rows); got != 300 {
		t.Errorf("コミット済みの行が失われた: want 300, got %d", got)
	}
	// インデックスも再構築されているので、主キーで引ける
	res := db2.query("SELECT name FROM users WHERE id = 123")
	if len(res.Rows) != 1 || res.Rows[0][0].Str != "user123-padding-padding-padding" {
		t.Errorf("インデックス経由の検索結果が不正: %v", res.Rows)
	}
}

func TestRecovery_UncommittedChangesAreUndone(t *testing.T) {
	dir := t.TempDir()
	db := open(t, dir)
	db.createUsers()

	tx1 := db.tm.Begin()
	db.exec(tx1, "INSERT INTO users VALUES (1, 'committed')")
	db.tm.Commit(tx1)

	// TX2 は大量に挿入したがコミットしていない
	tx2 := db.tm.Begin()
	for i := 100; i < 400; i++ {
		db.exec(tx2, fmt.Sprintf("INSERT INTO users VALUES (%d, 'uncommitted-padding-padding-padding')", i))
	}
	// 未コミットの変更がデータファイルに書き出された状態を作る
	if err := db.bpm.FlushAllPages(); err != nil {
		t.Fatal(err)
	}
	db.crash()

	db2 := open(t, dir)
	res := db2.query("SELECT * FROM users")
	if len(res.Rows) != 1 || res.Rows[0][1].Str != "committed" {
		t.Errorf("未コミットの行が残っている: %d件", len(res.Rows))
	}
	// 取り消したキーは再び使える
	tx := db2.tm.Begin()
	db2.exec(tx, "INSERT INTO users VALUES (100, 'reused')")
	db2.tm.Commit(tx)
}

func TestRecovery_RollbackBeforeCrash(t *testing.T) {
	dir := t.TempDir()
	db := open(t, dir)
	db.createUsers()

	tx1 := db.tm.Begin()
	for i := 1; i <= 50; i++ {
		db.exec(tx1, fmt.Sprintf("INSERT INTO users VALUES (%d, 'rolled back')", i))
	}
	db.tm.Rollback(tx1)

	tx2 := db.tm.Begin()
	db.exec(tx2, "INSERT INTO users VALUES (1000, 'kept')")
	db.tm.Commit(tx2) // このfsyncで、TX1のDELETE・ABORTのログもディスクに書かれる
	db.crash()

	db2 := open(t, dir)
	res := db2.query("SELECT * FROM users")
	if len(res.Rows) != 1 || res.Rows[0][0].Int != 1000 {
		t.Errorf("id=1000の1件だけが残るはず: got %d件", len(res.Rows))
	}
}

func TestRecovery_Idempotent(t *testing.T) {
	dir := t.TempDir()
	db := open(t, dir)
	db.createUsers()
	tx := db.tm.Begin()
	for i := 1; i <= 100; i++ {
		db.exec(tx, fmt.Sprintf("INSERT INTO users VALUES (%d, 'x')", i))
	}
	db.tm.Commit(tx)
	loser := db.tm.Begin()
	db.exec(loser, "INSERT INTO users VALUES (999, 'loser')")
	db.lm.Flush()
	db.crash()

	// チェックポイントを挟まずにリカバリを2回続けて実行しても、結果は同じになる
	dm, _ := disk.NewFileDiskManager(filepath.Join(dir, "test.db"))
	bpm := buffer.NewBufferPoolManager(dm, 16)
	lm, _ := log.NewLogManager(filepath.Join(dir, "wal.log"))
	bpm.SetLogFlusher(func(lsn uint64) error { return lm.FlushTo(log.LSN(lsn)) })
	for i := 0; i < 2; i++ {
		if _, err := log.NewRecoveryManager(bpm, lm).Recover(); err != nil {
			t.Fatalf("%d回目のリカバリに失敗: %v", i+1, err)
		}
	}
	bpm.FlushAllPages()
	dm.Close()

	db2 := open(t, dir)
	if got := len(db2.query("SELECT * FROM users").Rows); got != 100 {
		t.Errorf("2回リカバリした後の件数: want 100, got %d", got)
	}
}

func TestWAL_LogIsFlushedBeforePage(t *testing.T) {
	dir := t.TempDir()
	db := open(t, dir)
	db.createUsers()

	tx := db.tm.Begin()
	db.exec(tx, "INSERT INTO users VALUES (1, 'not committed yet')")

	// コミット前なので、まだINSERTのログはディスクにない
	if countOnDisk(t, dir, log.RecordInsert) != 0 {
		t.Fatal("コミット前なのにINSERTのログがディスクにある")
	}
	// ページを書き出すと、そのページLSNまでのログが先にディスクへ書かれる
	if err := db.bpm.FlushAllPages(); err != nil {
		t.Fatal(err)
	}
	if countOnDisk(t, dir, log.RecordInsert) != 1 {
		t.Error("ページを書き出したのにINSERTのログがディスクにない（WALのルール違反）")
	}
	db.tm.Commit(tx)
}

// countOnDisk はディスク上のWALファイルにある、指定した種類のレコード数を数える
func countOnDisk(t *testing.T, dir string, typ log.RecordType) int {
	t.Helper()
	// 別のLogManagerで開くと、ファイルに書かれている分だけが読める
	lm, err := log.NewLogManager(filepath.Join(dir, "wal.log"))
	if err != nil {
		t.Fatal(err)
	}
	records, err := lm.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range records {
		if r.Type == typ {
			n++
		}
	}
	return n
}
