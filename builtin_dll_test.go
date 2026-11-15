package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

// DLL呼び出し・直接配列引数 (int[]/float[]) のテスト。実DLLを使う live 系は
// OS 別のシステムライブラリ (Windows: kernel32、Linux: libc.so.6、
// macOS: libSystem) で行い、値の断定は範囲・一致の安定形にする。
// 公開 API は dll/dlldef のみ。dlload/dllcall/dllfree と buf 系は廃止した。

func TestDllCString(t *testing.T) {
	b := append([]byte("hello"), 0)
	got, err := dllCString(uintptr(unsafe.Pointer(&b[0])), Pos{})
	if err != nil {
		t.Fatalf("dllCString: %v", err)
	}
	if got != "hello" {
		t.Fatalf("dllCString = %q, want %q", got, "hello")
	}
	runtime.KeepAlive(b)

	got, err = dllCString(0, Pos{})
	if err != nil || got != "" {
		t.Fatalf("dllCString(0) = %q, %v; want empty, nil", got, err)
	}
}

func TestDllMarshalArrays(t *testing.T) {
	// int[]: 正常変換。
	buf, err := dllMarshalIntArr("dlldef", 3, ArrayOf([]Value{Int(10), Int(-3), Int(7)}), Pos{})
	if err != nil {
		t.Fatalf("marshal int: %v", err)
	}
	if len(buf) != 3 || buf[0] != 10 || buf[1] != -3 || buf[2] != 7 {
		t.Fatalf("marshal int = %v", buf)
	}
	// float[]: int は昇格する。
	fbuf, err := dllMarshalFloatArr("dlldef", 3, ArrayOf([]Value{Int(1), Float(2.5)}), Pos{})
	if err != nil {
		t.Fatalf("marshal float: %v", err)
	}
	if len(fbuf) != 2 || fbuf[0] != 1 || fbuf[1] != 2.5 {
		t.Fatalf("marshal float = %v", fbuf)
	}
	// 空配列は空バッファ（呼び出し側で NULL になる）。
	buf, err = dllMarshalIntArr("dlldef", 3, ArrayOf(nil), Pos{})
	if err != nil || len(buf) != 0 {
		t.Fatalf("marshal empty int = %v, %v", buf, err)
	}
	// 型違い・範囲外は日本語エラー。
	if _, err := dllMarshalIntArr("dlldef", 3, Int(1), Pos{}); err == nil || !strings.Contains(err.Error(), "配列である必要があります") {
		t.Fatalf("int non-array: err = %v", err)
	}
	if _, err := dllMarshalFloatArr("dlldef", 3, Str("x"), Pos{}); err == nil || !strings.Contains(err.Error(), "配列である必要があります") {
		t.Fatalf("float non-array: err = %v", err)
	}
	if _, err := dllMarshalIntArr("dlldef", 3, ArrayOf([]Value{Float(1.5)}), Pos{}); err == nil || !strings.Contains(err.Error(), "要素 0 は整数である必要があります") {
		t.Fatalf("int elem: err = %v", err)
	}
	if _, err := dllMarshalIntArr("dlldef", 3, ArrayOf([]Value{Int(2147483648)}), Pos{}); err == nil || !strings.Contains(err.Error(), "32bit に収まりません") {
		t.Fatalf("int range: err = %v", err)
	}
	if _, err := dllMarshalFloatArr("dlldef", 3, ArrayOf([]Value{Str("x")}), Pos{}); err == nil || !strings.Contains(err.Error(), "要素 0 は数値である必要があります") {
		t.Fatalf("float elem: err = %v", err)
	}
}

// 廃止した buf 系は移行エラーになる。dlload/dllcall/dllfree も同様。
func TestDllLegacyRemoved(t *testing.T) {
	mustErrIO(t, "dlload(\"x.dll\")\n", "廃止されました")
	mustErrIO(t, "dllcall(\"x.dll\", \"f\", \"void()\")\n", "廃止されました")
	mustErrIO(t, "dllfree(1)\n", "廃止されました")
	mustErrIO(t, "def dlload(x)\nreturn x\nend\n", "廃止されました")
	mustErrIO(t, "def dllcall(x)\nreturn x\nend\n", "廃止されました")
	mustErrIO(t, "def dllfree(x)\nreturn x\nend\n", "廃止されました")
	for _, src := range []string{
		"bufnewf(3)\n", "bufnewi(3)\n", "buflen([1])\n",
		"bufget([1], 0)\n", "bufset([1], 0, 1)\n",
		"bufpackf([1])\n", "bufpacki([1])\n", "bufunpack([1])\n",
	} {
		mustErrIO(t, src, "廃止されました")
	}
	for _, src := range []string{
		"def bufnewf(x)\nreturn x\nend\n",
		"def bufunpack(x)\nreturn x\nend\n",
	} {
		mustErrIO(t, src, "廃止されました")
	}
	// 隠し組込は字句で書けない。
	mustErrIO(t, "msg($dllinvoke)\n", "不正な文字")
}

// 相対パスの DLL はスクリプト隣、次にカレントディレクトリの順で
// 解決する。見つからない名前は OS の検索 (システム DLL) に任せる。
func TestResolveDllPath(t *testing.T) {
	dirScript := t.TempDir()
	dirCwd := t.TempDir()
	local := filepath.Join(dirScript, "local_test.dll")
	if err := os.WriteFile(local, []byte("not a dll"), 0o666); err != nil {
		t.Fatal(err)
	}
	localCwd := filepath.Join(dirCwd, "cwd_test.dll")
	if err := os.WriteFile(localCwd, []byte("not a dll"), 0o666); err != nil {
		t.Fatal(err)
	}
	// macOS の t.TempDir() は /var 配下を返すが、getcwd は /private/var を
	// 返す。どちらでも同じファイルを指すため、シンボリックリンク解決後の
	// パスも比較対象に含める。
	localCanon, err := filepath.EvalSymlinks(local)
	if err != nil {
		t.Fatal(err)
	}
	localCwdCanon, err := filepath.EvalSymlinks(localCwd)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dirCwd); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()
	in := NewInterp(io.Discard)
	in.SetScriptDir(dirScript)
	if got := in.resolveDllPath("local_test.dll"); got != local && got != localCanon {
		t.Fatalf("script dir: got %q, want %q or %q", got, local, localCanon)
	}
	if got := in.resolveDllPath("cwd_test.dll"); got != localCwd && got != localCwdCanon {
		t.Fatalf("cwd: got %q, want %q or %q", got, localCwd, localCwdCanon)
	}
	const missing = "zz_no_such_dll_12345.dll"
	if got := in.resolveDllPath(missing); got != missing {
		t.Fatalf("missing: got %q, want %q", got, missing)
	}
}

func TestParseDllSig(t *testing.T) {
	ok, err := parseDllSig("int(int,string)")
	if err != nil {
		t.Fatalf("valid sig rejected: %s", err)
	}
	if ok.ret != "int" || len(ok.args) != 2 || ok.args[0] != "int" || ok.args[1] != "string" {
		t.Fatalf("sig parsed wrong: %+v", ok)
	}
	ok, err = parseDllSig("void( int[], float[] )")
	if err != nil {
		t.Fatalf("valid sig rejected: %s", err)
	}
	if ok.ret != "void" || len(ok.args) != 2 || ok.args[0] != "int[]" || ok.args[1] != "float[]" {
		t.Fatalf("sig parsed wrong: %+v", ok)
	}
	ok, err = parseDllSig("int()")
	if err != nil || ok.ret != "int" || len(ok.args) != 0 {
		t.Fatalf("empty args rejected: %+v %s", ok, err)
	}
	for _, bad := range []string{
		"int", "int(", "int)", "(int)", "int(int", "intint)", "int()x",
		"ptr(int)", "int(ptr)", "int(buf)", "buf(int)", "void(buf)",
		"int(int[])", // 戻り値ではなく引数としては正しいので別で確認
	} {
		if bad == "int(int[])" {
			continue
		}
		if _, err := parseDllSig(bad); err == nil {
			t.Fatalf("bad sig accepted: %q", bad)
		}
	}
	// 旧 buf 型は型エラーになる。
	if _, err := parseDllSig("int(buf)"); err == nil || !strings.Contains(err.Error(), "int[]/float[]") {
		t.Fatalf("buf sig: err = %v, want int[]/float[]", err)
	}
	// 配列は戻り値に使えない。
	if _, err := parseDllSig("int[](int)"); err == nil {
		t.Fatalf("array ret accepted")
	}
	if _, err := parseDllSig("float[]()"); err == nil {
		t.Fatalf("array ret accepted")
	}
	// 引数は15個まで。
	many := "void(" + strings.Repeat("int[],", 15) + "int[])"
	if _, err := parseDllSig(many); err == nil {
		t.Fatalf("16 args accepted: %q", many)
	}
}

// live 系: システム DLL で int/string 往復を確認する (dll/dlldef 経由)。
func TestDllLiveCall(t *testing.T) {
	var lib, countFn, strlenFn string
	switch runtime.GOOS {
	case "windows":
		lib = "kernel32.dll"
		countFn = "GetTickCount"
		strlenFn = "lstrlenA"
	case "darwin":
		lib = "/usr/lib/libSystem.B.dylib"
		countFn = "getpid"
		strlenFn = "strlen"
	default:
		lib = "libc.so.6"
		countFn = "getpid"
		strlenFn = "strlen"
	}
	dir := t.TempDir()
	runMain := func(name, src string) (string, error) {
		t.Helper()
		writeFile(t, filepath.Join(dir, name), src)
		return runFile(t, filepath.Join(dir, name))
	}
	out, err := runMain("live.gsh", "dll \""+lib+"\"\ndlldef cnt \""+countFn+"\" \"int()\"\ndlldef slen \""+strlenFn+"\" \"int(string)\"\nmsg(cnt() >= 0)\nmsg(slen(\"abc\"))\n")
	if err != nil || out != "1\n3\n" {
		t.Fatalf("dlldef live = %q, %v", out, err)
	}
	// 存在しないシンボル・引数不一致・開けない DLL は日本語エラー。
	_, err = runMain("nosym.gsh", "dll \""+lib+"\"\ndlldef f \"zz_no_such_fn_12345\" \"void()\"\nf()\n")
	if err == nil || !strings.Contains(err.Error(), "ありません") {
		t.Fatalf("nosym: err = %v, want ありません", err)
	}
	_, err = runMain("argc.gsh", "dll \""+lib+"\"\ndlldef s \""+strlenFn+"\" \"int(string)\"\ns(\"a\", \"b\")\n")
	if err == nil || !strings.Contains(err.Error(), "引数を 1 個必要としますが、2 個") {
		t.Fatalf("argc: err = %v, want 引数 1 個ですが 2 個", err)
	}
	_, err = runMain("open.gsh", "dll \"zz_no_such_dll_12345.dll\"\ndlldef f \"F\" \"void()\"\nf()\n")
	if err == nil || !strings.Contains(err.Error(), "開けません") {
		t.Fatalf("open: err = %v, want 開けません", err)
	}
}

// live 系: int[] 配列を直接渡す。strlen/lstrlenA で先頭バイト列を読み、
// strcpy/lstrcpyA で書き戻しを確認する。
func TestDllLiveArray(t *testing.T) {
	var lib, strlenFn, strcpyFn string
	switch runtime.GOOS {
	case "windows":
		lib = "kernel32.dll"
		strlenFn = "lstrlenA"
		strcpyFn = "lstrcpyA"
	case "darwin":
		lib = "/usr/lib/libSystem.B.dylib"
		strlenFn = "strlen"
		strcpyFn = "strcpy"
	default:
		lib = "libc.so.6"
		strlenFn = "strlen"
		strcpyFn = "strcpy"
	}
	dir := t.TempDir()
	runMain := func(name, src string) (string, error) {
		t.Helper()
		writeFile(t, filepath.Join(dir, name), src)
		return runFile(t, filepath.Join(dir, name))
	}
	// int32 65 ('A') は先頭バイト 0x41 の後に 0x00 が続くため長さ 1。
	// int32 0 は先頭が NUL のため長さ 0。空配列は NULL のため長さ 0。
	out, err := runMain("arr_read.gsh",
		"dll \""+lib+"\"\n"+
			"dlldef slen \""+strlenFn+"\" \"int(int[])\"\n"+
			"msg(slen([65, 0]))\nmsg(slen([0, 0]))\nmsg(slen([]))\n")
	if err != nil || out != "1\n0\n0\n" {
		t.Fatalf("dlldef array read = %q, %v", out, err)
	}
	// float[] の空配列も NULL で渡せる。
	out, err = runMain("arr_readf.gsh",
		"dll \""+lib+"\"\n"+
			"dlldef slenf \""+strlenFn+"\" \"int(float[])\"\n"+
			"msg(slenf([]))\nmsg(slenf([0.0]))\n")
	if err != nil || out != "0\n0\n" {
		t.Fatalf("dlldef float array read = %q, %v", out, err)
	}
	// 書き戻し: dest に "hi" を書くと int32[0] が 26984 ('h'+'i'*256) になる。
	out, err = runMain("arr_write.gsh",
		"dll \""+lib+"\"\n"+
			"dlldef cpy \""+strcpyFn+"\" \"string(int[],string)\"\n"+
			"d = [0, 0]\nmsg(cpy(d, \"hi\"))\nmsg(d[0])\nmsg(d[1])\n")
	if err != nil || out != "hi\n26984\n0\n" {
		t.Fatalf("dlldef array write = %q, %v", out, err)
	}
	// 配列引数の型違いは日本語エラー（DLL は開けた後の引数検査）。
	_, err = runMain("arr_tyerr.gsh", "dll \""+lib+"\"\ndlldef s \""+strlenFn+"\" \"int(int[])\"\ns(123)\n")
	if err == nil || !strings.Contains(err.Error(), "配列である必要があります") {
		t.Fatalf("arr typerr: err = %v", err)
	}
	_, err = runMain("arr_elem.gsh", "dll \""+lib+"\"\ndlldef s \""+strlenFn+"\" \"int(int[])\"\ns([1.5])\n")
	if err == nil || !strings.Contains(err.Error(), "要素 0 は整数である必要があります") {
		t.Fatalf("arr elem: err = %v", err)
	}
	_, err = runMain("arr_range.gsh", "dll \""+lib+"\"\ndlldef s \""+strlenFn+"\" \"int(int[])\"\ns([2147483648])\n")
	if err == nil || !strings.Contains(err.Error(), "32bit に収まりません") {
		t.Fatalf("arr range: err = %v", err)
	}
	_, err = runMain("arr_felem.gsh", "dll \""+lib+"\"\ndlldef s \""+strlenFn+"\" \"int(float[])\"\ns([\"x\"])\n")
	if err == nil || !strings.Contains(err.Error(), "要素 0 は数値である必要があります") {
		t.Fatalf("arr felem: err = %v", err)
	}
}

// dll / dlldef 宣言: 宣言・呼び出し・誤りの日本語検査。
// 正常系は OS 別のシステム DLL で live 確認する。
func TestDllDirectives(t *testing.T) {
	var lib, countFn, strlenFn string
	switch runtime.GOOS {
	case "windows":
		lib = "kernel32.dll"
		countFn = "GetTickCount"
		strlenFn = "lstrlenA"
	case "darwin":
		lib = "/usr/lib/libSystem.B.dylib"
		countFn = "getpid"
		strlenFn = "strlen"
	default:
		lib = "libc.so.6"
		countFn = "getpid"
		strlenFn = "strlen"
	}
	dir := t.TempDir()
	runMain := func(name, src string) (string, error) {
		t.Helper()
		writeFile(t, filepath.Join(dir, name), src)
		return runFile(t, filepath.Join(dir, name))
	}
	// 宣言と呼び出し。
	out, err := runMain("ok.gsh", "dll \""+lib+"\"\ndlldef cnt \""+countFn+"\" \"int()\"\ndlldef slen \""+strlenFn+"\" \"int(string)\"\nmsg(cnt() >= 0)\nmsg(slen(\"abcdef\"))\n")
	if err != nil || out != "1\n6\n" {
		t.Fatalf("dlldef live = %q, %v", out, err)
	}
	// 0引数・空白つき署名も通る。
	out, err = runMain("ws.gsh", "dll \""+lib+"\"\ndlldef c \""+countFn+"\" \" int( ) \"\nmsg(c() >= 0)\n")
	if err != nil || out != "1\n" {
		t.Fatalf("dlldef ws = %q, %v", out, err)
	}
	// 途中で対象 DLL を切り替えられる。
	out, err = runMain("sw.gsh", "dll \""+lib+"\"\ndlldef c \""+countFn+"\" \"int()\"\ndll \""+lib+"\"\ndlldef s \""+strlenFn+"\" \"int(string)\"\nmsg(c() >= 0)\nmsg(s(\"abcd\"))\n")
	if err != nil || out != "1\n4\n" {
		t.Fatalf("dlldef switch = %q, %v", out, err)
	}
	// 同じ DLL の関数は繰り返し呼べる (開き直さず共用)。
	out, err = runMain("cache.gsh", "dll \""+lib+"\"\ndlldef c1 \""+countFn+"\" \"int()\"\ndlldef c2 \""+countFn+"\" \"int()\"\nmsg(c1() >= 0)\nmsg(c2() >= 0)\nmsg(c1() >= 0)\n")
	if err != nil || out != "1\n1\n1\n" {
		t.Fatalf("dlldef cache = %q, %v", out, err)
	}
	// 型違いは日本語エラー。
	_, err = runMain("typerr.gsh", "dll \""+lib+"\"\ndlldef s \""+strlenFn+"\" \"int(string)\"\ns(123)\n")
	if err == nil || !strings.Contains(err.Error(), "文字列である必要があります") {
		t.Fatalf("typerr: err = %v", err)
	}
	bad := func(name, src, substr string) {
		t.Helper()
		writeFile(t, filepath.Join(dir, name), src)
		_, err := LoadFile(filepath.Join(dir, name))
		if err == nil || !strings.Contains(err.Error(), substr) {
			t.Fatalf("%s: err = %v, want %q", name, err, substr)
		}
	}
	// 重複・未定義ライブラリ・不正な署名・予約語。
	bad("dup.gsh", "dll \"x.dll\"\ndlldef f \"S\" \"void()\"\ndlldef f \"S\" \"void()\"\n", "既に定義されています")
	bad("nodll.gsh", "dlldef f \"S\" \"void()\"\n", "対象 DLL がありません")
	bad("badsig.gsh", "dll \"x.dll\"\ndlldef f \"S\" \"bogus\"\n", "署名")
	bad("kw.gsh", "dll \"x.dll\"\ndlldef sin \"S\" \"void()\"\n", "使用できません")
	bad("kw2.gsh", "dll \"x.dll\"\ndlldef dlload \"S\" \"void()\"\n", "廃止されました")
	bad("kw3.gsh", "dll \"x.dll\"\ndlldef bufnewf \"S\" \"void()\"\n", "廃止されました")
	bad("tail.gsh", "dll\n", "不正な dll")
	bad("tail2.gsh", "dll \"x.dll\"\ndlldef f\n", "不正な dlldef")
	// 旧 # ディレクティブは移行エラーになる。
	bad("legacy.gsh", "#mode cli\n", "# ディレクティブは廃止されました")
}
