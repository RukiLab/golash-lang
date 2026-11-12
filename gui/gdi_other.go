//go:build !windows

package gui

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
)

// 非 Windows の殻: 埋め込みビットマップ描画は Windows 専用のため、
// 常に outline 描画へ落とす。

func resolveGDIFont(fontPath string) (string, map[int]bool, bool) {
	return "", nil, false
}

func gdiMeasureWidth(fam string, size int, s string) (float64, bool) {
	return 0, false
}

func gdiRasterizeMask(fam string, size int, s string) (*image.RGBA, bool) {
	return nil, false
}

func gdiDrawFrag(img *ebiten.Image, op textOp) bool {
	return false
}
