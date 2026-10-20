// GUI組み込み関数（G2 入力＋音声）。常に登録される。GUI呼び出しには
// ウィンドウ（GUIバックエンド）が必要で、なければエラーを返す。
package main

import (
	"gsh/gui"
)

func init() {
	// getkey(code [, mode]): キーとマウスボタンの状態を取得する。
	// code 0-2 はマウスボタン（0 左、1 中、2 右。clicked/mousedown の
	// 番号付けをそのまま統合）。それ以外は Windows 仮想キーに従う:
	// 8 backspace、9 tab、13 enter、16 shift、17 ctrl、
	// 18 alt、27 esc、32 space、33-36 pageup/pagedown/end/home、37-40
	// 矢印キー、45 insert、46 delete、48-57 数字、65-90 A-Z、112-123
	// F1-F12、186-192/219-222 記号キー。
	// mode 省略時・0 は押されている間 true を返し続けるレベル検出
	// （従来の getkey/mousedown、消費しない）。mode 1 は押した瞬間だけ
	// true になる立上り検出で、呼び出しで消費される（従来の
	// clicked。同一ティック内の複数回呼び出しは2回目以降 false）。
	register("getkey", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "getkey")
		if err != nil {
			return Null(), err
		}
		code, err := needInt("getkey", args, 0, at)
		if err != nil {
			return Null(), err
		}
		mode := int64(0)
		if len(args) == 2 {
			mode, err = needInt("getkey", args, 1, at)
			if err != nil {
				return Null(), err
			}
			if mode != 0 && mode != 1 {
				return Null(), argErr("getkey", 1, at, "0 または 1 で指定してください。%d が指定されました", mode)
			}
		}
		if code >= 0 && code <= 2 {
			btn := int(code)
			if mode == 1 {
				c, err := wb.ClickedButton(btn)
				if err != nil {
					return Null(), rtErrf(at, "%s", err.Error())
				}
				return Bool(c), nil
			}
			d, err := wb.MouseDown(btn)
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Bool(d), nil
		}
		if mode == 1 {
			hit, ok := wb.KeyHit(int(code))
			if !ok {
				return Null(), argErr("getkey", 0, at, "不明なキーコード %d です", code)
			}
			return Bool(hit), nil
		}
		down, ok := wb.KeyDown(int(code))
		if !ok {
			return Null(), argErr("getkey", 0, at, "不明なキーコード %d です", code)
		}
		return Bool(down), nil
	})

	// mousex()/mousey(): ウィンドウピクセルでのカーソル位置。
	register("mousex", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mousex")
		if err != nil {
			return Null(), err
		}
		x, _ := wb.MousePos()
		return Int(int64(x)), nil
	})
	register("mousey", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mousey")
		if err != nil {
			return Null(), err
		}
		_, y := wb.MousePos()
		return Int(int64(y)), nil
	})

	// cursor([v]): 引数なしは IME 入力の照会、ありは設定（0以外 = オン）。
	register("cursor", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "cursor")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			return Bool(wb.CursorVisible()), nil
		}
		v, err := needInt("cursor", args, 0, at)
		if err != nil {
			return Null(), err
		}
		wb.SetCursorVisible(v != 0)
		return Null(), nil
	})

	// mousewheel(): 前回呼び出し以降の垂直ホイール量
	// （消費される。正は上・奥方向）。
	register("mousewheel", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mousewheel")
		if err != nil {
			return Null(), err
		}
		return Int(int64(wb.MouseWheel())), nil
	})

	// padcount(): 接続中のゲームパッド数。
	register("padcount", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "padcount")
		if err != nil {
			return Null(), err
		}
		return Int(int64(wb.PadCount())), nil
	})

	// padbtn(id, btn): 標準配置のボタン（0-16）。
	register("padbtn", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "padbtn")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("padbtn", args, 0, at)
		if err != nil {
			return Null(), err
		}
		btn, err := needInt("padbtn", args, 1, at)
		if err != nil {
			return Null(), err
		}
		down, err := wb.PadButton(int(id), int(btn))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Bool(down), nil
	})

	// padaxis(id, axis): スティック軸 0-3（-1..1）。
	register("padaxis", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "padaxis")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("padaxis", args, 0, at)
		if err != nil {
			return Null(), err
		}
		axis, err := needInt("padaxis", args, 1, at)
		if err != nil {
			return Null(), err
		}
		v, err := wb.PadAxis(int(id), int(axis))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Float(v), nil
	})

	// padname(id): ゲームパッド名。
	register("padname", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "padname")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("padname", args, 0, at)
		if err != nil {
			return Null(), err
		}
		name, err := wb.PadName(int(id))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Str(name), nil
	})

	// touchcount(): 有効なタッチ数。
	register("touchcount", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "touchcount")
		if err != nil {
			return Null(), err
		}
		return Int(int64(wb.TouchCount())), nil
	})

	// touchx(i)/touchy(i): i 番目のタッチ位置（ウィンドウピクセル）。
	register("touchx", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "touchx")
		if err != nil {
			return Null(), err
		}
		i, err := needInt("touchx", args, 0, at)
		if err != nil {
			return Null(), err
		}
		x, _, err := wb.TouchPos(int(i))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(x)), nil
	})
	register("touchy", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "touchy")
		if err != nil {
			return Null(), err
		}
		i, err := needInt("touchy", args, 0, at)
		if err != nil {
			return Null(), err
		}
		_, y, err := wb.TouchPos(int(i))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(y)), nil
	})

	// dropfiles(): 前回呼び出し以降にウィンドウへドロップされた名前
	// （消費される。なければ空）。
	register("dropfiles", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "dropfiles")
		if err != nil {
			return Null(), err
		}
		names := wb.DropFiles()
		elems := make([]Value, len(names))
		for i, s := range names {
			elems[i] = Str(s)
		}
		return ArrayOf(elems), nil
	})

	// dropload(name): ドロップされたファイルをバッファに展開し、その id を返す。
	register("dropload", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "dropload")
		if err != nil {
			return Null(), err
		}
		p, err := needString("dropload", args, 0, at)
		if err != nil {
			return Null(), err
		}
		id, err := wb.DropLoad(p)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(id)), nil
	})

	// screensize(): [ウィンドウ幅, 高さ]（ピクセル）。実際の OS
	// ウィンドウであり、モニターでも screen() の論理キャンバスでもない。
	// resizable() が許可されている間はキャンバスがウィンドウに追従するため、
	// 描画ピクセルでもある。
	register("screensize", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		if _, err := guiBE(in, at, "screensize"); err != nil {
			return Null(), err
		}
		w, h := gui.WindowSize()
		return ArrayOf([]Value{Int(int64(w)), Int(int64(h))}), nil
	})

	// winmove(x, y): ウィンドウを移動する（モニタ左上角からの
	// デスクトップピクセル）。
	register("winmove", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "winmove")
		if err != nil {
			return Null(), err
		}
		x, err := needInt("winmove", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("winmove", args, 1, at)
		if err != nil {
			return Null(), err
		}
		wb.MoveWindow(int(x), int(y))
		return Null(), nil
	})

	// closing(): 閉じるボタンが押されたかどうか。最初の呼出がオプト
	// インになる: closing はループの終了をやめるため、これをポーリング
	// して保存し、それから end() する。
	register("closing", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "closing")
		if err != nil {
			return Null(), err
		}
		return Bool(wb.Closing()), nil
	})

	// fullscreen([v]): 引数なしなら全画面状態を報告（1/0）する。v を
	// 渡すと切り替える（非ゼロ=オン）。
	register("fullscreen", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "fullscreen")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			return Bool(wb.IsFullscreen()), nil
		}
		v, err := needInt("fullscreen", args, 0, at)
		if err != nil {
			return Null(), err
		}
		wb.SetFullscreen(v != 0)
		return Null(), nil
	})

	// resizable([v]): 引数なしならウィンドウをドラッグしてリサイズ
	// できるかを報告（1/0）する。v を渡すと設定する（非ゼロ=オン）。
	// キャンバスは論理サイズを保ち、大きなウィンドウはビューを
	// スケールする。
	register("resizable", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "resizable")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			return Bool(wb.IsResizable()), nil
		}
		v, err := needInt("resizable", args, 0, at)
		if err != nil {
			return Null(), err
		}
		wb.SetResizable(v != 0)
		return Null(), nil
	})

	// mmload(path): wav/mp3/ogg をデコードし、サウンド id を返す。
	register("mmload", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mmload")
		if err != nil {
			return Null(), err
		}
		p, err := needString("mmload", args, 0, at)
		if err != nil {
			return Null(), err
		}
		id, err := wb.MmLoad(in.resolvePath(p))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(id)), nil
	})

	// mmplay(id [, loop]): サウンドを再生する。現在の再生があれば
	// 最初から再開する。
	register("mmplay", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mmplay")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("mmplay", args, 0, at)
		if err != nil {
			return Null(), err
		}
		loop := false
		if len(args) == 2 {
			l, err := needInt("mmplay", args, 1, at)
			if err != nil {
				return Null(), err
			}
			loop = l != 0
		}
		if err := wb.MmPlay(int(id), loop); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// mmstop([id]): 1つのサウンドを停止する。引数なしならすべて。
	// 未知の id は無音で何もしない。
	register("mmstop", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mmstop")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			wb.MmStop(nil)
			return Null(), nil
		}
		id, err := needInt("mmstop", args, 0, at)
		if err != nil {
			return Null(), err
		}
		i := int(id)
		wb.MmStop(&i)
		return Null(), nil
	})

	// mmvol(id, vol): サウンドの音量をパーセントで（0..100、既定 100）。
	register("mmvol", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mmvol")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("mmvol", args, 0, at)
		if err != nil {
			return Null(), err
		}
		vol, err := needInt("mmvol", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.MmVolume(int(id), int(vol)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
