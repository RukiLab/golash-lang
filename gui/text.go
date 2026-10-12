package gui

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Out exposes os.Stdout for exec passthrough in GUI mode.
func (b *WindowBackend) Out() io.Writer {
	return os.Stdout
}

// tabSpaces is the GUI rendering of a tab in mes()/print(): four
// half-width cells of blank. It is a fixed advance, not a tab stop, so
// the cell math stays independent of the current column.
const tabSpaces = "    "

// textOp is one ordered step of a mes()/print() call: an optional buffer
// wipe or upward scroll, followed by an optional text fragment. A whole
// call is applied as one game-thread closure in order, so text shares the
// canvas layer with pset()/line()/boxf(): a later boxf() covers earlier
// text, and later text covers an earlier boxf() (call order wins, like HSP).
type textOp struct {
	clear    bool // wipe the buffer (a scroll jump past a whole screen)
	scrollPx int  // shift the buffer image up first
	frag     bool // draw the fragment after scrolling
	x, y     int  // pixel origin of the fragment (whole pixels)
	s        string
	fg       color.NRGBA
	st       TextStyle
	face     *text.GoTextFace
	lineH    float64
	fontSize float64
}

// textDraw is one Print/Println call bound to its target buffer.
type textDraw struct {
	sel int
	ops []textOp
}

// textBatch accumulates consecutive Print/Println calls before they are
// handed to the game thread. A burst of mes()/print() calls is merged into
// one ordered op list (and normally one queued closure), while any other
// drawing, immediate frame read, yield, or target switch flushes first so
// script call order is preserved. Batched calls are not atomic: they are
// only a spool for repetition, mirroring Ebiten's own command batching.
type textBatch struct {
	sel int
	ops []textOp
}

func (x *textBatch) empty() bool {
	return x == nil || len(x.ops) == 0
}

// merge appends the next text call. Adjacent upward scrolls can be summed:
// with no fragment/clear in between, shifting twice in a row moves the
// same pixels as shifting once by the total.
func (x *textBatch) merge(sel int, ops []textOp) {
	if len(ops) == 0 {
		return
	}
	if x.sel != sel {
		x.sel = sel
	}
	for _, op := range ops {
		if n := len(x.ops); n > 0 && op.scrollPx > 0 && !op.clear && !op.frag &&
			x.ops[n-1].scrollPx > 0 && !x.ops[n-1].clear && !x.ops[n-1].frag {
			x.ops[n-1].scrollPx += op.scrollPx
			continue
		}
		x.ops = append(x.ops, op)
	}
}

// Print appends text without a newline, rasterized onto the current canvas
// buffer like any other drawing.
func (b *WindowBackend) Print(s string, st TextStyle) {
	b.printRaw(s, st, false)
}

// Println appends text with a trailing newline, rasterized onto the
// current canvas buffer like any other drawing.
func (b *WindowBackend) Println(s string, st TextStyle) {
	b.printRaw(s, st, true)
}

// printRaw lays out one call on the cell grid and stages it in the pending
// text batch (nil when nothing is visible). It returns the draw so tests
// can inspect fragment positions and styles.
func (b *WindowBackend) printRaw(s string, st TextStyle, newline bool) *textDraw {
	b.mu.Lock()
	d := b.layoutTextLocked(s, st, newline)
	if d != nil {
		if b.textBatch == nil {
			b.textBatch = &textBatch{}
		}
		b.textBatch.merge(d.sel, d.ops)
	}
	b.mu.Unlock()
	return d
}

// flushTextBatchLocked moves the accumulated Print/Println calls into one
// game-thread closure. Caller must hold mu.
func (b *WindowBackend) flushTextBatchLocked() {
	if b.textBatch.empty() {
		return
	}
	d := &textDraw{sel: b.textBatch.sel, ops: b.textBatch.ops}
	b.textBatch = nil
	if b.yieldedOnce {
		b.pending = append(b.pending, func() { b.applyTextDraw(d) })
		return
	}
	b.queue = append(b.queue, func() { b.applyTextDraw(d) })
}

// layoutTextLocked splits s into drawable fragments, advancing the text
// cursor by pixels. When the cursor passes the bottom edge the whole
// screen scrolls (text and graphics share one screen, like HSP). Caller
// must hold mu; the returned draw captures all rendering state, so later
// font()/color() calls never affect already-queued text.
func (b *WindowBackend) layoutTextLocked(s string, st TextStyle, newline bool) *textDraw {
	sel := b.sel
	// Text honors its own alpha multiplied by the global alpha (galpha),
	// exactly like fillColor() does for shapes.
	fg := color.NRGBA{R: b.fg.R, G: b.fg.G, B: b.fg.B,
		A: uint8(uint16(b.fg.A) * uint16(b.alpha) / 255)}
	face, lineH, fontSize := b.face, b.lineH, b.fontSize
	if lineH <= 0 {
		lineH = lineHeight
	}
	h := b.h
	// lastY is the lowest origin whose line box still fits the canvas;
	// a cursor below it scrolls (or clears on a huge jump).
	lastY := float64(h) - lineH
	d := &textDraw{sel: sel}
	// scrollForRow keeps the cursor visible. A jump past a whole screen
	// (e.g. pos() far below) clears instead of replaying hundreds of
	// full-screen shifts: the old content would scroll away anyway.
	scrollForRow := func() {
		if b.curY <= lastY {
			return
		}
		if b.curY-lastY > float64(h) {
			d.ops = append(d.ops, textOp{clear: true})
			b.curY = lastY
			return
		}
		for b.curY > lastY {
			// The current lineHeight ratio is an integer, so one row is
			// exactly one step; the carry still keeps a fractional pitch
			// (should lineHeight ever change) drift-free over time.
			b.scrollExact += lineH
			want := int(b.scrollExact + 0.5)
			px := want - b.scrollDone
			if px < 1 {
				// Sub-pixel heights can round to no step; always move at
				// least one pixel so the text never stalls.
				px = 1
				want = b.scrollDone + px
			}
			b.scrollDone = want
			d.ops = append(d.ops, textOp{scrollPx: px})
			b.curY -= lineH
		}
	}
	if s != "" {
		lines := strings.Split(s, "\n")
		for i, ln := range lines {
			if i > 0 {
				// One line down, back to the block anchor: continuation
				// lines start at the x this call began at (the pos()
				// origin), not at the screen's left edge.
				b.curY += lineH
				b.curX = b.lineStartX
			}
			// pos() may have parked the cursor off-screen: scroll (or
			// clear on a huge jump) before drawing, not after.
			scrollForRow()
			if ln == "" {
				continue
			}
			// A tab advances 4 half-width columns; expanding it here
			// keeps the drawn text and the cursor advance in agreement.
			ln = expandTabs(ln)
			d.ops = append(d.ops, textOp{frag: true,
				x: pxRound(b.curX), y: pxRound(b.curY), s: ln,
				fg: fg, st: st, face: face, lineH: lineH, fontSize: fontSize})
			// Advance by the width actually drawn, so the next fragment
			// (a continuation print(), or CJK echoed one char per call)
			// lands right where this one ended, at any face's metrics.
			adv, _ := text.Measure(ln, face, 0)
			b.curX += adv
		}
	}
	if newline {
		b.curY += lineH
		b.curX, b.lineStartX = 0, 0
		scrollForRow()
	}
	if len(d.ops) == 0 {
		return nil
	}
	return d
}

// pxRound snaps a float pixel origin to whole pixels: a subpixel origin
// would soften the raster (the glyph image lands between pixels), and a
// 1px-granular cursor has no use for the fractional part.
func pxRound(v float64) int {
	return int(math.Floor(v + 0.5))
}

// applyTextDraw runs a laid-out call on the game thread, in order.
func (b *WindowBackend) applyTextDraw(d *textDraw) {
	img := b.ensureTarget(d.sel)
	for _, op := range d.ops {
		if op.clear {
			img.Clear()
		}
		if op.scrollPx > 0 {
			b.scrollCanvasUp(img, op.scrollPx)
		}
		if op.frag {
			drawTextFrag(img, op)
		}
	}
}

// scrollCanvasUp shifts the buffer image up by px pixels, leaving the
// bottom strip transparent (black shows through). A scratch copy keeps the
// overlapping self-blit well-defined; the scratch is reused per size so
// scrolling stays allocation-free after the first shift.
func (b *WindowBackend) scrollCanvasUp(img *ebiten.Image, px int) {
	if px <= 0 {
		return
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if px >= h {
		img.Clear()
		return
	}
	upper := img.SubImage(image.Rect(0, px, w, h)).(*ebiten.Image)
	if b.scrollScratch == nil {
		b.scrollScratch = map[[2]int]*ebiten.Image{}
	}
	key := [2]int{w, h - px}
	tmp, ok := b.scrollScratch[key]
	if !ok || tmp == nil {
		tmp = ebiten.NewImage(w, h-px)
		b.scrollScratch[key] = tmp
	}
	tmp.DrawImage(upper, nil)
	img.Clear()
	img.DrawImage(tmp, nil)
}

// drawTextFrag rasterizes one fragment onto the canvas buffer with the
// faux text styles (single deformed face: bold over-strikes, italic
// shears, underline rules below the baseline). Because it writes straight
// into the canvas, shapes can cover the text and the text can cover
// shapes, depending on call order.
func drawTextFrag(img *ebiten.Image, op textOp) {
	if op.s == "" || op.face == nil || op.lineH <= 0 {
		return
	}
	baseX, baseY := float64(op.x), float64(op.y)
	italic := op.st&StyleItalic != 0
	draw := func(dx float64) {
		dop := &text.DrawOptions{}
		// Region top lands on the origin; glyphs hang below it.
		dop.GeoM.Translate(baseX+dx, baseY)
		if italic {
			// Element (0,1) is b in x' = a*x + b*y + tx:
			// slant glyphs right around the pen point.
			dop.GeoM.SetElement(0, 1, italicShear)
		}
		// ColorScale zero value is transparent; reset to identity first.
		dop.ColorScale.Reset()
		dop.ColorScale.ScaleWithColor(op.fg)
		text.Draw(img, op.s, op.face, dop)
	}
	draw(0)
	if op.st&StyleBold != 0 {
		dx := 1.0
		if op.fontSize >= 40 {
			dx = 2
		}
		draw(dx)
	}
	if op.st&StyleUnderline != 0 {
		w, _ := text.Measure(op.s, op.face, op.lineH)
		ascent := op.face.Metrics().HAscent
		descent := op.face.Metrics().HDescent
		th := float32(1)
		if op.fontSize >= 32 {
			th = 2
		}
		y := float32(baseY + ascent + descent*0.5) // baseline + half descent
		vector.StrokeLine(img, float32(baseX), y, float32(baseX+w), y, th, op.fg, false)
	}
}

// ReadLine is a Backend-interface stub: the GUI has no blocking line
// editor. GUI input() is immediate (see ReadImmediate); script text
// entry uses inputbox widgets. Always reports EOF so any accidental
// use ends promptly instead of hanging.
//
// IME: focus an inputbox (or call ime(1)) to enable conversion input
// (Windows/macOS/Web); committed text lands in the box or the
// input() stream, and the in-conversion string shows in the fixed bar.
func (b *WindowBackend) ReadLine(prompt string) (string, bool) {
	_ = prompt
	return "", false
}

// Clear wipes the current canvas buffer (text is baked into it, like any
// other drawing, so it goes together). Widgets survive: they are removed
// only by clrobj(), so draw loops can cls() every frame with live widgets.
// Canvas removal runs on the game thread via enqueue, so after the
// first yield it is staged in pending and applied together with the rest
// of the frame at await()/sleep()/end (no one-frame flicker).
func (b *WindowBackend) Clear() {
	b.mu.Lock()
	b.curX, b.curY = 0, 0
	b.lineStartX = 0
	b.scrollExact, b.scrollDone = 0, 0
	sel := b.sel
	b.mu.Unlock()
	b.enqueue(func() {
		b.ensureTarget(sel).Clear()
	})
}

// SetColor sets subsequent text/draw color (alpha included).
func (b *WindowBackend) SetColor(r, g, bl, a int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fg = color.NRGBA{R: uint8(clamp(r)), G: uint8(clamp(g)), B: uint8(clamp(bl)), A: uint8(clamp(a))}
}

// ResetColor restores black text (the white-window default).
func (b *WindowBackend) ResetColor() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fg = color.NRGBA{0x00, 0x00, 0x00, 0xFF}
}

// IsFullscreen reports fullscreen state (script-thread safe).
func (b *WindowBackend) IsFullscreen() bool {
	return ebiten.IsFullscreen()
}

// Closing reports whether the user asked to close the window. The
// first call opts in: the close button stops terminating the loop so
// the script can save and call end() itself.
func (b *WindowBackend) Closing() bool {
	b.mu.Lock()
	armed := b.closingArmed
	b.mu.Unlock()
	if !armed {
		ebiten.SetWindowClosingHandled(true)
		b.mu.Lock()
		b.closingArmed = true
		b.mu.Unlock()
	}
	return ebiten.IsWindowBeingClosed()
}

// SetFullscreen toggles fullscreen on the game thread.
func (b *WindowBackend) SetFullscreen(on bool) {
	b.runOnLoop(func() {
		ebiten.SetFullscreen(on)
	})
}

// IsResizable reports whether the window can be dragged to resize.
func (b *WindowBackend) IsResizable() bool {
	return ebiten.IsWindowResizable()
}

// SetResizable allows (or forbids) drag-resizing the window.
// While resizing is allowed the drawing resolution follows the window:
// the canvas is re-created at the window size (1:1 pixels, no scaling),
// so a widened window widens the drawing area instead of magnifying a
// fixed canvas. Turning it off freezes the current resolution; screen()
// re-establishes the logical size.
func (b *WindowBackend) SetResizable(on bool) {
	b.mu.Lock()
	b.followWindow = on
	b.mu.Unlock()
	ebiten.SetWindowResizable(on)
}

// WindowSize reports the OS window size in pixels: the real window, not
// the logical screen() canvas and not the monitor. Following a drag
// resize (see SetResizable), the window and the canvas are the same
// pixels. Safe to call headless in tests (Ebiten tracks the size even
// without a running loop).
func WindowSize() (w, h int) {
	return ebiten.WindowSize()
}

// MoveWindow moves the window (concurrent-safe; desktop only).
func (b *WindowBackend) MoveWindow(x, y int) {
	ebiten.SetWindowPosition(x, y)
}

// SetTitle changes the window title.
func (b *WindowBackend) SetTitle(s string) {
	ebiten.SetWindowTitle(s)
}

// SetFontSize changes the text face size in pixels (8..64). The line
// pitch scales with it so mes()/print() lines keep their spacing; widgets
// follow through uiFace. Script-thread safe (face/buffer swaps are
// mutex-guarded).
func (b *WindowBackend) SetFontSize(size int) error {
	if size < 8 || size > 64 {
		return fmt.Errorf("font：サイズは 8 から 64 の範囲で指定してください。%d が指定されました", size)
	}
	face, err := loadFace(float64(size))
	if err != nil {
		return fmt.Errorf("font：%s", err.Error())
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.face = face
	b.fontSize = float64(size)
	b.lineH = lineHeight * float64(size) / fontSize
	return nil
}

// FontSize reports the current face size in pixels.
func (b *WindowBackend) FontSize() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int(b.fontSize + 0.5)
}

// FontFile reports the resolved font file in use ("" when the
// discovery default is active and unchanged).
func (b *WindowBackend) FontFile() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fontPath != "" {
		return b.fontPath
	}
	return FontPath()
}

// TextWidth measures s in pixels at the current face (whole string;
// editors typically pass a single line for caret placement). Tabs count
// as tabSpaces, exactly like the mes()/print() canvas rendering.
func (b *WindowBackend) TextWidth(s string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.face == nil {
		return len([]rune(expandTabs(s)))
	}
	w, _ := text.Measure(expandTabs(s), b.face, 0)
	return int(w + 0.5)
}

// expandTabs replaces every tab with tabSpaces, the fixed 4-cell advance
// mes()/print() render (and strwidth() measures).
func expandTabs(s string) string {
	if strings.ContainsRune(s, '\t') {
		return strings.ReplaceAll(s, "\t", tabSpaces)
	}
	return s
}

// SetFontFile switches the typeface to spec (path or system font name)
// at the given pixel size (8..64), rescaling the line pitch like
// SetFontSize. Script-thread safe. extraDirs are searched before the
// system font directories (script directory, working directory).
func (b *WindowBackend) SetFontFile(spec string, size int, extraDirs ...string) error {
	if size < 8 || size > 64 {
		return fmt.Errorf("font：サイズは 8 から 64 の範囲で指定してください。%d が指定されました", size)
	}
	src, path, err := loadFontSpec(spec, extraDirs...)
	if err != nil {
		return err
	}
	face := &text.GoTextFace{Source: src, Size: float64(size)}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.face = face
	b.fontSize = float64(size)
	b.lineH = lineHeight * float64(size) / fontSize
	b.fontPath = path
	return nil
}

// MoveTo sets the cursor. In GUI both cursors are pixel-based: the
// graphics cursor (gcopy etc) and the mes()/print() text origin, which
// takes the pixel as-is (1px granularity, no character-cell rounding).
// In CUI the console backend moves by character cells.
func (b *WindowBackend) MoveTo(x, y int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gx = x
	b.gy = y
	b.curX, b.curY = float64(x), float64(y)
	// A pos() call re-anchors the line: embedded newlines from here on
	// return to this x.
	b.lineStartX = b.curX
}

// Sleep pauses the script; the window keeps rendering.
// Like await() it is a frame boundary: the frame built so far (text and
// shapes alike, all staged canvas commands) is published first, so the
// paused frame is complete (no half-drawn content left over during pause).
func (b *WindowBackend) Sleep(ms int64) {
	b.publishAtYieldLocked()
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
