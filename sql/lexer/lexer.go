// sql/lexer/lexer.go
package lexer

import "strings"

// Lexer はSQL文字列を先頭から読み、トークンを1つずつ切り出す
type Lexer struct {
	input   string
	pos     int  // 現在の文字の位置
	readPos int  // 次に読む文字の位置
	ch      byte // 現在の文字（入力の終わりでは0）
}

// New は新しいLexerを作る
func New(input string) *Lexer {
	l := &Lexer{input: input}
	l.readChar()
	return l
}

// Tokenize はSQL文字列全体をトークン列に変換する（末尾は必ずEOF）
func Tokenize(input string) []Token {
	l := New(input)
	var tokens []Token
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == EOF {
			return tokens
		}
	}
}

// NextToken は次のトークンを返す
func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	var tok Token
	switch l.ch {
	case 0:
		return Token{EOF, ""}
	case '*':
		tok = Token{ASTERISK, "*"}
	case ',':
		tok = Token{COMMA, ","}
	case ';':
		tok = Token{SEMICOLON, ";"}
	case '(':
		tok = Token{LPAREN, "("}
	case ')':
		tok = Token{RPAREN, ")"}
	case '.':
		tok = Token{DOT, "."}
	case '=':
		tok = Token{EQ, "="}
	case '<':
		switch l.peekChar() {
		case '=':
			l.readChar()
			tok = Token{LTE, "<="}
		case '>':
			l.readChar()
			tok = Token{NEQ, "<>"}
		default:
			tok = Token{LT, "<"}
		}
	case '>':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{GTE, ">="}
		} else {
			tok = Token{GT, ">"}
		}
	case '!':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{NEQ, "!="}
		} else {
			tok = Token{ILLEGAL, "!"}
		}
	case '\'':
		return l.readString()
	default:
		if isLetter(l.ch) {
			ident := l.readIdentifier()
			return Token{LookupIdent(ident), ident}
		}
		if isDigit(l.ch) {
			return Token{INT, l.readNumber()}
		}
		tok = Token{ILLEGAL, string(l.ch)}
	}

	l.readChar()
	return tok
}

func (l *Lexer) readChar() {
	if l.readPos >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPos]
	}
	l.pos = l.readPos
	l.readPos++
}

func (l *Lexer) peekChar() byte {
	if l.readPos >= len(l.input) {
		return 0
	}
	return l.input[l.readPos]
}

func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		l.readChar()
	}
}

// readIdentifier は識別子を読み、大文字に揃えて返す
// （テーブル名・カラム名は大文字小文字を区別しない）
func (l *Lexer) readIdentifier() string {
	start := l.pos
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return strings.ToUpper(l.input[start:l.pos])
}

func (l *Lexer) readNumber() string {
	start := l.pos
	for isDigit(l.ch) {
		l.readChar()
	}
	return l.input[start:l.pos]
}

// readString はシングルクォートで囲まれた文字列を読む（大文字化はしない）
func (l *Lexer) readString() Token {
	l.readChar() // 開きクォートを読み飛ばす
	start := l.pos
	for l.ch != '\'' {
		if l.ch == 0 {
			return Token{ILLEGAL, l.input[start-1 : l.pos]} // 閉じられていない文字列
		}
		l.readChar()
	}
	str := l.input[start:l.pos]
	l.readChar() // 閉じクォートを読み飛ばす
	return Token{STRING, str}
}

// isLetter は識別子に使える文字（ASCIIの英字とアンダースコア）かを判定する
func isLetter(ch byte) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}
