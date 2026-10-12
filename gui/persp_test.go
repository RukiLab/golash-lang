package gui

import (
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

// applyH maps (u, v) through row-major H with the projective divide.
func applyH(h [9]float64, u, v float64) (float64, float64) {
	w := h[6]*u + h[7]*v + h[8]
	return (h[0]*u + h[1]*v + h[2]) / w, (h[3]*u + h[4]*v + h[5]) / w
}

func TestSolveHomographyIdentity(t *testing.T) {
	su := [4]float64{0, 1, 1, 0}
	sv := [4]float64{0, 0, 1, 1}
	h, ok := solveHomography(su, sv, su, sv)
	if !ok {
		t.Fatal("identity should solve")
	}
	want := [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	for i := range want {
		if math.Abs(h[i]-want[i]) > 1e-9 {
			t.Fatalf("h[%d] = %v, want %v (h=%v)", i, h[i], want[i], h)
		}
	}
	if w := quadWs(h, su, sv); w != [4]float64{1, 1, 1, 1} {
		t.Fatalf("identity weights = %v, want all 1", w)
	}
}

func TestSolveHomographyAffine(t *testing.T) {
	su := [4]float64{10, 40, 40, 10}
	sv := [4]float64{20, 20, 60, 60}
	du := [4]float64{15, 45, 45, 15}
	dv := [4]float64{27, 27, 67, 67}
	h, ok := solveHomography(su, sv, du, dv)
	if !ok {
		t.Fatal("translated rect should solve")
	}
	for i := range su {
		if x, y := applyH(h, su[i], sv[i]); math.Abs(x-du[i]) > 1e-9 || math.Abs(y-dv[i]) > 1e-9 {
			t.Fatalf("corner %d maps to (%v,%v), want (%v,%v)", i, x, y, du[i], dv[i])
		}
	}
	// Affine maps keep every weight at 1: the shader path then matches
	// DrawTriangles exactly.
	for i, wv := range quadWs(h, su, sv) {
		if math.Abs(wv-1) > 1e-9 {
			t.Fatalf("affine weight %d = %v, want 1", i, wv)
		}
	}
}

func TestSolveHomographyTrapezoid(t *testing.T) {
	su := [4]float64{0, 100, 100, 0}
	sv := [4]float64{0, 0, 100, 100}
	du := [4]float64{0, 200, 160, 40}
	dv := [4]float64{0, 0, 100, 100}
	h, ok := solveHomography(su, sv, du, dv)
	if !ok {
		t.Fatal("trapezoid should solve")
	}
	for i := range su {
		if x, y := applyH(h, su[i], sv[i]); math.Abs(x-du[i]) > 1e-6 || math.Abs(y-dv[i]) > 1e-6 {
			t.Fatalf("corner %d maps to (%v,%v), want (%v,%v)", i, x, y, du[i], dv[i])
		}
	}
	w := quadWs(h, su, sv)
	for i, wv := range w {
		if !(wv > 0) || math.IsNaN(wv) || math.IsInf(wv, 0) {
			t.Fatalf("weight %d = %v, want finite positive", i, wv)
		}
	}
	// The correction must actually kick in: a trapezoid is not affine,
	// so the weights cannot all be 1 (that would crease the diagonal).
	allOne := true
	for _, wv := range w {
		if math.Abs(wv-1) > 1e-6 {
			allOne = false
		}
	}
	if allOne {
		t.Fatalf("trapezoid weights = %v, want non-affine", w)
	}
	// Interior stays inside: the source center must land inside the quad.
	if x, y := applyH(h, 50, 50); x < 0 || x > 200 || y < 0 || y > 100 {
		t.Fatalf("center maps to (%v,%v), want inside the trapezoid", x, y)
	}
}

func TestSolveHomographySingular(t *testing.T) {
	su := [4]float64{0, 10, 10, 0}
	sv := [4]float64{0, 0, 10, 10}
	// All four images collinear: no projective map exists.
	du := [4]float64{0, 10, 20, 30}
	dv := [4]float64{0, 0, 0, 0}
	if _, ok := solveHomography(su, sv, du, dv); ok {
		t.Fatal("collinear images should not solve")
	}
}

func TestPerspVertEncodesProjective(t *testing.T) {
	v := perspVert(7, 9, 100, 200, 4, [4]float32{1, 0.5, 0.25, 0.5})
	if v.DstX != 7 || v.DstY != 9 {
		t.Fatalf("dst = (%v,%v), want (7,9)", v.DstX, v.DstY)
	}
	if v.Custom0 != 25 || v.Custom1 != 50 || v.Custom2 != 0.25 {
		t.Fatalf("custom = (%v,%v,%v), want (25,50,0.25)", v.Custom0, v.Custom1, v.Custom2)
	}
	if v.ColorR != 1 || v.ColorG != 0.5 || v.ColorB != 0.25 || v.ColorA != 0.5 {
		t.Fatalf("color = (%v,%v,%v,%v), want (1,0.5,0.25,0.5)", v.ColorR, v.ColorG, v.ColorB, v.ColorA)
	}
}

var white3 = [3][3]float64{{1, 1, 1}, {1, 1, 1}, {1, 1, 1}}

func TestPerspShaderCompiles(t *testing.T) {
	// NewShader type-checks the Kage source on the CPU; a failure here
	// means the embedded shader no longer compiles, independent of any
	// window or game loop.
	if _, err := ebiten.NewShader([]byte(perspShaderSrc)); err != nil {
		t.Fatalf("perspShaderSrc: %v", err)
	}
}

// TestDrawQuadsValidation covers gquad checks without a game loop:
// every failure returns before enqueueing anything, and a batch lands
// as exactly one command.
func TestDrawQuadsValidation(t *testing.T) {
	b := mustNew(t)
	if err := b.SelectTarget(0); err != nil {
		t.Fatalf("SelectTarget(0): %v", err)
	}
	white := [4][3]float64{{1, 1, 1}, {1, 1, 1}, {1, 1, 1}, {1, 1, 1}}
	q := func() Quad {
		return Quad{
			X: [4]float64{0, 10, 10, 0}, Y: [4]float64{0, 0, 10, 10},
			U: [4]float64{0, 8, 8, 0}, V: [4]float64{0, 0, 8, 8},
			Light: white,
		}
	}
	if err := b.DrawQuads(99, []Quad{q()}); err == nil {
		t.Fatal("unknown buffer should error")
	}
	// NaN/Inf coordinates are rejected (defense in depth for Go callers).
	bad := q()
	bad.Y[1] = math.NaN()
	if err := b.DrawQuads(0, []Quad{bad}); err == nil {
		t.Fatal("NaN coordinate should error")
	}
	bad = q()
	bad.Light[2] = [3]float64{math.Inf(1), 1, 1}
	if err := b.DrawQuads(0, []Quad{bad}); err == nil {
		t.Fatal("Inf light should error")
	}
	// A point quad is a silent no-op, never an error, and enqueues nothing.
	pt := q()
	pt.X = [4]float64{5, 5, 5, 5}
	pt.Y = [4]float64{5, 5, 5, 5}
	if err := b.DrawQuads(0, []Quad{pt}); err != nil {
		t.Fatalf("degenerate quad: %v", err)
	}
	if p, q := queuedLens(b); p != 0 || q != 0 {
		t.Fatalf("degenerate quad enqueued: pending=%d queue=%d", p, q)
	}
	// Two source corners sharing one image forces a singular map.
	dup := q()
	dup.U = [4]float64{0, 0, 10, 0}
	dup.V = [4]float64{0, 0, 0, 10}
	if err := b.DrawQuads(0, []Quad{q(), dup}); err == nil {
		t.Fatal("collapsed corner pair should error")
	}
	if p, q := queuedLens(b); p != 0 || q != 0 {
		t.Fatalf("a bad quad must not stage anything: pending=%d queue=%d", p, q)
	}
	// An empty batch is a no-op (a loop that culled every face), staging
	// nothing — the builtin returns before even reaching here, but the
	// face layer holds the same rule for Go-side callers.
	if err := b.DrawQuads(0, nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if p, q := queuedLens(b); p != 0 || q != 0 {
		t.Fatalf("empty batch enqueued: pending=%d queue=%d", p, q)
	}
	// One valid quad stages exactly one command.
	if err := b.DrawQuads(0, []Quad{q()}); err != nil {
		t.Fatalf("DrawQuads: %v", err)
	}
	if p, q := queuedLens(b); p != 0 || q != 1 {
		t.Fatalf("valid quad staged: pending=%d queue=%d, want 0/1", p, q)
	}
	// A batch of N faces still stages exactly one command.
	batch := make([]Quad, 0, 7)
	for i := 0; i < 7; i++ {
		f := q()
		f.X = [4]float64{float64(i), float64(i) + 10, float64(i) + 10, float64(i)}
		batch = append(batch, f)
	}
	if err := b.DrawQuads(0, batch); err != nil {
		t.Fatalf("DrawQuads batch: %v", err)
	}
	if p, q := queuedLens(b); p != 0 || q != 2 {
		t.Fatalf("batch staged: pending=%d queue=%d, want 0/2 (one per command)", p, q)
	}
	// Past the face cap the command is rejected before staging.
	if err := b.DrawQuads(0, make([]Quad, maxQuadsPerCall+1)); err == nil {
		t.Fatal("over the face cap should error")
	}
	if p, q := queuedLens(b); p != 0 || q != 2 {
		t.Fatalf("rejected batch staged: pending=%d queue=%d, want 0/2", p, q)
	}
}
