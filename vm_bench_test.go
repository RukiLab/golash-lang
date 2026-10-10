// VM のベンチマークと性能回帰テスト。
//
// 目標：
//   - 数値ホットループはループ1周あたり 0 allocs（実行回数 N を変えても
//     1 実行の割当てが増えないことで検証）。
//   - 再帰・ループとも単一バックエンド（VM）として正しく高速動作すること。
package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func mustParseB(b *testing.B, src string) *Program {
	b.Helper()
	prog, err := Parse(src)
	if err != nil {
		b.Fatalf("parse: %v", err)
	}
	return prog
}

const benchLoopSrc = "i = 0\nwhile i < 1000000 {\ni = i + 1\n}\n"

const benchFibSrc = "def fib(n) {\nif n < 2 {\nreturn n\n}\nreturn fib(n - 1) + fib(n - 2)\n}\nmes(fib(24))\n"

// vmAllocsPerRun は VM 1 実行あたりの平均割当て数を返す（機械は毎回新規）。
func vmAllocsPerRun(b *testing.B, src string, runs int) float64 {
	b.Helper()
	prog := mustParseB(b, src)
	vprog, err := Compile(prog)
	if err != nil {
		b.Fatalf("compile: %v", err)
	}
	return testing.AllocsPerRun(runs, func() {
		var buf bytes.Buffer
		in := NewInterpWithIO(&buf, strings.NewReader(""))
		if err := newVmachine(in).runMain(vprog); err != nil {
			b.Fatalf("vm run: %v", err)
		}
	})
}

// TestVMZeroAllocLoop はループ1周あたり 0 allocs を検証する。
// 反復回数を 10 倍にしても 1 実行の割当てが増えなければ、
// 増分（＝ループ本体）は割当てゼロである。
func TestVMZeroAllocLoop(t *testing.T) {
	small := "i = 0\nwhile i < 100000 {\ni = i + 1\n}\n"
	large := "i = 0\nwhile i < 1000000 {\ni = i + 1\n}\n"
	aSmall := vmAllocsPerRunT(t, small)
	aLarge := vmAllocsPerRunT(t, large)
	t.Logf("allocs/run small=%v large=%v", aSmall, aLarge)
	if aLarge > aSmall+1 {
		t.Fatalf("loop allocates per iteration: small=%v large=%v", aSmall, aLarge)
	}
}

func vmAllocsPerRunT(t *testing.T, src string) float64 {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	vprog, err := Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return testing.AllocsPerRun(10, func() {
		var buf bytes.Buffer
		in := NewInterpWithIO(&buf, strings.NewReader(""))
		if err := newVmachine(in).runMain(vprog); err != nil {
			t.Fatalf("vm run: %v", err)
		}
	})
}

// TestVMRepIncClosedForm は単一加算ループの閉形最適化を検証する。
// 1000万回の反復が一括適用で完結すること（値の正確さ＋余裕ある時間内）。
func TestVMRepIncClosedForm(t *testing.T) {
	src := "i = 0\nrepeat 10000000 {\ni += 1\n}\nmes(i)\n"
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
	start := time.Now()
	if err := newVmachine(in).runMain(vprog); err != nil {
		t.Fatalf("vm run: %v", err)
	}
	dur := time.Since(start)
	if buf.String() != "10000000\n" {
		t.Fatalf("got %q want %q", buf.String(), "10000000\n")
	}
	// 閉形でなければ秒単位かかる。1秒は十分に余裕ある上限。
	if dur > time.Second {
		t.Fatalf("too slow (closed form broken?): %v", dur)
	}
	t.Logf("10M-inc loop: %v", dur)
}

func BenchmarkVMNumLoop(b *testing.B) {
	prog := mustParseB(b, benchLoopSrc)
	vprog, err := Compile(prog)
	if err != nil {
		b.Fatalf("compile: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		in := NewInterpWithIO(&buf, strings.NewReader(""))
		if err := newVmachine(in).runMain(vprog); err != nil {
			b.Fatalf("vm run: %v", err)
		}
	}
}

func BenchmarkVMFib(b *testing.B) {
	prog := mustParseB(b, benchFibSrc)
	vprog, err := Compile(prog)
	if err != nil {
		b.Fatalf("compile: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		in := NewInterpWithIO(&buf, strings.NewReader(""))
		if err := newVmachine(in).runMain(vprog); err != nil {
			b.Fatalf("vm run: %v", err)
		}
	}
}
