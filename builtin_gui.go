// GUI組み込み関数（G1 描画）。常に登録される。GUI呼び出しには
// ウィンドウ（GUIバックエンド）が必要で、なければエラーを返す。
package main

import (
	"gsh/gui"
)

// guiBE はウィンドウバックエンドを取得し、なければ整形済みエラーを返す。
func guiBE(in *Interp, at Pos, name string) (*gui.WindowBackend, error) {
	wb, ok := in.Backend().(*gui.WindowBackend)
	if !ok {
		return nil, rtErrf(at, "%s は GUI ビルドが必要です", name)
	}
	return wb, nil
}

// guiResizeScreen は screen(w, h) / screen(0, w, h) の共通処理。
func guiResizeScreen(wb *gui.WindowBackend, w, h int64, at Pos) (Value, error) {
	if w <= 0 || h <= 0 {
		return Null(), rtErrf(at, "screen のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	// 巨大なキャンバスはループ用ゴルーチン内でエンジン内部でパニックし（捕捉不可）、
	// ここでスクリプトエラーとして上限を設ける。
	if w > int64(gui.MaxImageDim) || h > int64(gui.MaxImageDim) {
		return Null(), rtErrf(at, "screen のサイズは上限 %d x %d までです", gui.MaxImageDim, gui.MaxImageDim)
	}
	wb.ResizeCanvas(int(w), int(h))
	return Null(), nil
}

func init() {
	// screen(...) はウィンドウと描画バッファを引数の個数で切り替える:
	//   screen()        → [OSウィンドウ幅, 高さ]
	//   screen(id)      → バッファ id の [幅, 高さ]（0 はメインキャンバス）
	//   screen(w, h)    → ウィンドウ/メインキャンバスを w x h にリサイズ
	//   screen(id,w,h)  → バッファ id を作成・置換（0 はメインのリサイズ）
	register("screen", 0, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "screen")
		if err != nil {
			return Null(), err
		}
		switch len(args) {
		case 0:
			w, h := gui.WindowSize()
			return ArrayOf([]Value{Int(int64(w)), Int(int64(h))}), nil
		case 1:
			id, err := needInt("screen", args, 0, at)
			if err != nil {
				return Null(), err
			}
			w, h, err := wb.BufferSize(int(id))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return ArrayOf([]Value{Int(int64(w)), Int(int64(h))}), nil
		case 2:
			w, err := needInt("screen", args, 0, at)
			if err != nil {
				return Null(), err
			}
			h, err := needInt("screen", args, 1, at)
			if err != nil {
				return Null(), err
			}
			return guiResizeScreen(wb, w, h, at)
		default:
			id, err := needInt("screen", args, 0, at)
			if err != nil {
				return Null(), err
			}
			w, err := needInt("screen", args, 1, at)
			if err != nil {
				return Null(), err
			}
			h, err := needInt("screen", args, 2, at)
			if err != nil {
				return Null(), err
			}
			if id == 0 {
				return guiResizeScreen(wb, w, h, at)
			}
			if err := wb.NewBuffer(int(id), int(w), int(h)); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Null(), nil
		}
	})

	// gsel(id): 描画バッファを切り替える（0 = メイン、表示対象）。
	register("gsel", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gsel")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("gsel", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.SelectTarget(int(id)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// pget(x, y): 指定座標から1ピクセルを読み取る。
	// 色を 0xRRGGBB 整数（r*65536+g*256+b）で返す。範囲外はエラー。
	register("pget", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "pget")
		if err != nil {
			return Null(), err
		}
		x, err := needInt("pget", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("pget", args, 1, at)
		if err != nil {
			return Null(), err
		}
		c, err := wb.GetPixel(int(x), int(y))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(c[0])*65536 + int64(c[1])*256 + int64(c[2])), nil
	})

	// pset(x, y): 指定座標に現在の色で1ピクセルを描く。
	register("pset", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "pset")
		if err != nil {
			return Null(), err
		}
		x, err := needInt("pset", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("pset", args, 1, at)
		if err != nil {
			return Null(), err
		}
		wb.SetPixel(int(x), int(y), wb.Foreground())
		return Null(), nil
	})

	// line(x1, y1, x2, y2): (x1, y1) から (x2, y2) へ現在の色で
	// 1ピクセル幅の直線を引く。
	register("line", 4, 4, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "line")
		if err != nil {
			return Null(), err
		}
		nums := make([]int, 4)
		for i := range nums {
			v, err := needInt("line", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = int(v)
		}
		wb.StrokeLine(nums[0], nums[1], nums[2], nums[3], wb.Foreground())
		return Null(), nil
	})

	// rect(): 画面全体を塗りつぶす。rect(x, y, w, h): (x, y) を
	// 左上として w x h の矩形を塗りつぶす（負の辺は逆向きに正規化される）。
	register("rect", 0, 4, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "rect")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			w, h := wb.CanvasSize()
			wb.FillRect(0, 0, w, h, wb.Foreground())
			return Null(), nil
		}
		if len(args) != 4 {
			return Null(), rtErrf(at, "rect は引数を 0 個または 4 個必要としますが、%d 個が渡されました", len(args))
		}
		nums := make([]int64, 4)
		for i := range nums {
			v, err := needInt("rect", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = v
		}
		x, y, w, h := nums[0], nums[1], nums[2], nums[3]
		if w < 0 {
			x += w
			w = -w
		}
		if h < 0 {
			y += h
			h = -h
		}
		wb.FillRect(int(x), int(y), int(w), int(h), wb.Foreground())
		return Null(), nil
	})

	// ellipse(x, y, w, h [, fill]): (x, y) を中心に全幅 w・全高 h の
	// 楕円を現在の色で描く（w == h で真円）。中心基準のため負の w/h は
	// 絶対値と同じ形になる。w/h が 0 は何も描画しない。
	// fill が非0なら塗り潰し（0=false、非0=true）。
	register("ellipse", 4, 5, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "ellipse")
		if err != nil {
			return Null(), err
		}
		x, err := needInt("ellipse", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("ellipse", args, 1, at)
		if err != nil {
			return Null(), err
		}
		w, err := needInt("ellipse", args, 2, at)
		if err != nil {
			return Null(), err
		}
		h, err := needInt("ellipse", args, 3, at)
		if err != nil {
			return Null(), err
		}
		// 負の w/h は中心基準のため絶対値と同じ形になる（バックエンドで吸収）。
		// 0 は何も描画しない（エラーにしない）。
		fill := false
		if len(args) == 5 {
			if args[4].K != KInt {
				return Null(), argErr("ellipse", 4, at, "整数（0=false、非0=true）である必要があります。%s が指定されました", typeNameOf(args[4]))
			}
			fill = args[4].I != 0
		}
		wb.Ellipse(int(x), int(y), int(w), int(h), fill, wb.Foreground())
		return Null(), nil
	})

	// gcopy(src, sx, sy, w, h, dx, dy [, dw, dh]): バッファの
	// 領域を (dx, dy) に転送する。引数7個は等倍コピー。dw, dh は
	// コピー先サイズ。コピー元は (sx, sy) を角とした |w| x |h|（負は
	// 逆向きに正規化）、コピー先は (dx, dy) を角とした |dw| x |dh|
	//（等倍時は |w| x |h|。負は逆向きに正規化した上で反転描画）。
	// w/h/dw/dh が 0 はエラー。
	register("gcopy", 7, 9, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gcopy")
		if err != nil {
			return Null(), err
		}
		if len(args) == 8 {
			return Null(), rtErrf(at, "gcopy は引数を 7 個または 9 個必要としますが、%d 個が渡されました", len(args))
		}
		nums := make([]int, 7)
		for i := range nums {
			v, err := needInt("gcopy", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = int(v)
		}
		if nums[3] == 0 || nums[4] == 0 {
			return Null(), rtErrf(at, "gcopy のサイズは 0 以外である必要があります。%d x %d が指定されました", nums[3], nums[4])
		}
		dx, dy := nums[5], nums[6]
		if len(args) == 7 {
			if err := wb.Blit(nums[0], nums[1], nums[2], nums[3], nums[4], dx, dy); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Null(), nil
		}
		dw, err := needInt("gcopy", args, 7, at)
		if err != nil {
			return Null(), err
		}
		dh, err := needInt("gcopy", args, 8, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.BlitScaled(nums[0], nums[1], nums[2], nums[3], nums[4], int(dw), int(dh), dx, dy); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// gmode(mode): 0 通常描画、1 加算合成、2 乗算合成。
	register("gmode", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gmode")
		if err != nil {
			return Null(), err
		}
		m, err := needInt("gmode", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.SetBlend(int(m)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// picload(path): 画像を新しいバッファに読み込み、その id を返す。
	register("picload", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "picload")
		if err != nil {
			return Null(), err
		}
		p, err := needString("picload", args, 0, at)
		if err != nil {
			return Null(), err
		}
		id, err := wb.PicLoad(in.resolvePath(p))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(id)), nil
	})

}
