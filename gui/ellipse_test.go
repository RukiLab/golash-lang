package gui

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2/vector"
)

// TestEllipsePathBounds は楕円パスの幾何が中心・半径に従うことを検証する
// （純粋な計算で GPU 不要。w/h は全幅・全高なので半径は半分）。
func TestEllipsePathBounds(t *testing.T) {
	var p vector.Path
	ellipsePath(&p, 100, 50, 20, 10)
	b := p.Bounds()
	// 楕円は x: 80..120, y: 40..60 に収まる（ベジェ近似のため±1pxの余裕を見る）。
	if b.Min.X > 81 || b.Max.X < 119 || b.Min.Y > 41 || b.Max.Y < 59 {
		t.Fatalf("ellipse bounds = %v, want ~(80,40)-(120,60)", b)
	}
	// 真円は対称な正方形に収まる。
	p.Reset()
	ellipsePath(&p, 0, 0, 10, 10)
	b = p.Bounds()
	if b.Dx() != b.Dy() {
		t.Fatalf("circle bounds = %v, want square", b)
	}
}

// TestEllipseDegenerate は縮退サイズがキューに何も積まず失敗しないこと、
// 正常系のエンキューがヘッドレスでも失敗しないことを検証する。
func TestEllipseDegenerate(t *testing.T) {
	b := mustNew(t)
	b.Ellipse(10, 10, 0, 5, true, [4]int{255, 0, 0, 255})
	b.Ellipse(10, 10, -1, 5, false, [4]int{255, 0, 0, 255})
	b.Ellipse(10, 10, 20, 10, true, [4]int{255, 0, 0, 255})
	b.Ellipse(10, 10, 20, 20, false, [4]int{255, 0, 0, 255})
}
