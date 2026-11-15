package main

import (
	"bytes"
	"strings"
	"testing"
)

// runSrc は src を VM で実行し、msg() の出力を返す。
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
	mustOut(t, "x = 10 + 2 * 3\nmsg(x)\n", "16\n")
	mustOut(t, "msg((10 + 2) * 3)\n", "36\n")
	mustOut(t, "msg(10 % 3)\n", "1\n")
	mustOut(t, "msg(7 / 2)\nmsg(7.0 / 2)\n", "3\n3.5\n")
	mustOut(t, "msg(-5 + 8)\nmsg(!true)\n", "3\n0\n")
	mustOut(t, "msg(1 + 2.5)\n", "3.5\n")
	mustErr(t, "msg(1 / 0)\n", "0 による除算")
	mustErr(t, "msg(1 % 0)\n", "0 による剰余")
	mustErr(t, "msg(1.5 % 2)\n", "整数が必要です")
	mustOut(t, "msg(1 + true)\n", "2\n")
}

func TestStrings(t *testing.T) {
	mustOut(t, "name = \"Alice\"\nmsg(\"Hello, \" + name)\n", "Hello, Alice\n")
	mustOut(t, "msg(\"n=\" + 42)\n", "n=42\n")
	mustOut(t, "msg(\"a\" < \"b\")\n", "1\n")
}

func TestConditions(t *testing.T) {
	mustOut(t, "if 1 < 2 && 2 < 3 || false\nmsg(\"y\")\nend\n", "y\n")
	mustOut(t, "if 1\nmsg(1)\nend\n", "1\n")
	mustOut(t, "if 0\nmsg(1)\nelse\nmsg(0)\nend\n", "0\n")
	mustOut(t, "if 2\nmsg(2)\nend\n", "2\n")
	mustErr(t, "if \"s\"\nmsg(1)\nend\n", "整数")
	mustOut(t, "x = 1 && true\nmsg(x)\n", "1\n")
	mustOut(t, "msg(!1)\n", "0\n")
	mustOut(t, "msg(!0)\n", "1\n")
	mustOut(t, "msg(2 && 3)\n", "1\n")
	mustOut(t, "msg(2 || 0)\n", "1\n")
}

func TestIfElse(t *testing.T) {
	src := "x = 7\nif x > 10\nmsg(\"large\")\nelseif x > 5\nmsg(\"middle\")\nelse\nmsg(\"small\")\nend\n"
	mustOut(t, src, "middle\n")
	mustOut(t, "x = 3\nif x > 10\nmsg(1)\nelse\nmsg(2)\nend\n", "2\n")
}

func TestRepeatCounter(t *testing.T) {
	mustOut(t, "repeat 3 as i\nmsg(i)\nend\n", "0\n1\n2\n")
	mustOut(t, "s = 0\nrepeat 10 as i\ns = s + i\nend\nmsg(s)\n", "45\n")
	mustErr(t, "repeat 2 as i\nmsg(i)\nend\nmsg(i)\n", "未定義の変数")
	mustErr(t, "repeat 2 as i\ni = 100\nend\n", "読み取り専用")
	mustErr(t, "repeat 2 as i\ndef i()\nend\nend\n", "トップレベル")
	mustOut(t, "repeat true\nmsg(1)\nend\n", "1\n")
	mustErr(t, "repeat -1\nmsg(1)\nend\n", "0 以上")
}

func TestBreakContinue(t *testing.T) {
	mustOut(t, "s = -1\nrepeat 100 as i\nif i == 10\nbreak\nend\ns = i\nend\nmsg(s)\n", "9\n")
	mustOut(t, "repeat 10 as i\nif i % 2 == 0\ncontinue\nend\nmsg(i)\nend\n", "1\n3\n5\n7\n9\n")
	mustErr(t, "break\n", "ループ外の break")
	mustErr(t, "continue\n", "ループ外の continue")
	mustErr(t, "repeat 2 as i\nmsg(i)\nend\nmsg(i)\n", "未定義の変数")
}

func TestWhile(t *testing.T) {
	mustOut(t, "i = 0\nwhile i < 3\nmsg(i)\ni = i + 1\nend\n", "0\n1\n2\n")
	mustOut(t, "while false\nmsg(1)\nend\nmsg(0)\n", "0\n")
	mustOut(t, "i = 0\ns = 0\nwhile true\nif i >= 5\nbreak\nend\ns = s + i\ni = i + 1\nend\nmsg(s)\n", "10\n")
	mustOut(t, "i = 0\nwhile i < 10\ni = i + 1\nif i % 2 == 0\ncontinue\nend\nmsg(i)\nend\n", "1\n3\n5\n7\n9\n")
	mustOut(t, "i = 0\nwhile i < 3\nj = 0\nwhile j < 2\nmsg(i * 10 + j)\nj = j + 1\nend\ni = i + 1\nend\n", "0\n1\n10\n11\n20\n21\n")
	mustOut(t, "i = 0\nwhile i < 5\ni = i + 1\nif i == 2\nbreak\nend\nend\nmsg(i)\n", "2\n")
	mustOut(t, "while 1\nbreak\nend\nmsg(1)\n", "1\n")
	mustErr(t, "while \"x\"\nend\n", "条件式は整数")
}

func TestSwitch(t *testing.T) {
	mustOut(t, "x = 2\nswitch x\ncase 1\nmsg(\"one\")\ncase 2\nmsg(\"two\")\ndefault\nmsg(\"other\")\nend\n", "two\n")
	mustOut(t, "x = 2\nswitch x\ncase 1, 2, 3\nmsg(\"few\")\ndefault\nmsg(\"many\")\nend\n", "few\n")
	mustOut(t, "x = 9\nswitch x\ncase 1\nmsg(\"one\")\nend\nmsg(\"done\")\n", "done\n")
	mustOut(t, "x = 9\nswitch x\ncase 1\nmsg(\"one\")\ndefault\nmsg(\"other\")\nend\n", "other\n")
	// 最初に一致したものが勝つ。フォールスルーはしない。
	mustOut(t, "x = 1\nswitch x\ncase 1\nmsg(\"a\")\ncase 1\nmsg(\"b\")\nend\n", "a\n")
	mustOut(t, "x = 1\nswitch x\ncase 1\nmsg(\"a\")\nbreak\nmsg(\"unreached\")\ndefault\nmsg(\"d\")\nend\nmsg(\"after\")\n", "a\nafter\n")
	// ループ内の switch の break はループではなく switch を終わる。
	mustOut(t, "i = 0\nrepeat 3\nswitch i\ncase 1\nbreak\ndefault\nmsg(i)\nend\ni = i + 1\nend\n", "0\n2\n")
	// continue は switch を通り抜けてループに移る。
	mustOut(t, "repeat 3 as i\nswitch i\ncase 1\ncontinue\ndefault\nmsg(i)\nend\nend\n", "0\n2\n")
	// case 内の repeat 内の break は repeat を終わる。
	mustOut(t, "x = 1\nswitch x\ncase 1\ns = -1\nrepeat 10 as i\nif i == 2\nbreak\nend\ns = i\nend\nmsg(s)\nend\n", "1\n")
	// スカーチニーは一度評価される。
	mustOut(t, "n = 0\ndef next()\nn = n + 1\nreturn n\nend\nswitch next()\ncase 1\nmsg(\"one\")\ndefault\nmsg(\"other\")\nend\nmsg(n)\n", "one\n1\n")
	// 型の強制と不一致。
	mustOut(t, "x = 1.0\nswitch x\ncase 1\nmsg(\"int\")\ndefault\nmsg(\"other\")\nend\n", "int\n")
	mustOut(t, "x = 1\nswitch x\ncase \"1\"\nmsg(\"str\")\ndefault\nmsg(\"other\")\nend\n", "other\n")
	mustOut(t, "s = \"b\"\nswitch s\ncase \"a\"\nmsg(1)\ncase \"b\"\nmsg(2)\nend\n", "2\n")
	// 入れ子の switch。
	mustOut(t, "x = 1\ny = 2\nswitch x\ncase 1\nswitch y\ncase 2\nmsg(\"1-2\")\ndefault\nmsg(\"1-?\")\nend\ndefault\nmsg(\"outer\")\nend\n", "1-2\n")
	mustErr(t, "switch 1\ndefault\ndefault\nend\n", "default が重複")
	mustErr(t, "switch 1\nmsg(1)\nend\n", "case または default が必要")
}

func TestBitwise(t *testing.T) {
	mustOut(t, "msg(6 & 3)\nmsg(6 | 3)\nmsg(6 ^ 3)\n", "2\n7\n5\n")
	mustOut(t, "msg(~0)\nmsg(~5)\nmsg(1 << 4)\nmsg(256 >> 3)\nmsg(-8 >> 2)\n", "-1\n-6\n16\n32\n-2\n")
	mustOut(t, "msg(1 | 2 & 3)\nmsg(1 << 2 + 1)\nmsg(8 >> 1 + 1)\n", "3\n8\n2\n")
	mustOut(t, "msg(255 & 15)\nmsg(1 << 0)\nmsg(0 >> 5)\n", "15\n1\n0\n")
	// 近代化された優先順位: & | ^ は == より強く結合する（C とは違う）。
	mustOut(t, "msg(1 & 1 == 1)\nmsg(2 | 1 == 3)\nmsg(0 & 0 == 0)\n", "1\n1\n1\n")
	mustErr(t, "msg(1.5 & 1)\n", "整数が必要です")
	mustErr(t, "msg(\"a\" | 1)\n", "整数が必要です")
	mustErr(t, "msg(1 ^ 2.0)\n", "整数が必要です")
	mustErr(t, "msg(1 << 2.5)\n", "整数が必要です")
	mustErr(t, "msg(1 << -1)\n", "シフト数は 0 から 63")
	mustErr(t, "msg(1 << 64)\n", "シフト数は 0 から 63")
	mustErr(t, "msg(1 >> -2)\n", "シフト数は 0 から 63")
	mustErr(t, "msg(~1.5)\n", "整数のみ")
	mustErr(t, "msg(~\"a\")\n", "整数のみ")
}

func TestCompoundAssign(t *testing.T) {
	mustOut(t, "x = 10\nx += 5\nmsg(x)\nx -= 3\nmsg(x)\nx *= 2\nmsg(x)\nx /= 4\nmsg(x)\nx %= 5\nmsg(x)\n", "15\n12\n24\n6\n1\n")
	mustOut(t, "x = 6\nx &= 3\nmsg(x)\nx |= 8\nmsg(x)\nx ^= 7\nmsg(x)\nx <<= 2\nmsg(x)\nx >>= 1\nmsg(x)\n", "2\n10\n13\n52\n26\n")
	mustOut(t, "s = \"a\"\ns += \"b\"\nmsg(s)\nf = 1.5\nf += 1\nmsg(f)\n", "ab\n2.5\n")
	mustOut(t, "a = [1, 2]\na[0] += 10\na[1] *= 3\nmsg(a)\n", "[11, 6]\n")
	mustOut(t, "p = [[1]]\np[0][0] += 41\nmsg(p[0][0])\n", "42\n")
	mustErr(t, "x = 1\nx += [1]\n", "加算できません")
	mustErr(t, "x = 1.5\nx &= 1\n", "整数が必要です")
	mustErr(t, "x = 1\nx /= 0\n", "0 による除算")
	// 単一評価: 添字式はちょうど一度実行される。
	mustOut(t, "i = 0\ndef next()\ni = i + 1\nreturn 0\nend\na = [5]\na[next()] += 10\nmsg(i)\nmsg(a[0])\n", "1\n15\n")
	mustOut(t, "i = 0\ndef next()\ni = i + 1\nreturn 0\nend\nm = [[7]]\nm[next()][0] *= 2\nmsg(i)\nmsg(m[0][0])\n", "1\n14\n")
	mustErr(t, "x += 1\n", "未定義の変数")
	mustErr(t, "msg += 1\n", "未定義の変数")
}

func TestMinInt64Literal(t *testing.T) {
	// -9223372036854775808 は MinInt64 に畳まれる（正の半分は int64 を
	// 桁あふれするため、裸のリテラルは存在しない）。
	mustOut(t, "msg(-9223372036854775808)\n", "-9223372036854775808\n")
	mustOut(t, "msg(-0x8000000000000000)\n", "-9223372036854775808\n")
	mustOut(t, "m = -9223372036854775808\nmsg(m == -9223372036854775808)\nmsg(m < 0)\n", "1\n1\n")
	mustErr(t, "msg(9223372036854775808)\n", "不正な整数リテラル")
	mustErr(t, "msg(0x8000000000000000)\n", "不正な整数リテラル")
	// 整数演算はラップする（Go ライク）。abs() だけが MinInt64 を拒否する。
	mustOut(t, "msg(9223372036854775807 + 1)\n", "-9223372036854775808\n")
	mustOut(t, "msg(-(-9223372036854775808))\n", "-9223372036854775808\n")
}

func TestFiniteFloats(t *testing.T) {
	// 桁あふれする浮動小数点数演算はエラーで、±Inf には決してならない。
	mustErr(t, "msg(1e308 * 10)\n", "有限ではありません")
	mustErr(t, "msg(1e308 + 1e308)\n", "有限ではありません")
	mustErr(t, "msg(1e308 / 1e-308)\n", "有限ではありません")
	// 通常の浮動小数点数は影響を受けない。
	mustOut(t, "msg(0.1 + 0.2 == 0.30000000000000004)\n", "1\n")
}

func TestHardeningTraps(t *testing.T) {
	// 文字列連結は void を "null" として蘇らせない。
	mustErr(t, "def f()\nend\nmsg(\"a\" + f())\n", "void値を使用できません")
	// 組込み名は代入できない（組込みをシャドーイングしてしまうため）。
	mustErr(t, "msg = 1\n", "組み込み関数")
	mustErr(t, "len = 1\n", "組み込み関数")
	// ユーザー定義関数は再定義できない。
	mustErr(t, "def f()\nend\ndef f()\nend\n", "既に定義されています")
	// switch case の値はスカーチニーと同じく void を拒否する。
	mustErr(t, "def f()\nend\nswitch 1\ncase f()\nmsg(\"x\")\nend\n", "void値を使用できません")
}

func TestRecursionLimit(t *testing.T) {
	mustErr(t, "def boom()\nreturn boom()\nend\nmsg(boom())\n", "深すぎます")
	// 健全性: 通常の再帰は依然動く。
	mustOut(t, "def fact(n)\nif n <= 1\nreturn 1\nend\nreturn n * fact(n - 1)\nend\nmsg(fact(10))\n", "3628800\n")
}

func TestParseDepthLimit(t *testing.T) {
	// 50段の入れ子はコンパイルされ実行される。1100段はパーサに
	// 拒否される（ホストのスタック防護。VM のレジスタ上限も制限する）。
	shallow := strings.Repeat("if true\n", 50) + "msg(1)\n" + strings.Repeat("end\n", 50)
	mustOut(t, shallow, "1\n")
	deep := strings.Repeat("if true\n", 1100) + "msg(1)\n" + strings.Repeat("end\n", 1100)
	mustErr(t, deep, "ネストが深すぎます")
	shallowExpr := strings.Repeat("(", 500) + "1" + strings.Repeat(")", 500) + "\n"
	mustOut(t, "msg"+shallowExpr, "1\n")
	deepExpr := strings.Repeat("(", 1100) + "1" + strings.Repeat(")", 1100) + "\n"
	mustErr(t, "msg"+deepExpr, "ネストが深すぎます")
}

func TestTryCatchAbolished(t *testing.T) {
	// try/catch は今や通常の識別子である。try ブロックは無くなった。
	mustOut(t, "try = 5\nmsg(try)\n", "5\n")
	mustErr(t, "try {\nmsg(1)\n}\n", "'{' は使用できません")
	// throw() も廃止された: 通常の未定義呼出である。
	mustErr(t, "throw(\"late\")\n", "未定義の関数")
}

func TestDeclareByAssign(t *testing.T) {
	// `let` 廃止後：通常代入が見える定義の更新、なければ宣言になる。
	mustOut(t, "x = 1\nmsg(x)\n", "1\n")
	// 同じスコープの上書きは許可される（ループが動き続ける）。
	mustOut(t, "x = 1\nx = 2\nmsg(x)\n", "2\n")
	// シャドーイング宣言は無くなった: 関数内の代入は可視の（ここでは
	// グローバルの）定義を更新する。
	mustOut(t, "x = 1\ndef f()\nx = 2\nmsg(x)\nend\nf()\nmsg(x)\n", "2\n2\n")
	// ...一方、どこにも見えない名前は関数のローカルになる。
	mustOut(t, "x = 1\ndef f()\nx = 2\nend\nf()\nmsg(x)\n", "2\n")
	// 呼出のローカルは return 後に消える。
	mustErr(t, "def f()\ny = 3\nend\nf()\nmsg(y)\n", "未定義の変数")
	// ブロックはスコープを作らない: if 内の代入は外側のスコープに置かれる。
	mustOut(t, "if true\nz = 9\nend\nmsg(z)\n", "9\n")
	// ループ本体での代入は毎反復で機能する。
	mustOut(t, "repeat 3 as i\nt = i * 2\nmsg(t)\nend\n", "0\n2\n4\n")
	// 組込み名は代入と同じく拒否される。
	mustErr(t, "msg = 1\n", "組み込み関数")
	// void の代入は拒否される。
	mustErr(t, "def v()\nreturn\nend\nx = v()\n", "void値を使用できません")
	// `let` は今や通常の識別子である（goto/enum と同じ）。
	mustOut(t, "let = 5\nmsg(let)\n", "5\n")
	// 代入できない対象は依然拒否される。
	mustErr(t, "5 = 2\n", "左辺に代入できません")
}

func TestRepeatArray(t *testing.T) {
	mustOut(t, "repeat [10, 20, 30] as x\nmsg(x)\nend\n", "10\n20\n30\n")
	mustOut(t, "repeat [\"a\", \"b\"] as i, x\nmsg(str(i) + \":\" + x)\nend\n", "0:a\n1:b\n")
	mustOut(t, "repeat [] as x\nmsg(\"bad\")\nend\nmsg(\"ok\")\n", "ok\n")
	mustOut(t, "repeat [1, 2, 3]\nmsg(\"n\")\nend\n", "n\nn\nn\n")
	// break/continue は動く。total が早期終了を示す。
	mustOut(t, "total = 0\nrepeat [1, 2, 3, 4] as x\nif x == 3\nbreak\nend\ntotal += x\nend\nmsg(total)\n", "3\n")
	mustOut(t, "repeat [1, 2, 3] as x\nif x == 2\ncontinue\nend\nmsg(x)\nend\n", "1\n3\n")
	// 要素の束縛は書き込み可能な複製。配列は変更されず、ループ後に
	// 変数は見えなくなる。
	mustOut(t, "a = [1]\nrepeat a as x\nx = 9\nend\nmsg(a)\n", "[1]\n")
	mustErr(t, "repeat [1] as x\nend\nmsg(x)\n", "未定義の変数")
	// インデックスは読み出し専用。外側の束縛は後で復元される。
	mustOut(t, "i = 100\nx = 200\nrepeat [7, 8] as i, x\nend\nmsg(str(i) + \",\" + str(x))\n", "100,200\n")
	mustErr(t, "repeat [1] as i, x\ni = 5\nend\n", "読み取り専用")
	mustErr(t, "repeat 5 as i, x\nend\n", "1 つまで")
	mustErr(t, "repeat 1.5 as x\nend\n", "整数・配列・文字列")
	// 文字列は rune 単位で反復する。as i, ch は添字と1文字、
	// as ch は1文字だけを束縛する。
	mustOut(t, "repeat \"aあb\" as i, ch\nmsg(str(i) + \":\" + ch)\nend\n", "0:a\n1:あ\n2:b\n")
	mustOut(t, "repeat \"あい\" as ch\nmsg(ch)\nend\n", "あ\nい\n")
	mustOut(t, "repeat \"ab\"\nmsg(\"x\")\nend\n", "x\nx\n")
	mustOut(t, "repeat \"\" as ch\nmsg(\"bad\")\nend\nmsg(\"ok\")\n", "ok\n")
	mustErr(t, "repeat \"ab\" as i, ch\ni = 5\nend\n", "読み取り専用")
	// 長さはループ開始時に固定される: 内部の push は決して延長しない。
	mustOut(t, "a = [1]\nrepeat a as x\npush(a, 2)\nmsg(x)\nend\nmsg(len(a))\n", "1\n2\n")
}

func TestStringIndex(t *testing.T) {
	mustOut(t, "s = \"abc\"\nmsg(s[0])\nmsg(s[2])\n", "a\nc\n")
	// 文字列の添字はバイトではなく rune 単位。
	mustOut(t, "s = \"あいう\"\nmsg(s[1])\n", "い\n")
	// 配列要素の文字列にも連鎖してアクセスできる。
	mustOut(t, "a = [\"xyz\"]\nmsg(a[0][1])\n", "y\n")
	mustErr(t, "msg(\"abc\"[3])\n", "範囲外")
	mustErr(t, "msg(\"abc\"[-1])\n", "0 以上")
	mustErr(t, "msg(\"abc\"[0.0])\n", "整数である必要があります")
	mustErr(t, "msg(\"abc\"[\"x\"])\n", "整数である必要があります")
	// 文字列は不変なので、添字への代入はエラーになる。
	mustErr(t, "s = \"abc\"\ns[0] = \"x\"\n", "変更できません")
	mustErr(t, "a = [\"abc\"]\na[0][1] = \"x\"\n", "変更できません")
	mustErr(t, "s = \"abc\"\ns[0] += \"x\"\n", "変更できません")
}

func TestMapAbolished(t *testing.T) {
	// マップリテラルは解析エラーである。
	mustErr(t, "m = {\"a\": 1}\n", "'{' は使用できません")
	mustErr(t, "m = {}\n", "'{' は使用できません")
	// フィールドアクセスは解析エラーである。
	mustErr(t, "msg(m.a)\n", "'.' は使用できません")
	mustErr(t, "m.a = 2\n", "'.' は使用できません")
	// 文字列の添字はマップの検索ではなく整数添字のエラーである。
	mustErr(t, "m = [1]\nm[\"a\"] = 2\n", "整数である必要があります")
	mustErr(t, "m = [1]\nmsg(m[\"a\"])\n", "整数である必要があります")
	// マップの組込みは無くなった。
	mustErr(t, "msg(keys([1]))\n", "未定義の関数")
	mustErr(t, "msg(has([1], \"a\"))\n", "未定義の関数")
	mustErr(t, "del([1], \"a\")\n", "未定義の関数")
	// vartype にマップは無い。
	mustOut(t, "msg(vartype([1]))\n", "array\n")
}

func TestRadixLiterals(t *testing.T) {
	mustOut(t, "msg(0xFF)\nmsg(0b101)\nmsg(0o17)\n", "255\n5\n15\n")
	mustOut(t, "msg(0XAB + 0B1 + 0O7)\n", "179\n")
	mustOut(t, "msg(0xFF & 0xF)\nmsg(0b1000 >> 2)\nmsg(0o10 | 1)\n", "15\n2\n9\n")
	mustOut(t, "x = 0x10\nmsg(x * 2)\n", "32\n")
	mustErr(t, "msg(0x)\n", "不正な16進数リテラル")
	mustErr(t, "msg(0b2)\n", "不正な2進数リテラル")
	mustErr(t, "msg(0o8)\n", "不正な8進数リテラル")
}

func TestArrays(t *testing.T) {
	mustOut(t, "a = [1, 2, 3]\nmsg(a[0])\nmsg(a[2])\n", "1\n3\n")
	mustOut(t, "a = [10, \"hello\", true]\nmsg(a)\n", "[10, hello, 1]\n")
	mustOut(t, "a = [[1, 2, 3], [4, 5, 6]]\nmsg(a[0][1])\nmsg(a[1][2])\n", "2\n6\n")
	mustOut(t, "a = dim(1, 2)\na[0][1] = 123\nmsg(a[0][1])\nmsg(a)\n", "123\n[[0, 123]]\n")
	mustOut(t, "a = [1]\na[0] = [10, 20, 30]\nmsg(a[0][2])\n", "30\n")
	mustOut(t, "a = dim(3)\na[0] = 100\na[2] = 300\nmsg(a)\nmsg(len(a))\n", "[100, 0, 300]\n3\n")
	mustOut(t, "a = dim(0)\nmsg(a)\nmsg(len(a))\n", "[]\n0\n")
	mustOut(t, "a = [1]\npush(a, 2, 3)\nmsg(a)\n", "[1, 2, 3]\n")
	mustErr(t, "a = [1]\nmsg(a[5])\n", "範囲外")
	mustErr(t, "a = [1]\na[1] = 2\n", "範囲外")
	mustErr(t, "a = []\na[0] = 1\n", "範囲外")
	mustErr(t, "nosuch[0] = 1\n", "未定義の変数")
	mustErr(t, "a = [1]\nmsg(a[0.5])\n", "整数である必要があります")
	mustErr(t, "a = [1]\nmsg(a[-1])\n", "0 以上")
	mustErr(t, "a = [1]\na(0)\n", "[...]")
	mustErr(t, "x = 1\nmsg(x[0])\n", "配列または文字列に対応しています")
	mustErr(t, "dim()\n", "1 個")
	mustErr(t, "dim(-1)\n", "0 以上")
	mustErr(t, "dim(\"x\")\n", "整数である必要があります")
	// 総要素数（サイズの積）は 2^20 で上限を設ける。それを超えると
	// make() での Go の panic ではなくスクリプトエラーになる。
	// dim(0) は有効なままで、上限自身も確保できる。
	mustErr(t, "dim(9223372036854775807)\n", "上限 1048576 を超えています")
	mustErr(t, "dim(1024, 1025)\n", "上限 1048576 を超えています")
	mustOut(t, "a = dim(1024, 1024)\nmsg(len(a))\nmsg(len(a[0]))\n", "1024\n1024\n")
}

func TestFunctions(t *testing.T) {
	mustOut(t, "def hello()\nmsg(\"Hello\")\nend\nhello()\n", "Hello\n")
	mustOut(t, "def add(a, b)\nreturn a + b\nend\nmsg(add(10, 20))\n", "30\n")
	mustOut(t, "def f()\nreturn 1\nreturn 2\nend\nmsg(f())\n", "1\n")
	mustOut(t, "def f()\nend\nmsg(f())\n", "\n")
	mustOut(t, "def fact(n)\nif n <= 1\nreturn 1\nend\nreturn n * fact(n - 1)\nend\nmsg(fact(5))\n", "120\n")
	mustErr(t, "hello()\n", "未定義の関数")
	mustErr(t, "def f(a)\nend\nf(1, 2)\n", "1 個必要")
	mustErr(t, "def greet(name, mark=\"!\")\nend\n", "デフォルト引数")
	mustErr(t, "def f(a)\nend\nf()\n", "1 個必要")
	mustErr(t, "def f(a=1, b)\nend\n", "デフォルト引数")
	mustErr(t, "x = 5\nx()\n", "関数ではありません")
	// 関数は値ではない: 名前は呼ぶだけで、読んだり格納したりしない。
	mustErr(t, "def f()\nend\nmsg(f)\n", "未定義の変数")
	mustErr(t, "def f()\nend\ng = f\n", "未定義の変数")
}

func TestVoidValues(t *testing.T) {
	// msg()/print() は void を無音でスキップする（単一引数）。
	mustOut(t, "def f()\nend\nmsg(f())\n", "\n")
	mustOut(t, "def f()\nend\nprint(f())\nmsg(\"ok\")\n", "ok\n")
	// その他のあらゆる使用はエラーである。
	mustErr(t, "def f()\nend\nx = f()\n", "void値")
	mustErr(t, "def g()\nend\ndef h(a)\nreturn a\nend\nmsg(h(g()))\n", "void値")
	mustErr(t, "def f()\nend\nmsg(str(f()))\n", "void値")
	mustErr(t, "def f()\nend\na = [f()]\n", "void値")
	mustErr(t, "def f()\nend\nmsg(f() == 1)\n", "void値")
	mustErr(t, "def f()\nend\nmsg(1 == f())\n", "void値")
	mustErr(t, "def f()\nend\nswitch f()\ncase 1\nmsg(1)\nend\n", "void値")
	// void は return を通じて伝播し、文としては破棄される。
	mustOut(t, "def g()\nend\ndef f()\nreturn g()\nend\nf()\nmsg(\"ok\")\n", "ok\n")
	// 非 void の文脈は自身のエラーを保つ。
	mustErr(t, "def f()\nend\nif f()\nend\n", "整数")
}

func TestMsg(t *testing.T) {
	mustOut(t, "msg(\"Hello\")\n", "Hello\n")
	mustOut(t, "x = 10\nmsg(x + 20)\n", "30\n")
	mustOut(t, "x = 1\ny = 2\nmsg(\"x = \" + str(x) + \" y = \" + str(y))\n", "x = 1 y = 2\n")
	mustErr(t, "msg()\n", "1 個必要")
	mustErr(t, "msg(\"a\", \"b\")\n", "1 個必要")
	mustErr(t, "print()\n", "1 個必要")
	mustErr(t, "print(\"a\", \"b\")\n", "1 個必要")
	mustOut(t, "msg([1, [2, 3]])\n", "[1, [2, 3]]\n")
	// 装飾指定は廃止: そのまま印字される。
	mustOut(t, "msg(\"hi\")\n", "hi\n")
	mustOut(t, "msg(\"bold\")\n", "bold\n")
	mustOut(t, "msg(\"a\" + \" \" + \"Bold\")\n", "a Bold\n")
	mustOut(t, "msg(\"a\" + \" \" + str(1))\n", "a 1\n")
}

func TestCommentsAndNewlines(t *testing.T) {
	mustOut(t, "// hello\nmsg(1)\nmsg(2)\n", "1\n2\n")
	mustErr(t, "msg(1);\n", "セミコロン")
}

func TestFormerKeywordsAreIdents(t *testing.T) {
	// goto/gosub/elif は特別な意味を持たない。それらは通常の名前である。
	mustOut(t, "goto = 5\nmsg(goto)\n", "5\n")
	mustOut(t, "gosub = 6\nmsg(gosub)\n", "6\n")
	mustOut(t, "elif = 7\nmsg(elif)\n", "7\n")
	if _, err := Parse("*label\n"); err == nil {
		t.Fatal("labels should be rejected")
	}
	// `const` / `enum` は廃止され、通常の識別子である（定数は変数への代入で代用）。
	mustOut(t, "enum = 5\nmsg(enum)\n", "5\n")
	mustOut(t, "const = 5\nmsg(const)\n", "5\n")
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
