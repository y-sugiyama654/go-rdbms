// sql/lexer/lexer_test.go
package lexer_test

import (
	"testing"

	"github.com/yourname/go-rdbms/sql/lexer"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  []lexer.Token
	}{
		{
			"SELECT id, name FROM users WHERE id >= 10",
			[]lexer.Token{
				{Type: lexer.SELECT, Literal: "SELECT"},
				{Type: lexer.IDENT, Literal: "ID"},
				{Type: lexer.COMMA, Literal: ","},
				{Type: lexer.IDENT, Literal: "NAME"},
				{Type: lexer.FROM, Literal: "FROM"},
				{Type: lexer.IDENT, Literal: "USERS"},
				{Type: lexer.WHERE, Literal: "WHERE"},
				{Type: lexer.IDENT, Literal: "ID"},
				{Type: lexer.GTE, Literal: ">="},
				{Type: lexer.INT, Literal: "10"},
				{Type: lexer.EOF, Literal: ""},
			},
		},
		{
			"insert into t values ('Alice', 42);",
			[]lexer.Token{
				{Type: lexer.INSERT, Literal: "INSERT"},
				{Type: lexer.INTO, Literal: "INTO"},
				{Type: lexer.IDENT, Literal: "T"},
				{Type: lexer.VALUES, Literal: "VALUES"},
				{Type: lexer.LPAREN, Literal: "("},
				{Type: lexer.STRING, Literal: "Alice"}, // 文字列リテラルは大文字にしない
				{Type: lexer.COMMA, Literal: ","},
				{Type: lexer.INT, Literal: "42"},
				{Type: lexer.RPAREN, Literal: ")"},
				{Type: lexer.SEMICOLON, Literal: ";"},
				{Type: lexer.EOF, Literal: ""},
			},
		},
		{
			"a <> b != c <= d.e_1",
			[]lexer.Token{
				{Type: lexer.IDENT, Literal: "A"},
				{Type: lexer.NEQ, Literal: "<>"},
				{Type: lexer.IDENT, Literal: "B"},
				{Type: lexer.NEQ, Literal: "!="},
				{Type: lexer.IDENT, Literal: "C"},
				{Type: lexer.LTE, Literal: "<="},
				{Type: lexer.IDENT, Literal: "D"},
				{Type: lexer.DOT, Literal: "."},
				{Type: lexer.IDENT, Literal: "E_1"},
				{Type: lexer.EOF, Literal: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := lexer.Tokenize(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("トークン数不一致: want %d, got %d (%v)", len(tt.want), len(got), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("%d番目: want %+v, got %+v", i, tt.want[i], got[i])
				}
			}
		})
	}
}

func TestTokenize_UnterminatedString(t *testing.T) {
	tokens := lexer.Tokenize("SELECT 'abc")
	if tokens[1].Type != lexer.ILLEGAL {
		t.Errorf("閉じていない文字列はILLEGALになるはず: got %v", tokens[1])
	}
}
