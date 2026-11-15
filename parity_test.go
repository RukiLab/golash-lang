// vmCases は旧 tree/VM パリティコーパスに対する VM の振る舞いを固定する：
// 標準出力・エラー文言（位置付き）・終了コードは完全一致しなければならない。
// 期待値は合意済みの tree/VM の振る舞いから作成した
// （両バックエンドでスイートがグリーンだったもの）。
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
	{"x = 10 + 2 * 3\nmsg(x)\n", "16\n", "", 0, false},
	{"msg((10 + 2) * 3)\n", "36\n", "", 0, false},
	{"msg(10 % 3)\nmsg(7 / 2)\nmsg(7.0 / 2)\n", "1\n3\n3.5\n", "", 0, false},
	{"msg(-5 + 8)\nmsg(!true)\n", "3\n0\n", "", 0, false},
	{"msg(1 + 2.5)\nmsg(10 - 3.5)\nmsg(2 * 1.5)\n", "3.5\n6.5\n3\n", "", 0, false},
	{"msg(1 / 0)\n", "", "1:7: 0 による除算です", 0, false},
	{"msg(1.0 / 0.0)\n", "", "1:9: 0 による除算です", 0, false},
	{"msg(1 % 0)\n", "", "1:7: 0 による剰余演算です", 0, false},
	{"msg(1.5 % 2)\n", "", "1:9: 演算子 % は整数が必要です。float と int が指定されました", 0, false},
	{"msg(1 + true)\n", "2\n", "", 0, false},
	{"msg(true + false)\n", "1\n", "", 0, false},
	{"msg(1 - true)\nmsg(2 * \"a\")\n", "0\n", "2:7: 演算子 * は数値が必要です。int と string が指定されました", 0, false},
	{"msg(1 & 3)\nmsg(1 | 2)\nmsg(1 ^ 3)\nmsg(1 << 4)\nmsg(256 >> 3)\n", "1\n3\n2\n16\n32\n", "", 0, false},
	{"msg(1.5 & 1)\nmsg(1 << -1)\nmsg(1 << 64)\n", "", "1:9: 演算子 & は整数が必要です。float と int が指定されました", 0, false},
	{"msg(1 < 2)\nmsg(\"a\" < \"b\")\nmsg(1 == 1.0)\nmsg(1 != 2)\n", "1\n1\n1\n1\n", "", 0, false},
	{"msg(1 < \"a\")\n", "", "1:7: int と string を比較できません（< <= > >= は数値または文字列のみ対応しています）", 0, false},
	{"msg([1,2] == [1,2])\n", "0\n", "", 0, false},
	{"a = [1,2]\nb = a\nmsg(a == b)\n", "1\n", "", 0, false},
	{"msg(-\"a\")\nmsg(~1.5)\nmsg(~7)\n", "", "1:5: string を負数化できません", 0, false},
	{"def void_fn()\nreturn\nend\nmsg(1 == void_fn())\n", "", "4:17: void値を使用できません", 0, false},
	{"name = \"Alice\"\nmsg(\"Hello, \" + name)\n", "Hello, Alice\n", "", 0, false},
	{"msg(\"n=\" + 42)\nmsg(1 + \"a\" + 2)\n", "n=42\n1a2\n", "", 0, false},
	{"def void_fn()\nreturn\nend\nmsg(\"a\" + void_fn())\n", "", "4:9: void値を使用できません", 0, false},
	{"def void_fn()\nreturn\nend\nx = void_fn()\n", "", "4:1: void値を使用できません", 0, false},
	{"def void_fn()\nreturn\nend\nmsg(void_fn())\n", "\n", "", 0, false},
	{"if 1 < 2 && 2 < 3 || false\nmsg(\"y\")\nend\n", "y\n", "", 0, false},
	{"if 1\nmsg(1)\nend\n", "1\n", "", 0, false},
	{"x = 1 && true\nmsg(x)\n", "1\n", "", 0, false},
	{"msg(!1)\n", "0\n", "", 0, false},
	{"msg(true && false)\nmsg(false || 1 < 2)\n", "0\n1\n", "", 0, false},
	{"def void_fn()\nreturn\nend\nmsg(true || void_fn())\nmsg(false && void_fn())\n", "1\n0\n", "", 0, false},
	{"def void_fn()\nreturn\nend\nmsg(true && void_fn())\n", "", "4:20: 条件式は整数（0=false、非0=true）である必要があります。void が指定されました", 0, false},
	{"x = 3\nif x > 10\nmsg(1)\nelseif x > 5\nmsg(2)\nelse\nmsg(3)\nend\n", "3\n", "", 0, false},
	{"i = 0\nwhile i < 3\nmsg(i)\ni += 1\nend\n", "0\n1\n2\n", "", 0, false},
	{"while 1\nbreak\nend\nmsg(\"ok\")\n", "ok\n", "", 0, false},
	{"repeat 3 as i\nmsg(i)\nend\n", "0\n1\n2\n", "", 0, false},
	{"repeat 3\nmsg(\"a\")\nend\n", "a\na\na\n", "", 0, false},
	{"repeat 0 as i\nmsg(i)\nend\nmsg(\"done\")\n", "done\n", "", 0, false},
	{"repeat -1\nmsg(1)\nend\n", "", "1:8: repeat の回数は 0 以上である必要があります。-1 が指定されました", 0, false},
	{"repeat 1.5\nmsg(1)\nend\n", "", "1:8: repeat は整数・配列・文字列が必要です。float が指定されました", 0, false},
	{"repeat 3 as i, x\nmsg(i)\nend\n", "", "1:1: 整数の repeat では変数は 1 つまでです（as i, item は配列・文字列用です）", 0, false},
	{"a = [10, 20, 30]\nrepeat a as x\nmsg(x)\nend\n", "10\n20\n30\n", "", 0, false},
	{"a = [10, 20, 30]\nrepeat a as i, x\nmsg(i)\nmsg(x)\nend\n", "0\n10\n1\n20\n2\n30\n", "", 0, false},
	{"a = [1,2]\nrepeat a as x\npush(a, 99)\nmsg(x)\nend\nmsg(len(a))\n", "1\n2\n4\n", "", 0, false},
	{"s = \"aあb\"\nrepeat s as i, ch\nmsg(str(i) + \":\" + ch)\nend\n", "0:a\n1:あ\n2:b\n", "", 0, false},
	{"s = \"あい\"\nrepeat s as ch\nmsg(ch)\nend\n", "あ\nい\n", "", 0, false},
	{"repeat 3 as i\ni = 99\nend\n", "", "2:1: \"i\" に代入できません：repeat カウンタは読み取り専用です", 0, false},
	{"x = 99\nrepeat 3 as x\nmsg(x)\nend\nmsg(x)\n", "0\n1\n2\n99\n", "", 0, false},
	{"repeat 2 as i\nrepeat 2 as i\nmsg(i)\nend\nend\n", "0\n1\n0\n1\n", "", 0, false},
	{"repeat 5 as i\nif i == 2\nbreak\nend\nmsg(i)\nend\n", "0\n1\n", "", 0, false},
	{"repeat 5 as i\nif i == 2\ncontinue\nend\nmsg(i)\nend\n", "0\n1\n3\n4\n", "", 0, false},
	{"x = 2\nswitch x\ncase 1\nmsg(1)\ncase 2, 3\nmsg(23)\ndefault\nmsg(\"d\")\nend\n", "23\n", "", 0, false},
	{"x = 9\nswitch x\ncase 1\nmsg(1)\ndefault\nmsg(\"d\")\nend\n", "d\n", "", 0, false},
	{"x = 1\nswitch x\ncase 1\nmsg(1)\nend\nmsg(\"after\")\n", "1\nafter\n", "", 0, false},
	{"def void_fn()\nreturn\nend\nswitch void_fn()\ncase 1\nmsg(1)\nend\n", "", "4:15: void値を使用できません", 0, false},
	{"def void_fn()\nreturn\nend\nswitch 1\ncase void_fn()\nmsg(1)\nend\n", "", "5:13: void値を使用できません", 0, false},
	{"i = 0\nrepeat 3 as k\nswitch k\ncase 1\nbreak\nend\nmsg(k)\nend\n", "0\n1\n2\n", "", 0, false},
	{"i = 0\nrepeat 3 as k\nswitch k\ncase 1\ncontinue\nend\nmsg(k)\nend\n", "0\n2\n", "", 0, false},
	{"n = 0\ndef f_inc()\nn = n + 1\nreturn n\nend\nswitch f_inc()\ncase f_inc()\nmsg(\"a\")\ncase 2\nmsg(\"b\")\nend\nmsg(n)\n", "2\n", "", 0, false},
	{"def add(a, b)\nreturn a + b\nend\nmsg(add(2, 3))\n", "5\n", "", 0, false},
	{"msg(add(1, 2))\ndef add(a, b)\nreturn a + b\nend\n", "", "1:8: 未定義の関数 \"add\" です", 0, false},
	{"def f(a)\nreturn a\nend\nmsg(f(1, 2))\n", "", "4:6: 関数 f は引数を 1 個必要としますが、2 個が渡されました", 0, false},
	{"def f(a, b)\nreturn a\nend\nmsg(f(1))\n", "", "4:6: 関数 f は引数を 2 個必要としますが、1 個が渡されました", 0, false},
	{"def f(a, a)\nreturn a\nend\n", "", "1:10: 関数 \"f\" にパラメータ \"a\" が重複しています", 0, false},
	{"def msg(x)\nreturn x\nend\n", "", "1:1: \"msg\" を再定義できません：組み込み関数です", 0, false},
	{"def f()\nreturn 1\nend\ndef f()\nreturn 2\nend\n", "", "4:1: 関数 \"f\" は既に定義されています", 0, false},
	{"g = 10\ndef f()\ng = 20\nend\nf()\nmsg(g)\n", "20\n", "", 0, false},
	{"g = 10\ndef f()\nq = 30\nend\nf()\nmsg(q)\n", "", "3:1: 未定義の変数 \"q\" です", 0, false},
	{"x = 1\ndef f()\nlocal x = 2\nreturn x\nend\nmsg(f())\nmsg(x)\n", "2\n1\n", "", 0, false},
	{"def f()\nlocal y = 3\nreturn y\nend\nmsg(f())\n", "3\n", "", 0, false},
	{"def f(x)\nx = x + 1\nreturn x\nend\nmsg(f(5))\n", "6\n", "", 0, false},
	{"def fact(n)\nif n <= 1\nreturn 1\nend\nreturn n * fact(n - 1)\nend\nmsg(fact(10))\n", "3628800\n", "", 0, false},
	{"def f()\nreturn\nend\nmsg(\"r\")\nf()\nmsg(\"s\")\n", "r\ns\n", "", 0, false},
	{"def f()\nend\nmsg(\"void test\")\nx = f()\n", "void test\n", "4:1: void値を使用できません", 0, false},
	{"def void_fn()\nreturn\nend\ndef f()\nreturn void_fn()\nend\nmsg(f())\n", "\n", "", 0, false},
	{"x = 5\nmsg(x(1))\n", "", "2:6: \"x\" は int であり、関数ではありません", 0, false},
	{"a = [1]\nmsg(a(0))\n", "", "2:6: 値は配列です。配列へのアクセスは [...] を使用してください（例：a[0]、a(0) ではありません）", 0, false},
	{"msg(nope(1))\n", "", "1:9: 未定義の関数 \"nope\" です", 0, false},
	{"msg(1 + 2 * unknown_fn(3))\n", "", "1:23: 未定義の関数 \"unknown_fn\" です", 0, false},
	{"f = 5\ndef g()\nreturn f\nend\nmsg(g())\n", "5\n", "", 0, false},
	{"x = 1\nmsg(x)\n", "1\n", "", 0, false},
	{"x = 1\nx = 2\nmsg(x)\n", "2\n", "", 0, false},
	{"x = 1\ndef f()\nx = 2\nmsg(x)\nend\nf()\nmsg(x)\n", "2\n2\n", "", 0, false},
	{"x = 1\ndef f()\nx = 2\nend\nf()\nmsg(x)\n", "2\n", "", 0, false},
	{"def f()\ny = 3\nend\nf()\nmsg(y)\n", "", "2:1: 未定義の変数 \"y\" です", 0, false},
	{"if true\nz = 9\nend\nmsg(z)\n", "9\n", "", 0, false},
	{"repeat 3 as i\nt = i * 2\nmsg(t)\nend\n", "0\n2\n4\n", "", 0, false},
	{"msg = 1\n", "", "1:1: \"msg\" に代入できません：組み込み関数です", 0, false},
	{"a = [1, 2, 3]\nmsg(a[1])\nmsg(len(a))\n", "2\n3\n", "", 0, false},
	{"a = [1]\nmsg(a[5])\n", "", "2:6: インデックス 5 は範囲外です（長さ 1）", 0, false},
	{"a = [1]\nmsg(a[-1])\n", "", "2:7: インデックスは 0 以上である必要があります。-1 が指定されました", 0, false},
	{"a = [1]\nmsg(a[0.0])\n", "", "2:7: インデックスは整数である必要があります。float が指定されました", 0, false},
	{"s = \"abc\"\nmsg(s[0])\nmsg(s[2])\n", "a\nc\n", "", 0, false},
	{"s = \"あいう\"\nmsg(s[1])\n", "い\n", "", 0, false},
	{"s = \"abc\"\nmsg(s[3])\n", "", "2:6: インデックス 3 は範囲外です（長さ 3）", 0, false},
	{"s = \"abc\"\ns[0] = \"x\"\n", "", "2:2: 文字列はインデックスで変更できません（文字列は不変です）", 0, false},
	{"a = [1]\na[0] = 99\nmsg(a[0])\n", "99\n", "", 0, false},
	{"a = [1]\na[3] = 99\n", "", "2:2: インデックス 3 は範囲外です（長さ 1）", 0, false},
	{"a = [[1, 2], [3, 4]]\nmsg(a[1][0])\na[0][1] = 99\nmsg(a[0][1])\n", "3\n99\n", "", 0, false},
	{"a = [[1, 2], [3, 4]]\na[0][5] = 1\n", "", "2:5: インデックス 5 は範囲外です（長さ 2）", 0, false},
	{"a = [[1, 2], 5]\na[0][1] = 9\n", "", "", 0, false},
	{"a = [1, 2]\ndef f_idx()\nreturn 0\nend\na[f_idx()] = 9\nmsg(a[0])\n", "9\n", "", 0, false},
	{"b = [0]\nc = 0\ndef f_idx()\nc = c + 1\nreturn 1\nend\na = [7, 8]\na[f_idx()] += 10\nmsg(a[1])\nmsg(c)\n", "18\n1\n", "", 0, false},
	{"def void_fn()\nreturn\nend\na = [void_fn()]\n", "", "4:13: void値を使用できません", 0, false},
	{"x = 0\nx += 5\nmsg(x)\n", "5\n", "", 0, false},
	{"x = 3\nx *= 4\nmsg(x)\n", "12\n", "", 0, false},
	{"y += 1\n", "", "1:1: 未定義の変数 \"y\" です", 0, false},
	{"msg = 1\n", "", "1:1: \"msg\" に代入できません：組み込み関数です", 0, false},
	{"a = [1,2,3]\ni = 1\na[i] += 10\nmsg(a[1])\n", "12\n", "", 0, false},
	{"msg(zzz_undefined)\n", "", "1:5: 未定義の変数 \"zzz_undefined\" です", 0, false},
	{"break\n", "", "0:0: ループ外の break です", 0, false},
	{"continue\n", "", "0:0: ループ外の continue です", 0, false},
	{"return 1\n", "", "0:0: 関数外の return です", 0, false},
	{"return\n", "", "0:0: 関数外の return です", 0, false},
	{"while true\nbreak\nend\nmsg(\"ok\")\n", "ok\n", "", 0, false},
	{"repeat 3 as i\nbreak\nend\nmsg(\"ok\")\n", "ok\n", "", 0, false},
	{"def f()\nwhile true\nbreak\nend\nreturn 7\nend\nmsg(f())\n", "7\n", "", 0, false},
	{"msg(len([1,2,3]))\nmsg(len(\"abc\"))\n", "3\n3\n", "", 0, false},
	{"msg()\n", "", "1:4: msg は引数を 1 個必要としますが、0 個が渡されました", 0, false},
	{"print(\"a\\nb\")\nmsg(\"c\")\n", "a\nbc\n", "", 0, false},
	{"exit(3)\nmsg(\"no\")\n", "", "", 3, true},
	{"exit()\n", "", "", 0, true},
	{"x = 1\nexit(0)\n", "", "", 0, true},
	{"assert(true)\nmsg(\"ok\")\n", "ok\n", "", 0, false},
	{"assert(false, \"boom\")\n", "", "1:7: boom", 0, false},
	{"msg(int(\"3.9\"))\n", "", "1:8: int：引数 1 \"3.9\" を整数に変換できません", 0, false},
	{"msg(float(\"nan\"))\n", "", "1:10: float：引数 1 \"nan\" を浮動小数に変換できません", 0, false},
	{"msg(1e308 * 10)\n", "", "1:11: 浮動小数の計算結果が有限ではありません（±Inf・NaN は使用できません）", 0, false},
	{"a = [3, 1, 2]\nsort(a)\nmsg(a[0])\n", "1\n", "", 0, false},
	{"s = slice(\"hello\", 1, 4)\nmsg(s)\n", "ell\n", "", 0, false},
	{"msg(slice(\"hi\", 0, 99))\n", "", "1:10: slice：引数 2 範囲 0 から 99 は範囲外です（長さ 2）", 0, false},
	{"msg(find(\"hello\", \"l\", 99))\n", "", "1:9: find：引数 3 start 99 は範囲外です（長さ 5）", 0, false},
	{"def d2(a)\nreturn a * 2\nend\nmsg(d2(d2(21)))\n", "84\n", "", 0, false},
	{"i = 1\ns = \"a\" + i + [1][0]\nmsg(s)\n", "a11\n", "", 0, false},
	{"a = [1,2,3,4]\ns = 0\nrepeat a as x\ns += x\nend\nmsg(s)\n", "10\n", "", 0, false},
	{"x = 0\nrepeat 100 as i\nx = x + i\nend\nmsg(x)\n", "4950\n", "", 0, false},
	{"i = 0\nrepeat 5\ni += 1\nend\nmsg(i)\n", "5\n", "", 0, false},
	{"i = 0\nrepeat 100000\ni += 1\nend\nmsg(i)\n", "100000\n", "", 0, false},
	{"i = 10\nrepeat 5\ni -= 3\nend\nmsg(i)\n", "-5\n", "", 0, false},
	{"i = 0\nrepeat 0\ni += 1\nend\nmsg(i)\n", "0\n", "", 0, false},
	{"i = 0\nrepeat 1\ni += 1\nend\nmsg(i)\n", "1\n", "", 0, false},
	{"repeat 3\nzzz_repinc_undef += 1\nend\n", "", "2:1: 未定義の変数 \"zzz_repinc_undef\" です", 0, false},
	{"s = \"a\"\nrepeat 3\ns += 1\nend\nmsg(s)\n", "a111\n", "", 0, false},
	{"x = 1.5\nrepeat 3\nx += 2\nend\nmsg(x)\n", "7.5\n", "", 0, false},
	{"b = true\nrepeat 2\nb += 1\nend\nmsg(b)\n", "3\n", "", 0, false},
	{"i = 9223372036854775800\nrepeat 3\ni += 5\nend\nmsg(i)\n", "-9223372036854775801\n", "", 0, false},
	{"i = 0\nrepeat 3\nrepeat 2\ni += 1\nend\nend\nmsg(i)\n", "6\n", "", 0, false},
	{"def f_repinc()\nlocal t = 0\nrepeat 5\nt += 1\nend\nreturn t\nend\nmsg(f_repinc())\n", "5\n", "", 0, false},
	{"i = 0\nx = 2\nrepeat 3\ni += x\nend\nmsg(i)\n", "6\n", "", 0, false},
	{"repeat 2 as g_repinc\nrepeat 3\ng_repinc += 1\nend\nend\n", "", "3:1: \"g_repinc\" に代入できません：repeat カウンタは読み取り専用です", 0, false},
	{"i = 0\nrepeat 3 as k\ni += 1\nend\nmsg(i)\nmsg(k)\n", "3\n", "6:5: 未定義の変数 \"k\" です", 0, false},
}

// builtinsGshExpected は testdata/parity/builtins.gsh 用の期待出力を
// 現在の年/月で組み立てる。gettime(0)/gettime(1) は壁時計を返すため、
// 月を直書きすると毎月古くなる（例：9月に埋め込んだ month=9 は
// 10月に失敗する）。time.Now から計算すればテストは通り続けつつ、
// 月の off-by-one バグも検出できる（builtin も同じ時計を使う）。
func builtinsGshExpected() string {
	now := time.Now()
	return fmt.Sprintf("1d6 x5:\n  3\n  4\n  3\n  3\n  6\nlen=21\nmid=Hello\npos=9\ntrim=[Hello, HSP World!]\nsplit: [a, b, c] n=3\n0042\npi=3.14159\nabs=7 sqrt=1.4142135623730951\nclamp=10\npath base=app\npath ext=.gsh\nyear=%d month=%d\nb0=65 b12=17218\ntypes: int string array\nstack: 1-2-3 pop=3\nuser: Alice 98\ntotal=6\ndone\n", now.Year(), int(now.Month()))
}

var vmFiles = []vmFileCase{
	{"testdata/parity/hello.gsh", "Hello\nWorld\n16\n36\nHello, Alice\nx = 16 y = 20\n", "", 0, false},
	{"testdata/parity/builtins.gsh", "", "", 0, false},
	{"testdata/parity/edge.gsh", "2\n99\nZed\n77\n5\n7\n9\n10\n1\n1\n", "", 0, false},
	{"testdata/parity/sample19.gsh", "Hello, Alice\nHello, Bob\nodd\n", "", 0, false},
	{"testdata/parity/define.gsh", "0\n1\n2\n3\n10\n11\n15\nsouth\n", "", 0, false},
	{"testdata/parity/include_demo.gsh", "=== include demo ===\n42\n", "", 0, false},
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
			if c.file == "testdata/parity/builtins.gsh" {
				wantOut = builtinsGshExpected()
			}
			if out != wantOut || errStr != c.err || code != c.code || hasExit != c.hasExit {
				t.Fatalf("file %s:\n got  out=%q err=%q exit=%d(%v)\n want out=%q err=%q exit=%d(%v)",
					c.file, out, errStr, code, hasExit, wantOut, c.err, c.code, c.hasExit)
			}
		})
	}
}
