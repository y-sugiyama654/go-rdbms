// sql/executor/executor_test.go
package executor_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/sql/executor"
	"github.com/yourname/go-rdbms/sql/parser"
	"github.com/yourname/go-rdbms/sql/planner"
	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/disk"
	"github.com/yourname/go-rdbms/transaction"
	"github.com/yourname/go-rdbms/transaction/lock"
)

type testDB struct {
	t    *testing.T
	cat  *catalog.Catalog
	exec *executor.Executor
	tm   *transaction.TransactionManager
}

func setup(t *testing.T) *testDB {
	t.Helper()
	db, _ := openDB(t, t.TempDir())
	return db
}

// openDB はdir内のデータファイルとカタログファイルを開く
func openDB(t *testing.T, dir string) (*testDB, *buffer.BufferPoolManager) {
	t.Helper()
	dm, err := disk.NewFileDiskManager(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dm.Close() })
	bpm := buffer.NewBufferPoolManager(dm, 64)
	cat, err := catalog.NewCatalog(bpm, filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	lockMgr := lock.NewLockManager()
	tm := transaction.NewTransactionManager(lockMgr)
	return &testDB{t: t, cat: cat, exec: executor.New(cat, lockMgr), tm: tm}, bpm
}

// run はSQLを実行し、エラーならテストを失敗させる
func (db *testDB) run(sql string) *executor.Result {
	db.t.Helper()
	res, err := db.try(sql)
	if err != nil {
		db.t.Fatalf("%s: %v", sql, err)
	}
	return res
}

// try はSQLを1文だけのトランザクションとして実行する（成功したらコミット、失敗したらロールバック）
func (db *testDB) try(sql string) (*executor.Result, error) {
	stmt, err := parser.Parse(sql)
	if err != nil {
		return nil, err
	}
	tx := db.tm.Begin()
	res, err := db.exec.Execute(tx, stmt)
	if err != nil {
		db.tm.Rollback(tx)
		return nil, err
	}
	return res, db.tm.Commit(tx)
}

// rows は結果を "1,Alice" のような文字列の並びにする
func rows(res *executor.Result) []string {
	var out []string
	for _, row := range res.Rows {
		vals := make([]string, len(row))
		for i, v := range row {
			vals[i] = v.String()
		}
		out = append(out, strings.Join(vals, ","))
	}
	return out
}

func (db *testDB) seedUsers() {
	db.run("CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(20), age INT)")
	db.run("INSERT INTO users VALUES (1, 'Alice', 30)")
	db.run("INSERT INTO users VALUES (2, 'Bob', 25)")
	db.run("INSERT INTO users (name, age, id) VALUES ('Charlie', 35, 3)") // カラム順を入れ替えてもよい
}

func TestExecutor_SelectWhere(t *testing.T) {
	db := setup(t)
	db.seedUsers()

	res := db.run("SELECT name, id FROM users WHERE age >= 30 AND name <> 'Bob'")
	if got := strings.Join(res.Columns, ","); got != "NAME,ID" {
		t.Errorf("列名不一致: got %s", got)
	}
	if got := strings.Join(rows(res), " | "); got != "Alice,1 | Charlie,3" {
		t.Errorf("結果不一致: got %s", got)
	}
}

func TestExecutor_IndexScan(t *testing.T) {
	db := setup(t)
	db.run("CREATE TABLE items (id INT PRIMARY KEY, label VARCHAR(10))")
	for i := 1; i <= 500; i++ {
		db.run(fmt.Sprintf("INSERT INTO items VALUES (%d, 'item%d')", i, i))
	}

	tests := []struct {
		where     string
		wantPlan  string
		wantCount int
	}{
		{"id = 42", "IndexScan ITEMS [42, 42]", 1},
		{"id > 490", "IndexScan ITEMS [491, +inf]", 10},
		{"100 >= id AND id >= 91", "IndexScan ITEMS [91, 100]", 10},
		{"id >= 10 AND label = 'item20'", "IndexScan ITEMS [10, +inf]", 1},
		{"label = 'item20'", "SeqScan ITEMS", 1},
		{"id = 1 OR id = 2", "SeqScan ITEMS", 2}, // ORはインデックスで絞らない
	}
	for _, tt := range tests {
		sql := "SELECT * FROM items WHERE " + tt.where
		stmt, _ := parser.Parse(sql)
		plan, err := planner.New(db.cat).PlanSelect(stmt.(*parser.SelectStatement))
		if err != nil {
			t.Fatal(err)
		}
		if explain := planner.Explain(plan); !strings.Contains(explain, tt.wantPlan) {
			t.Errorf("%s: 実行計画に %q が含まれない:\n%s", tt.where, tt.wantPlan, explain)
		}
		if got := len(db.run(sql).Rows); got != tt.wantCount {
			t.Errorf("%s: want %d件, got %d件", tt.where, tt.wantCount, got)
		}
	}
}

func TestExecutor_Join(t *testing.T) {
	db := setup(t)
	db.seedUsers()
	db.run("CREATE TABLE orders (id INT PRIMARY KEY, user_id INT, item VARCHAR(20))")
	db.run("INSERT INTO orders VALUES (10, 1, 'book')")
	db.run("INSERT INTO orders VALUES (11, 3, 'pen')")
	db.run("INSERT INTO orders VALUES (12, 1, 'desk')")

	res := db.run("SELECT users.name, orders.item FROM users JOIN orders ON users.id = orders.user_id WHERE orders.id >= 11")
	if got := strings.Join(res.Columns, ","); got != "USERS.NAME,ORDERS.ITEM" {
		t.Errorf("列名不一致: got %s", got)
	}
	if got := strings.Join(rows(res), " | "); got != "Alice,desk | Charlie,pen" {
		t.Errorf("結果不一致: got %s", got)
	}

	// どちらのテーブルにもある ID を修飾せずに使うとエラー
	if _, err := db.try("SELECT id FROM users JOIN orders ON users.id = orders.user_id"); err == nil {
		t.Error("あいまいなカラム参照がエラーにならなかった")
	}
}

func TestExecutor_InsertErrors(t *testing.T) {
	db := setup(t)
	db.seedUsers()

	for _, sql := range []string{
		"INSERT INTO users VALUES (1, 'Dup', 20)",                  // 主キーの重複
		"INSERT INTO users VALUES ('x', 'Eve', 20)",                // 型の不一致
		"INSERT INTO users VALUES (4, 'Eve')",                      // 値が足りない
		"INSERT INTO users VALUES (5, 'abcdefghijklmnopqrstu', 1)", // VARCHAR(20)を超える
		"INSERT INTO users VALUES (3000000000, 'Big', 1)",          // INTの範囲外
		"INSERT INTO nothing VALUES (1)",                           // 存在しないテーブル
	} {
		if _, err := db.try(sql); err == nil {
			t.Errorf("エラーになるはず: %s", sql)
		} else {
			t.Logf("%s → %v", sql, err)
		}
	}
	if got := len(db.run("SELECT * FROM users").Rows); got != 3 {
		t.Errorf("失敗したINSERTで行が増えている: %d件", got)
	}
}

func TestPlanner_Explain(t *testing.T) {
	db := setup(t)
	db.seedUsers()
	stmt, _ := parser.Parse("SELECT name FROM users WHERE id = 2")
	plan, err := planner.New(db.cat).PlanSelect(stmt.(*parser.SelectStatement))
	if err != nil {
		t.Fatal(err)
	}
	want := "Projection NAME\n" +
		"  Filter (ID = 2)\n" +
		"    IndexScan USERS [2, 2]\n"
	if got := planner.Explain(plan); got != want {
		t.Errorf("Explain不一致:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestExecutor_Reopen(t *testing.T) {
	dir := t.TempDir()

	// 1回目: テーブルを作って1000行入れ、ページを書き出す
	db, bpm := openDB(t, dir)
	db.run("CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(20))")
	for i := 1; i <= 1000; i++ {
		db.run(fmt.Sprintf("INSERT INTO users VALUES (%d, 'user%d')", i, i))
	}
	if err := bpm.FlushAllPages(); err != nil {
		t.Fatal(err)
	}

	// 2回目: 開き直してもSQLで同じデータが読める（インデックスも使える）
	db2, _ := openDB(t, dir)
	if got := rows(db2.run("SELECT name FROM users WHERE id = 777")); len(got) != 1 || got[0] != "user777" {
		t.Errorf("再オープン後の検索結果が不正: %v", got)
	}
	if got := len(db2.run("SELECT * FROM users").Rows); got != 1000 {
		t.Errorf("再オープン後の件数: want 1000, got %d", got)
	}
}
