package gui

import (
	"fmt"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Perspective-correct textured raster (G1): gsquare() and gtritex() share
// one Kage shader and one projective core. Screen-space UVs travel as
// (u/w, v/w, 1/w) in the vertex custom attribute and are divided back in
// the fragment shader, so a quad split into two triangles has no diagonal
// seam (per-triangle affine interpolation would crease there).
//
// Draw calls keep the canvas frame model: validation runs on the script
// goroutine (headless-safe, like the Blit errors) and raster runs on the
// game thread via enqueue, honoring gmode/galpha and the gsel target.

// perspShaderSrc is the shared fragment shader. Pixel units throughout:
// geometry and sampling coordinates are both plain pixels. Out-of-range
// UVs sample transparent (imageSrc0At), so source rects never panic the
// loop goroutine the way a SubImage outside its bounds would.
const perspShaderSrc = `//kage:unit pixels

package main

func Fragment(dstPos vec4, srcPos vec2, color vec4, custom vec4) vec4 {
	q := custom.z
	if q <= 0 {
		return vec4(0)
	}
	uv := vec2(custom.x/q, custom.y/q)
	clr := imageSrc0At(uv)
	clr *= color
	return clr
}
`

// perspMinW floors homogeneous weights: below it 1/w overflows float32
// and the UV quotient degrades to NaN instead of transparent.
const perspMinW = 1e-9

// degenArea2 treats twice-areas below this as empty. A quad or triangle
// animated through zero never errors; it just draws nothing that frame.
const degenArea2 = 1e-9

// perspShader compiles the shared shader once. Game thread only: every
// caller arrives serialized through the draw queue.
func (b *WindowBackend) perspShader() *ebiten.Shader {
	if b.persp == nil && b.perspErr == nil {
		b.persp, b.perspErr = ebiten.NewShader([]byte(perspShaderSrc))
	}
	return b.persp
}

// triArea2 is twice the signed area of (a, b, c).
func triArea2(ax, ay, bx, by, cx, cy float64) float64 {
	return (bx-ax)*(cy-ay) - (cx-ax)*(by-ay)
}

// finiteFloats reports whether every value is a finite number. Script
// values can never be NaN/Inf (producing one is already an error), so
// this is defense in depth for Go-side callers.
func finiteFloats(fs ...float64) bool {
	for _, f := range fs {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
	}
	return true
}

// solveHomography finds row-major H with H[8] == 1 mapping each (su, sv)
// onto (du, dv). It reports false when the four pairs are singular
// (collinear or otherwise degenerate).
func solveHomography(su, sv, du, dv [4]float64) ([9]float64, bool) {
	var m [8][9]float64
	for i := 0; i < 4; i++ {
		u, v, x, y := su[i], sv[i], du[i], dv[i]
		r := 2 * i
		m[r][0], m[r][1], m[r][2] = -u, -v, -1
		m[r][6], m[r][7] = x*u, x*v
		m[r][8] = -x
		m[r+1][3], m[r+1][4], m[r+1][5] = -u, -v, -1
		m[r+1][6], m[r+1][7] = y*u, y*v
		m[r+1][8] = -y
	}
	// Gauss-Jordan with partial pivoting.
	for c := 0; c < 8; c++ {
		p := c
		for r := c + 1; r < 8; r++ {
			if math.Abs(m[r][c]) > math.Abs(m[p][c]) {
				p = r
			}
		}
		if math.Abs(m[p][c]) < 1e-12 {
			return [9]float64{}, false
		}
		m[c], m[p] = m[p], m[c]
		d := m[c][c]
		for k := c; k < 9; k++ {
			m[c][k] /= d
		}
		for r := 0; r < 8; r++ {
			if r == c {
				continue
			}
			if f := m[r][c]; f != 0 {
				for k := c; k < 9; k++ {
					m[r][k] -= f * m[c][k]
				}
			}
		}
	}
	var h [9]float64
	for i := 0; i < 8; i++ {
		h[i] = m[i][8]
	}
	h[8] = 1
	return h, true
}

// quadWs reads the homogeneous weights off H's last row
// (w = h6*u + h7*v + 1) at each source corner.
func quadWs(h [9]float64, su, sv [4]float64) [4]float64 {
	var w [4]float64
	for i := 0; i < 4; i++ {
		w[i] = h[6]*su[i] + h[7]*sv[i] + 1
	}
	return w
}

// perspVert packs one raster vertex: the projective payload goes in
// custom, the global alpha in the color multiplier. The shader applies
// `clr *= color` exactly like the built-in pipeline, so galpha composes
// the same way Blit does (premultiplied-safe).
func perspVert(dx, dy, u, v, w float64, alpha float32) ebiten.Vertex {
	return ebiten.Vertex{
		DstX:   float32(dx),
		DstY:   float32(dy),
		ColorR: 1, ColorG: 1, ColorB: 1, ColorA: alpha,
		Custom0: float32(u / w), Custom1: float32(v / w), Custom2: float32(1 / w),
	}
}

// drawPersp is the shared game-thread raster: verts carry absolute source
// pixels in custom and land on dst. On a shader failure it falls back to
// affine DrawTriangles so a frame still appears instead of vanishing.
func drawPersp(b *WindowBackend, dst, simg *ebiten.Image, vs []ebiten.Vertex, idx []uint16, su, sv []float64, blend ebiten.CompositeMode, alpha float32) {
	if sh := b.perspShader(); sh != nil {
		op := &ebiten.DrawTrianglesShaderOptions{}
		op.Images[0] = simg
		op.CompositeMode = blend
		dst.DrawTrianglesShader(vs, idx, sh, op)
		return
	}
	fb := make([]ebiten.Vertex, len(vs))
	for i, v := range vs {
		fb[i] = ebiten.Vertex{
			DstX: v.DstX, DstY: v.DstY,
			SrcX: float32(su[i]), SrcY: float32(sv[i]),
			ColorR: 1, ColorG: 1, ColorB: 1, ColorA: alpha,
		}
	}
	op := &ebiten.DrawTrianglesOptions{}
	op.CompositeMode = blend
	dst.DrawTriangles(fb, idx, simg, op)
}

// DrawQuad maps the src rectangle onto the dst quad (corners in order,
// clockwise or counter-clockwise) with perspective correction. An empty
// quad is a no-op. Script threads enqueue; Update applies.
func (b *WindowBackend) DrawQuad(src int, sx, sy, sw, sh float64, dx, dy [4]float64) error {
	const name = "gsquare"
	if !b.hasBuf(src) {
		return fmt.Errorf("%s：不明なバッファ %d です", name, src)
	}
	if !finiteFloats(sx, sy, sw, sh, dx[0], dy[0], dx[1], dy[1], dx[2], dy[2], dx[3], dy[3]) {
		return fmt.Errorf("%s：座標は有限の数値である必要があります", name)
	}
	if sw <= 0 || sh <= 0 {
		return fmt.Errorf("%s のサイズは正の値である必要があります。%g x %g が指定されました", name, sw, sh)
	}
	a1 := triArea2(dx[0], dy[0], dx[1], dy[1], dx[2], dy[2])
	a2 := triArea2(dx[0], dy[0], dx[2], dy[2], dx[3], dy[3])
	if math.Abs(a1) < degenArea2 && math.Abs(a2) < degenArea2 {
		return nil
	}
	su := [4]float64{sx, sx + sw, sx + sw, sx}
	sv := [4]float64{sy, sy, sy + sh, sy + sh}
	h, ok := solveHomography(su, sv, dx, dy)
	if !ok {
		return fmt.Errorf("%s：変換先の四角形が縮退しています（頂点の順序を確認してください）", name)
	}
	w := quadWs(h, su, sv)
	for _, wv := range w {
		if !(wv > 0) {
			return fmt.Errorf("%s：変換先の四角形は描画できません（頂点の順序を確認してください）", name)
		}
	}
	sel := b.currentSel()
	blend := b.currentBlend()
	alpha := b.alphaScale()
	b.enqueue(func() {
		vs := []ebiten.Vertex{
			perspVert(dx[0], dy[0], su[0], sv[0], w[0], alpha),
			perspVert(dx[1], dy[1], su[1], sv[1], w[1], alpha),
			perspVert(dx[2], dy[2], su[2], sv[2], w[2], alpha),
			perspVert(dx[3], dy[3], su[3], sv[3], w[3], alpha),
		}
		drawPersp(b, b.ensureTarget(sel), b.ensureTarget(src),
			vs, []uint16{0, 1, 2, 0, 2, 3},
			[]float64{su[0], su[1], su[2], su[3]},
			[]float64{sv[0], sv[1], sv[2], sv[3]}, blend, alpha)
	})
	return nil
}

// DrawTri maps the src triangle onto the dst triangle with perspective
// correction (w holds one homogeneous weight per vertex; all 1 is affine).
// An empty triangle is a no-op. Script threads enqueue; Update applies.
func (b *WindowBackend) DrawTri(src int, su, sv, du, dv [3]float64, w [3]float64) error {
	const name = "gtritex"
	if !b.hasBuf(src) {
		return fmt.Errorf("%s：不明なバッファ %d です", name, src)
	}
	if !finiteFloats(su[0], sv[0], su[1], sv[1], su[2], sv[2],
		du[0], dv[0], du[1], dv[1], du[2], dv[2], w[0], w[1], w[2]) {
		return fmt.Errorf("%s：座標と重みは有限の数値である必要があります", name)
	}
	for _, wv := range w {
		if !(wv >= perspMinW) {
			return fmt.Errorf("%s：重み w は %g 以上の有限値である必要があります", name, perspMinW)
		}
	}
	if math.Abs(triArea2(du[0], dv[0], du[1], dv[1], du[2], dv[2])) < degenArea2 {
		return nil
	}
	sel := b.currentSel()
	blend := b.currentBlend()
	alpha := b.alphaScale()
	b.enqueue(func() {
		vs := []ebiten.Vertex{
			perspVert(du[0], dv[0], su[0], sv[0], w[0], alpha),
			perspVert(du[1], dv[1], su[1], sv[1], w[1], alpha),
			perspVert(du[2], dv[2], su[2], sv[2], w[2], alpha),
		}
		drawPersp(b, b.ensureTarget(sel), b.ensureTarget(src),
			vs, []uint16{0, 1, 2},
			[]float64{su[0], su[1], su[2]},
			[]float64{sv[0], sv[1], sv[2]}, blend, alpha)
	})
	return nil
}
