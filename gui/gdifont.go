package gui

import (
	"encoding/binary"
	"os"
	"unicode/utf16"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// 埋め込みビットマップ描画 (GDI) のためのフォント解決。
//
// MS ゴシック・MS 明朝などは EBLC/EBDT テーブルにサイズ別の
// 1bpp ビットマップ (strike) を持つ。text/v2 の経路はこれを無視して
// アウトラインを AA ラスタ化するため、小さいサイズでぼやける。
// Windows では GDI (NONANTIALIASED) で描くと strike が選ばれて
// ドットのまま出る。使うかどうかの判定に要る情報
// (ファミリ名・strike の ppem 集合) をここで純 Go で抜く。
// GDI 呼び出し自体は gdi_windows.go、非 Windows の殻は gdi_other.go。

// parseBitmapFontFile はフォントファイルの先頭フェイス (face 0。
// loadFontFile が .ttc で選ぶのと同じ) から GDI 用ファミリ名候補と
// 埋め込みビットマップ strike の ppem 集合を取り出す。
// EBLC が無い・strike が無い・読めない場合は ok=false。
// ファミリ名は英語 (0x409) 優先の順序付き一覧。
func parseBitmapFontFile(path string) (families []string, strikes map[int]bool, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) < 12 {
		return nil, nil, false
	}
	faceOff := 0
	if string(b[0:4]) == "ttcf" {
		num := int(binary.BigEndian.Uint32(b[8:12]))
		if num < 1 || len(b) < 12+4*num {
			return nil, nil, false
		}
		faceOff = int(binary.BigEndian.Uint32(b[12:16]))
	}
	if faceOff < 0 || len(b) < faceOff+6 {
		return nil, nil, false
	}
	numTables := int(binary.BigEndian.Uint16(b[faceOff+4 : faceOff+6]))
	if numTables <= 0 || numTables > 64 || len(b) < faceOff+12+numTables*16 {
		return nil, nil, false
	}
	nameOff, nameLen := 0, 0
	eblcOff := 0
	nameFound, eblcFound := false, false
	for i := 0; i < numTables; i++ {
		e := faceOff + 12 + i*16
		off := int(binary.BigEndian.Uint32(b[e+8 : e+12]))
		ln := int(binary.BigEndian.Uint32(b[e+12 : e+16]))
		switch string(b[e : e+4]) {
		case "name":
			nameOff, nameLen, nameFound = off, ln, true
		case "EBLC":
			eblcOff, eblcFound = off, true
		}
	}
	families = bitmapFontFamilies(b, nameOff, nameLen)
	strikes = bitmapFontStrikes(b, eblcOff, eblcFound)
	if !nameFound || len(families) == 0 || !eblcFound || len(strikes) == 0 {
		return nil, nil, false
	}
	return families, strikes, true
}

// bitmapFontFamilies は name テーブルからファミリ名候補を抜く。
// nameID 16 (typographic family)・英語 (0x409) を優先した順序付き
// 一覧 (重複除去)。日本語 Windows の GDI は全角名
// (例「ＭＳ ゴシック」) を返すため、候補すべてで照合する。
func bitmapFontFamilies(b []byte, off, ln int) []string {
	if off < 0 || ln < 6 || off+ln > len(b) {
		return nil
	}
	count := int(binary.BigEndian.Uint16(b[off+2 : off+4]))
	strOff := int(binary.BigEndian.Uint16(b[off+4 : off+6]))
	if count <= 0 || count > 512 {
		return nil
	}
	type cand struct {
		s     string
		score int
	}
	var cands []cand
	for i := 0; i < count; i++ {
		r := off + 6 + i*12
		if r+12 > off+ln || r+12 > len(b) {
			break
		}
		plat := binary.BigEndian.Uint16(b[r : r+2])
		lang := binary.BigEndian.Uint16(b[r+4 : r+6])
		nid := binary.BigEndian.Uint16(b[r+6 : r+8])
		l := int(binary.BigEndian.Uint16(b[r+8 : r+10]))
		o := int(binary.BigEndian.Uint16(b[r+10 : r+12]))
		if nid != 1 && nid != 16 {
			continue
		}
		if plat != 1 && plat != 3 {
			continue
		}
		start := off + strOff + o
		if start < off || l < 0 || start+l > off+ln || start+l > len(b) {
			continue
		}
		raw := b[start : start+l]
		var s string
		if plat == 3 {
			if len(raw)%2 != 0 {
				continue
			}
			u16 := make([]uint16, len(raw)/2)
			for j := range u16 {
				u16[j] = binary.BigEndian.Uint16(raw[j*2:])
			}
			s = string(utf16.Decode(u16))
		} else {
			// Mac Roman の ASCII 部分は latin-1 と同じ。ファミリ名は
			// 実用上 ASCII のため、そのまま写す。
			rs := make([]rune, len(raw))
			for j, c := range raw {
				rs[j] = rune(c)
			}
			s = string(rs)
		}
		if s == "" {
			continue
		}
		score := 0
		if lang == 0x409 {
			score += 4
		}
		if nid == 16 {
			score += 2
		}
		if plat == 3 {
			score++
		}
		cands = append(cands, cand{s: s, score: score})
	}
	// スコア順に並べ (安定)、重複を除く。
	out := []string{}
	seen := map[string]bool{}
	for {
		best := -1
		for i, c := range cands {
			if seen[c.s] {
				continue
			}
			if best < 0 || c.score > cands[best].score {
				best = i
			}
		}
		if best < 0 {
			break
		}
		seen[cands[best].s] = true
		out = append(out, cands[best].s)
	}
	return out
}

// bitmapFontStrikes は EBLC から strike の ppem 集合を抜く。
// BitmapSize 48 バイト中の ppemX/ppemY (オフセット +44/+45) のみ読む。
func bitmapFontStrikes(b []byte, off int, found bool) map[int]bool {
	if !found || off < 0 || off+8 > len(b) {
		return nil
	}
	num := int(binary.BigEndian.Uint32(b[off+4 : off+8]))
	if num <= 0 || num > 128 {
		return nil
	}
	out := make(map[int]bool)
	for i := 0; i < num; i++ {
		base := off + 8 + i*48
		if base+48 > len(b) {
			break
		}
		if px := int(b[base+44]); px > 0 {
			out[px] = true
		}
		if py := int(b[base+45]); py > 0 {
			out[py] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// gdiSnap は描画時の GDI ビットマップ指定。ゲームスレッドへ渡す
// スナップショットに載せ、ロック外で使う。
type gdiSnap struct {
	use  bool
	fam  string
	size int
}

// gdiSnapLocked は現在のフォントに対する GDI スナップを返す。
// 呼出し側は mu を保持すること。
func (b *WindowBackend) gdiSnapLocked() gdiSnap {
	fam, ok := b.gdiForSize(int(b.fontSize + 0.5))
	if !ok {
		return gdiSnap{}
	}
	return gdiSnap{use: true, fam: fam, size: int(b.fontSize + 0.5)}
}

// measureFaceLocked は文字列幅を測る。GDI 有効時は GDI 実測、
// さもなくば outline 実測。呼出し側は mu を保持すること。
func (b *WindowBackend) measureFaceLocked(s string) float64 {
	if gs := b.gdiSnapLocked(); gs.use {
		if w, ok := gdiMeasureWidth(gs.fam, gs.size, s); ok {
			return w
		}
	}
	w, _ := text.Measure(s, b.face, 0)
	return w
}

// refreshGDILocked は fontPath から GDI 用情報を取り直す。
// 呼出し側は mu を保持するか、まだ公開前のバックエンドを触ること。
func (b *WindowBackend) refreshGDILocked() {
	fam, strikes, ok := resolveGDIFont(b.fontPath)
	if !ok {
		b.gdiFam, b.gdiStrikes = "", nil
		return
	}
	b.gdiFam, b.gdiStrikes = fam, strikes
}

// gdiForSize は size に埋め込みビットマップ strike があれば
// GDI ファミリ名を返す。なければ outline 描画へ落とす。
func (b *WindowBackend) gdiForSize(size int) (string, bool) {
	if b.gdiFam == "" || b.gdiStrikes == nil {
		return "", false
	}
	if !b.gdiStrikes[size] {
		return "", false
	}
	return b.gdiFam, true
}
