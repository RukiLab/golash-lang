// Goulash v0.2 バイトコード逆アセンブラ。
//
// `gsh disasm <file.gsh>` が定数プール・レジスタ数・ソース位置付きで出力する。
// GOULASH_TRACE=1 の実行時トレースも同一フォーマッタを使う。
package main

import (
	"fmt"
	"strings"
)

// fmtInstr は1命令を人間可読形式で描画する（トレース用・位置なし）。
// 実行時トレースでは G系の D が addProg により gidx へ書換え済みのため、
// グローバル名で解決し直す。
func (m *vmachine) fmtInstr(proto *VMProto, pc int) string {
	ins := proto.Code[pc]
	op, a, b, _, d := ins.Decode()
	r := func(x uint8) string { return fmt.Sprintf("R%d", x) }
	gname := func(gidx uint32) string {
		if int(gidx) < len(m.globals.names) {
			return m.globals.names[gidx]
		}
		return fmt.Sprintf("G#%d", gidx)
	}
	switch op {
	case OpLoadG:
		return fmt.Sprintf("LOADG %s, G[%s]", r(a), gname(d))
	case OpStoreG:
		return fmt.Sprintf("STOREG %s, G[%s]", r(a), gname(d))
	case OpPutG:
		return fmt.Sprintf("PUTG %s, G[%s]", r(a), gname(d))
	case OpSaveVar:
		if b&SaveIsGlobal != 0 {
			return fmt.Sprintf("SAVEVAR G[%s], flags=%d", gname(d), b)
		}
		return fmt.Sprintf("SAVEVAR S%d, flags=%d", a, b)
	}
	return formatInstr(proto, pc)
}

func formatInstr(proto *VMProto, pc int) string {
	return formatInstrProg(nil, proto, pc)
}

func formatInstrProg(prog *VMProgram, proto *VMProto, pc int) string {
	ins := proto.Code[pc]
	op, a, b, c, d := ins.Decode()
	r := func(x uint8) string { return fmt.Sprintf("R%d", x) }
	name := func(idx uint32) string {
		if int(idx) < len(proto.Names) {
			return proto.Names[idx]
		}
		return fmt.Sprintf("#%d", idx)
	}
	kval := func(idx uint32) string {
		if int(idx) < len(proto.Consts) {
			return valueRepr(proto.Consts[idx])
		}
		return "?"
	}
	switch op {
	case OpMove:
		return fmt.Sprintf("MOVE %s, %s", r(a), r(b))
	case OpLoadK:
		return fmt.Sprintf("LOADK %s, K%d (%s)", r(a), d, kval(d))
	case OpLoadG:
		return fmt.Sprintf("LOADG %s, G[%s]", r(a), name(d))
	case OpStoreG:
		return fmt.Sprintf("STOREG %s, G[%s]", r(a), name(d))
	case OpLoadN:
		return fmt.Sprintf("LOADN %s, N[%s], S%d", r(a), name(d), c)
	case OpStoreN:
		return fmt.Sprintf("STOREN %s, N[%s], S%d", r(a), name(d), c)
	case OpCkVal:
		return fmt.Sprintf("CKVAL %s", r(a))
	case OpTest:
		return fmt.Sprintf("TEST %s", r(a))
	case OpJmp:
		return fmt.Sprintf("JMP %d", pc+1+int(ins.SBx()))
	case OpJmpT:
		return fmt.Sprintf("JMPT %s, %d", r(a), pc+1+int(ins.SBx()))
	case OpJmpF:
		return fmt.Sprintf("JMPF %s, %d", r(a), pc+1+int(ins.SBx()))
	case OpCkInt:
		return fmt.Sprintf("CKINT %s", r(a))
	case OpCkNeg:
		return fmt.Sprintf("CKNEG %s", r(a))
	case OpCkArr:
		return fmt.Sprintf("CKARR %s", r(a))
	case OpCkBnd:
		return fmt.Sprintf("CKBND %s, %s", r(a), r(b))
	case OpGetI:
		return fmt.Sprintf("GETI %s, %s, %s", r(a), r(b), r(c))
	case OpSetI:
		return fmt.Sprintf("SETI %s, %s, %s", r(a), r(b), r(c))
	case OpNewArr:
		if d == 0 {
			return fmt.Sprintf("NEWARR %s (empty)", r(a))
		}
		return fmt.Sprintf("NEWARR %s, %s..%s", r(a), r(b), r(uint8(int(b)+int(d)-1)))
	case OpAppend:
		return fmt.Sprintf("APPEND %s, %s", r(a), r(b))
	case OpAdd, OpSub, OpMul, OpDiv, OpMod,
		OpBitAnd, OpBitOr, OpBitXor, OpShl, OpShr,
		OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
		return fmt.Sprintf("%s %s, %s, %s", op, r(a), r(b), r(c))
	case OpNot, OpNeg, OpBitNot:
		return fmt.Sprintf("%s %s, %s", op, r(a), r(b))
	case OpRepDisp:
		return fmt.Sprintf("REPDISP %s, %d", r(a), pc+1+int(ins.SBx()))
	case OpRepIntErr:
		return "REPITMERR"
	case OpForIPrep:
		return fmt.Sprintf("FORIPREP %s, %s, %d", r(a), regOrNone(b), pc+1+int(ins.SBx()))
	case OpIncChk:
		// word1（名前表 index）は呼出側で解決して表示する。
		return fmt.Sprintf("INCCHK %s, slot=%s, flags=%d, ->%d [+data]", incRhs(proto, a, c), incSlot(b), c, pc+1+int(ins.SBx()))
	case OpRepInc:
		return fmt.Sprintf("REPINC %s, flags=%d, top=%d [+data]", incRhs(proto, a, c), c, pc+1+int(ins.SBx()))
	case OpForAPrep:
		return fmt.Sprintf("FORAPREP %s", r(a))
	case OpSaveVar:
		return fmt.Sprintf("SAVEVAR %s, flags=%d", saveVarName(proto, a, b, d), b)
	case OpForILoop:
		return fmt.Sprintf("FORILOOP %s, %d", r(a), pc+1+int(ins.SBx()))
	case OpForALoop:
		return fmt.Sprintf("FORALOOP %s, %s, %d", regOrNone(a), regOrNone(b), pc+1+int(ins.SBx()))
	case OpPutN:
		return fmt.Sprintf("PUTN S%d, %s", a, r(b))
	case OpPutG:
		return fmt.Sprintf("PUTG %s, G[%s]", r(a), name(d))
	case OpPopLoop:
		return "POPLOOP"
	case OpFuncDef:
		return fmt.Sprintf("FUNCDEF P%d (%s)", d, mprogProtoName(prog, int(d)))
	case OpCkCall:
		return fmt.Sprintf("CKCALL argc=%d, %s", c, name(d))
	case OpCallF:
		return fmt.Sprintf("CALLF %s, args=%s+%d, %s", r(a), r(b), c, name(d))
	case OpCallB:
		return fmt.Sprintf("CALLB %s, args=%s+%d, %s", r(a), r(b), c, builtinNameByID(int(d)))
	case OpCallV:
		s := "?"
		if int(d) < len(proto.Strs) {
			s = proto.Strs[d]
		}
		return fmt.Sprintf("CALLV %s, %q", r(a), s)
	case OpRet:
		if c == 1 {
			return fmt.Sprintf("RET %s", r(a))
		}
		return "RET void"
	case OpBrkTop:
		return "BRKTOP"
	case OpContTop:
		return "CONTTOP"
	case OpRetTop:
		return "RETTOP"
	case OpHalt:
		return "HALT"
	}
	return fmt.Sprintf("OP(%d) %d %d %d %d", uint8(op), a, b, c, d)
}

func regOrNone(x uint8) string {
	if x == NoReg {
		return "-"
	}
	return fmt.Sprintf("R%d", x)
}

func incRhs(proto *VMProto, a, c uint8) string {
	if c&IncRhsReg != 0 {
		return fmt.Sprintf("R%d", a)
	}
	if int(a) < len(proto.Consts) {
		return "K" + fmt.Sprintf("%d(%s)", a, valueRepr(proto.Consts[a]))
	}
	return fmt.Sprintf("K%d", a)
}

func incSlot(b uint8) string {
	if b == NoReg {
		return "G"
	}
	return fmt.Sprintf("S%d", b)
}

// saveVarName は SAVEVAR の退避先を表示する。
// エンコードは A=スロット(NoRegでグローバル)・B=フラグ・D=名前表
// （compile.go saveVar / vm.go OpSaveVar と一致）。
func saveVarName(proto *VMProto, slot, flags uint8, d uint32) string {
	if flags&SaveIsGlobal != 0 {
		if int(d) < len(proto.Names) {
			return "G[" + proto.Names[d] + "]"
		}
		return fmt.Sprintf("G#%d", d)
	}
	return fmt.Sprintf("S%d", slot)
}

func valueRepr(v Value) string {
	switch v.K {
	case KNull:
		return "void"
	case KString:
		return fmt.Sprintf("%q", v.S)
	default:
		return Stringify(v)
	}
}

func builtinNameByID(id int) string {
	ensureBuiltinTable()
	if id >= 0 && id < len(builtinTable) {
		return builtinTable[id].name
	}
	return fmt.Sprintf("builtin#%d", id)
}

func mprogProtoName(prog *VMProgram, idx int) string {
	if prog != nil && idx >= 0 && idx < len(prog.Protos) {
		return prog.Protos[idx].Name
	}
	return fmt.Sprintf("P%d", idx)
}

// Disassemble はプログラム全体をテキスト化する。
func Disassemble(prog *VMProgram) string {
	var sb strings.Builder
	dump := func(p *VMProto) {
		fmt.Fprintf(&sb, "== %s (params=%d slots=%d regs=%d maxargs=%d) ==\n",
			p.Name, p.NumParams, p.NumSlots, p.NumRegs, p.MaxArgs)
		if len(p.Consts) > 0 {
			sb.WriteString("-- consts --\n")
			for i, kv := range p.Consts {
				fmt.Fprintf(&sb, "  K%d = %s\n", i, valueRepr(kv))
			}
		}
		if len(p.Names) > 0 {
			sb.WriteString("-- names --\n")
			for i, n := range p.Names {
				fmt.Fprintf(&sb, "  N%d = %s\n", i, n)
			}
		}
		for pc := 0; pc < len(p.Code); pc++ {
			pos := ""
			if pc < len(p.Positions) {
				pos = p.Positions[pc].String()
			}
			fmt.Fprintf(&sb, "  %4d  %-28s ; %s\n", pc, formatInstrProg(prog, p, pc), pos)
			if op, _, _, _, _ := p.Code[pc].Decode(); op == OpIncChk || op == OpRepInc {
				// word1（名前表 index／REPINC は上位に低速 sBx）を表示して読み飛ばす。
				// 切り詰めバンドル対策：範囲外参照では panic せず表示する。
				if pc+1 >= len(p.Code) {
					pc++
					fmt.Fprintf(&sb, "  %4d  %-28s ; %s\n", pc, "DATA (missing)", "")
					break
				}
				w1 := uint64(p.Code[pc+1])
				ni := uint32(w1)
				name := fmt.Sprintf("N%d", ni)
				if int(ni) < len(p.Names) {
					name = fmt.Sprintf("N[%s]", p.Names[ni])
				}
				if op == OpRepInc {
					slowOff := int32(uint32(w1 >> 32))
					name = fmt.Sprintf("%s slow=%d", name, pc+1+int(slowOff))
				}
				pc++
				dpos := ""
				if pc < len(p.Positions) {
					dpos = p.Positions[pc].String()
				}
				fmt.Fprintf(&sb, "  %4d  %-28s ; %s\n", pc, "DATA "+name, dpos)
			}
		}
	}
	dump(prog.Main)
	for _, p := range prog.Protos {
		dump(p)
	}
	return sb.String()
}
