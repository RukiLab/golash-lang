//go:build windows

package gui

import (
	"image"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/hajimehoshi/ebiten/v2"
)

// GDI による埋め込みビットマップ描画 (Windows 専用)。
//
// MS ゴシック等の EBLC/EBDT strike は GDI を NONANTIALIASED_QUALITY で
// 使うと選ばれる。断片ごとにメモリ DC + 32bpp DIB を作り白文字で描き、
// マスク化して Ebiten 画像へ貼る。フォント切替時 (resolveGDIFont) に
// 1度だけ実在確認 (GetTextFaceW で代替検出) し、ふだんの描画では
// 代替フォントを掴む心配がないようにする。

var (
	modGdi32 = syscall.NewLazyDLL("gdi32.dll")

	procCreateCompatibleDC    = modGdi32.NewProc("CreateCompatibleDC")
	procDeleteDC              = modGdi32.NewProc("DeleteDC")
	procCreateFontW           = modGdi32.NewProc("CreateFontW")
	procSelectObject          = modGdi32.NewProc("SelectObject")
	procDeleteObject          = modGdi32.NewProc("DeleteObject")
	procSetBkMode             = modGdi32.NewProc("SetBkMode")
	procSetTextColor          = modGdi32.NewProc("SetTextColor")
	procGetTextExtentPoint32W = modGdi32.NewProc("GetTextExtentPoint32W")
	procTextOutW              = modGdi32.NewProc("TextOutW")
	procCreateDIBSection      = modGdi32.NewProc("CreateDIBSection")
	procGdiFlush              = modGdi32.NewProc("GdiFlush")
	procGetTextFaceW          = modGdi32.NewProc("GetTextFaceW")
)

const (
	gdiTransparent           = 1 // TRANSPARENT
	gdiDefaultCharset        = 1 // DEFAULT_CHARSET
	gdiNonAntialiasedQuality = 3 // NONANTIALIASED_QUALITY
	gdiFWNormal              = 400
	gdiDIBRGBColors          = 0 // DIB_RGB_COLORS
	gdiBIRGB                 = 0 // BI_RGB
	gdiFaceSize              = 32
)

// gdiBitmapInfoHeader は BITMAPINFOHEADER (40 バイト)。
type gdiBitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

// gdiBitmapInfo は 32bpp 用 BITMAPINFO (パレット不要のため詰物1語)。
type gdiBitmapInfo struct {
	header gdiBitmapInfoHeader
	filler uint32
}

// gdiTextSize は SIZE (GetTextExtentPoint32W の受け皿)。
type gdiTextSize struct {
	cx int32
	cy int32
}

type gdiFontKey struct {
	fam  string
	size int
}

var (
	gdiFontMu sync.Mutex
	gdiFonts  = make(map[gdiFontKey]uintptr)
)

// gdiFont は (fam, size) の HFONT を返す。失敗時は 0。
// 成功分はプロセス終了まで保持する (フォント数は高々数十)。
func gdiFont(fam string, size int) uintptr {
	key := gdiFontKey{fam: fam, size: size}
	gdiFontMu.Lock()
	defer gdiFontMu.Unlock()
	if h, ok := gdiFonts[key]; ok {
		return h
	}
	name, err := syscall.UTF16PtrFromString(fam)
	if err != nil {
		return 0
	}
	h, _, _ := procCreateFontW.Call(
		uintptr(int32(-size)), 0, 0, 0,
		gdiFWNormal, 0, 0, 0,
		gdiDefaultCharset, 0, 0,
		gdiNonAntialiasedQuality, 0,
		uintptr(unsafe.Pointer(name)),
	)
	if h == 0 {
		return 0
	}
	gdiFonts[key] = h
	return h
}

// gdiWithFont は hdc にフォントを選んで f を実行する。
func gdiWithFont(hdc, hfont uintptr, f func()) {
	old, _, _ := procSelectObject.Call(hdc, hfont)
	defer procSelectObject.Call(hdc, old)
	f()
}

// resolveGDIFont は GDI ビットマップ描画用の (ファミリ名, ppem集合) を返す。
// fontPath "" は既定フォント。埋め込みビットマップが無い・
// ファミリが未インストール (代替になる) なら ok=false。
func resolveGDIFont(fontPath string) (string, map[int]bool, bool) {
	if fontPath == "" {
		fontPath = FontPath()
		if fontPath == "" {
			return "", nil, false
		}
	}
	fams, strikes, ok := parseBitmapFontFile(fontPath)
	if !ok || len(fams) == 0 || len(strikes) == 0 {
		return "", nil, false
	}
	for _, fam := range fams {
		if gdiFontInstalledAs(fam, fams) {
			return fam, strikes, true
		}
	}
	return "", nil, false
}

// gdiFontInstalledAs は fam で作ったフォントが GDI でそのまま選べる
// (代替フォントにすり替わらない) かを確かめる。選ばれた実名が
// 候補 (known) のいずれかと一致すればよい。日本語 Windows では
// "MS Gothic" 指定でも実名「ＭＳ ゴシック」が返る。
func gdiFontInstalledAs(fam string, known []string) bool {
	const probeSize = 16
	hfont := gdiFont(fam, probeSize)
	if hfont == 0 {
		return false
	}
	hdc, _, _ := procCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return false
	}
	defer procDeleteDC.Call(hdc)
	installed := false
	gdiWithFont(hdc, hfont, func() {
		var buf [gdiFaceSize]uint16
		n, _, _ := procGetTextFaceW.Call(hdc, gdiFaceSize, uintptr(unsafe.Pointer(&buf[0])))
		if n == 0 {
			return
		}
		got := syscall.UTF16ToString(buf[:])
		for _, k := range known {
			if strings.EqualFold(got, k) {
				installed = true
				return
			}
		}
	})
	return installed
}

// gdiMeasureWidth は文字列の GDI 送り幅をピクセルで返す。
func gdiMeasureWidth(fam string, size int, s string) (float64, bool) {
	if s == "" {
		return 0, true
	}
	if size < 1 || size > 256 {
		return 0, false
	}
	hfont := gdiFont(fam, size)
	if hfont == 0 {
		return 0, false
	}
	u16, err := syscall.UTF16FromString(s)
	if err != nil {
		return 0, false
	}
	u16 = u16[:len(u16)-1]
	if len(u16) == 0 {
		return 0, true
	}
	hdc, _, _ := procCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return 0, false
	}
	defer procDeleteDC.Call(hdc)
	var ext gdiTextSize
	ok := false
	gdiWithFont(hdc, hfont, func() {
		r, _, _ := procGetTextExtentPoint32W.Call(hdc,
			uintptr(unsafe.Pointer(&u16[0])),
			uintptr(len(u16)),
			uintptr(unsafe.Pointer(&ext)))
		if r != 0 && ext.cx >= 0 && ext.cy > 0 {
			ok = true
		}
	})
	if !ok {
		return 0, false
	}
	return float64(ext.cx), true
}

// gdiRasterizeMask は文字列を白マスク RGBA へ描く。
// NONANTIALIASED のため画素は白 opaque / 透明の2値になる。
func gdiRasterizeMask(fam string, size int, s string) (*image.RGBA, bool) {
	if s == "" || size < 1 || size > 256 {
		return nil, false
	}
	hfont := gdiFont(fam, size)
	if hfont == 0 {
		return nil, false
	}
	u16, err := syscall.UTF16FromString(s)
	if err != nil {
		return nil, false
	}
	u16 = u16[:len(u16)-1]
	if len(u16) == 0 {
		return nil, false
	}
	hdc, _, _ := procCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return nil, false
	}
	defer procDeleteDC.Call(hdc)
	var mask *image.RGBA
	ok := false
	gdiWithFont(hdc, hfont, func() {
		var ext gdiTextSize
		r, _, _ := procGetTextExtentPoint32W.Call(hdc,
			uintptr(unsafe.Pointer(&u16[0])),
			uintptr(len(u16)),
			uintptr(unsafe.Pointer(&ext)))
		if r == 0 || ext.cx <= 0 || ext.cy <= 0 {
			return
		}
		if ext.cx > 8192 || ext.cy > 512 {
			return
		}
		w, h := int(ext.cx), int(ext.cy)
		var bmi gdiBitmapInfo
		bmi.header.biSize = 40
		bmi.header.biWidth = int32(w)
		bmi.header.biHeight = int32(-h) // top-down
		bmi.header.biPlanes = 1
		bmi.header.biBitCount = 32
		bmi.header.biCompression = gdiBIRGB
		var bits unsafe.Pointer
		hbm, _, _ := procCreateDIBSection.Call(hdc,
			uintptr(unsafe.Pointer(&bmi)),
			gdiDIBRGBColors,
			uintptr(unsafe.Pointer(&bits)), 0, 0)
		if hbm == 0 || bits == nil {
			return
		}
		oldBm, _, _ := procSelectObject.Call(hdc, hbm)
		defer procSelectObject.Call(hdc, oldBm)
		defer procDeleteObject.Call(hbm)
		// 背景を黒で埋める (未初期化の DIB を読まない)。
		px := unsafe.Slice((*byte)(bits), w*h*4)
		for i := range px {
			px[i] = 0
		}
		procSetBkMode.Call(hdc, gdiTransparent)
		procSetTextColor.Call(hdc, 0x00FFFFFF)
		r, _, _ = procTextOutW.Call(hdc, 0, 0,
			uintptr(unsafe.Pointer(&u16[0])), uintptr(len(u16)))
		if r == 0 {
			return
		}
		procGdiFlush.Call()
		// DIB 32bpp は B,G,R,予約の順。
		m := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if px[(y*w+x)*4+2] >= 128 {
					o := m.PixOffset(x, y)
					m.Pix[o], m.Pix[o+1], m.Pix[o+2], m.Pix[o+3] = 0xFF, 0xFF, 0xFF, 0xFF
				}
			}
		}
		mask, ok = m, true
	})
	return mask, ok
}

// gdiDrawFrag は断片を GDI ビットマップで描く。描けたら true。
// ゲームスレッドから呼ぶこと (Ebiten 画像を作るため)。
func gdiDrawFrag(img *ebiten.Image, op textOp) bool {
	if op.s == "" || op.gdiFam == "" || op.gdiSize <= 0 {
		return false
	}
	mask, ok := gdiRasterizeMask(op.gdiFam, op.gdiSize, op.s)
	if !ok || mask == nil {
		return false
	}
	src := ebiten.NewImageFromImage(mask)
	dop := &ebiten.DrawImageOptions{}
	dop.GeoM.Translate(float64(op.x), float64(op.y))
	dop.ColorScale.Reset()
	dop.ColorScale.ScaleWithColor(op.fg)
	img.DrawImage(src, dop)
	return true
}
