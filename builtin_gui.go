// GUI builtins (G1 drawing). Always registered; GUI calls need a
// window (a GUI backend), otherwise they report an error.
package main

import (
	"gsh/gui"
)

// guiBE fetches the window backend or reports a clean error.
func guiBE(in *Interp, at Pos, name string) (*gui.WindowBackend, error) {
	wb, ok := in.Backend().(*gui.WindowBackend)
	if !ok {
		return nil, rtErrf(at, "%s は GUI ビルドが必要です", name)
	}
	return wb, nil
}

func init() {
	// screen(w, h): resize the window and re-init the canvas.
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
		// Oversized canvases panic inside the engine on the loop
		// goroutine (uncatchable), so cap them here with a script error.
		if w > int64(gui.MaxImageDim) || h > int64(gui.MaxImageDim) {
			return Null(), rtErrf(at, "screen のサイズは上限 %d x %d までです", gui.MaxImageDim, gui.MaxImageDim)
		}
		wb.ResizeCanvas(int(w), int(h))
		return Null(), nil
	})

	// gsel(id): switch the draw buffer (0 = main, visible).
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

	// buffer(id, w, h): create (or replace) an empty drawing buffer.
	// The visible canvas (id 0) stays with screen(); buffer() owns the
	// off-screen ids a script gsel()s into.
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

	// pget(x, y): read one pixel from the current target.
	// Returns the color as a 0xRRGGBB integer (r*65536+g*256+b).
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

	// pset(x, y): one pixel in the current color.
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

	// line(x1, y1, x2, y2): 1px line in the current color.
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

	// boxf(): filled fullscreen. boxf(x1, y1, x2, y2): filled rectangle (corners normalized).
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

	// circle(x, y, r [, fill]): circle in the current color.
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

	// gcopy(src, sx, sy, w, h [, zx, zy [, deg]]): blit a buffer
	// region at the cursor. 5 args is a plain copy; zx, zy scale
	// it (former gzoom); deg additionally rotates about the region
	// center (former grotate), keeping the center at the cursor.
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

	// gmode(mode): 0 normal, 1 additive.
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

	// picload(path): load an image into a new buffer, return its id.
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
