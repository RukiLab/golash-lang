package main

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	return prog
}

func mustFailParse(t *testing.T, src string, substr string) {
	t.Helper()
	_, err := Parse(src)
	if err == nil {
		t.Fatalf("Parse(%q): expected error containing %q, got nil", src, substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("Parse(%q) error %q does not contain %q", src, err.Error(), substr)
	}
}

func TestElidedCallArgs(t *testing.T) {
	callOf := func(src string) *CallExpr {
		t.Helper()
		prog := mustParse(t, src)
		es, ok := prog.Stmts[0].(*ExprStmt)
		if !ok {
			t.Fatalf("%q: want ExprStmt, got %T", src, prog.Stmts[0])
		}
		c, ok := es.X.(*CallExpr)
		if !ok {
			t.Fatalf("%q: want CallExpr, got %T", src, es.X)
		}
		return c
	}
	// 途中の省略は null プレースホルダ。
	c := callOf("f(1, , 3)\n")
	if len(c.Args) != 3 {
		t.Fatalf("f(1, , 3): got %d args", len(c.Args))
	}
	if _, ok := c.Args[1].(*NullLit); !ok {
		t.Fatalf("middle arg = %T, want *NullLit", c.Args[1])
	}
	// 先頭の省略と末尾カンマ。
	c = callOf("f(, 2)\n")
	if len(c.Args) != 2 {
		t.Fatalf("f(, 2): got %d args", len(c.Args))
	}
	if _, ok := c.Args[0].(*NullLit); !ok {
		t.Fatalf("first arg = %T, want *NullLit", c.Args[0])
	}
	if c = callOf("f(1,)\n"); len(c.Args) != 1 {
		t.Fatalf("f(1,): got %d args, want 1", len(c.Args))
	}
}

func TestParsePrecedence(t *testing.T) {
	prog := mustParse(t, "x = 10 + 2 * 3\n")
	as := prog.Stmts[0].(*AssignStmt)
	// (10 + (2 * 3))
	bin, ok := as.Value.(*BinaryExpr)
	if !ok || bin.Op != TokPlus {
		t.Fatalf("top op should be +, got %s", as.Value.String())
	}
	if _, ok := bin.R.(*BinaryExpr); !ok {
		t.Fatalf("right side should be 2*3, got %s", bin.R.String())
	}
}

func TestParseVarDecl(t *testing.T) {
	prog := mustParse(t, "def f()\nlocal x = 1 + 2\nend\n")
	ds, ok := prog.Stmts[0].(*DefStmt)
	if !ok {
		t.Fatalf("want DefStmt, got %T", prog.Stmts[0])
	}
	vs, ok := ds.Body.Stmts[0].(*VarStmt)
	if !ok {
		t.Fatalf("want VarStmt, got %T", ds.Body.Stmts[0])
	}
	if vs.Name != "x" || vs.Value == nil {
		t.Fatalf("bad var node: %+v", vs)
	}
	if got := vs.String(); got != "local x = (1 + 2)" {
		t.Fatalf("bad rendering: %q", got)
	}
	mustFailParse(t, "local x = 1\n", "関数内でのみ")
	mustFailParse(t, "def f()\nlocal x\nend\n", "'=' が必要")
	mustFailParse(t, "def f()\nlocal 1 = 2\nend\n", "識別子")
	mustFailParse(t, "def f()\nlocal a[0] = 1\nend\n", "'=' が必要")
	// 旧 let は通常の識別子のまま。
	prog = mustParse(t, "let = 5\n")
	if _, ok := prog.Stmts[0].(*AssignStmt); !ok {
		t.Fatalf("let assignment: got %T", prog.Stmts[0])
	}
}

func TestParseParens(t *testing.T) {
	prog := mustParse(t, "x = (10 + 2) * 3\n")
	as := prog.Stmts[0].(*AssignStmt)
	if as.Value.String() != "((10 + 2) * 3)" {
		t.Fatalf("got %s", as.Value.String())
	}
}

func TestParseElseIfChain(t *testing.T) {
	// elseif 単語のみ連結する（単一 end）。`else if`（スペース区切り）は拒否する。
	prog := mustParse(t, "if x > 10\nmes(1)\nelseif x > 5\nmes(2)\nelse\nmes(3)\nend\n")
	ifs := prog.Stmts[0].(*IfStmt)
	if _, ok := ifs.Else.(*IfStmt); !ok {
		t.Fatalf("elseif should chain, got %T", ifs.Else)
	}
	mustFailParse(t, "if x > 10\nmes(1)\nelse if x > 5\nmes(2)\nelse\nmes(3)\nend\n", "else の後に改行が必要")
}

func TestParseRepeatForms(t *testing.T) {
	prog := mustParse(t, "repeat 10\nmes(1)\nend\nrepeat 10 as i\nmes(i)\nend\n")
	r1 := prog.Stmts[0].(*RepeatStmt)
	if r1.HasVar {
		t.Fatal("first repeat should have no counter")
	}
	r2 := prog.Stmts[1].(*RepeatStmt)
	if !r2.HasVar || r2.Var != "i" {
		t.Fatalf("second repeat should have counter i: %+v", r2)
	}
	prog = mustParse(t, "repeat a as i, x\nmes(x)\nend\n")
	r3 := prog.Stmts[0].(*RepeatStmt)
	if !r3.HasVar || !r3.HasItem || r3.Var != "i" || r3.Item != "x" {
		t.Fatalf("index+item repeat: %+v", r3)
	}
	if got := r3.String(); got != "repeat a as i, x\n  mes(x)\nend" {
		t.Fatalf("bad rendering: %q", got)
	}
}

func TestParseWhile(t *testing.T) {
	prog := mustParse(t, "while x < 10\nmes(x)\nend\n")
	w := prog.Stmts[0].(*WhileStmt)
	if w.Cond == nil || w.Body == nil || len(w.Body.Stmts) != 1 {
		t.Fatalf("bad while node: %+v", w)
	}
	if w.String() != "while (x < 10)\n  mes(x)\nend" {
		t.Fatalf("bad while rendering: %q", w.String())
	}
}

func TestParseSwitch(t *testing.T) {
	prog := mustParse(t, "switch x\ncase 1, 2\nmes(1)\ndefault\nmes(0)\nend\n")
	sw := prog.Stmts[0].(*SwitchStmt)
	if len(sw.Cases) != 2 {
		t.Fatalf("want 2 cases, got %+v", sw)
	}
	if sw.Cases[0].Default || len(sw.Cases[0].Values) != 2 {
		t.Fatalf("bad first case: %+v", sw.Cases[0])
	}
	if !sw.Cases[1].Default {
		t.Fatalf("second case should be default: %+v", sw.Cases[1])
	}
	want := "switch x\ncase 1, 2\n  mes(1)\ndefault\n  mes(0)\nend"
	if sw.String() != want {
		t.Fatalf("bad switch rendering: %q", sw.String())
	}
}

func TestParseBitwisePrecedence(t *testing.T) {
	// 近代化された順序（Python/Rust 風）: & | ^ はすべて == より強く
	// 結合する。ビット単位内では | が最も緩く、次に ^、そして &。
	// << >> は関係演算子より強く、+ より緩い。
	for _, tc := range []struct{ src, want string }{
		{"mes(1 | 2 & 3)\n", "(1 | (2 & 3))"},
		{"mes(6 ^ 3 | 1)\n", "((6 ^ 3) | 1)"},
		{"mes(1 & 1 == 1)\n", "((1 & 1) == 1)"},
		{"mes(1 == 2 & 3)\n", "(1 == (2 & 3))"},
		{"mes(1 << 2 + 1)\n", "(1 << (2 + 1))"},
		{"mes(1 < 2 << 3)\n", "(1 < (2 << 3))"},
		{"mes(~5)\n", "(~5)"},
		{"mes(1 | 2 && 0)\n", "((1 | 2) && 0)"},
	} {
		prog := mustParse(t, tc.src)
		es := prog.Stmts[0].(*ExprStmt)
		call := es.X.(*CallExpr)
		if got := call.Args[0].String(); got != tc.want {
			t.Fatalf("src %q: got %s, want %s", tc.src, got, tc.want)
		}
	}
}

func TestParseCompoundAssign(t *testing.T) {
	// `x op= v` はその対象を保つ（実行時に一度だけ評価される）。
	for _, tc := range []struct{ src, want string }{
		{"x += 1\n", "x += 1"},
		{"x -= 1\n", "x -= 1"},
		{"x *= 2\n", "x *= 2"},
		{"x /= 2\n", "x /= 2"},
		{"x %= 2\n", "x %= 2"},
		{"x &= 3\n", "x &= 3"},
		{"x |= 3\n", "x |= 3"},
		{"x ^= 3\n", "x ^= 3"},
		{"x <<= 1\n", "x <<= 1"},
		{"x >>= 1\n", "x >>= 1"},
		{"a[0] += 5\n", "a[0] += 5"},
	} {
		prog := mustParse(t, tc.src)
		st, ok := prog.Stmts[0].(*CompoundAssignStmt)
		if !ok {
			t.Fatalf("src %q: got %T, want *CompoundAssignStmt", tc.src, prog.Stmts[0])
		}
		if got := st.String(); got != tc.want {
			t.Fatalf("src %q: got %s, want %s", tc.src, got, tc.want)
		}
	}
	mustFailParse(t, "1 += 2\n", "代入できません")
	mustFailParse(t, "x += \n", "式が必要です")
	// 組込み名は束縛できない（代入、引数、ループ変数）。
	mustFailParse(t, "def f(mes)\nend\n", "組み込み関数と同名")
	mustFailParse(t, "repeat 3 as length\nend\n", "組み込み関数と同名")
	mustFailParse(t, "repeat [1] as i, push\nend\n", "組み込み関数と同名")
}

func TestParseTryAbolished(t *testing.T) {
	// try/catch は今や通常の識別子である: 波括弧は廃止され、改行〜end を使う。
	mustFailParse(t, "try\nmes(1)\nend\ncatch(e)\nmes(e)\nend\n", "予期しない")
	prog := mustParse(t, "try = 5\n")
	if got := prog.Stmts[0].String(); got != "try = 5" {
		t.Fatalf("try as ident: %q", got)
	}
}

func TestParseConstEnumAbolished(t *testing.T) {
	// const / enum は廃止され、通常の識別子である。
	prog := mustParse(t, "const = 5\n")
	if got := prog.Stmts[0].String(); got != "const = 5" {
		t.Fatalf("const as ident: %q", got)
	}
	prog = mustParse(t, "enum = 5\n")
	if got := prog.Stmts[0].String(); got != "enum = 5" {
		t.Fatalf("enum as ident: %q", got)
	}
}

func TestParseTopLevelDef(t *testing.T) {
	prog := mustParse(t, "def f()\nmes(1)\nend\n")
	if _, ok := prog.Stmts[0].(*DefStmt); !ok {
		t.Fatalf("top def: %T", prog.Stmts[0])
	}
	mustFailParse(t, "if true\ndef f()\nend\nend\n", "トップレベル")
	mustFailParse(t, "def f()\ndef g()\nend\nend\n", "トップレベル")
	mustFailParse(t, "def f(a=1)\nend\n", "デフォルト引数")
}

func TestParseMultilineArray(t *testing.T) {
	prog := mustParse(t, "a = [\n[1, 2, 3],\n[4, 5, 6],\n]\n")
	as := prog.Stmts[0].(*AssignStmt)
	al := as.Value.(*ArrayLit)
	if len(al.Elems) != 2 {
		t.Fatalf("got %d elems", len(al.Elems))
	}
}

func TestParseMapAbolished(t *testing.T) {
	mustFailParse(t, "m = {\"a\": 1}\n", "'{' は使用できません")
	mustFailParse(t, "m = {}\n", "'{' は使用できません")
	mustFailParse(t, "x = m.a\n", "'.' は使用できません")
	mustFailParse(t, "m.a = 2\n", "'.' は使用できません")
	mustFailParse(t, "p = Person\nname: \"A\",\nend\n", "文の後に改行")
}

func TestParseConditionWithBraceVar(t *testing.T) {
	// `if x` 改行 ... `end` が条件＋ブロック。
	prog := mustParse(t, "x = true\nif x\nmes(1)\nend\n")
	ifs := prog.Stmts[1].(*IfStmt)
	if _, ok := ifs.Cond.(*VarExpr); !ok {
		t.Fatalf("cond: %T", ifs.Cond)
	}
}

func TestParseRejections(t *testing.T) {
	mustFailParse(t, "x = 1;\n", "セミコロン")
	mustFailParse(t, "mode cli\n", "mode は廃止")
	mustFailParse(t, "*label\n", "*")
	mustFailParse(t, "a = [1, 2\n", "']'")
	mustFailParse(t, "if x\nmes(1)\n", "ブロックが閉じられていません")
	mustFailParse(t, "x = (1 + 2\n", "')'")
	mustFailParse(t, "def f()\nmes(1)\n", "ブロックが閉じられていません")
	mustFailParse(t, "if x\nmes(1)\nend mes(2)\n", "文の後に改行")
	mustFailParse(t, "x = 1\nend\n", "予期しない 'end'")
	mustFailParse(t, "if x\nmes(1)\nend\nelse\nmes(2)\nend\n", "予期しない 'else'")
}

func TestParseEndKeyword(t *testing.T) {
	// end は予約語であり、識別子としては使えない。
	mustFailParse(t, "end = 5\n", "予期しない 'end'")
	mustFailParse(t, "end()\n", "予期しない 'end'")
	// exit は組込みのため識別子にできない（def 引数など）。
	mustFailParse(t, "def f(exit)\nend\n", "組み込み関数と同名")
}

func TestParseCallVsIndex(t *testing.T) {
	// a(0) は呼出として解析される。インタプリタは [...] ヒント付きで
	// それを拒否する。
	prog := mustParse(t, "a(0)\n")
	es := prog.Stmts[0].(*ExprStmt)
	if _, ok := es.X.(*CallExpr); !ok {
		t.Fatalf("a(0) should parse as call: %T", es.X)
	}
	prog = mustParse(t, "a[0][1]\n")
	es = prog.Stmts[0].(*ExprStmt)
	if _, ok := es.X.(*IndexExpr); !ok {
		t.Fatalf("a[0][1] should parse as index: %T", es.X)
	}
}
