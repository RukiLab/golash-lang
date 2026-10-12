// Package gui implements the Ebiten WindowBackend (G0: window, text,
// close handling). Drawing (G1), polling input/audio/dialog (G2) and
// widgets (G3) extend this file set.
package gui

import (
	"errors"
	"image"
	"image/color"
	"io/fs"
	"sync"

	"github.com/ebitenui/ebitenui"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/exp/textinput"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	fontSize   = 16.0
	lineHeight = 16.0
	// italicShear slants faux-italic glyphs (~11 degrees).
	italicShear = 0.2
)

// TextStyle is a bitmask of faux text decorations for mes()/print().
// Styles deform the single loaded face (no variant files needed):
// bold over-strikes, italic shears, underline rules below the baseline.
type TextStyle int

const (
	StyleBold TextStyle = 1 << iota
	StyleItalic
	StyleUnderline
)

// ParseTextStyle maps a mes()/print() trailing keyword to a style flag.
// Exact lowercase match only, so ordinary words never vanish from output.
func ParseTextStyle(s string) (TextStyle, bool) {
	switch s {
	case "bold":
		return StyleBold, true
	case "italic":
		return StyleItalic, true
	case "bolditalic":
		return StyleBold | StyleItalic, true
	case "underline":
		return StyleUnderline, true
	}
	return 0, false
}

// WindowBackend is a Backend rendering into an Ebiten window.
// All state is guarded by mu; the Ebiten loop and the script goroutine
// share it. Canvas images are touched only on the game thread: the
// script enqueues draw commands processed by Update.
type WindowBackend struct {
	mu   sync.Mutex
	w, h int
	// followWindow mirrors resizable(): while the window may be dragged,
	// Layout returns the window size so the drawing resolution follows it
	// (the canvas is re-created at the window size, 1:1 pixels) instead of
	// pinning the screen() size and scaling the view.
	followWindow bool
	// Text layout state (guarded by mu). Text is rasterized into the
	// canvas buffer (targets[sel]) exactly like pset()/line()/boxf(), so
	// text and shapes share one layer and the call order decides what
	// covers what (HSP-like). These fields are only the script-side
	// cursor: nothing is drawn until the frame boundary applies the
	// queued draw commands.
	// Text layout state (guarded by mu), in device pixels: pos() places
	// the text origin exactly (1px granularity, no cell rounding), and
	// each mes()/print() fragment advances the cursor by the width it
	// actually drew, so a proportional face flows naturally.
	curX float64
	curY float64
	// lineStartX is the x the current text line started at: 0 after a
	// full-line break, the pos() origin otherwise. Embedded newlines
	// (\n inside one string) return the cursor here instead of to 0,
	// so a multi-line mes() prints as a block from its anchor.
	lineStartX float64
	// textBatch spools consecutive mes()/print() calls on the script side
	// so a burst shares one queued closure and adjacent repeated scrolls
	// collapse (guarded by mu).
	textBatch *textBatch
	// scrollExact is the exact accumulated scroll height and scrollDone
	// the pixels already shifted, so the scroll never drifts the baked
	// text off the cell grid even for a fractional line height.
	scrollExact float64
	scrollDone  int
	// yieldedOnce: the script has parked in await()/sleep() at least once.
	// From then on canvas commands (text included) are staged in pending
	// and published only at yield points (await/sleep/end) instead of
	// every Update, so a frame reaches the screen only after the script
	// finished building it (see publishAtYieldLocked).
	yieldedOnce bool

	gx, gy int // pixel cursor for graphics (gcopy destination)
	fg     color.NRGBA
	// Font state (guarded by mu): face plus the line pitch derived from
	// it (one mes()/print() line is lineH tall), at any size.
	face     *text.GoTextFace
	fontSize float64
	lineH    float64
	fontPath string // resolved font file in use ("": discovery default)
	wantDone bool   // script finished; keep window open
	wantQuit bool   // end() called; close the window
	// Canvas state (images only on the game thread).
	targets map[int]*ebiten.Image // draw buffers by id; 0 is main
	bufs    map[int]bool          // allocated ids (guarded by mu)
	sel     int                   // current draw target (guarded by mu)
	// scrollScratch reuses the temp image used for canvas text scrolling,
	// so a flurry of scrolls does not allocate a new GPU image every line.
	// Game thread only; keyed per buffer size below.
	scrollScratch map[[2]int]*ebiten.Image
	blend         ebiten.CompositeMode
	// persp is the shared perspective-correct texture shader for
	// gquad() (compiled once on the game thread; see
	// persp.go). perspErr pins a compile failure so every later draw
	// takes the affine fallback instead of retrying each frame.
	persp    *ebiten.Shader
	perspErr error
	queue    []func()
	// pending はフレーム構築中の canvas コマンド置き場。yieldedOnce 以後は
	// enqueue がここへ溜め、await()/sleep()/end のフレーム境界で queue へ
	// 一括移動する。これで cls() 直後の空 canvas や描きかけが1フレーム
	// 表示される撕裂 (ちらつき) が出ない。await 前のスクリプトは高速に
	// 全コマンドを積み、次の Update で原子的に適用される。
	pending []func()
	// drawSel は Draw が読む公開済み可視バッファ。gsel() の途中切替が
	// そのまま画面に出て裏バッファの描きかけが見えるのを防ぐ。
	drawSel int
	// Input latch + audio (G2). Mouse buttons are indexed 0 left,
	// 1 middle, 2 right (clicked() numbering).
	mouseDown  [3]bool
	clickFlag  [3]bool
	wheelAccum float64 // unconsumed vertical wheel detents
	audio      *audioState
	// Global drawing alpha 0-255 (galpha; shapes and blits).
	alpha uint8
	// Frame counter for await()/tick() (guarded by mu).
	tick     int64
	tickWait []chan struct{}
	// closingArmed enables close-button handling (closing(); guarded).
	closingArmed bool
	// Dropped files (dropfiles/dropload; guarded by mu).
	dropNames []string
	dropFS    fs.FS // last snapshot (kept for dropload)
	// Immediate-input repeat state (input(); guarded by mu).
	charSt charRep
	ctrlSt []ctrlState // one slot per ctrlKeys entry
	// IME state (ime only; Field is pumped on the game thread only,
	// mirrors are guarded by mu for bar/draw reads).
	imeField   textinput.Field
	imePrev    string // previous tick's committed field text (diff base)
	imePending string // committed text awaiting input()
	// imeComposing is the uncommitted (conversion) text mirror.
	imeComposing string
	// imeClause mirrors the target conversion clause (文節) inside
	// imeComposing ("" when unfocused, idle, or the platform reports
	// no clause range). imeClauseStart/End are its rune offsets into
	// imeComposing (0, 0 when the clause is empty).
	imeClause      string
	imeClauseStart int
	imeClauseEnd   int
	// imeOff is the sticky script-side disable set by ime(0): box
	// focus never auto-focuses the IME field while set. ime(1) or
	// the next box focus edge clears it. Guarded by mu.
	imeOff bool
	// Retained widgets (G3, game thread only except where noted).
	ui        *ebitenui.UI
	root      *widget.Container
	fixLayout *fixedLayout
	widgets   map[int]*widgetEntry
	uiW, uiH  int // root location size applied
}

// New creates a window backend of w x h logical pixels.
func New(w, h int) (*WindowBackend, error) {
	face, err := loadFace(fontSize)
	if err != nil {
		return nil, err
	}
	b := &WindowBackend{
		w: w, h: h,
		ctrlSt:   make([]ctrlState, len(ctrlKeys)),
		// White window: the default face is black text on white,
		// matching the Windows-standard widget theme.
		fg:       color.NRGBA{0x00, 0x00, 0x00, 0xFF},
		face:     face,
		fontSize: fontSize,
		lineH:    lineHeight,
		alpha:    0xFF,
		// Image maps must exist before the first image op: PicLoad
		// and DropLoad write targets without going through
		// ensureTarget, so lazy init would panic on nil map.
		targets: make(map[int]*ebiten.Image),
		bufs:    make(map[int]bool),
	}
	// 初期フレーム (空 canvas) も Draw に公開しておく。
	b.mu.Lock()
	b.publishFrameLocked()
	b.mu.Unlock()
	return b, nil
}

// RunLoop runs run() in a goroutine and the Ebiten loop on this thread.
// It returns when the window closes or the script calls end().
func RunLoop(be *WindowBackend, run func()) int {
	ebiten.SetWindowSize(be.w, be.h)
	ebiten.SetWindowTitle("Golash")
	ebiten.SetVsyncEnabled(true)
	go run()
	if err := ebiten.RunGame(be); err != nil && !errors.Is(err, ebiten.Termination) {
		return 1
	}
	return 0
}

// SetDone marks natural script completion (window stays open).
// The final frame is published so the finished output stays on screen.
func (b *WindowBackend) SetDone() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wantDone = true
	b.publishFrameLocked()
	b.yieldedOnce = true
}

// RequestClose asks the game loop to terminate (end() builtin).
// The last frame is published before the window goes away.
func (b *WindowBackend) RequestClose() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wantQuit = true
	b.publishFrameLocked()
	b.yieldedOnce = true
}

// publishAtYieldLocked commits the frame at a script yield point
// (await()/sleep()) and switches Update off from publishing.
// Caller must NOT hold mu.
func (b *WindowBackend) publishAtYieldLocked() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.publishFrameLocked()
	b.yieldedOnce = true
}

// Closed reports whether the loop should exit.
func (b *WindowBackend) Closed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wantQuit
}

// publishFrameLocked commits the frame being built: staged canvas
// commands (shapes and mes()/print() text alike) move to the game queue
// as one batch and the visible buffer sel is snapshotted into drawSel.
// Caller must hold mu.
// Draw reads only the snapshot, so the working state of a cls()/mes()
// sequence in progress is never rendered: a frame is committed either at
// a script yield point (Await/Sleep, i.e. once the frame is complete) or
// by Update while the script has not yielded yet.
// Text is baked into the canvas buffer, so it needs no snapshot of its
// own: it is committed with the rest of the frame, and the committed
// buffer (drawSel) keeps the previous frame intact until then.
func (b *WindowBackend) publishFrameLocked() {
	b.flushTextBatchLocked()
	if len(b.pending) > 0 {
		b.queue = append(b.queue, b.pending...)
		b.pending = nil
	}
	b.drawSel = b.sel
}

// advanceTick bumps the frame counter and wakes await() callers.
// Called from Update (game thread); tests drive it directly.
func (b *WindowBackend) advanceTick() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tick++
	for _, ch := range b.tickWait {
		close(ch)
	}
	b.tickWait = nil
}

// Tick is the Update counter (script-thread safe).
func (b *WindowBackend) Tick() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tick
}

// Await blocks until n frames pass. Must come from the script goroutine,
// never the game thread (Update could not run to wake it).
// The call is the frame boundary: everything drawn since the previous
// await (shapes and mes()/print() text alike) is committed here, so Draw
// never observes a half-drawn frame
// (cls() 直後や mes() 前の状態が画面に出るちらつきの防止).
// await(0) therefore doubles as an explicit "show this frame now".
func (b *WindowBackend) Await(n int64) {
	b.publishAtYieldLocked()
	if n <= 0 {
		return
	}
	for {
		b.mu.Lock()
		target := b.tick + n
		// Re-check under the lock: ticks may have passed already.
		if b.tick >= target {
			b.mu.Unlock()
			return
		}
		ch := make(chan struct{})
		b.tickWait = append(b.tickWait, ch)
		b.mu.Unlock()
		<-ch
		n = target - b.Tick()
		if n <= 0 {
			return
		}
	}
}

// --- ebiten.Game ---

// Update pumps input, applies queued draw commands, updates widgets.
func (b *WindowBackend) Update() error {
	b.drainQueue()
	b.pumpInput()
	b.pumpDrops()
	b.pumpIME()
	b.pumpInputs()
	// Root は window 全面。リサイズ反映は Update 側で行い、Draw 中の
	// SetLocation を避ける (Draw 中のレイアウト変更は ebitenui の描画を
	// 1フレーム乱し、GUI部品のちらつき・ずれに見えるため)。
	if b.ui != nil {
		b.mu.Lock()
		ww, hh := b.w, b.h
		needReloc := b.root != nil && (b.uiW != ww || b.uiH != hh)
		b.mu.Unlock()
		if needReloc {
			b.mu.Lock()
			b.uiW, b.uiH = ww, hh
			b.mu.Unlock()
			b.root.SetLocation(image.Rect(0, 0, ww, hh))
		}
		if b.fixLayout != nil {
			b.fixLayout.w, b.fixLayout.h = ww, hh
		}
		b.ui.Update()
	}
	b.syncWidgetCache()
	// フレーム確定は原則ここでは行わない (Draw は確定済みフレームのみ描く)。
	// await()/sleep() を一度も使わないスクリプトはフレーム境界が無いので、
	// その場合だけ Update で確定して描画がいつまでも出ないのを防ぐ。
	b.mu.Lock()
	if !b.yieldedOnce {
		b.publishFrameLocked()
	}
	b.mu.Unlock()
	b.advanceTick()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.wantQuit {
		return ebiten.Termination
	}
	return nil
}

// Draw renders the committed canvas frame plus widgets and input boxes.
// Text lives in the same canvas buffer as the shapes (mes()/print() pixels
// are rasterized into it by applyTextDraw), so Draw only blits one layer:
// whichever was drawn last in the script wins, exactly like HSP.
func (b *WindowBackend) Draw(screen *ebiten.Image) {
	// White window: cleared (transparent) canvas shows white.
	screen.Fill(color.White)
	// Visible canvas: the committed buffer (drawSel). gsel() の途中切替が
	// そのまま出ると裏バッファの描きかけが1フレーム見えるため、frame 境界で
	// 公開された面だけを描く。targets 自体は game thread のみが触る。
	b.mu.Lock()
	sel := b.drawSel
	b.mu.Unlock()
	if img := b.targets[sel]; img != nil {
		screen.DrawImage(img, nil)
	}

	if b.ui != nil {
		b.ui.Draw(screen)
	}
	b.drawInputs(screen)
	b.drawIMEComp(screen)
}

// Layout fixes the logical screen size. While resizable() is on, the
// drawing resolution follows the window instead: the canvas is re-created
// at the window size (pixel 1:1, no scaling), so dragging the window
// widens the drawing area rather than magnifying a fixed canvas.
func (b *WindowBackend) Layout(outsideWidth, outsideHeight int) (int, int) {
	b.mu.Lock()
	if !b.followWindow || outsideWidth <= 0 || outsideHeight <= 0 ||
		(outsideWidth == b.w && outsideHeight == b.h) {
		defer b.mu.Unlock()
		return b.w, b.h
	}
	b.w, b.h = outsideWidth, outsideHeight
	b.mu.Unlock()
	// Layout runs on the game thread before Update/Draw of the same
	// frame, so the canvas swap lands with the grown framebuffer.
	b.resizeMainCanvas(outsideWidth, outsideHeight, true)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.w, b.h
}
