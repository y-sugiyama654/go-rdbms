// db/session.go
package db

import (
	"errors"
	"fmt"

	"github.com/yourname/go-rdbms/sql/executor"
	"github.com/yourname/go-rdbms/sql/parser"
	"github.com/yourname/go-rdbms/transaction"
	"github.com/yourname/go-rdbms/transaction/lock"
)

// Session は1つのクライアント（接続）に相当し、実行中のトランザクションを覚えておく
// 1つのSessionを複数のゴルーチンから同時に使ってはいけない
type Session struct {
	db *DB
	tx *transaction.Transaction // BEGIN〜COMMIT/ROLLBACKの間だけnilでない
}

// InTransaction はBEGINで始めたトランザクションの途中かどうかを返す
func (s *Session) InTransaction() bool { return s.tx != nil }

// Exec はSQL文を1つ実行する
// BEGINしていなければ、1文ごとに自動でコミット（失敗したらロールバック）する
func (s *Session) Exec(sql string) (*executor.Result, error) {
	stmt, err := parser.Parse(sql)
	if err != nil {
		return nil, err
	}

	switch stmt.(type) {
	case *parser.BeginStatement:
		if s.tx != nil {
			return nil, fmt.Errorf("db: 既にトランザクションの途中です")
		}
		s.tx = s.db.txMgr.Begin()
		return &executor.Result{Message: "BEGIN"}, nil
	case *parser.CommitStatement:
		if s.tx == nil {
			return nil, fmt.Errorf("db: トランザクションが開始されていません")
		}
		tx := s.tx
		s.tx = nil
		if err := s.db.txMgr.Commit(tx); err != nil {
			s.db.txMgr.Rollback(tx)
			return nil, err
		}
		return &executor.Result{Message: "COMMIT"}, nil
	case *parser.RollbackStatement:
		if s.tx == nil {
			return nil, fmt.Errorf("db: トランザクションが開始されていません")
		}
		tx := s.tx
		s.tx = nil
		if err := s.db.txMgr.Rollback(tx); err != nil {
			return nil, err
		}
		return &executor.Result{Message: "ROLLBACK"}, nil
	}

	// BEGINしたトランザクションの中で実行する
	if s.tx != nil {
		res, err := s.db.exec.Execute(s.tx, stmt)
		if errors.Is(err, lock.ErrDeadlock) {
			// デッドロックの犠牲になったトランザクションは続けられないのでロールバックする
			s.db.txMgr.Rollback(s.tx)
			s.tx = nil
			return nil, fmt.Errorf("%w（トランザクションをロールバックしました）", err)
		}
		return res, err
	}

	// 自動コミット
	tx := s.db.txMgr.Begin()
	res, err := s.db.exec.Execute(tx, stmt)
	if err != nil {
		s.db.txMgr.Rollback(tx)
		return nil, err
	}
	if err := s.db.txMgr.Commit(tx); err != nil {
		s.db.txMgr.Rollback(tx)
		return nil, err
	}
	return res, nil
}

// Close は途中のトランザクションがあればロールバックする
func (s *Session) Close() error {
	if s.tx == nil {
		return nil
	}
	tx := s.tx
	s.tx = nil
	return s.db.txMgr.Rollback(tx)
}
