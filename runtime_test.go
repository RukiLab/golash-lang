package main

import (
	"bytes"
	"strings"
	"testing"
)

// runSrc は src を VM で実行し、mes() の出力を返す。
// 解析時のエラーと実行時のエラーの両方を error（fatal ではない）として
// 返すため、拒否のテストはどちらの局面も扱える。
func runSrc(t *testing.T, src string) (string, error) {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		return "", err
	}
	vprog, err := Compile(prog)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	in := NewInterp(&buf)
	err = newVmachine(in).runMain(vprog)
	return buf.String(), err
}

func mustOut(t *testing.T, src, want string) {
	t.Helper()
	got, err := runSrc(t, src)
	if err != nil {
		t.Fatalf("src %q: runtime error: %v", src, err)
	}
	if got != want {
		t.Fatalf("src %q:\n got %q\nwant %q", src, got, want)
	}
}

func mustErr(t *testing.T, src, substr string) {
	t.Helper()
	_, err := runSrc(t, src)
	if err == nil {
		t.Fatalf("src %q: expected error containing %q, got nil", src, substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("src %q: error %q does not contain %q", src, err.Error(), substr)
	}
}

func TestArithmetic(t *testing.T) {
	mustOut(t, "x = 10 + 2 * 3\nmes(x)\n", "16\n")
	mustOut(t, "mes((10 + 2) * 3)\n", "36\n")
	mustOut(t, "mes(10 % 3)\n", "1\n")
	mustOut(t, "mes(7 / 2)\nmes(7.0 / 2)\n", "3\n3.5\n")
	mustOut(t, "mes(-5 + 8)\nmes(!true)\n", "3\nfalse\n")
	mustOut(t, "mes(1 + 2.5)\n", "3.5\n")
	mustErr(t, "mes(1 / 0)\n", "0 による除算")
	mustErr(t, "mes(1 % 0)\n", "0 による剰余")
	mustErr(t, "mes(1.5 % 2)\n", "整数が必要です")
	mustErr(t, "mes(1 + true)\n", "加算できません")
}

func TestStrings(t *testing.T) {
	mustOut(t, "name = \"Alice\"\nmes(\"Hello, \" + name)\n", "Hello, Alice\n")
	mustOut(t, "mes(\"n=\" + 42)\n", "n=42\n")
	mustOut(t, "mes(\"a\" < \"b\")\n", "true\n")
}

func TestStrictConditions(t *testing.T) {
	mustOut(t, "if 1 < 2 && 2 < 3 || false {\nmes(\"y\")\n}\n", "y\n")
	mustErr(t, "if 1 {\nmes(1)\n}\n", "bool 型")
	mustErr(t, "if \"s\" {\nmes(1)\n}\n", "bool 型")
	mustErr(t, "x = 1 && true\n", "bool 型")
	mustErr(t, "mes(!1)\n", "bool 型")
}

func TestIfElse(t *testing.T) {
	src := "x = 7\nif x > 10 {\nmes(\"large\")\n} else if x > 5 {\nmes(\"middle\")\n} else {\nmes(\"small\")\n}\n"
	mustOut(t, src, "middle\n")
	mustOut(t, "x = 3\nif x > 10 {\nmes(1)\n} else {\nmes(2)\n}\n", "2\n")
}

func TestRepeatCounter(t *testing.T) {
	mustOut(t, "repeat 3 as i {\nmes(i)\n}\n", "0\n1\n2\n")
	mustOut(t, "s = 0\nrepeat 10 as i {\ns = s + i\n}\nmes(s)\n", "45\n")
	mustErr(t, "repeat 2 as i {\nmes(i)\n}\nmes(i)\n", "未定義の変数")
	mustErr(t, "repeat 2 as i {\ni = 100\n}\n", "読み取り専用")
	mustErr(t, "repeat 2 as i {\ndef i() {\n}\n}\n", "トップレベル")
	mustErr(t, "repeat true {\nmes(1)\n}\n", "整数または配列")
	mustErr(t, "repeat -1 {\nmes(1)\n}\n", "0 以上")
}

func TestBreakContinue(t *testing.T) {
	mustOut(t, "s = -1\nrepeat 100 as i {\nif i == 10 {\nbreak\n}\ns = i\n}\nmes(s)\n", "9\n")
	mustOut(t, "repeat 10 as i {\nif i % 2 == 0 {\ncontinue\n}\nmes(i)\n}\n", "1\n3\n5\n7\n9\n")
	mustErr(t, "break\n", "ループ外の break")
	mustErr(t, "continue\n", "ループ外の continue")
	mustErr(t, "repeat 2 as i {\nmes(i)\n}\nmes(i)\n", "未定義の変数")
}

func TestWhile(t *testing.T) {
	mustOut(t, "i = 0\nwhile i < 3 {\nmes(i)\ni = i + 1\n}\n", "0\n1\n2\n")
	mustOut(t, "while false {\nmes(1)\n}\nmes(0)\n", "0\n")
	mustOut(t, "i = 0\ns = 0\nwhile true {\nif i >= 5 {\nbreak\n}\ns = s + i\ni = i + 1\n}\nmes(s)\n", "10\n")
	mustOut(t, "i = 0\nwhile i < 10 {\ni = i + 1\nif i % 2 == 0 {\ncontinue\n}\nmes(i)\n}\n", "1\n3\n5\n7\n9\n")
	mustOut(t, "i = 0\nwhile i < 3 {\nj = 0\nwhile j < 2 {\nmes(i * 10 + j)\nj = j + 1\n}\ni = i + 1\n}\n", "0\n1\n10\n11\n20\n21\n")
	mustOut(t, "i = 0\nwhile i < 5 {\ni = i + 1\nif i == 2 {\nbreak\n}\n}\nmes(i)\n", "2\n")
	mustErr(t, "while 1 {\nmes(1)\n}\n", "条件式は bool 型")
	mustErr(t, "while \"x\" {\n}\n", "条件式は bool 型")
}

func TestSwitch(t *testing.T) {
	mustOut(t, "x = 2\nswitch x {\ncase 1 {\nmes(\"one\")\n}\ncase 2 {\nmes(\"two\")\n}\ndefault {\nmes(\"other\")\n}\n}\n", "two\n")
	mustOut(t, "x = 2\nswitch x {\ncase 1, 2, 3 {\nmes(\"few\")\n}\ndefault {\nmes(\"many\")\n}\n}\n", "few\n")
	mustOut(t, "x = 9\nswitch x {\ncase 1 {\nmes(\"one\")\n}\n}\nmes(\"done\")\n", "done\n")
	mustOut(t, "x = 9\nswitch x {\ncase 1 {\nmes(\"one\")\n}\ndefault {\nmes(\"other\")\n}\n}\n", "other\n")
	// 最初に一致したものが勝つ。フォールスルーはしない。
	mustOut(t, "x = 1\nswitch x {\ncase 1 {\nmes(\"a\")\n}\ncase 1 {\nmes(\"b\")\n}\n}\n", "a\n")
	mustOut(t, "x = 1\nswitch x {\ncase 1 {\nmes(\"a\")\nbreak\nmes(\"unreached\")\n}\ndefault {\nmes(\"d\")\n}\n}\nmes(\"after\")\n", "a\nafter\n")
	// ループ内の switch の break はループではなく switch を終わる。
	mustOut(t, "i = 0\nrepeat 3 {\nswitch i {\ncase 1 {\nbreak\n}\ndefault {\nmes(i)\n}\n}\ni = i + 1\n}\n", "0\n2\n")
	// continue は switch を通り抜けてループに移る。
	mustOut(t, "repeat 3 as i {\nswitch i {\ncase 1 {\ncontinue\n}\ndefault {\nmes(i)\n}\n}\n}\n", "0\n2\n")
	// case 内の repeat 内の break は repeat を終わる。
	mustOut(t, "x = 1\nswitch x {\ncase 1 {\ns = -1\nrepeat 10 as i {\nif i == 2 {\nbreak\n}\ns = i\n}\nmes(s)\n}\n}\n", "1\n")
	// スカーチニーは一度評価される。
	mustOut(t, "n = 0\ndef next() {\nn = n + 1\nreturn n\n}\nswitch next() {\ncase 1 {\nmes(\"one\")\n}\ndefault {\nmes(\"other\")\n}\n}\nmes(n)\n", "one\n1\n")
	// 型の強制と不一致。
	mustOut(t, "x = 1.0\nswitch x {\ncase 1 {\nmes(\"int\")\n}\ndefault {\nmes(\"other\")\n}\n}\n", "int\n")
	mustOut(t, "x = 1\nswitch x {\ncase \"1\" {\nmes(\"str\")\n}\ndefault {\nmes(\"other\")\n}\n}\n", "other\n")
	mustOut(t, "s = \"b\"\nswitch s {\ncase \"a\" {\nmes(1)\n}\ncase \"b\" {\nmes(2)\n}\n}\n", "2\n")
	// 入れ子の switch。
	mustOut(t, "x = 1\ny = 2\nswitch x {\ncase 1 {\nswitch y {\ncase 2 {\nmes(\"1-2\")\n}\ndefault {\nmes(\"1-?\")\n}\n}\n}\ndefault {\nmes(\"outer\")\n}\n}\n", "1-2\n")
	mustErr(t, "switch 1 {\ndefault {\n}\ndefault {\n}\n}\n", "default が重複")
	mustErr(t, "switch 1 {\nmes(1)\n}\n", "case または default が必要")
}

func TestBitwise(t *testing.T) {
	mustOut(t, "mes(6 & 3)\nmes(6 | 3)\nmes(6 ^ 3)\n", "2\n7\n5\n")
	mustOut(t, "mes(~0)\nmes(~5)\nmes(1 << 4)\nmes(256 >> 3)\nmes(-8 >> 2)\n", "-1\n-6\n16\n32\n-2\n")
	mustOut(t, "mes(1 | 2 & 3)\nmes(1 << 2 + 1)\nmes(8 >> 1 + 1)\n", "3\n8\n2\n")
	mustOut(t, "mes(255 & 15)\nmes(1 << 0)\nmes(0 >> 5)\n", "15\n1\n0\n")
	// 近代化された優先順位: & | ^ は == より強く結合する（C とは違う）。
	mustOut(t, "mes(1 & 1 == 1)\nmes(2 | 1 == 3)\nmes(0 & 0 == 0)\n", "true\ntrue\ntrue\n")
	mustErr(t, "mes(1.5 & 1)\n", "整数が必要です")
	mustErr(t, "mes(\"a\" | 1)\n", "整数が必要です")
	mustErr(t, "mes(1 ^ 2.0)\n", "整数が必要です")
	mustErr(t, "mes(1 << 2.5)\n", "整数が必要です")
	mustErr(t, "mes(1 << -1)\n", "シフト数は 0 から 63")
	mustErr(t, "mes(1 << 64)\n", "シフト数は 0 から 63")
	mustErr(t, "mes(1 >> -2)\n", "シフト数は 0 から 63")
	mustErr(t, "mes(~1.5)\n", "整数のみ")
	mustErr(t, "mes(~\"a\")\n", "整数のみ")
}

func TestCompoundAssign(t *testing.T) {
	mustOut(t, "x = 10\nx += 5\nmes(x)\nx -= 3\nmes(x)\nx *= 2\nmes(x)\nx /= 4\nmes(x)\nx %= 5\nmes(x)\n", "15\n12\n24\n6\n1\n")
	mustOut(t, "x = 6\nx &= 3\nmes(x)\nx |= 8\nmes(x)\nx ^= 7\nmes(x)\nx <<= 2\nmes(x)\nx >>= 1\nmes(x)\n", "2\n10\n13\n52\n26\n")
	mustOut(t, "s = \"a\"\ns += \"b\"\nmes(s)\nf = 1.5\nf += 1\nmes(f)\n", "ab\n2.5\n")
	mustOut(t, "a = [1, 2]\na[0] += 10\na[1] *= 3\nmes(a)\n", "[11, 6]\n")
	mustOut(t, "p = [[1]]\np[0][0] += 41\nmes(p[0][0])\n", "42\n")
	mustErr(t, "x = 1\nx += true\n", "加算できません")
	mustErr(t, "x = 1.5\nx &= 1\n", "整数が必要です")
	mustErr(t, "x = 1\nx /= 0\n", "0 による除算")
	// 単一評価: 添字式はちょうど一度実行される。
	mustOut(t, "i = 0\ndef next() {\ni = i + 1\nreturn 0\n}\na = [5]\na[next()] += 10\nmes(i)\nmes(a[0])\n", "1\n15\n")
	mustOut(t, "i = 0\ndef next() {\ni = i + 1\nreturn 0\n}\nm = [[7]]\nm[next()][0] *= 2\nmes(i)\nmes(m[0][0])\n", "1\n14\n")
	mustErr(t, "x += 1\n", "未定義の変数")
	mustErr(t, "mes += 1\n", "未定義の変数")
}

func TestMinInt64Literal(t *testing.T) {
	// -9223372036854775808 は MinInt64 に畳まれる（正の半分は int64 を
	// 桁あふれするため、裸のリテラルは存在しない）。
	mustOut(t, "mes(-9223372036854775808)\n", "-9223372036854775808\n")
	mustOut(t, "mes(-0x8000000000000000)\n", "-9223372036854775808\n")
	mustOut(t, "m = -9223372036854775808\nmes(m == -9223372036854775808)\nmes(m < 0)\n", "true\ntrue\n")
	mustErr(t, "mes(9223372036854775808)\n", "不正な整数リテラル")
	mustErr(t, "mes(0x8000000000000000)\n", "不正な整数リテラル")
	// 整数演算はラップする（Go ライク）。abs() だけが MinInt64 を拒否する。
	mustOut(t, "mes(9223372036854775807 + 1)\n", "-9223372036854775808\n")
	mustOut(t, "mes(-(-9223372036854775808))\n", "-9223372036854775808\n")
}

func TestFiniteFloats(t *testing.T) {
	// 桁あふれする浮動小数点数演算はエラーで、±Inf には決してならない。
	mustErr(t, "mes(1e308 * 10)\n", "有限ではありません")
	mustErr(t, "mes(1e308 + 1e308)\n", "有限ではありません")
	mustErr(t, "mes(1e308 / 1e-308)\n", "有限ではありません")
	// 通常の浮動小数点数は影響を受けない。
	mustOut(t, "mes(0.1 + 0.2 == 0.30000000000000004)\n", "true\n")
}

func TestHardeningTraps(t *testing.T) {
	// 文字列連結は void を "null" として蘇らせない。
	mustErr(t, "def f() {\n}\nmes(\"a\" + f())\n", "void値を使用できません")
	// 組込み名は代入できない（組込みをシャドーイングしてしまうため）。
	mustErr(t, "mes = 1\n", "組み込み関数")
	mustErr(t, "length = 1\n", "組み込み関数")
	// ユーザー定義関数は再定義できない。
	mustErr(t, "def f() {\n}\ndef f() {\n}\n", "既に定義されています")
	// switch case の値はスカーチニーと同じく void を拒否する。
	mustErr(t, "def f() {\n}\nswitch 1 {\ncase f() {\nmes(\"x\")\n}\n}\n", "void値を使用できません")
}

func TestRecursionLimit(t *testing.T) {
	mustErr(t, "def boom() {\nreturn boom()\n}\nmes(boom())\n", "深すぎます")
	// 健全性: 通常の再帰は依然動く。
	mustOut(t, "def fact(n) {\nif n <= 1 {\nreturn 1\n}\nreturn n * fact(n - 1)\n}\nmes(fact(10))\n", "3628800\n")
}

func TestParseDepthLimit(t *testing.T) {
	// 50段の入れ子はコンパイルされ実行される。1100段はパーサに
	// 拒否される（ホストのスタック防護。VM のレジスタ上限も制限する）。
	shallow := strings.Repeat("if true {\n", 50) + "mes(1)\n" + strings.Repeat("}\n", 50)
	mustOut(t, shallow, "1\n")
	deep := strings.Repeat("if true {\n", 1100) + "mes(1)\n" + strings.Repeat("}\n", 1100)
	mustErr(t, deep, "ネストが深すぎます")
	shallowExpr := strings.Repeat("(", 500) + "1" + strings.Repeat(")", 500) + "\n"
	mustOut(t, "mes"+shallowExpr, "1\n")
	deepExpr := strings.Repeat("(", 1100) + "1" + strings.Repeat(")", 1100) + "\n"
	mustErr(t, "mes"+deepExpr, "ネストが深すぎます")
}

func TestTryCatchAbolished(t *testing.T) {
	// try/catch は今や通常の識別子である。try ブロックは無くなった。
	mustOut(t, "try = 5\nmes(try)\n", "5\n")
	mustErr(t, "try {\nmes(1)\n}\n", "予期しない '{'")
	// throw() も廃止された: 通常の未定義呼出である。
	mustErr(t, "throw(\"late\")\n", "未定義の関数")
}

func TestDeclareByAssign(t *testing.T) {
	// `let` 廃止後：通常代入が見える定義の更新、なければ宣言になる。
	mustOut(t, "x = 1\nmes(x)\n", "1\n")
	// 同じスコープの上書きは許可される（ループが動き続ける）。
	mustOut(t, "x = 1\nx = 2\nmes(x)\n", "2\n")
	// シャドーイング宣言は無くなった: 関数内の代入は可視の（ここでは
	// グローバルの）定義を更新する。
	mustOut(t, "x = 1\ndef f() {\nx = 2\nmes(x)\n}\nf()\nmes(x)\n", "2\n2\n")
	// ...一方、どこにも見えない名前は関数のローカルになる。
	mustOut(t, "x = 1\ndef f() {\nx = 2\n}\nf()\nmes(x)\n", "2\n")
	// 呼出のローカルは return 後に消える。
	mustErr(t, "def f() {\ny = 3\n}\nf()\nmes(y)\n", "未定義の変数")
	// ブロックはスコープを作らない: if 内の代入は外側のスコープに置かれる。
	mustOut(t, "if true {\nz = 9\n}\nmes(z)\n", "9\n")
	// ループ本体での代入は毎反復で機能する。
	mustOut(t, "repeat 3 as i {\nt = i * 2\nmes(t)\n}\n", "0\n2\n4\n")
	// 組込み名は代入と同じく拒否される。
	mustErr(t, "mes = 1\n", "組み込み関数")
	// void の代入は拒否される。
	mustErr(t, "def v() {\nreturn\n}\nx = v()\n", "void値を使用できません")
	// `let` は今や通常の識別子である（goto/enum と同じ）。
	mustOut(t, "let = 5\nmes(let)\n", "5\n")
	// 代入できない対象は依然拒否される。
	mustErr(t, "5 = 2\n", "左辺に代入できません")
}

func TestRepeatArray(t *testing.T) {
	mustOut(t, "repeat [10, 20, 30] as x {\nmes(x)\n}\n", "10\n20\n30\n")
	mustOut(t, "repeat [\"a\", \"b\"] as i, x {\nmes(str(i) + \":\" + x)\n}\n", "0:a\n1:b\n")
	mustOut(t, "repeat [] as x {\nmes(\"bad\")\n}\nmes(\"ok\")\n", "ok\n")
	mustOut(t, "repeat [1, 2, 3] {\nmes(\"n\")\n}\n", "n\nn\nn\n")
	// break/continue は動く。total が早期終了を示す。
	mustOut(t, "total = 0\nrepeat [1, 2, 3, 4] as x {\nif x == 3 {\nbreak\n}\ntotal += x\n}\nmes(total)\n", "3\n")
	mustOut(t, "repeat [1, 2, 3] as x {\nif x == 2 {\ncontinue\n}\nmes(x)\n}\n", "1\n3\n")
	// 要素の束縛は書き込み可能な複製。配列は変更されず、ループ後に
	// 変数は見えなくなる。
	mustOut(t, "a = [1]\nrepeat a as x {\nx = 9\n}\nmes(a)\n", "[1]\n")
	mustErr(t, "repeat [1] as x {\n}\nmes(x)\n", "未定義の変数")
	// インデックスは読み出し専用。外側の束縛は後で復元される。
	mustOut(t, "i = 100\nx = 200\nrepeat [7, 8] as i, x {\n}\nmes(str(i) + \",\" + str(x))\n", "100,200\n")
	mustErr(t, "repeat [1] as i, x {\ni = 5\n}\n", "読み取り専用")
	mustErr(t, "repeat 5 as i, x {\n}\n", "1 つまで")
	mustErr(t, "repeat \"s\" {\n}\n", "整数または配列")
	mustErr(t, "repeat 1.5 as x {\n}\n", "整数または配列")
	// 長さはループ開始時に固定される: 内部の push は決して延長しない。
	mustOut(t, "a = [1]\nrepeat a as x {\npush(a, 2)\nmes(x)\n}\nmes(length(a))\n", "1\n2\n")
}

func TestMapAbolished(t *testing.T) {
	// マップリテラルは解析エラーである。
	mustErr(t, "m = {\"a\": 1}\n", "廃止")
	mustErr(t, "m = {}\n", "廃止")
	// フィールドアクセスは解析エラーである。
	mustErr(t, "mes(m.a)\n", "廃止")
	mustErr(t, "m.a = 2\n", "廃止")
	// 文字列の添字はマップの検索ではなく整数添字のエラーである。
	mustErr(t, "m = [1]\nm[\"a\"] = 2\n", "整数である必要があります")
	mustErr(t, "m = [1]\nmes(m[\"a\"])\n", "整数である必要があります")
	// マップの組込みは無くなった。
	mustErr(t, "mes(keys([1]))\n", "未定義の関数")
	mustErr(t, "mes(has([1], \"a\"))\n", "未定義の関数")
	mustErr(t, "del([1], \"a\")\n", "未定義の関数")
	// vartype にマップは無い。
	mustOut(t, "mes(vartype([1]))\n", "array\n")
}

func TestRadixLiterals(t *testing.T) {
	mustOut(t, "mes(0xFF)\nmes(0b101)\nmes(0o17)\n", "255\n5\n15\n")
	mustOut(t, "mes(0XAB + 0B1 + 0O7)\n", "179\n")
	mustOut(t, "mes(0xFF & 0xF)\nmes(0b1000 >> 2)\nmes(0o10 | 1)\n", "15\n2\n9\n")
	mustOut(t, "x = 0x10\nmes(x * 2)\n", "32\n")
	mustErr(t, "mes(0x)\n", "不正な16進数リテラル")
	mustErr(t, "mes(0b2)\n", "不正な2進数リテラル")
	mustErr(t, "mes(0o8)\n", "不正な8進数リテラル")
}

func TestArrays(t *testing.T) {
	mustOut(t, "a = [1, 2, 3]\nmes(a[0])\nmes(a[2])\n", "1\n3\n")
	mustOut(t, "a = [10, \"hello\", true]\nmes(a)\n", "[10, hello, true]\n")
	mustOut(t, "a = [[1, 2, 3], [4, 5, 6]]\nmes(a[0][1])\nmes(a[1][2])\n", "2\n6\n")
	mustOut(t, "a = dim(1, 2)\na[0][1] = 123\nmes(a[0][1])\nmes(a)\n", "123\n[[0, 123]]\n")
	mustOut(t, "a = [1]\na[0] = [10, 20, 30]\nmes(a[0][2])\n", "30\n")
	mustOut(t, "a = dim(3)\na[0] = 100\na[2] = 300\nmes(a)\nmes(length(a))\n", "[100, 0, 300]\n3\n")
	mustOut(t, "a = dim(0)\nmes(a)\nmes(length(a))\n", "[]\n0\n")
	mustOut(t, "a = [1]\npush(a, 2, 3)\nmes(a)\n", "[1, 2, 3]\n")
	mustErr(t, "a = [1]\nmes(a[5])\n", "範囲外")
	mustErr(t, "a = [1]\na[1] = 2\n", "範囲外")
	mustErr(t, "a = []\na[0] = 1\n", "範囲外")
	mustErr(t, "nosuch[0] = 1\n", "未定義の変数")
	mustErr(t, "a = [1]\nmes(a[0.5])\n", "整数である必要があります")
	mustErr(t, "a = [1]\nmes(a[-1])\n", "0 以上")
	mustErr(t, "a = [1]\na(0)\n", "[...]")
	mustErr(t, "x = 1\nmes(x[0])\n", "配列に対応しています")
	mustErr(t, "dim()\n", "1 個")
	mustErr(t, "dim(-1)\n", "0 以上")
	mustErr(t, "dim(\"x\")\n", "整数である必要があります")
	// 総要素数（サイズの積）は 2^20 で上限を設ける。それを超えると
	// make() での Go の panic ではなくスクリプトエラーになる。
	// dim(0) は有効なままで、上限自身も確保できる。
	mustErr(t, "dim(9223372036854775807)\n", "上限 1048576 を超えています")
	mustErr(t, "dim(1024, 1025)\n", "上限 1048576 を超えています")
	mustOut(t, "a = dim(1024, 1024)\nmes(length(a))\nmes(length(a[0]))\n", "1024\n1024\n")
}

func TestFunctions(t *testing.T) {
	mustOut(t, "def hello() {\nmes(\"Hello\")\n}\nhello()\n", "Hello\n")
	mustOut(t, "def add(a, b) {\nreturn a + b\n}\nmes(add(10, 20))\n", "30\n")
	mustOut(t, "def f() {\nreturn 1\nreturn 2\n}\nmes(f())\n", "1\n")
	mustOut(t, "def f() {\n}\nmes(f())\n", "\n")
	mustOut(t, "def fact(n) {\nif n <= 1 {\nreturn 1\n}\nreturn n * fact(n - 1)\n}\nmes(fact(5))\n", "120\n")
	mustErr(t, "hello()\n", "未定義の関数")
	mustErr(t, "def f(a) {\n}\nf(1, 2)\n", "1 個必要")
	mustErr(t, "def greet(name, mark=\"!\") {\n}\n", "デフォルト引数")
	mustErr(t, "def f(a) {\n}\nf()\n", "1 個必要")
	mustErr(t, "def f(a=1, b) {\n}\n", "デフォルト引数")
	mustErr(t, "x = 5\nx()\n", "関数ではありません")
	// 関数は値ではない: 名前は呼ぶだけで、読んだり格納したりしない。
	mustErr(t, "def f() {\n}\nmes(f)\n", "未定義の変数")
	mustErr(t, "def f() {\n}\ng = f\n", "未定義の変数")
}

func TestVoidValues(t *testing.T) {
	// mes()/print() は void を無音でスキップする。
	mustOut(t, "def f() {\n}\nmes(f())\n", "\n")
	mustOut(t, "def f() {\n}\nmes(\"a\", f(), \"b\")\n", "a b\n")
	// その他のあらゆる使用はエラーである。
	mustErr(t, "def f() {\n}\nx = f()\n", "void値")
	mustErr(t, "def g() {\n}\ndef h(a) {\nreturn a\n}\nmes(h(g()))\n", "void値")
	mustErr(t, "def f() {\n}\nmes(str(f()))\n", "void値")
	mustErr(t, "def f() {\n}\na = [f()]\n", "void値")
	mustErr(t, "def f() {\n}\nmes(f() == 1)\n", "void値")
	mustErr(t, "def f() {\n}\nmes(1 == f())\n", "void値")
	mustErr(t, "def f() {\n}\nswitch f() {\ncase 1 {\nmes(1)\n}\n}\n", "void値")
	// void は return を通じて伝播し、文としては破棄される。
	mustOut(t, "def g() {\n}\ndef f() {\nreturn g()\n}\nf()\nmes(\"ok\")\n", "ok\n")
	// 非 void の文脈は自身のエラーを保つ。
	mustErr(t, "def f() {\n}\nif f() {\n}\n", "bool")
}

func TestMes(t *testing.T) {
	mustOut(t, "mes(\"Hello\")\n", "Hello\n")
	mustOut(t, "x = 10\nmes(x + 20)\n", "30\n")
	mustOut(t, "x = 1\ny = 2\nmes(\"x =\", x, \"y =\", y)\n", "x = 1 y = 2\n")
	mustOut(t, "mes()\n", "\n")
	mustOut(t, "mes([1, [2, 3]])\n", "[1, [2, 3]]\n")
	// 末尾のスタイルキーワードは装飾として消費される（CUI: SGR）。
	mustOut(t, "mes(\"hi\", \"bold\")\n", "\x1b[1mhi\x1b[22;23;24m\n")
	mustOut(t, "mes(\"a\", \"b\", \"italic\", \"underline\")\n", "\x1b[3m\x1b[4ma b\x1b[22;23;24m\n")
	mustOut(t, "mes(\"t\", \"bold\", \"italic\")\n", "\x1b[1m\x1b[3mt\x1b[22;23;24m\n")
	mustOut(t, "mes(\"bold\")\n", "\x1b[1m\x1b[22;23;24m\n")
	// 完全一致のみ: 他の語や非文字列は印字可能なまま。
	mustOut(t, "mes(\"a\", \"Bold\")\n", "a Bold\n")
	mustOut(t, "mes(\"a\", 1)\n", "a 1\n")
}

func TestCommentsAndNewlines(t *testing.T) {
	mustOut(t, "// hello\n/* multi\nline */\nmes(1)\nmes(2)\n", "1\n2\n")
	mustErr(t, "mes(1);\n", "セミコロン")
}

func TestFormerKeywordsAreIdents(t *testing.T) {
	// goto/gosub/elif は特別な意味を持たない。それらは通常の名前である。
	mustOut(t, "goto = 5\nmes(goto)\n", "5\n")
	mustOut(t, "gosub = 6\nmes(gosub)\n", "6\n")
	mustOut(t, "elif = 7\nmes(elif)\n", "7\n")
	if _, err := Parse("*label\n"); err == nil {
		t.Fatal("labels should be rejected")
	}
	// `enum` は通常の識別子である（enum 文は廃止された。連番定数は値なしの
	// #define を使い、include_test で検証される）。
	mustOut(t, "enum = 5\nmes(enum)\n", "5\n")
}

func TestSample19Golden(t *testing.T) {
	// ParseFile を経由して、プリプロセッサ行（#mode cli）が尊重される。
	prog, err := ParseFile("testdata/parity/sample19.gsh")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	vprog, err := Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := newVmachine(NewInterp(&buf)).runMain(vprog); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "Hello, Alice\nHello, Bob\nodd\n"
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}
