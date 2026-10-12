// Golash レジスタVMのプログラム表現。
//
// ast.Program（構文木）との混同を避けるため、バイトコード側は
// VMProgram / VMProto と名付ける。ソース位置は ast.Pos をそのまま使い
// （file:line:col の表示形式・精度を共有する）、命令と 1:1 に対応させる。
package main

// VMProto は1関数（またはトップレベル）分のバイトコード。
type VMProto struct {
	Name      string         // 関数名（トップレベルは "main"）
	Params    []string       // 仮引数名（FUNCDEF 時の重複検査用）
	ParamPos  []Pos          // 仮引数の位置（重複エラーの位置用）
	NumParams int            // 引数スロット数（Params と同数）
	NumSlots  int            // 関数内スロット総数（引数＋割当済ローカル）。main は 0
	Slots     map[string]int // 名前→スロット（CkCall 防御経路用。main は nil）
	NumRegs   int            // 必要レジスタ総数（スロット＋式評価用一時）
	MaxArgs   int            // CALLB 用に切り出す最大引数数（フレーム毎バッファ確保用）
	Code      []Instr        // 命令列
	Consts    []Value        // 定数プール（LOADK 用。void も可、配列は不可）
	Names     []string       // 名前表（LOADG/STOREG/LOADN/STOREN/PUTG/SAVEVAR/CALLF 用）
	Strs      []string       // CALLV 用 callee 表記表
	Positions []Pos          // 命令と 1:1 のエラー位置
}

// VMProgram は実行単位。Main は必ず index 0 ではなく Main フィールドに置く。
// Protos は DefStmt の出現順に並び、FUNCDEF Bx はこの Bx を登録する。
type VMProgram struct {
	Main   *VMProto
	Protos []*VMProto
}

// builtinRef は解決済み組込関数の参照（CALLB の D operand に対応）。
type builtinRef struct {
	name    string
	minArgs int
	maxArgs int
	fn      builtinFn
}

// builtinTable は名前順に安定化した組込 ID 表。コンパイラが名前→ID を引き、
// VM が ID→実体を引く。builtins マップ自体は builtin.go のものを使う。
// init 順序（各ファイルの init 実行順）に依存しないよう遅延初期化する。
var builtinTable []builtinRef
var builtinIDByName map[string]int

func ensureBuiltinTable() {
	if builtinIDByName != nil {
		return
	}
	names := builtinNames() // sort 済み
	builtinTable = make([]builtinRef, len(names))
	builtinIDByName = make(map[string]int, len(names))
	for i, n := range names {
		d := builtins[n]
		builtinTable[i] = builtinRef{name: n, minArgs: d.minArgs, maxArgs: d.maxArgs, fn: d.fn}
		builtinIDByName[n] = i
	}
}

// lookupBuiltinID は組込名から ID を返す（未登録なら -1）。
func lookupBuiltinID(name string) int {
	ensureBuiltinTable()
	if id, ok := builtinIDByName[name]; ok {
		return id
	}
	return -1
}

// callBuiltinByID は arity 検査＋呼出を行う。文言は callBuiltin と同一にする
// ため、検査部分は callBuiltin と同じ式で組み立てる（将来の変更時は両方更新）。
func callBuiltinByID(id int, in *Interp, args []Value, at Pos) (Value, error) {
	ensureBuiltinTable()
	if id < 0 || id >= len(builtinTable) {
		return Null(), rtErrf(at, "内部エラー：不正な組込IDです")
	}
	b := builtinTable[id]
	if len(args) < b.minArgs || (b.maxArgs >= 0 && len(args) > b.maxArgs) {
		if b.minArgs == b.maxArgs {
			return Null(), rtErrf(at, "%s は引数を %d 個必要としますが、%d 個が渡されました", b.name, b.minArgs, len(args))
		}
		if b.maxArgs < 0 {
			return Null(), rtErrf(at, "%s は少なくとも %d 個の引数が必要ですが、%d 個が渡されました", b.name, b.minArgs, len(args))
		}
		return Null(), rtErrf(at, "%s は %d から %d 個の引数が必要ですが、%d 個が渡されました", b.name, b.minArgs, b.maxArgs, len(args))
	}
	return b.fn(in, args, at)
}
