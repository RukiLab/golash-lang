// コンパイラの構造テスト：命令選択・位置対応・レジスタ割付の検証。
package main

import (
	"testing"
)

func mustCompileSrc(t *testing.T, src string) *VMProgram {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	vprog, err := Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return vprog
}

func hasOp(p *VMProto, op Op) bool {
	for _, ins := range p.Code {
		if o, _, _, _, _ := ins.Decode(); o == op {
			return true
		}
	}
	return false
}

func countOp(p *VMProto, op Op) int {
	n := 0
	for _, ins := range p.Code {
		if o, _, _, _, _ := ins.Decode(); o == op {
			n++
		}
	}
	return n
}

func TestCompilePositions(t *testing.T) {
	// 命令と位置は 1:1 に対応する。
	vprog := mustCompileSrc(t, "x = 1 + 2\nmes(x)\n")
	if len(vprog.Main.Code) != len(vprog.Main.Positions) {
		t.Fatalf("code/positions mismatch: %d vs %d", len(vprog.Main.Code), len(vprog.Main.Positions))
	}
	if len(vprog.Main.Code) == 0 || !hasOp(vprog.Main, OpHalt) {
		t.Fatalf("main must be non-empty and HALT-terminated")
	}
}

func TestCompileCallOrder(t *testing.T) {
	// CKCALL は引数評価・CALL より前に発行される（解決順序の再現）。
	vprog := mustCompileSrc(t, "mes(f(1))\ndef f(a) {\nreturn a\n}\n")
	code := vprog.Main.Code
	ckPos, callPos := -1, -1
	for i, ins := range code {
		op, _, _, _, _ := ins.Decode()
		if op == OpCkCall && ckPos < 0 {
			ckPos = i
		}
		if (op == OpCallF || op == OpCallB) && callPos < 0 {
			callPos = i
		}
	}
	if ckPos < 0 || callPos < 0 || ckPos > callPos {
		t.Fatalf("CKCALL must precede CALL: ck=%d call=%d", ckPos, callPos)
	}
}

func TestCompileAssignDeclares(t *testing.T) {
	// `let` 廃止後：通常代入が宣言を兼ねる（STOREG/STOREN のみ）。
	// DEFG/DEFN は撤去済みのため現れてはならない。
	vprog := mustCompileSrc(t, "x = 1\nmes(x)\n")
	if !hasOp(vprog.Main, OpStoreG) {
		t.Fatalf("top-level assign must use STOREG")
	}
	vprog = mustCompileSrc(t, "x = 1\ndef f() {\nx = 2\nmes(x)\n}\n")
	if len(vprog.Protos) != 1 {
		t.Fatalf("protos = %d, want 1", len(vprog.Protos))
	}
	if !hasOp(vprog.Protos[0], OpStoreN) {
		t.Fatalf("function assign must use STOREN")
	}
}

func TestCompileCompoundSingle(t *testing.T) {
	// 複合代入の添字評価は各1回（GETI/SETI の発行で確認）。
	vprog := mustCompileSrc(t, "a = [1,2]\na[0] += 5\nmes(a[0])\n")
	if !hasOp(vprog.Main, OpSetI) {
		t.Fatalf("compound index assign must use SETI")
	}
}

func TestCompileRepeatShapes(t *testing.T) {
	// 整数・配列の両経路（REPDISP 分岐＋FORILOOP/FORALOOP）が発行される。
	vprog := mustCompileSrc(t, "repeat 3 as i {\nmes(i)\n}\na = [1]\nrepeat a as x {\nmes(x)\n}\n")
	for _, op := range []Op{OpRepDisp, OpForIPrep, OpForILoop, OpForAPrep, OpForALoop, OpPopLoop, OpSaveVar} {
		if !hasOp(vprog.Main, op) {
			t.Fatalf("missing op %s", op)
		}
	}
}

func TestCompileSwitch(t *testing.T) {
	vprog := mustCompileSrc(t, "switch 1 {\ncase 1 {\nmes(1)\n}\ndefault {\nmes(2)\n}\n}\n")
	for _, op := range []Op{OpCkVal, OpEq, OpJmpT, OpJmp} {
		if !hasOp(vprog.Main, op) {
			t.Fatalf("missing op %s", op)
		}
	}
}

func TestCompileFuncProto(t *testing.T) {
	vprog := mustCompileSrc(t, "def f(a, b) {\nreturn a\n}\nmes(f(1, 2))\n")
	if len(vprog.Protos) != 1 {
		t.Fatalf("want 1 proto, got %d", len(vprog.Protos))
	}
	p := vprog.Protos[0]
	if p.Name != "f" || p.NumParams != 2 || len(p.Params) != 2 {
		t.Fatalf("bad proto header: %+v", p)
	}
	if !hasOp(p, OpRet) || !hasOp(vprog.Main, OpFuncDef) {
		t.Fatalf("missing RET/FUNCDEF")
	}
}

func TestCompileRegsBound(t *testing.T) {
	// レジスタ数は 255 以内（8bit operand）。
	vprog := mustCompileSrc(t, "x = 1 + 2 * 3 - 4 / 2\nmes(x)\n")
	if vprog.Main.NumRegs > 0xFF {
		t.Fatalf("too many regs: %d", vprog.Main.NumRegs)
	}
}

func TestDisasmSmoke(t *testing.T) {
	vprog := mustCompileSrc(t, "x = 1\nmes(x)\n")
	out := Disassemble(vprog)
	for _, want := range []string{"== main", "LOADK", "STOREG", "CALLB", "HALT", "-- consts --", "-- names --"} {
		if !containsStr(out, want) {
			t.Fatalf("disasm lacks %q:\n%s", want, out)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
