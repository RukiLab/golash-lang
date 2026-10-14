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

	// pget(x, y): 現在の描画先から1ピクセルを読み取る。
	// 色を 0xRRGGBB 整数（r*65536+g*256+b）で返す。
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

	// pset(x, y): 現在の色で1ピクセルを描く。
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

	// line(x1, y1, x2, y2): 現在の色で1ピクセル幅の直線を引く。
	register("line", 4, 4, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "line")
		if err != nil {
			return Null(), err
		}
		pts := make([]int, 4)
		for i := range pts {
			v, err := needInt("line", args, i, at)
			if err != nil {
				return Null(), err
			}
			pts[i] = int(v)
		}
		wb.StrokeLine(pts[0], pts[1], pts[2], pts[3], wb.Foreground())
		return Null(), nil
	})

	// boxf(): 画面全体を塗りつぶす。boxf(x1, y1, x2, y2): 塗りつぶし矩形（角は正規化される）。
	register("boxf", 0, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "boxf")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			w, h := wb.CanvasSize()
			wb.FillRect(0, 0, w, h, wb.Foreground())
			return Null(), nil
		}
		if len(args) != 4 {
			return Null(), rtErrf(at, "boxf は引数を 0 個または 4 個必要としますが、%d 個が渡されました", len(args))
		}
		pts := make([]int, 4)
		for i := range pts {
			v, err := needInt("boxf", args, i, at)
			if err != nil {
				return Null(), err
			}
			pts[i] = int(v)
		}
		x, w := normSpan(pts[0], pts[2])
		y, h := normSpan(pts[1], pts[3])
		wb.FillRect(x, y, w, h, wb.Foreground())
		return Null(), nil
	})

	// circle(x, y, r [, fill]): 現在の色で円を描く。
	register("circle", 3, 4, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "circle")
		if err != nil {
			return Null(), err
		}
		x, err := needInt("circle", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("circle", args, 1, at)
		if err != nil {
			return Null(), err
		}
		r, err := needInt("circle", args, 2, at)
		if err != nil {
			return Null(), err
		}
		if r < 0 {
			return Null(), argErr("circle", 2, at, "半径は 0 以上である必要があります。%d が指定されました", r)
		}
		fill := false
		if len(args) == 4 {
			f, err := needInt("circle", args, 3, at)
			if err != nil {
				return Null(), err
			}
			fill = f != 0
		}
		wb.Circle(int(x), int(y), int(r), fill, wb.Foreground())
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

func normSpan(a, b int) (int, int) {
	if a <= b {
		return a, b - a
	}
	return b, a - b
}
