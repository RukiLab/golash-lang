// VM の振舞いテスト：パリティでは拾いにくい境界（継続性・復元・上限）を検証。
package main

import (
	"bytes"
	"strings"
	"testing"
)

// runVMOnly は VM 単体で実行する。
func runVMOnly(t *testing.T, src string) (string, error) {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	vprog, err := Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	in := NewInterpWithIO(&buf, strings.NewReader(""))
	err = newVmachine(in).runMain(vprog)
	return buf.String(), err
}

func TestVMRecursionLimit(t *testing.T) {
	_, err := runVMOnly(t, "def f(n) {\nreturn f(n + 1)\n}\nmes(f(0))\n")
	if err == nil || !strings.Contains(err.Error(), "深すぎます") {
		t.Fatalf("want depth error, got %v", err)
	}
	// 位置は再帰呼出の位置（tree と同一）。
	if !strings.Contains(err.Error(), "2:9") {
		t.Fatalf("want call position 2:9, got %v", err)
	}
}

func TestVMDefTiming(t *testing.T) {
	// def は逐次登録：定義前呼出は未定義エラー。
	_, err := runVMOnly(t, "mes(f())\ndef f() {\nreturn 1\n}\n")
	if err == nil || !strings.Contains(err.Error(), "未定義の関数") {
		t.Fatalf("want undefined function, got %v", err)
	}
	out, err := runVMOnly(t, "def f() {\nreturn 7\n}\nmes(f())\n")
	if err != nil || out != "7\n" {
		t.Fatalf("got out=%q err=%v", out, err)
	}
}

func TestVMRepeatRestoreOnError(t *testing.T) {
	// エラー時も repeat 束縛は復元される（runtime.go の defer 対応）。
	// REPL 的継続：同一機械で2入力。
	prog1, _ := Parse("x = 99\nrepeat 5 as x {\nmes(undefined_fn())\n}\n")
	vprog1, _ := Compile(prog1)
	var buf bytes.Buffer
	in := NewInterpWithIO(&buf, strings.NewReader(""))
	m := newVmachine(in)
	if err := m.runMain(vprog1); err == nil {
		t.Fatalf("want error")
	}
	// x は 99 に復元されていること。
	prog2, _ := Parse("mes(x)\n")
	vprog2, _ := Compile(prog2)
	if err := m.runMain(vprog2); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if buf.String() != "99\n" {
		t.Fatalf("x not restored: %q", buf.String())
	}
}

func TestVMRepeatShadowRestore(t *testing.T) {
	out, err := runVMOnly(t, "x = 1\nrepeat 3 as x {\nmes(x)\n}\nmes(x)\n")
	if err != nil || out != "0\n1\n2\n1\n" {
		t.Fatalf("got out=%q err=%v", out, err)
	}
}

func TestVMFuncScopeRule(t *testing.T) {
	// 関数内新規代入は呼出ローカル（グローバルを作らない）。
	out, err := runVMOnly(t, "def f() {\nq_local_xyz = 5\nreturn q_local_xyz\n}\nmes(f())\nmes(q_local_xyz)\n")
	if err == nil || !strings.Contains(err.Error(), "未定義の変数") {
		t.Fatalf("want undefined variable, got out=%q err=%v", out, err)
	}
	// グローバル更新は可視。
	out, err = runVMOnly(t, "g = 1\ndef f() {\ng = 2\n}\nf()\nmes(g)\n")
	if err != nil || out != "2\n" {
		t.Fatalf("got out=%q err=%v", out, err)
	}
}

func TestVMEndExitCode(t *testing.T) {
	prog, _ := Parse("mes(\"a\")\nend(3)\nmes(\"b\")\n")
	vprog, _ := Compile(prog)
	var buf bytes.Buffer
	in := NewInterpWithIO(&buf, strings.NewReader(""))
	m := newVmachine(in)
	if err := m.runMain(vprog); err != nil {
		t.Fatalf("end must not error: %v", err)
	}
	if code, ok := in.ExitCode(); !ok || code != 3 {
		t.Fatalf("want exit 3, got %d %v", code, ok)
	}
	if buf.String() != "a\n" {
		t.Fatalf("want only first mes, got %q", buf.String())
	}
}

func TestVMArityBeforeArgs(t *testing.T) {
	// arity エラーは引数評価より先（未定義引数でも arity が出る）。
	_, err := runVMOnly(t, "def f(a) {\nreturn a\n}\nmes(f(1, nope_undefined_xyz))\n")
	if err == nil || !strings.Contains(err.Error(), "引数を 1 個必要") {
		t.Fatalf("want arity error, got %v", err)
	}
}

func TestVMTopLevelControl(t *testing.T) {
	for src, want := range map[string]string{
		"break\n":    "ループ外の break です",
		"continue\n": "ループ外の continue です",
		"return 1\n": "関数外の return です",
	} {
		_, err := runVMOnly(t, src)
		// tree と同様、位置なし RuntimeError（"0:0: ..." 形式）で返る。
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("src %q: got %v want substring %q", src, err, want)
		}
	}
}

func TestVMNestedIndexWriteOrder(t *testing.T) {
	// 複層書込の診断位置（内側範囲外は inner.At）。
	_, err := runVMOnly(t, "a = [[1,2],[3,4]]\na[9] = 1\n")
	_ = err
	_, err = runVMOnly(t, "a = [[1,2],[3,4]]\na[0][9] = 1\n")
	if err == nil || !strings.Contains(err.Error(), "範囲外") {
		t.Fatalf("want bounds error, got %v", err)
	}
	out, err := runVMOnly(t, "a = [[1,2],[3,4]]\na[1][0] = 9\nmes(a[1][0])\n")
	if err != nil || out != "9\n" {
		t.Fatalf("got out=%q err=%v", out, err)
	}
}
