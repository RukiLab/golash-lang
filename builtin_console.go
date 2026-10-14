// P4 組み込み: コンソールの入出力。
package main

import "strings"

func init() {
	// print(args...): 末尾の改行なしで表示する。末尾の
	// スタイル指定 ("bold", "italic", "bolditalic", "underline")
	// は装飾として消費され表示されない。Void 引数
	// は黙って無視される。
	register("print", 0, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		rest, st := splitStyleArgs(args)
		parts := make([]string, 0, len(rest))
		for _, a := range rest {
			if a.K == KNull {
				continue
			}
			parts = append(parts, Stringify(a))
		}
		out := ""
		for i, p := range parts {
			if i > 0 {
				out += " "
			}
			out += p
		}
		in.be.Print(out, st)
		return Null(), nil
	})

	// cls(): clear the screen. GUIではキャンバス/テキストを消去します。
	// ウィジェットは残ります（消去は clrobj() のみ）。
	register("cls", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		in.be.Clear()
		return Null(), nil
	})

	// color(r, g, b [, a]): 文字・描画色を設定する (各 0-255)。
	// a は不透明度 (0 が透明 .. 255 が不透明、既定値 255)。
	// 端末は文字の不透明度を描画できないため、CUI では無視される。
	// color(rgb): 0xRRGGBB 形式の整数で色を設定する (pget()形式)。
	// color() で色をリセットする。
	register("color", 0, 4, func(in *Interp, args []Value, at Pos) (Value, error) {
		if len(args) == 0 {
			in.be.ResetColor()
			return Null(), nil
		}
		if len(args) == 1 {
			v, err := needInt("color", args, 0, at)
			if err != nil {
				return Null(), err
			}
			if v < 0 || v > 0xFFFFFF {
				return Null(), argErr("color", 0, at, "0x000000 から 0xFFFFFF の範囲で指定してください。%d が指定されました", v)
			}
			in.be.SetColor(int((v>>16)&0xFF), int((v>>8)&0xFF), int(v&0xFF), 255)
			return Null(), nil
		}
		if len(args) != 3 && len(args) != 4 {
			return Null(), rtErrf(at, "color は 0 個、1 個、3 個または 4 個の引数が必要ですが、%d 個が渡されました", len(args))
		}
		rgba := []int{0, 0, 0, 255}
		for i := range args {
			v, err := needInt("color", args, i, at)
			if err != nil {
				return Null(), err
			}
			if v < 0 || v > 255 {
				return Null(), argErr("color", i, at, "0 から 255 の範囲で指定してください。%d が指定されました", v)
			}
			rgba[i] = int(v)
		}
		in.be.SetColor(rgba[0], rgba[1], rgba[2], rgba[3])
		return Null(), nil
	})

	// title(s): 端末のタイトル変更を要求する (ベストエフォート)。
	// NUL は GUI のウィンドウ層を異常終了させるため、ここで拒否する
	// (両バックエンド共通)。
	register("title", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("title", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if strings.IndexByte(s, 0) >= 0 {
			return Null(), argErr("title", 0, at, "タイトルに NUL 文字は使用できません")
		}
		in.be.SetTitle(s)
		return Null(), nil
	})

	// pos(x, y): move the cursor. CUIでは文字セル、GUIではピクセル基準。負値も許可し画面外への描画に利用できます。
	register("pos", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		x, err := needInt("pos", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("pos", args, 1, at)
		if err != nil {
			return Null(), err
		}
		in.be.MoveTo(int(x), int(y))
		return Null(), nil
	})

	// input([prompt]): 1 行読み込み、文字列として返す。
	// EOF は "" になる (仕様上、空行と区別しない)。
	//
	// GUI は行モードではなく即時モード: input() は
	// 前回呼び出し以降に入力された文字を返す (IME 確定済みテキスト
	// を含む、テキストの OS キーリピートに加え制御キー向けの
	// 合成リピートあり、入力なしは "")。決して待たないため、
	// ポーリングループで独自の行編集器を組む ("\r" で確定、
	// コードは asc() で取得)。フォーカスされた inputbox が
	// ストリームを占有する (その間 input() は "" を返す)。
	// GUI では引数を取らない: プロンプトは毎回繰り返し表示されるため、
	// 代わりに mes() で表示すること。
	register("input", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		if ir, ok := in.be.(immediateReader); ok {
			if len(args) != 0 {
				return Null(), rtErrf(at, "input は GUI では引数を取りません（プロンプトは mes() で表示してください）。%d 個が渡されました", len(args))
			}
			return Str(ir.ReadImmediate()), nil
		}
		prompt := ""
		if len(args) == 1 {
			var err error
			prompt, err = needString("input", args, 0, at)
			if err != nil {
				return Null(), err
			}
		}
		line, ok := in.be.ReadLine(prompt)
		if !ok {
			return Str(""), nil
		}
		return Str(line), nil
	})
}
