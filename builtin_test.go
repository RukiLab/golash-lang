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

	"gsh/gui"
)

// runIO executes src with given stdin, capturing stdout and logmes output.
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
		"mes", "print", "cls", "color", "title", "pos", "input",
		"int", "float", "str", "vartype", "length",
		"abs", "sqrt", "sin", "cos", "tan", "atan", "exp", "log", "pow", "limit", "min", "max",
		"rnd", "randomize",
		"strlen", "strmid", "instr", "strtrim", "split", "strf", "getpath",
		"gettime", "sleep", "await", "tick", "nanotime", "end", "assert", "logmes", "exec", "execgui", "args",
		"getenv", "setenv", "open", "clipget", "clipset",
		"dlgopen", "dlgsave", "httpget", "httppost",
		"exist", "filetime", "fileinfo", "dirlist", "delete", "mkdir", "chdir", "bcopy", "bload", "bsave",
		"getcwd", "direxe", "homedir", "tmpdir",
		"notemax", "noteget", "noteadd", "notedel", "noteload", "notesave",
		"peek", "wpeek", "lpeek", "poke", "wpoke", "lpoke",
		"push", "pop", "join", "dim",
		"sort", "reverse", "insert", "remove", "slice", "find",
		"replace", "upper", "lower", "asc", "chr",
		"lextokens", "strwidth", "parsetree", "checksrc", "pipeexec", "piperun",
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
	// GUI words (builtin_gui*.go) are valid extras.
	for _, n := range guiBuiltinNames {
		delete(got, n)
	}
	for n := range got {
		t.Errorf("unexpected builtin %q (add to test or remove)", n)
	}
}

func TestBuiltinRedefineGuard(t *testing.T) {
	mustErrIO(t, "def sin(x) {\nreturn x\n}\n", "組み込み関数")
	mustErrIO(t, "def mes() {\n}\n", "組み込み関数")
}

func TestConversions(t *testing.T) {
	mustOutIO(t, "mes(int(3.9))\nmes(int(\" 12 \"))\nmes(int(true))\n", "", "3\n12\n1\n")
	mustOutIO(t, "mes(float(3))\nmes(float(\"2.5\"))\nmes(float(false))\n", "", "3\n2.5\n0\n")
	// int(string) is strict base-10 integers only: float spellings fail.
	mustErrIO(t, "mes(int(\"3.7\"))\n", "変換できません")
	mustErrIO(t, "mes(int(\"1e2\"))\n", "変換できません")
	mustErrIO(t, "mes(int(\"0x10\"))\n", "変換できません")
	// Out-of-range floats and NaN never convert to int.
	mustErrIO(t, "mes(int(1e300))\n", "変換できません")
	// NaN/Inf never enter through float() either.
	mustErrIO(t, "mes(float(\"nan\"))\n", "変換できません")
	mustErrIO(t, "mes(float(\"inf\"))\n", "変換できません")
	mustErrIO(t, "mes(float(\"Infinity\"))\n", "変換できません")
	mustOutIO(t, "mes(str(42))\nmes(str([1, 2]))\nmes(vartype(1))\nmes(vartype(1.5))\nmes(vartype(\"s\"))\nmes(vartype(true))\nmes(vartype([1]))\n", "", "42\n[1, 2]\nint\nfloat\nstring\nbool\narray\n")
	mustErrIO(t, "mes(int(\"abc\"))\n", "変換できません")
	mustErrIO(t, "mes(int([1]))\n", "変換できません")
	mustErrIO(t, "mes(float(\"x\"))\n", "変換できません")
	mustErrIO(t, "mes(int())\n", "1 個必要")
	mustErrIO(t, "mes(int(1, 2))\n", "1 個必要")
}

func TestMath(t *testing.T) {
	mustOutIO(t, "mes(abs(-5))\nmes(abs(5.5))\nmes(abs(0))\n", "", "5\n5.5\n0\n")
	mustOutIO(t, "mes(sqrt(16))\nmes(sin(0))\nmes(cos(0))\nmes(pow(2, 10))\nmes(exp(0))\nmes(log(1))\n", "", "4\n0\n1\n1024\n1\n0\n")
	mustOutIO(t, "mes(limit(5, 0, 10))\nmes(limit(-3, 0, 10))\nmes(limit(99, 0, 10))\nmes(limit(5.5, 0, 10))\n", "", "5\n0\n10\n5.5\n")
	mustOutIO(t, "mes(vartype(limit(5, 0, 10)))\nmes(vartype(limit(5.5, 0, 10)))\n", "", "int\nfloat\n")
	// limit() takes exactly 3 arguments: shorter forms and elision fail.
	mustErrIO(t, "mes(limit(5))\n", "3 個必要")
	mustErrIO(t, "mes(limit(5, 0))\n", "3 個必要")
	mustErrIO(t, "mes(limit(1, 2, 3, 4))\n", "3 個必要")
	mustErrIO(t, "mes(limit(5, , 10))\n", "void値")
	mustErrIO(t, "mes(limit(, 0, 10))\n", "void値")
	// Elision is still void; mes() skips it silently.
	mustOutIO(t, "mes(,)\n", "", "\n")
	// min/max: least/greatest of 2+ args; all-int yields int.
	mustOutIO(t, "mes(min(3, 1, 2))\nmes(max(3, 1, 2))\nmes(min(-5, -2))\nmes(max(-5, -2))\nmes(min(7, 7))\n", "", "1\n3\n-5\n-2\n7\n")
	mustOutIO(t, "mes(min(1.5, 2))\nmes(max(1, 2.5))\nmes(min(3, 1.5, 2))\n", "", "1.5\n2.5\n1.5\n")
	mustOutIO(t, "mes(vartype(min(1, 2)))\nmes(vartype(max(1.0, 2)))\n", "", "int\nfloat\n")
	mustErrIO(t, "mes(min(1))\n", "少なくとも 2 個")
	mustErrIO(t, "mes(max())\n", "少なくとも 2 個")
	mustErrIO(t, "mes(min(\"a\", 1))\n", "数値である必要があります")
	mustOutIO(t, "mes(atan(0))\nmes(tan(0))\n", "", "0\n0\n")
	mustErrIO(t, "mes(sqrt(-1))\n", "平方根")
	mustErrIO(t, "mes(log(0))\n", "正の数")
	mustErrIO(t, "mes(abs(\"x\"))\n", "数値である必要があります")
	// Non-finite float results are an error, never ±Inf/NaN.
	mustErrIO(t, "mes(exp(1000))\n", "有限の浮動小数ではありません")
	mustErrIO(t, "mes(pow(10, 309))\n", "有限の浮動小数ではありません")
	mustErrIO(t, "mes(pow(-1, 0.5))\n", "有限の浮動小数ではありません")
	// abs(MinInt64) has no representable negation; it is an error,
	// not a wrapped negative.
	mustErrIO(t, "mes(abs(-9223372036854775808))\n", "絶対値を取得できません")
	mustOutIO(t, "mes(abs(-9223372036854775807))\n", "", "9223372036854775807\n")
	mustErrIO(t, "mes(limit(1, 9, 2))\n", "下限")
}

func TestRandom(t *testing.T) {
	got, _, _, err := runIO(t, "randomize(42)\na = rnd(100)\nrandomize(42)\nb = rnd(100)\nmes(a == b)\nmes(a >= 0 && a < 100)\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "true\ntrue\n" {
		t.Fatalf("got %q", got)
	}
	mustErrIO(t, "mes(rnd(0))\n", "正の上限")
	mustErrIO(t, "mes(rnd(\"x\"))\n", "整数である必要があります")
	mustOutIO(t, "randomize()\nmes(rnd(10) >= 0 && rnd(10) < 10)\n", "", "true\n")
}

func TestBuiltinStrings(t *testing.T) {
	mustOutIO(t, "mes(strlen(\"hello\"))\nmes(strlen(\"あいう\"))\nmes(strlen(\"\"))\n", "", "5\n3\n0\n")
	mustOutIO(t, "mes(strmid(\"abcdef\", 1, 3))\nmes(strmid(\"あいうえお\", 1, 2))\nmes(strmid(\"abcdef\", -2, 2))\nmes(strmid(\"abc\", 1, 2))\n", "", "bcd\nいう\nef\nbc\n")
	// Over-length ends are an error, never a silent clamp (huge counts
	// wrap start+count, which is also an error).
	mustErrIO(t, "mes(strmid(\"abc\", 1, 99))\n", "超えています")
	mustErrIO(t, "mes(strmid(\"abc\", 1, 9223372036854775807))\n", "超えています")
	mustOutIO(t, "mes(instr(\"hello\", \"ll\"))\nmes(instr(\"hello\", 3, \"l\"))\nmes(instr(\"hello\", \"z\"))\n", "", "2\n3\n-1\n")
	// Out-of-range starts are an error, never a silent clamp or -1.
	mustErrIO(t, "mes(instr(\"hello\", 99, \"h\"))\n", "範囲外")
	mustErrIO(t, "mes(instr(\"hello\", -1, \"h\"))\n", "範囲外")
	mustOutIO(t, "mes(\"[\" + strtrim(\"  hi  \") + \"]\")\nmes(strtrim(\"xxhiix\", \"x\"))\nmes(strtrim(\"xxhi\", \"x\", 1))\nmes(strtrim(\"hixx\", \"x\", 2))\n", "", "[hi]\nhii\nhi\nhi\n")
	mustOutIO(t, "a = split(\"a,b,c\", \",\")\nmes(a[1])\nmes(length(a))\n", "", "b\n3\n")
	mustOutIO(t, "mes(strf(\"%02d\", 5))\nmes(strf(\"%s=%d\", \"n\", 7))\n", "", "05\nn=7\n")
	mustErrIO(t, "mes(strmid(\"abc\", 9, 1))\n", "範囲外")
	mustErrIO(t, "mes(strmid(\"abc\", 0, -1))\n", "0 以上")
	mustErrIO(t, "mes(split(\"a\", \"\"))\n", "空にすることはできません")
	mustErrIO(t, "mes(strtrim(\"a\", \"x\", 5))\n", "mode は")
}

func TestGetpath(t *testing.T) {
	mustOutIO(t, "p = \"C:\\\\a\\\\b.exe\"\nmes(getpath(p))\nmes(getpath(p, \"dir\", \"base\"))\nmes(getpath(p, \"ext\"))\nmes(getpath(p, \"file\"))\nmes(getpath(p, \"base\"))\nmes(getpath(p, \"dir\"))\nmes(getpath(p, \"lower\"))\n", "",
		"C:\\a\\b.exe\nC:\\a\\b\n.exe\nb.exe\nb\nC:\\a\\\nc:\\a\\b.exe\n")
	mustOutIO(t, "mes(getpath(\"/x/y.txt\", \"file\"))\nmes(getpath(\"/x/y.txt\", \"dir\"))\n", "", "y.txt\n/x/\n")
	// Composition: dir+file, base+ext (= file), dir lowercased.
	mustOutIO(t, "mes(getpath(\"/x/y.txt\", \"dir\", \"file\"))\nmes(getpath(\"/x/y.txt\", \"base\", \"ext\"))\nmes(getpath(\"/X/Y.TXT\", \"dir\", \"lower\"))\n", "", "/x/y.txt\ny.txt\n/x/\n")
	mustErrIO(t, "mes(getpath(\"a\", \"bogus\"))\n", "dir/file/base/ext/lower")
	mustErrIO(t, "mes(getpath(\"a\", \"file\", \"base\"))\n", "同時に指定できません")
	mustErrIO(t, "mes(getpath(\"a\", 8))\n", "文字列である必要があります")
}

func TestGettimeRanges(t *testing.T) {
	got, _, _, err := runIO(t, "mes(gettime(1) >= 1 && gettime(1) <= 12)\nmes(gettime(2) >= 0 && gettime(2) <= 6)\nmes(gettime(4) >= 0 && gettime(4) <= 23)\nmes(gettime(6) >= 0 && gettime(6) <= 59)\nmes(gettime(7) >= 0 && gettime(7) <= 999)\nmes(gettime(0) >= 2026)\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "true\ntrue\ntrue\ntrue\ntrue\ntrue\n" {
		t.Fatalf("got %q", got)
	}
	mustErrIO(t, "mes(gettime(8))\n", "0 から 7")
}

func TestSleepEndAssertLogmes(t *testing.T) {
	mustOutIO(t, "sleep(0)\nsleep(5)\nmes(\"ok\")\n", "", "ok\n")
	mustErrIO(t, "sleep(-1)\n", "0 以上")
	mustOutIO(t, "await(0)\nawait()\nmes(tick() >= 0)\n", "", "true\n")
	mustErrIO(t, "await(-1)\n", "0 以上")
	// A wait whose target tick is unrepresentable is an error, not an
	// immediate return via wrap.
	mustErrIO(t, "await(9223372036854775807)\n", "大きすぎます")
	mustOutIO(t, "a = nanotime()\nmes(a > 0 && nanotime() >= a)\n", "", "true\n")
	_, _, in, err := runIO(t, "mes(\"hi\")\nend(3)\nmes(\"bye\")\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if code, ok := in.ExitCode(); !ok || code != 3 {
		t.Fatalf("exit code: %d, %v", code, ok)
	}
	// The GUI frontend reads ExitCode() after the loop while the script
	// goroutine may still run; concurrent access must be race-free
	// (meaningful under -race).
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
	// nanotime() is monotonic (process-start anchored, never backward).
	mustOutIO(t, "a = nanotime()\nb = nanotime()\nmes(b >= a)\nmes(a >= 0)\n", "", "true\ntrue\n")
	mustOutIO(t, "assert(true)\nassert(1 < 2, \"math broke\")\nmes(\"ok\")\n", "", "ok\n")
	mustErrIO(t, "assert(false)\n", "assertion failed")
	mustErrIO(t, "assert(false, \"custom\")\n", "custom")
	mustErrIO(t, "assert(1)\n", "bool 型")
	_, errout, _, err := runIO(t, "logmes(\"dbg\", 42)\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if errout != "dbg 42\n" {
		t.Fatalf("logmes got %q", errout)
	}
	// Void arguments are skipped, like mes()/print().
	_, errout, _, err = runIO(t, "def f() {\n}\nlogmes(\"a\", f(), \"b\")\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if errout != "a b\n" {
		t.Fatalf("logmes void got %q", errout)
	}
}

func TestArgs(t *testing.T) {
	// No arguments: empty array.
	mustOutIO(t, "mes(args())\nmes(length(args()))\n", "", "[]\n0\n")
	// With arguments passed through SetArgs (as run does).
	prog, err := Parse("mes(args())\nmes(args()[1])\n")
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

// TestFileDialogBuilder locks the dlgopen/dlgsave configuration without
// opening an OS window: the start folder, the optional filter and the
// pre-filled save name.
func TestFileDialogBuilder(t *testing.T) {
	in := &Interp{}
	// dlgStartDir falls back to the working directory when the script
	// has no directory of its own.
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
	// The script directory wins over the working directory.
	scriptDir := t.TempDir()
	in.SetScriptDir(scriptDir)
	if got := dlgStartDir(in); got != scriptDir {
		t.Fatalf("dlgStartDir = %q, want %q (script dir)", got, scriptDir)
	}
	// A script directory that no longer exists is skipped.
	in.SetScriptDir(filepath.Join(scriptDir, "gone"))
	if got := dlgStartDir(in); got != wd {
		t.Fatalf("dlgStartDir = %q, want %q after a missing script dir", got, wd)
	}
	in.SetScriptDir(scriptDir)
	b = dlgFileBuilder(in, "保存", "", "out.png")
	if b.StartDir != scriptDir || b.StartFile != "out.png" {
		t.Fatalf("save builder: dir %q name %q", b.StartDir, b.StartFile)
	}
	// A malformed filter and an empty name configure nothing.
	b = dlgFileBuilder(in, "保存", "no pipe here", "")
	if len(b.Filters) != 0 || b.StartFile != "" {
		t.Fatalf("empty filter must not configure anything: %+v", b)
	}
}

func TestEnv(t *testing.T) {
	mustOutIO(t, "setenv(\"GOU_TEST\", \"42\")\nmes(getenv(\"GOU_TEST\"))\nmes(getenv(\"GOU_NO_SUCH_VAR_XYZ\") == \"\")\n", "", "42\ntrue\n")
	mustErrIO(t, "getenv(1)\n", "文字列である必要があります")
	mustErrIO(t, "setenv(\"a\")\n", "2 個必要")
	mustErrIO(t, "open(1)\n", "文字列である必要があります")
	mustErrIO(t, "open(\"\")\n", "空にすることはできません")
	mustErrIO(t, "dlgopen(1)\n", "文字列である必要があります")
	mustErrIO(t, "dlgsave(1)\n", "文字列である必要があります")
	mustErrIO(t, "dlgsave(\"\", 2)\n", "文字列である必要があります")
	mustErrIO(t, "dlgsave(\"a\", \"b\", \"c\")\n", "0 から 2 個")
	mustErrIO(t, "httpget(1)\n", "文字列である必要があります")
	mustOutIO(t, "clipset(\"gsh\")\nmes(clipget())\n", "", "gsh\n")
	mustErrIO(t, "clipset(1)\n", "文字列である必要があります")
}

func TestHttpget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			fmt.Fprint(w, "hello")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	mustOutIO(t, fmt.Sprintf("mes(httpget(%q))\n", srv.URL+"/ok"), "", "hello\n")
	mustErrIO(t, fmt.Sprintf("mes(httpget(%q))\n", srv.URL+"/missing"), "404")
	// Save form writes the body and returns its size.
	dir := t.TempDir()
	join := func(n string) string { return filepath.ToSlash(filepath.Join(dir, n)) }
	mustOutIO(t, fmt.Sprintf("mes(httpget(%q, %q))\n", srv.URL+"/ok", join("out.txt")), "", "5\n")
	data, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("saved body = %q, %v", data, err)
	}
	// Unreachable host is an error, not a hang (client timeout).
	mustErrIO(t, "mes(httpget(\"http://127.0.0.1:1/nope\"))\n", "httpget")
}

func TestHttppost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "want POST", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s|%s", r.Header.Get("Content-Type"), body)
	}))
	defer srv.Close()
	// Default content type is application/json.
	mustOutIO(t, fmt.Sprintf("mes(httppost(%q, %q))\n", srv.URL, `{"a":1}`), "", "application/json|{\"a\":1}\n")
	// Explicit content type.
	mustOutIO(t, fmt.Sprintf("mes(httppost(%q, %q, %q))\n", srv.URL, "x=1", "application/x-www-form-urlencoded"), "", "application/x-www-form-urlencoded|x=1\n")
	// Save form writes the body and returns its size.
	dir := t.TempDir()
	out := filepath.ToSlash(filepath.Join(dir, "post.txt"))
	mustOutIO(t, fmt.Sprintf("mes(httppost(%q, %q, %q, %q))\n", srv.URL, "hi", "text/plain", out), "", "13\n")
	data, err := os.ReadFile(filepath.Join(dir, "post.txt"))
	if err != nil || string(data) != "text/plain|hi" {
		t.Fatalf("saved body = %q, %v", data, err)
	}
	mustErrIO(t, "mes(httppost(1, \"x\"))\n", "文字列である必要があります")
	mustErrIO(t, "mes(httppost(\"http://127.0.0.1:1/nope\", \"x\"))\n", "httppost")
}

func TestExec(t *testing.T) {
	// Async launch: returns 0 once started, without waiting.
	if runtime.GOOS == "windows" {
		mustOutIO(t, "mes(exec(\"cmd\", \"/c\", \"exit\", \"3\"))\n", "", "0\n")
	} else {
		mustOutIO(t, "mes(exec(\"sh\", \"-c\", \"exit 3\"))\n", "", "0\n")
	}
	mustErrIO(t, "mes(exec(\"no-such-command-xyz\"))\n", "exec：")
}

// TestExecgui mirrors TestExec for the detached twin: async launch
// returns 0 once started, start failure is an error (only the console
// differs, which output-based tests cannot observe).
func TestExecgui(t *testing.T) {
	if runtime.GOOS == "windows" {
		mustOutIO(t, "mes(execgui(\"cmd\", \"/c\", \"exit\", \"3\"))\n", "", "0\n")
	} else {
		mustOutIO(t, "mes(execgui(\"sh\", \"-c\", \"exit 3\"))\n", "", "0\n")
	}
	mustErrIO(t, "mes(execgui(\"no-such-command-xyz\"))\n", "execgui：")
	mustErrIO(t, "mes(execgui(1))\n", "execgui")
}

// TestChildOutWriters locks the child-output guard: a real *os.File
// passes through (the child inherits the handle, no copy goroutine), an
// in-process writer is wrapped so os/exec's copy goroutine cannot race
// the interpreter's own writes. exec/pipeexec hand the interpreter's
// output to a child, and an unwrapped bytes.Buffer lost whole writes
// under that race (TestExec flaked about 10% of runs).
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
		mustOutIO(t, "mes(pipeexec(\"cmd\", \"/c\", \"echo\", \"hi\"))\n", "", "hi\r\n\n")
	} else {
		mustOutIO(t, "mes(pipeexec(\"printf\", \"hi\"))\n", "", "hi\n")
	}
	mustErrIO(t, "mes(pipeexec(\"no-such-command-xyz\"))\n", "pipeexec：")
	if runtime.GOOS == "windows" {
		mustErrIO(t, "mes(pipeexec(\"cmd\", \"/c\", \"exit\", \"3\"))\n", "終了コード 3")
	} else {
		mustErrIO(t, "mes(pipeexec(\"sh\", \"-c\", \"exit 3\"))\n", "終了コード 3")
	}
}

func TestPiperun(t *testing.T) {
	if runtime.GOOS == "windows" {
		mustOutIO(t, "r = piperun(\"cmd\", \"/c\", \"echo\", \"hi\")\nmes(r[0])\nmes(r[1])\nmes(r[2])\n", "", "0\nhi\r\n\n\n")
	} else {
		mustOutIO(t, "r = piperun(\"printf\", \"hi\")\nmes(r[0])\nmes(r[1])\nmes(r[2])\n", "", "0\nhi\n\n")
	}
	// Non-zero exits are values, not errors.
	if runtime.GOOS == "windows" {
		mustOutIO(t, "r = piperun(\"cmd\", \"/c\", \"exit\", \"3\")\nmes(r[0])\nmes(r[1])\n", "", "3\n\n")
	} else {
		mustOutIO(t, "r = piperun(\"sh\", \"-c\", \"exit 3\")\nmes(r[0])\nmes(r[1])\n", "", "3\n\n")
	}
	// Stderr is captured into r[2].
	if runtime.GOOS == "windows" {
		mustOutIO(t, "r = piperun(\"cmd\", \"/c\", \"echo oops>&2\")\nmes(r[0])\nmes(r[1])\nmes(r[2])\n", "", "0\n\noops\r\n\n")
	} else {
		mustOutIO(t, "r = piperun(\"sh\", \"-c\", \"echo oops 1>&2\")\nmes(r[0])\nmes(r[1])\nmes(r[2])\n", "", "0\n\noops\n\n")
	}
	// Start failure is [-1, "", message], never an error.
	mustOutIO(t, "r = piperun(\"no-such-command-xyz\")\nmes(r[0])\nmes(r[2] != \"\")\n", "", "-1\ntrue\n")
}

// GUI calls (mousedown included) fail cleanly outside a window.
func TestMouseDownOutsideWindow(t *testing.T) {
	mustErrIO(t, "mes(mousedown())\n", "GUI")
}

func TestAscChr(t *testing.T) {
	mustOut(t, "mes(asc(\"A\"))\nmes(asc(\"あ\"))\nmes(chr(65))\nmes(chr(12354))\n", "65\n12354\nA\nあ\n")
	mustErr(t, "asc(\"\")\n", "空文字列")
	mustErr(t, "chr(-1)\n", "無効なコードポイント")
	mustErr(t, "chr(55296)\n", "無効なコードポイント")
	mustErr(t, "chr(1114112)\n", "無効なコードポイント")
}

func TestConsoleIO(t *testing.T) {
	mustOutIO(t, "print(\"a\", 1)\nprint(\"b\")\nmes(\"c\")\n", "", "a 1bc\n")
	mustOutIO(t, "cls()\n", "", "\x1b[2J\x1b[H")
	mustOutIO(t, "color(255, 0, 0)\ncolor()\n", "", "\x1b[38;2;255;0;0m\x1b[0m")
	mustOutIO(t, "color(255, 0, 0, 128)\ncolor()\n", "", "\x1b[38;2;255;0;0m\x1b[0m")
	mustOutIO(t, "color(0xFF0000)\ncolor()\n", "", "\x1b[38;2;255;0;0m\x1b[0m")
	mustOutIO(t, "pos(3, 4)\n", "", "\x1b[5;4H")
	mustOutIO(t, "pos(-1, 0)\n", "", "\x1b[1;0H")
	mustOutIO(t, "pos(-5, -3)\n", "", "\x1b[-2;-4H")
	// mes()/print() follow the cursor on CUI too (terminal positions).
	mustOutIO(t, "pos(5, 2)\nmes(\"hi\")\n", "", "\x1b[3;6Hhi\n")
	mustOutIO(t, "pos(1, 1)\nprint(\"ab\")\npos(4, 1)\nprint(\"cd\")\n", "", "\x1b[2;2Hab\x1b[2;5Hcd")
	mustOutIO(t, "title(\"T\")\n", "", "\x1b]0;T\x07")
	mustErrIO(t, "title(\"a\\0b\")\n", "NUL")
	mustOutIO(t, "print(\"x\", \"bold\")\nprint(\"y\")\n", "", "\x1b[1mx\x1b[22;23;24my")
	mustErrIO(t, "color(1, 2)\n", "0 個、1 個、3 個または 4 個")
	mustErrIO(t, "color(300, 0, 0)\n", "0 から 255")
	mustErrIO(t, "color(0x1000000)\n", "0x000000")
	mustErrIO(t, "color(-1)\n", "0x000000")
}

func TestSplitStyleArgs(t *testing.T) {
	rest, st := splitStyleArgs([]Value{Str("a"), Str("b"), Str("bold"), Str("underline")})
	if len(rest) != 2 || st != gui.StyleBold|gui.StyleUnderline {
		t.Fatalf("got %v, %v", rest, st)
	}
	rest, st = splitStyleArgs([]Value{Str("bolditalic")})
	if len(rest) != 0 || st != gui.StyleBold|gui.StyleItalic {
		t.Fatalf("got %v, %v", rest, st)
	}
	// A trailing keyword is consumed even after non-strings; the
	// non-string itself stays printable. Near-misses stay printable.
	rest, st = splitStyleArgs([]Value{Str("a"), Int(1), Str("bold")})
	if len(rest) != 2 || st != gui.StyleBold {
		t.Fatalf("got %v, %v", rest, st)
	}
	rest, st = splitStyleArgs([]Value{Str("a"), Str("Bold")})
	if len(rest) != 2 || st != 0 {
		t.Fatalf("got %v, %v", rest, st)
	}
}

func TestInput(t *testing.T) {
	mustOutIO(t, "s = input()\nmes(s)\n", "hello\n", "hello\n")
	mustOutIO(t, "s = input(\"name? \")\nmes(s)\n", "bob\n", "name? bob\n")
	mustOutIO(t, "mes(input())\n", "", "\n")                    // EOF yields ""
	mustOutIO(t, "s = input()\nmes(s == \"\")\n", "", "true\n") // EOF
}

// runScriptDir runs src with the working directory and script directory
// given, returning captured stdout.
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
	dirA := t.TempDir() // script home
	dirB := t.TempDir() // working directory
	write := func(dir, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o666); err != nil {
			t.Fatal(err)
		}
	}
	write(dirA, "data.txt", "from-a")
	write(dirB, "data.txt", "from-b")
	write(dirB, "only-b.txt", "b")

	// Script dir wins over the working directory.
	if got := runScriptDir(t, "mes(noteload(\"data.txt\"))\n", dirB, dirA); got != "from-a\n" {
		t.Fatalf("script-dir first: got %q", got)
	}
	// Working directory is the fallback.
	if got := runScriptDir(t, "mes(noteload(\"only-b.txt\"))\n", dirB, dirA); got != "b\n" {
		t.Fatalf("cwd fallback: got %q", got)
	}
	// Absolute paths pass through untouched.
	abs := filepath.ToSlash(filepath.Join(dirB, "data.txt"))
	if got := runScriptDir(t, fmt.Sprintf("mes(noteload(%q))\n", abs), dirB, dirA); got != "from-b\n" {
		t.Fatalf("absolute: got %q", got)
	}
	// New files land beside the script, not in the working directory.
	runScriptDir(t, "notesave(\"new.txt\", \"hi\")\n", dirB, dirA)
	if _, err := os.Stat(filepath.Join(dirA, "new.txt")); err != nil {
		t.Fatalf("save beside script: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirB, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("save must not land in cwd: %v", err)
	}
	// No script dir (REPL): today's working-directory behavior.
	if got := runScriptDir(t, "mes(noteload(\"only-b.txt\"))\n", dirB, ""); got != "b\n" {
		t.Fatalf("repl cwd: got %q", got)
	}
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	// Forward slashes: the script language has no backslash escapes besides
	// \" \\ \n \t \r, and Windows accepts forward slashes.
	join := func(n string) string { return filepath.ToSlash(filepath.Join(dir, n)) }

	mustOutIO(t, "mes(exist(\""+join("nope")+"\"))\n", "", "false\n")
	mustErrIO(t, "delete(\""+join("nope")+"\")\n", "delete：")

	// notesave/noteload + exist roundtrip.
	mustOutIO(t, "notesave(\""+join("t.txt")+"\", \"a\\nb\")\nmes(exist(\""+join("t.txt")+"\"))\nmes(noteload(\""+join("t.txt")+"\"))\n", "",
		"true\na\nb\n")

	// bsave/bload roundtrip with size clip.
	mustOutIO(t, "bsave(\""+join("b.bin")+"\", [65, 66, 67])\na = bload(\""+join("b.bin")+"\")\nmes(a)\nmes(length(a))\nbsave(\""+join("c.bin")+"\", [1, 2, 3], 2)\nmes(length(bload(\""+join("c.bin")+"\")))\n", "",
		"[65, 66, 67]\n3\n2\n")
	mustErrIO(t, "bsave(\""+join("x.bin")+"\", [256])\n", "0 から 255")
	// A huge size is a range error, never a Go panic, and writes nothing.
	mustErrIO(t, "bsave(\""+join("y.bin")+"\", [1, 2, 3], 9223372036854775807)\n", "サイズが範囲外")
	if _, err := os.Stat(filepath.Join(dir, "y.bin")); !os.IsNotExist(err) {
		t.Fatalf("bsave with huge size must not create the file: %v", err)
	}
	mustErrIO(t, "mes(bload(\""+join("missing.bin")+"\"))\n", "bload：")

	// mkdir/delete/bcopy.
	mustOutIO(t, "mkdir(\""+join("sub")+"\")\nmes(exist(\""+join("sub")+"\"))\n", "", "true\n")
	mustOutIO(t, "bcopy(\""+join("t.txt")+"\", \""+join("t2.txt")+"\")\nmes(exist(\""+join("t2.txt")+"\"))\ndelete(\""+join("t2.txt")+"\")\nmes(exist(\""+join("t2.txt")+"\"))\n", "",
		"true\nfalse\n")

	// dirlist + chdir (restore cwd afterwards).
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
	mustOutIO(t, "a = dirlist(\"*.gsh\")\nmes(a)\n", "", "[z.gsh]\n")
}

// TestFileinfo pins the one-stat metadata triple [size, mtime_ms,
// isdir]: regular file, missing path, and directory.
func TestFileinfo(t *testing.T) {
	dir := t.TempDir()
	join := func(n string) string { return filepath.ToSlash(filepath.Join(dir, n)) }

	mustOutIO(t, "notesave(\""+join("t.txt")+"\", \"a\\nb\")\nr = fileinfo(\""+join("t.txt")+"\")\nmes(r[0])\nmes(r[1] > 0)\nmes(r[2])\n", "",
		"3\ntrue\nfalse\n")
	mustOutIO(t, "r = fileinfo(\""+join("nope")+"\")\nmes(r[0])\nmes(r[1])\nmes(r[2])\n", "",
		"-1\n0\nfalse\n")
	mustOutIO(t, "mkdir(\""+join("sub")+"\")\nr = fileinfo(\""+join("sub")+"\")\nmes(r[1] > 0)\nmes(r[2])\n", "",
		"true\ntrue\n")
	mustErrIO(t, "mes(fileinfo(1))\n", "fileinfo")
}

func TestDirs(t *testing.T) {
	mustOutIO(t, "mes(strlen(getcwd()) > 0)\nmes(strlen(direxe()) > 0)\nmes(strlen(homedir()) > 0)\nmes(strlen(tmpdir()) > 0)\n", "", "true\ntrue\ntrue\ntrue\n")
	mustOutIO(t, "mes(exist(getcwd()))\n", "", "true\n")
}

func TestNotes(t *testing.T) {
	mustOutIO(t, "mes(notemax(\"a\\nb\\nc\"))\nmes(notemax(\"\"))\n", "", "3\n0\n")
	mustOutIO(t, "mes(noteget(\"a\\nb\", 0))\nmes(noteget(\"a\\nb\", 1))\n", "", "a\nb\n")
	mustOutIO(t, "mes(noteadd(\"a\\nb\", 1, \"B\"))\nmes(noteadd(\"a\", 1, \"b\"))\n", "", "a\nB\na\nb\n")
	mustOutIO(t, "mes(notedel(\"a\\nb\\nc\", 1))\n", "", "a\nc\n")
	mustErrIO(t, "mes(noteget(\"a\", 5))\n", "範囲外")
	mustErrIO(t, "mes(notedel(\"a\", 2))\n", "範囲外")
	mustErrIO(t, "mes(noteadd(\"a\", 9, \"x\"))\n", "範囲外")
}

func TestPeekPoke(t *testing.T) {
	mustOutIO(t, "a = [0, 0, 0, 0]\npoke(a, 0, 65)\nmes(peek(a, 0))\nwpoke(a, 1, 16961)\nmes(wpeek(a, 1))\nlpoke(a, 0, 16909060)\nmes(lpeek(a, 0))\nmes(a)\n", "",
		"65\n16961\n16909060\n[4, 3, 2, 1]\n")
	mustErrIO(t, "mes(peek([1], 5))\n", "範囲外")
	mustErrIO(t, "mes(peek([1], -1))\n", "範囲外")
	mustErrIO(t, "poke([0], 0, 256)\n", "0 から 255")
	mustErrIO(t, "mes(peek([\"x\"], 0))\n", "バイト")
	mustErrIO(t, "mes(lpeek([1, 2], 0))\n", "範囲外")
	// Huge indexes must be range errors, never a Go panic (int(i)+n
	// wraps on MaxInt64 and used to skip the bounds check).
	mustErrIO(t, "mes(peek([1], 9223372036854775807))\n", "範囲外")
	mustErrIO(t, "mes(wpeek([1, 2], 9223372036854775807))\n", "範囲外")
	mustErrIO(t, "mes(lpeek([1, 2, 3, 4], 9223372036854775804))\n", "範囲外")
	mustErrIO(t, "poke([0], 9223372036854775807, 1)\n", "範囲外")
}

func TestPushPopJoin(t *testing.T) {
	mustOutIO(t, "a = []\nmes(push(a, 1))\nmes(push(a, 2, 3))\nmes(a)\n", "", "1\n3\n[1, 2, 3]\n")
	mustOutIO(t, "a = [1, 2, 3]\nmes(pop(a))\nmes(a)\n", "", "3\n[1, 2]\n")
	mustOutIO(t, "mes(join([1, 2, 3]))\nmes(join([\"a\", \"b\"], \"-\"))\nmes(join([], \",\"))\nmes(join([1, 2, 3], \",\"))\n", "", "123\na-b\n\n1,2,3\n")
	// Shared reference: push through an alias is visible.
	mustOutIO(t, "a = [1]\nb = a\npush(b, 2)\nmes(a)\n", "", "[1, 2]\n")
	mustErrIO(t, "mes(pop([]))\n", "配列が空")
	mustErrIO(t, "push(1, 2)\n", "配列である必要があります")
	mustErrIO(t, "mes(join([1], 2))\n", "文字列である必要があります")
}

func TestArrayExtras(t *testing.T) {
	mustOutIO(t, "a = [3, 1, 2]\nmes(sort(a))\n", "", "[1, 2, 3]\n")
	mustOutIO(t, "a = [\"b\", \"a\"]\nsort(a)\nmes(a)\n", "", "[a, b]\n")
	mustOutIO(t, "a = [1, 2, 3]\nreverse(a)\nmes(a)\n", "", "[3, 2, 1]\n")
	mustOutIO(t, "a = [1, 3]\nmes(insert(a, 1, 2))\nmes(a)\n", "", "3\n[1, 2, 3]\n")
	mustOutIO(t, "a = [1, 2, 3, 4]\nmes(remove(a, 1, 2))\nmes(a)\n", "", "[2, 3]\n[1, 4]\n")
	mustOutIO(t, "a = [1, 2, 3]\nmes(remove(a, 0))\nmes(a)\n", "", "[1]\n[2, 3]\n")
	mustOutIO(t, "a = [1, 2, 3, 4]\nmes(slice(a, 1, 3))\nmes(slice(a, -2))\nmes(slice(a, 0, -1))\n", "", "[2, 3]\n[3, 4]\n[1, 2, 3]\n")
	mustOutIO(t, "a = [\"x\", \"y\"]\nmes(find(a, \"y\"))\nmes(find(a, \"z\"))\nmes(find(a, 1))\n", "", "1\n-1\n-1\n")
	// Chaining: sort returns the array itself.
	mustOutIO(t, "mes(join(sort([3, 1, 2]), \"-\"))\n", "", "1-2-3\n")
	// sort accepts ints, floats, and mixed numerics; strings sort
	// separately; anything else is an error.
	mustOutIO(t, "a = [2.5, 1.5, 10]\nmes(sort(a))\n", "", "[1.5, 2.5, 10]\n")
	mustOutIO(t, "a = [2, 1.5, 10]\nmes(sort(a))\n", "", "[1.5, 2, 10]\n")
	mustErrIO(t, "sort([1, \"a\"])\n", "すべて数値かすべて文字列")
	mustErrIO(t, "sort([true])\n", "すべて数値かすべて文字列")
	mustErrIO(t, "insert([1], 5, 2)\n", "範囲外")
	mustErrIO(t, "remove([1], 0, 2)\n", "範囲外")
	// Huge index/count must be range errors, never a Go panic.
	mustErrIO(t, "insert([1], 9223372036854775807, 2)\n", "範囲外")
	mustErrIO(t, "remove([1, 2], 9223372036854775807, 1)\n", "範囲外")
	mustErrIO(t, "remove([1, 2], 0, 9223372036854775807)\n", "範囲外")
	mustErrIO(t, "slice([1, 2], 2, 1)\n", "範囲外")
	mustErrIO(t, "sort(1)\n", "配列である必要があります")
}

func TestStringExtras(t *testing.T) {
	mustOutIO(t, "mes(replace(\"aaa\", \"a\", \"b\"))\nmes(replace(\"hello\", \"z\", \"y\"))\n", "", "bbb\nhello\n")
	mustOutIO(t, "mes(upper(\"abc-あ\"))\nmes(lower(\"ABC-ア\"))\n", "", "ABC-あ\nabc-ア\n")
	mustErrIO(t, "replace(\"a\", \"\", \"b\")\n", "空にすることはできません")
	mustErrIO(t, "upper(1)\n", "文字列である必要があります")
}

func TestVartypeFunc(t *testing.T) {
	mustErrIO(t, "def f() {\n}\nmes(vartype(f))\n", "未定義の変数")
	mustOutIO(t, "def f() {\n}\nmes(vartype([1]))\nmes(f())\n", "", "array\n\n")
}

func TestLexTokens(t *testing.T) {
	mustOutIO(t, "t = lextokens(\"x = 1\")\nmes(length(t))\nmes(t[0][0])\nmes(t[0][1])\nmes(t[1][1])\nmes(t[2][1])\nmes(t[0][2])\nmes(t[0][3])\n", "", "3\nIDENT\nx\n=\n1\n1\n1\n")
	mustOutIO(t, "t = lextokens(\"if x {\\n}\")\nmes(t[0][0])\nmes(t[2][0])\nmes(t[3][0])\n", "", "IF\nLBRACE\nNEWLINE\n")
	mustErrIO(t, "lextokens(\"'a'\")\n", "シングルクォート")
	mustErrIO(t, "lextokens(1)\n", "文字列である必要があります")
}

func TestParsetree(t *testing.T) {
	mustOutIO(t, "t = parsetree(\"def f(a, b) {\\nreturn a\\n}\\nmes(f(1, 2))\\n\")\nmes(t[0])\nmes(length(t[4]))\nmes(t[4][0][4])\nmes(t[4][0][5][1][4])\nmes(t[4][1][0])\nmes(t[4][0][1])\n", "",
		"program\n2\nf\nb\ncall\n1\n")
	// Absent optionals are "" (readable into variables, testable with ==).
	mustOutIO(t, "t = parsetree(\"if true {\\nmes(1)\\n}\\n\")\nmes(t[4][0][6] == \"\")\n", "", "true\n")
	mustOutIO(t, "t = parsetree(\"def f() {\\nreturn\\n}\\n\")\nmes(t[4][0][6][0][4] == \"\")\n", "", "true\n")
	mustOutIO(t, "t = parsetree(\"repeat 3 {\\n}\\n\")\nmes(t[4][0][5] == \"\")\n", "", "true\n")
	// Operators use source symbols; expression statements unwrap.
	mustOutIO(t, "t = parsetree(\"x = 1 + 2 * 3\\n\")\nmes(t[4][0][0])\nmes(t[4][0][5][4])\nmes(t[4][0][5][6][0])\n", "", "assign\n+\nbinary\n")
	mustOutIO(t, "mes(vartype(parsetree(\"x = 1\")))\n", "", "array\n")
	mustErrIO(t, "parsetree(1)\n", "文字列である必要があります")
	mustErrIO(t, "mes(parsetree(\"mes(\"))\n", "が必要です")
}

func TestStrWidth(t *testing.T) {
	mustOutIO(t, "mes(strwidth(\"abc\"))\nmes(strwidth(\"あいう\"))\nmes(strwidth(\"\"))\n", "", "3\n3\n0\n")
	mustErrIO(t, "strwidth(1)\n", "文字列である必要があります")
}
