package gui

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// gquad()/tri() のバッチ描画（G6）。
// gquad() は1四角形を透視補正付きテクスチャ三角形2枚に割って描き、
// 縮退（特異・折返し・非有限重み）時はアフィン変換付き三角形（w=1）に
// フォールバックする。tri() は現在の色で塗りつぶし三角形1枚を描く。
// どちらもスクリプトスレッドで内部バッファに貯め、適切なタイミング
// （他描画・読戻し・フレーム境界・上限・src/sel/blend 切替）でまとめて
// Draw する。これにより gquad() 連打でも Draw 呼出は数回に畳まれる。

// degenArea2 はこれより小さい2倍面積を空と見なし描画しない。
// ゼロを通るアニメでもエラーにせずそのフレームは何も描かない。
const degenArea2 = 1e-9

// affineW は縮退時のフォールバック重み（w=1、透視なし）。
// シェーダは uv = (u/1, v/1) となり2三角形ともアフィン補間になる。
var affineW = [4]float64{1, 1, 1, 1}

// maxVertsPerDraw は1回の DrawTriangles(Shader) の頂点上限。
// インデックスは uint16 のため分割が必要（スクリプトからは透過）。
const maxVertsPerDraw = 65536

// quadBatchCap/triBatchCap はフラッシュ前に貯める上限。
// 超えたら古い分を先に流してから新しい分を貯める。
const quadBatchCap = 2048
const triBatchCap = 4096

// perspShaderSrc は共有のフラグメントシェーダ。ピクセル単位。
// UV は範囲外で透明をサンプルし（imageSrc0At）、ソース矩形が
// 境界外でもループゴルーチンを panic させない。ソース画像は
// テクスチャアトラス内のどこかにありうるため imageSrc0Origin を足す。
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

// quadEntry はバッファ済み四角形1面。w は同次重み、alpha は
// 追加時点の galpha（0..1）。色は白（テクスチャそのまま）。
type quadEntry struct {
	x, y, u, v, w [4]float64
	alpha         float32
}

// quadBatchState は同一 src/sel/blend のまとまり。
type quadBatchState struct {
	src   int
	sel   int
	blend ebiten.CompositeMode
	items []quadEntry
}

// triEntry はバッファ済み三角形1枚。col は頂点色（0..1、α込み）。
type triEntry struct {
	x   [3]float64
	y   [3]float64
	col [4]float32
}

// triBatchState は同一 sel/blend のまとまり。
type triBatchState struct {
	sel   int
	blend ebiten.CompositeMode
	items []triEntry
}

// triArea2 は (a,b,c) の符号付き面積の2倍。
func triArea2(ax, ay, bx, by, cx, cy float64) float64 {
	return (bx-ax)*(cy-ay) - (cx-ax)*(by-ay)
}

// finiteFloats は全て有限かを報告する。
func finiteFloats(fs ...float64) bool {
	for _, f := range fs {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
	}
	return true
}

// solveHomography は各 (su,sv) を (du,dv) へ写す H[8]==1 の行優先 H を
// 求める。4組が特異（共線など）の場合は false。
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

// quadWs は各ソース隅で H 最終行（w = h6*u + h7*v + 1）から重みを読む。
func quadWs(h [9]float64, su, sv [4]float64) [4]float64 {
	var w [4]float64
	for i := 0; i < 4; i++ {
		w[i] = h[6]*su[i] + h[7]*sv[i] + 1
	}
	return w
}

// perspShader は共有シェーダを一度だけコンパイルする。ゲームスレッドのみ。
func (b *WindowBackend) perspShader() *ebiten.Shader {
	if b.persp == nil && b.perspErr == nil {
		b.persp, b.perspErr = ebiten.NewShader([]byte(perspShaderSrc))
	}
	return b.persp
}

// flatWhite は1px白画像を返す（単色塗りの転送元。ゲームスレッドのみ）。
func (b *WindowBackend) flatWhite() *ebiten.Image {
	if b.flat == nil {
		b.flat = ebiten.NewImage(1, 1)
		b.flat.Fill(color.White)
	}
	return b.flat
}

// perspVert はラスタ頂点1つを詰める。射影ペイロードは custom、色合いは
// 乗数（シェーダが clr *= color するため galpha 合成は Blit と同じ）。
func perspVert(dx, dy, u, v, w float64, alpha float32) ebiten.Vertex {
	return ebiten.Vertex{
		DstX:   float32(dx),
		DstY:   float32(dy),
		ColorR: 1, ColorG: 1, ColorB: 1, ColorA: alpha,
		Custom0: float32(u / w), Custom1: float32(v / w), Custom2: float32(1 / w),
	}
}

// QuadBuffered は四角形1面を検証してバッファする。有限でない座標は
// エラー、空（変換先の2三角形とも面積なし）は沈黙の無描画。
// ホモグラフィが特異・折返し（重み非正・非有限）の縮退時は
// アフィン変換付き三角形（w=1、2三角形ともアフィン補間）に
// フォールバックしてバッファする。成功時は AppendQuad へ委ねる。
// スクリプトスレッド用。
func (b *WindowBackend) QuadBuffered(src int, dx, dy, su, sv [4]float64) error {
	const name = "gquad"
	if !finiteFloats(dx[0], dy[0], dx[1], dy[1], dx[2], dy[2], dx[3], dy[3],
		su[0], sv[0], su[1], sv[1], su[2], sv[2], su[3], sv[3]) {
		return fmt.Errorf("%s：座標は有限の数値である必要があります", name)
	}
	a1 := triArea2(dx[0], dy[0], dx[1], dy[1], dx[2], dy[2])
	a2 := triArea2(dx[0], dy[0], dx[2], dy[2], dx[3], dy[3])
	if math.Abs(a1) < degenArea2 && math.Abs(a2) < degenArea2 {
		return nil
	}
	h, ok := solveHomography(su, sv, dx, dy)
	if !ok {
		return b.AppendQuad(src, dx, dy, su, sv, affineW)
	}
	w := quadWs(h, su, sv)
	for _, wv := range w {
		if !(wv > 0) || math.IsNaN(wv) || math.IsInf(wv, 0) {
			return b.AppendQuad(src, dx, dy, su, sv, affineW)
		}
	}
	return b.AppendQuad(src, dx, dy, su, sv, w)
}

// TriBuffered は三角形1枚を検証して現在の色でバッファする。有限で
// ない座標はエラー、面積なしは沈黙の無描画。スクリプトスレッド用。
func (b *WindowBackend) TriBuffered(x, y [3]float64, fg [4]int) error {
	const name = "tri"
	if !finiteFloats(x[0], y[0], x[1], y[1], x[2], y[2]) {
		return fmt.Errorf("%s：座標は有限の数値である必要があります", name)
	}
	if math.Abs(triArea2(x[0], y[0], x[1], y[1], x[2], y[2])) < degenArea2 {
		return nil
	}
	b.AppendTri(x, y, fg)
	return nil
}

// AppendQuad は四角形1面をバッファする。w は同次重みで、通常は
// ホモグラフィ由来、縮退時は affineW（w=1、アフィン補間）。
// 同一 src/sel/blend が続く間は貯め、切替・上限で古い分を先に流す。
// スクリプトスレッド用（mu 内で完結）。
func (b *WindowBackend) AppendQuad(src int, dx, dy, su, sv, w [4]float64) error {
	const name = "gquad"
	if !b.hasBuf(src) {
		return fmt.Errorf("%s：不明なバッファ %d です", name, src)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	// 新しい quad より古い tri/テキストを先に流して順序を保つ。
	// （printRaw/enqueue 側も quad を先に流すため、3種は常に
	// 高々1種だけが残る。順序は呼出順どおりになる。）
	b.flushTriBatchLocked()
	b.flushTextBatchLocked()
	sel := b.sel
	blend := b.blend
	alpha := float32(b.alpha) / 255
	if b.quadBatch != nil {
		q := b.quadBatch
		if q.src != src || q.sel != sel || q.blend != blend || len(q.items) >= quadBatchCap {
			b.flushQuadBatchLocked()
		}
	}
	if b.quadBatch == nil {
		b.quadBatch = &quadBatchState{src: src, sel: sel, blend: blend}
	}
	b.quadBatch.items = append(b.quadBatch.items, quadEntry{x: dx, y: dy, u: su, v: sv, w: w, alpha: alpha})
	return nil
}

// AppendTri は三角形1枚を現在の色でバッファする。検証済み（有限・
// 非縮退）のみ呼ぶこと。同一 sel/blend が続く間は貯める。
// スクリプトスレッド用（mu 内で完結）。
func (b *WindowBackend) AppendTri(x, y [3]float64, fg [4]int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// 新しい tri より古い quad/テキストを先に流して順序を保つ。
	b.flushQuadBatchLocked()
	b.flushTextBatchLocked()
	sel := b.sel
	blend := b.blend
	// fg に galpha を掛けた頂点色（FillRect の fillColor と同じ）。
	a := float32(fg[3]) * float32(b.alpha) / (255 * 255)
	col := [4]float32{
		float32(fg[0]) / 255,
		float32(fg[1]) / 255,
		float32(fg[2]) / 255,
		a,
	}
	if b.triBatch != nil {
		t := b.triBatch
		if t.sel != sel || t.blend != blend || len(t.items) >= triBatchCap {
			b.flushTriBatchLocked()
		}
	}
	if b.triBatch == nil {
		b.triBatch = &triBatchState{sel: sel, blend: blend}
	}
	b.triBatch.items = append(b.triBatch.items, triEntry{x: x, y: y, col: col})
}

// flushQuadBatchLocked は貯めた quad を1つ以上の Draw へ流す。
// 呼出し側は mu を保持すること。yieldedOnce に応じて pending/queue へ積む。
func (b *WindowBackend) flushQuadBatchLocked() {
	q := b.quadBatch
	if q == nil || len(q.items) == 0 {
		b.quadBatch = nil
		return
	}
	src, sel, blend := q.src, q.sel, q.blend
	items := q.items
	b.quadBatch = nil
	const quadsPerDraw = maxVertsPerDraw / 4
	for start := 0; start < len(items); start += quadsPerDraw {
		end := start + quadsPerDraw
		if end > len(items) {
			end = len(items)
		}
		chunk := items[start:end]
		vs := make([]ebiten.Vertex, 0, len(chunk)*4)
		idx := make([]uint16, 0, len(chunk)*6)
		su := make([]float64, 0, len(chunk)*4)
		sv := make([]float64, 0, len(chunk)*4)
		for _, f := range chunk {
			base := uint16(len(vs))
			for c := 0; c < 4; c++ {
				vs = append(vs, perspVert(f.x[c], f.y[c], f.u[c], f.v[c], f.w[c], f.alpha))
			}
			idx = append(idx, base+0, base+1, base+2, base+0, base+2, base+3)
			su = append(su, f.u[0], f.u[1], f.u[2], f.u[3])
			sv = append(sv, f.v[0], f.v[1], f.v[2], f.v[3])
		}
		cmd := func(vs []ebiten.Vertex, idx []uint16, su, sv []float64) func() {
			return func() {
				dst, simg := b.ensureTarget(sel), b.ensureTarget(src)
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
						ColorR: 1, ColorG: 1, ColorB: 1, ColorA: v.ColorA,
					}
				}
				op := &ebiten.DrawTrianglesOptions{}
				op.CompositeMode = blend
				dst.DrawTriangles(fb, idx, simg, op)
			}
		}(vs, idx, su, sv)
		if b.yieldedOnce {
			b.pending = append(b.pending, cmd)
		} else {
			b.queue = append(b.queue, cmd)
		}
	}
}

// flushTriBatchLocked は貯めた tri を1つ以上の Draw へ流す。
// 呼出し側は mu を保持すること。
func (b *WindowBackend) flushTriBatchLocked() {
	t := b.triBatch
	if t == nil || len(t.items) == 0 {
		b.triBatch = nil
		return
	}
	sel, blend := t.sel, t.blend
	items := t.items
	b.triBatch = nil
	const trisPerDraw = maxVertsPerDraw / 3
	for start := 0; start < len(items); start += trisPerDraw {
		end := start + trisPerDraw
		if end > len(items) {
			end = len(items)
		}
		chunk := items[start:end]
		vs := make([]ebiten.Vertex, 0, len(chunk)*3)
		idx := make([]uint16, 0, len(chunk)*3)
		for _, e := range chunk {
			base := uint16(len(vs))
			for c := 0; c < 3; c++ {
				vs = append(vs, ebiten.Vertex{
					DstX:   float32(e.x[c]),
					DstY:   float32(e.y[c]),
					SrcX:   0,
					SrcY:   0,
					ColorR: e.col[0], ColorG: e.col[1], ColorB: e.col[2], ColorA: e.col[3],
				})
			}
			idx = append(idx, base+0, base+1, base+2)
		}
		cmd := func(vs []ebiten.Vertex, idx []uint16) func() {
			return func() {
				dst := b.ensureTarget(sel)
				white := b.flatWhite()
				op := &ebiten.DrawTrianglesOptions{}
				op.CompositeMode = blend
				dst.DrawTriangles(vs, idx, white, op)
			}
		}(vs, idx)
		if b.yieldedOnce {
			b.pending = append(b.pending, cmd)
		} else {
			b.queue = append(b.queue, cmd)
		}
	}
}
