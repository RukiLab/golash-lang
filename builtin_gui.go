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

func init() {
	// screen(w, h): ウィンドウをリサイズしキャンバスを再初期化する。
	register("screen", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "screen")
		if err != nil {
			return Null(), err
		}
		w, err := needInt("screen", args, 0, at)
		if err != nil {
			return Null(), err
		}
		h, err := needInt("screen", args, 1, at)
		if err != nil {
			return Null(), err
		}
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

	// buffer(id, w, h): 空の描画バッファを作成（または置換）する。
	// 表示キャンバス（id 0）は screen() のまま。buffer() は
	// スクリプトが gsel() で切り替えるオフスクリーン id を管理する。
	register("buffer", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "buffer")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("buffer", args, 0, at)
		if err != nil {
			return Null(), err
		}
		w, err := needInt("buffer", args, 1, at)
		if err != nil {
			return Null(), err
		}
		h, err := needInt("buffer", args, 2, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.NewBuffer(int(id), int(w), int(h)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// pget(): pos() の位置から1ピクセルを読み取る。
	// 色を 0xRRGGBB 整数（r*65536+g*256+b）で返す。範囲外はエラー。
	register("pget", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "pget")
		if err != nil {
			return Null(), err
		}
		x, y := wb.CursorPixels()
		c, err := wb.GetPixel(x, y)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(c[0])*65536 + int64(c[1])*256 + int64(c[2])), nil
	})

	// pset(): pos() の位置に現在の色で1ピクセルを描く。
	register("pset", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "pset")
		if err != nil {
			return Null(), err
		}
		x, y := wb.CursorPixels()
		wb.SetPixel(x, y, wb.Foreground())
		return Null(), nil
	})

	// line(x2, y2): pos() の位置から (x2, y2) へ現在の色で
	// 1ピクセル幅の直線を引く。pos() 自体は移動しない。
	register("line", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "line")
		if err != nil {
			return Null(), err
		}
		x1, y1 := wb.CursorPixels()
		x2, err := needInt("line", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y2, err := needInt("line", args, 1, at)
		if err != nil {
			return Null(), err
		}
		wb.StrokeLine(x1, y1, int(x2), int(y2), wb.Foreground())
		return Null(), nil
	})

	// rect(): 画面全体を塗りつぶす。rect(w, h): pos() の位置を
	// 左上として w x h の矩形を塗りつぶす（負の辺は逆向きに正規化される）。
	register("rect", 0, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "rect")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			w, h := wb.CanvasSize()
			wb.FillRect(0, 0, w, h, wb.Foreground())
			return Null(), nil
		}
		if len(args) != 2 {
			return Null(), rtErrf(at, "rect は引数を 0 個または 2 個必要としますが、%d 個が渡されました", len(args))
		}
		w, err := needInt("rect", args, 0, at)
		if err != nil {
			return Null(), err
		}
		h, err := needInt("rect", args, 1, at)
		if err != nil {
			return Null(), err
		}
		x, y := wb.CursorPixels()
		if w < 0 {
			x += int(w)
			w = -w
		}
		if h < 0 {
			y += int(h)
			h = -h
		}
		wb.FillRect(x, y, int(w), int(h), wb.Foreground())
		return Null(), nil
	})

	// ellipse(w, h [, fill]): pos() の位置を中心に全幅 w・全高 h の
	// 楕円を現在の色で描く（w == h で真円）。w・h は 0 以上。
	register("ellipse", 2, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "ellipse")
		if err != nil {
			return Null(), err
		}
		w, err := needInt("ellipse", args, 0, at)
		if err != nil {
			return Null(), err
		}
		h, err := needInt("ellipse", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if w < 0 || h < 0 {
			return Null(), argErr("ellipse", 0, at, "大きさは 0 以上である必要があります。%d x %d が指定されました", w, h)
		}
		fill := false
		if len(args) == 3 {
			f, err := needInt("ellipse", args, 2, at)
			if err != nil {
				return Null(), err
			}
			fill = f != 0
		}
		x, y := wb.CursorPixels()
		wb.Ellipse(x, y, int(w), int(h), fill, wb.Foreground())
		return Null(), nil
	})

	// gcopy(src, sx, sy, w, h [, zx, zy [, deg]]): バッファの
	// 領域をカーソル位置に転送する。引数5個は単純コピー。zx, zy は
	// 拡大縮小（旧 gzoom）。deg はさらに領域中心を軸に回転し
	// （旧 grotate）、中心をカーソル位置に保つ。
	register("gcopy", 5, 8, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gcopy")
		if err != nil {
			return Null(), err
		}
		if len(args) == 6 {
			return Null(), rtErrf(at, "gcopy は引数を 5 個、7 個または 8 個必要としますが、%d 個が渡されました", len(args))
		}
		nums := make([]int, 5)
		for i := range nums {
			v, err := needInt("gcopy", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = int(v)
		}
		if nums[3] <= 0 || nums[4] <= 0 {
			return Null(), rtErrf(at, "gcopy のサイズは正の値である必要があります。%d x %d が指定されました", nums[3], nums[4])
		}
		dx, dy := wb.CursorPixels()
		if len(args) == 5 {
			if err := wb.Blit(nums[0], nums[1], nums[2], nums[3], nums[4], dx, dy); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Null(), nil
		}
		zx, err := needFloat("gcopy", args, 5, at)
		if err != nil {
			return Null(), err
		}
		zy, err := needFloat("gcopy", args, 6, at)
		if err != nil {
			return Null(), err
		}
		if len(args) == 7 {
			if err := wb.BlitScaled(nums[0], nums[1], nums[2], nums[3], nums[4], zx, zy, dx, dy); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Null(), nil
		}
		deg, err := needFloat("gcopy", args, 7, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.BlitRotate(nums[0], nums[1], nums[2], nums[3], nums[4], deg, zx, zy, dx, dy); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// gmode(mode): 0 通常描画、1 加算合成。
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
