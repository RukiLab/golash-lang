// GUI builtins (G6 perspective-correct textured raster).
// Always registered; GUI calls need a window (a GUI backend),
// otherwise they report an error.
package main

import (
	"gsh/gui"
)

func init() {
	// gquad(tex, xs, ys, us, vs, lights): map source quads onto
	// destination quads with perspective correction (the homography is
	// solved per face, so the script never passes weights).
	//
	// xs/ys/us/vs are flat arrays holding four values per face: the
	// destination corners in order (clockwise or counter-clockwise) and
	// the source pixels they sample. lights holds one 0xRRGGBB color per
	// corner, Gouraud-interpolated across the face (four equal colors are
	// flat shading). Arrays of 4N elements draw N faces in one command
	// (capped at 65536); a face with no area draws nothing.
	register("gquad", 6, 6, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gquad")
		if err != nil {
			return Null(), err
		}
		src, err := needInt("gquad", args, 0, at)
		if err != nil {
			return Null(), err
		}
		xs, err := numArrArg("gquad", args, 1, at)
		if err != nil {
			return Null(), err
		}
		ys, err := numArrArg("gquad", args, 2, at)
		if err != nil {
			return Null(), err
		}
		us, err := numArrArg("gquad", args, 3, at)
		if err != nil {
			return Null(), err
		}
		vs, err := numArrArg("gquad", args, 4, at)
		if err != nil {
			return Null(), err
		}
		lights, err := colorArrArg("gquad", args, 5, at)
		if err != nil {
			return Null(), err
		}
		// Four values per face, the same face count everywhere: one
		// rule for both a single face and a batch. All empty is a no-op
		// (a loop that culled every face draws nothing, buffer included),
		// like a degenerate face; anything else misshapen is an error.
		if len(xs) == 0 && len(ys) == 0 && len(us) == 0 && len(vs) == 0 && len(lights) == 0 {
			return Null(), nil
		}
		n := len(xs) / 4
		if n == 0 || len(xs)%4 != 0 || len(ys) != len(xs) || len(us) != len(xs) ||
			len(vs) != len(xs) || len(lights) != len(xs) {
			return Null(), rtErrf(at, "gquad：xs/ys/us/vs/lights は 4 の倍数の同じ長さにしてください（1面あたり4要素）。xs=%d ys=%d us=%d vs=%d lights=%d",
				len(xs), len(ys), len(us), len(vs), len(lights))
		}
		quads := make([]gui.Quad, n)
		for i := 0; i < n; i++ {
			k := i * 4
			q := gui.Quad{
				X: [4]float64{xs[k], xs[k+1], xs[k+2], xs[k+3]},
				Y: [4]float64{ys[k], ys[k+1], ys[k+2], ys[k+3]},
				U: [4]float64{us[k], us[k+1], us[k+2], us[k+3]},
				V: [4]float64{vs[k], vs[k+1], vs[k+2], vs[k+3]},
			}
			for j := 0; j < 4; j++ {
				q.Light[j] = rgb01(lights[k+j])
			}
			quads[i] = q
		}
		if err := wb.DrawQuads(int(src), quads); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}

// numArrArg reads one gquad() coordinate array: an array of int/float
// elements. Rejects other element types with the element index, so a
// mixed batch (one string inside the loop output) is pinpointed.
func numArrArg(name string, args []Value, i int, at Pos) ([]float64, error) {
	arr, err := needArray(name, args, i, at)
	if err != nil {
		return nil, err
	}
	out := make([]float64, len(arr.Elems))
	for j, e := range arr.Elems {
		v, ok := numValue(e)
		if !ok {
			return nil, rtErrf(at, "%s：配列の要素 %d は数値である必要があります。%s が指定されました", name, j, typeNameOf(e))
		}
		out[j] = v
	}
	return out, nil
}

// colorArrArg reads one gquad() lights array: an array of 0xRRGGBB ints.
// Colors follow the pget() convention (color()/objprm() already use it).
func colorArrArg(name string, args []Value, i int, at Pos) ([]int64, error) {
	arr, err := needArray(name, args, i, at)
	if err != nil {
		return nil, err
	}
	out := make([]int64, len(arr.Elems))
	for j, e := range arr.Elems {
		if e.K != KInt {
			return nil, rtErrf(at, "%s：配列の要素 %d は 0x000000〜0xFFFFFF の整数である必要があります。%s が指定されました", name, j, typeNameOf(e))
		}
		if e.I < 0 || e.I > 0xFFFFFF {
			return nil, rtErrf(at, "%s：配列の要素 %d は 0x000000〜0xFFFFFF の整数である必要があります。%d が指定されました", name, j, e.I)
		}
		out[j] = e.I
	}
	return out, nil
}

// numValue unwraps an int/float Value (needFloat works on args; array
// elements need the same acceptance without an args slice).
func numValue(v Value) (float64, bool) {
	switch v.K {
	case KFloat:
		return v.F, true
	case KInt:
		return float64(v.I), true
	}
	return 0, false
}

// rgb01 unpacks a 0xRRGGBB color into 0..1 triplets (pget convention).
func rgb01(c int64) [3]float64 {
	return [3]float64{
		float64((c>>16)&0xFF) / 255,
		float64((c>>8)&0xFF) / 255,
		float64(c&0xFF) / 255,
	}
}
