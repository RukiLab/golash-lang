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
//   - 条件は厳密に bool である。`if 1` はエラー。
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
//   - 代入はその名前を保持する可視スコープを更新し、可視のものが
//     なければ現在のスコープに宣言する（トップレベル: グローバル。
//     関数: ローカルスロット）。VM は格納先を実行時に解決する。
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
// （#include と同じ順序）。存在しないパスはスクリプトディレクトリ基準で
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

// setIndex は境界内に格納する。範囲外はエラー。
// 配列の拡大は（あらかじめ）dim() か（末尾に）push() で行う。
func setIndex(arr *Array, idx int, v Value, at Pos) error {
	if idx < 0 || idx >= len(arr.Elems) {
		return rtErrf(at, "インデックス %d は範囲外です（長さ %d）", idx, len(arr.Elems))
	}
	arr.Elems[idx] = v
	return nil
}

func requireBool(v Value, at Pos) (bool, error) {
	if v.K != KBool {
		return false, rtErrf(at, "条件式は bool 型である必要があります。%s が指定されました", typeNameOf(v))
	}
	return v.B, nil
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
		return Bool(c < 0), nil
	case TokLtEq:
		return Bool(c <= 0), nil
	case TokGt:
		return Bool(c > 0), nil
	case TokGtEq:
		return Bool(c >= 0), nil
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
