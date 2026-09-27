// sql/parser/parser.go
package parser

import (
	"fmt"
	"strconv"

	"github.com/yourname/go-rdbms/sql/lexer"
)

// Parser はトークン列を先頭から読み、ASTを組み立てる（再帰下降構文解析）
type Parser struct {
	tokens []lexer.Token
	pos    int
}

// Parse はSQL文字列を1文ぶん解析してASTを返す
func Parse(input string) (Statement, error) {
	p := &Parser{tokens: lexer.Tokenize(input)}

	stmt, err := p.parseStatement()
	if err != nil {
		return nil, err
	}
	// 末尾のセミコロンは省略可。その後に何か残っていたらエラーにする
	if p.current().Type == lexer.SEMICOLON {
		p.advance()
	}
	if p.current().Type != lexer.EOF {
		return nil, p.unexpected()
	}
	return stmt, nil
}

func (p *Parser) parseStatement() (Statement, error) {
	switch p.current().Type {
	case lexer.SELECT:
		return p.parseSelect()
	case lexer.INSERT:
		return p.parseInsert()
	case lexer.CREATE:
		return p.parseCreateTable()
	case lexer.BEGIN:
		p.advance()
		return &BeginStatement{}, nil
	case lexer.COMMIT:
		p.advance()
		return &CommitStatement{}, nil
	case lexer.ROLLBACK:
		p.advance()
		return &RollbackStatement{}, nil
	default:
		return nil, fmt.Errorf("parser: 未対応の文です: %q", p.current().Literal)
	}
}

// ---- SELECT ----
// SELECT (* | カラム, ...) FROM テーブル [JOIN テーブル ON 式]... [WHERE 式]

func (p *Parser) parseSelect() (*SelectStatement, error) {
	p.advance() // SELECT
	stmt := &SelectStatement{}

	if p.current().Type == lexer.ASTERISK {
		p.advance()
	} else {
		for {
			col, err := p.parseColumnRef()
			if err != nil {
				return nil, err
			}
			stmt.Columns = append(stmt.Columns, col)
			if p.current().Type != lexer.COMMA {
				break
			}
			p.advance()
		}
	}

	if _, err := p.expect(lexer.FROM); err != nil {
		return nil, err
	}
	table, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	stmt.From = table

	for p.current().Type == lexer.JOIN {
		p.advance()
		joinTable, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.ON); err != nil {
			return nil, err
		}
		on, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		stmt.Joins = append(stmt.Joins, JoinClause{Table: joinTable, On: on})
	}

	if p.current().Type == lexer.WHERE {
		p.advance()
		where, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}
	return stmt, nil
}

// ---- INSERT ----
// INSERT INTO テーブル [(カラム, ...)] VALUES (リテラル, ...)

func (p *Parser) parseInsert() (*InsertStatement, error) {
	p.advance() // INSERT
	if _, err := p.expect(lexer.INTO); err != nil {
		return nil, err
	}
	table, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	stmt := &InsertStatement{Table: table}

	if p.current().Type == lexer.LPAREN {
		p.advance()
		for {
			col, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			stmt.Columns = append(stmt.Columns, col)
			if p.current().Type != lexer.COMMA {
				break
			}
			p.advance()
		}
		if _, err := p.expect(lexer.RPAREN); err != nil {
			return nil, err
		}
	}

	if _, err := p.expect(lexer.VALUES); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.LPAREN); err != nil {
		return nil, err
	}
	for {
		val, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		stmt.Values = append(stmt.Values, val)
		if p.current().Type != lexer.COMMA {
			break
		}
		p.advance()
	}
	if _, err := p.expect(lexer.RPAREN); err != nil {
		return nil, err
	}
	return stmt, nil
}

// ---- CREATE TABLE ----
// CREATE TABLE テーブル (カラム定義, ...)

func (p *Parser) parseCreateTable() (*CreateTableStatement, error) {
	p.advance() // CREATE
	if _, err := p.expect(lexer.TABLE); err != nil {
		return nil, err
	}
	table, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	stmt := &CreateTableStatement{Table: table}

	if _, err := p.expect(lexer.LPAREN); err != nil {
		return nil, err
	}
	for {
		col, err := p.parseColumnDef()
		if err != nil {
			return nil, err
		}
		stmt.Columns = append(stmt.Columns, col)
		if p.current().Type != lexer.COMMA {
			break
		}
		p.advance()
	}
	if _, err := p.expect(lexer.RPAREN); err != nil {
		return nil, err
	}
	return stmt, nil
}

// カラム名 (INT | VARCHAR(n)) [PRIMARY KEY]
func (p *Parser) parseColumnDef() (ColumnDef, error) {
	name, err := p.expectIdent()
	if err != nil {
		return ColumnDef{}, err
	}
	col := ColumnDef{Name: name}

	switch p.current().Type {
	case lexer.INT_TYPE:
		p.advance()
		col.Type = "INT"
	case lexer.VARCHAR:
		p.advance()
		if _, err := p.expect(lexer.LPAREN); err != nil {
			return col, err
		}
		tok, err := p.expect(lexer.INT)
		if err != nil {
			return col, err
		}
		n, err := strconv.Atoi(tok.Literal)
		if err != nil || n <= 0 {
			return col, fmt.Errorf("parser: VARCHARの長さが不正です: %s", tok.Literal)
		}
		if _, err := p.expect(lexer.RPAREN); err != nil {
			return col, err
		}
		col.Type = "VARCHAR"
		col.Length = n
	default:
		return col, fmt.Errorf("parser: 未対応の型です: %q", p.current().Literal)
	}

	if p.current().Type == lexer.PRIMARY {
		p.advance()
		if _, err := p.expect(lexer.KEY); err != nil {
			return col, err
		}
		col.PrimaryKey = true
	}
	return col, nil
}

// ---- 式 ----
// 優先順位の低い順に関数を分け、低い方から高い方を呼び出す。
//
//	expression := or
//	or         := and { OR and }
//	and        := not { AND not }
//	not        := NOT not | comparison
//	comparison := primary [ (= | <> | < | > | <= | >=) primary ]
//	primary    := 整数 | 文字列 | カラム参照 | "(" expression ")"

func (p *Parser) parseExpression() (Expression, error) {
	return p.parseOr()
}

func (p *Parser) parseOr() (Expression, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.current().Type == lexer.OR {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseAnd() (Expression, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.current().Type == lexer.AND {
		p.advance()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseNot() (Expression, error) {
	if p.current().Type == lexer.NOT {
		p.advance()
		expr, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &NotExpr{Expr: expr}, nil
	}
	return p.parseComparison()
}

var comparisonOps = map[lexer.TokenType]string{
	lexer.EQ: "=", lexer.NEQ: "<>", lexer.LT: "<", lexer.GT: ">", lexer.LTE: "<=", lexer.GTE: ">=",
}

func (p *Parser) parseComparison() (Expression, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	op, ok := comparisonOps[p.current().Type]
	if !ok {
		return left, nil
	}
	p.advance()
	right, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	return &BinaryExpr{Op: op, Left: left, Right: right}, nil
}

func (p *Parser) parsePrimary() (Expression, error) {
	switch p.current().Type {
	case lexer.INT, lexer.STRING:
		return p.parseLiteral()
	case lexer.IDENT:
		return p.parseColumnRef()
	case lexer.LPAREN:
		p.advance()
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RPAREN); err != nil {
			return nil, err
		}
		return expr, nil
	default:
		return nil, p.unexpected()
	}
}

func (p *Parser) parseLiteral() (Expression, error) {
	tok := p.current()
	switch tok.Type {
	case lexer.INT:
		p.advance()
		v, err := strconv.ParseInt(tok.Literal, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parser: 整数に変換できません: %s", tok.Literal)
		}
		return &IntLiteral{Value: v}, nil
	case lexer.STRING:
		p.advance()
		return &StringLiteral{Value: tok.Literal}, nil
	default:
		return nil, p.unexpected()
	}
}

// カラム名 または テーブル名.カラム名
func (p *Parser) parseColumnRef() (*ColumnRef, error) {
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	if p.current().Type != lexer.DOT {
		return &ColumnRef{Name: name}, nil
	}
	p.advance()
	col, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	return &ColumnRef{Table: name, Name: col}, nil
}

// ---- ヘルパー ----

func (p *Parser) current() lexer.Token {
	if p.pos >= len(p.tokens) {
		return lexer.Token{Type: lexer.EOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

// expect は現在のトークンがttなら読み進めて返し、違えばエラーを返す
func (p *Parser) expect(tt lexer.TokenType) (lexer.Token, error) {
	tok := p.current()
	if tok.Type != tt {
		return tok, fmt.Errorf("parser: %s を期待しましたが %s でした", tt, describe(tok))
	}
	p.advance()
	return tok, nil
}

func (p *Parser) expectIdent() (string, error) {
	tok, err := p.expect(lexer.IDENT)
	return tok.Literal, err
}

func (p *Parser) unexpected() error {
	if tok := p.current(); tok.Type == lexer.ILLEGAL {
		return fmt.Errorf("parser: 不正な文字があります: %q", tok.Literal)
	}
	return fmt.Errorf("parser: 予期しないトークンです: %s", describe(p.current()))
}

func describe(tok lexer.Token) string {
	if tok.Type == lexer.EOF {
		return "文の終わり"
	}
	return fmt.Sprintf("%q", tok.Literal)
}
