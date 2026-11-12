package gui

import (
	"strings"
	"testing"
)

// TestParseBitmapFontSystem は既定フォントの EBLC 解析を試す。
// ビットマップ strike が無ければ skip (Linux の Noto 等)。
func TestParseBitmapFontSystem(t *testing.T) {
	p := FontPath()
	if p == "" {
		t.Skip("no system font")
	}
	fam, strikes, ok := parseBitmapFontFile(p)
	if !ok {
		t.Skipf("no embedded bitmaps in %s", p)
	}
	if len(fam) == 0 {
		t.Fatal("empty families")
	}
	if len(strikes) == 0 {
		t.Fatal("empty strikes")
	}
	t.Logf("families=%q strikes=%v", fam, strikes)
	lower := strings.ToLower(p)
	has := func(want string) bool {
		for _, f := range fam {
			if f == want {
				return true
			}
		}
		return false
	}
	if strings.Contains(lower, "msgothic") {
		if !has("MS Gothic") {
			t.Fatalf("families = %q, want MS Gothic", fam)
		}
		for _, s := range []int{7, 12, 16, 22} {
			if !strikes[s] {
				t.Fatalf("msgothic missing strike %d", s)
			}
		}
	}
	if strings.Contains(lower, "msmincho") {
		if !has("MS Mincho") {
			t.Fatalf("families = %q, want MS Mincho", fam)
		}
		if !strikes[16] {
			t.Fatal("msmincho missing strike 16")
		}
	}
}

// TestGDIDefaultFont は既定フォントで GDI 解決が効くかを見る。
// 非 Windows・非ビットマップ環境では skip。
func TestGDIDefaultFont(t *testing.T) {
	b, err := New(320, 200)
	if err != nil {
		t.Skipf("no backend: %v", err)
	}
	fam, ok := b.gdiForSize(16)
	if !ok {
		t.Skipf("no GDI bitmap font (family resolution off)")
	}
	if fam == "" {
		t.Fatal("empty GDI family")
	}
	t.Logf("GDI family=%q", fam)
}

// TestGDIMeasure は GDI 実測が outline 実測と一致すること
// (MS ゴシック 16px は半角 8px・全角 16px) を確かめる。
func TestGDIMeasure(t *testing.T) {
	b, err := New(320, 200)
	if err != nil {
		t.Skipf("no backend: %v", err)
	}
	fam, ok := b.gdiForSize(16)
	if !ok || (fam != "MS Gothic" && fam != "MS Mincho") {
		t.Skipf("not an MS bitmap font (fam=%q ok=%v)", fam, ok)
	}
	for s, want := range map[string]float64{
		"ABC": 24, "あいう": 48, "あAう": 40, "    ": 32,
	} {
		w, ok := gdiMeasureWidth(fam, 16, s)
		if !ok {
			t.Fatalf("gdiMeasureWidth(%q) failed", s)
		}
		if w != want {
			t.Fatalf("gdiMeasureWidth(%q) = %v, want %v", s, w, want)
		}
	}
}

// TestGDIRasterizeCrisp は GDI マスクが2値
// (白 opaque / 透明のみ。中間調なし) であることを確かめる。
func TestGDIRasterizeCrisp(t *testing.T) {
	b, err := New(320, 200)
	if err != nil {
		t.Skipf("no backend: %v", err)
	}
	fam, ok := b.gdiForSize(16)
	if !ok {
		t.Skipf("no GDI bitmap font")
	}
	m, ok := gdiRasterizeMask(fam, 16, "あ")
	if !ok || m == nil {
		t.Fatal("gdiRasterizeMask failed")
	}
	dx, dy := m.Bounds().Dx(), m.Bounds().Dy()
	if dx != 16 || dy != 16 {
		t.Fatalf("mask size = %dx%d, want 16x16", dx, dy)
	}
	opaque, transp := 0, 0
	for i := 0; i < len(m.Pix); i += 4 {
		a := m.Pix[i+3]
		if a != 0 && a != 0xFF {
			t.Fatalf("mid-tone alpha %d (AA mixed in?)", a)
		}
		if a == 0xFF {
			if m.Pix[i] != 0xFF || m.Pix[i+1] != 0xFF || m.Pix[i+2] != 0xFF {
				t.Fatal("non-white opaque pixel")
			}
			opaque++
		} else {
			transp++
		}
	}
	if opaque == 0 || transp == 0 {
		t.Fatalf("degenerate mask (opaque=%d transp=%d)", opaque, transp)
	}
	t.Logf("mask 16x16 opaque=%d transp=%d", opaque, transp)
}

// TestMeasureWithSnapGDI は描画時スナップでの幅測定が GDI 実測に
// なることを確かめる (画像を作らないため RunGame 後も安全)。
func TestMeasureWithSnapGDI(t *testing.T) {
	b := mustNew(t)
	b.mu.Lock()
	gs := b.gdiSnapLocked()
	face := b.face
	b.mu.Unlock()
	if !gs.use || (gs.fam != "MS Gothic" && gs.fam != "MS Mincho") {
		t.Skipf("not an MS bitmap font (fam=%q use=%v)", gs.fam, gs.use)
	}
	if w := measureWithSnap("あいう", face, gs); w != 48 {
		t.Fatalf("gdi snap width = %v, want 48", w)
	}
	if w := measureWithSnap("あいう", face, gdiSnap{}); w != 48 {
		t.Fatalf("outline width = %v, want 48", w)
	}
}
