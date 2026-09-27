// db/db_test.go
package db_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/yourname/go-rdbms/db"
	"github.com/yourname/go-rdbms/transaction/lock"
)

func mustOpen(t *testing.T, dir string) *db.DB {
	t.Helper()
	database, err := db.Open(db.Config{Dir: dir, BufferSize: 64})
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func mustExec(t *testing.T, s *db.Session, sql string) {
	t.Helper()
	if _, err := s.Exec(sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func count(t *testing.T, s *db.Session, table string) int {
	t.Helper()
	res, err := s.Exec("SELECT * FROM " + table)
	if err != nil {
		t.Fatal(err)
	}
	return len(res.Rows)
}

func TestDB_SessionTransaction(t *testing.T) {
	database := mustOpen(t, t.TempDir())
	defer database.Close()
	s := database.NewSession()

	mustExec(t, s, "CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(20))")
	mustExec(t, s, "INSERT INTO users VALUES (1, 'Alice')") // 自動コミット

	mustExec(t, s, "BEGIN")
	mustExec(t, s, "INSERT INTO users VALUES (2, 'Bob')")
	if got := count(t, s, "users"); got != 2 {
		t.Errorf("トランザクション内では自分の挿入が見える: want 2, got %d", got)
	}
	mustExec(t, s, "ROLLBACK")
	if got := count(t, s, "users"); got != 1 {
		t.Errorf("ROLLBACK後: want 1, got %d", got)
	}

	if _, err := s.Exec("COMMIT"); err == nil {
		t.Error("BEGINしていないCOMMITがエラーにならなかった")
	}
}

func TestDB_CloseAndReopen(t *testing.T) {
	dir := t.TempDir()
	database := mustOpen(t, dir)
	s := database.NewSession()
	mustExec(t, s, "CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(20))")
	mustExec(t, s, "INSERT INTO users VALUES (1, 'Alice')")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database2 := mustOpen(t, dir)
	defer database2.Close()
	if database2.Recovered() {
		t.Error("正常終了したのにリカバリが行われた")
	}
	if got := count(t, database2.NewSession(), "users"); got != 1 {
		t.Errorf("再オープン後: want 1, got %d", got)
	}
}

func TestDB_CrashRecovery(t *testing.T) {
	dir := t.TempDir()
	database := mustOpen(t, dir)
	s := database.NewSession()
	mustExec(t, s, "CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(20))")
	for i := 1; i <= 100; i++ {
		mustExec(t, s, fmt.Sprintf("INSERT INTO users VALUES (%d, 'user%d')", i, i))
	}
	mustExec(t, s, "BEGIN")
	mustExec(t, s, "INSERT INTO users VALUES (1000, 'uncommitted')")
	// Closeせずに放置して、プロセスが落ちた状況を模す

	database2 := mustOpen(t, dir)
	defer database2.Close()
	if !database2.Recovered() {
		t.Error("異常終了後なのにリカバリが行われていない")
	}
	s2 := database2.NewSession()
	if got := count(t, s2, "users"); got != 100 {
		t.Errorf("リカバリ後の件数: want 100, got %d", got)
	}
	res, _ := s2.Exec("SELECT name FROM users WHERE id = 42")
	if len(res.Rows) != 1 || res.Rows[0][0].Str != "user42" {
		t.Errorf("インデックス経由の検索結果が不正: %v", res.Rows)
	}
}

func TestDB_Deadlock(t *testing.T) {
	database := mustOpen(t, t.TempDir())
	defer database.Close()
	s1, s2 := database.NewSession(), database.NewSession()
	mustExec(t, s1, "CREATE TABLE a (id INT PRIMARY KEY)")
	mustExec(t, s1, "CREATE TABLE b (id INT PRIMARY KEY)")

	mustExec(t, s1, "BEGIN")
	mustExec(t, s2, "BEGIN")
	mustExec(t, s1, "SELECT * FROM a")
	mustExec(t, s2, "SELECT * FROM b")

	done := make(chan error, 1)
	go func() {
		_, err := s1.Exec("INSERT INTO b VALUES (1)") // s1 は s2 を待つ
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)

	_, err := s2.Exec("INSERT INTO a VALUES (1)") // 閉路ができる
	if !errors.Is(err, lock.ErrDeadlock) {
		t.Fatalf("デッドロックになるはず: %v", err)
	}
	t.Logf("s2: %v", err)
	if s2.InTransaction() {
		t.Error("デッドロックの犠牲になったトランザクションが残っている")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mustExec(t, s1, "COMMIT")
	if got := count(t, s1, "b"); got != 1 {
		t.Errorf("s1の挿入がコミットされていない: %d件", got)
	}
}
