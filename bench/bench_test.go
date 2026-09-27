// bench/bench_test.go
package bench_test

import (
	"fmt"
	"testing"

	"github.com/yourname/go-rdbms/db"
)

const numRows = 10000

// setup はテーブルを作ってnumRows件のデータを入れたデータベースを返す
func setup(b *testing.B, rows int) (*db.DB, *db.Session) {
	b.Helper()
	database, err := db.Open(db.Config{Dir: b.TempDir(), BufferSize: 1024})
	if err != nil {
		b.Fatal(err)
	}
	s := database.NewSession()
	exec(b, s, "CREATE TABLE bench (id INT PRIMARY KEY, val INT, memo VARCHAR(30))")
	exec(b, s, "BEGIN")
	for i := 0; i < rows; i++ {
		exec(b, s, fmt.Sprintf("INSERT INTO bench VALUES (%d, %d, 'memo-%d')", i, i, i))
	}
	exec(b, s, "COMMIT")
	return database, s
}

func exec(b *testing.B, s *db.Session, sql string) {
	if _, err := s.Exec(sql); err != nil {
		b.Fatalf("%s: %v", sql, err)
	}
}

// 1件ごとにコミットする（毎回WALのfsyncが走る）
func BenchmarkInsertAutoCommit(b *testing.B) {
	database, s := setup(b, 0)
	defer database.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exec(b, s, fmt.Sprintf("INSERT INTO bench VALUES (%d, %d, 'memo')", i, i))
	}
}

// 1つのトランザクションにまとめてコミットする（fsyncは最後の1回だけ）
func BenchmarkInsertInTransaction(b *testing.B) {
	database, s := setup(b, 0)
	defer database.Close()
	b.ResetTimer()
	exec(b, s, "BEGIN")
	for i := 0; i < b.N; i++ {
		exec(b, s, fmt.Sprintf("INSERT INTO bench VALUES (%d, %d, 'memo')", i, i))
	}
	exec(b, s, "COMMIT")
}

// 主キーで1件を引く（IndexScan）
func BenchmarkPointQueryIndexScan(b *testing.B) {
	database, s := setup(b, numRows)
	defer database.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exec(b, s, fmt.Sprintf("SELECT * FROM bench WHERE id = %d", i%numRows))
	}
}

// インデックスのないカラムで1件を引く（SeqScan + Filter）
func BenchmarkPointQuerySeqScan(b *testing.B) {
	database, s := setup(b, numRows)
	defer database.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exec(b, s, fmt.Sprintf("SELECT * FROM bench WHERE val = %d", i%numRows))
	}
}

// 全件を読む
func BenchmarkFullScan(b *testing.B) {
	database, s := setup(b, numRows)
	defer database.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exec(b, s, "SELECT * FROM bench")
	}
}
