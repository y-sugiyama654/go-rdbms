// db/db.go
package db

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/yourname/go-rdbms/sql/catalog"
	"github.com/yourname/go-rdbms/sql/executor"
	"github.com/yourname/go-rdbms/storage/buffer"
	"github.com/yourname/go-rdbms/storage/disk"
	"github.com/yourname/go-rdbms/transaction"
	"github.com/yourname/go-rdbms/transaction/lock"
	"github.com/yourname/go-rdbms/transaction/log"
)

// Config はデータベースの設定
type Config struct {
	Dir        string // データファイル・カタログ・WALを置くディレクトリ
	BufferSize int    // バッファプールのフレーム数（0なら1024 = 4MB）
}

// DB はすべてのコンポーネントを束ねたデータベース本体
type DB struct {
	dm        *disk.FileDiskManager
	bpm       *buffer.BufferPoolManager
	logMgr    *log.LogManager
	catalog   *catalog.Catalog
	txMgr     *transaction.TransactionManager
	exec      *executor.Executor
	recovered bool
}

// Open はデータベースを開く（なければ新しく作る）
// 前回が異常終了だった場合は、ここでリカバリが行われる
func Open(cfg Config) (*DB, error) {
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 1024
	}
	if err := os.MkdirAll(cfg.Dir, 0755); err != nil {
		return nil, err
	}

	// ストレージ層とWAL
	dm, err := disk.NewFileDiskManager(filepath.Join(cfg.Dir, "data.db"))
	if err != nil {
		return nil, err
	}
	bpm := buffer.NewBufferPoolManager(dm, cfg.BufferSize)
	logMgr, err := log.NewLogManager(filepath.Join(cfg.Dir, "wal.log"))
	if err != nil {
		dm.Close()
		return nil, err
	}
	bpm.SetLogFlusher(func(lsn uint64) error { return logMgr.FlushTo(log.LSN(lsn)) })

	// リカバリ → カタログ → インデックス再構築 → チェックポイント の順に行う
	recovered, err := log.NewRecoveryManager(bpm, logMgr).Recover()
	if err != nil {
		dm.Close()
		return nil, fmt.Errorf("db: リカバリに失敗しました: %w", err)
	}
	cat, err := catalog.NewCatalog(bpm, filepath.Join(cfg.Dir, "catalog.json"))
	if err != nil {
		dm.Close()
		return nil, err
	}
	if recovered {
		if err := executor.RebuildIndexes(cat); err != nil {
			dm.Close()
			return nil, fmt.Errorf("db: インデックスの再構築に失敗しました: %w", err)
		}
	}
	if err := log.Checkpoint(bpm, logMgr); err != nil {
		dm.Close()
		return nil, err
	}

	// ロックとトランザクション、SQLの実行
	lockMgr := lock.NewLockManager()
	return &DB{
		dm:        dm,
		bpm:       bpm,
		logMgr:    logMgr,
		catalog:   cat,
		txMgr:     transaction.NewTransactionManager(lockMgr, logMgr),
		exec:      executor.New(cat, lockMgr),
		recovered: recovered,
	}, nil
}

// Recovered は、Open時にクラッシュからのリカバリを行ったかどうかを返す
func (db *DB) Recovered() bool { return db.recovered }

// ListTables はテーブル名の一覧を返す
func (db *DB) ListTables() []string { return db.catalog.ListTables() }

// NewSession はSQLを実行するためのセッションを作る
func (db *DB) NewSession() *Session { return &Session{db: db} }

// Close はチェックポイントを取ってからデータベースを閉じる
// 実行中のトランザクションがあれば、それが終わるまで待つ
func (db *DB) Close() error {
	err := db.txMgr.Checkpoint(func() error {
		return log.Checkpoint(db.bpm, db.logMgr)
	})
	if err != nil {
		return err
	}
	if err := db.logMgr.Close(); err != nil {
		return err
	}
	return db.dm.Close()
}
