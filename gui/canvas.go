package gui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Canvas model (G1): buffers by id (0 = main, visible when selected),
// a pixel cursor (gx, gy), and a blend mode. Script threads enqueue
// commands; Update applies them on the game thread.

// maxBuffers caps window ids (memory guard).
const maxBuffers = 16

// MaxImageDim caps one side of screen()/picload images. Bigger images
// panic inside the engine on the loop goroutine (uncatchable across
// goroutines), so scripts get a normal error instead.
const MaxImageDim = 4096

// enqueue adds a draw command. Before the first yield it goes straight
// to the game-thread queue (no frame boundary yet); afterwards it is
// staged in pending and flushed as one frame at await()/sleep()/end so
// Draw never observes a half-built canvas (cls 直後の空白1フレーム防止).
// A pending mes()/print() burst is flushed first so script call order
// (shapes vs text) is preserved.
func (b *WindowBackend) enqueue(cmd func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushTextBatchLocked()
	if b.yieldedOnce {
		b.pending = append(b.pending, cmd)
		return
	}
	b.queue = append(b.queue, cmd)
}

// drainQueue runs pending commands on the game thread.
func (b *WindowBackend) drainQueue() {
	b.mu.Lock()
	b.flushTextBatchLocked()
	q := b.queue
	b.queue = nil
	b.mu.Unlock()
	for _, cmd := range q {
		cmd()
	}
}

// runOnLoop runs fn on the game thread and waits (for picload upload).
// Staged frame commands are flushed first so ordering is preserved:
// e.g. drawing into buffer 1 then button(..., img=1) snapshots the new
// pixels, and pget/pngsave read back the frame built so far.
func (b *WindowBackend) runOnLoop(fn func()) {
	done := make(chan struct{})
	b.mu.Lock()
	b.flushTextBatchLocked()
	if len(b.pending) > 0 {
		b.queue = append(b.queue, b.pending...)
		b.pending = nil
	}
	b.queue = append(b.queue, func() {
		fn()
		close(done)
	})
	b.mu.Unlock()
	<-done
}

// ensureTarget returns the buffer, creating it on the game thread.
func (b *WindowBackend) ensureTarget(id int) *ebiten.Image {
	if b.targets == nil {
		b.targets = map[int]*ebiten.Image{}
	}
	img, ok := b.targets[id]
	if !ok || img == nil {
		// w/h は script の screen() が mu 下で書くため、game thread からは
		// ロックして読む (ロックなし読みは競合し、確保サイズが撕裂く)。
		b.mu.Lock()
		w, h := b.w, b.h
		b.mu.Unlock()
		img = ebiten.NewImage(w, h)
		b.targets[id] = img
	}
	return img
}

// hasBuf reports allocated ids (script-thread safe).
func (b *WindowBackend) hasBuf(id int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bufs[id]
}

// allocBuf marks an id allocated (script-thread safe).
func (b *WindowBackend) allocBuf(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bufs == nil {
		b.bufs = map[int]bool{}
	}
	b.bufs[id] = true
}

// NewBuffer creates (or replaces) an off-screen buffer of w x h: an
// empty, fully transparent image the script can gsel() into and draw on
// (text included). The visible canvas (id 0) belongs to screen(), so
// buffer() takes the off-screen ids only. Replacing an id drops whatever
// it held, which lets a draw loop re-create its scratch buffer every
// frame. Script-thread safe: the image is made on the game thread.
func (b *WindowBackend) NewBuffer(id, w, h int) error {
	if id <= 0 || id >= maxBuffers {
		return fmt.Errorf("buffer：id は 1 から %d の範囲で指定してください。%d が指定されました", maxBuffers-1, id)
	}
	if w <= 0 || h <= 0 {
		return fmt.Errorf("buffer のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	// Oversized images panic inside the engine on the loop goroutine
	// (uncatchable), so cap them here with a script error.
	if w > MaxImageDim || h > MaxImageDim {
		return fmt.Errorf("buffer のサイズは上限 %d x %d までです", MaxImageDim, MaxImageDim)
	}
	b.allocBuf(id)
	b.enqueue(func() {
		if b.targets == nil {
			b.targets = map[int]*ebiten.Image{}
		}
		b.targets[id] = ebiten.NewImage(w, h)
	})
	return nil
}

// currentSel returns the selected target (script-thread safe).
func (b *WindowBackend) currentSel() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sel
}

// CurrentSel exposes the selected target to script builtins.
func (b *WindowBackend) CurrentSel() int { return b.currentSel() }

// Foreground returns the current drawing color with alpha
// (script-thread safe).
func (b *WindowBackend) Foreground() [4]int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return [4]int{int(b.fg.R), int(b.fg.G), int(b.fg.B), int(b.fg.A)}
}

// SetAlpha sets the global drawing alpha 0-255 (galpha).
func (b *WindowBackend) SetAlpha(a int) error {
	if a < 0 || a > 255 {
		return fmt.Errorf("galpha：不透明度は 0 から 255 の範囲で指定してください。%d が指定されました", a)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.alpha = uint8(a)
	return nil
}

// alphaScale is the ColorScale alpha factor (script-thread safe).
func (b *WindowBackend) alphaScale() float32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return float32(b.alpha) / 255
}

// fillColor is the drawing color with its own alpha multiplied by
// the global alpha (galpha).
func (b *WindowBackend) fillColor(fg [4]int) color.NRGBA {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := uint16(fg[3]) * uint16(b.alpha) / 255
	return color.NRGBA{R: uint8(fg[0]), G: uint8(fg[1]), B: uint8(fg[2]), A: uint8(a)}
}

// CanvasSize returns the logical canvas size (script-thread safe).
func (b *WindowBackend) CanvasSize() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.w, b.h
}

// CursorPixels returns the graphics cursor (script-thread safe).
func (b *WindowBackend) CursorPixels() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gx, b.gy
}

// SelectTarget switches the draw buffer (gsel). It flushes any pending
// text first so batched text stays on the buffer it was laid out for.
func (b *WindowBackend) SelectTarget(id int) error {
	if id < 0 || id >= maxBuffers {
		return fmt.Errorf("gsel：id は 0 から %d の範囲で指定してください。%d が指定されました", maxBuffers-1, id)
	}
	b.allocBuf(id)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushTextBatchLocked()
	b.sel = id
	return nil
}

// ResizeCanvas re-inits for screen(w, h). Window resize is applied directly
// (Ebiten queues it); canvas reset runs on the game thread.
// Only the main buffer (0) is recreated at the new size; other buffers
// (picload etc) are preserved because their size is independent of the
// window and wiping them would turn loaded images into blank canvases.
func (b *WindowBackend) ResizeCanvas(w, h int) {
	ebiten.SetWindowSize(w, h)
	b.mu.Lock()
	b.w, b.h = w, h
	b.mu.Unlock()
	b.enqueue(func() { b.resizeMainCanvas(w, h, false) })
}

// resizeMainCanvas swaps the main buffer (id 0) for a w x h one; other
// buffers keep their own size (picload images), so only id 0 moves.
// preserve keeps the old pixels, top-left aligned, when a window drag
// grows the canvas; screen()/cls()-like starts pass false. The text
// cursor and scroll state reset (the cell grid changed) and the
// per-size scroll scratch is dropped. Game thread only: callers run
// inside the draw queue or from Layout.
func (b *WindowBackend) resizeMainCanvas(w, h int, preserve bool) {
	if b.targets == nil {
		b.targets = map[int]*ebiten.Image{}
	}
	old := b.targets[0]
	delete(b.targets, 0)
	next := ebiten.NewImage(w, h)
	if preserve && old != nil {
		next.DrawImage(old, nil)
	}
	b.targets[0] = next
	b.mu.Lock()
	if b.bufs == nil {
		b.bufs = map[int]bool{}
	}
	b.bufs[0] = true
	b.curX, b.curY, b.gx, b.gy = 0, 0, 0, 0
	b.lineStartX = 0
	b.scrollExact, b.scrollDone = 0, 0
	// Stale scratch images belong to the old size; drop them so a
	// later scroll cannot blit from a mismatched buffer.
	b.scrollScratch = nil
	b.mu.Unlock()
}

// SetBlend sets the composite mode: 0 normal, 1 additive.
func (b *WindowBackend) SetBlend(mode int) error {
	var m ebiten.CompositeMode
	switch mode {
	case 0:
		m = ebiten.CompositeModeSourceOver
	case 1:
		m = ebiten.CompositeModeLighter
	default:
		return fmt.Errorf("gmode：サポートされていないモード %d です（0 または 1 が必要です）", mode)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.blend = m
	return nil
}

// currentBlend reads the blend (script-thread safe).
func (b *WindowBackend) currentBlend() ebiten.CompositeMode {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.blend
}

// SetPixel draws one pixel on the current target.
func (b *WindowBackend) SetPixel(x, y int, fg [4]int) {
	sel := b.currentSel()
	c := b.fillColor(fg)
	b.enqueue(func() {
		b.ensureTarget(sel).Set(x, y, c)
	})
}

// GetPixel reads one pixel from the current target (pget).
// Bounds are validated before touching the game thread so the error
// is headless-safe (no RunGame needed); the readback itself blocks
// until the game thread applies it (like PicLoad/SavePNG), so call
// only from the script goroutine, never from Update/Draw.
// Returned channels are raw 0-255 bytes (opaque pixels round-trip
// exactly; translucent ones follow ebiten's readback convention).
func (b *WindowBackend) GetPixel(x, y int) ([4]int, error) {
	b.mu.Lock()
	w, h := b.w, b.h
	b.mu.Unlock()
	if x < 0 || y < 0 || x >= w || y >= h {
		return [4]int{}, fmt.Errorf("pget：座標 (%d, %d) は %d x %d の範囲外です", x, y, w, h)
	}
	sel := b.currentSel()
	var out [4]int
	var opErr error
	b.runOnLoop(func() {
		img := b.ensureTarget(sel)
		bw, bh := img.Bounds().Dx(), img.Bounds().Dy()
		if x >= bw || y >= bh {
			opErr = fmt.Errorf("pget：座標 (%d, %d) はバッファ %d (%d x %d) の範囲外です", x, y, sel, bw, bh)
			return
		}
		if c, ok := img.At(x, y).(color.RGBA); ok {
			out = [4]int{int(c.R), int(c.G), int(c.B), int(c.A)}
			return
		}
		r, g, bl, a := img.At(x, y).RGBA()
		out = [4]int{int(r >> 8), int(g >> 8), int(bl >> 8), int(a >> 8)}
	})
	if opErr != nil {
		return [4]int{}, opErr
	}
	return out, nil
}

// FillRect draws a filled rectangle on the current target.
func (b *WindowBackend) FillRect(x, y, w, h int, fg [4]int) {
	sel := b.currentSel()
	c := b.fillColor(fg)
	b.enqueue(func() {
		vector.DrawFilledRect(b.ensureTarget(sel), float32(x), float32(y), float32(w), float32(h), c, false)
	})
}

// StrokeLine draws a 1px line on the current target.
func (b *WindowBackend) StrokeLine(x1, y1, x2, y2 int, fg [4]int) {
	sel := b.currentSel()
	c := b.fillColor(fg)
	b.enqueue(func() {
		vector.StrokeLine(b.ensureTarget(sel), float32(x1), float32(y1), float32(x2), float32(y2), 1, c, false)
	})
}

// Circle draws a circle (filled when fill is true) on the current target.
func (b *WindowBackend) Circle(x, y, r int, fill bool, fg [4]int) {
	sel := b.currentSel()
	c := b.fillColor(fg)
	b.enqueue(func() {
		img := b.ensureTarget(sel)
		if fill {
			vector.DrawFilledCircle(img, float32(x), float32(y), float32(r), c, false)
		} else {
			vector.StrokeCircle(img, float32(x), float32(y), float32(r), 1, c, false)
		}
	})
}

// ClearCanvas wipes the current target (transparent; black shows through).
func (b *WindowBackend) ClearCanvas() {
	sel := b.currentSel()
	b.enqueue(func() {
		b.ensureTarget(sel).Clear()
	})
}

// Blit copies a region of buffer src onto the current target at (dx, dy).
func (b *WindowBackend) Blit(src, sx, sy, w, h, dx, dy int) error {
	if !b.hasBuf(src) {
		return fmt.Errorf("gcopy：不明なバッファ %d です", src)
	}
	dst := b.currentSel()
	blend := b.currentBlend()
	b.enqueue(func() {
		simg := b.ensureTarget(src)
		part := simg.SubImage(image.Rect(sx, sy, sx+w, sy+h)).(*ebiten.Image)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(dx), float64(dy))
		op.ColorScale.Scale(1, 1, 1, b.alphaScale())
		op.CompositeMode = blend
		b.ensureTarget(dst).DrawImage(part, op)
	})
	return nil
}

// PicLoad decodes an image file into a new buffer, returning its id.
// Upload runs on the game thread; this waits for it (≤ a frame or two).
func (b *WindowBackend) PicLoad(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("picload：%s", err.Error())
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return 0, fmt.Errorf("picload：%s", err.Error())
	}
	id, err := b.allocUpload(src)
	if err != nil {
		return 0, fmt.Errorf("picload：%s", err.Error())
	}
	return id, nil
}

// allocImageID reserves a buffer id (mu-guarded).
func (b *WindowBackend) allocImageID() (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bufs == nil {
		b.bufs = map[int]bool{}
	}
	id := 1
	for b.bufs[id] {
		id++
		if id >= maxBuffers {
			return 0, fmt.Errorf("バッファが多すぎます（最大 %d）", maxBuffers-1)
		}
	}
	b.bufs[id] = true
	return id, nil
}

// allocUpload reserves a buffer id and uploads src on the game thread.
func (b *WindowBackend) allocUpload(src image.Image) (int, error) {
	// Check before reserving an id: a rejection must not leak one.
	if size := src.Bounds().Size(); size.X > MaxImageDim || size.Y > MaxImageDim {
		return 0, fmt.Errorf("picload：画像が大きすぎます（上限 %d x %d）", MaxImageDim, MaxImageDim)
	}
	id, err := b.allocImageID()
	if err != nil {
		return 0, err
	}
	b.runOnLoop(func() {
		b.targets[id] = ebiten.NewImageFromImage(src)
	})
	return id, nil
}

// BlitScaled copies a region of buffer src scaled by (zx, zy) onto the
// current target at (dx, dy). Script threads enqueue; Update applies.
func (b *WindowBackend) BlitScaled(src, sx, sy, w, h int, zx, zy float64, dx, dy int) error {
	if !b.hasBuf(src) {
		return fmt.Errorf("gcopy：不明なバッファ %d です", src)
	}
	if w <= 0 || h <= 0 {
		return fmt.Errorf("gcopy のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	if zx <= 0 || zy <= 0 {
		return fmt.Errorf("gcopy のスケールは正の値である必要があります。%g x %g が指定されました", zx, zy)
	}
	dst := b.currentSel()
	blend := b.currentBlend()
	b.enqueue(func() {
		simg := b.ensureTarget(src)
		part := simg.SubImage(image.Rect(sx, sy, sx+w, sy+h)).(*ebiten.Image)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(zx, zy)
		op.GeoM.Translate(float64(dx), float64(dy))
		op.ColorScale.Scale(1, 1, 1, b.alphaScale())
		op.CompositeMode = blend
		b.ensureTarget(dst).DrawImage(part, op)
	})
	return nil
}

// rotateTransform maps a w×h region (scaled by zx, zy) rotated deg
// degrees about its center onto cursor (dx, dy) = center. Pure math,
// so rotation geometry is unit-testable without a game loop.
func rotateTransform(w, h int, zx, zy, deg, dx, dy float64) ebiten.GeoM {
	var m ebiten.GeoM
	cx, cy := float64(w)/2, float64(h)/2
	m.Translate(-cx, -cy)
	m.Scale(zx, zy)
	m.Rotate(deg * math.Pi / 180)
	m.Translate(dx, dy)
	return m
}

// BlitRotate copies a region of buffer src rotated deg degrees (about
// its center) onto the current target, centered at the cursor (dx, dy).
// Script threads enqueue; Update applies.
func (b *WindowBackend) BlitRotate(src, sx, sy, w, h int, deg, zx, zy float64, dx, dy int) error {
	if !b.hasBuf(src) {
		return fmt.Errorf("gcopy：不明なバッファ %d です", src)
	}
	if w <= 0 || h <= 0 {
		return fmt.Errorf("gcopy のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	if zx <= 0 || zy <= 0 {
		return fmt.Errorf("gcopy のスケールは正の値である必要があります。%g x %g が指定されました", zx, zy)
	}
	dst := b.currentSel()
	blend := b.currentBlend()
	b.enqueue(func() {
		simg := b.ensureTarget(src)
		part := simg.SubImage(image.Rect(sx, sy, sx+w, sy+h)).(*ebiten.Image)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Concat(rotateTransform(w, h, zx, zy, deg, float64(dx), float64(dy)))
		op.ColorScale.Scale(1, 1, 1, b.alphaScale())
		op.CompositeMode = blend
		b.ensureTarget(dst).DrawImage(part, op)
	})
	return nil
}

// FloodFill replaces the connected seed-color region at (x, y) with fg
// on the current target. A seed outside the canvas is an error; a seed
// already in fg is a no-op. Script threads enqueue; Update applies.
// Pixel access is batched (one readback, one writeback): per-pixel At/Set
// would round-trip the GPU on every call.
func (b *WindowBackend) FloodFill(x, y int, fg [4]int) error {
	b.mu.Lock()
	w, h := b.w, b.h
	b.mu.Unlock()
	if x < 0 || y < 0 || x >= w || y >= h {
		return fmt.Errorf("paint：開始点 (%d, %d) は %d x %d の範囲外です", x, y, w, h)
	}
	sel := b.currentSel()
	c := b.fillColor(fg)
	b.enqueue(func() {
		img := b.ensureTarget(sel)
		bw, bh := img.Bounds().Dx(), img.Bounds().Dy()
		if x >= bw || y >= bh {
			return
		}
		px := make([]byte, 4*bw*bh)
		img.ReadPixels(px)
		if flood(px, bw, bh, x, y, c) {
			img.WritePixels(px)
		}
	})
	return nil
}

// flood fills the connected seed-color region in RGBA pixels, reporting
// whether anything changed. Scanline flood with in-place marking: each
// pixel is written once, so the work is linear in the region size.
func flood(px []byte, w, h, x, y int, fill color.NRGBA) bool {
	si := (y*w + x) * 4
	seed := [4]byte{px[si], px[si+1], px[si+2], px[si+3]}
	if seed == [4]byte{fill.R, fill.G, fill.B, fill.A} {
		return false
	}
	same := func(i int) bool {
		return px[i] == seed[0] && px[i+1] == seed[1] && px[i+2] == seed[2] && px[i+3] == seed[3]
	}
	set := func(i int) {
		px[i], px[i+1], px[i+2], px[i+3] = fill.R, fill.G, fill.B, fill.A
	}
	stack := [][2]int{{x, y}}
	set(si)
	for len(stack) > 0 {
		cx, cy := stack[len(stack)-1][0], stack[len(stack)-1][1]
		stack = stack[:len(stack)-1]
		lx := cx
		for lx > 0 && same((cy*w+lx-1)*4) {
			lx--
			set((cy*w + lx) * 4)
		}
		rx := cx
		for rx+1 < w && same((cy*w+rx+1)*4) {
			rx++
			set((cy*w + rx) * 4)
		}
		for _, ny := range [2]int{cy - 1, cy + 1} {
			if ny < 0 || ny >= h {
				continue
			}
			nx := lx
			for nx <= rx {
				for nx <= rx && !same((ny*w+nx)*4) {
					nx++
				}
				if nx > rx {
					break
				}
				sx := nx
				for nx <= rx && same((ny*w+nx)*4) {
					set((ny*w + nx) * 4)
					nx++
				}
				stack = append(stack, [2]int{sx, ny})
			}
		}
	}
	return true
}

// SavePNG writes buffer id as a PNG file. It blocks until the game thread
// applies it (like PicLoad); call only from the script goroutine, never
// from Update/Draw (the waiter would deadlock the loop).
func (b *WindowBackend) SavePNG(path string, id int) error {
	if id < 0 || id >= maxBuffers {
		return fmt.Errorf("pngsave：id は 0 から %d の範囲で指定してください。%d が指定されました", maxBuffers-1, id)
	}
	if path == "" {
		return fmt.Errorf("pngsave：パスが空です")
	}
	b.mu.Lock()
	allocated := id == 0 || b.bufs[id]
	b.mu.Unlock()
	if !allocated {
		return fmt.Errorf("pngsave：不明なバッファ %d です", id)
	}
	var opErr error
	b.runOnLoop(func() {
		img := b.ensureTarget(id)
		bw, bh := img.Bounds().Dx(), img.Bounds().Dy()
		px := make([]byte, 4*bw*bh)
		img.ReadPixels(px)
		var buf bytes.Buffer
		// Opaque canvas pixels round-trip exactly; translucent ones
		// follow ebiten's readback convention.
		if err := png.Encode(&buf, &image.NRGBA{Pix: px, Stride: 4 * bw, Rect: image.Rect(0, 0, bw, bh)}); err != nil {
			opErr = fmt.Errorf("pngsave：エンコードエラー：%s", err.Error())
			return
		}
		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			opErr = fmt.Errorf("pngsave：%s", err.Error())
		}
	})
	return opErr
}
