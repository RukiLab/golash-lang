// GUI builtins (G6 perspective-correct textured raster).
// Always registered; GUI calls need a window (a GUI backend),
// otherwise they report an error.
package main

func init() {
	// gsquare(src, sx, sy, sw, sh, x0, y0, x1, y1, x2, y2, x3, y3):
	// map a buffer rectangle onto a free quad (corners in order,
	// clockwise or counter-clockwise) with perspective correction.
	// An empty quad draws nothing; a self-intersecting or otherwise
	// unrepresentable quad is an error.
	register("gsquare", 13, 13, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gsquare")
		if err != nil {
			return Null(), err
		}
		src, err := needInt("gsquare", args, 0, at)
		if err != nil {
			return Null(), err
		}
		rect := make([]int, 4)
		for i := range rect {
			v, err := needInt("gsquare", args, i+1, at)
			if err != nil {
				return Null(), err
			}
			rect[i] = int(v)
		}
		dst := make([]float64, 8)
		for i := range dst {
			v, err := needFloat("gsquare", args, i+5, at)
			if err != nil {
				return Null(), err
			}
			dst[i] = v
		}
		dx := [4]float64{dst[0], dst[2], dst[4], dst[6]}
		dy := [4]float64{dst[1], dst[3], dst[5], dst[7]}
		if err := wb.DrawQuad(int(src),
			float64(rect[0]), float64(rect[1]), float64(rect[2]), float64(rect[3]),
			dx, dy); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// gtritex(src, sx0, sy0, sx1, sy1, sx2, sy2,
	//         dx0, dy0, dx1, dy1, dx2, dy2 [, w0, w1, w2]):
	// map a buffer triangle onto a screen triangle with perspective
	// correction. Without w the mapping is affine; with per-vertex w
	// (all > 0) it is projective. An empty triangle draws nothing.
	register("gtritex", 13, 16, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gtritex")
		if err != nil {
			return Null(), err
		}
		if len(args) == 14 || len(args) == 15 {
			return Null(), rtErrf(at, "gtritex は引数を 13 個または 16 個必要としますが、%d 個が渡されました", len(args))
		}
		src, err := needInt("gtritex", args, 0, at)
		if err != nil {
			return Null(), err
		}
		pts := make([]float64, 12)
		for i := range pts {
			v, err := needFloat("gtritex", args, i+1, at)
			if err != nil {
				return Null(), err
			}
			pts[i] = v
		}
		w := [3]float64{1, 1, 1}
		if len(args) == 16 {
			for i := range w {
				v, err := needFloat("gtritex", args, i+13, at)
				if err != nil {
					return Null(), err
				}
				w[i] = v
			}
		}
		su := [3]float64{pts[0], pts[2], pts[4]}
		sv := [3]float64{pts[1], pts[3], pts[5]}
		du := [3]float64{pts[6], pts[8], pts[10]}
		dv := [3]float64{pts[7], pts[9], pts[11]}
		if err := wb.DrawTri(int(src), su, sv, du, dv, w); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
