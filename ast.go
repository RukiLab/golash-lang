// HSP後継言語のAST定義。
package main

import (
	"fmt"
	"strings"
)

// Posはソース位置（1始まり、File設定時はファイル内位置）。
type Pos struct {
	File   string
	Line   int
	Column int
}

func (p Pos) String() string {
	if p.File != "" {
		return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Column)
	}
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// NodeはすべてのASTノードの基底。
type Node interface {
	Pos() Pos
	String() string
}

// StmtとExprのマーカーインターフェース。
type Stmt interface {
	Node
	stmtNode()
}
type Expr interface {
	Node
	exprNode()
}

// Programは翻訳単位。
type Program struct {
	Stmts []Stmt
	At    Pos
}

func (n *Program) Pos() Pos { return n.At }
func (n *Program) String() string {
	var sb strings.Builder
	for _, s := range n.Stmts {
		sb.WriteString(s.String())
		sb.WriteString("\n")
	}
	return sb.String()
}

// ---------- 文 ----------

type BlockStmt struct {
	Stmts []Stmt
	At    Pos
}

func (n *BlockStmt) Pos() Pos  { return n.At }
func (n *BlockStmt) stmtNode() {}
func (n *BlockStmt) String() string {
	var sb strings.Builder
	sb.WriteString("{")
	if len(n.Stmts) > 0 {
		sb.WriteString("\n")
		for _, s := range n.Stmts {
			for _, line := range strings.Split(strings.TrimSuffix(s.String(), "\n"), "\n") {
				sb.WriteString("  " + line + "\n")
			}
		}
		sb.WriteString("}")
	} else {
		sb.WriteString("}")
	}
	return sb.String()
}

type AssignStmt struct {
	Target Expr
	Value  Expr
	At     Pos
}

func (n *AssignStmt) Pos() Pos  { return n.At }
func (n *AssignStmt) stmtNode() {}
func (n *AssignStmt) String() string {
	return fmt.Sprintf("%s = %s", n.Target.String(), n.Value.String())
}

// CompoundAssignStmtは `target op= value`（例 `x += 1`）。旧来の
// 糖衣構文展開とは異なり、代入先アドレスはちょうど1回だけ評価される。
type CompoundAssignStmt struct {
	Target Expr
	Op     TokenType // 二項演算子（TokPlus、TokMinus、…）
	Value  Expr
	At     Pos
}

func (n *CompoundAssignStmt) Pos() Pos  { return n.At }
func (n *CompoundAssignStmt) stmtNode() {}
func (n *CompoundAssignStmt) String() string {
	return fmt.Sprintf("%s %s= %s", n.Target.String(), opSymbol(n.Op), n.Value.String())
}

type ExprStmt struct {
	X  Expr
	At Pos
}

func (n *ExprStmt) Pos() Pos  { return n.At }
func (n *ExprStmt) stmtNode() {}
func (n *ExprStmt) String() string {
	return n.X.String()
}

type IfStmt struct {
	Cond Expr
	Then *BlockStmt
	Else Stmt // nil、*BlockStmt、または*IfStmt（else if用）
	At   Pos
}

func (n *IfStmt) Pos() Pos  { return n.At }
func (n *IfStmt) stmtNode() {}
func (n *IfStmt) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("if %s %s", n.Cond.String(), n.Then.String()))
	if e, ok := n.Else.(*IfStmt); ok {
		sb.WriteString(" else " + e.String())
	} else if n.Else != nil {
		sb.WriteString(" else " + n.Else.String())
	}
	return sb.String()
}

type RepeatStmt struct {
	Count   Expr
	Var     string // カウンタ／インデックス名。ない場合は空
	HasVar  bool
	Item    string // 要素名（`as i, item`）。ない場合は空
	HasItem bool
	Body    *BlockStmt
	At      Pos
}

func (n *RepeatStmt) Pos() Pos  { return n.At }
func (n *RepeatStmt) stmtNode() {}
func (n *RepeatStmt) String() string {
	if n.HasVar {
		if n.HasItem {
			return fmt.Sprintf("repeat %s as %s, %s %s", n.Count.String(), n.Var, n.Item, n.Body.String())
		}
		return fmt.Sprintf("repeat %s as %s %s", n.Count.String(), n.Var, n.Body.String())
	}
	return fmt.Sprintf("repeat %s %s", n.Count.String(), n.Body.String())
}

type WhileStmt struct {
	Cond Expr
	Body *BlockStmt
	At   Pos
}

func (n *WhileStmt) Pos() Pos  { return n.At }
func (n *WhileStmt) stmtNode() {}
func (n *WhileStmt) String() string {
	return fmt.Sprintf("while %s %s", n.Cond.String(), n.Body.String())
}

// SwitchCaseは `case v, ... { }` または `default { }` の1分岐。
// default分岐はValuesを持たない。
type SwitchCase struct {
	Values  []Expr
	Body    *BlockStmt
	Default bool
	At      Pos
}

type SwitchStmt struct {
	Value Expr
	Cases []SwitchCase
	At    Pos
}

func (n *SwitchStmt) Pos() Pos  { return n.At }
func (n *SwitchStmt) stmtNode() {}
func (n *SwitchStmt) String() string {
	var sb strings.Builder
	sb.WriteString("switch " + n.Value.String() + " {")
	for _, c := range n.Cases {
		sb.WriteString("\n")
		if c.Default {
			sb.WriteString("default ")
		} else {
			vs := make([]string, len(c.Values))
			for i, v := range c.Values {
				vs[i] = v.String()
			}
			sb.WriteString("case " + strings.Join(vs, ", ") + " ")
		}
		sb.WriteString(c.Body.String())
	}
	sb.WriteString("\n}")
	return sb.String()
}

type BreakStmt struct{ At Pos }

func (n *BreakStmt) Pos() Pos  { return n.At }
func (n *BreakStmt) stmtNode() {}
func (n *BreakStmt) String() string {
	return "break"
}

type ContinueStmt struct{ At Pos }

func (n *ContinueStmt) Pos() Pos  { return n.At }
func (n *ContinueStmt) stmtNode() {}
func (n *ContinueStmt) String() string {
	return "continue"
}

type ReturnStmt struct {
	Value Expr // 素の `return` のときはnil
	At    Pos
}

func (n *ReturnStmt) Pos() Pos  { return n.At }
func (n *ReturnStmt) stmtNode() {}
func (n *ReturnStmt) String() string {
	if n.Value == nil {
		return "return"
	}
	return fmt.Sprintf("return %s", n.Value.String())
}

type Param struct {
	Name string
	At   Pos
}

type DefStmt struct {
	Name   string
	Params []Param
	Body   *BlockStmt
	At     Pos
}

func (n *DefStmt) Pos() Pos  { return n.At }
func (n *DefStmt) stmtNode() {}
func (n *DefStmt) String() string {
	names := make([]string, len(n.Params))
	for i, p := range n.Params {
		names[i] = p.Name
	}
	return fmt.Sprintf("def %s(%s) %s", n.Name, strings.Join(names, ", "), n.Body.String())
}

// ---------- 式 ----------

type IntLit struct {
	Raw   string
	Value int64
	At    Pos
}

func (n *IntLit) Pos() Pos       { return n.At }
func (n *IntLit) exprNode()      {}
func (n *IntLit) String() string { return n.Raw }

type FloatLit struct {
	Raw   string
	Value float64
	At    Pos
}

func (n *FloatLit) Pos() Pos       { return n.At }
func (n *FloatLit) exprNode()      {}
func (n *FloatLit) String() string { return n.Raw }

type StringLit struct {
	Value string
	At    Pos
}

func (n *StringLit) Pos() Pos  { return n.At }
func (n *StringLit) exprNode() {}
func (n *StringLit) String() string {
	return fmt.Sprintf("%q", n.Value)
}

type BoolLit struct {
	Value bool
	At    Pos
}

func (n *BoolLit) Pos() Pos  { return n.At }
func (n *BoolLit) exprNode() {}
func (n *BoolLit) String() string {
	if n.Value {
		return "true"
	}
	return "false"
}

// NullLitはnull値。リテラル構文はなく、パーサーが
// 省略された呼び出し引数（f(a, , b)）用に生成する。
type NullLit struct {
	At Pos
}

func (n *NullLit) Pos() Pos       { return n.At }
func (n *NullLit) exprNode()      {}
func (n *NullLit) String() string { return "null" }

type VarExpr struct {
	Name string
	At   Pos
	// Paren は `(x)` のように括弧で囲まれていた場合に真。
	// 代入先としては認めない（`(x)=1` はエラー）。
	Paren bool
}

func (n *VarExpr) Pos() Pos       { return n.At }
func (n *VarExpr) exprNode()      {}
func (n *VarExpr) String() string { return n.Name }

type ArrayLit struct {
	Elems []Expr
	At    Pos
}

func (n *ArrayLit) Pos() Pos  { return n.At }
func (n *ArrayLit) exprNode() {}
func (n *ArrayLit) String() string {
	parts := make([]string, len(n.Elems))
	for i, e := range n.Elems {
		parts[i] = e.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

type IndexExpr struct {
	Base  Expr
	Index Expr
	At    Pos // '[' の位置
	// Paren は `(a[0])` のように全体が括弧で囲まれていた場合に真。
	// 代入先としては認めない。
	Paren bool
}

func (n *IndexExpr) Pos() Pos  { return n.At }
func (n *IndexExpr) exprNode() {}
func (n *IndexExpr) String() string {
	return fmt.Sprintf("%s[%s]", n.Base.String(), n.Index.String())
}

type CallExpr struct {
	Callee Expr
	Args   []Expr
	At     Pos // '(' の位置
}

func (n *CallExpr) Pos() Pos  { return n.At }
func (n *CallExpr) exprNode() {}
func (n *CallExpr) String() string {
	parts := make([]string, len(n.Args))
	for i, a := range n.Args {
		parts[i] = a.String()
	}
	return fmt.Sprintf("%s(%s)", n.Callee.String(), strings.Join(parts, ", "))
}

type UnaryExpr struct {
	Op TokenType
	X  Expr
	At Pos
}

func (n *UnaryExpr) Pos() Pos  { return n.At }
func (n *UnaryExpr) exprNode() {}
func (n *UnaryExpr) String() string {
	return fmt.Sprintf("(%s%s)", opSymbol(n.Op), n.X.String())
}

type BinaryExpr struct {
	Op TokenType
	L  Expr
	R  Expr
	At Pos // 演算子の位置
}

func (n *BinaryExpr) Pos() Pos  { return n.At }
func (n *BinaryExpr) exprNode() {}
func (n *BinaryExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", n.L.String(), opSymbol(n.Op), n.R.String())
}

// opSymbolは演算子トークンをソース上の記号として描画する（構文木ダンプ用）。
func opSymbol(op TokenType) string {
	switch op {
	case TokPlus:
		return "+"
	case TokMinus:
		return "-"
	case TokStar:
		return "*"
	case TokSlash:
		return "/"
	case TokMod:
		return "%"
	case TokEq:
		return "=="
	case TokNotEq:
		return "!="
	case TokLt:
		return "<"
	case TokLtEq:
		return "<="
	case TokGt:
		return ">"
	case TokGtEq:
		return ">="
	case TokAnd:
		return "&&"
	case TokOr:
		return "||"
	case TokBitAnd:
		return "&"
	case TokBitOr:
		return "|"
	case TokBitXor:
		return "^"
	case TokBitNot:
		return "~"
	case TokShl:
		return "<<"
	case TokShr:
		return ">>"
	case TokBang:
		return "!"
	}
	return string(op)
}
