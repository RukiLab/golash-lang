package gui

import (
	"image"
	"unicode/utf8"
)

// IME via ebiten/exp/textinput (Windows/macOS/Web; other platforms get
// committed text only through AppendInputChars as before).
//
// One backend-wide Field: ime(1) focuses it, ime(0) blurs it. While
// focused, Update pumps it every tick; committed text flows into the
// input() line editor, and the in-conversion string is mirrored for
// the fixed top-left bar. Bar/draw code reads the mu-guarded mirrors
// because the Field itself is only safe on the game thread.

// IMEState reports 1 when the IME field is focused, else 0.
func (b *WindowBackend) IMEState() int {
	if b.imeField.IsFocused() {
		return 1
	}
	return 0
}

// IMESet focuses (on != 0) or blurs the IME field, returning the state.
func (b *WindowBackend) IMESet(on bool) int {
	if on {
		b.mu.Lock()
		b.imeOff = false
		b.mu.Unlock()
		b.imeField.Focus()
	} else {
		b.imeField.Blur()
		// Blur drops the in-conversion text: clear the mirror at once
		// so the bar blanks immediately instead of showing a stale
		// composition until the next pumpIME tick (which early-returns
		// while unfocused and would never clear it).
		// NOTE: Focus/Blur stay outside mu on purpose. Blur ends the
		// OS session with a synchronous dispatch to the main thread,
		// which must never block on mu behind us (Draw snapshots
		// under mu): holding mu here deadlocks the whole window.
		b.mu.Lock()
		b.imeOff = true
		b.imeComposing = ""
		b.imeClause = ""
		b.imeClauseStart, b.imeClauseEnd = 0, 0
		b.mu.Unlock()
	}
	return b.IMEState()
}

// IMEComposition returns the in-conversion (uncommitted) text.
func (b *WindowBackend) IMEComposition() string {
	if !b.imeField.IsFocused() {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.imeComposing
}

// IMEClause returns the target conversion clause (文節) inside the
// in-conversion text with its rune offsets into the composition
// string: (text, runeStart, runeEnd), all zeros when unfocused, idle,
// or the platform reports no clause range. One call is one tick's
// snapshot, so the offsets always match the text (two separate calls
// could straddle a tick). Script-thread safe.
func (b *WindowBackend) IMEClause() (string, int, int) {
	if !b.imeField.IsFocused() {
		return "", 0, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.imeClause == "" {
		return "", 0, 0
	}
	return b.imeClause, b.imeClauseStart, b.imeClauseEnd
}

// imeClauseOf cuts the target clause out of the composition text:
// cs/ce are ebiten's CompositionSelection byte offsets relative to
// the composition start (ok reports a live range). It returns the
// clause with its rune offsets into comp, so repeated words stay
// distinguishable. Out-of-range or non-UTF-8 cuts (and a dead range)
// yield ("", 0, 0). Pure logic, unit-testable.
func imeClauseOf(comp string, cs, ce int, ok bool) (string, int, int) {
	if !ok || comp == "" {
		return "", 0, 0
	}
	if cs < 0 || ce < cs || ce > len(comp) {
		return "", 0, 0
	}
	out := comp[cs:ce]
	if out == "" || !utf8.ValidString(out) {
		return "", 0, 0
	}
	rs := len([]rune(comp[:cs]))
	return out, rs, rs + len([]rune(out))
}

// imeCandidateBounds fixes the candidate window below the composition
// bar: the OS opens the list below the given rect, so the rect spans
// the bar height (Max.Y = line height) and the list top lands right
// under the bar. lh is the line height in pixels (same value the bar
// uses for its height). Pure logic, unit-testable.
func imeCandidateBounds(lh float64) image.Rectangle {
	h := int(lh + 0.5)
	return image.Rect(0, 0, 1, h)
}

// pumpIME advances the IME field; game thread only (called from Update).
func (b *WindowBackend) pumpIME() {
	if !b.imeField.IsFocused() {
		// Unfocused means no composition (UncommittedTextLengthInBytes
		// is 0 by definition): drop any stale mirror so the bar blanks
		// once the unconfirmed string becomes empty, including
		// blur/cancel paths that never reach the update below.
		b.mu.Lock()
		if b.imeComposing != "" {
			b.imeComposing = ""
		}
		if b.imeClause != "" {
			b.imeClause = ""
			b.imeClauseStart, b.imeClauseEnd = 0, 0
		}
		b.mu.Unlock()
		return
	}
	// The candidate window sits below the composition bar (same
	// height the bar uses, so the list top lands right under it).
	b.mu.Lock()
	lh := b.lineH
	b.mu.Unlock()
	bounds := imeCandidateBounds(lh)
	if _, err := b.imeField.HandleInputWithBounds(bounds); err != nil {
		// On error the composition is unusable: never leave a stale
		// mirror behind for the bar.
		b.mu.Lock()
		if b.imeComposing != "" {
			b.imeComposing = ""
		}
		if b.imeClause != "" {
			b.imeClause = ""
			b.imeClauseStart, b.imeClauseEnd = 0, 0
		}
		b.mu.Unlock()
		return
	}
	full := b.imeField.Text()
	start, _ := b.imeField.Selection()
	ulen := b.imeField.UncommittedTextLengthInBytes()
	comp := ""
	clause := ""
	cstart, cend := 0, 0
	if ulen > 0 && start >= 0 {
		rendering := b.imeField.TextForRendering()
		if start+ulen <= len(rendering) && utf8.ValidString(rendering) {
			comp = rendering[start : start+ulen]
			cs, ce, cok := b.imeField.CompositionSelection()
			clause, cstart, cend = imeClauseOf(comp, cs, ce, cok)
		}
	}
	// ebiten's Windows backend never reports an emptied composition
	// (zero-length COMPSTR is dropped silently, cancel sends nothing),
	// so comp can stay stale after the unconfirmed string becomes
	// empty. The OS ground truth wins: empty means clear for the bar.
	comp = reconcileComposition(comp)
	if comp == "" {
		clause = ""
		cstart, cend = 0, 0
	}
	b.mu.Lock()
	// Mirror the field into the pending stream: typed text appends,
	// in-field deletions (backspace etc.) truncate the stream tail.
	// The tail covers the undrained pending first, then the line
	// buffer or the focused editor, because a consumer drains every
	// tick and already holds older text.
	if drop, add := imeDiff(b.imePrev, full); drop > 0 || add != "" {
		if drop > 0 {
			b.dropLastLocked(drop)
		}
		b.imePending += add
	}
	b.imePrev = full
	b.imeComposing = comp
	b.imeClause = clause
	b.imeClauseStart = cstart
	b.imeClauseEnd = cend
	// Drained and idle: reset the field so the next tick starts from
	// a clean, append-only slate (invisible-field caret drifts cannot
	// accumulate across ticks).
	if ulen == 0 && b.imePending == "" && b.imePrev != "" {
		b.imeField.SetTextAndSelection("", 0, 0)
		b.imePrev = ""
	}
	b.mu.Unlock()
}

// reconcileComposition prefers the OS ground truth over ebiten's
// mirror: when ebiten still reports a composition but the OS-side
// unconfirmed string is empty (0 chars), the composition became empty
// and the bar must blank. Unknown OS state keeps ebiten's value.
// Pure logic, unit-testable (osLen is injected for tests).
func reconcileComposition(ebitenComp string) string {
	if ebitenComp == "" {
		return ""
	}
	n, ok := osIMECompositionLen()
	return reconcileCompositionLen(ebitenComp, n, ok)
}

// reconcileCompositionLen is reconcileComposition with an injectable
// OS length (n==0 with ok means the unconfirmed string is empty).
// Pure logic, unit-testable.
func reconcileCompositionLen(ebitenComp string, n int, ok bool) string {
	if ebitenComp != "" && ok && n == 0 {
		return ""
	}
	return ebitenComp
}

// imeDiff splits new committed field text against the previous tick:
// drop is the rune count to remove from the consumer tail (in-field
// deletions), add is the newly typed text. Pure logic, unit-testable.
func imeDiff(prev, full string) (drop int, add string) {
	or, nr := []rune(prev), []rune(full)
	i := 0
	for i < len(or) && i < len(nr) && or[i] == nr[i] {
		i++
	}
	return len(or) - i, string(nr[i:])
}

// dropLastLocked removes n runes from the end of the IME consumer
// stream: undrained pending first, then the focused editor. Caller
// holds mu (game thread, from pumpIME).
func (b *WindowBackend) dropLastLocked(n int) {
	for n > 0 && b.imePending != "" {
		pr := []rune(b.imePending)
		b.imePending = string(pr[:len(pr)-1])
		n--
	}
	if n == 0 {
		return
	}
	if ed := b.focusedLocked(); ed != nil {
		for n > 0 && len(ed.text) > 0 {
			ed.text = ed.text[:len(ed.text)-1]
			if ed.caret > len(ed.text) {
				ed.caret = len(ed.text)
			}
			n--
		}
	}
}
