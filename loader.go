// Golash のローダ（include / dll / dlldef 宣言）。
//
// マクロ・条件分岐・実行モードのプリプロセッサは廃止しました。
// このファイルが扱うのは、ファイルを取り込む include と、DLL 関数を
// 宣言する dll / dlldef だけです。いずれも行頭の宣言として書きます。
//
//	include "path"                   ファイルを埋め込みます（一度だけ、循環はエラー）
//	dll "path"                       以降の dlldef が対象にする DLL
//	dlldef NAME "symbol" "sig"        DLL 関数の薄い def 包みを生成します
//
// 探索順：取り込み元ファイルのあるディレクトリ、次に作業ディレクトリ。
// 絶対パスはそのまま使います。トークンはファイル内の行番号を保ち、
// 表示用パスを Token.File に持ちます。include 行は子トークンに置換され、
// dll / dlldef 行は空行または生成された def 行になります。
//
// 注意：include / dll / dlldef は行頭の単語として認識されるため、
// ブロックコメント内の行頭にある同名の行も宣言として扱われます。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// loadState は1回のロード中の共有状態です。
type loadState struct {
	done    map[string]bool // 取り込み済みの絶対パス（一度だけ）
	stack   []string        // 現在の連鎖上の絶対パス（循環検出用）
	dllSeen map[string]bool // dlldef で定義済みの名前（重複はエラー）
	// dllPath は直近の dll で指定されたパス（引用符つき原文）。
	// include ツリー全体で共有し、最後に有効だったものが勝ちます。
	dllPath string
}

func newLoadState() *loadState {
	return &loadState{done: map[string]bool{}, dllSeen: map[string]bool{}}
}

// LoadFile は entry を読み込み、include ツリーを1つのトークン列に
// 埋め込みます（末尾は EOF）。表示用パスは可能な限り相対のままです。
func LoadFile(entry string) ([]Token, error) {
	st := newLoadState()
	return st.loadFile(entry, "")
}

// LoadSource は src 内の include / dll / dlldef を処理します（REPL 入力や
// テスト片向け）。display はエラー表示用の疑似ファイル名、parentDir は
// 相対 include の基準です。
func LoadSource(display, src, parentDir string) ([]Token, error) {
	st := newLoadState()
	return st.loadSource(display, src, parentDir)
}

func (st *loadState) loadFile(path, parentDir string) ([]Token, error) {
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
			return nil, fmt.Errorf("循環する include です：%s", strings.Join(chain, " -> "))
		}
	}
	if st.done[abs] {
		return nil, nil // 一度限りの意味論: 共有ライブラリは一度だけ差し込まれる
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("include %q を開けません：%s", path, err.Error())
	}
	st.stack = append(st.stack, abs)
	toks, err := st.loadSource(displayPath(abs), string(data), filepath.Dir(abs))
	st.stack = st.stack[:len(st.stack)-1]
	if err != nil {
		return nil, err
	}
	st.done[abs] = true
	return toks, nil
}

// loadKind はローダ宣言行の種類です。
type loadKind int

const (
	loadInclude loadKind = iota + 1
	loadDll
	loadDlldef
)

// loadDirective は解析されたローダ宣言1行です。
type loadDirective struct {
	kind loadKind
	// include: arg は引用符を外したパス、dll: path は引用符つき原文。
	arg string
	// dlldef: path は対象 DLL（引用符つき原文）、sym/sig も原文。
	path string
	sym  string
	sig  string
	argc int
}

// loadSource は行頭の include / dll / dlldef を処理し、残りを字句解析して
// 1つのトークン列にします。行番号はすべて保たれます。
func (st *loadState) loadSource(display, src, parentDir string) ([]Token, error) {
	lines := strings.Split(src, "\n")
	blanked := make([]string, len(lines))
	incToks := map[int][]Token{}

	for i, ln := range lines {
		blanked[i] = ln
		d, err := parseLoadDirective(ln)
		if err != nil {
			return nil, &LexError{File: display, Line: i + 1, Column: 1, Msg: err.Error()}
		}
		if d == nil {
			continue
		}
		switch d.kind {
		case loadInclude:
			blanked[i] = ""
			child, err := st.loadFile(d.arg, parentDir)
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
		case loadDll:
			blanked[i] = ""
			st.dllPath = d.path
		case loadDlldef:
			if st.dllSeen[d.arg] {
				return nil, &LexError{File: display, Line: i + 1, Column: 1, Msg: fmt.Sprintf("dlldef %q は既に定義されています", d.arg)}
			}
			if st.dllPath == "" {
				return nil, &LexError{File: display, Line: i + 1, Column: 1, Msg: fmt.Sprintf("dlldef %q の対象 DLL がありません（先に dll \"path\" で指定してください）", d.arg)}
			}
			st.dllSeen[d.arg] = true
			// def NAME(__a0,__a1)
			// return $dllinvoke("dll","sym","sig",__a0,__a1)
			// end
			// $ は字句で書けない隠し名のため、一旦字句可能な仮名で生成し、
			// 生成トークンだけ隠し名へ書換える（利用者の同名記述とは衝突しない）。
			params := make([]string, d.argc)
			for k := range params {
				params[k] = "__a" + strconv.Itoa(k)
			}
			call := dllPlaceholder + "(" + st.dllPath + "," + d.sym + "," + d.sig
			if len(params) > 0 {
				call += "," + strings.Join(params, ",")
			}
			call += ")"
			snippet := "def " + d.arg + "(" + strings.Join(params, ",") + ")\nreturn " + call + "\nend\n"
			gen, err := Lex(snippet)
			if err != nil {
				return nil, &LexError{File: display, Line: i + 1, Column: 1, Msg: err.Error()}
			}
			var sp []Token
			for _, t := range gen {
				if t.Type == TokEOF {
					continue
				}
				t.File = display
				t.Line = i + 1
				if t.Type == TokIdent && t.Lit == dllPlaceholder {
					t.Lit = dllHiddenName
				}
				sp = append(sp, t)
			}
			blanked[i] = ""
			incToks[i+1] = sp
		}
	}

	toks, err := Lex(strings.Join(blanked, "\n"))
	if err != nil {
		if le, ok := err.(*LexError); ok {
			le.File = display
			return nil, le
		}
		return nil, err
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

	var out []Token
	for ln := 1; ln <= len(lines)+1; ln++ {
		if ln <= len(lines) {
			if sp, ok := incToks[ln]; ok {
				out = append(out, sp...)
				continue
			}
		}
		out = append(out, byLine[ln]...)
	}
	out = append(out, Token{Type: TokEOF, File: display, Line: eofLine, Column: eofCol})
	return out, nil
}

// parseLoadDirective は行頭の include / dll / dlldef 宣言を認識します。
// 通常のコード（同名の変数への代入など）は nil を返します。
func parseLoadDirective(line string) (*loadDirective, error) {
	line = strings.TrimSuffix(line, "\r")
	trimmed := strings.TrimLeft(line, " \t")
	i := 0
	for i < len(trimmed) && isWordByte(trimmed[i]) {
		i++
	}
	word := trimmed[:i]
	if word != "include" && word != "dll" && word != "dlldef" {
		return nil, nil
	}
	if i >= len(trimmed) {
		switch word {
		case "include":
			return nil, fmt.Errorf("不正な include 宣言です（include \"file\" が必要です）")
		case "dll":
			return nil, fmt.Errorf("不正な dll 宣言です（dll \"path\" が必要です）")
		default:
			return nil, fmt.Errorf("不正な dlldef 宣言です（dlldef NAME \"symbol\" \"sig\" が必要です）")
		}
	}
	if trimmed[i] != ' ' && trimmed[i] != '\t' {
		return nil, nil
	}
	tail := strings.TrimLeft(trimmed[i:], " \t")
	if tail == "" {
		switch word {
		case "include":
			return nil, fmt.Errorf("不正な include 宣言です（include \"file\" が必要です）")
		case "dll":
			return nil, fmt.Errorf("不正な dll 宣言です（dll \"path\" が必要です）")
		case "dlldef":
			return nil, fmt.Errorf("不正な dlldef 宣言です（dlldef NAME \"symbol\" \"sig\" が必要です）")
		}
	}

	switch word {
	case "include":
		if !strings.HasPrefix(tail, "\"") {
			return nil, nil // 変数 include への代入など
		}
		raw, rest, ok := scanQuoted(tail)
		if !ok {
			return nil, fmt.Errorf("不正な include 宣言です（include \"file\" が必要です）")
		}
		if err := checkDeclTail(rest); err != nil {
			return nil, fmt.Errorf("不正な include 宣言です（%s）", err.Error())
		}
		target := raw[1 : len(raw)-1]
		if target == "" {
			return nil, fmt.Errorf("不正な include 宣言です（パスが空です）")
		}
		return &loadDirective{kind: loadInclude, arg: target}, nil

	case "dll":
		if !strings.HasPrefix(tail, "\"") {
			return nil, nil
		}
		raw, rest, ok := scanQuoted(tail)
		if !ok {
			return nil, fmt.Errorf("不正な dll 宣言です（dll \"path\" が必要です）")
		}
		if err := checkDeclTail(rest); err != nil {
			return nil, fmt.Errorf("不正な dll 宣言です（%s）", err.Error())
		}
		if raw == `""` {
			return nil, fmt.Errorf("不正な dll 宣言です（パスが空です）")
		}
		return &loadDirective{kind: loadDll, path: raw}, nil

	case "dlldef":
		name, rest := splitDirectiveArg(tail)
		if !isDefineName(name) {
			return nil, nil // 変数 dlldef への代入など
		}
		if err := dllResName("dlldef", name); err != nil {
			return nil, err
		}
		sym, rest, ok := scanQuoted(strings.TrimLeft(rest, " \t"))
		if !ok {
			return nil, fmt.Errorf("不正な dlldef 宣言です（dlldef NAME \"symbol\" \"sig\" が必要です）")
		}
		sig, extra, ok := scanQuoted(strings.TrimLeft(rest, " \t"))
		if !ok {
			return nil, fmt.Errorf("不正な dlldef 宣言です（dlldef NAME \"symbol\" \"sig\" が必要です）")
		}
		if err := checkDeclTail(extra); err != nil {
			return nil, fmt.Errorf("不正な dlldef 宣言です（%s）", err.Error())
		}
		parsed, err := parseDllSig(sig[1 : len(sig)-1])
		if err != nil {
			return nil, fmt.Errorf("不正な dlldef の署名です: %s", err.Error())
		}
		return &loadDirective{kind: loadDlldef, arg: name, sym: sym, sig: sig, argc: len(parsed.args)}, nil
	}
	return nil, nil
}

// checkDeclTail は宣言の後ろに許されるのは空白と行コメントだけです。
func checkDeclTail(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.HasPrefix(rest, "//") {
		return nil
	}
	return fmt.Errorf("末尾に %q があります", rest)
}

func isWordByte(c byte) bool {
	return c == '_' || 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9'
}

// splitDirectiveArg は "NAME rest..." を最初の空白で分割する。
func splitDirectiveArg(s string) (name, rest string) {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// scanQuoted は s の先頭から "..." を原文のまま切り出す。
// バックスラッシュ直後の文字は引用符でも終端にしない。
// 戻り値は引用符つき原文と残り。原文を使うため、Windows のパスにある
// 円記号を文字列リテラルのエスケープとして解釈しない。
func scanQuoted(s string) (raw, rest string, ok bool) {
	if len(s) == 0 || s[0] != '"' {
		return "", s, false
	}
	i := 1
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == '"' {
			return s[:i+1], strings.TrimLeft(s[i+1:], " \t"), true
		}
		i++
	}
	return "", s, false
}

// dllResName は dll / dlldef の名前を検査する。
func dllResName(word, name string) error {
	if !isDefineName(name) {
		return fmt.Errorf("不正な %s 宣言です", word)
	}
	if isLegacyDllName(name) {
		return fmt.Errorf("%s は廃止されました。dll \"path\" / dlldef NAME \"symbol\" \"sig\" を使ってください", name)
	}
	if _, isKw := keywords[name]; isKw || isBuiltin(name) {
		return fmt.Errorf("%s %q は使用できません：キーワード・真偽値・組み込み関数と同名です", word, name)
	}
	return nil
}

// isDefineName は s が有効な識別子かを返す。
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

// resolveInclude は path を parentDir、次に作業ディレクトリに対して検索する。
func resolveInclude(path, parentDir string) (string, error) {
	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("include %q を開けません：%s", path, err.Error())
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
	return "", fmt.Errorf("include %q を開けません：ファイルが見つかりません", path)
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
