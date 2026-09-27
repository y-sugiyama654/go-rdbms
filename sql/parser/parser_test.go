// sql/parser/parser_test.go
package parser_test

import (
	"fmt"
	"testing"

	"github.com/yourname/go-rdbms/sql/parser"
)

func TestParse_Select(t *testing.T) {
	stmt, err := parser.Parse("SELECT id, name FROM users WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}
	sel, ok := stmt.(*parser.SelectStatement)
	if !ok {
		t.Fatalf("SelectStatementではない: %T", stmt)
	}
	if sel.From != "USERS" {
		t.Errorf("テーブル名不一致: got %s", sel.From)
	}
	if len(sel.Columns) != 2 || sel.Columns[0].Name != "ID" || sel.Columns[1].Name != "NAME" {
		t.Errorf("カラム不一致: got %v", sel.Columns)
	}
	if got := sel.Where.String(); got != "(ID = 1)" {
		t.Errorf("WHERE句不一致: got %s", got)
	}
}

func TestParse_SelectAll(t *testing.T) {
	stmt, err := parser.Parse("SELECT * FROM products;")
	if err != nil {
		t.Fatal(err)
	}
	if sel := stmt.(*parser.SelectStatement); sel.Columns != nil {
		t.Errorf("SELECT * のときColumnsはnilのはず: got %v", sel.Columns)
	}
}

func TestParse_WherePrecedence(t *testing.T) {
	tests := []struct {
		where string
		want  string
	}{
		{"a = 1 AND b = 2", "((A = 1) AND (B = 2))"},
		{"a = 1 OR b = 2 AND c = 3", "((A = 1) OR ((B = 2) AND (C = 3)))"},
		{"(a = 1 OR b = 2) AND c = 3", "(((A = 1) OR (B = 2)) AND (C = 3))"},
		{"NOT a = 1 AND b <> 'x'", "((NOT (A = 1)) AND (B <> 'x'))"},
	}
	for _, tt := range tests {
		stmt, err := parser.Parse("SELECT * FROM t WHERE " + tt.where)
		if err != nil {
			t.Fatalf("%s: %v", tt.where, err)
		}
		if got := stmt.(*parser.SelectStatement).Where.String(); got != tt.want {
			t.Errorf("%s: want %s, got %s", tt.where, tt.want, got)
		}
	}
}

func TestParse_Join(t *testing.T) {
	stmt, err := parser.Parse(
		"SELECT users.name, orders.amount FROM users JOIN orders ON users.id = orders.user_id")
	if err != nil {
		t.Fatal(err)
	}
	sel := stmt.(*parser.SelectStatement)
	if sel.Columns[0].String() != "USERS.NAME" {
		t.Errorf("修飾付きカラム不一致: got %s", sel.Columns[0])
	}
	if len(sel.Joins) != 1 || sel.Joins[0].Table != "ORDERS" {
		t.Fatalf("JOIN句不一致: got %+v", sel.Joins)
	}
	if got := sel.Joins[0].On.String(); got != "(USERS.ID = ORDERS.USER_ID)" {
		t.Errorf("ON句不一致: got %s", got)
	}
}

func TestParse_Insert(t *testing.T) {
	stmt, err := parser.Parse("INSERT INTO users (id, name) VALUES (42, 'Bob')")
	if err != nil {
		t.Fatal(err)
	}
	ins := stmt.(*parser.InsertStatement)
	if ins.Table != "USERS" || len(ins.Columns) != 2 || len(ins.Values) != 2 {
		t.Fatalf("INSERT文の解析結果が不正: %+v", ins)
	}
	if ins.Values[0].String() != "42" || ins.Values[1].String() != "'Bob'" {
		t.Errorf("VALUES不一致: got %v", ins.Values)
	}
}

func TestParse_CreateTable(t *testing.T) {
	stmt, err := parser.Parse("CREATE TABLE orders (id INT PRIMARY KEY, user_id INT, memo VARCHAR(20))")
	if err != nil {
		t.Fatal(err)
	}
	ct := stmt.(*parser.CreateTableStatement)
	want := []parser.ColumnDef{
		{Name: "ID", Type: "INT", PrimaryKey: true},
		{Name: "USER_ID", Type: "INT"},
		{Name: "MEMO", Type: "VARCHAR", Length: 20},
	}
	if len(ct.Columns) != len(want) {
		t.Fatalf("カラム数不一致: got %d", len(ct.Columns))
	}
	for i := range want {
		if ct.Columns[i] != want[i] {
			t.Errorf("カラム%d: want %+v, got %+v", i, want[i], ct.Columns[i])
		}
	}
}

func TestParse_TransactionStatements(t *testing.T) {
	tests := map[string]parser.Statement{
		"BEGIN":     &parser.BeginStatement{},
		"commit;":   &parser.CommitStatement{},
		"ROLLBACK;": &parser.RollbackStatement{},
	}
	for input, want := range tests {
		stmt, err := parser.Parse(input)
		if err != nil {
			t.Fatalf("%s: %v", input, err)
		}
		if fmt.Sprintf("%T", stmt) != fmt.Sprintf("%T", want) {
			t.Errorf("%s: want %T, got %T", input, want, stmt)
		}
	}
}

func TestParse_Errors(t *testing.T) {
	inputs := []string{
		"SELECT * FROM",                          // テーブル名がない
		"SELECT * FROM t WHERE a = 1 b = 2",      // 余計なトークンが残る
		"INSERT INTO t (a, b VALUES (1, 2)",      // 閉じ括弧がない（無限ループしない）
		"INSERT INTO t VALUES (1, 2",             // 閉じ括弧がない
		"CREATE TABLE t (id TEXT)",               // 未対応の型
		"SELECT * FROM t WHERE name = 'unclosed", // 閉じていない文字列
		"DELETE FROM t",                          // 未対応の文
	}
	for _, input := range inputs {
		if _, err := parser.Parse(input); err == nil {
			t.Errorf("エラーになるはず: %s", input)
		} else {
			t.Logf("%-45s → %v", input, err)
		}
	}
}
