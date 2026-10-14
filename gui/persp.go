package gui

import (
	"fmt"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// 遠近補正されたテクスチャ・ラスタ（G1）: gquad() は1つの Kage シェーダ
// と1つの射影コアを通じてテクスチャ四角形を描く。画面空間の UV は頂点の
// custom 属性で (u/w, v/w, 1/w) として伝わり、フラグメントシェーダで
// 割り返されるため、2つの三角形に分割された四角形に対角線の継ぎ目が
// 生じない（三角形ごとのアフィン補間ではそこに折れ目ができる）。
//
// 描画呼出はキャンバスのフレームモデルを保つ: 検証はスクリプトの
// ゴルーチンで（Blit のエラーと同じくヘッドレス安全に）実行され、
// ラスタは enqueue 経由でゲームスレッドで実行される。gmode/galpha と
// gsel の対象を尊重する。

// perspShaderSrc は共有のフラグメントシェーダ。全体を通じてピクセル
// 単位: 幾何もサンプリング座標も単なるピクセルである。
// UV は範囲外でも透明をサンプルする（imageSrc0At）ため、ソース矩形が
// 境界外の SubImage のようにループのゴルーチンを panic させることは
// ない。ソース画像は Ebiten のテクスチャアトラス内のどこかにありうる
// ため、imageSrc0Origin が加えられる（単独の画像は (0,0) を報告し、
// これによりピクセル単位で正確なサンプリングが保たれる）。
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

// degenArea2 はこれより小さい2倍の面積を空と見なす。ゼロを通って
// アニメーションする四角形や三角形は決してエラーにならない。その
// フレームでは単に何も描かれないだけである。
const degenArea2 = 1e-9

// maxQuadsPerCall は1回の gquad() 命令の面数を制限する。意図的に
// 寛大である（スクリプトの配列自身が dim() の要素上限で止まる）が、
// 暴走するループが1フレームで無制限の頂点メモリをエンキューできない
// よう上限がある。
const maxQuadsPerCall = 65536

// maxVertsPerDraw は1回の DrawTrianglesShader の頂点上限である:
// インデックスは uint16 なので、その 1/4 を超える面数は分割を
// 必要とする（これはスクリプトからは透過的である）。
const maxVertsPerDraw = 65536

// perspShader は共有のシェーダを一度だけコンパイルする。ゲームスレッド
// のみ: すべての呼出し側は描画キューを通じて直列に到着する。
func (b *WindowBackend) perspShader() *ebiten.Shader {
	if b.persp == nil && b.perspErr == nil {
		b.persp, b.perspErr = ebiten.NewShader([]byte(perspShaderSrc))
	}
	return b.persp
}

// triArea2 は (a, b, c) の符号付き面積の2倍。
func triArea2(ax, ay, bx, by, cx, cy float64) float64 {
	return (bx-ax)*(cy-ay) - (cx-ax)*(by-ay)
}

// solveHomography は各 (su, sv) を (du, dv) に写像する H[8] == 1 の
// 行優先の H を求める。4組が特異（共線など退化）の場合は false を
// 報告する。
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
	// 部分ピボット付きガウス・ジョルダン法。
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

// quadWs は各ソースの隅で H の最終行（w = h6*u + h7*v + 1）から
// 同次座標の重みを読み出す。
func quadWs(h [9]float64, su, sv [4]float64) [4]float64 {
	var w [4]float64
	for i := 0; i < 4; i++ {
		w[i] = h[6]*su[i] + h[7]*sv[i] + 1
	}
	return w
}

// perspVert はラスタの頂点1つを詰める: 射影のペイロードは custom に、
// 色合いは色の乗数に入る。シェーダは組み込みのパイプラインとまったく
// 同じく `clr *= color` を適用するため、頂点ごとの色合いは三角形全体に
// 補間され（グーロー）、galpha は Blit と同じく合成される
// （事前乗算済み安全）。col は (r, g, b, a) で rgb は 0..1。平坦な
// 陰影付けは等しい3頂点にすぎない。
func perspVert(dx, dy, u, v, w float64, col [4]float32) ebiten.Vertex {
	return ebiten.Vertex{
		DstX:   float32(dx),
		DstY:   float32(dy),
		ColorR: col[0], ColorG: col[1], ColorB: col[2], ColorA: col[3],
		Custom0: float32(u / w), Custom1: float32(v / w), Custom2: float32(1 / w),
	}
}

// drawPersp は共有のゲームスレッド・ラスタ: verts は custom に絶対
// ソースピクセルを持ち、dst に着地する。シェーダが失敗した場合は
// フレームが消えないようアフィンの DrawTriangles にフォールバック
// する。（信頼性のためのフォールバックのみ: ユーザー向けのアフィン
// モードは提供しない。）
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

// Quad は遠近補正されたテクスチャ面1つ: 順番に並んだ4つの変換先の隅、
// それらがサンプルする4つのソースピクセル、および隅ごとの1つの 0..1 の
// rgb トリプレット（白はテクスチャを変更しない。等しい4つのトリプレット
// は平坦な陰影付け）。
type Quad struct {
	X, Y, U, V [4]float64
	Light      [4][3]float64
}

// finite は四角形のすべての座標とライトが有限かを報告する。スクリプトの
// 値は決して NaN/Inf になりえない（それを生成すること自体が既に
// エラー）ため、これは Go 側の呼出しに対する多層防御である。
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

// DrawQuads はすべての四角形のソースの隅を遠近補正で変換先の隅に
// 写像する: ホモグラフィは四角形ごとに解かれるため、呼出し側が w を
// 与えることはない（アフィンな写像はすべての重みが 1 になる場合に
// すぎない）。空の四角形は何も描かない。ホモグラフィが特異な四角形、
// または重みがすべて正でない四角形はエラーである。
//
// まとまりはアトミックで、他のあらゆるキャンバス命令と同じくフレームに
// 乗る: すべての四角形は何かが準備される前に検証され、面は1つの命令と
// してゲームスレッドに着地する（インデックスが16bitなので、裏では
// いくつかの描画呼出に分割される）。
func (b *WindowBackend) DrawQuads(src int, quads []Quad) error {
	const name = "gquad"
	// 安価な健全性検査を先に: 上限を超える面数は他の何より先に
	// 拒否されるため、暴走するループは（同様に壊れている）呼出の
	// 残りではなく面数で失敗する。
	if len(quads) > maxQuadsPerCall {
		return fmt.Errorf("%s：1度に描画できるのは %d 面までです。%d 面が指定されました", name, maxQuadsPerCall, len(quads))
	}
	if !b.hasBuf(src) {
		return fmt.Errorf("%s：不明なバッファ %d です", name, src)
	}
	// face は検証済みの描画可能な四角形1つ: 解かれた重みと、シェーダが
	// 期待する隅ごとの色の乗数。
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
			continue // 空の四角形: 描画するものはない
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
		// 面の塊ごとに1回の描画: uint16 インデックスは1回の呼出を
		// maxVertsPerDraw 個の頂点に制限する。
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
