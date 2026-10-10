// vmCases pins VM behavior for the former tree/VM parity corpus:
// standard output, error text (with positions), and exit codes must
// match exactly. Expectations were baked from the agreed tree/VM
// behavior (the suite was green on both backends).
package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

type vmCase struct {
	src     string
	out     string
	err     string
	code    int
	hasExit bool
}

type vmFileCase struct {
	file    string
	out     string
	err     string
	code    int
	hasExit bool
}

var vmCases = []vmCase{
	{"x = 10 + 2 * 3\nmes(x)\n", "16\n", "", 0, false},
	{"mes((10 + 2) * 3)\n", "36\n", "", 0, false},
	{"mes(10 % 3)\nmes(7 / 2)\nmes(7.0 / 2)\n", "1\n3\n3.5\n", "", 0, false},
	{"mes(-5 + 8)\nmes(!true)\n", "3\nfalse\n", "", 0, false},
	{"mes(1 + 2.5)\nmes(10 - 3.5)\nmes(2 * 1.5)\n", "3.5\n6.5\n3\n", "", 0, false},
	{"mes(1 / 0)\n", "", "1:7: 0 による除算です", 0, false},
	{"mes(1.0 / 0.0)\n", "", "1:9: 0 による除算です", 0, false},
	{"mes(1 % 0)\n", "", "1:7: 0 による剰余演算です", 0, false},
	{"mes(1.5 % 2)\n", "", "1:9: 演算子 % は整数が必要です。float と int が指定されました", 0, false},
	{"mes(1 + true)\n", "", "1:7: int と bool を加算できません", 0, false},
	{"mes(true + false)\n", "", "1:10: bool と bool を加算できません", 0, false},
	{"mes(1 - true)\nmes(2 * \"a\")\n", "", "1:7: 演算子 - は数値が必要です。int と bool が指定されました", 0, false},
	{"mes(1 & 3)\nmes(1 | 2)\nmes(1 ^ 3)\nmes(1 << 4)\nmes(256 >> 3)\n", "1\n3\n2\n16\n32\n", "", 0, false},
	{"mes(1.5 & 1)\nmes(1 << -1)\nmes(1 << 64)\n", "", "1:9: 演算子 & は整数が必要です。float と int が指定されました", 0, false},
	{"mes(1 < 2)\nmes(\"a\" < \"b\")\nmes(1 == 1.0)\nmes(1 != 2)\n", "true\ntrue\ntrue\ntrue\n", "", 0, false},
	{"mes(1 < \"a\")\n", "", "1:7: int と string を比較できません（< <= > >= は数値または文字列のみ対応しています）", 0, false},
	{"mes([1,2] == [1,2])\n", "false\n", "", 0, false},
	{"a = [1,2]\nb = a\nmes(a == b)\n", "true\n", "", 0, false},
	{"mes(-\"a\")\nmes(~1.5)\nmes(~7)\n", "", "1:5: string を負数化できません", 0, false},
	{"def void_fn() {\nreturn\n}\nmes(1 == void_fn())\n", "", "4:17: void値を使用できません", 0, false},
	{"name = \"Alice\"\nmes(\"Hello, \" + name)\n", "Hello, Alice\n", "", 0, false},
	{"mes(\"n=\" + 42)\nmes(1 + \"a\" + 2)\n", "n=42\n1a2\n", "", 0, false},
	{"def void_fn() {\nreturn\n}\nmes(\"a\" + void_fn())\n", "", "4:9: void値を使用できません", 0, false},
	{"def void_fn() {\nreturn\n}\nx = void_fn()\n", "", "4:1: void値を使用できません", 0, false},
	{"def void_fn() {\nreturn\n}\nmes(void_fn(), \"x\")\n", "x\n", "", 0, false},
	{"if 1 < 2 && 2 < 3 || false {\nmes(\"y\")\n}\n", "y\n", "", 0, false},
	{"if 1 {\nmes(1)\n}\n", "", "1:4: 条件式は bool 型である必要があります。int が指定されました", 0, false},
	{"x = 1 && true\n", "", "1:5: 条件式は bool 型である必要があります。int が指定されました", 0, false},
	{"mes(!1)\n", "", "1:6: 条件式は bool 型である必要があります。int が指定されました", 0, false},
	{"mes(true && false)\nmes(false || 1 < 2)\n", "false\ntrue\n", "", 0, false},
	{"def void_fn() {\nreturn\n}\nmes(true || void_fn())\nmes(false && void_fn())\n", "true\nfalse\n", "", 0, false},
	{"def void_fn() {\nreturn\n}\nmes(true && void_fn())\n", "", "4:20: 条件式は bool 型である必要があります。void が指定されました", 0, false},
	{"x = 3\nif x > 10 {\nmes(1)\n} else if x > 5 {\nmes(2)\n} else {\nmes(3)\n}\n", "3\n", "", 0, false},
	{"i = 0\nwhile i < 3 {\nmes(i)\ni += 1\n}\n", "0\n1\n2\n", "", 0, false},
	{"while 1 {\nmes(\"x\")\n}\n", "", "1:7: 条件式は bool 型である必要があります。int が指定されました", 0, false},
	{"repeat 3 as i {\nmes(i)\n}\n", "0\n1\n2\n", "", 0, false},
	{"repeat 3 {\nmes(\"a\")\n}\n", "a\na\na\n", "", 0, false},
	{"repeat 0 as i {\nmes(i)\n}\nmes(\"done\")\n", "done\n", "", 0, false},
	{"repeat -1 {\nmes(1)\n}\n", "", "1:8: repeat の回数は 0 以上である必要があります。-1 が指定されました", 0, false},
	{"repeat \"s\" {\nmes(1)\n}\n", "", "1:8: repeat は整数または配列が必要です。string が指定されました", 0, false},
	{"repeat 3 as i, x {\nmes(i)\n}\n", "", "1:1: 整数の repeat では変数は 1 つまでです（as i, item は配列専用です）", 0, false},
	{"a = [10, 20, 30]\nrepeat a as x {\nmes(x)\n}\n", "10\n20\n30\n", "", 0, false},
	{"a = [10, 20, 30]\nrepeat a as i, x {\nmes(i)\nmes(x)\n}\n", "0\n10\n1\n20\n2\n30\n", "", 0, false},
	{"a = [1,2]\nrepeat a as x {\npush(a, 99)\nmes(x)\n}\nmes(length(a))\n", "1\n2\n4\n", "", 0, false},
	{"repeat 3 as i {\ni = 99\n}\n", "", "2:1: \"i\" に代入できません：repeat カウンタは読み取り専用です", 0, false},
	{"x = 99\nrepeat 3 as x {\nmes(x)\n}\nmes(x)\n", "0\n1\n2\n99\n", "", 0, false},
	{"repeat 2 as i {\nrepeat 2 as i {\nmes(i)\n}\n}\n", "0\n1\n0\n1\n", "", 0, false},
	{"repeat 5 as i {\nif i == 2 {\nbreak\n}\nmes(i)\n}\n", "0\n1\n", "", 0, false},
	{"repeat 5 as i {\nif i == 2 {\ncontinue\n}\nmes(i)\n}\n", "0\n1\n3\n4\n", "", 0, false},
	{"x = 2\nswitch x {\ncase 1 {\nmes(1)\n}\ncase 2, 3 {\nmes(23)\n}\ndefault {\nmes(\"d\")\n}\n}\n", "23\n", "", 0, false},
	{"x = 9\nswitch x {\ncase 1 {\nmes(1)\n}\ndefault {\nmes(\"d\")\n}\n}\n", "d\n", "", 0, false},
	{"x = 1\nswitch x {\ncase 1 {\nmes(1)\n}\n}\nmes(\"after\")\n", "1\nafter\n", "", 0, false},
	{"def void_fn() {\nreturn\n}\nswitch void_fn() {\ncase 1 {\nmes(1)\n}\n}\n", "", "4:15: void値を使用できません", 0, false},
	{"def void_fn() {\nreturn\n}\nswitch 1 {\ncase void_fn() {\nmes(1)\n}\n}\n", "", "5:13: void値を使用できません", 0, false},
	{"i = 0\nrepeat 3 as k {\nswitch k {\ncase 1 {\nbreak\n}\n}\nmes(k)\n}\n", "0\n1\n2\n", "", 0, false},
	{"i = 0\nrepeat 3 as k {\nswitch k {\ncase 1 {\ncontinue\n}\n}\nmes(k)\n}\n", "0\n2\n", "", 0, false},
	{"n = 0\ndef f_inc() {\nn = n + 1\nreturn n\n}\nswitch f_inc() {\ncase f_inc() {\nmes(\"a\")\n}\ncase 2 {\nmes(\"b\")\n}\n}\nmes(n)\n", "2\n", "", 0, false},
	{"def add(a, b) {\nreturn a + b\n}\nmes(add(2, 3))\n", "5\n", "", 0, false},
	{"mes(add(1, 2))\ndef add(a, b) {\nreturn a + b\n}\n", "", "1:8: 未定義の関数 \"add\" です", 0, false},
	{"def f(a) {\nreturn a\n}\nmes(f(1, 2))\n", "", "4:6: 関数 f は引数を 1 個必要としますが、2 個が渡されました", 0, false},
	{"def f(a, b) {\nreturn a\n}\nmes(f(1))\n", "", "4:6: 関数 f は引数を 2 個必要としますが、1 個が渡されました", 0, false},
	{"def f(a, a) {\nreturn a\n}\n", "", "1:10: 関数 \"f\" にパラメータ \"a\" が重複しています", 0, false},
	{"def mes(x) {\nreturn x\n}\n", "", "1:1: \"mes\" を再定義できません：組み込み関数です", 0, false},
	{"def f() {\nreturn 1\n}\ndef f() {\nreturn 2\n}\n", "", "4:1: 関数 \"f\" は既に定義されています", 0, false},
	{"g = 10\ndef f() {\ng = 20\n}\nf()\nmes(g)\n", "20\n", "", 0, false},
	{"g = 10\ndef f() {\nq = 30\n}\nf()\nmes(q)\n", "", "6:5: 未定義の変数 \"q\" です", 0, false},
	{"def f(x) {\nx = x + 1\nreturn x\n}\nmes(f(5))\n", "6\n", "", 0, false},
	{"def fact(n) {\nif n <= 1 {\nreturn 1\n}\nreturn n * fact(n - 1)\n}\nmes(fact(10))\n", "3628800\n", "", 0, false},
	{"def f() {\nreturn\n}\nmes(\"r\")\nf()\nmes(\"s\")\n", "r\ns\n", "", 0, false},
	{"def f() {\n}\nmes(\"void test\")\nx = f()\n", "void test\n", "4:1: void値を使用できません", 0, false},
	{"def void_fn() {\nreturn\n}\ndef f() {\nreturn void_fn()\n}\nmes(f())\n", "\n", "", 0, false},
	{"x = 5\nmes(x(1))\n", "", "2:6: \"x\" は int であり、関数ではありません", 0, false},
	{"a = [1]\nmes(a(0))\n", "", "2:6: 値は配列です。配列へのアクセスは [...] を使用してください（例：a[0]、a(0) ではありません）", 0, false},
	{"mes(nope(1))\n", "", "1:9: 未定義の関数 \"nope\" です", 0, false},
	{"mes(1 + 2 * unknown_fn(3))\n", "", "1:23: 未定義の関数 \"unknown_fn\" です", 0, false},
	{"f = 5\ndef g() {\nreturn f\n}\nmes(g())\n", "5\n", "", 0, false},
	{"x = 1\nmes(x)\n", "1\n", "", 0, false},
	{"x = 1\nx = 2\nmes(x)\n", "2\n", "", 0, false},
	{"x = 1\ndef f() {\nx = 2\nmes(x)\n}\nf()\nmes(x)\n", "2\n2\n", "", 0, false},
	{"x = 1\ndef f() {\nx = 2\n}\nf()\nmes(x)\n", "2\n", "", 0, false},
	{"def f() {\ny = 3\n}\nf()\nmes(y)\n", "", "5:5: 未定義の変数 \"y\" です", 0, false},
	{"if true {\nz = 9\n}\nmes(z)\n", "9\n", "", 0, false},
	{"repeat 3 as i {\nt = i * 2\nmes(t)\n}\n", "0\n2\n4\n", "", 0, false},
	{"mes = 1\n", "", "1:1: \"mes\" に代入できません：組み込み関数です", 0, false},
	{"a = [1, 2, 3]\nmes(a[1])\nmes(length(a))\n", "2\n3\n", "", 0, false},
	{"a = [1]\nmes(a[5])\n", "", "2:6: インデックス 5 は範囲外です（長さ 1）", 0, false},
	{"a = [1]\nmes(a[-1])\n", "", "2:7: 配列のインデックスは 0 以上である必要があります。-1 が指定されました", 0, false},
	{"a = [1]\nmes(a[0.0])\n", "", "2:7: 配列のインデックスは整数である必要があります。float が指定されました", 0, false},
	{"a = [1]\na[0] = 99\nmes(a[0])\n", "99\n", "", 0, false},
	{"a = [1]\na[3] = 99\n", "", "2:2: インデックス 3 は範囲外です（長さ 1）", 0, false},
	{"a = [[1, 2], [3, 4]]\nmes(a[1][0])\na[0][1] = 99\nmes(a[0][1])\n", "3\n99\n", "", 0, false},
	{"a = [[1, 2], [3, 4]]\na[0][5] = 1\n", "", "2:5: インデックス 5 は範囲外です（長さ 2）", 0, false},
	{"a = [[1, 2], 5]\na[0][1] = 9\n", "", "", 0, false},
	{"a = [1, 2]\ndef f_idx() {\nreturn 0\n}\na[f_idx()] = 9\nmes(a[0])\n", "9\n", "", 0, false},
	{"b = [0]\nc = 0\ndef f_idx() {\nc = c + 1\nreturn 1\n}\na = [7, 8]\na[f_idx()] += 10\nmes(a[1])\nmes(c)\n", "18\n1\n", "", 0, false},
	{"def void_fn() {\nreturn\n}\na = [void_fn()]\n", "", "4:13: void値を使用できません", 0, false},
	{"x = 0\nx += 5\nmes(x)\n", "5\n", "", 0, false},
	{"x = 3\nx *= 4\nmes(x)\n", "12\n", "", 0, false},
	{"y += 1\n", "", "1:1: 未定義の変数 \"y\" です", 0, false},
	{"mes = 1\n", "", "1:1: \"mes\" に代入できません：組み込み関数です", 0, false},
	{"a = [1,2,3]\ni = 1\na[i] += 10\nmes(a[1])\n", "12\n", "", 0, false},
	{"mes(zzz_undefined)\n", "", "1:5: 未定義の変数 \"zzz_undefined\" です", 0, false},
	{"break\n", "", "0:0: ループ外の break です", 0, false},
	{"continue\n", "", "0:0: ループ外の continue です", 0, false},
	{"return 1\n", "", "0:0: 関数外の return です", 0, false},
	{"return\n", "", "0:0: 関数外の return です", 0, false},
	{"while true {\nbreak\n}\nmes(\"ok\")\n", "ok\n", "", 0, false},
	{"repeat 3 as i {\nbreak\n}\nmes(\"ok\")\n", "ok\n", "", 0, false},
	{"def f() {\nwhile true {\nbreak\n}\nreturn 7\n}\nmes(f())\n", "7\n", "", 0, false},
	{"mes(length([1,2,3]))\nmes(len(\"abc\"))\n", "3\n", "2:8: 未定義の関数 \"len\" です", 0, false},
	{"mes()\n", "\n", "", 0, false},
	{"print(\"a\\nb\")\nmes(\"c\")\n", "a\nbc\n", "", 0, false},
	{"end(3)\nmes(\"no\")\n", "", "", 3, true},
	{"end()\n", "", "", 0, true},
	{"x = 1\nend(0)\n", "", "", 0, true},
	{"assert(true)\nmes(\"ok\")\n", "ok\n", "", 0, false},
	{"assert(false, \"boom\")\n", "", "1:7: boom", 0, false},
	{"mes(int(\"3.9\"))\n", "", "1:8: int：引数 1 \"3.9\" を整数に変換できません", 0, false},
	{"mes(float(\"nan\"))\n", "", "1:10: float：引数 1 \"nan\" を浮動小数に変換できません", 0, false},
	{"mes(1e308 * 10)\n", "", "1:11: 浮動小数の計算結果が有限ではありません（±Inf・NaN は使用できません）", 0, false},
	{"a = [3, 1, 2]\nsort(a)\nmes(a[0])\n", "1\n", "", 0, false},
	{"s = strmid(\"hello\", 1, 3)\nmes(s)\n", "ell\n", "", 0, false},
	{"mes(strmid(\"hi\", 0, 99))\n", "", "1:11: strmid：引数 3 start 0 から count 99 は長さ 2 を超えています", 0, false},
	{"mes(instr(\"hello\", \"l\", 99))\n", "", "1:10: instr：引数 2 整数である必要があります。string が指定されました", 0, false},
	{"def d2(a) {\nreturn a * 2\n}\nmes(d2(d2(21)))\n", "84\n", "", 0, false},
	{"i = 1\ns = \"a\" + i + [1][0]\nmes(s)\n", "a11\n", "", 0, false},
	{"a = [1,2,3,4]\ns = 0\nrepeat a as x {\ns += x\n}\nmes(s)\n", "10\n", "", 0, false},
	{"x = 0\nrepeat 100 as i {\nx = x + i\n}\nmes(x)\n", "4950\n", "", 0, false},
	{"i = 0\nrepeat 5 {\ni += 1\n}\nmes(i)\n", "5\n", "", 0, false},
	{"i = 0\nrepeat 100000 {\ni += 1\n}\nmes(i)\n", "100000\n", "", 0, false},
	{"i = 10\nrepeat 5 {\ni -= 3\n}\nmes(i)\n", "-5\n", "", 0, false},
	{"i = 0\nrepeat 0 {\ni += 1\n}\nmes(i)\n", "0\n", "", 0, false},
	{"i = 0\nrepeat 1 {\ni += 1\n}\nmes(i)\n", "1\n", "", 0, false},
	{"repeat 3 {\nzzz_repinc_undef += 1\n}\n", "", "2:1: 未定義の変数 \"zzz_repinc_undef\" です", 0, false},
	{"s = \"a\"\nrepeat 3 {\ns += 1\n}\nmes(s)\n", "a111\n", "", 0, false},
	{"x = 1.5\nrepeat 3 {\nx += 2\n}\nmes(x)\n", "7.5\n", "", 0, false},
	{"b = true\nrepeat 2 {\nb += 1\n}\n", "", "3:1: bool と int を加算できません", 0, false},
	{"i = 9223372036854775800\nrepeat 3 {\ni += 5\n}\nmes(i)\n", "-9223372036854775801\n", "", 0, false},
	{"i = 0\nrepeat 3 {\nrepeat 2 {\ni += 1\n}\n}\nmes(i)\n", "6\n", "", 0, false},
	{"def f_repinc() {\nt = 0\nrepeat 5 {\nt += 1\n}\nreturn t\n}\nmes(f_repinc())\n", "5\n", "", 0, false},
	{"i = 0\nx = 2\nrepeat 3 {\ni += x\n}\nmes(i)\n", "6\n", "", 0, false},
	{"repeat 2 as g_repinc {\nrepeat 3 {\ng_repinc += 1\n}\n}\n", "", "3:1: \"g_repinc\" に代入できません：repeat カウンタは読み取り専用です", 0, false},
	{"i = 0\nrepeat 3 as k {\ni += 1\n}\nmes(i)\nmes(k)\n", "3\n", "6:5: 未定義の変数 \"k\" です", 0, false},
}

// builtinsGshExpected builds the expected output for examples/builtins.gsh
// with the current year/month. gettime(0)/gettime(1) return the wall clock,
// so a hardcoded month goes stale every month (e.g. month=9 baked in Sept
// fails in Oct). Computing it from time.Now keeps the test green while
// still catching off-by-one month bugs (builtin uses the same clock).
func builtinsGshExpected() string {
	now := time.Now()
	return fmt.Sprintf("1d6 x5:\n  3\n  4\n  3\n  3\n  6\nlen=21\nmid=Hello\npos=9\ntrim=[Hello, HSP World!]\nsplit: [a, b, c] n=3\n0042\npi=3.14159\nabs=7 sqrt=1.4142135623730951\nclamp=10\npath base=app\npath ext=.gsh\nyear=%d month=%d\nlines=3 line2=two\none\ntwo\nthree\nfour\npeek=65 wpeek=17218\ntypes: int string array\nstack: 1-2-3 pop=3\nuser: Alice 98\ntotal=6\ndone\n", now.Year(), int(now.Month()))
}

var vmFiles = []vmFileCase{
	{"examples/hello.gsh", "Hello\nWorld\n16\n36\nHello, Alice\nx = 16 y = 20\n", "", 0, false},
	{"examples/builtins.gsh", "", "", 0, false},
	{"examples/edge.gsh", "2\n99\nZed\n77\n5\n7\n9\n10\ntrue\ntrue\n", "", 0, false},
	{"examples/sample19.gsh", "Hello, Alice\nHello, Bob\nodd\n", "", 0, false},
	{"examples/define.gsh", "0\n1\n2\n3\n10\n11\n15\nsouth\nIDENT\nx\n", "", 0, false},
	{"examples/include_demo.gsh", "=== include demo ===\n42\n", "", 0, false},
}

func runVMCase(t *testing.T, src string) (string, string, int, bool) {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		return "", "parse: " + err.Error(), 0, false
	}
	var buf bytes.Buffer
	in := NewInterpWithIO(&buf, strings.NewReader(""))
	vprog, verr := Compile(prog)
	if verr != nil {
		return buf.String(), "compile: " + verr.Error(), 0, false
	}
	var errStr string
	if rerr := newVmachine(in).runMain(vprog); rerr != nil {
		errStr = rerr.Error()
	}
	code, hasExit := in.ExitCode()
	return buf.String(), errStr, code, hasExit
}

func TestVMLanguage(t *testing.T) {
	for _, c := range vmCases {
		out, errStr, code, hasExit := runVMCase(t, c.src)
		if out != c.out || errStr != c.err || code != c.code || hasExit != c.hasExit {
			t.Fatalf("src %q:\n got  out=%q err=%q exit=%d(%v)\n want out=%q err=%q exit=%d(%v)",
				c.src, out, errStr, code, hasExit, c.out, c.err, c.code, c.hasExit)
		}
	}
}

func TestVMFiles(t *testing.T) {
	for _, c := range vmFiles {
		t.Run(c.file, func(t *testing.T) {
			prog, err := ParseFile(c.file)
			if err != nil {
				if "parse: "+err.Error() != c.err {
					t.Fatalf("file %s: got parse error %q, want %q", c.file, err.Error(), c.err)
				}
				return
			}
			var buf bytes.Buffer
			in := NewInterpWithIO(&buf, strings.NewReader(""))
			in.SetScriptDir(scriptDirOf(c.file))
			vprog, verr := Compile(prog)
			if verr != nil {
				if "compile: "+verr.Error() != c.err {
					t.Fatalf("file %s: got compile error %q, want %q", c.file, verr.Error(), c.err)
				}
				return
			}
			var errStr string
			if rerr := newVmachine(in).runMain(vprog); rerr != nil {
				errStr = rerr.Error()
			}
			code, hasExit := in.ExitCode()
			out := buf.String()
			wantOut := c.out
			if c.file == "examples/builtins.gsh" {
				wantOut = builtinsGshExpected()
			}
			if out != wantOut || errStr != c.err || code != c.code || hasExit != c.hasExit {
				t.Fatalf("file %s:\n got  out=%q err=%q exit=%d(%v)\n want out=%q err=%q exit=%d(%v)",
					c.file, out, errStr, code, hasExit, wantOut, c.err, c.code, c.hasExit)
			}
		})
	}
}
