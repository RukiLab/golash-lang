package main

import (
	"strings"
	"testing"
)

func lexTypes(t *testing.T, src string) []TokenType {
	t.Helper()
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q) error: %v", src, err)
	}
	types := make([]TokenType, len(toks))
	for i, tok := range toks {
		types[i] = tok.Type
	}
	return types
}

func TestLexBasic(t *testing.T) {
	types := lexTypes(t, "x = 10\n")
	want := []TokenType{TokIdent, TokAssign, TokInt, TokNewline, TokEOF}
	if len(types) != len(want) {
		t.Fatalf("got %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("token %d: got %s, want %s", i, types[i], want[i])
		}
	}
}

func TestLexKeywordsAndOps(t *testing.T) {
	types := lexTypes(t, "def if else repeat while switch case default as break continue return == != <= >= && || & | ^ ~ << >> += -= *= /= %= &= |= ^= <<= >>= ! < > + - * / % = , : . ( ) [ ] { }")
	found := map[TokenType]bool{}
	for _, ty := range types {
		found[ty] = true
	}
	// try/catch は通常の識別子として扱われる（goto/gosub/elif と同様）。
	// `let` も廃止され通常の識別子である。
	for _, word := range []string{"try", "catch", "goto", "let"} {
		types := lexTypes(t, word)
		if len(types) == 0 || types[0] != TokIdent {
			t.Fatalf("%s should lex as IDENT, got %v", word, types)
		}
	}
	for _, want := range []TokenType{TokDef, TokIf, TokElse, TokRepeat, TokWhile, TokSwitch, TokCase, TokDefault, TokAs, TokBreak,
		TokContinue, TokReturn, TokEq, TokNotEq, TokLtEq, TokGtEq, TokAnd, TokOr,
		TokBitAnd, TokBitOr, TokBitXor, TokBitNot, TokShl, TokShr,
		TokPlusAssign, TokMinusAssign, TokStarAssign, TokSlashAssign, TokModAssign,
		TokBitAndAssign, TokBitOrAssign, TokBitXorAssign, TokShlAssign, TokShrAssign,
		TokBang, TokLt, TokGt, TokPlus, TokMinus, TokStar, TokSlash, TokMod,
		TokAssign, TokComma, TokColon, TokDot, TokLParen, TokRParen,
		TokLBracket, TokRBracket, TokLBrace, TokRBrace} {
		if !found[want] {
			t.Errorf("missing token type %s in %v", want, types)
		}
	}
}

func TestLexComments(t *testing.T) {
	toks, err := Lex("// line comment\nx = 1 /* block */ + /* multi\nline */ 2\n")
	if err != nil {
		t.Fatal(err)
	}
	var lits []string
	for _, tok := range toks {
		if tok.Type != TokNewline && tok.Type != TokEOF {
			lits = append(lits, tok.Lit)
		}
	}
	want := []string{"x", "=", "1", "+", "2"}
	if strings.Join(lits, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", lits, want)
	}
}

func TestLexStringEscapes(t *testing.T) {
	toks, err := Lex("\"a\\\"b\\\\c\\n\"")
	if err != nil {
		t.Fatal(err)
	}
	if toks[0].Type != TokString || toks[0].Lit != "a\"b\\c\n" {
		t.Fatalf("bad string token: %+v", toks[0])
	}
}

func TestLexNumbers(t *testing.T) {
	types := lexTypes(t, "10 3.14 2e3 .5")
	want := []TokenType{TokInt, TokFloat, TokFloat, TokFloat, TokEOF}
	if len(types) != len(want) {
		t.Fatalf("got %v", types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("token %d: got %s, want %s (all=%v)", i, types[i], want[i], types)
		}
	}
}

func TestLexRadix(t *testing.T) {
	toks, err := Lex("0xFF 0b101 0o17 0XAB 0B1 0O7 0o77 0")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"255", "5", "15", "171", "1", "7", "63", "0"}
	if len(toks) != len(want)+1 { // +EOF
		t.Fatalf("got %v", toks)
	}
	for i, w := range want {
		if toks[i].Type != TokInt || toks[i].Lit != w {
			t.Fatalf("token %d: got %+v, want INT(%s)", i, toks[i], w)
		}
	}
	for _, src := range []string{
		"0x", "0b", "0b2", "0o8", "0xg", "0x1p3",
		"0xFFFFFFFFFFFFFFFFFF", "0x1.5",
	} {
		if _, err := Lex(src); err == nil {
			t.Errorf("Lex(%q): expected error, got nil", src)
		}
	}
}

func TestLexSemicolonIsToken(t *testing.T) {
	// ';' は字句解析では正常に扱われ、パーサ側で拒否されます。
	types := lexTypes(t, "x = 1;")
	if types[3] != TokSemicolon {
		t.Fatalf("got %v, want SEMICOLON at index 3", types)
	}
}

func TestLexErrors(t *testing.T) {
	for _, src := range []string{
		// 注意：`a & b` / `a | b` は以前ここでエラーでしたが、現在はビット演算子です
		// （TestParseBitwisePrecedence を参照）。
		"'x'", "\"unterminated", "/* nope",
		"\"bad \\q escape\"", "x = 1e",
		// 不正な数値：末尾のドット、文字やアンダースコアの貼り付き。
		"3.", "1_000", "123abc", "0d5", "3.14x", "10else",
	} {
		if _, err := Lex(src); err == nil {
			t.Errorf("Lex(%q): expected error, got nil", src)
		}
	}
}

func TestLexNulRejected(t *testing.T) {
	// 生の NUL バイトはソーステキストとして決して有効になりません（`\0` エスケープだけが
	// 唯一の書き方です）：コード内、文字列内、コメント内のいずれでも同様です。
	for _, src := range []string{
		"x = 1\x00\n",
		"\"ab\x00cd\"",
		"// com\x00ment\n",
		"/* bl\x00ock */",
	} {
		if _, err := Lex(src); err == nil {
			t.Errorf("Lex(%q): expected NUL error, got nil", src)
		}
	}
	// エスケープは今も有効です。
	toks, err := Lex("\"a\\0b\"")
	if err != nil {
		t.Fatal(err)
	}
	if toks[0].Lit != "a\x00b" {
		t.Fatalf("escape: got %q", toks[0].Lit)
	}
}

func TestLexPositions(t *testing.T) {
	toks, err := Lex("ab\n  cd")
	if err != nil {
		t.Fatal(err)
	}
	if toks[0].Line != 1 || toks[0].Column != 1 {
		t.Fatalf("first token pos: %+v", toks[0])
	}
	// トークン列：IDENT(ab) NEWLINE IDENT(cd) EOF
	if toks[2].Line != 2 || toks[2].Column != 3 {
		t.Fatalf("cd token pos: %+v", toks[2])
	}
}
