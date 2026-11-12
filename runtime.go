// レジスタ VM の実行時コンテキストと共有の値ヘルパー。
package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// 決定事項:
//   - 2つの int の `/` は切り捨て整数除算（Go ライク）。どちらかの
//     オペランドが float なら float になる。`%` は int のみ受け付ける。
//   - 条件は整数である。0 は偽、非 0 は真（`if 1` は常に真）。
//   - `;` はパーサに拒否される。
//   - 配列への書き込みは境界検査される: 確保は dim()、リテラル、
//     push() で行い、範囲外と基底の欠落はエラーで、自動生成は
//     決してされない。
//   - `b = a` は配列を共有する（参照意味論）。
//   - どちらかの辺が文字列の `+` は stringify で連結する（void
//     オペランドは他と同じくエラー）。
//   - int の `+ - *`、単項 `-`、`<<` は桁あふれでラップする
//     （abs(MinInt64) のみエラー）。`-9223372036854775808` は
//     MinInt64 に畳まれる。
//   - NaN と ±Inf は決して値に現れない: 桁あふれする浮動小数点数
//     演算、exp/pow の桁あふれ、float("nan"/"inf")、範囲外の int()
//     変換はすべてエラー。
//   - 複合代入は対象のアドレスをちょうど一度評価する。
//   - トップレベルでは代入がグローバルを更新・宣言する。関数内で新しい
//     ローカルを作るには `local name = 式` が必要。`local` なしの代入は
//     既存のローカル／グローバルを更新し、どちらもなければエラー。
//     VM は格納先を実行時に解決する。
//   - 再帰は maxCallDepth で上限を設け、ソースの入れ子は
//     maxParseDepth で上限を設ける。

// RuntimeError はソース位置を伴う実行時エラー。
type RuntimeError struct {
	File   string
	Line   int
	Column int
	Msg    string
}

func (e *RuntimeError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Msg)
	}
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Msg)
}

func rtErrf(at Pos, format string, args ...any) *RuntimeError {
	return &RuntimeError{File: at.File, Line: at.Line, Column: at.Column, Msg: fmt.Sprintf(format, args...)}
}

// Interp は文（と REPL 入力）をまたいで共有されるグローバル状態を保持する。
type Interp struct {
	be     Backend
	in     io.Reader
	errOut io.Writer
	// スクリプトのゴルーチン上の end() が書き込み、フロントエンドが
	// 実行後に読み取る。スクリプトの途中で閉じられた GUI ウィンドウが
	// 競合しないようアトミックにする。
	exitCode atomic.Pointer[int]
	cliArgs  []string
	// 実行中のスクリプトファイルのディレクトリ（REPL では ""）:
	// ファイル系組込での相対パスの基点。
	scriptDir string
	// vm は実行中の VM（wbind/wsync 用）。runMain/evalOne が設定し、
	// 組込からグローバル変数へ触るために使う。スクリプトスレッドのみ。
	vm *vmachine
	// curFrame/curProto は組込呼出中のフレーム（ローカル変数解決用）。
	// vm.loop の OpCallB が設定・復元する。スクリプトスレッドのみ。
	curFrame *vmFrame
	curProto *VMProto
	// wbinds はウィジェット id から内容を入れる変数名への対応（主要値）。
	// button: pressed(int 0/1) / chkbox: checked(int 0/1) /
	// inputbox/mesbox: text(string) / listbox/combox: sel(int)。
	wbinds map[int]string
	// wbindItems は listbox/combox の項目配列を入れる変数名。
	wbindItems map[int]string
	// wlast は前回同期時の主要値（両方向の変更検出用）。
	wlast map[int]Value
	// wlastItems は前回同期時の項目（配列要素の複製）。
	wlastItems map[int][]string
}

// maxCallDepth はユーザー定義関数の再帰を制限する: 超過はホストの
// スタックオーバーフローではなく実行時エラーになる。
const maxCallDepth = 10000

// NewInterp は os.Stdin から入力を読み、出力へ書き出すインタプリタを
// 生成する。
func NewInterp(out io.Writer) *Interp {
	return NewInterpWithIO(out, os.Stdin)
}

// NewInterpWithIO は明示的な出力/入力ストリームでインタプリタを生成する
// （テストで使用）。
func NewInterpWithIO(out io.Writer, in io.Reader) *Interp {
	return &Interp{be: NewConsoleBackend(out, in), in: in, errOut: os.Stderr}
}

// NewInterpWithBackend は任意の Backend 上にインタプリタを生成する
// （GUI フロントエンドで使用）。
func NewInterpWithBackend(be Backend, in io.Reader) *Interp {
	return &Interp{be: be, in: in, errOut: os.Stderr}
}

// SetArgs は args() 用のコマンドライン引数を格納する（run で使用）。
func (in *Interp) SetArgs(args []string) {
	in.cliArgs = args
}

// SetScriptDir は実行中のスクリプトファイルのディレクトリを記録する
// （run で使用。REPL では空）。ファイル系組込はまずこれに対して
// 相対パスを解決し、次に作業ディレクトリに対して解決する。
func (in *Interp) SetScriptDir(dir string) {
	in.scriptDir = dir
}

// ScriptDir は SetScriptDir が記録したディレクトリを報告する
// （未設定なら ""。例: REPL 内）。
func (in *Interp) ScriptDir() string {
	return in.scriptDir
}

// lookupPath は p をスクリプトディレクトリ、次に作業ディレクトリに対して
// 解決し、最初に存在した一致を返す（無い場合、または入力が空の場合は
// ""）。絶対パスは直接存在を検査する。
func (in *Interp) lookupPath(p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	cands := []string{}
	if in.scriptDir != "" {
		cands = append(cands, filepath.Join(in.scriptDir, p))
	}
	if cwd, err := os.Getwd(); err == nil {
		cands = append(cands, filepath.Join(cwd, p))
	}
	seen := map[string]bool{}
	for _, q := range cands {
		if seen[q] {
			continue
		}
		seen[q] = true
		if _, err := os.Stat(q); err == nil {
			return q
		}
	}
	return ""
}

// resolvePath は組込みのパス引数をマップする: 絶対パスはそのまま通過し、
// 相対パスはスクリプトディレクトリ、次に作業ディレクトリを優先する
// （include と同じ順序）。存在しないパスはスクリプトディレクトリ基準で
// 解決されるため、新しいファイルはスクリプトの隣に作られる
// （REPL 内では指定どおり、つまり作業ディレクトリ基準のまま）。
func (in *Interp) resolvePath(p string) string {
	if q := in.lookupPath(p); q != "" {
		return q
	}
	if p == "" || filepath.IsAbs(p) || in.scriptDir == "" {
		return p
	}
	return filepath.Join(in.scriptDir, p)
}

// ExitCode は end() が要求した終了コードがあればそれを報告する。
func (in *Interp) ExitCode() (int, bool) {
	if p := in.exitCode.Load(); p != nil {
		return *p, true
	}
	return 0, false
}

// Backend は IO バックエンドを公開する（gui タグ付き組込みで使用）。
func (in *Interp) Backend() Backend {
	return in.be
}

// parseVarRef は "name" または "name[idx]" を分解する。
// idx は 10 進整数のみ。余分な文字があれば ok=false。
func parseVarRef(s string) (base string, idx int64, hasIdx bool, ok bool) {
	lb := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '[' {
			lb = i
			break
		}
	}
	if lb < 0 {
		if s == "" {
			return "", 0, false, false
		}
		for i := 0; i < len(s); i++ {
			if s[i] == ']' || s[i] == '[' {
				return "", 0, false, false
			}
		}
		return s, 0, false, true
	}
	if !strings.HasSuffix(s, "]") {
		return "", 0, false, false
	}
	base = s[:lb]
	inner := s[lb+1 : len(s)-1]
	if base == "" || inner == "" {
		return "", 0, false, false
	}
	for i := 0; i < len(inner); i++ {
		if inner[i] < '0' || inner[i] > '9' {
			if !(i == 0 && (inner[i] == '-' || inner[i] == '+')) {
				return "", 0, false, false
			}
		}
	}
	var n int64
	neg := false
	j := 0
	if inner[0] == '-' || inner[0] == '+' {
		neg = inner[0] == '-'
		j = 1
		if len(inner) == 1 {
			return "", 0, false, false
		}
	}
	for ; j < len(inner); j++ {
		n = n*10 + int64(inner[j]-'0')
		if n > 1<<40 {
			return "", 0, false, false
		}
	}
	if neg {
		n = -n
	}
	return base, n, true, true
}

// getBoundVar は wbind 用に変数を読む。現在のフレームのローカルを優先し、
// なければグローバルを見る。"arr[0]" 形式の配列要素も読める。
func (in *Interp) getBoundVar(name string) (Value, bool) {
	base, idx, hasIdx, ok := parseVarRef(name)
	if !ok {
		return Null(), false
	}
	var arrV Value
	var found bool
	if in.vm == nil {
		return Null(), false
	}
	if in.curFrame != nil && in.curProto != nil {
		if s, ok := in.curProto.Slots[base]; ok {
			if s >= 0 && s < len(in.curFrame.regs) && in.curFrame.regs[s].K != KNull {
				arrV, found = in.curFrame.regs[s], true
			}
		}
	}
	if !found {
		gi, ok := in.vm.globals.index[base]
		if !ok || !in.vm.globals.has[gi] {
			return Null(), false
		}
		arrV = in.vm.globals.vals[gi]
		found = true
	}
	if !found {
		return Null(), false
	}
	if !hasIdx {
		return arrV, true
	}
	if arrV.K != KArray {
		return Null(), false
	}
	if idx < 0 || idx >= int64(len(arrV.Arr.Elems)) {
		return Null(), false
	}
	return arrV.Arr.Elems[int(idx)], true
}

// setBoundVar は wbind 用に変数へ書く。"arr[0]" 形式の配列要素にも書ける。
// ローカルが束縛済みならそこへ、なければグローバルへ。
func (in *Interp) setBoundVar(name string, v Value, at Pos) error {
	if err := requireValue(v, at); err != nil {
		return err
	}
	if in.vm == nil {
		return rtErrf(at, "内部エラー：VM が未初期化です")
	}
	base, idx, hasIdx, ok := parseVarRef(name)
	if !ok {
		return rtErrf(at, "変数名 %q は不正です", name)
	}
	if isBuiltin(base) {
		return rtErrf(at, "%q に代入できません：組み込み関数です", base)
	}
	// 配列要素への書込は共有 Array を直接更新する。
	if hasIdx {
		var arrV Value
		var found bool
		var slot int = -1
		var isLocal bool
		if in.curFrame != nil && in.curProto != nil {
			if s, ok := in.curProto.Slots[base]; ok {
				if s >= 0 && s < len(in.curFrame.regs) && in.curFrame.regs[s].K != KNull {
					arrV = in.curFrame.regs[s]
					slot = s
					isLocal = true
					found = true
				}
			}
		}
		var gi int
		if !found {
			var ok bool
			gi, ok = in.vm.globals.index[base]
			if !ok || !in.vm.globals.has[gi] {
				return rtErrf(at, "未定義の変数 %q です", base)
			}
			arrV = in.vm.globals.vals[gi]
		} else {
			_ = slot
		}
		if arrV.K != KArray {
			return rtErrf(at, "%q は配列である必要があります。%s です", base, typeNameOf(arrV))
		}
		if idx < 0 || idx >= int64(len(arrV.Arr.Elems)) {
			return rtErrf(at, "インデックス %d は範囲外です（長さ %d）", idx, len(arrV.Arr.Elems))
		}
		if isLocal {
			if in.curFrame.ro != nil && slot < len(in.curFrame.ro) && in.curFrame.ro[slot] {
				return rtErrf(at, "%q に代入できません：repeat カウンタは読み取り専用です", base)
			}
		} else {
			if in.vm.globals.ro[gi] {
				return rtErrf(at, "%q に代入できません：repeat カウンタは読み取り専用です", base)
			}
		}
		arrV.Arr.Elems[int(idx)] = v
		return nil
	}
	if in.curFrame != nil && in.curProto != nil {
		if s, ok := in.curProto.Slots[base]; ok {
			if s >= 0 && s < len(in.curFrame.regs) && in.curFrame.regs[s].K != KNull {
				if in.curFrame.ro != nil && s < len(in.curFrame.ro) && in.curFrame.ro[s] {
					return rtErrf(at, "%q に代入できません：repeat カウンタは読み取り専用です", base)
				}
				in.curFrame.regs[s] = v
				return nil
			}
		}
	}
	gi, ok := in.vm.globals.index[base]
	if !ok || !in.vm.globals.has[gi] {
		return rtErrf(at, "未定義の変数 %q です", base)
	}
	if in.vm.globals.ro[gi] {
		return rtErrf(at, "%q に代入できません：repeat カウンタは読み取り専用です", base)
	}
	in.vm.globals.vals[gi] = v
	return nil
}

// setIndex は境界内に格納する。範囲外はエラー。
// 配列の拡大は（あらかじめ）dim() か（末尾に）push() で行う。
func setIndex(arr *Array, idx int, v Value, at Pos) error {
	if idx < 0 || idx >= len(arr.Elems) {
		return rtErrf(at, "インデックス %d は範囲外です（長さ %d）", idx, len(arr.Elems))
	}
	arr.Elems[idx] = v
	return nil
}

// truthy は条件式の整数値を真偽に解釈する（0=false、非0=true）。
func truthy(v Value, at Pos) (bool, error) {
	if v.K != KInt {
		return false, rtErrf(at, "条件式は整数（0=false、非0=true）である必要があります。%s が指定されました", typeNameOf(v))
	}
	return v.I != 0, nil
}

// needTruthy は組込みの真偽値引数を整数（0=false、非0=true）として読む。
func needTruthy(name string, args []Value, i int, at Pos) (bool, error) {
	if args[i].K != KInt {
		return false, argErr(name, i, at, "整数（0=false、非0=true）である必要があります。%s が指定されました", typeNameOf(args[i]))
	}
	return args[i].I != 0, nil
}

// requireValue は void を拒否する: 内部的な null がスクリプトに
// 使える値として現れてはならない（代入/引数/オペランドはここで
// 失敗する。mes() と print() は代わりに void を無音でスキップする）。
func requireValue(v Value, at Pos) error {
	if v.K == KNull {
		return rtErrf(at, "void値を使用できません")
	}
	return nil
}

func isNum(v Value) bool { return v.K == KInt || v.K == KFloat }
func toFloat(v Value) float64 {
	if v.K == KFloat {
		return v.F
	}
	return float64(v.I)
}

func compare(op TokenType, l, r Value, at Pos) (Value, error) {
	var c int // -1, 0, +1
	switch {
	case isNum(l) && isNum(r):
		lf, rf := toFloat(l), toFloat(r)
		switch {
		case lf < rf:
			c = -1
		case lf > rf:
			c = 1
		}
	case l.K == KString && r.K == KString:
		c = strings.Compare(l.S, r.S)
	default:
		return Null(), rtErrf(at, "%s と %s を比較できません（< <= > >= は数値または文字列のみ対応しています）", typeNameOf(l), typeNameOf(r))
	}
	switch op {
	case TokLt:
		return IntOfBool(c < 0), nil
	case TokLtEq:
		return IntOfBool(c <= 0), nil
	case TokGt:
		return IntOfBool(c > 0), nil
	case TokGtEq:
		return IntOfBool(c >= 0), nil
	}
	return Null(), rtErrf(at, "内部エラー：不正な比較演算子です")
}

// finiteFloat は有限でない浮動小数点数の結果を拒否する: NaN と ±Inf は
// 決して値に現れてはならない（リテラル、除算、数学組込みすべてが
// それを拒否する）ため、それを生成することは常にエラーであり、値では
// 決してない。
func finiteFloat(f float64, at Pos) (Value, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return Null(), rtErrf(at, "浮動小数の計算結果が有限ではありません（±Inf・NaN は使用できません）")
	}
	return Float(f), nil
}

func add(l, r Value, at Pos) (Value, error) {
	if err := requireValue(l, at); err != nil {
		return Null(), err
	}
	if err := requireValue(r, at); err != nil {
		return Null(), err
	}
	if l.K == KString || r.K == KString {
		return Str(Stringify(l) + Stringify(r)), nil
	}
	if isNum(l) && isNum(r) {
		if l.K == KFloat || r.K == KFloat {
			return finiteFloat(toFloat(l)+toFloat(r), at)
		}
		return Int(l.I + r.I), nil
	}
	return Null(), rtErrf(at, "%s と %s を加算できません", typeNameOf(l), typeNameOf(r))
}

func arith(op TokenType, l, r Value, at Pos) (Value, error) {
	if !isNum(l) || !isNum(r) {
		return Null(), rtErrf(at, "演算子 %s は数値が必要です。%s と %s が指定されました", opSymbol(op), typeNameOf(l), typeNameOf(r))
	}
	if op == TokMod {
		if l.K != KInt || r.K != KInt {
			return Null(), rtErrf(at, "演算子 %% は整数が必要です。%s と %s が指定されました", typeNameOf(l), typeNameOf(r))
		}
		if r.I == 0 {
			return Null(), rtErrf(at, "0 による剰余演算です")
		}
		return Int(l.I % r.I), nil
	}
	if l.K == KFloat || r.K == KFloat {
		lf, rf := toFloat(l), toFloat(r)
		switch op {
		case TokMinus:
			return finiteFloat(lf-rf, at)
		case TokStar:
			return finiteFloat(lf*rf, at)
		case TokSlash:
			if rf == 0 {
				return Null(), rtErrf(at, "0 による除算です")
			}
			return finiteFloat(lf/rf, at)
		}
	}
	switch op {
	case TokMinus:
		return Int(l.I - r.I), nil
	case TokStar:
		return Int(l.I * r.I), nil
	case TokSlash:
		if r.I == 0 {
			return Null(), rtErrf(at, "0 による除算です")
		}
		return Int(l.I / r.I), nil
	}
	return Null(), rtErrf(at, "内部エラー：不正な算術演算子です")
}

// bitwise は & | ^ を実装する（% と同じく整数のみ）。
func bitwise(op TokenType, l, r Value, at Pos) (Value, error) {
	if l.K != KInt || r.K != KInt {
		return Null(), rtErrf(at, "演算子 %s は整数が必要です。%s と %s が指定されました", opSymbol(op), typeNameOf(l), typeNameOf(r))
	}
	switch op {
	case TokBitAnd:
		return Int(l.I & r.I), nil
	case TokBitOr:
		return Int(l.I | r.I), nil
	case TokBitXor:
		return Int(l.I ^ r.I), nil
	}
	return Null(), rtErrf(at, "内部エラー：不正なビット演算子です")
}

// shift は << >> を実装する（整数のみ。算術右シフト）。
func shift(op TokenType, l, r Value, at Pos) (Value, error) {
	if l.K != KInt || r.K != KInt {
		return Null(), rtErrf(at, "演算子 %s は整数が必要です。%s と %s が指定されました", opSymbol(op), typeNameOf(l), typeNameOf(r))
	}
	if r.I < 0 || r.I > 63 {
		return Null(), rtErrf(at, "シフト数は 0 から 63 の範囲で指定してください。%d が指定されました", r.I)
	}
	if op == TokShl {
		return Int(l.I << uint(r.I)), nil
	}
	return Int(l.I >> uint(r.I)), nil
}
