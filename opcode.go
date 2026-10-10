// Goulash v0.2 レジスタ型VMの命令セット。
//
// 設計方針（詳細は docs/DESIGN_VM.md）：
//   - 命令長は 64bit 固定。op(8bit) A(8bit) B(8bit) C(8bit) D(32bit)。
//     D は用途別に符号なし Bx（定数・名前・プロトタイプ・組込ID・個数）
//     または符号付き sBx（ジャンプオフセット。pc からの相対）として解釈する。
//   - 各命令は高々1つのソース位置（Positions[pc]）だけを持つ。
//     runtime.go で複数位置を使い分ける検査（例：== の左右の void 検査、
//     代入 RHS の void 検査位置）は、コンパイラが CKVAL 等の検査命令を
//     正しい位置付きで先行発行することで再現する。VM 側に位置テーブルは不要。
//   - 純粋な値演算（add/arith/bitwise/shift/compare 等）は runtime.go の
//     同名関数をそのまま呼び出す。文言・条件の一致は共有によって保証する。
package main

import "fmt"

// Op は命令コード（8bit に収まること）。
//
// OpIncChk / OpRepInc は2ワード命令である。
// INCCHK: word0 が [op|A=右辺(reg/const) B=スロット(NoRegでグローバル)
//
//	C=フラグ D=sBx(低速)]、word1 が名前表 index
//	（グローバル形は addProg で gidx へ書換え）。
//	C bit0=sub(1)/add(0)、bit1=右辺reg(1)/const(0)。
//	低速スタブはプロトタイプ末尾に置き、高速はフォールスルーする。
//
// REPINC: 本体が単一のグローバル定数INCCHKである計数ループの融合。
//
//	word0 が [op|A=定数index C=subフラグ D=sBx(topへ)]、
//	word1 が [上位32bit=sBx(低速) 下位32bit=名前表index→gidx]。
//	idx++ して終了ならフォールスルー（end の POPLOOP へ）、
//	継続なら加算（低速はスタブへ）して top（＝自身）へ戻る。
type Op uint8

// IncChk フラグ（OpIncChk の C オペランド）。
const (
	IncSub      = 1 << iota // 減算（なければ加算）
	IncRhsReg               // 右辺がレジスタ（なければ定数）
	IncConstInt             // 定数形かつ右辺が int（タグ検査省略可）
)

const (
	OpMove      Op = iota // MOVE A,B        R[A] = R[B]
	OpLoadK               // LOADK A,Bx      R[A] = Consts[Bx]
	OpLoadG               // LOADG A,Bx      R[A] = global[Names[Bx]]
	OpStoreG              // STOREG A,Bx     global[Names[Bx]] = R[A]
	OpLoadN               // LOADN A,Bx,C    動的読出（C:スロット、voidならグローバル）
	OpStoreN              // STOREN A,Bx,C   動的書込（C:スロット、voidならグローバル）
	OpCkVal               // CKVAL A         void 検査
	OpTest                // TEST A          requireBool（値は保持）
	OpJmp                 // JMP sBx         無条件ジャンプ
	OpJmpT                // JMPT A,sBx      R[A].B == true でジャンプ
	OpJmpF                // JMPF A,sBx      R[A].B == false でジャンプ
	OpCkInt               // CKINT A         整数検査（添字用）
	OpCkNeg               // CKNEG A         非負検査（添字用）
	OpCkArr               // CKARR A         配列検査（書込基底用）
	OpCkBnd               // CKBND A,B       R[A] が R[B] の範囲内か（負数も範囲外扱い）
	OpGetI                // GETI A,B,C      R[A] = R[B][R[C]]（全検査内蔵）
	OpSetI                // SETI A,B,C      R[A][R[B]] = R[C]（全検査内蔵）
	OpNewArr              // NEWARR A,B,D    R[A] = 新配列（R[B:B+D] を複写）
	OpAppend              // APPEND A,B      R[A] に R[B] を追加（配列リテラルの逐次構築用）
	OpAdd                 // ADD A,B,C       add()
	OpSub                 // SUB A,B,C       arith(-)
	OpMul                 // MUL A,B,C       arith(*)
	OpDiv                 // DIV A,B,C       arith(/)
	OpMod                 // MOD A,B,C       arith(%)
	OpBitAnd              // BAND A,B,C      bitwise(&)
	OpBitOr               // BOR A,B,C       bitwise(|)
	OpBitXor              // BXOR A,B,C      bitwise(^)
	OpShl                 // SHL A,B,C       shift(<<)
	OpShr                 // SHR A,B,C       shift(>>)
	OpEq                  // EQ A,B,C        ==（void は事前 CKVAL、前提は値）
	OpNe                  // NE A,B,C        !=（同上）
	OpLt                  // LT A,B,C        compare(<)
	OpLe                  // LE A,B,C        compare(<=)
	OpGt                  // GT A,B,C        compare(>)
	OpGe                  // GE A,B,C        compare(>=)
	OpNot                 // NOT A,B         !（requireBool 内蔵）
	OpNeg                 // NEG A,B         単項 -（型分岐内蔵、void は負数化エラー）
	OpBitNot              // BITNOT A,B      ~（整数のみ）
	OpRepDisp             // REPDISP A,sBx   配列ならジャンプ・整数なら継続・他はエラー
	OpRepIntErr           // REPITMERR       整数 repeat の as i,x 形エラー（無条件）
	OpForIPrep            // FORIPREP A      整数ループ準備（負数検査・制御 push）
	OpForAPrep            // FORAPREP A      配列ループ準備（スナップショット・制御 push）
	OpSaveVar             // SAVEVAR A,B,D   ループ変数の退避（A:スロット/NoRegでグローバル B:フラグ D:名前）
	OpForILoop            // FORILOOP A,sBx  整数ループ継続判定（R[A] = 現在値）
	OpForALoop            // FORALOOP A,B,sBx 配列ループ継続判定（R[A]=添字 R[B]=要素）
	OpPutN                // PUTN A,B        slot[A] = R[B]（検査なし・ループ変数書込用）
	OpPutG                // PUTG A,D        global[Names[D]] = R[A]（検査なし）
	OpPopLoop             // POPLOOP         ループ制御 pop＋束縛復元
	OpFuncDef             // FUNCDEF Bx      関数登録（実行時検査つき）
	OpCkCall              // CKCALL C,D      呼出の事前解決（C:引数個数 D:名前）
	OpIncChk              // INCCHK 2ワード複合代入(+/-)融合（低速時は sBx へ）
	OpRepInc              // REPINC 2ワード計数ループ融合（本体が単一INCCHK時に置換）
	OpCallF               // CALLF A,B,C,D   ユーザ関数（A:戻先 B:引数基底 C:個数 D:名前）
	OpCallB               // CALLB A,B,C,D   組込関数（A:戻先 B:引数基底 C:個数 D:組込ID）
	OpCallV               // CALLV A,D       非変数 callee 呼出（常にエラー、A:callee D:表記）
	OpRet                 // RET A,B         復帰（B=1:R[A]返却 B=0:void返却）
	OpBrkTop              // BRKTOP          トップレベルの break エラー
	OpContTop             // CONTTOP         トップレベルの continue エラー
	OpRetTop              // RETTOP          トップレベルの return エラー
	OpHalt // HALT 実行終了

	OpCount // 命令数（番兵）
)

// SaveVar フラグ（OpSaveVar の B オペランド）。
const (
	SaveIsGlobal = 1 << iota // 束縛先がグローバル（B は未使用）
	SaveReadonly             // ループ期間中 readonly 化（カウンタ・添字用）
	SaveInitZero             // Int(0) で初期化（カウンタ・添字用。要素用は付けない）
)

// NoReg は「レジスタなし」を表す番兵（FORALOOP の不要な R[A]/R[B] 用）。
const NoReg = 0xFF

// Instr は 64bit 固定長命令。
// ビット配置: [op:8][A:8][B:8][C:8][D:32]
type Instr uint64

// EncodeInstr は命令ワードを組み立てる。
func EncodeInstr(op Op, a, b, c uint8, d uint32) Instr {
	return Instr(uint64(op)<<56 | uint64(a)<<48 | uint64(b)<<40 | uint64(c)<<32 | uint64(d))
}

// Decode は各フィールドを取り出す。
func (in Instr) Decode() (op Op, a, b, c uint8, d uint32) {
	return Op(in >> 56), uint8(in >> 48), uint8(in >> 40), uint8(in >> 32), uint32(in)
}

// Bx は D を符号なしとして読む。
func (in Instr) Bx() uint32 {
	return uint32(in)
}

// SBx は D を符号付きジャンプオフセットとして読む。
func (in Instr) SBx() int32 {
	return int32(uint32(in))
}

// EncodeSBx は符号付きオフセットを D 用に変換する。
func EncodeSBx(off int) uint32 {
	return uint32(int32(off))
}

func (op Op) String() string {
	switch op {
	case OpMove:
		return "MOVE"
	case OpLoadK:
		return "LOADK"
	case OpLoadG:
		return "LOADG"
	case OpStoreG:
		return "STOREG"
	case OpLoadN:
		return "LOADN"
	case OpStoreN:
		return "STOREN"
	case OpCkVal:
		return "CKVAL"
	case OpTest:
		return "TEST"
	case OpJmp:
		return "JMP"
	case OpJmpT:
		return "JMPT"
	case OpJmpF:
		return "JMPF"
	case OpCkInt:
		return "CKINT"
	case OpCkNeg:
		return "CKNEG"
	case OpCkArr:
		return "CKARR"
	case OpCkBnd:
		return "CKBND"
	case OpGetI:
		return "GETI"
	case OpSetI:
		return "SETI"
	case OpNewArr:
		return "NEWARR"
	case OpAppend:
		return "APPEND"
	case OpAdd:
		return "ADD"
	case OpSub:
		return "SUB"
	case OpMul:
		return "MUL"
	case OpDiv:
		return "DIV"
	case OpMod:
		return "MOD"
	case OpBitAnd:
		return "BAND"
	case OpBitOr:
		return "BOR"
	case OpBitXor:
		return "BXOR"
	case OpShl:
		return "SHL"
	case OpShr:
		return "SHR"
	case OpEq:
		return "EQ"
	case OpNe:
		return "NE"
	case OpLt:
		return "LT"
	case OpLe:
		return "LE"
	case OpGt:
		return "GT"
	case OpGe:
		return "GE"
	case OpNot:
		return "NOT"
	case OpNeg:
		return "NEG"
	case OpBitNot:
		return "BITNOT"
	case OpRepDisp:
		return "REPDISP"
	case OpRepIntErr:
		return "REPITMERR"
	case OpForIPrep:
		return "FORIPREP"
	case OpForAPrep:
		return "FORAPREP"
	case OpSaveVar:
		return "SAVEVAR"
	case OpForILoop:
		return "FORILOOP"
	case OpForALoop:
		return "FORALOOP"
	case OpPutN:
		return "PUTN"
	case OpPutG:
		return "PUTG"
	case OpPopLoop:
		return "POPLOOP"
	case OpFuncDef:
		return "FUNCDEF"
	case OpCkCall:
		return "CKCALL"
	case OpIncChk:
		return "INCCHK"
	case OpRepInc:
		return "REPINC"
	case OpCallF:
		return "CALLF"
	case OpCallB:
		return "CALLB"
	case OpCallV:
		return "CALLV"
	case OpRet:
		return "RET"
	case OpBrkTop:
		return "BRKTOP"
	case OpContTop:
		return "CONTTOP"
	case OpRetTop:
		return "RETTOP"
	case OpHalt:
		return "HALT"
	}
	return fmt.Sprintf("OP(%d)", uint8(op))
}
