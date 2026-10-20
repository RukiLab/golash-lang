package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

// DLL呼び出し・連続バッファのテスト。実DLLを使う live 系は
// OS 別のシステムライブラリ (Windows: kernel32、Linux: libc.so.6、
// macOS: libSystem) で行い、値の断定は範囲・一致の安定形にする。

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

func TestDllBufRoundTrip(t *testing.T) {
	mustOutIO(t, `
b = bufnewf(3)
bufset(b, 0, 1.5)
bufset(b, 1, 2)
mes(buflen(b))
mes(bufget(b, 0))
mes(bufget(b, 1))
mes(bufget(b, 2))
a = bufunpack(b)
mes(a[0])
mes(a[2])
`, "", "3\n1.5\n2\n0\n1.5\n0\n")
	mustOutIO(t, `
b = bufpacki([10, 0 - 3, 7])
mes(buflen(b))
mes(bufget(b, 0))
mes(bufget(b, 1))
mes(bufget(b, 2))
a = bufunpack(b)
mes(a[0])
mes(a[1])
`, "", "3\n10\n-3\n7\n10\n-3\n")
	mustOutIO(t, `
b = bufpackf([1, 2.5])
mes(bufget(b, 0))
mes(bufget(b, 1))
`, "", "1\n2.5\n")
}

func TestDllBufErrors(t *testing.T) {
	mustErrIO(t, "buflen(999999)\n", "開いているバッファ id ではありません")
	mustErrIO(t, "b = bufnewf(2)\nbufget(b, 2)\n", "範囲外です")
	mustErrIO(t, "b = bufnewi(1)\nbufset(b, 0, 1.5)\n", "整数である必要があります")
	mustErrIO(t, "b = bufnewi(1)\nbufset(b, 0, 2147483648)\n", "32bit に収まりません")
	mustErrIO(t, "bufpackf([1, \"x\"])\n", "要素 1 は数値である必要があります")
	mustErrIO(t, "bufpacki([1.5])\n", "要素 0 は整数である必要があります")
	mustErrIO(t, "bufnewf(0 - 1)\n", "0 以上である必要があります")
	mustErrIO(t, "bufget(7, 0)\n", "開いているバッファ id ではありません")
}

func TestDllHandleErrors(t *testing.T) {
	mustErrIO(t, "dlload(\"zz_no_such_dll_12345.dll\")\n", "開けません")
	mustErrIO(t, "dllcall(999999, \"f\", \"void()\")\n", "ハンドルではありません")
	mustErrIO(t, "dllfree(999999)\n", "ハンドルではありません")
}

func TestParseDllSig(t *testing.T) {
	ok, err := parseDllSig("int(int,string)")
	if err != nil {
		t.Fatalf("valid sig rejected: %s", err)
	}
	if ok.ret != "int" || len(ok.args) != 2 || ok.args[0] != "int" || ok.args[1] != "string" {
		t.Fatalf("sig parsed wrong: %+v", ok)
	}
	ok, err = parseDllSig("void( buf )")
	if err != nil {
		t.Fatalf("valid sig rejected: %s", err)
	}
	if ok.ret != "void" || len(ok.args) != 1 || ok.args[0] != "buf" {
		t.Fatalf("sig parsed wrong: %+v", ok)
	}
	ok, err = parseDllSig("int()")
	if err != nil || ok.ret != "int" || len(ok.args) != 0 {
		t.Fatalf("empty args rejected: %+v %s", ok, err)
	}
	for _, bad := range []string{
		"int", "int(", "int)", "(int)", "int(int", "intint)", "int()x",
		"ptr(int)", "int(ptr)", "buf(buf,buf,buf,buf,buf,buf,buf,buf,buf,buf,buf,buf,buf,buf,buf,buf)",
	} {
		if _, err := parseDllSig(bad); err == nil {
			t.Fatalf("bad sig accepted: %q", bad)
		}
	}
}

// live 系: システム DLL で int/string 往復を確認する。
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
	mustOutIO(t, `
h = dlload("`+lib+`")
t = dllcall(h, "`+countFn+`", "int()")
mes(t >= 0)
n = dllcall(h, "`+strlenFn+`", "int(string)", "abc")
mes(n)
dllfree(h)
`, "", "true\n3\n")
	// 存在しないシンボル・引数不一致は日本語エラー。
	mustErrIO(t, "h = dlload(\""+lib+"\")\ndllcall(h, \"zz_no_such_fn_12345\", \"void()\")\n", "ありません")
	mustErrIO(t, "h = dlload(\""+lib+"\")\ndllcall(h, \""+strlenFn+"\", \"int(string)\", \"a\", \"b\")\n", "引数 1 個ですが 2 個")
}

// #dll/#dlldef 指示語: 宣言・呼び出し・誤りの日本語検査。
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
	// 宣言と呼び出し。def 包みと等価に動く。
	out, err := runMain("ok.gsh", "#mode cli\n#dll \""+lib+"\"\n#dlldef cnt \""+countFn+"\" \"int()\"\n#dlldef slen \""+strlenFn+"\" \"int(string)\"\nmes(cnt() >= 0)\nmes(slen(\"abcdef\"))\n")
	if err != nil || out != "true\n6\n" {
		t.Fatalf("dlldef live = %q, %v", out, err)
	}
	// 0引数・空白つき署名も通る。
	out, err = runMain("ws.gsh", "#mode cli\n#dll \""+lib+"\"\n#dlldef c \""+countFn+"\" \" int( ) \"\nmes(c() >= 0)\n")
	if err != nil || out != "true\n" {
		t.Fatalf("dlldef ws = %q, %v", out, err)
	}
	// 途中で対象 DLL を切り替えられる。
	out, err = runMain("sw.gsh", "#mode cli\n#dll \""+lib+"\"\n#dlldef c \""+countFn+"\" \"int()\"\n#dll \""+lib+"\"\n#dlldef s \""+strlenFn+"\" \"int(string)\"\nmes(c() >= 0)\nmes(s(\"abcd\"))\n")
	if err != nil || out != "true\n4\n" {
		t.Fatalf("dlldef switch = %q, %v", out, err)
	}
	// 同じ DLL の関数はハンドルを共有する（dlload のキャッシュ）。
	out, err = runMain("mix.gsh", "#mode cli\nh1 = dlload(\""+lib+"\")\nh2 = dlload(\""+lib+"\")\nmes(h1 == h2)\ndllfree(h1)\nh3 = dlload(\""+lib+"\")\nmes(h3 >= 0)\ndllfree(h3)\n")
	if err != nil || out != "true\ntrue\n" {
		t.Fatalf("dlload cache = %q, %v", out, err)
	}
	bad := func(name, src, substr string) {
		t.Helper()
		writeFile(t, filepath.Join(dir, name), src)
		_, _, err := CombineFileMode(filepath.Join(dir, name))
		if err == nil || !strings.Contains(err.Error(), substr) {
			t.Fatalf("%s: err = %v, want %q", name, err, substr)
		}
	}
	// 重複・未定義ライブラリ・不正な署名・予約語。
	bad("dup.gsh", "#mode cli\n#dll \"x.dll\"\n#dlldef f \"S\" \"void()\"\n#dlldef f \"S\" \"void()\"\n", "既に定義されています")
	bad("nodll.gsh", "#mode cli\n#dlldef f \"S\" \"void()\"\n", "対象 DLL がありません")
	bad("badsig.gsh", "#mode cli\n#dll \"x.dll\"\n#dlldef f \"S\" \"bogus\"\n", "署名")
	bad("kw.gsh", "#mode cli\n#dll \"x.dll\"\n#dlldef sin \"S\" \"void()\"\n", "使用できません")
	bad("tail.gsh", "#mode cli\n#dll\n", "不正な #dll")
	bad("tail2.gsh", "#mode cli\n#dll \"x.dll\"\n#dlldef f\n", "不正な #dlldef")
	// #ifdef の無効分岐内は無視される。
	out, err = runMain("cond.gsh", "#mode cli\n#ifdef NOPE\n#dll \"x.dll\"\n#endif\nmes(7)\n")
	if err != nil || out != "7\n" {
		t.Fatalf("dlldef cond = %q, %v", out, err)
	}
}
