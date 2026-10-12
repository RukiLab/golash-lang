// Runtime context and shared value helpers for the register VM.
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

// Decisions:
//   - `/` on two ints is truncating integer division (Go-like); any float
//     operand yields a float. `%` accepts ints only.
//   - Conditions are strictly bool; `if 1` is an error.
//   - `;` is rejected by the parser.
//   - Array writes are bounds-checked: allocate with dim(), a literal,
//     or push(); out-of-range and missing bases are errors, never
//     auto-created.
//   - `b = a` shares the array (reference semantics).
//   - `+` with either side a string concatenates via stringify (void
//     operands are an error, like everywhere else).
//   - Int `+ - *`, unary `-`, and `<<` wrap on overflow (only
//     abs(MinInt64) is an error); `-9223372036854775808` folds to MinInt64.
//   - NaN and ±Inf never appear in values: overflowing float arithmetic,
//     exp/pow overflow, float("nan"/"inf"), and out-of-range int()
//     conversions are all errors.
//   - Compound assignment evaluates its target address exactly once.
//   - Assignment updates the visible scope holding the name, or declares
//     it in the current scope when nothing visible exists (top-level:
//     global; function: local slot). The VM resolves the store target
//     at runtime.
//   - Recursion is capped at maxCallDepth; source nesting at maxParseDepth.

// RuntimeError is an execution error with source position.
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

// Interp holds global state shared across statements (and REPL inputs).
type Interp struct {
	be     Backend
	in     io.Reader
	errOut io.Writer
	// Written by end() on the script goroutine, read by the frontend
	// after the run; atomic so a GUI window closed mid-script cannot race.
	exitCode atomic.Pointer[int]
	cliArgs  []string
	// Directory of the running script file ("" in the REPL): base
	// for relative paths in file builtins.
	scriptDir string
}

// maxCallDepth caps user-function recursion: exceeding it is a runtime
// error instead of a host stack overflow.
const maxCallDepth = 10000

// NewInterp creates an interpreter writing output to out and reading input
// from os.Stdin.
func NewInterp(out io.Writer) *Interp {
	return NewInterpWithIO(out, os.Stdin)
}

// NewInterpWithIO creates an interpreter with explicit output/input streams
// (used by tests).
func NewInterpWithIO(out io.Writer, in io.Reader) *Interp {
	return &Interp{be: NewConsoleBackend(out, in), in: in, errOut: os.Stderr}
}

// NewInterpWithBackend creates an interpreter over an arbitrary Backend
// (used by the GUI frontend).
func NewInterpWithBackend(be Backend, in io.Reader) *Interp {
	return &Interp{be: be, in: in, errOut: os.Stderr}
}

// SetArgs stores command-line arguments for args() (used by run).
func (in *Interp) SetArgs(args []string) {
	in.cliArgs = args
}

// SetScriptDir records the directory of the running script file (used
// by run; empty in the REPL). File builtins resolve relative paths
// against it first, then the working directory.
func (in *Interp) SetScriptDir(dir string) {
	in.scriptDir = dir
}

// ScriptDir reports the directory recorded by SetScriptDir ("" when
// unset, e.g. in the REPL).
func (in *Interp) ScriptDir() string {
	return in.scriptDir
}

// lookupPath resolves p against the script directory, then the working
// directory, returning the first existing match ("" when none, or for
// empty input). Absolute paths check existence directly.
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

// resolvePath maps a builtin path argument: absolute paths pass
// through; relative paths prefer the script directory, then the
// working directory (same order as #include). Missing paths resolve
// against the script directory so new files land beside the script
// (in the REPL they stay as-given, i.e. working-directory relative).
func (in *Interp) resolvePath(p string) string {
	if q := in.lookupPath(p); q != "" {
		return q
	}
	if p == "" || filepath.IsAbs(p) || in.scriptDir == "" {
		return p
	}
	return filepath.Join(in.scriptDir, p)
}

// ExitCode reports the code requested by end(), if any.
func (in *Interp) ExitCode() (int, bool) {
	if p := in.exitCode.Load(); p != nil {
		return *p, true
	}
	return 0, false
}

// Backend exposes the IO backend (used by gui-tag builtins).
func (in *Interp) Backend() Backend {
	return in.be
}

// setIndex stores within bounds; out-of-range is an error.
// Grow arrays with dim() (up front) or push() (at the tail).
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

// requireValue rejects void: internal null must never surface to
// scripts as a usable value (assign/args/operands fail here; mes()
// and print() silently skip void instead).
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

// finiteFloat rejects non-finite float results: NaN and ±Inf can never
// appear in a value (literals, division, and math builtins all refuse
// them), so producing one is always an error, never a value.
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

// bitwise implements & | ^ (integers only, like %).
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

// shift implements << >> (integers only; arithmetic right shift).
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
