// transaction/transaction_manager_test.go
package transaction_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/sql/executor"
	"github.com/yourname/go-rdbms/sql/parser"
	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/disk"
	"github.com/yourname/go-rdbms/transaction"
	"github.com/yourname/go-rdbms/transaction/lock"
)

type env struct {
	t        *testing.T
	tm       *transaction.TransactionManager
	executor *executor.Executor
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	dm, err := disk.NewFileDiskManager(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dm.Close() })
	bpm := buffer.NewBufferPoolManager(dm, 32)
	cat, err := catalog.NewCatalog(bpm, filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	lockMgr := lock.NewLockManager()
	e := &env{
		t:        t,
		tm:       transaction.NewTransactionManager(lockMgr),
		executor: executor.New(cat, lockMgr),
	}

	tx := e.tm.Begin()
	e.exec(tx, "CREATE TABLE items (id INT PRIMARY KEY, name VARCHAR(10))")
	e.tm.Commit(tx)
	return e
}

// exec はトランザクションtxの中でSQLを実行する
func (e *env) exec(tx *transaction.Transaction, sql string) (*executor.Result, error) {
	stmt, err := parser.Parse(sql)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.executor.Execute(tx, stmt)
}

// count は新しいトランザクションで items の行数を数える
func (e *env) count() int {
	tx := e.tm.Begin()
	defer e.tm.Commit(tx)
	res, err := e.exec(tx, "SELECT * FROM items")
	if err != nil {
		e.t.Fatal(err)
	}
	return len(res.Rows)
}

func TestTransaction_Commit(t *testing.T) {
	e := setup(t)

	tx := e.tm.Begin()
	e.exec(tx, "INSERT INTO items VALUES (1, 'apple')")
	e.exec(tx, "INSERT INTO items VALUES (2, 'banana')")
	if err := e.tm.Commit(tx); err != nil {
		t.Fatal(err)
	}
	if got := e.count(); got != 2 {
		t.Errorf("コミット後の件数: want 2, got %d", got)
	}

	// 終了したトランザクションは再びコミットもロールバックもできない
	if err := e.tm.Commit(tx); err == nil {
		t.Error("2回目のCommitがエラーにならなかった")
	}
	if err := e.tm.Rollback(tx); err == nil {
		t.Error("コミット済みトランザクションのRollbackがエラーにならなかった")
	}
}

func TestTransaction_Rollback(t *testing.T) {
	e := setup(t)

	tx := e.tm.Begin()
	e.exec(tx, "INSERT INTO items VALUES (1, 'apple')")
	e.exec(tx, "INSERT INTO items VALUES (2, 'banana')")
	if err := e.tm.Rollback(tx); err != nil {
		t.Fatal(err)
	}
	if tx.Status() != transaction.StatusAborted {
		t.Errorf("状態がAbortedになっていない: %v", tx.Status())
	}
	if got := e.count(); got != 0 {
		t.Errorf("ロールバック後の件数: want 0, got %d", got)
	}

	// インデックスからもキーが消えているので、同じ主キーをもう一度挿入できる
	tx2 := e.tm.Begin()
	if _, err := e.exec(tx2, "INSERT INTO items VALUES (1, 'avocado')"); err != nil {
		t.Errorf("ロールバックした主キーを再利用できない: %v", err)
	}
	e.tm.Commit(tx2)
}

func TestTransaction_NoDirtyRead(t *testing.T) {
	e := setup(t)
	tx := e.tm.Begin()
	e.exec(tx, "CREATE TABLE accounts (id INT PRIMARY KEY, owner VARCHAR(10))")
	e.tm.Commit(tx)

	// TX1がINSERTしてまだコミットしていない（ITEMSのXロックを持っている）
	tx1 := e.tm.Begin()
	e.exec(tx1, "INSERT INTO items VALUES (1, 'apple')")

	// 別のトランザクションがITEMSを読もうとすると、TX1が終わるまで待たされる
	done := make(chan int, 1)
	go func() { done <- e.count() }()
	select {
	case n := <-done:
		t.Fatalf("TX1の終了前に読めてしまった（%d件）", n)
	case <-time.After(100 * time.Millisecond):
	}

	// 関係ないテーブルは待たずに読める
	tx3 := e.tm.Begin()
	if _, err := e.exec(tx3, "SELECT * FROM accounts"); err != nil {
		t.Fatal(err)
	}
	e.tm.Commit(tx3)

	// TX1がロールバックすると、待っていた読み取りは「0件」を返す
	e.tm.Rollback(tx1)
	select {
	case n := <-done:
		if n != 0 {
			t.Errorf("ロールバックされた行が見えている: %d件", n)
		}
	case <-time.After(time.Second):
		t.Fatal("TX1の終了後も待たされている")
	}
}

func TestTransaction_DeadlockVictimRollsBack(t *testing.T) {
	e := setup(t)
	tx := e.tm.Begin()
	e.exec(tx, "CREATE TABLE accounts (id INT PRIMARY KEY, owner VARCHAR(10))")
	e.tm.Commit(tx)

	tx1, tx2 := e.tm.Begin(), e.tm.Begin()
	e.exec(tx1, "SELECT * FROM items")    // TX1: ITEMS の S
	e.exec(tx2, "SELECT * FROM accounts") // TX2: ACCOUNTS の S

	// TX1 は ACCOUNTS に書こうとして TX2 を待つ
	done := make(chan error, 1)
	go func() {
		_, err := e.exec(tx1, "INSERT INTO accounts VALUES (1, 'alice')")
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)

	// TX2 が ITEMS に書こうとすると閉路ができ、TX2 の要求がデッドロックとして失敗する
	_, err := e.exec(tx2, "INSERT INTO items VALUES (1, 'apple')")
	if !errors.Is(err, lock.ErrDeadlock) {
		t.Fatalf("デッドロックになるはず: got %v", err)
	}
	// デッドロックの犠牲になったTX2をロールバックすると、TX1が先へ進める
	e.tm.Rollback(tx2)
	if err := <-done; err != nil {
		t.Fatalf("TX1のINSERTが失敗した: %v", err)
	}
	if err := e.tm.Commit(tx1); err != nil {
		t.Fatal(err)
	}
}
