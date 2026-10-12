package gui

import (
	"fmt"
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Custom single-line editor (inputbox): self-drawn box with caret,
// horizontal scroll and OS IME conversion. The in-conversion text
// renders in a fixed top-left bar (see drawIMEComp); the OS candidate
// window is likewise fixed at the screen origin. It replaces the
// ebitenui TextInput so the caret position is known.
//
// Focus model: left-click focuses one box (blurring the rest);
// clicking elsewhere blurs the box. The IME field follows box focus
// so conversion works without an explicit ime(1) (game-thread only):
// a fresh focus focuses the field, box defocus (click elsewhere,
// Enter/Escape) blurs it. Focus is otherwise sticky: immediate-mode
// scripts poll input() with no box focused, and an idle auto-blur
// would kill conversion between keystrokes. ime(0) force-disables
// conversion until ime(1) or the next box focus edge.

// inputState is one editor. Guarded by backend mu; game-thread
// mutated in pumpInputs, script-thread read in InputText.
type inputState struct {
	rect    image.Rectangle
	text    []rune
	caret   int // rune index 0..len(text)
	focused bool
	scroll  int // x pixel offset of the visible text
	// held-key repeat (backspace/delete/arrows on the game thread).
	repKey  ebiten.Key
	repNext int64
}

// insert adds runes at the caret.
func (e *inputState) insert(rs []rune) {
	if len(rs) == 0 {
		return
	}
	e.text = append(e.text[:e.caret], append(rs, e.text[e.caret:]...)...)
	e.caret += len(rs)
}

// backspace deletes before the caret; del deletes at it.
func (e *inputState) backspace() {
	if e.caret > 0 {
		e.text = append(e.text[:e.caret-1], e.text[e.caret:]...)
		e.caret--
	}
}

func (e *inputState) del() {
	if e.caret < len(e.text) {
		e.text = append(e.text[:e.caret], e.text[e.caret+1:]...)
	}
}

func (e *inputState) moveLeft() {
	if e.caret > 0 {
		e.caret--
	}
}

func (e *inputState) moveRight() {
	if e.caret < len(e.text) {
		e.caret++
	}
}

// repFire gates a held editing key: just-pressed fires at once and
// arms the repeat; a different key takes over; release disarms.
// Pure logic, unit-testable.
func (e *inputState) repFire(key ebiten.Key, just, held bool, now int64) bool {
	if !held {
		if e.repKey == key {
			e.repKey = 0
		}
		return false
	}
	if just || e.repKey != key {
		e.repKey, e.repNext = key, now+repDelay
		return true
	}
	if now >= e.repNext {
		e.repNext = now + repInterval
		return true
	}
	return false
}

// editAction is one editing-key effect, applied by pumpInputs.
func (e *inputState) editAction(key ebiten.Key) {
	switch key {
	case ebiten.KeyBackspace:
		e.backspace()
	case ebiten.KeyDelete:
		e.del()
	case ebiten.KeyArrowLeft:
		e.moveLeft()
	case ebiten.KeyArrowRight:
		e.moveRight()
	case ebiten.KeyHome:
		e.caret = 0
	case ebiten.KeyEnd:
		e.caret = len(e.text)
	}
}

// focusedLocked returns the focused editor; caller holds mu.
func (b *WindowBackend) focusedLocked() *inputState {
	for _, e := range b.widgets {
		if e.kind == wInput && e.edit != nil && e.edit.focused {
			return e.edit
		}
	}
	return nil
}

// AddInput creates (or replaces) a text input with initial text
// (caret at the end). Custom-drawn: no ebitenui child.
func (b *WindowBackend) AddInput(id, x, y, w, h int, text string) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("inputbox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	rs := []rune(text)
	st := &inputState{rect: image.Rect(x, y, x+w, y+h), text: rs, caret: len(rs)}
	b.runOnLoop(func() {
		b.placeCustom(id, &widgetEntry{id: id, kind: wInput, selIdx: -1, edit: st})
	})
	return nil
}

// InputText returns the current input content (script-thread safe,
// never blocks: the state itself is mu-guarded).
func (b *WindowBackend) InputText(id int) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wInput || e.edit == nil {
		return "", fmt.Errorf("gettext：不明な inputbox %d です", id)
	}
	return string(e.edit.text), nil
}

// blurEditsLocked defocuses every editor; caller holds mu.
func (b *WindowBackend) blurEditsLocked() {
	for _, e := range b.widgets {
		if e.kind == wInput && e.edit != nil && e.edit.focused {
			e.edit.focused = false
			e.edit.repKey = 0
		}
	}
}

// editFocusPlan is the IME-field decision for one pumpInputs press:
// whether to focus/blur the field and whether the sticky ime(0)
// disable must be cleared. Pure logic, unit-testable.
type editFocusPlan struct {
	focus  bool // focus the IME field (box focus, or re-enable after ime(0))
	blur   bool // blur the IME field (clicked outside the focused box)
	enable bool // clear the sticky ime(0) disable
}

// planEditFocus decides the IME field move for one press/observation.
// press is a real press edge (not the latched clicked() level), hit
// says the press landed inside an enabled inputbox, hadFocus says a
// box was focused before the press, boxFocused says one is focused
// now, fieldFocused/imeOff are the live IME field state. Pure logic,
// unit-testable.
//
// Rules: a press inside a box focuses the field and re-enables
// conversion after ime(0); a press outside blurs the field only when
// a box was focused (the entry is finished) and ime(0) was not set;
// with a box focused but the field not (any other blur) the field is
// re-focused so conversion keeps working.
func planEditFocus(press, hit, hadFocus, boxFocused, fieldFocused, imeOff bool) editFocusPlan {
	if hit {
		return editFocusPlan{focus: true, enable: true}
	}
	if boxFocused && !fieldFocused && !imeOff {
		return editFocusPlan{focus: true}
	}
	if press && !hit && hadFocus && fieldFocused && !imeOff {
		return editFocusPlan{blur: true}
	}
	return editFocusPlan{}
}

// editKeysOwnedByField reports whether editing keys (Backspace/Delete/
// arrows/Home/End) belong to the IME field this tick. While converting
// or while the field holds undrained text, the field owns them and the
// box must stay out, or keystrokes would apply twice (field and box).
// Otherwise the field is an empty slate that would swallow them
// (notably Backspace, which no-ops in an empty field), so the box
// handles them. Pure logic, unit-testable.
func editKeysOwnedByField(imeOn bool, comp, fieldText, pending string) bool {
	return imeOn && (comp != "" || fieldText != "" || pending != "")
}

// pumpInputs handles focus and editing on the game thread (called
// from Update after pumpIME). Clicks are observed, not consumed.
func (b *WindowBackend) pumpInputs() {
	x, y := ebiten.CursorPosition()
	// Press edge, not the latched clicked() flag: clickFlag stays set
	// until the script consumes it, so a latched level would blur a
	// box (and the IME field) on later frames while the mouse sits
	// outside, and would re-enable conversion right after ime(0).
	// The edge lasts for the Update that observed the press, which
	// always runs, so no click is missed even if the script consumes
	// clicked() in between.
	press := inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft)

	b.mu.Lock()
	hadFocus := b.focusedLocked() != nil
	hit := false
	if b.widgets != nil && press {
		pt := image.Pt(x, y)
		for _, e := range b.widgets {
			if e.kind != wInput || e.edit == nil || e.disabled {
				continue
			}
			if pt.In(e.edit.rect) {
				hit = true
				if !e.edit.focused {
					b.blurEditsLocked()
					e.edit.focused = true
					e.edit.repKey = 0
				}
				break
			}
		}
		if !hit {
			b.blurEditsLocked()
		}
	}
	ed := b.focusedLocked()
	plan := planEditFocus(press, hit, hadFocus, ed != nil, b.imeField.IsFocused(), b.imeOff)
	if plan.enable {
		b.imeOff = false
	}
	b.mu.Unlock()
	if plan.focus {
		b.imeField.Focus()
	} else if plan.blur {
		// Clicked outside any box: the entry is finished. A
		// conversion in flight is dropped with the blur (matches
		// the Enter/Escape path below); sticky ime(0) keeps the
		// field blurred without touching mirrors.
		b.mu.Lock()
		b.imePending = ""
		b.imePrev = ""
		b.imeComposing = ""
		b.imeClause = ""
		b.imeClauseStart, b.imeClauseEnd = 0, 0
		b.mu.Unlock()
		b.imeField.SetTextAndSelection("", 0, 0)
		b.imeField.Blur()
	}

	// While a box is focused it owns the key stream: input() reports
	// "" and Update routes committed characters into the caret.
	// Without a focused box the candidate window falls back to the
	// origin (there is no caret to anchor it to).

	if ed == nil {
		return
	}
	now := b.Tick()
	// Committed characters land at the caret. While the IME field is
	// focused they arrive via pumpIME; otherwise plain.
	b.mu.Lock()
	imeOn := b.imeField.IsFocused()
	comp := b.imeComposing
	pending := b.imePending
	b.imePending = ""
	b.mu.Unlock()
	// ed は game thread の pumpInputs/drawInputs と script の InputText で
	// 共有されるため、変異は mu 保持中に行う (保持なしの insert は
	// Draw スナップショットと競合し、入力文字のちらつき・欠落になる)。
	if imeOn {
		if len(pending) > 0 {
			b.mu.Lock()
			ed.insert([]rune(pending))
			b.mu.Unlock()
		}
	} else {
		if chars := ebiten.AppendInputChars(nil); len(chars) > 0 {
			b.mu.Lock()
			ed.insert(chars)
			b.mu.Unlock()
		}
	}
	// While converting, Enter/Escape belong to the IME (they commit
	// or cancel the composition); otherwise they blur the box.
	if !imeOn || comp == "" {
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyKPEnter) ||
			inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			b.mu.Lock()
			b.blurEditsLocked()
			// Drop field residue like the line editor does on
			// confirm: the entry is finished.
			b.imePending = ""
			b.imePrev = ""
			b.imeComposing = ""
			b.imeClause = ""
			b.imeClauseStart, b.imeClauseEnd = 0, 0
			b.mu.Unlock()
			b.imeField.SetTextAndSelection("", 0, 0)
			b.imeField.Blur()
			return
		}
	}
	// While the field owns input it also owns the editing keys below;
	// with an empty field the box handles them (see
	// editKeysOwnedByField): the invisible field would swallow them.
	fieldText := ""
	if imeOn {
		fieldText = b.imeField.Text()
	}
	if editKeysOwnedByField(imeOn, comp, fieldText, pending) {
		return
	}
	// Editing keys (game thread: inpututil is safe here).
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, k := range []ebiten.Key{
		ebiten.KeyBackspace, ebiten.KeyDelete,
		ebiten.KeyArrowLeft, ebiten.KeyArrowRight,
	} {
		if ed.repFire(k, inpututil.IsKeyJustPressed(k), ebiten.IsKeyPressed(k), now) {
			ed.editAction(k)
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyHome) {
		ed.caret = 0
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnd) {
		ed.caret = len(ed.text)
	}
}

// drawInputs paints custom editors above the UI (game thread; called
// from Draw). Text is clipped to the box via a sub-image viewport.
func (b *WindowBackend) drawInputs(screen *ebiten.Image) {
	type snap struct {
		id       int
		st       inputState
		disabled bool
		face     *text.GoTextFace
		ascent   float64
		descent  float64
		lineH    float64
		tick     int64
	}
	b.mu.Lock()
	var ss []snap
	for id, e := range b.widgets {
		if e.kind != wInput || e.edit == nil {
			continue
		}
		s := snap{id: id, st: *e.edit, disabled: e.disabled, face: b.face, tick: b.tick}
		if b.face != nil {
			m := b.face.Metrics()
			s.ascent, s.descent = m.HAscent, m.HDescent
			s.lineH = b.lineH
		}
		ss = append(ss, s)
	}
	b.mu.Unlock()
	for i := range ss {
		drawInput(screen, &ss[i].st, ss[i].disabled, ss[i].face, ss[i].ascent, ss[i].descent, ss[i].tick)
	}
	// scroll は Draw で確定するが、コピーへの調整を捨てると毎フレーム
	// 0 から再計算して行端で前後振動 (ちらつき) するため書き戻す。
	b.mu.Lock()
	for _, s := range ss {
		if e, ok := b.widgets[s.id]; ok && e.edit != nil {
			e.edit.scroll = s.st.scroll
		}
	}
	b.mu.Unlock()
}

// compSpan is one underline run over the composition: [start, end)
// in runes, thick for the conversion target clause.
type compSpan struct {
	start, end int
	thick      bool
}

// compSpans splits n composition runes into underline runs from the
// clause range, skipping empties and clamping into range. Pure logic,
// unit-testable.
func compSpans(n, rs, re int) []compSpan {
	if rs < 0 {
		rs = 0
	}
	if re > n {
		re = n
	}
	if rs > re {
		rs = re
	}
	var out []compSpan
	if rs > 0 {
		out = append(out, compSpan{0, rs, false})
	}
	if re > rs {
		out = append(out, compSpan{rs, re, true})
	}
	if re < n {
		out = append(out, compSpan{re, n, false})
	}
	return out
}

// drawIMEComp paints the in-conversion text in a top-left bar flush
// with the window corner (white face, gray ring, black text) above
// everything: conversion target clause with a thick underline, the
// rest thin. Unlike the box caret, the composition display never
// moves: one predictable spot for inputbox and immediate input()
// alike. Game thread (Draw).
func (b *WindowBackend) drawIMEComp(screen *ebiten.Image) {
	b.mu.Lock()
	comp := b.imeComposing
	clauseOn := b.imeClause != ""
	rs, re := b.imeClauseStart, b.imeClauseEnd
	face := b.face
	var ascent, descent, lineH float64
	if face != nil {
		m := face.Metrics()
		ascent, descent = m.HAscent, m.HDescent
		lineH = b.lineH
	}
	b.mu.Unlock()
	if comp == "" || face == nil {
		return
	}
	if lineH <= 0 {
		lineH = lineHeight
	}
	rsRunes := []rune(comp)
	if !clauseOn {
		rs, re = 0, 0
	}
	const pad = 2.0
	// imeCompExtraH pads the bar past the text line: a 16px line box
	// ends at baseline + half descent, where the 3px clause underline
	// would otherwise hang over the frame.
	const imeCompExtraH = 2.0
	w := measureInput(face, rsRunes)
	bw, bh := w+pad*2, lineH+imeCompExtraH
	const x, y = 0.0, 0.0
	vector.DrawFilledRect(screen, float32(x), float32(y), float32(bw), float32(bh), themeWhite, false)
	vector.StrokeRect(screen, float32(x)+0.5, float32(y)+0.5, float32(bw)-1, float32(bh)-1, 1, themeFrame, false)
	op := &text.DrawOptions{}
	tx := x + pad
	// Region top lands on the bar top (text/v2 semantics, same as
	// drawTextFrag); the baseline sits ascent below it.
	op.GeoM.Translate(tx, y)
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(themeInk)
	text.Draw(screen, comp, face, op)
	uy := float32(y + ascent + descent*0.5)
	cx := float32(tx)
	for _, sp := range compSpans(len(rsRunes), rs, re) {
		sw, _ := text.Measure(string(rsRunes[sp.start:sp.end]), face, 0)
		th := float32(1)
		if sp.thick {
			th = 3
		}
		vector.DrawFilledRect(screen, cx, uy, float32(sw), th, themeInk, false)
		cx += float32(sw)
	}
}

// inputFaceColors selects the editor face: focus tints the background
// pale blue with a blue frame, disabled greys out (winning over focus).
// Pure logic, unit-tested.
func inputFaceColors(focused, disabled bool) (bg, frameC color.NRGBA) {
	bg, frameC = themeWhite, themeFrame
	if focused {
		bg, frameC = themeFocusBg, themeFocusFrame
	}
	if disabled {
		bg, frameC = themeFaceDis, themeFrame
	}
	return bg, frameC
}

// drawInput paints one editor: frame, clipped text and caret. The
// in-conversion text is not drawn here; it renders in the fixed
// top-left bar (drawIMEComp). Fixed Windows-standard colors (white,
// gray frame, black text; pale-blue/blue focus face). Pure drawing
// (no backend), unit-testable where noted.
func drawInput(screen *ebiten.Image, st *inputState, disabled bool, face *text.GoTextFace, ascent, descent float64, tick int64) {
	r := st.rect
	bg, frameC := inputFaceColors(st.focused, disabled)
	vector.DrawFilledRect(screen, float32(r.Min.X), float32(r.Min.Y), float32(r.Dx()), float32(r.Dy()), bg, false)
	vector.StrokeRect(screen, float32(r.Min.X)+0.5, float32(r.Min.Y)+0.5, float32(r.Dx())-1, float32(r.Dy())-1, 1, frameC, false)
	if face == nil || r.Dy() < 8 || r.Dx() <= 12 {
		return
	}
	inner := image.Rect(r.Min.X+4, r.Min.Y+2, r.Max.X-4, r.Max.Y-2)
	if inner.Dx() <= 0 || inner.Dy() <= 0 {
		return
	}
	caretX := measureInput(face, st.text[:st.caret])
	visX := caretX - float64(st.scroll)
	// Keep the caret visible.
	if visX > float64(inner.Dx()) {
		st.scroll = int(caretX) - inner.Dx()
		visX = caretX - float64(st.scroll)
	}
	if visX < 0 {
		st.scroll = int(caretX)
		visX = caretX - float64(st.scroll)
	}
	if st.scroll < 0 {
		st.scroll = 0
	}
	dst, ok := screen.SubImage(inner).(*ebiten.Image)
	if !ok {
		return
	}
	fg := themeInk
	if disabled {
		fg = themeGrey
	}
	op := &text.DrawOptions{}
	// Vertically centered (region top lands on the origin, so offset
	// by half the leftover inner height).
	op.GeoM.Translate(float64(inner.Min.X)-float64(st.scroll),
		float64(inner.Min.Y)+(float64(inner.Dy())-(ascent+descent))/2)
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(fg)
	text.Draw(dst, string(st.text), face, op)
	// Caret blink (~2Hz).
	if st.focused && !disabled && tick/30%2 == 0 {
		cx := float32(inner.Min.X) + float32(visX)
		if cx >= float32(inner.Min.X) && cx <= float32(inner.Max.X) {
			vector.DrawFilledRect(screen, cx, float32(inner.Min.Y), 2, float32(inner.Dy()),
				themeInk, false)
		}
	}
}

// measureInput widths a rune prefix at face (caret placement).
// Pure, unit-testable.
func measureInput(face *text.GoTextFace, prefix []rune) float64 {
	if face == nil || len(prefix) == 0 {
		return 0
	}
	w, _ := text.Measure(string(prefix), face, 0)
	return w
}
