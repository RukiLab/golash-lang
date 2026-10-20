package gui

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// 単色 quad の高速ラスタ（G7）: gflat() は透視補正もホモグラフィも
// 使わない。quad を2つの三角形に割って DrawTriangles で塗るだけである。
// gquad() との違い:
//
//   - テクスチャ・UV・頂点色補間がない（面ごとに1色のフラット）。
//     ホモグラフィ求解（面ごとの8x9ガウス・ジョルダン）が消える。
//   - 縮退・裏返りの四角でエラーにならない。面積0の三角形は
//     GPU が無視するため、扇形三角 ([c0,c1,c2,c2] 形式) もそのまま渡せる。
//     gquad() の特異エラーに触れない。
//   - 折り返し（bowtie）四角は2つの三角形として描く（gquad は捨てる）。
//     呼出し側で背面カリング済みの凸面だけ渡すこと。
//
// 描画呼出は gquad と同じフレームモデル：検証後に1つの命令として
// ゲームスレッドへ着地する。

// FlatQuad は単色面1つ：順番に並んだ4つの隅と面全体の1色
// （0..1 の rgb。等しい4頂点のグーローと等価）。
type FlatQuad struct {
	X, Y [4]float64
	Col  [3]float64
}

// finite は座標と色が有限かを報告する。
func (q FlatQuad) finite() bool {
	for _, arr := range [][4]float64{q.X, q.Y} {
		for _, v := range arr {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return false
			}
		}
	}
	for _, v := range q.Col {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// flatWhite は1pxの白画像を返す（単色塗りの転送元。ゲームスレッドのみ）。
func (b *WindowBackend) flatWhite() *ebiten.Image {
	if b.flat == nil {
		b.flat = ebiten.NewImage(1, 1)
		b.flat.Fill(color.White)
	}
	return b.flat
}

// DrawFlatQuads は全 quad を単色で塗る。空・縮退だけのまとまりは
// 何もしない。まとまりはアトミックで、他のキャンバス命令と同じく
// フレームに乗る。
func (b *WindowBackend) DrawFlatQuads(quads []FlatQuad) error {
	const name = "gflat"
	if len(quads) > maxQuadsPerCall {
		return fmt.Errorf("%s：1度に描画できるのは %d 面までです。%d 面が指定されました", name, maxQuadsPerCall, len(quads))
	}
	type tri struct {
		x, y [3]float64
		col  [3]float32
	}
	tris := make([]tri, 0, len(quads)*2)
	for _, q := range quads {
		if !q.finite() {
			return fmt.Errorf("%s：座標は有限の数値である必要があります", name)
		}
		a1 := triArea2(q.X[0], q.Y[0], q.X[1], q.Y[1], q.X[2], q.Y[2])
		a2 := triArea2(q.X[0], q.Y[0], q.X[2], q.Y[2], q.X[3], q.Y[3])
		if math.Abs(a1) < degenArea2 && math.Abs(a2) < degenArea2 {
			continue // 空の四角形：描画するものはない
		}
		c := [3]float32{float32(q.Col[0]), float32(q.Col[1]), float32(q.Col[2])}
		tris = append(tris,
			tri{x: [3]float64{q.X[0], q.X[1], q.X[2]}, y: [3]float64{q.Y[0], q.Y[1], q.Y[2]}, col: c},
			tri{x: [3]float64{q.X[0], q.X[2], q.X[3]}, y: [3]float64{q.Y[0], q.Y[2], q.Y[3]}, col: c},
		)
	}
	if len(tris) == 0 {
		return nil
	}
	sel := b.currentSel()
	blend := b.currentBlend()
	alpha := b.alphaScale()
	b.enqueue(func() {
		dst := b.ensureTarget(sel)
		white := b.flatWhite()
		// 1回の呼出の頂点上限で分割する（DrawQuads と同じ）。
		const trisPerDraw = maxVertsPerDraw / 3
		for start := 0; start < len(tris); start += trisPerDraw {
			end := start + trisPerDraw
			if end > len(tris) {
				end = len(tris)
			}
			chunk := tris[start:end]
			vs := make([]ebiten.Vertex, 0, len(chunk)*3)
			idx := make([]uint16, 0, len(chunk)*3)
			for _, t := range chunk {
				base := uint16(len(vs))
				for c := 0; c < 3; c++ {
					vs = append(vs, ebiten.Vertex{
						DstX:   float32(t.x[c]),
						DstY:   float32(t.y[c]),
						SrcX:   0,
						SrcY:   0,
						ColorR: t.col[0], ColorG: t.col[1], ColorB: t.col[2], ColorA: alpha,
					})
				}
				idx = append(idx, base+0, base+1, base+2)
			}
			op := &ebiten.DrawTrianglesOptions{}
			op.CompositeMode = blend
			dst.DrawTriangles(vs, idx, white, op)
		}
	})
	return nil
}
