// GUI 組み込み関数 (G8): 生ピクセル配列の描画。
// gblit(pix, x, y, w, h [, dw, dh]) は
// 整数配列の 0xAARRGGBB を RGBA バイト列へ変換し、(x, y) を角とした
// |dw| x |dh|（dw, dh 省略時は |w| x |h|）に拡大・縮小して現在の描画先へ描く。負の dw/dh は反転描画する。
// pix は dim(w*h) などで作った通常の配列を直接渡す。
package main

import (
	"gsh/gui"
)

func init() {
	register("gblit", 5, 7, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gblit")
		if err != nil {
			return Null(), err
		}
		if len(args) == 6 {
			return Null(), rtErrf(at, "gblit は引数を 5 個または 7 個必要としますが、%d 個が渡されました", len(args))
		}
		arr, err := needArray("gblit", args, 0, at)
		if err != nil {
			return Null(), err
		}
		x, err := needInt("gblit", args, 1, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("gblit", args, 2, at)
		if err != nil {
			return Null(), err
		}
		w, err := needInt("gblit", args, 3, at)
		if err != nil {
			return Null(), err
		}
		h, err := needInt("gblit", args, 4, at)
		if err != nil {
			return Null(), err
		}
		if w == 0 || h == 0 {
			return Null(), rtErrf(at, "gblit のサイズは 0 以外である必要があります (%d x %d)", w, h)
		}
		aw, ah := w, h
		if aw < 0 {
			aw = -aw
		}
		if ah < 0 {
			ah = -ah
		}
		if aw > gui.MaxImageDim || ah > gui.MaxImageDim {
			return Null(), rtErrf(at, "gblit のサイズは 1〜%d の範囲で指定してください (%d x %d)", gui.MaxImageDim, w, h)
		}
		n := aw * ah
		if n/aw != ah {
			return Null(), rtErrf(at, "gblit のサイズが大きすぎます (%d x %d)", w, h)
		}
		if int64(len(arr.Elems)) < n {
			return Null(), rtErrf(at, "gblit の配列が足りません (必要 %d 要素、指定 %d 要素)", n, len(arr.Elems))
		}
		dw, dh := w, h
		if len(args) == 7 {
			dw, err = needInt("gblit", args, 5, at)
			if err != nil {
				return Null(), err
			}
			dh, err = needInt("gblit", args, 6, at)
			if err != nil {
				return Null(), err
			}
		}
		if dw == 0 || dh == 0 {
			return Null(), rtErrf(at, "gblit のコピー先サイズは 0 以外である必要があります (%d x %d)", dw, dh)
		}
		pix := make([]byte, int(n)*4)
		for i := int64(0); i < n; i++ {
			e := arr.Elems[i]
			if e.K != KInt {
				return Null(), argErr("gblit", 0, at, "要素 %d は整数である必要があります。%s が指定されました", i, typeNameOf(e))
			}
			if e.I < -2147483648 || e.I > 4294967295 {
				return Null(), argErr("gblit", 0, at, "要素 %d は 32bit に収まりません (%d)", i, e.I)
			}
			c := uint32(e.I)
			a := byte(c >> 24)
			r := byte(c >> 16)
			g := byte(c >> 8)
			bl := byte(c)
			// Ebiten の WritePixels は乗算済みアルファを期待する。
			if a != 255 {
				r = byte(uint32(r) * uint32(a) / 255)
				g = byte(uint32(g) * uint32(a) / 255)
				bl = byte(uint32(bl) * uint32(a) / 255)
			}
			pix[i*4] = r
			pix[i*4+1] = g
			pix[i*4+2] = bl
			pix[i*4+3] = a
		}
		if err := wb.DrawBitmap(0, pix, int(x), int(y), int(w), int(h), int(dw), int(dh)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
