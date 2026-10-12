// Preprocessor (#include, #define, conditionals) for Golash.
//
// A directive is a whole line whose first non-space character is `#`.
// Supported directives (minimal set):
//
//	#mode cli|gui  run mode for `gsh run` (last active one wins;
//	omitted means gui)
//
//	#include "path"   splice a file (once-semantics, circular = error)
//	#define NAME [lit] constant: one int/float/string/bool literal
//	                  (optional leading `-` for numbers); omitted lit
//	                  auto-numbers from a counter (explicit ints
//	                  reset it, other literals leave it alone)
//	#ifdef NAME / #ifndef NAME / #else / #endif
//	                  conditional lines (nestable)
//	#error message    fail with message when reached
//
// Search order: the including file's directory, then the working
// directory; absolute paths are used as-is. Tokens keep file-local
// line numbers and carry the display path in Token.File. Directive and
// inactive lines are blanked (never deleted) so every line number stays
// intact. #define applies textually in stream order (include-expansion
// order): a use sees defines from earlier stream positions only —
// earlier lines of the same file, defines from before the #include
// (which libraries also see), and names a library defined (visible to
// lines after its #include in the parent and to later siblings).
// A define after the #include never reaches the already-expanded
// library. Redefinition across the tree is still an error. Valueless
// #define auto-numbers are assigned in stream order, sharing one
// counter per run.
//
// NOTE: directives are recognized before comments, so a `#` at a line
// start inside a block comment still counts as a directive.
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CombineFile reads entry and splices its #include tree into one token
// stream (ending with EOF). Display paths stay relative when possible.
func CombineFile(entry string) ([]Token, error) {
	toks, _, err := CombineFileMode(entry)
	return toks, err
}

// CombineFileMode is CombineFile plus the run mode: the last active
// `#mode cli|gui` in the tree, or "gui" when absent.
func CombineFileMode(entry string) ([]Token, string, error) {
	st := newIncluder()
	toks, err := st.combineFile(entry, "")
	if err != nil {
		return nil, "gui", err
	}
	return toks, st.runMode(), nil
}

// CombineSource splices directives found in src (a REPL input or test
// snippet). display names the pseudo-file in errors ("" keeps the legacy
// position format); parentDir anchors relative targets. NOTE: each call
// starts with fresh preprocessor state, so REPL #defines live for one
// input only.
func CombineSource(display, src, parentDir string) ([]Token, error) {
	st := newIncluder()
	return st.combineSource(display, src, parentDir)
}

type includer struct {
	done  map[string]bool // absolute paths already spliced (once)
	stack []string        // absolute paths on the current chain (cycles)
	// Preprocessor state, shared across the whole include tree in
	// stream order: defines flow into included files (a library sees
	// defines from before its #include) and back out to later lines
	// (a use after the #include sees library defines); see
	// combineSource for the exact visibility rules.
	defines map[string]ppDefine
	// Auto-number counter for valueless #define (starts at 0 per run).
	seq   int64
	conds []ppFrame
	// Run mode: last active `#mode` argument ("cli"/"gui", "" unset).
	mode string
}

// runMode reports the effective mode ("gui" when unset).
func (st *includer) runMode() string {
	if st.mode == "cli" {
		return "cli"
	}
	return "gui"
}

// ppDefine is a #define constant: one literal token.
type ppDefine struct {
	typ TokenType // TokInt, TokFloat, TokString, TokTrue, TokFalse
	lit string
}

// ppFrame is one open #ifdef/#ifndef level.
type ppFrame struct {
	parentActive bool
	active       bool // this branch active (parentActive && taken-branch)
	taken        bool // the #if branch was taken (drives #else)
	elseSeen     bool
}

func newIncluder() *includer {
	return &includer{done: map[string]bool{}, defines: map[string]ppDefine{}}
}

// curActive reports whether the current line is in live code.
func (st *includer) curActive() bool {
	if len(st.conds) == 0 {
		return true
	}
	return st.conds[len(st.conds)-1].active
}

func (st *includer) combineFile(path, parentDir string) ([]Token, error) {
	found, err := resolveInclude(path, parentDir)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(found)
	if err != nil {
		return nil, fmt.Errorf("%q を解決できません：%s", path, err.Error())
	}
	for _, p := range st.stack {
		if p == abs {
			chain := append(append([]string{}, st.stack...), abs)
			for i := range chain {
				chain[i] = displayPath(chain[i])
			}
			return nil, fmt.Errorf("循環する #include です：%s", strings.Join(chain, " -> "))
		}
	}
	if st.done[abs] {
		return nil, nil // once-semantics: shared libraries splice once
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("#include %q を開けません：%s", path, err.Error())
	}
	st.stack = append(st.stack, abs)
	toks, err := st.combineSource(displayPath(abs), string(data), filepath.Dir(abs))
	st.stack = st.stack[:len(st.stack)-1]
	if err != nil {
		return nil, err
	}
	st.done[abs] = true
	return toks, nil
}

// ownDef is a validated #define with its 1-based line, applied to the
// shared table in stream order (see combineSource).
type ownDef struct {
	line int
	name string
	def  ppDefine
}

// childEv is one library contribution: defines a child file added,
// taking effect at the #include line (upward sharing).
type childEv struct {
	line int
	adds map[string]ppDefine
}

// combineSource runs the preprocessor line pass (conditionals, #define
// collection, #error), lexes the live lines, substitutes #define uses,
// and splices each #include's child tokens at its line, separated by a
// newline so statements never merge across files.
func (st *includer) combineSource(display, src, parentDir string) ([]Token, error) {
	lines := strings.Split(src, "\n")
	condDepth := len(st.conds) // our frames; a file must not pop its parent's
	dirs := make([]*directive, len(lines))
	active := make([]bool, len(lines))
	blanked := make([]string, len(lines))
	// base is the define set inherited at file entry (for own-token
	// substitution). live tracks base plus this file's defines and
	// child contributions in stream order (for #ifdef checks and
	// redefinition detection).
	base := make(map[string]ppDefine, len(st.defines))
	for k, v := range st.defines {
		base[k] = v
	}
	live := make(map[string]ppDefine, len(base))
	for k, v := range base {
		live[k] = v
	}
	var own []ownDef
	// pubIdx counts own entries already published to the shared table.
	// Phase 1 publishes early ones before recursing into includes so
	// children see exactly the earlier defines; phase 3 publishes the
	// rest in the same sweep order.
	pubIdx := 0
	publishUntil := func(line int) error {
		for pubIdx < len(own) && own[pubIdx].line < line {
			o := own[pubIdx]
			if _, ok := st.defines[o.name]; ok {
				return &LexError{File: display, Line: o.line, Column: 1, Msg: fmt.Sprintf("#define %q は既に定義されています", o.name)}
			}
			st.defines[o.name] = o.def
			pubIdx++
		}
		return nil
	}
	// chevs records library contributions in sweep order; incToks
	// caches each active include's splice tokens so phase 3 does not
	// reprocess files.
	var chevs []childEv
	incToks := map[int][]Token{}

	// Phase 1: directive pass in line order.
	for i, ln := range lines {
		d, err := parseDirective(ln)
		if err != nil {
			return nil, &LexError{File: display, Line: i + 1, Column: 1, Msg: err.Error()}
		}
		dirs[i] = d
		if d == nil {
			active[i] = st.curActive()
			if active[i] {
				blanked[i] = ln
			} else {
				blanked[i] = ""
			}
			continue
		}
		blanked[i] = "" // directive lines never reach the lexer
		active[i] = st.curActive()
		switch d.kind {
		case dirInclude:
			if !active[i] {
				continue
			}
			// Stream order: publish this file's earlier defines so
			// the child sees them, then process the child inline.
			// The child's defines merge into live, becoming visible
			// to later lines of this file (upward sharing).
			if err := publishUntil(i + 1); err != nil {
				return nil, err
			}
			child, err := st.combineFile(d.arg, parentDir)
			if err != nil {
				return nil, err
			}
			var sp []Token
			for _, t := range child {
				if t.Type == TokEOF {
					continue
				}
				sp = append(sp, t)
			}
			sp = append(sp, Token{Type: TokNewline, Lit: "\n", File: display, Line: i + 1, Column: 1})
			incToks[i+1] = sp
			added := map[string]ppDefine{}
			for k, v := range st.defines {
				if _, ok := live[k]; !ok {
					added[k] = v
				}
			}
			for k, v := range added {
				live[k] = v
			}
			if len(added) > 0 {
				chevs = append(chevs, childEv{line: i + 1, adds: added})
			}
		case dirDefine:
			if !active[i] {
				continue
			}
			def, err := st.defineValue(d.rest)
			if err != nil {
				return nil, &LexError{File: display, Line: i + 1, Column: 1, Msg: err.Error()}
			}
			if _, ok := live[d.arg]; ok {
				return nil, &LexError{File: display, Line: i + 1, Column: 1, Msg: fmt.Sprintf("#define %q は既に定義されています", d.arg)}
			}
			live[d.arg] = def
			own = append(own, ownDef{line: i + 1, name: d.arg, def: def})
		case dirIfdef, dirIfndef:
			parent := st.curActive()
			// live holds base plus this file's defines and child
			// contributions from earlier stream positions only, so
			// stream order is honored.
			_, defined := live[d.arg]
			met := defined == (d.kind == dirIfdef)
			st.conds = append(st.conds, ppFrame{parentActive: parent, active: parent && met, taken: met})
		case dirElse:
			if len(st.conds) == condDepth {
				return nil, &LexError{File: display, Line: i + 1, Column: 1, Msg: "対応する #ifdef のない #else です"}
			}
			top := &st.conds[len(st.conds)-1]
			if top.elseSeen {
				return nil, &LexError{File: display, Line: i + 1, Column: 1, Msg: "#else が重複しています"}
			}
			top.elseSeen = true
			top.active = top.parentActive && !top.taken
			active[i] = st.curActive()
		case dirEndif:
			if len(st.conds) == condDepth {
				return nil, &LexError{File: display, Line: i + 1, Column: 1, Msg: "対応する #ifdef のない #endif です"}
			}
			st.conds = st.conds[:len(st.conds)-1]
			active[i] = st.curActive()
		case dirError:
			if active[i] {
				msg := d.arg
				if msg == "" {
					msg = "エラーが発生しました"
				}
				return nil, &LexError{File: display, Line: i + 1, Column: 1, Msg: msg}
			}
		case dirMode:
			// Last active one wins (includes count: the tree shares st).
			if active[i] {
				st.mode = d.arg
			}
		}
	}
	if len(st.conds) != condDepth {
		return nil, &LexError{File: display, Line: len(lines), Column: 1, Msg: "閉じられていない #ifdef があります"}
	}

	// Phase 2: lex live lines, substituting #define uses. A use sees
	// defines from earlier stream positions only (base + own up to its
	// line + child contributions from earlier includes).
	toks, err := Lex(strings.Join(blanked, "\n"))
	if err != nil {
		if le, ok := err.(*LexError); ok {
			le.File = display
			return nil, le
		}
		return nil, err
	}
	eff := make(map[string]ppDefine, len(base))
	for k, v := range base {
		eff[k] = v
	}
	// Timeline walk in line order: own defines and child contributions
	// (both collected in sweep order) apply to tokens at their position
	// or later.
	oi, ci := 0, 0
	for i := range toks {
		for oi < len(own) && own[oi].line <= toks[i].Line {
			eff[own[oi].name] = own[oi].def
			oi++
		}
		for ci < len(chevs) && chevs[ci].line <= toks[i].Line {
			for k, v := range chevs[ci].adds {
				eff[k] = v
			}
			ci++
		}
		if toks[i].Type == TokIdent && !(i > 0 && toks[i-1].Type == TokDot) {
			if def, ok := eff[toks[i].Lit]; ok {
				toks[i].Type = def.typ
				toks[i].Lit = def.lit
			}
		}
	}
	byLine := map[int][]Token{}
	eofLine, eofCol := len(lines), 1
	for i := range toks {
		if toks[i].Type == TokEOF {
			// Lex の EOF 位置を保持する（0:0 のままだと
			// 「ブロックが閉じられていません」等が 0:0 になる）。
			eofLine, eofCol = toks[i].Line, toks[i].Column
			continue
		}
		toks[i].File = display
		byLine[toks[i].Line] = append(byLine[toks[i].Line], toks[i])
	}

	// Phase 3: sweep lines in order; publish the remaining own defines
	// and splice the cached child tokens (processed inline in phase 1
	// in stream order).
	var out []Token
	for ln := 1; ln <= len(lines)+1; ln++ {
		if err := publishUntil(ln); err != nil {
			return nil, err
		}
		if ln <= len(lines) {
			if d := dirs[ln-1]; d != nil && d.kind == dirInclude && active[ln-1] {
				out = append(out, incToks[ln]...)
				continue
			}
		}
		out = append(out, byLine[ln]...)
	}
	out = append(out, Token{Type: TokEOF, File: display, Line: eofLine, Column: eofCol})
	return out, nil
}

// dirKind classifies a preprocessor directive line.
type dirKind int

const (
	dirInclude dirKind = iota + 1
	dirDefine
	dirIfdef
	dirIfndef
	dirElse
	dirEndif
	dirError
	dirMode
)

// directive is one parsed `#...` line: arg holds the include path,
// define/ifdef name, or #error message; rest holds the #define value.
type directive struct {
	kind dirKind
	arg  string
	rest string
}

// parseDirective recognizes a whole-line directive. It returns nil when
// the line holds normal code. Malformed directives are an error
// (reported at the directive line).
func parseDirective(line string) (*directive, error) {
	// Tolerate CRLF sources: lines arrive split on "\n" with "\r" kept.
	line = strings.TrimSuffix(line, "\r")
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return nil, nil
	}
	rest := strings.TrimLeft(trimmed[1:], " \t")
	i := 0
	for i < len(rest) && isDirectiveLetter(rest[i]) {
		i++
	}
	word, tail := rest[:i], strings.TrimLeft(rest[i:], " \t")
	// A sharp alone (`#`) or `# 123`: no directive word.
	// `# 123` は `#` だけでなく `123` も落とさず報告する。
	unknown := func() (*directive, error) {
		got := "#" + word
		if word == "" {
			got = "#" + strings.TrimSpace(rest)
			if got == "#" {
				// `#` 単体はそのまま。
			}
		}
		return nil, fmt.Errorf("不明なディレクティブ %q です（#include/#define/#ifdef/#ifndef/#else/#endif/#error/#mode を使用してください）", got)
	}
	switch word {
	case "include":
		if !strings.HasPrefix(tail, "\"") {
			return nil, fmt.Errorf("不正な #include ディレクティブです（#include \"file\" が必要です）")
		}
		end := strings.Index(tail[1:], "\"")
		if end < 0 {
			return nil, fmt.Errorf("不正な #include ディレクティブです（閉じクォートがありません）")
		}
		target := tail[1 : 1+end]
		if tail := strings.Trim(tail[1+end+1:], " \t"); tail != "" {
			return nil, fmt.Errorf("不正な #include ディレクティブです（末尾に %q があります）", tail)
		}
		if target == "" {
			return nil, fmt.Errorf("不正な #include ディレクティブです（パスが空です）")
		}
		return &directive{kind: dirInclude, arg: target}, nil
	case "define":
		name, value := splitDirectiveArg(tail)
		if !isDefineName(name) {
			return nil, fmt.Errorf("不正な #define ディレクティブです（#define NAME [値] が必要です）")
		}
		// Keyword/builtin names would define dead macros (keywords never
		// match TokIdent) or hijack builtin calls: reject them outright.
		if _, isKw := keywords[name]; isKw || isBuiltin(name) {
			return nil, fmt.Errorf("#define %q は使用できません：キーワード・真偽値・組み込み関数と同名です", name)
		}
		// An empty value auto-numbers (resolved at collection).
		return &directive{kind: dirDefine, arg: name, rest: value}, nil
	case "ifdef", "ifndef":
		name, extra := splitDirectiveArg(tail)
		if !isDefineName(name) || strings.TrimSpace(extra) != "" {
			return nil, fmt.Errorf("不正な #%s ディレクティブです（#%s NAME が必要です）", word, word)
		}
		if word == "ifdef" {
			return &directive{kind: dirIfdef, arg: name}, nil
		}
		return &directive{kind: dirIfndef, arg: name}, nil
	case "else", "endif":
		if strings.TrimSpace(tail) != "" {
			return nil, fmt.Errorf("不正な #%s ディレクティブです（#%s の後に余分な文字があります）", word, word)
		}
		if word == "else" {
			return &directive{kind: dirElse}, nil
		}
		return &directive{kind: dirEndif}, nil
	case "error":
		return &directive{kind: dirError, arg: strings.TrimSpace(tail)}, nil
	case "mode":
		name, extra := splitDirectiveArg(tail)
		if (name != "cli" && name != "gui") || strings.TrimSpace(extra) != "" {
			return nil, fmt.Errorf("不正な #mode ディレクティブです（#mode cli または #mode gui が必要です）")
		}
		return &directive{kind: dirMode, arg: name}, nil
	}
	return unknown()
}

func isDirectiveLetter(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z'
}

// splitDirectiveArg splits "NAME rest..." at the first blank.
func splitDirectiveArg(s string) (name, rest string) {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// isDefineName reports whether s is a valid preprocessor name.
func isDefineName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || i > 0 && '0' <= c && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// defineValue resolves a #define value: empty means the next sequential
// number (auto-numbering); an explicit int resets the counter to
// value+1; other literals leave the counter alone.
func (st *includer) defineValue(rest string) (ppDefine, error) {
	if strings.TrimSpace(rest) == "" {
		if st.seq == math.MaxInt64 {
			return ppDefine{}, fmt.Errorf("連番カウンタが上限を超えました")
		}
		v := st.seq
		st.seq++
		return ppDefine{typ: TokInt, lit: strconv.FormatInt(v, 10)}, nil
	}
	def, err := parseDefineValue(rest)
	if err != nil {
		return ppDefine{}, err
	}
	if def.typ == TokInt {
		v, err := strconv.ParseInt(def.lit, 10, 64)
		if err != nil {
			return ppDefine{}, fmt.Errorf("不正な #define 値です（整数 %q が範囲外です）", def.lit)
		}
		if v == math.MaxInt64 {
			return ppDefine{}, fmt.Errorf("連番カウンタが上限を超えました")
		}
		st.seq = v + 1
	}
	return def, nil
}

// parseDefineValue lexes a #define value: exactly one int/float/string/
// bool literal, with an optional leading `-` for numbers.
func parseDefineValue(value string) (ppDefine, error) {
	bad := func() (ppDefine, error) {
		return ppDefine{}, fmt.Errorf("不正な #define 値です（整数・小数・文字列・真偽値のリテラルを 1 つ指定してください）")
	}
	toks, err := Lex(value)
	if err != nil {
		return ppDefine{}, err
	}
	var body []Token
	for _, t := range toks {
		if t.Type != TokEOF {
			body = append(body, t)
		}
	}
	neg := false
	if len(body) == 2 && body[0].Type == TokMinus && (body[1].Type == TokInt || body[1].Type == TokFloat) {
		neg = true
		body = body[1:]
	}
	if len(body) != 1 {
		return bad()
	}
	switch body[0].Type {
	case TokInt, TokFloat, TokString, TokTrue, TokFalse:
		lit := body[0].Lit
		if neg {
			lit = "-" + lit
		}
		return ppDefine{typ: body[0].Type, lit: lit}, nil
	}
	return bad()
}

// resolveInclude finds path against parentDir, then the working directory.
func resolveInclude(path, parentDir string) (string, error) {
	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("#include %q を開けません：%s", path, err.Error())
		}
		return path, nil
	}
	cands := []string{}
	if parentDir != "" {
		cands = append(cands, filepath.Join(parentDir, path))
	} else {
		cands = append(cands, path)
	}
	if cwd, err := os.Getwd(); err == nil {
		if c := filepath.Join(cwd, path); c != cands[0] {
			cands = append(cands, c)
		}
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("#include %q を開けません：ファイルが見つかりません", path)
}

// displayPath shortens absolute paths against the working directory.
func displayPath(abs string) string {
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, abs); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return abs
}
