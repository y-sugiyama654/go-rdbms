// sql/parser/ast.go
package parser

import (
	"fmt"
	"strconv"
)

// Statement はSQL文を表すASTノード
type Statement interface {
	statementNode()
}

// Expression は式を表すASTノード
type Expression interface {
	expressionNode()
	String() string
}

// ---- 文 ----

// SelectStatement は SELECT 文
type SelectStatement struct {
	Columns []*ColumnRef // nil なら SELECT *
	From    string
	Joins   []JoinClause
	Where   Expression // WHERE句がなければnil
}

// JoinClause は JOIN テーブル ON 条件
type JoinClause struct {
	Table string
	On    Expression
}

// InsertStatement は INSERT 文
type InsertStatement struct {
	Table   string
	Columns []string // カラムリストを省略した場合はnil
	Values  []Expression
}

// CreateTableStatement は CREATE TABLE 文
type CreateTableStatement struct {
	Table   string
	Columns []ColumnDef
}

// ColumnDef はCREATE TABLEのカラム定義
type ColumnDef struct {
	Name       string
	Type       string // "INT" または "VARCHAR"
	Length     int    // VARCHARの最大文字数
	PrimaryKey bool
}

// BeginStatement / CommitStatement / RollbackStatement はトランザクション制御文
type BeginStatement struct{}
type CommitStatement struct{}
type RollbackStatement struct{}

func (*SelectStatement) statementNode()      {}
func (*InsertStatement) statementNode()      {}
func (*CreateTableStatement) statementNode() {}
func (*BeginStatement) statementNode()       {}
func (*CommitStatement) statementNode()      {}
func (*RollbackStatement) statementNode()    {}

// ---- 式 ----

// ColumnRef はカラム参照（users.id のようにテーブル名で修飾してもよい）
type ColumnRef struct {
	Table string // 修飾なしなら空文字
	Name  string
}

// IntLiteral は整数リテラル
type IntLiteral struct {
	Value int64
}

// StringLiteral は文字列リテラル
type StringLiteral struct {
	Value string
}

// BinaryExpr は二項演算（比較演算子と AND / OR）
type BinaryExpr struct {
	Op    string // "=", "<>", "<", ">", "<=", ">=", "AND", "OR"
	Left  Expression
	Right Expression
}

// NotExpr は NOT 式
type NotExpr struct {
	Expr Expression
}

func (*ColumnRef) expressionNode()     {}
func (*IntLiteral) expressionNode()    {}
func (*StringLiteral) expressionNode() {}
func (*BinaryExpr) expressionNode()    {}
func (*NotExpr) expressionNode()       {}

func (e *ColumnRef) String() string {
	if e.Table != "" {
		return e.Table + "." + e.Name
	}
	return e.Name
}

func (e *IntLiteral) String() string    { return strconv.FormatInt(e.Value, 10) }
func (e *StringLiteral) String() string { return "'" + e.Value + "'" }
func (e *BinaryExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", e.Left, e.Op, e.Right)
}
func (e *NotExpr) String() string { return fmt.Sprintf("(NOT %s)", e.Expr) }
