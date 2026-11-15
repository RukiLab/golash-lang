// GUI組み込み関数（G2 入力＋音声）。常に登録される。GUI呼び出しには
// ウィンドウ（GUIバックエンド）が必要で、なければエラーを返す。
package main

func init() {
	// getkey(code [, mode]): キーとマウスボタンの状態を取得する。
	// code 0-2 はマウスボタン（0 左、1 中、2 右。clicked/mousedown の
	// 番号付けをそのまま統合）。それ以外は Windows 仮想キーに従う:
	// 8 backspace、9 tab、13 enter、16 shift、17 ctrl、
	// 18 alt、27 esc、32 space、33-36 pageup/pagedown/end/home、37-40
	// 矢印キー、45 insert、46 delete、48-57 数字、65-90 A-Z、112-123
	// F1-F12、186-192/219-222 記号キー。
	// mode 省略時・0 は押されている間 1 を返し続けるレベル検出
	// （従来の getkey/mousedown、消費しない）。1 は押した瞬間だけ
	// 1 になる立上り検出、2 は離した瞬間だけ 1 になる立下り検出で、
	// いずれも呼び出しで消費される（従来の clicked。同一ティック内の
	// 複数回呼び出しは2回目以降 0）。
	register("getkey", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "getkey")
		if err != nil {
			return Null(), err
		}
		code, err := needInt("getkey", args, 0, at)
		if err != nil {
			return Null(), err
		}
		mode := 0
		if len(args) == 2 {
			mode, err = needInputMode("getkey", args, 1, at)
			if err != nil {
				return Null(), err
			}
		}
		if code >= 0 && code <= 2 {
			btn := int(code)
			switch mode {
			case 1:
				c, err := wb.ClickedButton(btn)
				if err != nil {
					return Null(), rtErrf(at, "%s", err.Error())
				}
				return IntOfBool(c), nil
			case 2:
				r, err := wb.MouseReleased(btn)
				if err != nil {
					return Null(), rtErrf(at, "%s", err.Error())
				}
				return IntOfBool(r), nil
			default:
				d, err := wb.MouseDown(btn)
				if err != nil {
					return Null(), rtErrf(at, "%s", err.Error())
				}
				return IntOfBool(d), nil
			}
		}
		switch mode {
		case 1:
			hit, ok := wb.KeyHit(int(code))
			if !ok {
				return Null(), argErr("getkey", 0, at, "不明なキーコード %d です", code)
			}
			return IntOfBool(hit), nil
		case 2:
			rel, ok := wb.KeyReleased(int(code))
			if !ok {
				return Null(), argErr("getkey", 0, at, "不明なキーコード %d です", code)
			}
			return IntOfBool(rel), nil
		default:
			down, ok := wb.KeyDown(int(code))
			if !ok {
				return Null(), argErr("getkey", 0, at, "不明なキーコード %d です", code)
			}
			return IntOfBool(down), nil
		}
	})

	// mouse(): ウィンドウピクセルでのカーソル位置を [x, y] で返す。
	register("mouse", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mouse")
		if err != nil {
			return Null(), err
		}
		x, y := wb.MousePos()
		return ArrayOf([]Value{Int(int64(x)), Int(int64(y))}), nil
	})

	// cursor([v]): マウスカーソルの表示状態を取得・変更する。
	// 引数なしは現在の表示状態を 1/0 で返す。ありは整数で指定する
	// （0 = 非表示、非0 = 表示）。文字列は受け付けない。
	register("cursor", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "cursor")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			return IntOfBool(wb.CursorVisible()), nil
		}
		v, err := needTruthy("cursor", args, 0, at)
		if err != nil {
			return Null(), err
		}
		wb.SetCursorVisible(v)
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

	// pad(): 接続中のゲームパッド名を配列で返す。要素数が接続数。
	register("pad", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "pad")
		if err != nil {
			return Null(), err
		}
		n := wb.PadCount()
		elems := make([]Value, n)
		for i := 0; i < n; i++ {
			name, err := wb.PadName(i)
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			elems[i] = Str(name)
		}
		return ArrayOf(elems), nil
	})

	// padbtn(id, btn [, mode]): 標準配置のボタン（0-16）。
	// mode 省略時・0 は押されている間 1 を返し続けるレベル検出。
	// 1 は押した瞬間だけ 1 になる立上り検出、2 は離した瞬間だけ 1 に
	// なる立下り検出で、いずれも呼び出しで消費される。
	register("padbtn", 2, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
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
		mode := 0
		if len(args) == 3 {
			mode, err = needInputMode("padbtn", args, 2, at)
			if err != nil {
				return Null(), err
			}
		}
		switch mode {
		case 1:
			hit, err := wb.PadButtonHit(int(id), int(btn))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return IntOfBool(hit), nil
		case 2:
			rel, err := wb.PadButtonReleased(int(id), int(btn))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return IntOfBool(rel), nil
		default:
			down, err := wb.PadButton(int(id), int(btn))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return IntOfBool(down), nil
		}
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

	// touch(): 接触中のタッチ位置を [[x, y], ...] で返す（ウィンドウピクセル）。
	register("touch", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "touch")
		if err != nil {
			return Null(), err
		}
		n := wb.TouchCount()
		elems := make([]Value, n)
		for i := 0; i < n; i++ {
			x, y, err := wb.TouchPos(i)
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			elems[i] = ArrayOf([]Value{Int(int64(x)), Int(int64(y))})
		}
		return ArrayOf(elems), nil
	})

	// dropload(): 前回呼び出し以降にウィンドウへドロップされた名前
	// （消費される。なければ空）を返す。
	// dropload(name): ドロップされたファイルをバッファに展開し、その id を返す。
	register("dropload", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "dropload")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			names := wb.DropNames()
			elems := make([]Value, len(names))
			for i, s := range names {
				elems[i] = Str(s)
			}
			return ArrayOf(elems), nil
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

	// closing(): 閉じるボタンが押されたかどうか。最初の呼出がオプト
	// インになる: closing はループの終了をやめるため、これをポーリング
	// して保存し、それから end() する。
	register("closing", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "closing")
		if err != nil {
			return Null(), err
		}
		return IntOfBool(wb.Closing()), nil
	})

	// fullscreen([v]): 引数なしなら全画面状態を報告（1/0）する。v を
	// 渡すと切り替える（整数：0=オフ、非0=オン）。
	register("fullscreen", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "fullscreen")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			return IntOfBool(wb.IsFullscreen()), nil
		}
		v, err := needTruthy("fullscreen", args, 0, at)
		if err != nil {
			return Null(), err
		}
		wb.SetFullscreen(v)
		return Null(), nil
	})

	// resizable([v]): 引数なしならウィンドウをドラッグしてリサイズ
	// できるかを報告（1/0）する。v を渡すと設定する（整数：
	// 0=オフ、非0=オン）。
	// キャンバスは論理サイズを保ち、大きなウィンドウはビューを
	// スケールする。
	register("resizable", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "resizable")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			return IntOfBool(wb.IsResizable()), nil
		}
		v, err := needTruthy("resizable", args, 0, at)
		if err != nil {
			return Null(), err
		}
		wb.SetResizable(v)
		return Null(), nil
	})

	// sndload(path): wav/mp3/ogg をデコードし、サウンド id を返す。
	register("sndload", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "sndload")
		if err != nil {
			return Null(), err
		}
		p, err := needString("sndload", args, 0, at)
		if err != nil {
			return Null(), err
		}
		id, err := wb.SndLoad(in.resolvePath(p))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(id)), nil
	})

	// sndplay(id [, loop]): サウンドを再生する。現在の再生があれば
	// 最初から再開する。loop が非0の場合はループ再生する。
	register("sndplay", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "sndplay")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("sndplay", args, 0, at)
		if err != nil {
			return Null(), err
		}
		loop := false
		if len(args) == 2 {
			var err error
			loop, err = needTruthy("sndplay", args, 1, at)
			if err != nil {
				return Null(), err
			}
		}
		if err := wb.SndPlay(int(id), loop); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// sndstop([id]): 1つのサウンドを停止する。引数なしならすべて。
	// 未知の id は無音で何もしない。
	register("sndstop", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "sndstop")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			wb.SndStop(nil)
			return Null(), nil
		}
		id, err := needInt("sndstop", args, 0, at)
		if err != nil {
			return Null(), err
		}
		i := int(id)
		wb.SndStop(&i)
		return Null(), nil
	})

	// sndvol(id, vol): サウンドの音量をパーセントで（0..100、既定 100）。
	register("sndvol", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "sndvol")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("sndvol", args, 0, at)
		if err != nil {
			return Null(), err
		}
		vol, err := needInt("sndvol", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.SndVolume(int(id), int(vol)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// sndpitch(id, pitch): サウンドのピッチ（再生速度）をパーセントで（25..400、既定 100 = 原速）。
	// 50 で1オクターブ低く半分の速さ、200 で1オクターブ高く倍速になる。
	register("sndpitch", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "sndpitch")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("sndpitch", args, 0, at)
		if err != nil {
			return Null(), err
		}
		pitch, err := needInt("sndpitch", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.SndPitch(int(id), int(pitch)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}

// needInputMode は getkey/padbtn の mode 引数を読む（0=レベル検出、
// 1=押した瞬間、2=離した瞬間）。範囲外はエラー。
func needInputMode(name string, args []Value, i int, at Pos) (int, error) {
	mode, err := needInt(name, args, i, at)
	if err != nil {
		return 0, err
	}
	if mode < 0 || mode > 2 {
		return 0, argErr(name, i, at, "mode は 0（レベル）、1（押した瞬間）、2（離した瞬間）のいずれかである必要があります。%d が指定されました", mode)
	}
	return int(mode), nil
}
