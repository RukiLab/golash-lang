// P4 builtins: console input/output.
package main

import "strings"

func init() {
	// print(args...): mes without the trailing newline. Trailing
	// style keywords ("bold", "italic", "bolditalic", "underline")
	// are consumed as decoration instead of printed. Void arguments
	// are skipped silently.
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

	// color(r, g, b [, a]): set text/draw color (0-255 each).
	// a is the alpha (0 transparent .. 255 opaque, default 255).
	// Terminals cannot render text alpha, so CUI ignores it.
	// color(rgb): set color with a 0xRRGGBB integer (pget()形式).
	// color() resets the color.
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

	// title(s): ask the terminal to change its title (best effort).
	// NUL would panic the GUI windowing layer, so it is rejected here
	// for both backends.
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

	// input([prompt]): read one line, returned as a string.
	// EOF yields "" (indistinguishable from an empty line by design).
	//
	// GUI is immediate mode instead of line mode: input() returns
	// characters typed since the previous call (IME-committed text
	// included, OS key-repeat for text plus synthesized repeat for
	// control keys, "" when none) and never blocks,
	// so a polling loop builds its own line editor (break on "\r",
	// asc() for codes). A focused inputbox owns the stream (input()
	// reports "" then). GUI takes no arguments: a prompt would spam
	// every poll, so print it with mes() instead.
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
