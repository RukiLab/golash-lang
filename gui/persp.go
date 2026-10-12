package gui

import (
	"fmt"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Perspective-correct textured raster (G1): gquad() draws textured
// quads through one Kage shader and one projective core. Screen-space UVs
// travel as (u/w, v/w, 1/w) in the vertex custom attribute and are
// divided back in the fragment shader, so a quad split into two
// triangles has no diagonal seam (per-triangle affine interpolation
// would crease there).
//
// Draw calls keep the canvas frame model: validation runs on the script
// goroutine (headless-safe, like the Blit errors) and raster runs on the
// game thread via enqueue, honoring gmode/galpha and the gsel target.

// perspShaderSrc is the shared fragment shader. Pixel units throughout:
// geometry and sampling coordinates are both plain pixels. Out-of-range
// UVs sample transparent (imageSrc0At), so source rects never panic the
// loop goroutine the way a SubImage outside its bounds would. The source
// image may live anywhere in Ebiten's texture atlas, so imageSrc0Origin
// is added (a standalone image reports (0,0), which keeps pixel-exact
// sampling).
const perspShaderSrc = `//kage:unit pixels

package main

func Fragment(dstPos vec4, srcPos vec2, color vec4, custom vec4) vec4 {
	q := custom.z
	if q <= 0 {
		return vec4(0)
	}
	uv := vec2(custom.x/q, custom.y/q) + imageSrc0Origin()
	clr := imageSrc0At(uv)
	clr *= color
	return clr
}
`

// degenArea2 treats twice-areas below this as empty. A quad or triangle
// animated through zero never errors; it just draws nothing that frame.
const degenArea2 = 1e-9

// maxQuadsPerCall caps one gquad() command's faces. Generous on purpose
// (the script arrays themselves stop at dim()'s element cap), but bounded
// so a runaway loop cannot enqueue unbounded vertex memory in one frame.
const maxQuadsPerCall = 65536

// maxVertsPerDraw is the vertex ceiling of a single DrawTrianglesShader:
// indices are uint16, so a face count past 1/4 of it needs chunking
// (which is transparent to the script).
const maxVertsPerDraw = 65536

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
// custom, the tint in the color multiplier. The shader applies
// `clr *= color` exactly like the built-in pipeline, so per-vertex tints
// interpolate across the triangle (Gouraud) and galpha composes the same
// way Blit does (premultiplied-safe). col is (r, g, b, a) with rgb in
// 0..1; flat shading is just three equal vertices.
func perspVert(dx, dy, u, v, w float64, col [4]float32) ebiten.Vertex {
	return ebiten.Vertex{
		DstX:   float32(dx),
		DstY:   float32(dy),
		ColorR: col[0], ColorG: col[1], ColorB: col[2], ColorA: col[3],
		Custom0: float32(u / w), Custom1: float32(v / w), Custom2: float32(1 / w),
	}
}

// drawPersp is the shared game-thread raster: verts carry absolute source
// pixels in custom and land on dst. On a shader failure it falls back to
// affine DrawTriangles so a frame still appears instead of vanishing.
// (Reliability fallback only: no user-facing affine mode is offered.)
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

// Quad is one perspective-correct textured face: four destination
// corners in order (clockwise or counter-clockwise), the four source
// pixels they sample, and one 0..1 rgb triplet per corner (white leaves
// the texture unmodified; four equal triplets are flat shading).
type Quad struct {
	X, Y, U, V [4]float64
	Light      [4][3]float64
}

// finite reports whether every coordinate and light of the quad is
// finite. Script values can never be NaN/Inf (producing one is already an
// error), so this is defense in depth for Go-side callers.
func (q Quad) finite() bool {
	for _, arr := range [][4]float64{q.X, q.Y, q.U, q.V} {
		for _, v := range arr {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return false
			}
		}
	}
	for _, c := range q.Light {
		for _, v := range c {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return false
			}
		}
	}
	return true
}

// DrawQuads maps every quad's source corners onto its destination
// corners with perspective correction: the homography is solved per
// quad, so callers never supply w (an affine mapping is just the case
// where all weights come out 1). An empty quad draws nothing; a quad
// whose homography is singular, or whose weights are not all positive,
// is an error.
//
// The batch is atomic and rides the frame like any other canvas
// command: every quad validates before anything is staged, and the faces
// land on the game thread as one command (chunked into a few draw calls
// behind the scenes, since indices are 16-bit).
func (b *WindowBackend) DrawQuads(src int, quads []Quad) error {
	const name = "gquad"
	// Cheap sanity first: a face count past the cap is rejected before
	// anything else, so a runaway loop fails on the count, not on the
	// (also broken) rest of the call.
	if len(quads) > maxQuadsPerCall {
		return fmt.Errorf("%s：1度に描画できるのは %d 面までです。%d 面が指定されました", name, maxQuadsPerCall, len(quads))
	}
	if !b.hasBuf(src) {
		return fmt.Errorf("%s：不明なバッファ %d です", name, src)
	}
	// face is one validated, drawable quad: solved weights and the
	// per-corner color multiplier the shader expects.
	type face struct {
		x, y, u, v, w [4]float64
		light         [4][3]float32
	}
	faces := make([]face, 0, len(quads))
	for i, q := range quads {
		if !q.finite() {
			return fmt.Errorf("%s：座標は有限の数値である必要があります", name)
		}
		a1 := triArea2(q.X[0], q.Y[0], q.X[1], q.Y[1], q.X[2], q.Y[2])
		a2 := triArea2(q.X[0], q.Y[0], q.X[2], q.Y[2], q.X[3], q.Y[3])
		if math.Abs(a1) < degenArea2 && math.Abs(a2) < degenArea2 {
			continue // empty quad: nothing to draw
		}
		h, ok := solveHomography(q.U, q.V, q.X, q.Y)
		if !ok {
			return fmt.Errorf("%s：%d 面目の変換先の四角形が縮退しています（頂点の順序を確認してください）", name, i+1)
		}
		w := quadWs(h, q.U, q.V)
		for _, wv := range w {
			if !(wv > 0) {
				return fmt.Errorf("%s：%d 面目の変換先の四角形は描画できません（頂点の順序を確認してください）", name, i+1)
			}
		}
		f := face{x: q.X, y: q.Y, u: q.U, v: q.V, w: w}
		for c := 0; c < 4; c++ {
			f.light[c] = [3]float32{float32(q.Light[c][0]), float32(q.Light[c][1]), float32(q.Light[c][2])}
		}
		faces = append(faces, f)
	}
	if len(faces) == 0 {
		return nil
	}
	sel := b.currentSel()
	blend := b.currentBlend()
	alpha := b.alphaScale()
	b.enqueue(func() {
		dst, simg := b.ensureTarget(sel), b.ensureTarget(src)
		// One draw per chunk of faces: uint16 indices cap a call at
		// maxVertsPerDraw vertices.
		for start := 0; start < len(faces); start += maxVertsPerDraw / 4 {
			end := start + maxVertsPerDraw/4
			if end > len(faces) {
				end = len(faces)
			}
			chunk := faces[start:end]
			vs := make([]ebiten.Vertex, 0, len(chunk)*4)
			idx := make([]uint16, 0, len(chunk)*6)
			su := make([]float64, 0, len(chunk)*4)
			sv := make([]float64, 0, len(chunk)*4)
			for _, f := range chunk {
				base := uint16(len(vs))
				for c := 0; c < 4; c++ {
					vs = append(vs, perspVert(f.x[c], f.y[c], f.u[c], f.v[c], f.w[c],
						[4]float32{f.light[c][0], f.light[c][1], f.light[c][2], alpha}))
				}
				idx = append(idx, base+0, base+1, base+2, base+0, base+2, base+3)
				su = append(su, f.u[0], f.u[1], f.u[2], f.u[3])
				sv = append(sv, f.v[0], f.v[1], f.v[2], f.v[3])
			}
			drawPersp(b, dst, simg, vs, idx, su, sv, blend, alpha)
		}
	})
	return nil
}
