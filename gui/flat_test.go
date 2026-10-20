package gui

import (
	"math"
	"testing"
)

// TestDrawFlatQuadsValidation はゲームループなしで gflat の検査を扱う:
// あらゆる失敗は何かをエンキューする前に戻り、まとまりはちょうど
// 1つの命令として着地する。
func TestDrawFlatQuadsValidation(t *testing.T) {
	b := mustNew(t)
	if err := b.SelectTarget(0); err != nil {
		t.Fatalf("SelectTarget(0): %v", err)
	}
	q := func() FlatQuad {
		return FlatQuad{
			X: [4]float64{0, 10, 10, 0}, Y: [4]float64{0, 0, 10, 10},
			Col: [3]float64{1, 1, 1},
		}
	}
	// 上限を超える面数は他の何より先に拒否される。
	big := make([]FlatQuad, maxQuadsPerCall+1)
	if err := b.DrawFlatQuads(big); err == nil {
		t.Fatal("over the cap should error")
	}
	// NaN/Inf の座標・色は拒否される（Go 側の呼出しに対する多層防御）。
	bad := q()
	bad.Y[1] = math.NaN()
	if err := b.DrawFlatQuads([]FlatQuad{bad}); err == nil {
		t.Fatal("NaN coordinate should error")
	}
	bad = q()
	bad.Col[2] = math.Inf(1)
	if err := b.DrawFlatQuads([]FlatQuad{bad}); err == nil {
		t.Fatal("Inf color should error")
	}
	// 点の四角形は無音で何もせず、決してエラーにならず、何も
	// エンキューしない。
	pt := q()
	pt.X = [4]float64{5, 5, 5, 5}
	pt.Y = [4]float64{5, 5, 5, 5}
	if err := b.DrawFlatQuads([]FlatQuad{pt}); err != nil {
		t.Fatalf("degenerate quad: %v", err)
	}
	if p, q := queuedLens(b); p != 0 || q != 0 {
		t.Fatalf("degenerate quad enqueued: pending=%d queue=%d", p, q)
	}
	// 空のまとまりは何もしない。
	if err := b.DrawFlatQuads(nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if p, q := queuedLens(b); p != 0 || q != 0 {
		t.Fatalf("empty batch enqueued: pending=%d queue=%d", p, q)
	}
	// 有効な四角形1つはちょうど1つの命令を準備する。
	if err := b.DrawFlatQuads([]FlatQuad{q()}); err != nil {
		t.Fatalf("DrawFlatQuads: %v", err)
	}
	if p, q := queuedLens(b); p != 0 || q != 1 {
		t.Fatalf("valid quad staged: pending=%d queue=%d, want 0/1", p, q)
	}
	// N 面のまとまりも依然ちょうど1つの命令を準備する。
	batch := make([]FlatQuad, 0, 7)
	for i := 0; i < 7; i++ {
		fq := q()
		fq.X[0] += float64(i * 12)
		fq.X[1] += float64(i * 12)
		fq.X[2] += float64(i * 12)
		fq.X[3] += float64(i * 12)
		batch = append(batch, fq)
	}
	if err := b.DrawFlatQuads(batch); err != nil {
		t.Fatalf("DrawFlatQuads batch: %v", err)
	}
	if p, q := queuedLens(b); p != 0 || q != 2 {
		t.Fatalf("batch staged: pending=%d queue=%d, want 0/2", p, q)
	}
	// 扇形三角 ([c0,c1,c2,c2] 形式) はエラーにならず描く
	// (gquad と違い特異にならない)。
	tri := q()
	tri.X[3] = tri.X[2]
	tri.Y[3] = tri.Y[2]
	if err := b.DrawFlatQuads([]FlatQuad{tri}); err != nil {
		t.Fatalf("fan triangle: %v", err)
	}
	if p, q := queuedLens(b); p != 0 || q != 3 {
		t.Fatalf("fan triangle staged: pending=%d queue=%d, want 0/3", p, q)
	}
}
