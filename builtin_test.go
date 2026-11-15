package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// runIO は与えられた標準入力で src を実行し、標準出力と logmsg の出力を
// 捕まえる。
func runIO(t *testing.T, src, stdin string) (string, string, *Interp, error) {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		return "", "", nil, err
	}
	var out, errout bytes.Buffer
	in := NewInterpWithIO(&out, strings.NewReader(stdin))
	in.errOut = &errout
	vprog, verr := Compile(prog)
	if verr != nil {
		return out.String(), errout.String(), in, verr
	}
	err = newVmachine(in).runMain(vprog)
	return out.String(), errout.String(), in, err
}

func mustOutIO(t *testing.T, src, stdin, want string) {
	t.Helper()
	got, _, _, err := runIO(t, src, stdin)
	if err != nil {
		t.Fatalf("src %q: runtime error: %v", src, err)
	}
	if got != want {
		t.Fatalf("src %q:\n got %q\nwant %q", src, got, want)
	}
}

func mustErrIO(t *testing.T, src, substr string) {
	t.Helper()
	_, _, _, err := runIO(t, src, "")
	if err == nil {
		t.Fatalf("src %q: expected error containing %q, got nil", src, substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("src %q: error %q does not contain %q", src, err.Error(), substr)
	}
}

func TestBuiltinRegistryComplete(t *testing.T) {
	want := []string{
		"msg", "print", "cls", "color", "title", "pos", "input",
		"int", "float", "str", "vartype",
		"abs", "sqrt", "sin", "cos", "tan", "atan", "exp", "log", "pow", "limit", "min", "max",
		"rnd", "randomize",
		"len", "slice", "find", "strtrim", "split", "strf", "getpath",
		"gettime", "sleep", "await", "tick", "nanotime", "exit", "assert", "logmsg", "exec", "args",
		"env", "open", "clip", "dialog",
		"http",
		"fileinfo", "dirlist", "filedel", "dirmake", "filecopy", "binfile", "txtfile",
		"curdir", "exedir", "homedir", "tmpdir",
		"push", "pop", "join", "dim",
		"sort", "reverse", "insert", "remove",
		"replace", "upper", "lower", "asc", "chr",
		"strwidth", "pipeexec",
		"procopen", "procread", "procwrite", "proceof", "procpoll", "procclose",
	}
	got := map[string]bool{}
	for _, n := range builtinNames() {
		got[n] = true
	}
	for _, n := range want {
		if !got[n] {
			t.Errorf("missing builtin %q", n)
		}
		delete(got, n)
	}
	// GUI ワード（builtin_gui*.go）は有効な追加である。
	for _, n := range guiBuiltinNames {
		delete(got, n)
	}
	// $ 始まりは dlldef 展開だけが使う隠し組込（字句で書けない）。
	for n := range got {
		if strings.HasPrefix(n, "$") {
			delete(got, n)
		}
	}
	for n := range got {
		t.Errorf("unexpected builtin %q (add to test or remove)", n)
	}
}

func TestBuiltinRedefineGuard(t *testing.T) {
	mustErrIO(t, "def sin(x) {\nreturn x\n}\n", "組み込み関数")
	mustErrIO(t, "def msg() {\n}\n", "組み込み関数")
}

func TestConversions(t *testing.T) {
	mustOutIO(t, "msg(int(3.9))\nmsg(int(\" 12 \"))\nmsg(int(true))\n", "", "3\n12\n1\n")
	mustOutIO(t, "msg(float(3))\nmsg(float(\"2.5\"))\nmsg(float(false))\n", "", "3\n2.5\n0\n")
	// int(string) は厳密に十進整数のみ: 浮動小数点数の表記は失敗する。
	mustErrIO(t, "msg(int(\"3.7\"))\n", "変換できません")
	mustErrIO(t, "msg(int(\"1e2\"))\n", "変換できません")
	mustErrIO(t, "msg(int(\"0x10\"))\n", "変換できません")
	// 範囲外の浮動小数点数と NaN は決して int に変換されない。
	mustErrIO(t, "msg(int(1e300))\n", "変換できません")
	// NaN/Inf は float() からも決して入らない。
	mustErrIO(t, "msg(float(\"nan\"))\n", "変換できません")
	mustErrIO(t, "msg(float(\"inf\"))\n", "変換できません")
	mustErrIO(t, "msg(float(\"Infinity\"))\n", "変換できません")
	mustOutIO(t, "msg(str(42))\nmsg(str([1, 2]))\nmsg(vartype(1))\nmsg(vartype(1.5))\nmsg(vartype(\"s\"))\nmsg(vartype(true))\nmsg(vartype([1]))\n", "", "42\n[1, 2]\nint\nfloat\nstring\nint\narray\n")
	mustErrIO(t, "msg(int(\"abc\"))\n", "変換できません")
	mustErrIO(t, "msg(int([1]))\n", "変換できません")
	mustErrIO(t, "msg(float(\"x\"))\n", "変換できません")
	mustErrIO(t, "msg(int())\n", "1 個必要")
	mustErrIO(t, "msg(int(1, 2))\n", "1 個必要")
}

func TestMath(t *testing.T) {
	mustOutIO(t, "msg(abs(-5))\nmsg(abs(5.5))\nmsg(abs(0))\n", "", "5\n5.5\n0\n")
	mustOutIO(t, "msg(sqrt(16))\nmsg(sin(0))\nmsg(cos(0))\nmsg(pow(2, 10))\nmsg(exp(0))\nmsg(log(1))\n", "", "4\n0\n1\n1024\n1\n0\n")
	mustOutIO(t, "msg(limit(5, 0, 10))\nmsg(limit(-3, 0, 10))\nmsg(limit(99, 0, 10))\nmsg(limit(5.5, 0, 10))\n", "", "5\n0\n10\n5.5\n")
	mustOutIO(t, "msg(vartype(limit(5, 0, 10)))\nmsg(vartype(limit(5.5, 0, 10)))\n", "", "int\nfloat\n")
	// limit() はちょうど3つの引数を取る: 短い形式と省略は失敗する。
	mustErrIO(t, "msg(limit(5))\n", "3 個必要")
	mustErrIO(t, "msg(limit(5, 0))\n", "3 個必要")
	mustErrIO(t, "msg(limit(1, 2, 3, 4))\n", "3 個必要")
	mustErrIO(t, "msg(limit(5, , 10))\n", "void値")
	mustErrIO(t, "msg(limit(, 0, 10))\n", "void値")
	// 省略は依然 void。msg() はそれを無音でスキップする。
	mustOutIO(t, "msg(,)\n", "", "\n")
	// min/max: 2個以上の引数の最小/最大。すべて int なら int になる。
	mustOutIO(t, "msg(min(3, 1, 2))\nmsg(max(3, 1, 2))\nmsg(min(-5, -2))\nmsg(max(-5, -2))\nmsg(min(7, 7))\n", "", "1\n3\n-5\n-2\n7\n")
	mustOutIO(t, "msg(min(1.5, 2))\nmsg(max(1, 2.5))\nmsg(min(3, 1.5, 2))\n", "", "1.5\n2.5\n1.5\n")
	mustOutIO(t, "msg(vartype(min(1, 2)))\nmsg(vartype(max(1.0, 2)))\n", "", "int\nfloat\n")
	mustErrIO(t, "msg(min(1))\n", "少なくとも 2 個")
	mustErrIO(t, "msg(max())\n", "少なくとも 2 個")
	mustErrIO(t, "msg(min(\"a\", 1))\n", "数値である必要があります")
	mustOutIO(t, "msg(atan(0))\nmsg(tan(0))\n", "", "0\n0\n")
	mustErrIO(t, "msg(sqrt(-1))\n", "平方根")
	mustErrIO(t, "msg(log(0))\n", "正の数")
	mustErrIO(t, "msg(abs(\"x\"))\n", "数値である必要があります")
	// 有限でない浮動小数点数の結果はエラーで、±Inf/NaN には決してならない。
	mustErrIO(t, "msg(exp(1000))\n", "有限の浮動小数ではありません")
	mustErrIO(t, "msg(pow(10, 309))\n", "有限の浮動小数ではありません")
	mustErrIO(t, "msg(pow(-1, 0.5))\n", "有限の浮動小数ではありません")
	// abs(MinInt64) は表現可能な否定を持たないため、ラップした負ではなく
	// エラーになる。
	mustErrIO(t, "msg(abs(-9223372036854775808))\n", "絶対値を取得できません")
	mustOutIO(t, "msg(abs(-9223372036854775807))\n", "", "9223372036854775807\n")
	mustErrIO(t, "msg(limit(1, 9, 2))\n", "下限")
}

func TestRandom(t *testing.T) {
	got, _, _, err := runIO(t, "randomize(42)\na = rnd(100)\nrandomize(42)\nb = rnd(100)\nmsg(a == b)\nmsg(a >= 0 && a < 100)\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1\n1\n" {
		t.Fatalf("got %q", got)
	}
	mustErrIO(t, "msg(rnd(0))\n", "正の上限")
	mustErrIO(t, "msg(rnd(\"x\"))\n", "整数である必要があります")
	mustOutIO(t, "randomize()\nmsg(rnd(10) >= 0 && rnd(10) < 10)\n", "", "1\n")
}

func TestBuiltinStrings(t *testing.T) {
	mustOutIO(t, "msg(len(\"hello\"))\nmsg(len(\"あいう\"))\nmsg(len(\"\"))\n", "", "5\n3\n0\n")
	mustErrIO(t, "msg(len(1))\n", "文字列または配列")
	mustErrIO(t, "msg(strlen(\"a\"))\n", "未定義の関数")
	mustErrIO(t, "msg(length([1]))\n", "未定義の関数")
	mustOutIO(t, "msg(slice(\"abcdef\", 1, 3))\nmsg(slice(\"あいうえお\", 1, 3))\nmsg(slice(\"abcdef\", -2))\nmsg(slice(\"abcdef\", 1, -1))\nmsg(slice(\"abc\", 1, 3))\nmsg(slice(\"abcdef\", 2))\n", "", "bc\nいう\nef\nbcde\nbc\ncdef\n")
	// 終端は排他的（[start, end)）。範囲外の終端・逆転した範囲は
	// 無音の切り詰めではなくエラー。
	mustErrIO(t, "msg(slice(\"abc\", 1, 99))\n", "範囲外")
	mustErrIO(t, "msg(slice(\"abc\", 1, 9223372036854775807))\n", "範囲外")
	mustErrIO(t, "msg(slice(\"abc\", 3, 1))\n", "範囲外")
	mustErrIO(t, "msg(slice(\"abc\", 0, -99))\n", "範囲外")
	mustOutIO(t, "msg(slice([1, 2, 3, 4], 1, 3))\nmsg(slice([1, 2, 3, 4], -2))\nmsg(slice([1, 2, 3, 4], 1, -1))\nmsg(slice([1, 2, 3], 0, 0))\n", "", "[2, 3]\n[3, 4]\n[2, 3]\n[]\n")
	mustOutIO(t, "msg(find(\"hello\", \"ll\"))\nmsg(find(\"hello\", \"l\", 3))\nmsg(find(\"hello\", \"z\"))\n", "", "2\n3\n-1\n")
	mustOutIO(t, "msg(find([\"x\", \"y\"], \"y\"))\nmsg(find([1, 2, 3], 2, 1))\nmsg(find([1, 2], 9))\n", "", "1\n1\n-1\n")
	// 範囲外の開始は無音の切り詰めや -1 ではなくエラーである。
	mustErrIO(t, "msg(find(\"hello\", \"h\", 99))\n", "範囲外")
	mustErrIO(t, "msg(find(\"hello\", \"h\", -1))\n", "範囲外")
	mustOutIO(t, "msg(\"[\" + strtrim(\"  hi  \") + \"]\")\nmsg(strtrim(\"xxhiix\", \"x\"))\nmsg(strtrim(\"xxhi\", \"x\", 1))\nmsg(strtrim(\"hixx\", \"x\", 2))\n", "", "[hi]\nhii\nhi\nhi\n")
	mustOutIO(t, "a = split(\"a,b,c\", \",\")\nmsg(a[1])\nmsg(len(a))\n", "", "b\n3\n")
	mustOutIO(t, "msg(strf(\"%02d\", 5))\nmsg(strf(\"%s=%d\", \"n\", 7))\n", "", "05\nn=7\n")
	mustErrIO(t, "msg(slice(\"abc\", 9, 1))\n", "範囲外")
	mustErrIO(t, "msg(slice(\"abc\", -99, 0))\n", "範囲外")
	mustErrIO(t, "msg(slice(1, 0))\n", "文字列または配列")
	mustErrIO(t, "msg(find(1, 0))\n", "文字列または配列")
	mustErrIO(t, "msg(split(\"a\", \"\"))\n", "空にすることはできません")
	mustErrIO(t, "msg(strtrim(\"a\", \"x\", 5))\n", "mode は")
	// 統合前の名前は未定義関数になる。
	for _, name := range []string{"strmid", "instr"} {
		mustErrIO(t, "msg("+name+"(\"a\"))\n", "未定義の関数")
	}
}

func TestGetpath(t *testing.T) {
	mustOutIO(t, "p = \"C:\\\\a\\\\b.exe\"\nmsg(getpath(p))\nmsg(getpath(p, \"dir\", \"base\"))\nmsg(getpath(p, \"ext\"))\nmsg(getpath(p, \"file\"))\nmsg(getpath(p, \"base\"))\nmsg(getpath(p, \"dir\"))\nmsg(getpath(p, \"lower\"))\n", "",
		"C:\\a\\b.exe\nC:\\a\\b\n.exe\nb.exe\nb\nC:\\a\\\nc:\\a\\b.exe\n")
	mustOutIO(t, "msg(getpath(\"/x/y.txt\", \"file\"))\nmsg(getpath(\"/x/y.txt\", \"dir\"))\n", "", "y.txt\n/x/\n")
	// 合成: dir+file、base+ext (= file)、dir を小文字化。
	mustOutIO(t, "msg(getpath(\"/x/y.txt\", \"dir\", \"file\"))\nmsg(getpath(\"/x/y.txt\", \"base\", \"ext\"))\nmsg(getpath(\"/X/Y.TXT\", \"dir\", \"lower\"))\n", "", "/x/y.txt\ny.txt\n/x/\n")
	mustErrIO(t, "msg(getpath(\"a\", \"bogus\"))\n", "dir/file/base/ext/lower")
	mustErrIO(t, "msg(getpath(\"a\", \"file\", \"base\"))\n", "同時に指定できません")
	mustErrIO(t, "msg(getpath(\"a\", 8))\n", "文字列である必要があります")
}

func TestGettimeRanges(t *testing.T) {
	got, _, _, err := runIO(t, "msg(gettime(1) >= 1 && gettime(1) <= 12)\nmsg(gettime(2) >= 0 && gettime(2) <= 6)\nmsg(gettime(4) >= 0 && gettime(4) <= 23)\nmsg(gettime(6) >= 0 && gettime(6) <= 59)\nmsg(gettime(7) >= 0 && gettime(7) <= 999)\nmsg(gettime(0) >= 2026)\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1\n1\n1\n1\n1\n1\n" {
		t.Fatalf("got %q", got)
	}
	mustErrIO(t, "msg(gettime(8))\n", "0 から 7")
}

func TestSleepEndAssertLogmsg(t *testing.T) {
	mustOutIO(t, "sleep(0)\nsleep(5)\nmsg(\"ok\")\n", "", "ok\n")
	mustErrIO(t, "sleep(-1)\n", "0 以上")
	mustOutIO(t, "await(0)\nawait()\nmsg(tick() >= 0)\n", "", "1\n")
	mustErrIO(t, "await(-1)\n", "0 以上")
	// 対象のティックが表現できない待機は、ラップによる即時リターンでは
	// なくエラーである。
	mustErrIO(t, "await(9223372036854775807)\n", "大きすぎます")
	mustOutIO(t, "a = nanotime()\nmsg(a > 0 && nanotime() >= a)\n", "", "1\n")
	_, _, in, err := runIO(t, "msg(\"hi\")\nend(3)\nmsg(\"bye\")\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if code, ok := in.ExitCode(); !ok || code != 3 {
		t.Fatalf("exit code: %d, %v", code, ok)
	}
	// GUI フロントエンドはループ後の ExitCode() を読むが、スクリプトの
	// ゴルーチンはまだ動いているかもしれない。並行アクセスは競合がない
	// 必要がある（-race の下で意味を持つ）。
	prog, err := Parse("n = 0\nwhile n < 10000 {\nn = n + 1\n}\nend(3)\n")
	if err != nil {
		t.Fatal(err)
	}
	conc := NewInterpWithIO(&bytes.Buffer{}, strings.NewReader(""))
	done := make(chan error, 1)
	go func() {
		vprog, verr := Compile(prog)
		if verr != nil {
			done <- verr
			return
		}
		done <- newVmachine(conc).runMain(vprog)
	}()
	var runErr error
	gotDone := false
	for {
		if _, ok := conc.ExitCode(); ok {
			break
		}
		select {
		case runErr = <-done:
			gotDone = true
		default:
		}
		if gotDone {
			break
		}
	}
	if !gotDone {
		runErr = <-done
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	if code, ok := conc.ExitCode(); !ok || code != 3 {
		t.Fatalf("concurrent exit code: %d, %v", code, ok)
	}
	// nanotime() は単調増加する（プロセス開始を基準とし、決して逆行しない）。
	mustOutIO(t, "a = nanotime()\nb = nanotime()\nmsg(b >= a)\nmsg(a >= 0)\n", "", "1\n1\n")
	mustOutIO(t, "assert(true)\nassert(1 < 2, \"math broke\")\nmsg(\"ok\")\n", "", "ok\n")
	mustErrIO(t, "assert(false)\n", "assertion failed")
	mustErrIO(t, "assert(false, \"custom\")\n", "custom")
	mustOutIO(t, "assert(2)\nmsg(\"ok\")\n", "", "ok\n")
	mustErrIO(t, "assert(1.5)\n", "整数")
	_, errout, _, err := runIO(t, "logmsg(\"dbg\", 42)\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if errout != "dbg 42\n" {
		t.Fatalf("logmsg got %q", errout)
	}
	// void 引数は msg()/print() と同じくスキップされる。
	_, errout, _, err = runIO(t, "def f() {\n}\nlogmsg(\"a\", f(), \"b\")\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if errout != "a b\n" {
		t.Fatalf("logmsg void got %q", errout)
	}
}

func TestArgs(t *testing.T) {
	// 引数なし: 空の配列。
	mustOutIO(t, "msg(args())\nmsg(len(args()))\n", "", "[]\n0\n")
	// SetArgs 経由で渡された引数付き（run がそうするように）。
	prog, err := Parse("msg(args())\nmsg(args()[1])\n")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	in := NewInterpWithIO(&out, strings.NewReader(""))
	in.SetArgs([]string{"--hard", "lv=3"})
	vprog, verr := Compile(prog)
	if verr != nil {
		t.Fatal(verr)
	}
	if err := newVmachine(in).runMain(vprog); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[--hard, lv=3]\nlv=3\n" {
		t.Fatalf("got %q", out.String())
	}
}

// TestFileDialogBuilder は OS のウィンドウを開かずに dlgopen/dlgsave の
// 設定を固定する: 開始フォルダ、任意のフィルタ、事前入力された保存名。
func TestFileDialogBuilder(t *testing.T) {
	in := &Interp{}
	// dlgStartDir はスクリプトが自身のディレクトリを持たないとき
	// 作業ディレクトリにフォールバックする。
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	b := dlgFileBuilder(in, "開く", "画像|*.png;*.jpg", "")
	if got := b.StartDir; got != wd {
		t.Fatalf("StartDir = %q, want %q (working dir)", got, wd)
	}
	if len(b.Filters) != 1 || b.Filters[0].Desc != "画像" ||
		strings.Join(b.Filters[0].Extensions, ",") != "png,jpg" {
		t.Fatalf("filters = %+v, want one 画像/png,jpg group", b.Filters)
	}
	if b.StartFile != "" {
		t.Fatalf("StartFile = %q, want empty for an open dialog", b.StartFile)
	}
	// スクリプトのディレクトリが作業ディレクトリより優先される。
	scriptDir := t.TempDir()
	in.SetScriptDir(scriptDir)
	if got := dlgStartDir(in); got != scriptDir {
		t.Fatalf("dlgStartDir = %q, want %q (script dir)", got, scriptDir)
	}
	// もはや存在しないスクリプトのディレクトリは飛ばされる。
	in.SetScriptDir(filepath.Join(scriptDir, "gone"))
	if got := dlgStartDir(in); got != wd {
		t.Fatalf("dlgStartDir = %q, want %q after a missing script dir", got, wd)
	}
	in.SetScriptDir(scriptDir)
	b = dlgFileBuilder(in, "保存", "", "out.png")
	if b.StartDir != scriptDir || b.StartFile != "out.png" {
		t.Fatalf("save builder: dir %q name %q", b.StartDir, b.StartFile)
	}
	// 不正な形式のフィルタと空の名前は何も設定しない。
	b = dlgFileBuilder(in, "保存", "no pipe here", "")
	if len(b.Filters) != 0 || b.StartFile != "" {
		t.Fatalf("empty filter must not configure anything: %+v", b)
	}
}

func TestMesBuiltinsRenamed(t *testing.T) {
	mustErrIO(t, "mes(\"x\")\n", "未定義の関数")
	mustErrIO(t, "mesbox(1, 2, 3)\n", "未定義の関数")
	mustErrIO(t, "logmes(\"x\")\n", "未定義の関数")
	for _, name := range []string{"msg", "msgbox", "logmsg"} {
		if !isBuiltin(name) {
			t.Errorf("%s is not registered", name)
		}
	}
}

func TestSoundBuiltinsRenamed(t *testing.T) {
	for _, name := range []string{"mmload", "mmplay", "mmstop", "mmvol", "mmpitch"} {
		mustErrIO(t, "msg("+name+"(\"a\"))\n", "未定義の関数")
	}
	for _, name := range []string{"sndload", "sndplay", "sndstop", "sndvol", "sndpitch"} {
		if !isBuiltin(name) {
			t.Errorf("sound builtin %q is not registered", name)
		}
	}
}

func TestEnv(t *testing.T) {
	mustOutIO(t, "env(\"GOU_TEST\", \"42\")\nmsg(env(\"GOU_TEST\"))\nmsg(env(\"GOU_NO_SUCH_VAR_XYZ\") == \"\")\n", "", "42\n1\n")
	mustErrIO(t, "env(1)\n", "文字列である必要があります")
	mustErrIO(t, "env(\"a\", 1)\n", "文字列である必要があります")
	mustErrIO(t, "env()\n", "1 から 2 個")
	mustErrIO(t, "env(\"a\", \"b\", \"c\")\n", "1 から 2 個")
	mustErrIO(t, "getenv(\"a\")\n", "未定義の関数")
	mustErrIO(t, "setenv(\"a\", \"b\")\n", "未定義の関数")
	mustErrIO(t, "open(1)\n", "文字列である必要があります")
	mustErrIO(t, "open(\"\")\n", "空にすることはできません")
	mustErrIO(t, "dialog(1)\n", "文字列である必要があります")
	mustErrIO(t, "dialog(\"x\", \"y\")\n", "整数である必要があります")
	mustErrIO(t, "dialog(\"x\", 6)\n", "0 から 5")
	mustErrIO(t, "dialog(\"x\", 3, \"y\")\n", "1 から 2 個")
	mustErrIO(t, "http(1)\n", "文字列である必要があります")
	mustOutIO(t, "clip(\"gsh\")\nmsg(clip())\n", "", "gsh\n")
	mustErrIO(t, "clip(1)\n", "文字列である必要があります")
	mustErrIO(t, "clipget()\n", "未定義の関数")
	mustErrIO(t, "clipset(\"x\")\n", "未定義の関数")
}

func TestHttp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			body, _ := io.ReadAll(r.Body)
			// 受信内容の検査用：Content-Type と本文を | 区切りで返す。
			if r.URL.Path == "/echo" {
				fmt.Fprintf(w, "%s|%s", r.Header.Get("Content-Type"), body)
				return
			}
			// その他の POST は受信本文をそのまま返す。
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
			return
		}
		switch r.URL.Path {
		case "/ok":
			fmt.Fprint(w, "hello")
		case "/obj":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"b":2,"a":1}`)
		case "/arr":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[1,"x",true,null,{"k":"v"},[1,2]]`)
		case "/bad":
			fmt.Fprint(w, "{oops")
		case "/num":
			fmt.Fprint(w, "123")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	// GET: 応答は常に文字列で返す（JSON の解釈はしない）。
	mustOutIO(t, fmt.Sprintf("msg(http(%q))\n", srv.URL+"/ok"), "", "hello\n")
	mustErrIO(t, fmt.Sprintf("msg(http(%q))\n", srv.URL+"/missing"), "404")
	mustOutIO(t, fmt.Sprintf("msg(http(%q))\n", srv.URL+"/obj"), "", "{\"b\":2,\"a\":1}\n")
	mustOutIO(t, fmt.Sprintf("msg(http(%q))\n", srv.URL+"/arr"), "", "[1,\"x\",true,null,{\"k\":\"v\"},[1,2]]\n")
	mustOutIO(t, fmt.Sprintf("msg(http(%q))\n", srv.URL+"/bad"), "", "{oops\n")
	mustOutIO(t, fmt.Sprintf("msg(http(%q))\n", srv.URL+"/num"), "", "123\n")
	// 型で確認する：JSON でも文字列のまま返す。
	mustOutIO(t, fmt.Sprintf("msg(vartype(http(%q)))\n", srv.URL+"/obj"), "", "string\n")
	mustOutIO(t, fmt.Sprintf("msg(vartype(http(%q)))\n", srv.URL+"/ok"), "", "string\n")
	// POST: 文字列 val はそのまま送る（application/json 固定）。
	// 応答も文字列で返す。
	mustOutIO(t, fmt.Sprintf("msg(http(%q, %q))\n", srv.URL, `{"a":1}`), "", "{\"a\":1}\n")
	// val が文字列でなければエラーになる。
	mustErrIO(t, fmt.Sprintf("msg(http(%q, [1]))\n", srv.URL), "文字列である必要があります")
	// echo で送信内容（Content-Type と本文）を確認する。
	mustOutIO(t, fmt.Sprintf("msg(http(%q, %q))\n", srv.URL+"/echo", "hi"), "", "application/json|hi\n")
	// 保存形・contentType 指定は廃止し、引数は url [, val] のみ。
	mustErrIO(t, fmt.Sprintf("msg(http(%q, %q, %q))\n", srv.URL, "x", "text/plain"), "1 から 2 個")
	// 旧名は未定義になる。
	mustErrIO(t, fmt.Sprintf("msg(httpget(%q))\n", srv.URL+"/ok"), "未定義の関数")
	mustErrIO(t, fmt.Sprintf("msg(httppost(%q, %q))\n", srv.URL, "x"), "未定義の関数")
	mustErrIO(t, "msg(http(1))\n", "文字列である必要があります")
	mustErrIO(t, "msg(http(\"http://127.0.0.1:1/nope\"))\n", "http")
	mustErrIO(t, "msg(http(1, \"x\"))\n", "文字列である必要があります")
	mustErrIO(t, "msg(http(\"http://127.0.0.1:1/nope\", \"x\"))\n", "http")
}

func TestExec(t *testing.T) {
	// 非同期の起動: 待たずに、起動すると 0 を返す（コンソールなしの切り離し起動）。
	if runtime.GOOS == "windows" {
		mustOutIO(t, "msg(exec(\"cmd\", \"/c\", \"exit\", \"3\"))\n", "", "0\n")
	} else {
		mustOutIO(t, "msg(exec(\"sh\", \"-c\", \"exit 3\"))\n", "", "0\n")
	}
	mustErrIO(t, "msg(exec(\"no-such-command-xyz\"))\n", "exec：")
	mustErrIO(t, "msg(exec(1))\n", "exec")
	mustErrIO(t, "msg(execgui(\"sh\"))\n", "未定義の関数")
}

// TestChildOutWriters は子プロセス出力の防護を固定する: 実際の *os.File
// はそのまま通過し（子はハンドルを継承し、複製のゴルーチンは不要）、
// プロセス内のライタはラップされるため os/exec の複製ゴルーチンが
// インタプリタ自身の書き込みと競合できない。exec/pipeexec はインタプリタ
// の出力を子に渡すため、ラップしない bytes.Buffer はその競合で書き込み
// 全体を失っていた（TestExec は約 10% の実行でちらついた）。
func TestChildOutWriters(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "io")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := childOut(f); got != io.Writer(f) {
		t.Fatalf("childOut(*os.File) = %T, want the file itself", got)
	}
	var buf bytes.Buffer
	w := childOut(&buf)
	if _, ok := w.(*syncWriter); !ok {
		t.Fatalf("childOut(*bytes.Buffer) = %T, want *syncWriter", w)
	}
	const goroutines, per = 8, 200
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < per; j++ {
				if _, err := w.Write([]byte("x")); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if buf.Len() != goroutines*per {
		t.Fatalf("lost writes: len = %d, want %d", buf.Len(), goroutines*per)
	}
}

func TestPipeexec(t *testing.T) {
	if runtime.GOOS == "windows" {
		mustOutIO(t, "r = pipeexec(\"cmd\", \"/c\", \"echo\", \"hi\")\nmsg(r[0])\nmsg(r[1])\nmsg(r[2])\n", "", "0\nhi\r\n\n\n")
	} else {
		mustOutIO(t, "r = pipeexec(\"printf\", \"hi\")\nmsg(r[0])\nmsg(r[1])\nmsg(r[2])\n", "", "0\nhi\n\n")
	}
	// 非ゼロの終了はエラーではなく値である。
	if runtime.GOOS == "windows" {
		mustOutIO(t, "r = pipeexec(\"cmd\", \"/c\", \"exit\", \"3\")\nmsg(r[0])\nmsg(r[1])\n", "", "3\n\n")
	} else {
		mustOutIO(t, "r = pipeexec(\"sh\", \"-c\", \"exit 3\")\nmsg(r[0])\nmsg(r[1])\n", "", "3\n\n")
	}
	// 標準エラーは r[2] に取り込まれる。
	if runtime.GOOS == "windows" {
		mustOutIO(t, "r = pipeexec(\"cmd\", \"/c\", \"echo oops>&2\")\nmsg(r[0])\nmsg(r[1])\nmsg(r[2])\n", "", "0\n\noops\r\n\n")
	} else {
		mustOutIO(t, "r = pipeexec(\"sh\", \"-c\", \"echo oops 1>&2\")\nmsg(r[0])\nmsg(r[1])\nmsg(r[2])\n", "", "0\n\noops\n\n")
	}
	// 起動失敗は [-1, "", message] で、決してエラーではない。
	mustOutIO(t, "r = pipeexec(\"no-such-command-xyz\")\nmsg(r[0])\nmsg(r[2] != \"\")\n", "", "-1\n1\n")
	mustErrIO(t, "msg(piperun(\"sh\"))\n", "未定義の関数")
}

// GUI 呼出（getkey を含む）はウィンドウ外ではきれいに失敗する。
func TestMouseDownOutsideWindow(t *testing.T) {
	mustErrIO(t, "msg(getkey(0))\n", "GUI")
	mustErrIO(t, "msg(mousedown())\n", "未定義の関数")
	mustErrIO(t, "msg(clicked())\n", "未定義の関数")
}

func TestAscChr(t *testing.T) {
	mustOut(t, "msg(asc(\"A\"))\nmsg(asc(\"あ\"))\nmsg(chr(65))\nmsg(chr(12354))\n", "65\n12354\nA\nあ\n")
	mustErr(t, "asc(\"\")\n", "空文字列")
	mustErr(t, "chr(-1)\n", "無効なコードポイント")
	mustErr(t, "chr(55296)\n", "無効なコードポイント")
	mustErr(t, "chr(1114112)\n", "無効なコードポイント")
}

func TestColorGetter(t *testing.T) {
	mustOutIO(t, "msg(color())\n", "", "[0, 0, 0, 255]\n")
	mustOutIO(t, "color(255, 0, 0)\nmsg(color())\n", "", "\x1b[38;2;255;0;0m[255, 0, 0, 255]\n")
	mustOutIO(t, "color(255, 0, 0, 128)\nmsg(color())\n", "", "\x1b[38;2;255;0;0m[255, 0, 0, 128]\n")
	mustOutIO(t, "color(0xFF0000)\nmsg(color())\n", "", "\x1b[38;2;255;0;0m[255, 0, 0, 255]\n")
	mustErrIO(t, "color(1, 2)\n", "1 個、3 個または 4 個")
}

func TestPosGetter(t *testing.T) {
	mustOutIO(t, "msg(pos())\n", "", "[0, 0]\n")
	mustOutIO(t, "pos(3, 4)\nmsg(pos())\n", "", "\x1b[5;4H[3, 4]\n")
	mustErrIO(t, "pos(1)\n", "0 個または 2 個")
}

func TestConsoleIO(t *testing.T) {
	mustOutIO(t, "print(\"a\", 1)\nprint(\"b\")\nmsg(\"c\")\n", "", "a 1bc\n")
	mustOutIO(t, "cls()\n", "", "\x1b[2J\x1b[H")
	mustOutIO(t, "color(255, 0, 0)\nmsg(color())\n", "", "\x1b[38;2;255;0;0m[255, 0, 0, 255]\n")
	mustOutIO(t, "color(255, 0, 0, 128)\nmsg(color())\n", "", "\x1b[38;2;255;0;0m[255, 0, 0, 128]\n")
	mustOutIO(t, "color(0xFF0000)\nmsg(color())\n", "", "\x1b[38;2;255;0;0m[255, 0, 0, 255]\n")
	mustOutIO(t, "msg(color())\n", "", "[0, 0, 0, 255]\n")
	mustOutIO(t, "pos(3, 4)\n", "", "\x1b[5;4H")
	mustOutIO(t, "pos(-1, 0)\n", "", "\x1b[1;0H")
	mustOutIO(t, "pos(-5, -3)\n", "", "\x1b[-2;-4H")
	// msg()/print() は CUI でもカーソルに追従する（端末の位置決め）。
	mustOutIO(t, "pos(5, 2)\nmsg(\"hi\")\n", "", "\x1b[3;6Hhi\n")
	mustOutIO(t, "pos(1, 1)\nprint(\"ab\")\npos(4, 1)\nprint(\"cd\")\n", "", "\x1b[2;2Hab\x1b[2;5Hcd")
	mustOutIO(t, "title(\"T\")\n", "", "\x1b]0;T\x07")
	mustErrIO(t, "title(\"a\\0b\")\n", "NUL")
	mustOutIO(t, "print(\"x\", \"bold\")\nprint(\"y\")\n", "", "\x1b[1mx\x1b[22;23;24my")
	mustErrIO(t, "color(1, 2)\n", "1 個、3 個または 4 個")
	mustErrIO(t, "color(300, 0, 0)\n", "0 から 255")
	mustErrIO(t, "color(0x1000000)\n", "0x000000")
	mustErrIO(t, "color(-1)\n", "0x000000")
}

func TestInput(t *testing.T) {
	mustOutIO(t, "s = input()\nmsg(s)\n", "hello\n", "hello\n")
	mustOutIO(t, "s = input(\"name? \")\nmsg(s)\n", "bob\n", "name? bob\n")
	mustOutIO(t, "msg(input())\n", "", "\n")                 // EOF yields ""
	mustOutIO(t, "s = input()\nmsg(s == \"\")\n", "", "1\n") // EOF
}

// runScriptDir は与えられた作業ディレクトリとスクリプトディレクトリで
// src を実行し、捕まえた標準出力を返す。
func runScriptDir(t *testing.T, src, cwd, scriptDir string) string {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()
	var out bytes.Buffer
	in := NewInterpWithIO(&out, strings.NewReader(""))
	in.SetScriptDir(scriptDir)
	vprog, verr := Compile(prog)
	if verr != nil {
		t.Fatalf("src %q: %v", src, verr)
	}
	if err := newVmachine(in).runMain(vprog); err != nil {
		t.Fatalf("src %q: %v", src, err)
	}
	return out.String()
}

func TestScriptDirResolution(t *testing.T) {
	dirA := t.TempDir() // スクリプトのホーム
	dirB := t.TempDir() // 作業ディレクトリ
	write := func(dir, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o666); err != nil {
			t.Fatal(err)
		}
	}
	write(dirA, "data.txt", "from-a")
	write(dirB, "data.txt", "from-b")
	write(dirB, "only-b.txt", "b")

	// スクリプトのディレクトリが作業ディレクトリより優先される。
	if got := runScriptDir(t, "msg(txtfile(\"data.txt\"))\n", dirB, dirA); got != "from-a\n" {
		t.Fatalf("script-dir first: got %q", got)
	}
	// 作業ディレクトリはフォールバックである。
	if got := runScriptDir(t, "msg(txtfile(\"only-b.txt\"))\n", dirB, dirA); got != "b\n" {
		t.Fatalf("cwd fallback: got %q", got)
	}
	// 絶対パスはそのまま通過する。
	abs := filepath.ToSlash(filepath.Join(dirB, "data.txt"))
	if got := runScriptDir(t, fmt.Sprintf("msg(txtfile(%q))\n", abs), dirB, dirA); got != "from-b\n" {
		t.Fatalf("absolute: got %q", got)
	}
	// 新しいファイルは作業ディレクトリではなくスクリプトの隣に作られる。
	runScriptDir(t, "txtfile(\"new.txt\", \"hi\")\n", dirB, dirA)
	if _, err := os.Stat(filepath.Join(dirA, "new.txt")); err != nil {
		t.Fatalf("save beside script: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirB, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("save must not land in cwd: %v", err)
	}
	// スクリプトのディレクトリがない（REPL）: 現在の作業ディレクトリの
	// 挙動。
	if got := runScriptDir(t, "msg(txtfile(\"only-b.txt\"))\n", dirB, ""); got != "b\n" {
		t.Fatalf("repl cwd: got %q", got)
	}
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	// 前方スラッシュ: スクリプト言語は \" \\ \n \t \r 以外の
	// バックスラッシュエスケープを持たず、Windows は前方スラッシュを
	// 受け付ける。
	join := func(n string) string { return filepath.ToSlash(filepath.Join(dir, n)) }

	mustOutIO(t, "msg(fileinfo(\""+join("nope")+"\")[0] == -1)\n", "", "1\n")
	mustErrIO(t, "filedel(\""+join("nope")+"\")\n", "filedel：")

	// txtfile: テキストの保存・読み込み + fileinfo の往復。
	mustOutIO(t, "txtfile(\""+join("t.txt")+"\", \"a\\nb\")\nmsg(fileinfo(\""+join("t.txt")+"\")[0] != -1)\nmsg(txtfile(\""+join("t.txt")+"\"))\n", "",
		"1\na\nb\n")

	// binfile: バイト配列の読み書き（size 切り詰め付き）。
	mustOutIO(t, "binfile(\""+join("b.bin")+"\", [65, 66, 67])\na = binfile(\""+join("b.bin")+"\")\nmsg(a)\nmsg(len(a))\nbinfile(\""+join("c.bin")+"\", [1, 2, 3], 2)\nmsg(len(binfile(\""+join("c.bin")+"\")))\n", "",
		"[65, 66, 67]\n3\n2\n")
	mustErrIO(t, "binfile(\""+join("x.bin")+"\", [256])\n", "0 から 255")
	// 巨大なサイズは Go の panic ではなく範囲エラーで、何も書き込まない。
	mustErrIO(t, "binfile(\""+join("y.bin")+"\", [1, 2, 3], 9223372036854775807)\n", "サイズが範囲外")
	if _, err := os.Stat(filepath.Join(dir, "y.bin")); !os.IsNotExist(err) {
		t.Fatalf("binfile with huge size must not create the file: %v", err)
	}
	mustErrIO(t, "msg(binfile(\""+join("missing.bin")+"\"))\n", "binfile：")
	mustErrIO(t, "binfile(\""+join("z.bin")+"\", 2)\n", "配列")
	mustErrIO(t, "txtfile(\""+join("z.bin")+"\", \"x\", 1)\n", "1 から 2 個")
	mustErrIO(t, "binfile(\""+join("z.bin")+"\", [1], 0, 0)\n", "1 から 3 個")

	// テキスト読み込みは \r\n を \n に正規化し、バイト読み込みはそのまま返す。
	if err := os.WriteFile(filepath.Join(dir, "crlf.txt"), []byte("a\r\nb"), 0o666); err != nil {
		t.Fatal(err)
	}
	mustOutIO(t, "msg(txtfile(\""+join("crlf.txt")+"\"))\nmsg(binfile(\""+join("crlf.txt")+"\"))\n", "",
		"a\nb\n[97, 13, 10, 98]\n")

	// dirmake/filedel/filecopy。
	mustOutIO(t, "dirmake(\""+join("sub")+"\")\nmsg(fileinfo(\""+join("sub")+"\")[2])\n", "", "1\n")
	mustOutIO(t, "filecopy(\""+join("t.txt")+"\", \""+join("t2.txt")+"\")\nmsg(fileinfo(\""+join("t2.txt")+"\")[0] != -1)\nfiledel(\""+join("t2.txt")+"\")\nmsg(fileinfo(\""+join("t2.txt")+"\")[0] != -1)\n", "",
		"1\n0\n")

	// dirlist + カレントディレクトリ（後で復元）。
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("z.gsh", []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}
	mustOutIO(t, "a = dirlist(\"*.gsh\")\nmsg(a)\n", "", "[z.gsh]\n")
}

// TestFileinfo は1回の stat のメタデータ三つ組 [size, mtime_ms, isdir] を
// 固定する: 通常のファイル、存在しないパス、ディレクトリ。
func TestFileinfo(t *testing.T) {
	dir := t.TempDir()
	join := func(n string) string { return filepath.ToSlash(filepath.Join(dir, n)) }

	mustOutIO(t, "txtfile(\""+join("t.txt")+"\", \"a\\nb\")\nr = fileinfo(\""+join("t.txt")+"\")\nmsg(r[0])\nmsg(r[1] > 0)\nmsg(r[2])\n", "",
		"3\n1\n0\n")
	mustOutIO(t, "r = fileinfo(\""+join("nope")+"\")\nmsg(r[0])\nmsg(r[1])\nmsg(r[2])\n", "",
		"-1\n0\n0\n")
	mustOutIO(t, "dirmake(\""+join("sub")+"\")\nr = fileinfo(\""+join("sub")+"\")\nmsg(r[1] > 0)\nmsg(r[2])\n", "",
		"1\n1\n")
	mustErrIO(t, "msg(fileinfo(1))\n", "fileinfo")
	mustErrIO(t, "msg(filetime(\"x\"))\n", "未定義の関数")
	mustErrIO(t, "msg(exist(\"x\"))\n", "未定義の関数")
}

func TestDirs(t *testing.T) {
	mustOutIO(t, "msg(len(curdir()) > 0)\nmsg(len(exedir()) > 0)\nmsg(len(homedir()) > 0)\nmsg(len(tmpdir()) > 0)\n", "", "1\n1\n1\n1\n")
	mustOutIO(t, "msg(fileinfo(curdir())[2])\n", "", "1\n")
	// curdir(path)：変更後は curdir() で反映され、旧カレントディレクトリに戻せる。
	mustOutIO(t, "qold = curdir()\ncurdir(\".\")\nmsg(curdir() != \"\")\ncurdir(qold)\nmsg(curdir() == qold)\n", "", "1\n1\n")
	mustErrIO(t, "msg(curdir(1))\n", "文字列である必要があります")
	mustErrIO(t, "msg(curdir(\"a\", \"b\"))\n", "0 から 1 個")
	mustErrIO(t, "msg(cwd())\n", "未定義の関数")
	mustErrIO(t, "msg(direxe())\n", "未定義の関数")
	mustErrIO(t, "mkdir(\"x\")\n", "未定義の関数")
	mustErrIO(t, "msg(getcwd())\n", "未定義の関数")
	mustErrIO(t, "msg(chdir(\".\"))\n", "未定義の関数")
}

func TestNoteAndFileBuiltinsRemoved(t *testing.T) {
	for _, name := range []string{"notemax", "noteget", "noteadd", "notedel", "noteload", "notesave", "bload", "bsave", "file", "delete", "bcopy"} {
		mustErrIO(t, "msg("+name+"(\"a\"))\n", "未定義の関数")
	}
}

func TestPeekPoke(t *testing.T) {
	mustOutIO(t, "a = [0, 0, 0, 0]\npoke(a, 0, 65)\nmsg(peek(a, 0))\nwpoke(a, 1, 16961)\nmsg(wpeek(a, 1))\nlpoke(a, 0, 16909060)\nmsg(lpeek(a, 0))\nmsg(a)\n", "",
		"65\n16961\n16909060\n[4, 3, 2, 1]\n")
	mustErrIO(t, "msg(peek([1], 5))\n", "範囲外")
	mustErrIO(t, "msg(peek([1], -1))\n", "範囲外")
	mustErrIO(t, "poke([0], 0, 256)\n", "0 から 255")
	mustErrIO(t, "msg(peek([\"x\"], 0))\n", "バイト")
	mustErrIO(t, "msg(lpeek([1, 2], 0))\n", "範囲外")
	// 巨大な添字は Go の panic ではなく範囲エラーでなければならない
	// （int(i)+n は MaxInt64 でラップし、境界検査を飛ばしていた）。
	mustErrIO(t, "msg(peek([1], 9223372036854775807))\n", "範囲外")
	mustErrIO(t, "msg(wpeek([1, 2], 9223372036854775807))\n", "範囲外")
	mustErrIO(t, "msg(lpeek([1, 2, 3, 4], 9223372036854775804))\n", "範囲外")
	mustErrIO(t, "poke([0], 9223372036854775807, 1)\n", "範囲外")
}

func TestPushPopJoin(t *testing.T) {
	mustOutIO(t, "a = []\nmsg(push(a, 1))\nmsg(push(a, 2, 3))\nmsg(a)\n", "", "1\n3\n[1, 2, 3]\n")
	mustOutIO(t, "a = [1, 2, 3]\nmsg(pop(a))\nmsg(a)\n", "", "3\n[1, 2]\n")
	mustOutIO(t, "msg(join([1, 2, 3]))\nmsg(join([\"a\", \"b\"], \"-\"))\nmsg(join([], \",\"))\nmsg(join([1, 2, 3], \",\"))\n", "", "123\na-b\n\n1,2,3\n")
	// 共有参照: 別名を通じた push が見える。
	mustOutIO(t, "a = [1]\nb = a\npush(b, 2)\nmsg(a)\n", "", "[1, 2]\n")
	mustErrIO(t, "msg(pop([]))\n", "配列が空")
	mustErrIO(t, "push(1, 2)\n", "配列である必要があります")
	mustErrIO(t, "msg(join([1], 2))\n", "文字列である必要があります")
}

func TestArrayExtras(t *testing.T) {
	mustOutIO(t, "a = [3, 1, 2]\nmsg(sort(a))\n", "", "[1, 2, 3]\n")
	mustOutIO(t, "a = [\"b\", \"a\"]\nsort(a)\nmsg(a)\n", "", "[a, b]\n")
	mustOutIO(t, "a = [1, 2, 3]\nreverse(a)\nmsg(a)\n", "", "[3, 2, 1]\n")
	mustOutIO(t, "a = [1, 3]\nmsg(insert(a, 1, 2))\nmsg(a)\n", "", "3\n[1, 2, 3]\n")
	mustOutIO(t, "a = [1, 2, 3, 4]\nmsg(remove(a, 1, 2))\nmsg(a)\n", "", "[2, 3]\n[1, 4]\n")
	mustOutIO(t, "a = [1, 2, 3]\nmsg(remove(a, 0))\nmsg(a)\n", "", "[1]\n[2, 3]\n")
	mustOutIO(t, "a = [1, 2, 3, 4]\nmsg(slice(a, 1, 3))\nmsg(slice(a, -2))\nmsg(slice(a, 0, 3))\n", "", "[2, 3]\n[3, 4]\n[1, 2, 3]\n")
	mustOutIO(t, "a = [\"x\", \"y\"]\nmsg(find(a, \"y\"))\nmsg(find(a, \"z\"))\nmsg(find(a, 1))\nmsg(find([1, 2, 3], 3, 1))\n", "", "1\n-1\n-1\n2\n")
	// 連鎖: sort は配列自身を返す。
	mustOutIO(t, "msg(join(sort([3, 1, 2]), \"-\"))\n", "", "1-2-3\n")
	// sort は整数、浮動小数点数、混合の数値を受け付ける。文字列は別に
	// ソートされ、それ以外はエラーである。
	mustOutIO(t, "a = [2.5, 1.5, 10]\nmsg(sort(a))\n", "", "[1.5, 2.5, 10]\n")
	mustOutIO(t, "a = [2, 1.5, 10]\nmsg(sort(a))\n", "", "[1.5, 2, 10]\n")
	mustErrIO(t, "sort([1, \"a\"])\n", "すべて数値かすべて文字列")
	mustOutIO(t, "msg(sort([true]))\n", "", "[1]\n")
	mustErrIO(t, "insert([1], 5, 2)\n", "範囲外")
	mustErrIO(t, "remove([1], 0, 2)\n", "範囲外")
	// 巨大な添字/個数は Go の panic ではなく範囲エラーでなければ
	// ならない。
	mustErrIO(t, "insert([1], 9223372036854775807, 2)\n", "範囲外")
	mustErrIO(t, "remove([1, 2], 9223372036854775807, 1)\n", "範囲外")
	mustErrIO(t, "remove([1, 2], 0, 9223372036854775807)\n", "範囲外")
	mustErrIO(t, "slice([1, 2], 3)\n", "範囲外")
	mustErrIO(t, "slice([1, 2], 0, 3)\n", "範囲外")
	mustErrIO(t, "slice([1, 2], 2, 1)\n", "範囲外")
	mustErrIO(t, "sort(1)\n", "配列である必要があります")
}

func TestStringExtras(t *testing.T) {
	mustOutIO(t, "msg(replace(\"aaa\", \"a\", \"b\"))\nmsg(replace(\"hello\", \"z\", \"y\"))\n", "", "bbb\nhello\n")
	mustOutIO(t, "msg(upper(\"abc-あ\"))\nmsg(lower(\"ABC-ア\"))\n", "", "ABC-あ\nabc-ア\n")
	mustErrIO(t, "replace(\"a\", \"\", \"b\")\n", "空にすることはできません")
	mustErrIO(t, "upper(1)\n", "文字列である必要があります")
}

func TestVartypeFunc(t *testing.T) {
	mustErrIO(t, "def f() {\n}\nmsg(vartype(f))\n", "未定義の変数")
	mustOutIO(t, "def f() {\n}\nmsg(vartype([1]))\nmsg(f())\n", "", "array\n\n")
}

func TestLexTokens(t *testing.T) {
	mustOutIO(t, "t = lextokens(\"x = 1\")\nmsg(len(t))\nmsg(t[0][0])\nmsg(t[0][1])\nmsg(t[1][1])\nmsg(t[2][1])\nmsg(t[0][2])\nmsg(t[0][3])\n", "", "3\nIDENT\nx\n=\n1\n1\n1\n")
	mustOutIO(t, "t = lextokens(\"if x {\\n}\")\nmsg(t[0][0])\nmsg(t[2][0])\nmsg(t[3][0])\n", "", "IF\nLBRACE\nNEWLINE\n")
	mustErrIO(t, "lextokens(\"'a'\")\n", "シングルクォート")
	mustErrIO(t, "lextokens(1)\n", "文字列である必要があります")
}

// TestSrcAnalyze はエディタが使う1パスで例外を起こさない解析器を
// 固定する: lextokens/parsetree と同じトークン/木の形状を持つ
// [errstr, tokens, tree]。壊れたソースでは（例外を起こすのではなく）
// エラー文字列を返す。
func TestSrcAnalyze(t *testing.T) {
	mustOutIO(t, "r = srcanalyze(\"x = 1\\n\")\nmsg(r[0] == \"\")\nmsg(len(r[1]))\nmsg(r[1][0][0])\nmsg(r[2][0])\nmsg(len(r[2][4]))\n", "", "1\n4\nIDENT\nprogram\n1\n")
	// 字句エラー: メッセージ＋空の tokens/tree。例外は起こさない。
	mustOutIO(t, "r = srcanalyze(\"msg('a')\")\nmsg(r[0] == \"\")\nmsg(len(r[1]))\nmsg(len(r[2]))\n", "", "0\n0\n0\n")
	// 解析エラーも同様（途中まで入力されたソース）。
	mustOutIO(t, "r = srcanalyze(\"msg(\")\nmsg(r[0] == \"\")\nmsg(len(r[1]))\n", "", "0\n0\n")
	// 生のディレクティブはここでも lextokens と同じく字句エラーである。
	mustOutIO(t, "r = srcanalyze(\"#define X 1\\n\")\nmsg(r[0] == \"\")\n", "", "0\n")
	mustErrIO(t, "srcanalyze(1)\n", "文字列である必要があります")
}

func TestParsetree(t *testing.T) {
	mustOutIO(t, "t = parsetree(\"def f(a, b) {\\nreturn a\\n}\\nmsg(f(1, 2))\\n\")\nmsg(t[0])\nmsg(len(t[4]))\nmsg(t[4][0][4])\nmsg(t[4][0][5][1][4])\nmsg(t[4][1][0])\nmsg(t[4][0][1])\n", "",
		"program\n2\nf\nb\ncall\n1\n")
	// 存在しない省略可能なものは ""（変数へ読み出せ、== で判定できる）。
	mustOutIO(t, "t = parsetree(\"if true {\\nmsg(1)\\n}\\n\")\nmsg(t[4][0][6] == \"\")\n", "", "1\n")
	mustOutIO(t, "t = parsetree(\"def f() {\\nreturn\\n}\\n\")\nmsg(t[4][0][6][0][4] == \"\")\n", "", "1\n")
	mustOutIO(t, "t = parsetree(\"repeat 3 {\\n}\\n\")\nmsg(t[4][0][5] == \"\")\n", "", "1\n")
	// 演算子はソースの記号を使う。式文は展開される。
	mustOutIO(t, "t = parsetree(\"x = 1 + 2 * 3\\n\")\nmsg(t[4][0][0])\nmsg(t[4][0][5][4])\nmsg(t[4][0][5][6][0])\n", "", "assign\n+\nbinary\n")
	mustOutIO(t, "msg(vartype(parsetree(\"x = 1\")))\n", "", "array\n")
	mustErrIO(t, "parsetree(1)\n", "文字列である必要があります")
	mustErrIO(t, "msg(parsetree(\"msg(\"))\n", "が必要です")
}

func TestStrWidth(t *testing.T) {
	mustOutIO(t, "msg(strwidth(\"abc\"))\nmsg(strwidth(\"あいう\"))\nmsg(strwidth(\"\"))\n", "", "3\n3\n0\n")
	mustErrIO(t, "strwidth(1)\n", "文字列である必要があります")
}
