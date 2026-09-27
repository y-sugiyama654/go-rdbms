// transaction/transaction_manager_test.go
package transaction_test

import (
	"path/filepath"
	"testing"

	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/sql/executor"
	"github.com/yourname/go-rdbms/sql/parser"
	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/disk"
	"github.com/yourname/go-rdbms/transaction"
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
	e := &env{t: t, tm: transaction.NewTransactionManager(), executor: executor.New(cat)}

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

func TestTransaction_DirtyReadWithoutLocks(t *testing.T) {
	e := setup(t)

	// TX1がINSERTしてまだコミットしていない
	tx1 := e.tm.Begin()
	e.exec(tx1, "INSERT INTO items VALUES (1, 'apple')")

	// TX2からTX1の未コミットの行が見えてしまう（ダーティリード）
	if got := e.count(); got != 1 {
		t.Fatalf("この時点ではロックがないので未コミットの行が見える: got %d", got)
	}
	t.Log("ロックがないため、未コミットの行が他のトランザクションから見えた（第11章で解決する）")

	e.tm.Rollback(tx1)
	if got := e.count(); got != 0 {
		t.Errorf("ロールバック後の件数: want 0, got %d", got)
	}
}
