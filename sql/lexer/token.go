// sql/lexer/token.go
package lexer

import "strings"

// TokenType はトークンの種別
type TokenType int

const (
	// 特殊
	EOF     TokenType = iota // 入力の終わり
	ILLEGAL                  // 不正な文字

	// リテラル
	IDENT  // 識別子: users, id など
	INT    // 整数リテラル: 1, 42
	STRING // 文字列リテラル: 'Alice'

	// キーワード
	SELECT
	FROM
	WHERE
	INSERT
	INTO
	VALUES
	CREATE
	TABLE
	INT_TYPE // 型名の INT
	VARCHAR
	PRIMARY
	KEY
	AND
	OR
	NOT
	JOIN
	ON
	BEGIN
	COMMIT
	ROLLBACK

	// 記号・演算子
	ASTERISK  // *
	COMMA     // ,
	SEMICOLON // ;
	LPAREN    // (
	RPAREN    // )
	DOT       // .
	EQ        // =
	NEQ       // <> または !=
	LT        // <
	GT        // >
	LTE       // <=
	GTE       // >=
)

var tokenNames = map[TokenType]string{
	EOF: "EOF", ILLEGAL: "ILLEGAL", IDENT: "識別子", INT: "整数", STRING: "文字列",
	ASTERISK: "*", COMMA: ",", SEMICOLON: ";", LPAREN: "(", RPAREN: ")", DOT: ".",
	EQ: "=", NEQ: "<>", LT: "<", GT: ">", LTE: "<=", GTE: ">=",
}

// keywords はキーワード（大文字）からTokenTypeへの対応表
var keywords = map[string]TokenType{
	"SELECT": SELECT, "FROM": FROM, "WHERE": WHERE,
	"INSERT": INSERT, "INTO": INTO, "VALUES": VALUES,
	"CREATE": CREATE, "TABLE": TABLE,
	"INT": INT_TYPE, "VARCHAR": VARCHAR, "PRIMARY": PRIMARY, "KEY": KEY,
	"AND": AND, "OR": OR, "NOT": NOT,
	"JOIN": JOIN, "ON": ON,
	"BEGIN": BEGIN, "COMMIT": COMMIT, "ROLLBACK": ROLLBACK,
}

func init() {
	for word, tt := range keywords {
		tokenNames[tt] = word
	}
}

// String はエラーメッセージなどで使うトークン種別の表示名を返す
func (tt TokenType) String() string {
	if name, ok := tokenNames[tt]; ok {
		return name
	}
	return "UNKNOWN"
}

// Token はトークンの種別と元の文字列を持つ
type Token struct {
	Type    TokenType
	Literal string
}

// LookupIdent は識別子がキーワードならそのTokenTypeを、そうでなければIDENTを返す
func LookupIdent(ident string) TokenType {
	if tt, ok := keywords[strings.ToUpper(ident)]; ok {
		return tt
	}
	return IDENT
}
