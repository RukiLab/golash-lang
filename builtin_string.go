// P2 組み込み関数：文字列操作。
//
// 文字列と配列に共通する非破壊の操作（len/slice/find）は builtin_seq.go。
//
// HSP との違い（文書化・現代化済み）：
//   - 長さとインデックスはバイトではなく文字（ルーン）単位で数えるため、日本語
//     テキストは1グリフ＝1文字として扱われる。
//   - find は絶対インデックスを返す（HSP の instr は開始位置からの相対）。
//   - split/getpath/strf は out 引数への書き込みではなく戻り値を返す。
package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

func init() {
	// strtrim(s [, chars] [, mode])：両端から文字を取り除く（mode が
	// 0 の場合は両端、1 は左のみ、2 は右のみ）。chars 省略時は空白文字。
	register("strtrim", 1, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("strtrim", args, 0, at)
		if err != nil {
			return Null(), err
		}
		cutset := ""
		whitespace := true
		mode := int64(0)
		if len(args) >= 2 {
			cutset, err = needString("strtrim", args, 1, at)
			if err != nil {
				return Null(), err
			}
			whitespace = false
		}
		if len(args) == 3 {
			mode, err = needInt("strtrim", args, 2, at)
			if err != nil {
				return Null(), err
			}
			if mode < 0 || mode > 2 {
				return Null(), argErr("strtrim", 2, at, "mode は 0（両端）、1（左）、2（右）のいずれかである必要があります。%d が指定されました", mode)
			}
		}
		trim := func(t string) string {
			switch mode {
			case 1:
				if whitespace {
					return strings.TrimLeftFunc(t, unicode.IsSpace)
				}
				return strings.TrimLeft(t, cutset)
			case 2:
				if whitespace {
					return strings.TrimRightFunc(t, unicode.IsSpace)
				}
				return strings.TrimRight(t, cutset)
			default:
				if whitespace {
					return strings.TrimSpace(t)
				}
				return strings.Trim(t, cutset)
			}
		}
		return Str(trim(s)), nil
	})

	// replace(s, old, new)：old の全出現を new に置換する。
	register("replace", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("replace", args, 0, at)
		if err != nil {
			return Null(), err
		}
		old, err := needString("replace", args, 1, at)
		if err != nil {
			return Null(), err
		}
		nw, err := needString("replace", args, 2, at)
		if err != nil {
			return Null(), err
		}
		if old == "" {
			return Null(), argErr("replace", 1, at, "検索文字列を空にすることはできません")
		}
		return Str(strings.ReplaceAll(s, old, nw)), nil
	})

	// upper(s)／lower(s)：Unicode の大文字・小文字変換
	//（日本語テキストには影響しない）。
	register("upper", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("upper", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Str(strings.ToUpper(s)), nil
	})
	register("lower", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("lower", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Str(strings.ToLower(s)), nil
	})

	// split(s, delim)：文字列の配列に分割する。
	register("split", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("split", args, 0, at)
		if err != nil {
			return Null(), err
		}
		delim, err := needString("split", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if delim == "" {
			return Null(), argErr("split", 1, at, "区切り文字を空にすることはできません")
		}
		parts := strings.Split(s, delim)
		elems := make([]Value, len(parts))
		for i, p := range parts {
			elems[i] = Str(p)
		}
		return ArrayOf(elems), nil
	})

	// strf(format, args...)：書式付き文字列（C／Go 風の %d %f %s %x %c %%）。
	// 対応指定子は MANUAL の6種のみ（%d %f %s %x %c %%。フラグ・幅・精度付き可）。
	// それ以外の動詞（%v %T %p 等）・引数の過不足・型の不一致は、
	// Go の "%!d(MISSING)" 等の素通しではなくスクリプトエラーにする。
	register("strf", 1, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		format, err := needString("strf", args, 0, at)
		if err != nil {
			return Null(), err
		}
		verbs, err := parseStrfVerbs(format)
		if err != nil {
			return Null(), rtErrf(at, "strf：%s", err.Error())
		}
		if len(verbs) != len(args)-1 {
			return Null(), argErr("strf", 0, at, "書式指定子は %d 個ですが、引数は %d 個です", len(verbs), len(args)-1)
		}
		fargs := make([]any, len(args)-1)
		for i, v := range verbs {
			a := args[i+1]
			switch v {
			case 'd', 'x':
				if a.K != KInt {
					return Null(), argErr("strf", i+1, at, "%%%c には int が必要です。%s が指定されました", v, typeNameOf(a))
				}
				fargs[i] = a.I
			case 'c':
				if a.K != KInt {
					return Null(), argErr("strf", i+1, at, "%%c には int が必要です。%s が指定されました", typeNameOf(a))
				}
				if a.I < 0 || a.I > 0x10FFFF || (a.I >= 0xD800 && a.I <= 0xDFFF) {
					return Null(), argErr("strf", i+1, at, "%%c に無効なコードポイント %d です", a.I)
				}
				fargs[i] = a.I
			case 'f':
				if a.K == KFloat {
					fargs[i] = a.F
				} else if a.K == KInt {
					fargs[i] = float64(a.I)
				} else {
					return Null(), argErr("strf", i+1, at, "%%f には数値が必要です。%s が指定されました", typeNameOf(a))
				}
			case 's':
				fargs[i] = Stringify(a)
			}
		}
		return Str(fmt.Sprintf(format, fargs...)), nil
	})

	// getpath(path, mode)：パス分解。モードは HSP に準拠したビットフラグで
	// 加算で組み合わせる：0 はコピー、1 は拡張子除去、2 は拡張子のみ
	// （ドット付き）、8 はディレクトリ除去、16 は小文字化、32 は
	// ディレクトリのみ。
	register("getpath", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("getpath", args, 0, at)
		if err != nil {
			return Null(), err
		}
		mode, err := needInt("getpath", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if mode < 0 || mode&^(1|2|8|16|32) != 0 {
			return Null(), argErr("getpath", 1, at, "mode は 1/2/8/16/32 の組み合わせである必要があります。%d が指定されました", mode)
		}
		// 分解用に区切り文字を正規化し、その後で元の形式に戻す。
		win := strings.Contains(p, "\\")
		slash := strings.ReplaceAll(p, "\\", "/")
		dir, file := filepath.Split(slash)
		ext := filepath.Ext(file)
		base := strings.TrimSuffix(file, ext)
		out := p
		if mode&1 != 0 {
			out = joinDir(dir, base)
		}
		if mode&2 != 0 {
			// 拡張子のみ。8/32 との組み合わせでも HSP と同じく
			// 拡張子だけを返す。
			out = ext
		}
		if mode&8 != 0 && mode&2 == 0 {
			if mode&1 != 0 {
				out = base
			} else {
				out = file
			}
		}
		if mode&32 != 0 {
			out = dir
		}
		if mode&16 != 0 {
			out = strings.ToLower(out)
		}
		if win {
			out = strings.ReplaceAll(out, "/", "\\")
		}
		return Str(out), nil
	})

	// asc(s)：先頭文字のコードポイント（GUI の input()
	// の制御文字を含む：Backspace の asc(input()) は 8）。
	// 空文字列はエラー。
	register("asc", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("asc", args, 0, at)
		if err != nil {
			return Null(), err
		}
		r := []rune(s)
		if len(r) == 0 {
			return Null(), rtErrf(at, "asc：空文字列のコードは取得できません")
		}
		return Int(int64(r[0])), nil
	})

	// chr(code)：Unicode コードポイントに対応する1文字の文字列。
	// サロゲートと範囲外の値はエラー。
	register("chr", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		c, err := needInt("chr", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if c < 0 || c > 0x10FFFF || (c >= 0xD800 && c <= 0xDFFF) {
			return Null(), argErr("chr", 0, at, "無効なコードポイント %d です", c)
		}
		return Str(string(rune(c))), nil
	})
}

// joinDir は slash 形式の dir とファイル部位を再結合する。
func joinDir(dir, file string) string {
	if dir == "" {
		return file
	}
	return dir + file
}

// parseStrfVerbs は strf 書式から動詞列を取り出す。
// 対応は %d %f %s %x %c %% のみ（フラグ・幅・精度付き可）。
// `*`（幅・精度の引数取り）は対応しない。
func parseStrfVerbs(format string) ([]rune, error) {
	var verbs []rune
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		if i >= len(format) {
			return nil, fmt.Errorf("書式の末尾に %% があります")
		}
		if format[i] == '%' {
			continue
		}
		for i < len(format) && strings.ContainsRune("-+ #0", rune(format[i])) {
			i++
		}
		if i < len(format) && format[i] == '*' {
			return nil, fmt.Errorf("幅の * には対応していません")
		}
		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			i++
		}
		if i < len(format) && format[i] == '.' {
			i++
			if i < len(format) && format[i] == '*' {
				return nil, fmt.Errorf("精度の * には対応していません")
			}
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				i++
			}
		}
		if i >= len(format) {
			return nil, fmt.Errorf("書式の末尾に %% があります")
		}
		switch format[i] {
		case 'd', 'f', 's', 'x', 'c':
			verbs = append(verbs, rune(format[i]))
		default:
			return nil, fmt.Errorf("対応していない書式 %%%c です（%%d %%f %%s %%x %%c %%%% のみ）", format[i])
		}
	}
	return verbs, nil
}
