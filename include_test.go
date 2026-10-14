package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runFile はスクリプトファイルを（#include のスプライス込みで）解析して
// 実行し、mes() の出力を返す。解析時のエラーと実行時のエラーの両方が
// 表面化する。
func runFile(t *testing.T, path string) (string, error) {
	t.Helper()
	prog, err := ParseFile(path)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	in := NewInterp(&buf)
	vprog, verr := Compile(prog)
	if verr != nil {
		return buf.String(), verr
	}
	err = newVmachine(in).runMain(vprog)
	return buf.String(), err
}

func writeFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestModeDirective(t *testing.T) {
	dir := t.TempDir()
	modeOf := func(name, src string) string {
		t.Helper()
		writeFile(t, filepath.Join(dir, name), src)
		_, mode, err := CombineFileMode(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return mode
	}
	// 省略は gui を意味する。
	if m := modeOf("a.gsh", "mes(\"hi\")\n"); m != "gui" {
		t.Fatalf("default = %q, want gui", m)
	}
	if m := modeOf("b.gsh", "#mode cli\nmes(\"hi\")\n"); m != "cli" {
		t.Fatalf("cli = %q", m)
	}
	if m := modeOf("c.gsh", "#mode gui\nmes(\"hi\")\n"); m != "gui" {
		t.Fatalf("gui = %q", m)
	}
	// 最後に有効だったものが勝つ。
	if m := modeOf("d.gsh", "#mode cli\n#mode gui\n"); m != "gui" {
		t.Fatalf("last-wins = %q", m)
	}
	// 無効な分岐は数に入らない。
	if m := modeOf("e.gsh", "#ifdef NOPE\n#mode cli\n#endif\n"); m != "gui" {
		t.Fatalf("inactive = %q", m)
	}
	writeFile(t, filepath.Join(dir, "lib.gsh"), "#mode cli\n")
	if m := modeOf("f.gsh", "#include \"lib.gsh\"\n"); m != "cli" {
		t.Fatalf("include = %q", m)
	}
	// CRLF ソース: 末尾の \r がディレクティブを害してはならない。
	if m := modeOf("crlf.gsh", "#mode gui\r\nmes(\"hi\")\r\n"); m != "gui" {
		t.Fatalf("crlf gui = %q", m)
	}
	writeFile(t, filepath.Join(dir, "libcrlf.gsh"), "#mode cli\r\n")
	if m := modeOf("fcrlf.gsh", "#include \"libcrlf.gsh\"\r\n"); m != "cli" {
		t.Fatalf("crlf include = %q", m)
	}
	writeFile(t, filepath.Join(dir, "defcrlf.gsh"), "#define X 1\r\nmes(X)\r\n")
	if out, err := runFile(t, filepath.Join(dir, "defcrlf.gsh")); err != nil || out != "1\n" {
		t.Fatalf("crlf define = %q, %v", out, err)
	}
	// 不正な値はエラー。未知のディレクティブのヒントは #mode を列挙する。
	writeFile(t, filepath.Join(dir, "x.gsh"), "#mode bogus\n")
	if _, _, err := CombineFileMode(filepath.Join(dir, "x.gsh")); err == nil ||
		!strings.Contains(err.Error(), "#mode cli") {
		t.Fatalf("bad mode err = %v", err)
	}
	if _, err := CombineSource("", "#bogus\n", ""); err == nil ||
		!strings.Contains(err.Error(), "#mode") {
		t.Fatalf("unknown directive hint = %v", err)
	}
}

func TestIncludeBasic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lib.gsh"), "def double(x) {\nreturn x * 2\n}\nanswer = 20\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"lib.gsh\"\nmes(double(21))\nmes(answer + 1)\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "42\n21\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIncludeNestedDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sub", "util.gsh"), "def inc(x) {\nreturn x + 1\n}\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"sub/util.gsh\"\nmes(inc(41))\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "42\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIncludeOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "base.gsh"), "def f() {\nreturn 7\n}\n")
	writeFile(t, filepath.Join(dir, "mid.gsh"), "#include \"base.gsh\"\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"base.gsh\"\n#include \"mid.gsh\"\nmes(f())\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("shared library must splice once, got: %v", err)
	}
	if got != "7\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIncludeCircular(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.gsh"), "#include \"b.gsh\"\n")
	writeFile(t, filepath.Join(dir, "b.gsh"), "#include \"a.gsh\"\n")
	if _, err := ParseFile(filepath.Join(dir, "a.gsh")); err == nil || !strings.Contains(err.Error(), "循環") {
		t.Fatalf("want circular error, got %v", err)
	}
}

func TestIncludeMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"nope.gsh\"\n")
	if _, err := ParseFile(filepath.Join(dir, "main.gsh")); err == nil || !strings.Contains(err.Error(), "開けません") {
		t.Fatalf("want open error, got %v", err)
	}
}

func TestIncludeMalformed(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []string{
		"#include foo\n",
		"#include \"unclosed\n",
		"#include \"\" \n",
		"#include \"a.gsh\" trailing\n",
	} {
		writeFile(t, filepath.Join(dir, "main.gsh"), src)
		if _, err := ParseFile(filepath.Join(dir, "main.gsh")); err == nil {
			t.Fatalf("src %q: want directive error, got nil", src)
		}
	}
	// 行中の # はディレクティブではない: 字句解析器がそれを拒否する。
	writeFile(t, filepath.Join(dir, "main.gsh"), "x = 1 #include \"a.gsh\"\n")
	if _, err := ParseFile(filepath.Join(dir, "main.gsh")); err == nil || !strings.Contains(err.Error(), "不正な文字") {
		t.Fatalf("want illegal character error, got %v", err)
	}
}

func TestIncludeErrorPosition(t *testing.T) {
	dir := t.TempDir()
	// ライブラリ内の実行時エラーはライブラリのファイル+行を報告する。
	writeFile(t, filepath.Join(dir, "lib.gsh"), "def boom() {\nreturn nosuchvar\n}\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"lib.gsh\"\nmes(boom())\n")
	_, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err == nil || !strings.Contains(err.Error(), "lib.gsh:2:") {
		t.Fatalf("want lib.gsh:2: position, got %v", err)
	}
	// 構文エラーも同様である。
	writeFile(t, filepath.Join(dir, "bad.gsh"), "if {\n}\n")
	writeFile(t, filepath.Join(dir, "main2.gsh"), "#include \"bad.gsh\"\n")
	if _, err := ParseFile(filepath.Join(dir, "main2.gsh")); err == nil || !strings.Contains(err.Error(), "bad.gsh:1:") {
		t.Fatalf("want bad.gsh:1: position, got %v", err)
	}
	// メインファイル内のエラーはこれまでどおり動く。
	writeFile(t, filepath.Join(dir, "main3.gsh"), "mes(1)\nif {\n}\n")
	if _, err := ParseFile(filepath.Join(dir, "main3.gsh")); err == nil || !strings.Contains(err.Error(), "main3.gsh:2:") {
		t.Fatalf("want main3.gsh:2: position, got %v", err)
	}
}

func TestIncludeArrayAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.gsh"), "point = [3, 4]\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"types.gsh\"\nmes(point[0])\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "3\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDefineBasic(t *testing.T) {
	got, err := runSrcPP(t, "#define MAX 0x10\nmes(MAX)\nmes(MAX + 1)\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "16\n17\n" {
		t.Fatalf("got %q", got)
	}
	// 文字列、浮動小数点数、bool、負の定数。
	got, err = runSrcPP(t, "#define S \"hi\"\n#define F 2.5\n#define B true\n#define N -3\nmes(S)\nmes(F)\nmes(B)\nmes(N)\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "hi\n2.5\ntrue\n-3\n" {
		t.Fatalf("got %q", got)
	}
	// 定義より前の行での使用は置換されない。
	if _, err := runSrcPP(t, "mes(A)\n#define A 1\n"); err == nil {
		t.Fatal("forward define use should fail")
	}
}

func TestDefineErrors(t *testing.T) {
	for _, src := range []string{
		"#define M 2 * 3\n",          // 式は定数ではない
		"#define M x\n",              // 識別子はリテラルではない
		"#define 1X 2\n",             // 不正な名前
		"#define X 1\n#define X 2\n", // 再定義
		"#define if 1\n",             // キーワードは定義できない
		"#define true 1\n",           // true/false は定義できない
		"#define mes 1\n",            // 組込みは乗っ取れない
		"#define length\n",           // 値なしの組込みも同様にできない
	} {
		if _, err := runParsePP(src); err == nil {
			t.Fatalf("src %q: want define error, got nil", src)
		}
	}
}

func TestDefineAutoNumber(t *testing.T) {
	got, err := runSrcPP(t, "#define Color_Red\n#define Color_Green\n#define Color_Blue\nmes(Color_Red)\nmes(Color_Green)\nmes(Color_Blue)\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "0\n1\n2\n" {
		t.Fatalf("got %q", got)
	}
	// 明示的な値はカウンタをリセットする。
	got, err = runSrcPP(t, "#define A\n#define B 5\n#define C\nmes(A)\nmes(B)\nmes(C)\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "0\n5\n6\n" {
		t.Fatalf("got %q", got)
	}
	// 負の明示的な値も機能する。
	got, err = runSrcPP(t, "#define North\n#define South -2\n#define East\nmes(North)\nmes(South)\nmes(East)\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "0\n-2\n-1\n" {
		t.Fatalf("got %q", got)
	}
	// int でない define はカウンタをそのままにする。
	got, err = runSrcPP(t, "#define Name \"x\"\n#define A\nmes(A)\nmes(Name)\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "0\nx\n" {
		t.Fatalf("got %q", got)
	}
	// 無効な領域はカウンタに決して届かない。
	got, err = runSrcPP(t, "#ifdef MISSING\n#define Skip\n#endif\n#define A\nmes(A)\nmes(\"ok\")\n")
	if err != nil || got != "0\nok\n" {
		t.Fatalf("inactive define: got %q, %v", got, err)
	}
	// 使用は先行する行の define のみを見る。
	if _, err := runSrcPP(t, "mes(X_Q)\n#define X_Q 0\n"); err == nil {
		t.Fatalf("use before define should fail")
	}
	// 重複は値なしでも依然エラーになる。
	if _, err := runParsePP("#define A\n#define A\nmes(1)\n"); err == nil {
		t.Fatalf("duplicate valueless define should fail")
	}
	if _, err := runParsePP("#enum Color { Red }\nmes(1)\n"); err == nil {
		t.Fatalf("#enum directive should stay rejected")
	}
}

func TestIfdef(t *testing.T) {
	got, err := runSrcPP(t, "#define FOO 1\n#ifdef FOO\nmes(\"yes\")\n#else\nmes(\"no\")\n#endif\n")
	if err != nil || got != "yes\n" {
		t.Fatalf("ifdef taken: got %q, %v", got, err)
	}
	got, err = runSrcPP(t, "#ifdef MISSING\nmes(\"yes\")\n#else\nmes(\"no\")\n#endif\n")
	if err != nil || got != "no\n" {
		t.Fatalf("ifdef skipped: got %q, %v", got, err)
	}
	got, err = runSrcPP(t, "#ifndef MISSING\nmes(\"n\")\n#endif\n#ifdef MISSING\nmes(\"y\")\n#endif\n")
	if err != nil || got != "n\n" {
		t.Fatalf("ifndef: got %q, %v", got, err)
	}
	// 入れ子。
	got, err = runSrcPP(t, "#define A 1\n#ifdef A\n#ifdef B\nmes(\"ab\")\n#else\nmes(\"a\")\n#endif\n#else\nmes(\"none\")\n#endif\n")
	if err != nil || got != "a\n" {
		t.Fatalf("nested: got %q, %v", got, err)
	}
	// 無効な領域は完全に飛ばされる: その中の存在しない include や
	// 不正な define はエラーになってはならない。
	got, err = runSrcPP(t, "#ifdef MISSING\n#include \"no-such-file.gsh\"\n#define BAD 1 +\nmes(\"x\")\n#else\nmes(\"ok\")\n#endif\n")
	if err != nil || got != "ok\n" {
		t.Fatalf("inactive skip: got %q, %v", got, err)
	}
}

func TestConditionalErrors(t *testing.T) {
	for _, src := range []string{
		"#else\n", "#endif\n", // 迷子
		"#ifdef A\n#else\n#else\n#endif\n", // else の重複
		"#ifdef A\nmes(1)\n",               // 閉じられていない
		"#ifdef A\n",                       // 閉じられていない（空の本体）
		"#ifdef\n", "#ifdef A B\n",         // 不正な形式
		"#else x\n", "#endif x\n", // 末尾の余分なもの
	} {
		if _, err := runParsePP(src); err == nil {
			t.Fatalf("src %q: want conditional error, got nil", src)
		}
	}
}

func TestErrorDirective(t *testing.T) {
	if _, err := runParsePP("#error 使えません\n"); err == nil {
		t.Fatal("active #error should fail")
	} else if !strings.Contains(err.Error(), "使えません") {
		t.Fatalf("want custom message, got %v", err)
	}
	// 無効な #error は無視される。
	got, err := runSrcPP(t, "#ifdef MISSING\n#error boom\n#endif\nmes(\"ok\")\n")
	if err != nil || got != "ok\n" {
		t.Fatalf("inactive #error: got %q, %v", got, err)
	}
}

func TestDefineAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	// ライブラリの定数がメインファイルから見える（include より前に定義）。
	writeFile(t, filepath.Join(dir, "lib.gsh"), "mes(LIBVAL)\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#define LIBVAL 42\n#include \"lib.gsh\"\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil || got != "42\n" {
		t.Fatalf("lib use: got %q, %v", got, err)
	}
	// メインファイルの define がライブラリ内から見える（include 時に定義）。
	writeFile(t, filepath.Join(dir, "lib2.gsh"), "mes(VAL2 * 2)\n")
	writeFile(t, filepath.Join(dir, "main2.gsh"), "#define VAL2 21\n#include \"lib2.gsh\"\n")
	got, err = runFile(t, filepath.Join(dir, "main2.gsh"))
	if err != nil || got != "42\n" {
		t.Fatalf("lib2 use: got %q, %v", got, err)
	}
	// 行順: include より後の define はライブラリを書き換えてはならない。
	writeFile(t, filepath.Join(dir, "lib3.gsh"), "mes(LATE)\n")
	writeFile(t, filepath.Join(dir, "main3.gsh"), "#include \"lib3.gsh\"\n#define LATE 1\n")
	if _, err := runFile(t, filepath.Join(dir, "main3.gsh")); err == nil {
		t.Fatal("late define must not reach the library")
	}
}

func TestDefineUpwardSharing(t *testing.T) {
	dir := t.TempDir()
	// ライブラリの define が #include の後で親から見える。
	writeFile(t, filepath.Join(dir, "lib.gsh"), "#define UPVAR 7\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"lib.gsh\"\nmes(UPVAR * 6)\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil || got != "42\n" {
		t.Fatalf("upward use: got %q, %v", got, err)
	}
	// ...そして #include の後の #ifdef 判定からも。
	writeFile(t, filepath.Join(dir, "libf.gsh"), "#define FEAT 1\n")
	writeFile(t, filepath.Join(dir, "mainf.gsh"), "#include \"libf.gsh\"\n#ifdef FEAT\nmes(\"yes\")\n#else\nmes(\"no\")\n#endif\n")
	got, err = runFile(t, filepath.Join(dir, "mainf.gsh"))
	if err != nil || got != "yes\n" {
		t.Fatalf("upward ifdef: got %q, %v", got, err)
	}
	// ...そして後続の兄弟からも（A が定義し、B が使う）。
	writeFile(t, filepath.Join(dir, "a.gsh"), "#define SIB 21\n")
	writeFile(t, filepath.Join(dir, "b.gsh"), "mes(SIB * 2)\n")
	writeFile(t, filepath.Join(dir, "mains.gsh"), "#include \"a.gsh\"\n#include \"b.gsh\"\n")
	got, err = runFile(t, filepath.Join(dir, "mains.gsh"))
	if err != nil || got != "42\n" {
		t.Fatalf("sibling use: got %q, %v", got, err)
	}
	// ストリーム順は依然成り立つ: include より前の使用は失敗する。
	writeFile(t, filepath.Join(dir, "libp.gsh"), "#define PRE 1\n")
	writeFile(t, filepath.Join(dir, "mainp.gsh"), "mes(PRE)\n#include \"libp.gsh\"\n")
	if _, err := runFile(t, filepath.Join(dir, "mainp.gsh")); err == nil {
		t.Fatal("use before include should fail")
	}
	// ライブラリ名を親で再定義するのは依然エラーである。
	writeFile(t, filepath.Join(dir, "libd.gsh"), "#define DUP 1\n")
	writeFile(t, filepath.Join(dir, "maind.gsh"), "#include \"libd.gsh\"\n#define DUP 2\nmes(1)\n")
	if _, err := runFile(t, filepath.Join(dir, "maind.gsh")); err == nil {
		t.Fatal("parent redefinition of library name should fail")
	}
}

func TestChecksrc(t *testing.T) {
	dir := t.TempDir()
	chk := func(name, src string) string {
		t.Helper()
		writeFile(t, filepath.Join(dir, name), src)
		// 絶対パス: runFile は scriptDir を空にするため、相対の参照では
		// 一時ディレクトリを見逃してしまう。スラッシュを使うことで、
		// 生成されたソースに Windows のバックスラッシュエスケープを
		// 含めずに済む。
		abs := filepath.ToSlash(filepath.Join(dir, name))
		progSrc := "mes(checksrc(\"" + abs + "\"))\n"
		writeFile(t, filepath.Join(dir, "chk_"+name), "#mode cli\n"+progSrc)
		got, err := runFile(t, filepath.Join(dir, "chk_"+name))
		if err != nil {
			t.Fatalf("checksrc runner: %v", err)
		}
		return strings.TrimSuffix(got, "\n")
	}
	// クリーンなファイルは "" を報告する。
	if got := chk("ok.gsh", "mes(1 + 2)\n"); got != "" {
		t.Fatalf("clean = %q, want empty", got)
	}
	// 構文エラーは file:line:col: message を報告する。
	if got := chk("bad.gsh", "mes(1)\nif {\n}\n"); !strings.Contains(got, "bad.gsh:2:") {
		t.Fatalf("syntax err = %q, want bad.gsh:2:", got)
	}
	// include 内のエラーはライブラリのファイル名を挙げる。
	writeFile(t, filepath.Join(dir, "libbad.gsh"), "mes(1 +\n")
	if got := chk("use.gsh", "#include \"libbad.gsh\"\nmes(1)\n"); !strings.Contains(got, "libbad.gsh:") {
		t.Fatalf("include err = %q, want libbad.gsh:", got)
	}
	// 存在しないファイルはクラッシュではなく報告である。
	writeFile(t, filepath.Join(dir, "chkmiss.gsh"), "#mode cli\nmes(checksrc(\"no-such-file.gsh\"))\n")
	got, err := runFile(t, filepath.Join(dir, "chkmiss.gsh"))
	if err != nil || got == "" {
		t.Fatalf("missing file: got %q, %v", got, err)
	}
	// 上方共有された define はクリーンに検査される。
	writeFile(t, filepath.Join(dir, "libup.gsh"), "#define SHARED 3\n")
	if got := chk("up.gsh", "#include \"libup.gsh\"\nmes(SHARED)\n"); got != "" {
		t.Fatalf("upward = %q, want empty", got)
	}
}

func runSrcPP(t *testing.T, src string) (string, error) {
	t.Helper()
	prog, err := runParsePP(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	in := NewInterp(&buf)
	vprog, verr := Compile(prog)
	if verr != nil {
		return buf.String(), verr
	}
	err = newVmachine(in).runMain(vprog)
	return buf.String(), err
}

func runParsePP(src string) (*Program, error) {
	toks, err := CombineSource("", src, "")
	if err != nil {
		return nil, err
	}
	return ParseTokens(toks)
}

func TestCombineSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lib.gsh"), "v = 42\n")
	toks, err := CombineSource("", "#include \""+filepath.Join(dir, "lib.gsh")+"\"\nmes(v)\n", "")
	if err != nil {
		t.Fatalf("combine: %v", err)
	}
	prog, err := ParseTokens(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	in := NewInterp(&buf)
	vprog, verr := Compile(prog)
	if verr != nil {
		t.Fatalf("compile: %v", verr)
	}
	if err := newVmachine(in).runMain(vprog); err != nil {
		t.Fatalf("run: %v", err)
	}
	if buf.String() != "42\n" {
		t.Fatalf("got %q", buf.String())
	}
	// display "" は従来の位置（ファイル接頭辞なし）を保つ。
	if _, err := CombineSource("", "#bogus\n", ""); err == nil || !strings.Contains(err.Error(), "不明なディレクティブ") {
		t.Fatalf("want directive error, got %v", err)
	}
}
