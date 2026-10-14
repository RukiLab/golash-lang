// Golash 用プリプロセッサ（#include、#define、条件付き）。
//
// ディレクティブは、行頭の空白を除いた最初の文字が `#` である行全体を指します。
// 対応ディレクティブ（最小集合）：
//
// #mode cli|gui  `gsh run` の実行モード（最後の有効な指定が優先；
// 省略時は gui）
//
// #include "path"   ファイルを埋め込みます（一度だけ、循環はエラー）
// #define NAME [lit] 定数：int/float/string/bool リテラルを1つ
// （数値は先頭の `-` 可）；リテラル省略時は
// カウンタから連番を自動採番（明示した int で
// リセット、それ以外のリテラルは据え置き）
// #ifdef NAME / #ifndef NAME / #else / #endif
// 条件付きの行（入れ子可）
// #error message    到達したらメッセージ付きで失敗します
//
// 探索順：取り込み元ファイルのあるディレクトリ、次に作業
// ディレクトリ；絶対パスはそのまま使います。トークンはファイル内での
// 行番号を保ち、表示用パスを Token.File に持ちます。ディレクティブ行と
// 非有効行は（削除せず）空白化するため、行番号はすべて
// 保たれます。#define はストリーム順（include 展開順）でテキスト的に
// 適用されます：使用箇所から見えるのはストリーム上でそれより前の定義だけです —
// 同一ファイルのそれより前の行、#include より前の定義
// （ライブラリ側からも見えます）、ライブラリが定義した名前（親側では
// その #include より後の行や、後の兄弟から見えます）。
// #include より後の定義が、展開済みのライブラリに届くことはありません。
// ツリーをまたぐ再定義は依然エラーです。値なし
// #define の自動採番はストリーム順に行われ、1回の実行で1つの
// カウンタを共有します。
//
// 注意：ディレクティブはコメントより先に認識されるため、ブロックコメント内の
// 行頭にある `#` もディレクティブとして扱われます。
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CombineFile は entry を読み込み、#include ツリーを1つのトークン
// 列に埋め込みます（末尾は EOF）。表示用パスは可能な限り相対のままです。
func CombineFile(entry string) ([]Token, error) {
	toks, _, err := CombineFileMode(entry)
	return toks, err
}

// CombineFileMode は CombineFile に実行モードを加えたものです：ツリー内の
// 最後の有効な `#mode cli|gui`、存在しない場合は "gui" です。
func CombineFileMode(entry string) ([]Token, string, error) {
	st := newIncluder()
	toks, err := st.combineFile(entry, "")
	if err != nil {
		return nil, "gui", err
	}
	return toks, st.runMode(), nil
}

// CombineSource は src 内のディレクティブを埋め込みます（REPL 入力やテスト
// 片向け）。display はエラー表示用の疑似ファイル名（"" で従来の
// 位置形式のまま）；parentDir は相対先の基準です。注意：呼び出しごとに
// 新しいプリプロセッサ状態で始めるため、REPL の #define は1入力
// 限り有効です。
func CombineSource(display, src, parentDir string) ([]Token, error) {
	st := newIncluder()
	return st.combineSource(display, src, parentDir)
}

type includer struct {
	done  map[string]bool // 埋め込み済みの絶対パス（一度だけ）
	stack []string        // 現在の連鎖上の絶対パス（循環検出用）
	// プリプロセッサの状態。include ツリー全体でストリーム順に共有します：
	// 定義は取り込み先へ流れ込み（ライブラリはその #include より前の
	// 定義を見ます）、後の行へ流れ出します
	// （#include より後の使用箇所はライブラリの定義を見ます）。正確な
	// 可視規則は combineSource を参照。
	defines map[string]ppDefine
	// 値なし #define 用の自動採番カウンタ（1回の実行で 0 から開始）。
	seq   int64
	conds []ppFrame
	// 実行モード：最後の有効な `#mode` 引数（"cli"/"gui"、"" は未設定）。
	mode string
}

// runMode は実効モードを返します（未設定時は "gui"）。
func (st *includer) runMode() string {
	if st.mode == "cli" {
		return "cli"
	}
	return "gui"
}

// ppDefine は #define 定数: リテラルトークン1つ。
type ppDefine struct {
	typ TokenType // TokInt, TokFloat, TokString, TokTrue, TokFalse
	lit string
}

// ppFrame は開いている #ifdef/#ifndef の1レベル。
type ppFrame struct {
	parentActive bool
	active       bool // この分岐が有効（parentActive && 採用された分岐）
	taken        bool // #if 分岐が採用された（#else を決める）
	elseSeen     bool
}

func newIncluder() *includer {
	return &includer{done: map[string]bool{}, defines: map[string]ppDefine{}}
}

// curActive は現在の行が有効なコード中にあるかを返す。
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
		return nil, nil // 一度限りの意味論: 共有ライブラリは一度だけ差し込まれる
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

// ownDef は検証済みの #define とその 1 始まりの行番号。ストリーム順で
// 共有テーブルに適用される（combineSource を参照）。
type ownDef struct {
	line int
	name string
	def  ppDefine
}

// childEv はライブラリからの1貢献: 追加された子ファイルの define で、
// #include 行で効力を発現する（上方共有）。
type childEv struct {
	line int
	adds map[string]ppDefine
}

// combineSource はプリプロセッサの行パス（条件分岐、#define 収集、
// #error）を実行し、有効行を字句解析し、#define の使用箇所を置換し、
// 各 #include の子トークンをその行に差し込む。ファイル境界を越えて
// 文が連結しないよう改行で区切る。
func (st *includer) combineSource(display, src, parentDir string) ([]Token, error) {
	lines := strings.Split(src, "\n")
	condDepth := len(st.conds) // 我々のフレーム。ファイルは親のものを pop してはならない
	dirs := make([]*directive, len(lines))
	active := make([]bool, len(lines))
	blanked := make([]string, len(lines))
	// base はファイル入場時に継承された define 集合（自身のトークン置換用）。
	// live は base にこのファイルの define と子の貢献をストリーム順で
	// 加えたもの（#ifdef 判定と再定義検出用）。
	base := make(map[string]ppDefine, len(st.defines))
	for k, v := range st.defines {
		base[k] = v
	}
	live := make(map[string]ppDefine, len(base))
	for k, v := range base {
		live[k] = v
	}
	var own []ownDef
	// pubIdx は共有テーブルに既に公開された自身のエントリ数を数える。
	// フェーズ1は include へ再帰する前に初期のものを公開し、子が
	// 正確に先行する define を見られるようにする。フェーズ3は残りを
	// 同じスイープ順で公開する。
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
	// chevs はライブラリの貢献をスイープ順に記録する。incToks は
	// 各有効な include の差込トークンをキャッシュし、フェーズ3が
	// ファイルを再処理しないようにする。
	var chevs []childEv
	incToks := map[int][]Token{}

	// フェーズ1: 行順でのディレクティブパス。
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
		blanked[i] = "" // ディレクティブ行は決して字句解析器に届かない
		active[i] = st.curActive()
		switch d.kind {
		case dirInclude:
			if !active[i] {
				continue
			}
			// ストリーム順: このファイルの先行する define を公開して
			// 子が見られるようにし、それから子をその場で処理する。
			// 子の define は live に統合され、このファイルの後続行から
			// 見えるようになる（上方共有）。
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
			// live は base に加えてこのファイルの define と、先行する
			// ストリーム位置からの子の貢献のみを保持するため、ストリーム
			// 順が尊重される。
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
			// 最後に有効だったものが勝つ（include も数に入る: 木は st を共有する）。
			if active[i] {
				st.mode = d.arg
			}
		}
	}
	if len(st.conds) != condDepth {
		return nil, &LexError{File: display, Line: len(lines), Column: 1, Msg: "閉じられていない #ifdef があります"}
	}

	// フェーズ2: 有効行を字句解析し、#define の使用を置換する。ある使用が
	// 見るのは先行するストリーム位置の define のみ（base + その行までの
	// 自身の define + 先行する include からの子の貢献）。
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
	// 行順でのタイムライン探索: 自身の define と子の貢献（どちらも
	// スイープ順に収集）は、それぞれの位置またはそれ以降のトークンに
	// 適用される。
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

	// フェーズ3: 行を順に走査し、残りの自身の define を公開して子の
	// キャッシュ済みトークンを差し込む（フェーズ1でストリーム順に
	// その場で処理済み）。
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

// dirKind はプリプロセッサディレクティブ行を分類する。
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

// directive は解析された `#...` 行1つ: arg は include パス、
// define/ifdef 名、または #error メッセージを保持し、rest は
// #define の値を保持する。
type directive struct {
	kind dirKind
	arg  string
	rest string
}

// parseDirective は行全体のディレクティブを認識する。行が通常のコードを
// 含む場合は nil を返す。不正な形式のディレクティブはエラー
// （ディレクティブ行で報告される）。
func parseDirective(line string) (*directive, error) {
	// CRLF ソースを許容する: 行は "\n" で分割され、"\r" が残ったまま届く。
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
	// シャープ単独（`#`）または `# 123`: ディレクティブ語がない。
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
		// キーワードや組込み名を define すると死んだマクロ（キーワードは
		// 決して TokIdent と一致しない）を定義したり、組込み呼出を
		// 乗っ取ったりするため、それらは完全に拒否する。
		if _, isKw := keywords[name]; isKw || isBuiltin(name) {
			return nil, fmt.Errorf("#define %q は使用できません：キーワード・真偽値・組み込み関数と同名です", name)
		}
		// 空の値は自動採番される（収集時に解決）。
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

// splitDirectiveArg は "NAME rest..." を最初の空白で分割する。
func splitDirectiveArg(s string) (name, rest string) {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// isDefineName は s が有効なプリプロセッサ名かを返す。
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

// defineValue は #define の値を解決する: 空なら次の連番（自動採番）。
// 明示的な int ならカウンタを value+1 にリセットする。他のリテラルは
// カウンタをそのままにする。
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

// parseDefineValue は #define の値を字句解析する: int/float/string/bool
// リテラルをちょうど1つ、数値には先頭に `-` を付けられる。
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

// resolveInclude は path を parentDir、次に作業ディレクトリに対して
// 検索する。
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

// displayPath は絶対パスを作業ディレクトリ基準で短縮する。
func displayPath(abs string) string {
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, abs); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return abs
}
