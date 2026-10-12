package gui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"

	"github.com/ebitenui/ebitenui"
	euimage "github.com/ebitenui/ebitenui/image"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// Widgets (G3) on ebitenui: buttons, text inputs, lists, modal dialogs.
// Script threads enqueue mutations; the game thread applies them.
// HSP-style absolute positioning via fixedLayout.

// fixedLayout places children at exact rectangles (HSP-like coordinates).
type fixedLayout struct {
	w, h  int
	rects map[widget.PreferredSizeLocateableWidget]image.Rectangle
}

func (l *fixedLayout) PreferredSize(widgets []widget.PreferredSizeLocateableWidget) (int, int) {
	return l.w, l.h
}

func (l *fixedLayout) Layout(widgets []widget.PreferredSizeLocateableWidget, rect image.Rectangle) {
	for _, w := range widgets {
		if r, ok := l.rects[w]; ok {
			w.SetLocation(r)
		}
	}
}

type widgetKind int

const (
	wButton widgetKind = iota
	wInput
	wList
	wCheck
	wCombo
	wArea
)

type widgetEntry struct {
	id     int
	kind   widgetKind
	child  widget.PreferredSizeLocateableWidget
	remove widget.RemoveChildFunc
	// disabled freezes interaction (objprm "enable"; guarded by mu).
	disabled bool
	// button latch + list state (guarded by backend mu).
	pressed bool
	items   []string
	selIdx  int
	// cached mirrors the widget's text, refreshed on the game thread
	// every Update so InputText never blocks the script (a blocking
	// read mid-frame would stall cls/mes redraws and flicker).
	cached string
	// form widgets (G5): checkbox, combo, textarea mirrors.
	check   *widget.Checkbox
	combo   *widget.ListComboButton
	area    *widget.TextArea
	checked bool
	list    *widget.List
	// custom-drawn single-line editor (wInput; no ebitenui child).
	edit *inputState
}

// ensureUI builds the retained UI on the game thread.
func (b *WindowBackend) ensureUI() {
	if b.ui != nil {
		return
	}
	b.mu.Lock()
	w, h := b.w, b.h
	b.mu.Unlock()
	fix := &fixedLayout{w: w, h: h, rects: map[widget.PreferredSizeLocateableWidget]image.Rectangle{}}
	root := widget.NewContainer(widget.ContainerOpts.Layout(fix))
	b.fixLayout = fix
	b.root = root
	b.mu.Lock()
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	b.mu.Unlock()
	// Tab/Shift+Tab の既定フォーカス移動は無効にする (ebitenui の
	// handleFocusChangeRequest を止める)。Tab はキー入力 (keypush 9・
	// 自前エディタのタブ挿入) として使うためで、クリックによる
	// フォーカスは引き続き有効。プログラム側の Focus() も不変。
	b.ui = &ebitenui.UI{Container: root, DisableDefaultFocus: true}
}

// placeChild adds a child at an absolute rect on the game thread.
// widgets マップは script goroutine と共有のため mu で保護する。
// ebitenui 呼び出しはコールバックで mu を掴まないので、保持したまま
// 行い原子性を保つ (途中状態を Pressed/selected 等が観測してちらつかない)。
func (b *WindowBackend) placeChild(id int, e *widgetEntry, child widget.PreferredSizeLocateableWidget, x, y, w, h int) {
	b.ensureUI()
	b.mu.Lock()
	defer b.mu.Unlock()
	if old, ok := b.widgets[id]; ok {
		old.remove()
		delete(b.fixLayout.rects, old.child)
	}
	b.root.AddChild(child)
	b.fixLayout.rects[child] = image.Rect(x, y, x+w, y+h)
	remove := func() {
		b.root.RemoveChild(child)
		delete(b.fixLayout.rects, child)
	}
	e.child = child
	e.remove = remove
	b.widgets[id] = e
	b.root.RequestRelayout()
}

// placeCustom registers a custom-drawn entry (toggle/input: no
// ebitenui child), replacing any entry under id. Game thread only.
func (b *WindowBackend) placeCustom(id int, e *widgetEntry) {
	b.ensureUI()
	b.mu.Lock()
	defer b.mu.Unlock()
	if old, ok := b.widgets[id]; ok {
		old.remove()
		if old.child != nil {
			delete(b.fixLayout.rects, old.child)
		}
	}
	e.child = nil
	e.remove = func() {}
	b.widgets[id] = e
	if b.root != nil {
		b.root.RequestRelayout()
	}
}

// removeWidgetLocked drops a widget; caller runs on the game thread.
func (b *WindowBackend) removeWidgetLocked(id int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok {
		return fmt.Errorf("不明なウィジェット %d です", id)
	}
	e.remove()
	delete(b.widgets, id)
	return nil
}

// ---------- theme (Windows standard: white, gray frame, black text) ----------

// Fixed palette: white faces, 1px gray borders, black text. Focus is a
// pale-blue face with a blue frame; pressed/selected is a blue face
// with a dark-blue frame. Hover keeps its own pale blue. No per-widget
// overrides: objprm "textcolor"/"backcolor" were removed with the dark
// theme.
var (
	themeWhite      = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	themeFrame      = color.NRGBA{0x80, 0x80, 0x80, 0xFF}
	themeInk        = color.NRGBA{0x00, 0x00, 0x00, 0xFF}
	themeGrey       = color.NRGBA{0x77, 0x77, 0x77, 0xFF}
	themeHover      = color.NRGBA{0xE5, 0xF3, 0xFF, 0xFF}
	themeFocusBg    = color.NRGBA{224, 238, 249, 0xFF}
	themeFocusFrame = color.NRGBA{38, 140, 218, 0xFF}
	themePressBg    = color.NRGBA{204, 228, 247, 0xFF}
	themePressFrame = color.NRGBA{34, 108, 169, 0xFF}
	themeFaceDis    = color.NRGBA{0xF0, 0xF0, 0xF0, 0xFF}
)

// frame builds a stretchable face: 1px border around bg. A 3x3 source
// with 1px insets stretches without distorting the frame. Game thread
// only (creates an image; ebiten.NewImage also works headless in tests).
func frame(bg, border color.NRGBA) *euimage.NineSlice {
	src := ebiten.NewImage(3, 3)
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			c := bg
			if x == 0 || y == 0 || x == 2 || y == 2 {
				c = border
			}
			src.Set(x, y, c)
		}
	}
	return euimage.NewNineSlice(src, [3]int{1, 1, 1}, [3]int{1, 1, 1})
}

// nineImage stretches src across the widget (pure-stretch nine-slice,
// zero borders). Snapshots are already button-sized, so it is exact.
func nineImage(src *ebiten.Image) *euimage.NineSlice {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	return euimage.NewNineSlice(src, [3]int{0, w, 0}, [3]int{0, h, 0})
}

// uiFace adapts our face to ebitenui's *text.Face (pointer to interface).
// The face swaps on font(), so read it under mu.
func (b *WindowBackend) uiFace() *text.Face {
	b.mu.Lock()
	defer b.mu.Unlock()
	f := text.Face(b.face)
	return &f
}

func (b *WindowBackend) buttonImage() *widget.ButtonImage {
	idle := frame(themeWhite, themeFrame)
	hover := frame(themeHover, themeFrame)
	pressed := frame(themePressBg, themePressFrame)
	dis := frame(themeFaceDis, themeFrame)
	// PressedHover/PressedDisabled を nil のままにすると、押下+ホバー時に
	// ebitenui が nil NineSlice を描画して背景が透明に瞬く (ちらつき) ので
	// 対応する通常状態で埋める。
	return &widget.ButtonImage{
		Idle: idle, Hover: hover,
		Pressed: pressed, PressedHover: pressed, Disabled: dis, PressedDisabled: dis,
	}
}

// buttonTextColor は全状態で不透明な文字色を返す。Idle のみ指定すると
// Hover/Pressed 時に nil 色 (透明) が使われて文字が消えて見えるため。
func buttonTextColor() *widget.ButtonTextColor {
	return &widget.ButtonTextColor{Idle: themeInk, Hover: themeInk, Pressed: themeInk, Disabled: themeGrey}
}

// checkBoxSize is the checkbox art edge length, tracking font().
// Even and >= 12 so the nine-slice faces below reassemble pixel-perfect.
func (b *WindowBackend) checkBoxSize() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.checkBoxSizeLocked()
}

// checkBoxSizeLocked is checkBoxSize without locking (caller holds mu).
func (b *WindowBackend) checkBoxSizeLocked() int {
	s := int(b.fontSize*1.25 + 0.5)
	if s < 12 {
		s = 12
	}
	if s > 48 {
		s = 48
	}
	return s &^ 1
}

// checkArtRGBA renders one size×size checkbox face into plain pixels:
// body fill, 1px frame ring, and an optional check glyph. Pure Go, so
// tests can inspect the art without a game loop.
func checkArtRGBA(size int, body, frameC color.NRGBA, check bool, mark color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := body
			if x == 0 || y == 0 || x == size-1 || y == size-1 {
				c = frameC
			}
			img.SetNRGBA(x, y, c)
		}
	}
	if check {
		drawCheckMark(img, size, mark)
	}
	return img
}

// checkArt uploads one checkbox face. Created on the game thread
// (like all images; ebiten.NewImage also works headless in tests).
// The ring uses the theme frame so the white box reads on any canvas.
func checkArt(size int, body color.NRGBA, check bool, mark color.NRGBA) *ebiten.Image {
	return ebiten.NewImageFromImage(checkArtRGBA(size, body, themeFrame, check, mark))
}

// drawCheckMark paints a check from (0.28,0.55)->(0.45,0.72)->(0.74,0.30)
// with a square brush, so the checked state reads at any box size.
func drawCheckMark(img *image.NRGBA, size int, c color.NRGBA) {
	f := float64(size)
	pts := [][2]float64{{0.28 * f, 0.55 * f}, {0.45 * f, 0.72 * f}, {0.74 * f, 0.30 * f}}
	r := size / 8
	if r < 1 {
		r = 1
	}
	dot := func(x, y int) {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				img.SetNRGBA(x+dx, y+dy, c)
			}
		}
	}
	for i := 0; i+1 < len(pts); i++ {
		x0, y0, x1, y1 := pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1]
		dx, dy := x1-x0, y1-y0
		steps := int(absf(dx) + absf(dy) + 1)
		for s := 0; s <= steps; s++ {
			t := float64(s) / float64(steps)
			dot(int(x0+dx*t+0.5), int(y0+dy*t+0.5))
		}
	}
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// checkImage builds the checkbox faces with a real minimum size
// (white box, gray ring, black check glyph).
//
// NOTE: solid-color nine slices (NewNineSliceColor) report MinSize 0,
// and ebitenui draws the *labeled* box at checkboxPreferredSize, so the
// old faces rendered 0x0 (invisible box, label text only). Sized art
// reassembles pixel-perfect at MinSize == size and fixes it.
func (b *WindowBackend) checkImage() *widget.CheckboxImage {
	return b.checkImageForSize(b.checkBoxSize())
}

func (b *WindowBackend) checkImageForSize(s int) *widget.CheckboxImage {
	half := s / 2
	mk := func(body color.NRGBA, check bool, mark color.NRGBA) *euimage.NineSlice {
		return euimage.NewNineSliceSimple(checkArt(s, body, check, mark), half, 0)
	}
	return &widget.CheckboxImage{
		Unchecked:         mk(themeWhite, false, themeInk),
		UncheckedHovered:  mk(themeHover, false, themeInk),
		UncheckedDisabled: mk(themeFaceDis, false, themeGrey),
		Checked:           mk(themeWhite, true, themeInk),
		CheckedHovered:    mk(themeHover, true, themeInk),
		CheckedDisabled:   mk(themeFaceDis, true, themeGrey),
	}
}

// listEntryColors defines every list state. Press/hover backgrounds must
// never stay nil (transparent): with SelectPressed the entry is selected
// on press, and a nil Pressed background would hide the highlight until
// mouse release even though selected() already flipped. Selecting and
// selected entries use the pressed blue; the focused (but unselected)
// entry uses the focus pale blue (entries are flat colors, no frames).
func listEntryColors() *widget.ListEntryColor {
	return &widget.ListEntryColor{
		Unselected:                 themeInk,
		Selected:                   themeInk,
		DisabledUnselected:         themeGrey,
		DisabledSelected:           themeGrey,
		SelectingBackground:        themePressBg,
		SelectedBackground:         themePressBg,
		FocusedBackground:          themeFocusBg,
		SelectingFocusedBackground: themePressBg,
		SelectedFocusedBackground:  themePressBg,
		DisabledSelectedBackground: themeFaceDis,
	}
}

// ---------- script-thread API (mutations block until applied) ----------

// AddButton creates (or replaces) a button. imgs holds up to 4
// picload/dropload buffer ids for the Idle/Hover/Pressed/Disabled
// states (0 = slot unused): missing Hover/Pressed fall back to Idle,
// missing Disabled falls back to a darkened Idle.
func (b *WindowBackend) AddButton(id int, label string, x, y, w, h int, imgs ...int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("button のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	if len(imgs) > 4 {
		return fmt.Errorf("button：画像は最大 4 個（通常/ホバー/押下/無効）です。%d 個が指定されました", len(imgs))
	}
	for _, imgID := range imgs {
		if imgID != 0 && !b.hasBuf(imgID) {
			return fmt.Errorf("button：不明な画像バッファ %d です", imgID)
		}
	}
	var opErr error
	b.runOnLoop(func() {
		face := b.uiFace()
		e := &widgetEntry{id: id, kind: wButton, selIdx: -1}
		bi := b.buttonImage()
		bt := buttonTextColor()
		opts := []widget.ButtonOpt{
			widget.ButtonOpts.Image(bi),
			// Tight padding so short tab bars (18px with a 16px
			// font) keep their labels inside the face.
			widget.ButtonOpts.TextPadding(widget.NewInsetsSimple(2)),
			widget.ButtonOpts.ClickedHandler(func(args *widget.ButtonClickedEventArgs) {
				b.mu.Lock()
				e.pressed = true
				b.mu.Unlock()
			}),
		}
		if len(imgs) > 0 && imgs[0] != 0 {
			// Snapshot each state into button-sized images: Graphic
			// centers its image, so a raw window-sized buffer would
			// show the wrong (usually transparent) region. Exact-fit
			// buffers keep the live reference.
			snap := func(src *ebiten.Image) *ebiten.Image {
				sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
				if sw == w && sh == h {
					return src
				}
				out := ebiten.NewImage(w, h)
				op := &ebiten.DrawImageOptions{}
				op.GeoM.Scale(float64(w)/float64(sw), float64(h)/float64(sh))
				out.DrawImage(src, op)
				return out
			}
			srcs := make([]*ebiten.Image, 4)
			for i, imgID := range imgs {
				if imgID == 0 {
					continue
				}
				src := b.targets[imgID]
				if src == nil {
					opErr = fmt.Errorf("button：画像バッファ %d はまだ準備できていません", imgID)
					return
				}
				srcs[i] = snap(src)
			}
			idle := srcs[0]
			hover := srcs[1]
			if hover == nil {
				hover = idle
			}
			pressed := srcs[2]
			if pressed == nil {
				pressed = idle
			}
			disabled := srcs[3]
			if disabled == nil {
				disabled = dimImage(idle)
			}
			if label != "" {
				// Labeled: the icon row is centered by design, and
				// TextAndImage enables ebitenui's per-state swap
				// (separate Text+Graphic opts leave it frozen).
				opts = append(opts, widget.ButtonOpts.TextAndImage(label, face, &widget.GraphicImage{
					Idle: idle, Hover: hover, Pressed: pressed, Disabled: disabled,
				}, bt))
			} else {
				// Icon-only: state faces fill the button exactly.
				// (A Graphic row would add an empty-text member and
				// shift the icon a few pixels off center.)
				// PressedHover 等も埋めないと押下+ホバーで透明に瞬く。
				niIdle, niHover := nineImage(idle), nineImage(hover)
				niPressed, niDis := nineImage(pressed), nineImage(disabled)
				bi.Idle, bi.Hover = niIdle, niHover
				bi.Pressed, bi.PressedHover = niPressed, niPressed
				bi.Disabled, bi.PressedDisabled = niDis, niDis
				opts = append(opts, widget.ButtonOpts.Image(bi))
			}
		} else if label != "" {
			opts = append(opts, widget.ButtonOpts.Text(label, face, bt))
		}
		b.placeChild(id, e, widget.NewButton(opts...), x, y, w, h)
	})
	return opErr
}

// dimImage returns a darkened copy for the Disabled state.
// Call on the game thread (creates an image).
func dimImage(src *ebiten.Image) *ebiten.Image {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	out := ebiten.NewImage(w, h)
	op := &ebiten.DrawImageOptions{}
	op.ColorScale.Scale(0.45, 0.45, 0.45, 1)
	out.DrawImage(src, op)
	return out
}

// Pressed reports a click since the last call (consumed).
func (b *WindowBackend) Pressed(id int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wButton {
		return false, fmt.Errorf("pressed：不明なボタン %d です", id)
	}
	p := e.pressed
	e.pressed = false
	return p, nil
}

// AddInput lives in edit.go (custom-drawn editor).

// syncWidgetCache mirrors widget runtime state (game thread only;
// called from Update). Script reads use the mirrors so getters never
// block the script mid-frame (cf. the gettext flicker fix).
func (b *WindowBackend) syncWidgetCache() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.widgets {
		switch {
		case e.kind == wArea && e.area != nil:
			e.cached = e.area.GetText()
		case e.kind == wCheck && e.check != nil:
			e.checked = e.check.State() == widget.WidgetChecked
		case e.kind == wCombo && e.combo != nil:
			sel := e.combo.SelectedEntry()
			e.selIdx = -1
			for i, s := range e.items {
				if s == sel {
					e.selIdx = i
					break
				}
			}
		}
	}
}

// InputText lives in edit.go (custom editor state).

// AddList creates (or replaces) a list box.
func (b *WindowBackend) AddList(id, x, y, w, h int, items []string) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("listbox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	entries := make([]any, len(items))
	for i, s := range items {
		entries[i] = s
	}
	b.runOnLoop(func() {
		face := b.uiFace()
		e := &widgetEntry{id: id, kind: wList, selIdx: -1}
		e.items = append([]string(nil), items...)
		list := newListWidget(b, face, e, entries)
		b.placeChild(id, e, list, x, y, w, h)
		e.list = list
	})
	return nil
}

// listEntryTextPos is the shared entry text alignment for listbox and
// combox: left/top, like the hand-drawn outline in the Golash editor.
// Centering adds a second int() rounding on top of the scroll offset's
// (glyph origin = int((rectH - lineH) / 2)), which can leave row
// baselines a pixel apart while the list scrolls (reads as jitter).
func listEntryTextPos() (widget.TextPosition, widget.TextPosition) {
	start := widget.TextPositionStart
	return start, start
}

// newListWidget builds a list in the fixed Windows-standard theme.
func newListWidget(b *WindowBackend, face *text.Face, e *widgetEntry, entries []any) *widget.List {
	list := widget.NewList(
		widget.ListOpts.ContainerOpts(widget.ContainerOpts.BackgroundImage(
			frame(themeWhite, themeFrame))),
		widget.ListOpts.ScrollContainerImage(&widget.ScrollContainerImage{
			Idle: frame(themeWhite, themeFrame),
			Mask: frame(themeWhite, themeFrame),
		}),
		// Entries sit 1px inside the frame: without padding the
		// selected row paints over the 1px gray border.
		widget.ListOpts.ScrollContainerPadding(widget.NewInsetsSimple(1)),
		// Left/top aligned, matching the hand-drawn outline in the
		// Golash editor: centered text depends on two int() roundings
		// (the scroll offset, then the centering bias) that can leave
		// baselines a pixel apart, which reads as jitter while scrolling.
		widget.ListOpts.EntryTextPosition(widget.TextPositionStart, widget.TextPositionStart),
		widget.ListOpts.EntryTextPadding(widget.NewInsetsSimple(2)),
		// The script owns every key (arrows move its own caret):
		// a focused list must not also consume them, or one press
		// would drive both the list and the script at once.
		widget.ListOpts.DisableDefaultKeys(true), widget.ListOpts.HideHorizontalSlider(),
		widget.ListOpts.HideVerticalSlider(),
		// Select on press (not release): native listboxes highlight
		// the instant the button goes down; release-to-select
		// feels like a laggy click.
		widget.ListOpts.SelectPressed(),
		widget.ListOpts.Entries(entries),
		widget.ListOpts.EntryLabelFunc(func(en any) string {
			if s, ok := en.(string); ok {
				return s
			}
			return ""
		}),
		widget.ListOpts.EntryFontFace(face),
		widget.ListOpts.EntryColor(listEntryColors()),
		widget.ListOpts.EntrySelectedHandler(func(args *widget.ListEntrySelectedEventArgs) {
			b.mu.Lock()
			e.selIdx = -1
			for i, s := range e.items {
				if s == args.Entry {
					e.selIdx = i
					break
				}
			}
			b.mu.Unlock()
		}),
	)
	return list
}

// SelectedIndex returns the list/combo selection (-1 when none).
func (b *WindowBackend) SelectedIndex(id int) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || (e.kind != wList && e.kind != wCombo) {
		return 0, fmt.Errorf("selected：不明な listbox %d です", id)
	}
	return e.selIdx, nil
}

// AddCheck creates (or replaces) a checkbox. Non-positive w/h fall back
// to the preferred size (box + label), since the box art no longer
// scales with the layout rect.
func (b *WindowBackend) AddCheck(id int, label string, x, y, w, h int, checked bool) error {
	b.runOnLoop(func() {
		face := b.uiFace()
		init := widget.WidgetUnchecked
		if checked {
			init = widget.WidgetChecked
		}
		ci := b.checkImage()
		lc := &widget.LabelColor{Idle: themeInk, Disabled: themeGrey}
		cb := widget.NewCheckbox(
			widget.CheckboxOpts.Image(ci),
			widget.CheckboxOpts.Text(label, face, lc),
			widget.CheckboxOpts.Spacing(6),
			widget.CheckboxOpts.InitialState(init),
		)
		// Validate now (fail fast): PreferredSize needs computedParams,
		// which ebitenui otherwise fills lazily on first UI update.
		cb.Validate()
		if pw, ph := cb.PreferredSize(); w <= 0 || h <= 0 {
			if w <= 0 {
				w = pw
			}
			if h <= 0 {
				h = ph
			}
		}
		e := &widgetEntry{id: id, kind: wCheck, selIdx: -1, check: cb, checked: checked}
		b.placeChild(id, e, cb, x, y, w, h)
	})
	return nil
}

// Checked reports a checkbox state (script-thread safe, never blocks:
// it reads the Update-time mirror).
func (b *WindowBackend) Checked(id int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wCheck {
		return false, fmt.Errorf("checked：不明な checkbox %d です", id)
	}
	return e.checked, nil
}

// comboSliderParams images the dropdown scrollbar in the fixed theme.
func (b *WindowBackend) comboSliderParams() *widget.SliderParams {
	track := frame(themeWhite, themeFrame)
	return &widget.SliderParams{
		TrackImage:  &widget.SliderTrackImage{Idle: track, Hover: track, Disabled: track},
		HandleImage: b.buttonImage(),
	}
}

// AddCombo creates (or replaces) a dropdown list. sel is the initial
// index (-1 for none).
func (b *WindowBackend) AddCombo(id, x, y, w, h int, items []string, sel int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("combox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	if sel < -1 || sel >= len(items) {
		return fmt.Errorf("combox：選択 %d は範囲外です（%d 項目）", sel, len(items))
	}
	entries := make([]any, len(items))
	for i, s := range items {
		entries[i] = s
	}
	b.runOnLoop(func() {
		face := b.uiFace()
		e := &widgetEntry{id: id, kind: wCombo, selIdx: -1}
		e.items = append([]string(nil), items...)
		labelOf := func(en any) string {
			if s, ok := en.(string); ok {
				return s
			}
			return ""
		}
		cb := b.newComboWidget(face, e, entries, labelOf, h)
		if sel >= 0 {
			cb.SetSelectedEntry(entries[sel])
			e.selIdx = sel
		}
		e.combo = cb
		b.placeChild(id, e, cb, x, y, w, h)
	})
	return nil
}

// newComboWidget builds a dropdown in the fixed Windows-standard theme.
func (b *WindowBackend) newComboWidget(face *text.Face, e *widgetEntry, entries []any, labelOf func(any) string, h int) *widget.ListComboButton {
	pressed := true
	// Left/top entry text, same as newListWidget.
	textPosH, textPosV := listEntryTextPos()
	return widget.NewListComboButton(
		widget.ListComboButtonOpts.ButtonParams(&widget.ButtonParams{
			Image:       b.buttonImage(),
			TextColor:   buttonTextColor(),
			TextPadding: widget.NewInsetsSimple(4),
			TextFace:    face,
		}),
		widget.ListComboButtonOpts.ListParams(&widget.ListParams{
			EntryFace:                   face,
			EntryColor:                  listEntryColors(),
			EntryTextPadding:            widget.NewInsetsSimple(2),
			EntryTextHorizontalPosition: &textPosH,
			EntryTextVerticalPosition:   &textPosV,
			// Same 1px inset as listbox: selected rows must not
			// paint over the dropdown frame.
			ScrollContainerPadding: widget.NewInsetsSimple(1),
			SelectPressed:          &pressed,
			ScrollContainerImage: &widget.ScrollContainerImage{
				Idle: frame(themeWhite, themeFrame),
				Mask: frame(themeWhite, themeFrame),
			},
			// The dropdown keeps its vertical slider: without images
			// the slider handle button renders with a nil Image and
			// panics on open (Button.draw dereferences Image.Idle).
			Slider: b.comboSliderParams(),
		}),
		widget.ListComboButtonOpts.Text(face, nil, buttonTextColor()),
		widget.ListComboButtonOpts.Entries(entries),
		widget.ListComboButtonOpts.EntryLabelFunc(labelOf, labelOf),
		widget.ListComboButtonOpts.EntrySelectedHandler(func(args *widget.ListComboButtonEntrySelectedEventArgs) {
			b.mu.Lock()
			e.selIdx = -1
			for i, s := range e.items {
				if s == args.Entry {
					e.selIdx = i
					break
				}
			}
			b.mu.Unlock()
		}),
		widget.ListComboButtonOpts.MaxContentHeight(h*4),
	)
}

// AddArea creates (or replaces) a multiline text box.
func (b *WindowBackend) AddArea(id, x, y, w, h int, text string) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("mesbox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	b.runOnLoop(func() {
		face := b.uiFace()
		e := &widgetEntry{id: id, kind: wArea, selIdx: -1, cached: text}
		ta := newAreaWidget(face, text)
		e.area = ta
		b.placeChild(id, e, ta, x, y, w, h)
	})
	return nil
}

// newAreaWidget builds a multiline box in the fixed Windows-standard theme.
func newAreaWidget(face *text.Face, content string) *widget.TextArea {
	return widget.NewTextArea(
		widget.TextAreaOpts.ContainerOpts(widget.ContainerOpts.BackgroundImage(
			frame(themeWhite, themeFrame))),
		widget.TextAreaOpts.ScrollContainerImage(&widget.ScrollContainerImage{
			Idle: frame(themeWhite, themeFrame),
			Mask: frame(themeWhite, themeFrame),
		}),
		widget.TextAreaOpts.FontFace(face),
		widget.TextAreaOpts.FontColor(themeInk),
		widget.TextAreaOpts.TextPadding(*widget.NewInsetsSimple(4)),
		widget.TextAreaOpts.Text(content),
	)
}

// AreaText returns the multiline box content (script-thread safe,
// never blocks: it reads the Update-time mirror).
func (b *WindowBackend) AreaText(id int) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wArea || e.area == nil {
		return "", fmt.Errorf("getstr：不明な mesbox %d です", id)
	}
	return e.cached, nil
}

// SetAreaText replaces the multiline box content (game thread).
// ebitenui TextArea is display-only (no keyboard editing), so mesbox
// works as a program-driven log viewer: setstr() writes, getstr()
// reads. The mirror updates inline so a following getstr() is exact.
func (b *WindowBackend) SetAreaText(id int, text string) error {
	// Existence is checked up front (like AddButton/AddArea) so unknown
	// ids fail without blocking on the game thread.
	b.mu.Lock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wArea || e.area == nil {
		b.mu.Unlock()
		return fmt.Errorf("setstr：不明な mesbox %d です", id)
	}
	b.mu.Unlock()
	b.runOnLoop(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if cur, ok := b.widgets[id]; ok && cur.kind == wArea && cur.area != nil {
			cur.area.SetText(text)
			cur.cached = text
		}
	})
	return nil
}

// SetEnabled toggles widget interactivity; applied on the game thread.
// Custom-drawn entries (toggle/input) track it in e.disabled since
// they have no ebitenui child to grey out.
func (b *WindowBackend) SetEnabled(id int, on bool) error {
	b.mu.Lock()
	_, ok := b.widgets[id]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("objprm：不明なウィジェット %d です", id)
	}
	b.runOnLoop(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if e, ok := b.widgets[id]; ok {
			e.disabled = !on
			if e.child != nil {
				e.child.GetWidget().Disabled = !on
			}
		}
	})
	return nil
}

// RemoveWidget drops one widget.
func (b *WindowBackend) RemoveWidget(id int) error {
	b.mu.Lock()
	_, ok := b.widgets[id]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("不明なウィジェット %d です", id)
	}
	var opErr error
	b.runOnLoop(func() {
		opErr = b.removeWidgetLocked(id)
	})
	return opErr
}

// ClearWidgets drops all widgets.
func (b *WindowBackend) ClearWidgets() {
	b.runOnLoop(func() {
		// removeWidgetLocked が都度ロックするため、イテレーション中の
		// マップ直接走査は避け id をスナップショットしてから消す。
		// (script 側 HasWidget/Pressed との並行走査でちらつき・競合が出ない)
		b.mu.Lock()
		ids := make([]int, 0, len(b.widgets))
		for id := range b.widgets {
			ids = append(ids, id)
		}
		b.mu.Unlock()
		for _, id := range ids {
			_ = b.removeWidgetLocked(id)
		}
	})
}

// HasWidget reports registry presence (tests).
func (b *WindowBackend) HasWidget(id int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.widgets[id]
	return ok
}

// Dialog shows a modal choice and blocks until answered.
// mode: "ok" -> 1, "okcancel" -> 1/0, "yesno" -> 1/0.
func (b *WindowBackend) Dialog(msg, mode string) (int, error) {
	var labels []string
	var values []int
	switch mode {
	case "ok":
		labels, values = []string{"OK"}, []int{1}
	case "okcancel":
		labels, values = []string{"OK", "Cancel"}, []int{1, 0}
	case "yesno":
		labels, values = []string{"Yes", "No"}, []int{1, 0}
	default:
		return 0, fmt.Errorf("dialog：不明なモード %q です（ok/okcancel/yesno が必要です）", mode)
	}
	result := make(chan int, 1)
	var once sync.Once
	finish := func(v int) {
		once.Do(func() { result <- v })
	}
	b.runOnLoop(func() {
		b.ensureUI()
		face := b.uiFace()
		inner := widget.NewContainer(
			widget.ContainerOpts.BackgroundImage(frame(themeWhite, themeFrame)),
			widget.ContainerOpts.Layout(widget.NewRowLayout(
				widget.RowLayoutOpts.Direction(widget.DirectionVertical),
				widget.RowLayoutOpts.Spacing(12),
				widget.RowLayoutOpts.Padding(widget.NewInsetsSimple(16)))),
		)
		b.mu.Lock()
		ww, wh := b.w, b.h
		vface, lineH := b.face, b.lineH
		b.mu.Unlock()
		if lineH <= 0 {
			lineH = lineHeight
		}
		maxW := float64(ww) - 128
		if maxW < 200 {
			maxW = 200
		}
		lines := wrapDialogText(msg, vface, maxW)
		for _, ln := range lines {
			inner.AddChild(widget.NewLabel(
				widget.LabelOpts.Text(ln, face, &widget.LabelColor{Idle: themeInk}),
			))
		}
		btnRow := widget.NewContainer(
			widget.ContainerOpts.Layout(widget.NewRowLayout(
				widget.RowLayoutOpts.Direction(widget.DirectionHorizontal),
				widget.RowLayoutOpts.Spacing(12))),
		)
		win := widget.NewWindow(
			widget.WindowOpts.Contents(inner),
			widget.WindowOpts.Modal(),
			widget.WindowOpts.ClosedHandler(func(args *widget.WindowClosedEventArgs) {
				finish(0)
			}),
		)
		remove := b.ui.AddWindow(win)
		for i, lab := range labels {
			v := values[i]
			btnRow.AddChild(widget.NewButton(
				widget.ButtonOpts.Image(b.buttonImage()),
				widget.ButtonOpts.Text(lab, face, buttonTextColor()),
				widget.ButtonOpts.TextPadding(widget.NewInsetsSimple(6)),
				widget.ButtonOpts.ClickedHandler(func(args *widget.ButtonClickedEventArgs) {
					remove()
					finish(v)
				}),
			))
		}
		inner.AddChild(btnRow)
		needW := 0.0
		for _, ln := range lines {
			if w, _ := text.Measure(ln, vface, 0); w > needW {
				needW = w
			}
		}
		bw := int(needW) + 96
		if bw < 240 {
			bw = 240
		}
		if bw > ww-32 {
			bw = ww - 32
		}
		bh := int(float64(len(lines))*lineH) + 140
		if bh > wh-32 {
			bh = wh - 32
		}
		win.SetLocation(rectCentered(ww, wh, bw, bh))
	})
	return <-result, nil
}

// isCJKBreakable reports runes that may start a wrapped line anywhere
// (Japanese kana/kanji, Hangul, CJK symbols, fullwidth forms).
func isCJKBreakable(r rune) bool {
	switch {
	case r >= 0x3040 && r <= 0x30FF: // hiragana + katakana
		return true
	case r >= 0x3400 && r <= 0x9FFF: // CJK ext-A .. ideographs
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // hangul syllables
		return true
	case r >= 0xFF00 && r <= 0xFFEF: // fullwidth forms
		return true
	}
	return false
}

// wrapDialogText breaks s into lines fitting maxW pixels (measured
// with face). Explicit newlines are honored; Latin prefers word
// boundaries while CJK breaks anywhere.
func wrapDialogText(s string, face *text.GoTextFace, maxW float64) []string {
	if face == nil {
		return strings.Split(s, "\n")
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		rs := []rune(para)
		for len(rs) > 0 {
			n := len(rs)
			for n > 0 {
				w, _ := text.Measure(string(rs[:n]), face, 0)
				if w <= maxW {
					break
				}
				n--
			}
			if n == 0 {
				n = 1 // single rune wider than the window
			}
			if n < len(rs) {
				// Back to the last space when cutting inside a
				// Latin word (both sides non-space, non-CJK).
				tail := rs[:n]
				sp := -1
				for i := len(tail) - 1; i >= 0; i-- {
					if tail[i] == ' ' {
						sp = i
						break
					}
				}
				if sp > 0 && !isCJKBreakable(rs[sp-1]) && !isCJKBreakable(rs[n]) {
					n = sp
				}
			}
			lines = append(lines, strings.TrimRight(string(rs[:n]), " "))
			rs = rs[n:]
			for len(rs) > 0 && rs[0] == ' ' {
				rs = rs[1:]
			}
		}
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

func rectCentered(ww, wh, bw, bh int) image.Rectangle {
	x := (ww - bw) / 2
	if x < 0 {
		x = 0
	}
	y := (wh - bh) / 2
	if y < 0 {
		y = 0
	}
	return image.Rect(x, y, x+bw, y+bh)
}
