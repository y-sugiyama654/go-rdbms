// sql/executor/eval.go
package executor

import (
	"fmt"

	"github.com/yourname/go-rdbms/sql/parser"
)

// evalValue はカラム参照またはリテラルを評価して値を返す
func evalValue(expr parser.Expression, tuple *Tuple, cols []OutputColumn) (Value, error) {
	switch e := expr.(type) {
	case *parser.ColumnRef:
		idx, err := resolveColumn(cols, e)
		if err != nil {
			return Value{}, err
		}
		return tuple.Values[idx], nil
	case *parser.IntLiteral:
		return NewInt(e.Value), nil
	case *parser.StringLiteral:
		return NewString(e.Value), nil
	default:
		return Value{}, fmt.Errorf("executor: %s は値として評価できません", expr)
	}
}

// evalPredicate は条件式を評価して真偽を返す
func evalPredicate(expr parser.Expression, tuple *Tuple, cols []OutputColumn) (bool, error) {
	switch e := expr.(type) {
	case *parser.NotExpr:
		v, err := evalPredicate(e.Expr, tuple, cols)
		return !v, err
	case *parser.BinaryExpr:
		switch e.Op {
		case "AND", "OR":
			left, err := evalPredicate(e.Left, tuple, cols)
			if err != nil {
				return false, err
			}
			// 短絡評価: 左辺だけで結果が決まれば右辺は評価しない
			if e.Op == "AND" && !left || e.Op == "OR" && left {
				return left, nil
			}
			return evalPredicate(e.Right, tuple, cols)
		}

		left, err := evalValue(e.Left, tuple, cols)
		if err != nil {
			return false, err
		}
		right, err := evalValue(e.Right, tuple, cols)
		if err != nil {
			return false, err
		}
		c, err := Compare(left, right)
		if err != nil {
			return false, err
		}
		switch e.Op {
		case "=":
			return c == 0, nil
		case "<>":
			return c != 0, nil
		case "<":
			return c < 0, nil
		case ">":
			return c > 0, nil
		case "<=":
			return c <= 0, nil
		case ">=":
			return c >= 0, nil
		}
	}
	return false, fmt.Errorf("executor: %s は条件式ではありません", expr)
}
