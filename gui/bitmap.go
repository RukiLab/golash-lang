package gui

import (
	"fmt"
	"image"

	"github.com/hajimehoshi/ebiten/v2"
)

// DrawBitmap uploads premultiplied RGBA pixels to a cached image and draws it
// at (dx, dy) (absolute pixels), scaled to (dw x dh).
// (dx, dy) を角とした |dw| x |dh| へ写す。負の dw/dh は逆向きに正規化した
// 上で反転描画する（gcopy と同様）。w/h の負は配列解釈の絶対値のみに使い、
// 反転は dw/dh 側で表す。w/h/dw/dh が 0 はエラー。
//
// key selects the cache slot, so repeated uses at
// the same size reuse the same GPU texture. The caller's slice is copied
// before the command is queued: the script may start rendering the next frame
// into the same memory before the game thread runs the upload.
func (b *WindowBackend) DrawBitmap(key int, pixels []byte, dx, dy, w, h, dw, dh int) error {
	if w == 0 || h == 0 {
		return fmt.Errorf("gblit のサイズは 0 以外である必要があります (%d x %d)", w, h)
	}
	aw, ah := w, h
	if aw < 0 {
		aw = -aw
	}
	if ah < 0 {
		ah = -ah
	}
	if aw > MaxImageDim || ah > MaxImageDim {
		return fmt.Errorf("gblit のサイズは上限 %d x %d までです (%d x %d)", MaxImageDim, MaxImageDim, w, h)
	}
	if dw == 0 || dh == 0 {
		return fmt.Errorf("gblit のコピー先サイズは 0 以外である必要があります (%d x %d)", dw, dh)
	}
	adw, adh := dw, dh
	if adw < 0 {
		adw = -adw
	}
	if adh < 0 {
		adh = -adh
	}
	n := aw * ah * 4
	if len(pixels) != n {
		return fmt.Errorf("gblit のピクセル数が足りません (必要 %d バイト、指定 %d バイト)", n, len(pixels))
	}
	cp := make([]byte, n)
	copy(cp, pixels)

	// 描画先・合成モード・不透明度は enqueue 時点の値で固定する。
	// gblit の後に gsel(0) が呼ばれても、モデルが buffer 0 に
	// 描かれて gcopy に上書きされる競合を避ける。
	dstSel := b.currentSel()
	blend := b.currentBlend()
	alpha := b.alphaScale()
	zx := float64(adw) / float64(aw)
	zy := float64(adh) / float64(ah)
	dxn, dyn := dx, dy
	if dw < 0 {
		dxn += dw
		zx = -zx
	}
	if dh < 0 {
		dyn += dh
		zy = -zy
	}
	tx, ty := float64(dxn), float64(dyn)
	if dw < 0 {
		tx = float64(dxn + adw)
	}
	if dh < 0 {
		ty = float64(dyn + adh)
	}

	b.enqueue(func() {
		if b.pixelImages == nil {
			b.pixelImages = map[int]*ebiten.Image{}
		}
		img := b.pixelImages[key]
		if img == nil || img.Bounds().Dx() != aw || img.Bounds().Dy() != ah {
			// ソフトウェア描画の結果は毎フレーム WritePixels する。
			// アトラス上の他の画像との兼ね合いを避けるため非管理画像にする。
			img = ebiten.NewImageWithOptions(image.Rect(0, 0, aw, ah), &ebiten.NewImageOptions{Unmanaged: true})
			b.pixelImages[key] = img
		}
		img.WritePixels(cp)

		dst := b.ensureTarget(dstSel)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(zx, zy)
		op.GeoM.Translate(tx, ty)
		op.CompositeMode = blend
		op.ColorScale.ScaleAlpha(alpha)
		dst.DrawImage(img, op)
	})
	return nil
}
