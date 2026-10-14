// エディタツール向けのコード解析組込（CUI + GUI）。
//
// lextokens() は字句ハイライトと静的解析のために字句解析器を公開し、
// strwidth() は自作エディタでのキャレット位置決め用に表示幅を測り、
// parsetree() はアウトライン・ナビゲーション・ホバー用に構文解析器を
// 公開する。
package main

func init() {
	// lextokens(src): ソースをトークン化し、[type, text, line, col]
	// 配列の配列（EOF は除く）を返す。字句エラーは字句解析器の位置を
	// 伴う実行時エラーになる。
	register("lextokens", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		src, err := needString("lextokens", args, 0, at)
		if err != nil {
			return Null(), err
		}
		toks, err := Lex(src)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		out := make([]Value, 0, len(toks))
		for _, t := range toks {
			if t.Type == TokEOF {
				continue
			}
			out = append(out, ArrayOf([]Value{Str(string(t.Type)), Str(t.Lit), Int(int64(t.Line)), Int(int64(t.Column))}))
		}
		return ArrayOf(out), nil
	})

	// strwidth(s): s の表示幅 — GUI の現在のフェイスではピクセル、
	// CUI ではルーン数。エディタでのキャレット位置決め用。
	register("strwidth", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("strwidth", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Int(int64(in.be.TextWidth(s))), nil
	})

	// parsetree(src): ソースを解析してエディタ用の木
	// （アウトライン・ナビゲーション・ホバー）を返す:
	// ["program", line, col, text, stmts]。各ノードは
	// [type, line, col, text, ...種類別フィールド] で、本体は文の配列、
	// 式文はその式に展開される。生のディレクティブ（#...）は
	// lextokens と同じく字句エラーである。解析エラーは実行時エラー。
	register("parsetree", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		src, err := needString("parsetree", args, 0, at)
		if err != nil {
			return Null(), err
		}
		toks, err := Lex(src)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		prog, err := ParseTokens(toks)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		stmts := make([]Value, 0, len(prog.Stmts))
		for _, s := range prog.Stmts {
			stmts = append(stmts, stmtValue(s))
		}
		return nodeArr("program", Pos{Line: 1, Column: 1}, prog.String(), ArrayOf(stmts)), nil
	})

	// checksrc(path): エディタ用にファイルを字句解析＋構文解析する
	// （#include 展開込み）。正常なら ""、エラーなら最初のエラーの
	// "file:line:col: message" を返す。ファイルを実行するのとは違い、
	// これはコードを一切実行せず、例外も起こさない。エディタはこの
	// テキストを表示して該当位置へ飛ぶ。パスはファイル系組込と同じく
	// 解決される（スクリプトディレクトリ優先）。
	register("checksrc", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("checksrc", args, 0, at)
		if err != nil {
			return Null(), err
		}
		toks, err := CombineFile(in.resolvePath(p))
		if err != nil {
			return Str(err.Error()), nil
		}
		if _, err := ParseTokens(toks); err != nil {
			return Str(err.Error()), nil
		}
		return Str(""), nil
	})

	// srcanalyze(src): エディタツール用に字句解析＋構文解析を1回で
	// 行い、[errstr, tokens, tree] を返す。errstr は正常なら ""
	// （tokens/tree が埋まる）。そうでなければ位置付きメッセージで、
	// tokens/tree は空。壊れたコードで例外を起こしスクリプト全体を
	// 停止させてしまう lextokens/parsetree とは違い（try/catch は
	// 存在しない）、これによりエディタは途中まで入力されたソースを
	// 安全に解析できる。lextokens と同様、生のディレクティブ（#...）
	// はここでも字句エラーである — 呼出し側があらかじめ除去する
	// （エディタの hl_lex_src がそうする）。
	register("srcanalyze", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		src, err := needString("srcanalyze", args, 0, at)
		if err != nil {
			return Null(), err
		}
		empty := ArrayOf(nil)
		toks, err := Lex(src)
		if err != nil {
			return ArrayOf([]Value{Str(err.Error()), empty, empty}), nil
		}
		prog, err := ParseTokens(toks)
		if err != nil {
			return ArrayOf([]Value{Str(err.Error()), empty, empty}), nil
		}
		out := make([]Value, 0, len(toks))
		for _, t := range toks {
			if t.Type == TokEOF {
				continue
			}
			out = append(out, ArrayOf([]Value{Str(string(t.Type)), Str(t.Lit), Int(int64(t.Line)), Int(int64(t.Column))}))
		}
		stmts := make([]Value, 0, len(prog.Stmts))
		for _, s := range prog.Stmts {
			stmts = append(stmts, stmtValue(s))
		}
		tree := nodeArr("program", Pos{Line: 1, Column: 1}, prog.String(), ArrayOf(stmts))
		return ArrayOf([]Value{Str(""), ArrayOf(out), tree}), nil
	})
}

// nodeArr は共通のノード配列を構築する: 種類、位置、正規テキスト、
// その後ろに種類別フィールド。
func nodeArr(typ string, at Pos, text string, fields ...Value) Value {
	out := make([]Value, 0, 4+len(fields))
	out = append(out, Str(typ), Int(int64(at.Line)), Int(int64(at.Column)), Str(text))
	return ArrayOf(append(out, fields...))
}

// blockValues は文ブロックを変換する（nil 安全）。
func blockValues(b *BlockStmt) []Value {
	out := make([]Value, 0)
	if b == nil {
		return out
	}
	for _, s := range b.Stmts {
		out = append(out, stmtValue(s))
	}
	return out
}

// exprOrAbsent は省略可能な式を変換する。省略は ""（null には決して
// ならない）。これにより木のスロットは変数へ読み出せ、== "" で判定
// できる。
func exprOrAbsent(x Expr) Value {
	if x == nil {
		return Str("")
	}
	return exprValue(x)
}

// stmtValue は1文を変換する。式文はその式に展開されるため、木に
// "expr" ラッパーノードは生じない。
func stmtValue(s Stmt) Value {
	switch n := s.(type) {
	case *AssignStmt:
		return nodeArr("assign", n.Pos(), n.String(), exprValue(n.Target), exprValue(n.Value))
	case *CompoundAssignStmt:
		return nodeArr("compound", n.Pos(), n.String(), exprValue(n.Target), Str(opSymbol(n.Op)), exprValue(n.Value))
	case *ExprStmt:
		return exprValue(n.X)
	case *IfStmt:
		var elseV Value = Str("")
		if n.Else != nil {
			if b, ok := n.Else.(*BlockStmt); ok {
				elseV = ArrayOf(blockValues(b))
			} else {
				elseV = stmtValue(n.Else)
			}
		}
		return nodeArr("if", n.Pos(), n.String(), exprValue(n.Cond), ArrayOf(blockValues(n.Then)), elseV)
	case *RepeatStmt:
		var varV, itemV Value = Str(""), Str("")
		if n.HasVar {
			varV = Str(n.Var)
		}
		if n.HasItem {
			itemV = Str(n.Item)
		}
		return nodeArr("repeat", n.Pos(), n.String(), exprValue(n.Count), varV, itemV, ArrayOf(blockValues(n.Body)))
	case *WhileStmt:
		return nodeArr("while", n.Pos(), n.String(), exprValue(n.Cond), ArrayOf(blockValues(n.Body)))
	case *SwitchStmt:
		cases := make([]Value, 0, len(n.Cases))
		for _, c := range n.Cases {
			vals := make([]Value, 0, len(c.Values))
			for _, v := range c.Values {
				vals = append(vals, exprValue(v))
			}
			cases = append(cases, nodeArr("case", c.At, "", ArrayOf(vals), ArrayOf(blockValues(c.Body)), Bool(c.Default)))
		}
		return nodeArr("switch", n.Pos(), n.String(), exprValue(n.Value), ArrayOf(cases))
	case *BreakStmt:
		return nodeArr("break", n.Pos(), n.String())
	case *ContinueStmt:
		return nodeArr("continue", n.Pos(), n.String())
	case *ReturnStmt:
		return nodeArr("return", n.Pos(), n.String(), exprOrAbsent(n.Value))
	case *DefStmt:
		params := make([]Value, 0, len(n.Params))
		for _, p := range n.Params {
			params = append(params, nodeArr("param", p.At, p.Name, Str(p.Name)))
		}
		return nodeArr("def", n.Pos(), n.String(), Str(n.Name), ArrayOf(params), ArrayOf(blockValues(n.Body)))
	default:
		return nodeArr("unknown", s.Pos(), s.String())
	}
}

// exprValue は1つの式ノードを変換する。
func exprValue(x Expr) Value {
	switch n := x.(type) {
	case *IntLit:
		return nodeArr("int", n.Pos(), n.String(), Int(n.Value))
	case *FloatLit:
		return nodeArr("float", n.Pos(), n.String(), Float(n.Value))
	case *StringLit:
		return nodeArr("string", n.Pos(), n.String(), Str(n.Value))
	case *BoolLit:
		return nodeArr("bool", n.Pos(), n.String(), Bool(n.Value))
	case *NullLit:
		return nodeArr("null", n.Pos(), n.String())
	case *VarExpr:
		return nodeArr("var", n.Pos(), n.String(), Str(n.Name))
	case *ArrayLit:
		elems := make([]Value, 0, len(n.Elems))
		for _, e := range n.Elems {
			elems = append(elems, exprValue(e))
		}
		return nodeArr("array", n.Pos(), n.String(), ArrayOf(elems))
	case *IndexExpr:
		return nodeArr("index", n.Pos(), n.String(), exprValue(n.Base), exprValue(n.Index))
	case *CallExpr:
		args := make([]Value, 0, len(n.Args))
		for _, a := range n.Args {
			args = append(args, exprValue(a))
		}
		return nodeArr("call", n.Pos(), n.String(), exprValue(n.Callee), ArrayOf(args))
	case *UnaryExpr:
		return nodeArr("unary", n.Pos(), n.String(), Str(opSymbol(n.Op)), exprValue(n.X))
	case *BinaryExpr:
		return nodeArr("binary", n.Pos(), n.String(), Str(opSymbol(n.Op)), exprValue(n.L), exprValue(n.R))
	default:
		return nodeArr("unknown", x.Pos(), x.String())
	}
}
