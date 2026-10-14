// HSP後継言語のための再帰下降パーサ。
package main

import (
	"fmt"
	"strconv"
)

// ParseError は位置情報付きの構文エラー。
type ParseError struct {
	File   string
	Line   int
	Column int
	Msg    string
}

func (e *ParseError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Msg)
	}
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Msg)
}

// Parse は src をトークン化して Program に構文解析する。
func Parse(src string) (*Program, error) {
	toks, err := Lex(src)
	if err != nil {
		return nil, err
	}
	return ParseTokens(toks)
}

// ParseTokens は明示的なトークン列を構文解析する（ParseFile 後の
// #include 展開済みトークン用）。
func ParseTokens(toks []Token) (*Program, error) {
	p := &parser{toks: toks}
	return p.parseProgram()
}

// ParseFile はスクリプトファイルを読み込み、#include ツリーを展開して構文解析する。
func ParseFile(path string) (*Program, error) {
	prog, _, err := ParseFileMode(path)
	return prog, err
}

// ParseFileMode は ParseFile に実行モードを加えたもの：ツリー内の最後に有効な
// `#mode cli|gui`、存在しなければ "gui"。
func ParseFileMode(path string) (*Program, string, error) {
	toks, mode, err := CombineFileMode(path)
	if err != nil {
		return nil, "gui", err
	}
	prog, err := ParseTokens(toks)
	if err != nil {
		return nil, mode, err
	}
	return prog, mode, nil
}

type parser struct {
	toks []Token
	pos  int
	// ブロックと式のネスト深さ（maxParseDepth で上限を設け、
	// 異常な入力でパーサやツリーウォーク型インタプリタのホスト
	// スタックが溢れないようにする）。
	depth int
}

// maxParseDepth は `{...}` ブロックのネストと式のネスト
//（括弧・配列/呼び出しのネスト・単項演算子の連鎖）の上限。
const maxParseDepth = 1000

func (p *parser) enter() error {
	p.depth++
	if p.depth > maxParseDepth {
		return p.errAt(p.peek(), "ネストが深すぎます（上限 %d）", maxParseDepth)
	}
	return nil
}

func (p *parser) leave() { p.depth-- }

func (p *parser) peek() Token {
	if p.pos >= len(p.toks) {
		return Token{Type: TokEOF}
	}
	return p.toks[p.pos]
}

func (p *parser) peekAt(n int) Token {
	if p.pos+n >= len(p.toks) {
		return Token{Type: TokEOF}
	}
	return p.toks[p.pos+n]
}

func (p *parser) next() Token {
	t := p.peek()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *parser) errAt(t Token, format string, args ...any) error {
	return &ParseError{File: t.File, Line: t.Line, Column: t.Column, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) expect(tt TokenType) (Token, error) {
	t := p.peek()
	if t.Type != tt {
		return Token{}, p.errAt(t, "%s が必要です。%s が見つかりました", tokName(tt), tokName(t.Type))
	}
	return p.next(), nil
}

// skipNewlines は NEWLINE トークンを消費し、1つでも見つかれば true を返す。
func (p *parser) skipNewlines() bool {
	found := false
	for p.peek().Type == TokNewline {
		p.next()
		found = true
	}
	return found
}

func tokName(t TokenType) string {
	switch t {
	case TokEOF:
		return "入力の終端"
	case TokNewline:
		return "改行"
	case TokSemicolon:
		return "';'"
	case TokIdent:
		return "識別子"
	case TokInt:
		return "整数"
	case TokFloat:
		return "小数"
	case TokString:
		return "文字列"
	case TokTrue:
		return "'true'"
	case TokFalse:
		return "'false'"
	case TokDef:
		return "'def'"
	case TokIf:
		return "'if'"
	case TokElse:
		return "'else'"
	case TokRepeat:
		return "'repeat'"
	case TokWhile:
		return "'while'"
	case TokSwitch:
		return "'switch'"
	case TokCase:
		return "'case'"
	case TokDefault:
		return "'default'"
	case TokAs:
		return "'as'"
	case TokBreak:
		return "'break'"
	case TokContinue:
		return "'continue'"
	case TokReturn:
		return "'return'"
	case TokPlus:
		return "'+'"
	case TokMinus:
		return "'-'"
	case TokStar:
		return "'*'"
	case TokSlash:
		return "'/'"
	case TokMod:
		return "'%'"
	case TokAssign:
		return "'='"
	case TokEq:
		return "'=='"
	case TokBang:
		return "'!'"
	case TokNotEq:
		return "'!='"
	case TokLt:
		return "'<'"
	case TokLtEq:
		return "'<='"
	case TokGt:
		return "'>'"
	case TokGtEq:
		return "'>='"
	case TokAnd:
		return "'&&'"
	case TokOr:
		return "'||'"
	case TokBitAnd:
		return "'&'"
	case TokBitOr:
		return "'|'"
	case TokBitXor:
		return "'^'"
	case TokBitNot:
		return "'~'"
	case TokShl:
		return "'<<'"
	case TokShr:
		return "'>>'"
	case TokComma:
		return "','"
	case TokColon:
		return "':'"
	case TokDot:
		return "'.'"
	case TokLParen:
		return "'('"
	case TokRParen:
		return "')'"
	case TokLBracket:
		return "'['"
	case TokRBracket:
		return "']'"
	case TokLBrace:
		return "'{'"
	case TokRBrace:
		return "'}'"
	}
	if s, ok := opDisplay(t); ok {
		return s
	}
	return fmt.Sprintf("%q", string(t))
}

// opDisplay は演算子トークンをソース上の記号として描画する。
func opDisplay(t TokenType) (string, bool) {
	switch t {
	case TokPlusAssign:
		return "'+='", true
	case TokMinusAssign:
		return "'-='", true
	case TokStarAssign:
		return "'*='", true
	case TokSlashAssign:
		return "'/='", true
	case TokModAssign:
		return "'%='", true
	case TokBitAndAssign:
		return "'&='", true
	case TokBitOrAssign:
		return "'|='", true
	case TokBitXorAssign:
		return "'^='", true
	case TokShlAssign:
		return "'<<='", true
	case TokShrAssign:
		return "'>>='", true
	}
	return "", false
}

// ---------- プログラム ----------

func (p *parser) parseProgram() (*Program, error) {
	prog := &Program{At: Pos{Line: 1, Column: 1}}
	p.skipNewlines()
	for p.peek().Type != TokEOF {
		if p.peek().Type == TokSemicolon {
			t := p.peek()
			return nil, p.errAt(t, "セミコロンは使用できません。文は改行で区切ってください")
		}
		s, err := p.parseStmt(true)
		if err != nil {
			return nil, err
		}
		prog.Stmts = append(prog.Stmts, s)
		// 文の終端。
		t := p.peek()
		switch t.Type {
		case TokNewline:
			p.skipNewlines()
		case TokEOF:
		case TokRBrace:
			// 外側のブロックに消費させる（'}' 前の改行欠落も許容される）。
		case TokSemicolon:
			return nil, p.errAt(t, "セミコロンは使用できません。文は改行で区切ってください")
		default:
			return nil, p.errAt(t, "文の後に改行が必要です。%s が見つかりました", tokName(t.Type))
		}
	}
	return prog, nil
}

// ---------- 文 ----------

func (p *parser) parseStmt(top bool) (Stmt, error) {
	t := p.peek()
	switch t.Type {
	case TokDef:
		// 入れ子関数は廃止された: def はトップレベルにのみ存在する
		// （関数本体は他のすべてのブロックと同様、top=false の
		// parseBlock を通る）。
		if !top {
			return nil, p.errAt(t, "def はトップレベルにのみ記述できます")
		}
		return p.parseDef()
	case TokIf:
		return p.parseIf()
	case TokRepeat:
		return p.parseRepeat()
	case TokWhile:
		return p.parseWhile()
	case TokSwitch:
		return p.parseSwitch()
	case TokBreak:
		p.next()
		return &BreakStmt{At: posOf(t)}, nil
	case TokContinue:
		p.next()
		return &ContinueStmt{At: posOf(t)}, nil
	case TokReturn:
		return p.parseReturn()
	case TokRBrace:
		return nil, p.errAt(t, "予期しない '}' です")
	case TokStar:
		return nil, p.errAt(t, "予期しない '*' です")
	case TokIdent:
		// `enum` は通常の識別子である（enum 文は廃止された。連番定数は
		// 値なしの #define を使う）。
		return p.parseAssignOrExpr()
	default:
		// 式文はリテラル、'('、'['、'-'、'!'、true/false で始まりうる。
		return p.parseAssignOrExpr()
	}
}

func (p *parser) parseBlock() (*BlockStmt, error) {
	open, err := p.expect(TokLBrace)
	if err != nil {
		return nil, err
	}
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	b := &BlockStmt{At: posOf(open)}
	p.skipNewlines()
	for p.peek().Type != TokRBrace {
		if p.peek().Type == TokEOF {
			return nil, p.errAt(p.peek(), "ブロックが閉じられていません。'}' が必要です")
		}
		if p.peek().Type == TokSemicolon {
			t := p.peek()
			return nil, p.errAt(t, "セミコロンは使用できません。文は改行で区切ってください")
		}
		s, err := p.parseStmt(false)
		if err != nil {
			return nil, err
		}
		b.Stmts = append(b.Stmts, s)
		t := p.peek()
		switch t.Type {
		case TokNewline:
			p.skipNewlines()
		case TokRBrace:
			// '}' 前の改行欠落を許容する。
		case TokEOF:
			return nil, p.errAt(t, "ブロックが閉じられていません。'}' が必要です")
		case TokSemicolon:
			return nil, p.errAt(t, "セミコロンは使用できません。文は改行で区切ってください")
		default:
			return nil, p.errAt(t, "文の後に改行が必要です。%s が見つかりました", tokName(t.Type))
		}
	}
	p.next() // '}'
	return b, nil
}

func (p *parser) parseDef() (Stmt, error) {
	kw := p.next() // def
	name, err := p.expect(TokIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TokLParen); err != nil {
		return nil, err
	}
	var params []Param
	p.skipNewlines()
	if p.peek().Type != TokRParen {
		for {
			p.skipNewlines()
			pt, err := p.expect(TokIdent)
			if err != nil {
				return nil, err
			}
			if isBuiltin(pt.Lit) {
				return nil, p.errAt(pt, "パラメータ %q は組み込み関数と同名のため使用できません", pt.Lit)
			}
			pm := Param{Name: pt.Lit, At: posOf(pt)}
			p.skipNewlines()
			if p.peek().Type == TokAssign {
				return nil, p.errAt(pt, "デフォルト引数は廃止されました")
			}
			params = append(params, pm)
			p.skipNewlines()
			if p.peek().Type == TokComma {
				p.next()
				continue
			}
			break
		}
	}
	p.skipNewlines()
	if _, err := p.expect(TokRParen); err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &DefStmt{Name: name.Lit, Params: params, Body: body, At: posOf(kw)}, nil
}

func (p *parser) parseIf() (Stmt, error) {
	kw := p.next() // if
	cond, err := p.parseOr(false)
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	stmt := &IfStmt{Cond: cond, Then: then, At: posOf(kw)}
	// `} else {` を1行で、または改行を挟んで書けるようにするか？ サンプルは
	// `} else {` を1行に置いている。else の前の改行も許容する。
	save := p.pos
	p.skipNewlines()
	if p.peek().Type != TokElse {
		p.pos = save
		return stmt, nil
	}
	p.next() // else
	p.skipNewlines()
	if p.peek().Type == TokIf {
		chained, err := p.parseIf()
		if err != nil {
			return nil, err
		}
		stmt.Else = chained
		return stmt, nil
	}
	blk, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	stmt.Else = blk
	return stmt, nil
}

func (p *parser) parseRepeat() (Stmt, error) {
	kw := p.next() // repeat
	count, err := p.parseOr(false)
	if err != nil {
		return nil, err
	}
	stmt := &RepeatStmt{Count: count, At: posOf(kw)}
	if p.peek().Type == TokAs {
		p.next()
		v, err := p.expect(TokIdent)
		if err != nil {
			return nil, p.errAt(p.peek(), "'as' の後にカウンタ名が必要です")
		}
		if isBuiltin(v.Lit) {
			return nil, p.errAt(v, "カウンタ名 %q は組み込み関数と同名のため使用できません", v.Lit)
		}
		stmt.Var = v.Lit
		stmt.HasVar = true
		// `repeat arr as i, item`: インデックス＋要素（配列のみ）。
		if p.peek().Type == TokComma {
			p.next()
			w, err := p.expect(TokIdent)
			if err != nil {
				return nil, p.errAt(p.peek(), "',' の後に要素名が必要です")
			}
			if isBuiltin(w.Lit) {
				return nil, p.errAt(w, "要素名 %q は組み込み関数と同名のため使用できません", w.Lit)
			}
			stmt.Item = w.Lit
			stmt.HasItem = true
		}
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	stmt.Body = body
	return stmt, nil
}

func (p *parser) parseWhile() (Stmt, error) {
	kw := p.next() // while
	cond, err := p.parseOr(false)
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &WhileStmt{Cond: cond, Body: body, At: posOf(kw)}, nil
}

func (p *parser) parseSwitch() (Stmt, error) {
	kw := p.next() // switch
	val, err := p.parseOr(false)
	if err != nil {
		return nil, err
	}
	stmt := &SwitchStmt{Value: val, At: posOf(kw)}
	open, err := p.expect(TokLBrace)
	if err != nil {
		return nil, err
	}
	_ = open
	p.skipNewlines()
	seenDefault := false
	for p.peek().Type != TokRBrace {
		if p.peek().Type == TokEOF {
			return nil, p.errAt(p.peek(), "switch 文が閉じられていません。'}' が必要です")
		}
		var c SwitchCase
		switch p.peek().Type {
		case TokCase:
			t := p.next()
			c.At = posOf(t)
			p.skipNewlines()
			for {
				v, err := p.parseOr(false)
				if err != nil {
					return nil, err
				}
				c.Values = append(c.Values, v)
				if p.peek().Type != TokComma {
					break
				}
				p.next()
				p.skipNewlines()
			}
		case TokDefault:
			t := p.next()
			if seenDefault {
				return nil, p.errAt(t, "switch 文に default が重複しています")
			}
			seenDefault = true
			c.At = posOf(t)
			c.Default = true
		default:
			return nil, p.errAt(p.peek(), "switch 文では case または default が必要です。%s が見つかりました", tokName(p.peek().Type))
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		c.Body = body
		stmt.Cases = append(stmt.Cases, c)
		p.skipNewlines()
	}
	p.next() // '}'
	return stmt, nil
}

func (p *parser) parseReturn() (Stmt, error) {
	kw := p.next() // return
	switch p.peek().Type {
	case TokNewline, TokEOF, TokRBrace, TokSemicolon:
		return &ReturnStmt{At: posOf(kw)}, nil
	}
	v, err := p.parseOr(true)
	if err != nil {
		return nil, err
	}
	return &ReturnStmt{Value: v, At: posOf(kw)}, nil
}

// parseAssignOrExpr は `target = value` または裸の式のいずれかを解析する。
// 単純な `=` は可視のものがないときに宣言となる（トップレベル: グローバル、
// 関数内: ローカルスロット）。`let` は廃止されたため、シャドーイング宣言は
// もう存在しない。
func (p *parser) parseAssignOrExpr() (Stmt, error) {
	x, err := p.parseOr(true)
	if err != nil {
		return nil, err
	}
	if p.peek().Type == TokAssign {
		op := p.next()
		if !assignable(x) {
			return nil, p.errAt(op, "'=' の左辺に代入できません")
		}
		v, err := p.parseOr(true)
		if err != nil {
			return nil, err
		}
		return &AssignStmt{Target: x, Value: v, At: x.Pos()}, nil
	}
	// 複合代入はその対象を保つ: `x op= v` は対象のアドレスをちょうど一度
	// だけ評価する（assignCompound を参照）。
	if binOp, ok := compoundOp(p.peek().Type); ok {
		op := p.next()
		if !assignable(x) {
			return nil, p.errAt(op, "'%s' の左辺に代入できません", op.Lit)
		}
		v, err := p.parseOr(true)
		if err != nil {
			return nil, err
		}
		return &CompoundAssignStmt{
			Target: x,
			Op:     binOp,
			Value:  v,
			At:     x.Pos(),
		}, nil
	}
	return &ExprStmt{X: x, At: x.Pos()}, nil
}

// compoundOp は `op=` トークンを対応する二項演算子に写像する。
func compoundOp(t TokenType) (TokenType, bool) {
	switch t {
	case TokPlusAssign:
		return TokPlus, true
	case TokMinusAssign:
		return TokMinus, true
	case TokStarAssign:
		return TokStar, true
	case TokSlashAssign:
		return TokSlash, true
	case TokModAssign:
		return TokMod, true
	case TokBitAndAssign:
		return TokBitAnd, true
	case TokBitOrAssign:
		return TokBitOr, true
	case TokBitXorAssign:
		return TokBitXor, true
	case TokShlAssign:
		return TokShl, true
	case TokShrAssign:
		return TokShr, true
	}
	return "", false
}

func assignable(x Expr) bool {
	switch n := x.(type) {
	case *VarExpr:
		return !n.Paren
	case *IndexExpr:
		return !n.Paren
	}
	return false
}

// ---------- 式（優先度クライミング） ----------
// litOK は値の位置をマークする: そこで裸の `Name{...}` はエラーだが、
// 条件（`if` の条件、`repeat` の回数）では false を渡すため `if x {` は
// 条件＋ブロックとして読まれる。括弧で囲まれた文脈（呼出、添字、
// グループ、配列リテラル）は常に true にリセットされる。

func (p *parser) parseOr(litOK bool) (Expr, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	l, err := p.parseAnd(litOK)
	if err != nil {
		return nil, err
	}
	for p.peek().Type == TokOr {
		op := p.next()
		r, err := p.parseAnd(litOK)
		if err != nil {
			return nil, err
		}
		l = &BinaryExpr{Op: op.Type, L: l, R: r, At: posOf(op)}
	}
	return l, nil
}

func (p *parser) parseAnd(litOK bool) (Expr, error) {
	l, err := p.parseEquality(litOK)
	if err != nil {
		return nil, err
	}
	for p.peek().Type == TokAnd {
		op := p.next()
		r, err := p.parseEquality(litOK)
		if err != nil {
			return nil, err
		}
		l = &BinaryExpr{Op: op.Type, L: l, R: r, At: posOf(op)}
	}
	return l, nil
}

// ビット単位レベルは近代化された（Python/Rust 風の）順序に従う: & | ^ は
// すべて == より強く結合するため `a & b == c` は `(a & b) == c` として
// 読まれる。ビット単位内では | が最も緩く、次に ^、そして &。
func (p *parser) parseBitOr(litOK bool) (Expr, error) {
	l, err := p.parseBitXor(litOK)
	if err != nil {
		return nil, err
	}
	for p.peek().Type == TokBitOr {
		op := p.next()
		r, err := p.parseBitXor(litOK)
		if err != nil {
			return nil, err
		}
		l = &BinaryExpr{Op: op.Type, L: l, R: r, At: posOf(op)}
	}
	return l, nil
}

func (p *parser) parseBitXor(litOK bool) (Expr, error) {
	l, err := p.parseBitAnd(litOK)
	if err != nil {
		return nil, err
	}
	for p.peek().Type == TokBitXor {
		op := p.next()
		r, err := p.parseBitAnd(litOK)
		if err != nil {
			return nil, err
		}
		l = &BinaryExpr{Op: op.Type, L: l, R: r, At: posOf(op)}
	}
	return l, nil
}

func (p *parser) parseBitAnd(litOK bool) (Expr, error) {
	l, err := p.parseRelational(litOK)
	if err != nil {
		return nil, err
	}
	for p.peek().Type == TokBitAnd {
		op := p.next()
		r, err := p.parseRelational(litOK)
		if err != nil {
			return nil, err
		}
		l = &BinaryExpr{Op: op.Type, L: l, R: r, At: posOf(op)}
	}
	return l, nil
}

func (p *parser) parseEquality(litOK bool) (Expr, error) {
	l, err := p.parseBitOr(litOK)
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek().Type
		if t != TokEq && t != TokNotEq {
			return l, nil
		}
		op := p.next()
		r, err := p.parseBitOr(litOK)
		if err != nil {
			return nil, err
		}
		l = &BinaryExpr{Op: op.Type, L: l, R: r, At: posOf(op)}
	}
}

func (p *parser) parseRelational(litOK bool) (Expr, error) {
	l, err := p.parseShift(litOK)
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().Type {
		case TokLt, TokLtEq, TokGt, TokGtEq:
			op := p.next()
			r, err := p.parseShift(litOK)
			if err != nil {
				return nil, err
			}
			l = &BinaryExpr{Op: op.Type, L: l, R: r, At: posOf(op)}
		default:
			return l, nil
		}
	}
}

func (p *parser) parseShift(litOK bool) (Expr, error) {
	l, err := p.parseAdditive(litOK)
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().Type {
		case TokShl, TokShr:
			op := p.next()
			r, err := p.parseAdditive(litOK)
			if err != nil {
				return nil, err
			}
			l = &BinaryExpr{Op: op.Type, L: l, R: r, At: posOf(op)}
		default:
			return l, nil
		}
	}
}

func (p *parser) parseAdditive(litOK bool) (Expr, error) {
	l, err := p.parseMultiplicative(litOK)
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().Type {
		case TokPlus, TokMinus:
			op := p.next()
			r, err := p.parseMultiplicative(litOK)
			if err != nil {
				return nil, err
			}
			l = &BinaryExpr{Op: op.Type, L: l, R: r, At: posOf(op)}
		default:
			return l, nil
		}
	}
}

func (p *parser) parseMultiplicative(litOK bool) (Expr, error) {
	l, err := p.parseUnary(litOK)
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().Type {
		case TokStar, TokSlash, TokMod:
			op := p.next()
			r, err := p.parseUnary(litOK)
			if err != nil {
				return nil, err
			}
			l = &BinaryExpr{Op: op.Type, L: l, R: r, At: posOf(op)}
		default:
			return l, nil
		}
	}
}

func (p *parser) parseUnary(litOK bool) (Expr, error) {
	switch p.peek().Type {
	case TokMinus, TokBang, TokBitNot:
		if err := p.enter(); err != nil {
			return nil, err
		}
		defer p.leave()
		op := p.next()
		// -9223372036854775808 を MinInt64 に畳む: 正の半分は int64 を
		// 桁あふれするため、ここでは符号なしとして解析する。裸の正の
		// リテラルはエラーのまま（parsePrimary を参照）。
		if op.Type == TokMinus && p.peek().Type == TokInt {
			if u, uerr := strconv.ParseUint(p.peek().Lit, 10, 64); uerr == nil && u == 1<<63 {
				p.next()
				return &IntLit{Raw: "-9223372036854775808", Value: -1 << 63, At: posOf(op)}, nil
			}
		}
		x, err := p.parseUnary(litOK)
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: op.Type, X: x, At: posOf(op)}, nil
	}
	return p.parsePostfix(litOK)
}

func (p *parser) parsePostfix(litOK bool) (Expr, error) {
	x, err := p.parsePrimary(litOK)
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().Type {
		case TokLParen:
			open := p.next()
			var args []Expr
			p.skipNewlines()
			if p.peek().Type != TokRParen {
				for {
					p.skipNewlines()
					if p.peek().Type == TokComma {
						// 省略された引数 (,,): null プレースホルダ。
						// これにより f(a, , b) は途中に null を渡す。
						c := p.next()
						args = append(args, &NullLit{At: posOf(c)})
						continue
					}
					if p.peek().Type == TokRParen {
						break // 末尾カンマ: f(a,) は f(a)
					}
					a, err := p.parseOr(true)
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					p.skipNewlines()
					if p.peek().Type == TokComma {
						p.next()
						continue
					}
					break
				}
			}
			p.skipNewlines()
			if _, err := p.expect(TokRParen); err != nil {
				return nil, err
			}
			x = &CallExpr{Callee: x, Args: args, At: posOf(open)}
		case TokLBracket:
			open := p.next()
			p.skipNewlines()
			idx, err := p.parseOr(true)
			if err != nil {
				return nil, err
			}
			p.skipNewlines()
			if _, err := p.expect(TokRBracket); err != nil {
				return nil, err
			}
			x = &IndexExpr{Base: x, Index: idx, At: posOf(open)}
		case TokDot:
			return nil, p.errAt(p.peek(), "フィールドアクセス（a.b）は廃止されました")
		default:
			return x, nil
		}
	}
}

func (p *parser) parsePrimary(litOK bool) (Expr, error) {
	t := p.peek()
	switch t.Type {
	case TokInt:
		p.next()
		v, err := strconv.ParseInt(t.Lit, 10, 64)
		if err != nil {
			return nil, p.errAt(t, "不正な整数リテラル %q です", t.Lit)
		}
		return &IntLit{Raw: t.Lit, Value: v, At: posOf(t)}, nil
	case TokFloat:
		p.next()
		v, err := strconv.ParseFloat(t.Lit, 64)
		if err != nil {
			return nil, p.errAt(t, "不正な浮動小数リテラル %q です", t.Lit)
		}
		return &FloatLit{Raw: t.Lit, Value: v, At: posOf(t)}, nil
	case TokString:
		p.next()
		return &StringLit{Value: t.Lit, At: posOf(t)}, nil
	case TokTrue:
		p.next()
		return &BoolLit{Value: true, At: posOf(t)}, nil
	case TokFalse:
		p.next()
		return &BoolLit{Value: false, At: posOf(t)}, nil
	case TokIdent:
		p.next()
		if p.peek().Type == TokLBrace {
			// `Name{...}` はリテラルではない。値の位置ではこれはエラー、
			// 条件の位置（`if x {`）ではブロックが続く。
			if litOK {
				return nil, p.errAt(p.peek(), "%q の後に予期しない '{' があります", t.Lit)
			}
		}
		return &VarExpr{Name: t.Lit, At: posOf(t)}, nil
	case TokLBracket:
		return p.parseArrayLit()
	case TokLParen:
		p.next()
		p.skipNewlines()
		x, err := p.parseOr(true)
		if err != nil {
			return nil, err
		}
		p.skipNewlines()
		if _, err := p.expect(TokRParen); err != nil {
			return nil, err
		}
		// 括弧で囲まれた全体は代入先になれない（`(x)=1`・`(a[0])=1` は
		// `'=' の左辺に代入できません`）。基底の括弧 `(a)[0]` は外側が
		// 括弧ではないため引き続き代入可。
		switch n := x.(type) {
		case *VarExpr:
			n.Paren = true
		case *IndexExpr:
			n.Paren = true
		}
		return x, nil
	case TokLBrace:
		// マップリテラルは廃止された: 式の位置の `{...}` はエラー
		// （ブロックは if/while/def/... 文に属する）。
		return nil, p.errAt(t, "mapリテラルは廃止されました。'{' はブロックにのみ使用できます")
	default:
		return nil, p.errAt(t, "予期しない %s です。式が必要です", describeToken(t))
	}
}

func (p *parser) parseArrayLit() (Expr, error) {
	open := p.next() // [
	lit := &ArrayLit{At: posOf(open)}
	p.skipNewlines()
	if p.peek().Type == TokRBracket {
		p.next()
		return lit, nil
	}
	for {
		p.skipNewlines()
		e, err := p.parseOr(true)
		if err != nil {
			return nil, err
		}
		lit.Elems = append(lit.Elems, e)
		p.skipNewlines()
		switch p.peek().Type {
		case TokComma:
			p.next()
			p.skipNewlines()
			if p.peek().Type == TokRBracket {
				p.next()
				return lit, nil
			}
		case TokRBracket:
			p.next()
			return lit, nil
		default:
			return nil, p.errAt(p.peek(), "配列リテラルでは ',' または ']' が必要です。%s が見つかりました", tokName(p.peek().Type))
		}
	}
}

func posOf(t Token) Pos { return Pos{File: t.File, Line: t.Line, Column: t.Column} }

func describeToken(t Token) string {
	switch t.Type {
	case TokEOF:
		return "入力の終端"
	case TokNewline:
		return "改行"
	case TokSemicolon:
		return "';'（セミコロンは使用できません。改行を使用してください）"
	case TokIdent:
		return fmt.Sprintf("識別子 %q", t.Lit)
	case TokInt:
		return fmt.Sprintf("整数 %q", t.Lit)
	case TokFloat:
		return fmt.Sprintf("小数 %q", t.Lit)
	case TokString:
		return fmt.Sprintf("文字列 %q", t.Lit)
	}
	return tokName(t.Type)
}
