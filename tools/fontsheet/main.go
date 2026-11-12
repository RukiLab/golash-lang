package main

// fontsheet は TTF/OTF/TTC から二値化(PNGマスク+メトリクスJSON)グリフシートを焼く試作ツール。
// アンチエイリアスの中間グレーを閾値で潰すため、転写側は 1:1 の矩形コピーだけでドットが保たれる。
//
//	使い方 (golash-lang モジュール内):
//	  go run ./tools/fontsheet -font C:\Windows\Fonts\msgothic.ttc -size 16 -out sheet_msgothic16
//	  go run ./tools/fontsheet -font <PixelMplus10-Regular.ttf> -size 10 -out sheet_pixelmplus10
import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type GlyphMetric struct {
	Rune     string `json:"rune"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	W        int    `json:"w"`
	H        int    `json:"h"`
	Advance  int    `json:"advance"`
	OffsetX  int    `json:"offsetX"`
	OffsetY  int    `json:"offsetY"`
	CellTop  int    `json:"cellTop"`
	Ascent   int    `json:"ascent"`
	Descent  int    `json:"descent"`
}

type SheetMeta struct {
	Font   string        `json:"font"`
	Size   int           `json:"size"`
	Cell   int           `json:"cell"`
	Cols   int           `json:"cols"`
	Width  int           `json:"width"`
	Height int           `json:"height"`
	Glyphs []GlyphMetric `json:"glyphs"`
}

// 試作文字集合: ASCII印字可能 + Golashデモ文 + かな + 漢字サンプル。
func demoCharset(extra string) []rune {
	var sb strings.Builder
	for c := 0x20; c <= 0x7E; c++ {
		sb.WriteRune(rune(c))
	}
	sb.WriteString("こんにちは世界Ｇｏｌａｓｈ１２３漢字仮名交じり文表示テスト。")
	sb.WriteString("あいうえおかきくけこさしすせそ")
	sb.WriteString(extra)
	seen := map[rune]bool{}
	var out []rune
	for _, r := range sb.String() {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func main() {
	fontPath := flag.String("font", "", "TTF/OTF/TTC ファイルのパス")
	size := flag.Int("size", 16, "ピクセルサイズ")
	out := flag.String("out", "sheet", "出力ベース名 (PNG+JSON+preview)")
	extra := flag.String("extra", "", "追加文字")
	thresh := flag.Int("thresh", 128, "二値化閾値 (0-255)")
	faceIndex := flag.Int("index", 0, "TTC内のフェイス番号")
	flag.Parse()

	if *fontPath == "" || *size <= 0 {
		fmt.Fprintln(os.Stderr, "font と size を指定してください")
		os.Exit(2)
	}

	data, err := os.ReadFile(*fontPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}

	var f *opentype.Font
	ext := strings.ToLower(filepath.Ext(*fontPath))
	if ext == ".ttc" {
		coll, err := opentype.ParseCollection(data)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse ttc:", err)
			os.Exit(1)
		}
		if *faceIndex < 0 || *faceIndex >= coll.NumFonts() {
			fmt.Fprintln(os.Stderr, "index out of range, num =", coll.NumFonts())
			os.Exit(1)
		}
		f, err = coll.Font(*faceIndex)
		if err != nil {
			fmt.Fprintln(os.Stderr, "font:", err)
			os.Exit(1)
		}
	} else {
		f, err = opentype.Parse(data)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse:", err)
			os.Exit(1)
		}
	}

	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    float64(*size),
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "face:", err)
		os.Exit(1)
	}
	defer face.Close()

	m := face.Metrics()
	ascent := m.Ascent.Ceil()
	descent := m.Descent.Ceil()
	cell := (m.Ascent + m.Descent).Round()
	if cell < *size {
		cell = *size
	}
	cell += 4 // ベアリングはみ出し用パディング

	runes := demoCharset(*extra)

	type baked struct {
		r    rune
		img  *image.RGBA
		w, h int
		adv  int
		ox   int
		oy   int
	}
	var glyphs []baked
	maxW := 0
	for _, r := range runes {
		adv, ok := face.GlyphAdvance(r)
		if !ok {
			continue
		}
		bounds, _, ok := face.GlyphBounds(r)
		if !ok {
			continue
		}
		w := (bounds.Max.X - bounds.Min.X).Ceil() + 2
		h := (bounds.Max.Y - bounds.Min.Y).Ceil() + 2
		if w <= 2 || h <= 2 {
			// スペース等の空白グリフ: 画像なし、送りのみ記録
			glyphs = append(glyphs, baked{r: r, adv: adv.Round(), w: 0, h: 0})
			continue
		}
		if w > 256 || h > 256 {
			fmt.Fprintf(os.Stderr, "skip huge glyph U+%04X (%dx%d)\n", r, w, h)
			continue
		}
		tmp := image.NewRGBA(image.Rect(0, 0, w, h))
		d := &font.Drawer{
			Dst:  tmp,
			Src:  image.NewUniform(color.RGBA{255, 255, 255, 255}),
			Face: face,
			Dot: fixed.Point26_6{
				X: fixed.I(1) - bounds.Min.X,
				Y: fixed.I(1) - bounds.Min.Y,
			},
		}
		d.DrawString(string(r))
		// 二値化: 閾値で潰してAA由来のグレーを消す
		gray := 0
		for i := 3; i < len(tmp.Pix); i += 4 {
			if tmp.Pix[i] > 0 && tmp.Pix[i] < 255 {
				gray++
			}
			if int(tmp.Pix[i]) >= *thresh {
				tmp.Pix[i-3] = 255
				tmp.Pix[i-2] = 255
				tmp.Pix[i-1] = 255
				tmp.Pix[i] = 255
			} else {
				tmp.Pix[i-3] = 0
				tmp.Pix[i-2] = 0
				tmp.Pix[i-1] = 0
				tmp.Pix[i] = 0
			}
		}
		_ = gray
		if w > maxW {
			maxW = w
		}
		glyphs = append(glyphs, baked{
			r: r, img: tmp, w: w, h: h,
			adv: adv.Round(),
			ox:  (bounds.Min.X - fixed.I(1)).Round(),
			oy:  (bounds.Min.Y - fixed.I(1)).Round(),
		})
	}

	cols := 16
	rows := (len(glyphs) + cols - 1) / cols
	W, H := cols*cell, rows*cell
	sheet := image.NewNRGBA(image.Rect(0, 0, W, H))
	meta := SheetMeta{Font: filepath.Base(*fontPath), Size: *size, Cell: cell, Cols: cols, Width: W, Height: H}
	for i, g := range glyphs {
		cx, cy := (i%cols)*cell, (i/cols)*cell
		if g.img != nil {
			ox := (cell - g.w) / 2
			oy := (cell - g.h) / 2
			for y := 0; y < g.h; y++ {
				for x := 0; x < g.w; x++ {
					sheet.Set(cx+ox+x, cy+oy+y, g.img.At(x, y))
				}
			}
			meta.Glyphs = append(meta.Glyphs, GlyphMetric{
				Rune: string(g.r), X: cx + ox, Y: cy + oy, W: g.w, H: g.h,
				Advance: g.adv, OffsetX: g.ox, OffsetY: g.oy,
				CellTop: cy, Ascent: ascent, Descent: descent,
			})
		} else {
			meta.Glyphs = append(meta.Glyphs, GlyphMetric{
				Rune: string(g.r), X: cx, Y: cy, W: 0, H: 0,
				Advance: g.adv, CellTop: cy, Ascent: ascent, Descent: descent,
			})
		}
	}

	pngPath := *out + ".png"
	fp, err := os.Create(pngPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := png.Encode(fp, sheet); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fp.Close()

	jb, _ := json.MarshalIndent(meta, "", "  ")
	jsonPath := *out + ".json"
	if err := os.WriteFile(jsonPath, jb, 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// 比較プレビュー: 上=AA生 / 下=二値化 を4倍で並べた確認用
	preview := renderPreview(face, "Golash あいうえお漢字 123 ABC", *size)
	pp, err := os.Create(*out + "_preview.png")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	png.Encode(pp, preview)
	pp.Close()

	fmt.Printf("glyphs=%d cell=%d sheet=%dx%d\n  %s\n  %s\n  %s\n",
		len(glyphs), cell, W, H, pngPath, jsonPath, *out+"_preview.png")
}

// renderPreview は同一文字列のAA版と二値化版を上下に並べた4倍拡大プレビュー。
func renderPreview(face font.Face, s string, size int) image.Image {
	adv := font.MeasureString(face, s).Round()
	h := face.Metrics().Ascent.Ceil() + face.Metrics().Descent.Ceil() + 4
	raw := image.NewRGBA(image.Rect(0, 0, adv+8, h))
	d := &font.Drawer{
		Dst: raw, Src: image.NewUniform(color.Black),
		Face: face, Dot: fixed.P(4, face.Metrics().Ascent.Ceil()+2),
	}
	d.DrawString(s)
	bin := image.NewRGBA(raw.Bounds())
	grayCount, blackCount := 0, 0
	for i := 3; i < len(raw.Pix); i += 4 {
		a := raw.Pix[i]
		if a > 0 && a < 255 {
			grayCount++
		}
		if a > 0 {
			blackCount++
		}
		var v uint8
		if int(a) >= 128 {
			v = 255
		}
		bin.Pix[i-3], bin.Pix[i-2], bin.Pix[i-1], bin.Pix[i] = 0, 0, 0, v
	}
	// 白地に黒字で見やすく
	zoom := 4
	pw, ph := raw.Bounds().Dx(), raw.Bounds().Dy()
	out := image.NewRGBA(image.Rect(0, 0, pw*zoom, ph*2*zoom+8))
	white := color.RGBA{255, 255, 255, 255}
	for y := 0; y < out.Bounds().Dy(); y++ {
		for x := 0; x < out.Bounds().Dx(); x++ {
			out.Set(x, y, white)
		}
	}
	blit := func(src *image.RGBA, oy int) {
		for y := 0; y < ph; y++ {
			for x := 0; x < pw; x++ {
				a := src.RGBAAt(x, y).A
				c := color.RGBA{255, 255, 255, 255}
				if a > 0 {
					v := 255 - a
					c = color.RGBA{v, v, v, 255}
				}
				for zy := 0; zy < zoom; zy++ {
					for zx := 0; zx < zoom; zx++ {
						out.Set(x*zoom+zx, oy+y*zoom+zy, c)
					}
				}
			}
		}
	}
	blit(raw, 0)
	blit(bin, ph*zoom+8)
	fmt.Printf("preview gray=%d opaque-ish=%d grayRatio=%.2f\n",
		grayCount, blackCount, float64(grayCount)/float64(blackCount+1))
	return out
}
