package gui

import (
	"fmt"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

// Repeat timing in ticks (@60fps) for synthesized keys: first fire is
// immediate on a new press, then repDelay ticks later, then every
// repInterval ticks. Printable and IME-committed text already carries
// the OS key repeat, so it passes through untouched (see charStep).
const (
	repDelay    = 24
	repInterval = 2
)

// charRep dedups one immediate-input snapshot per tick. Printable and
// IME-committed text already carries the OS key repeat, so it passes
// through as-is; this only suppresses duplicate reports when input()
// is polled more than once within the same tick.
type charRep struct {
	last string
	tick int64 // tick of the last delivery
}

// ctrlKeys maps physical control keys to the character input()
// reports for them (the ASCII code as a single-char string).
// AppendInputChars never yields these; they are synthesized with
// the same repeat timing below.
var ctrlKeys = []struct {
	key ebiten.Key
	ch  string
}{
	{ebiten.KeyBackspace, "\x08"},
	{ebiten.KeyTab, "\x09"},
	{ebiten.KeyEnter, "\r"},
	{ebiten.KeyKPEnter, "\r"},
	{ebiten.KeyEscape, "\x1b"},
	{ebiten.KeyDelete, "\x7f"},
}

// ctrlState tracks one control key's repeat session in wall ticks.
type ctrlState struct {
	down  bool
	start int64 // tick the current hold began
	last  int64 // tick of the last fire
}

// ctrlStep fires on a new press, then while held with the
// repDelay/repInterval timing. Pure logic, unit-testable.
func ctrlStep(st *ctrlState, down bool, now int64) bool {
	if !down {
		st.down = false
		return false
	}
	if !st.down {
		st.down, st.start, st.last = true, now, now
		return true
	}
	if now-st.start >= repDelay && now-st.last >= repInterval {
		st.last = now
		return true
	}
	return false
}

// charStep maps a per-frame rune snapshot to a firing string.
// Text is delivered once per tick: new text fires at once, and OS
// auto-repeat arrivals on later ticks fire again untouched. A repeat
// poll within the same tick reports "" so scripts never see the same
// keystroke twice. Pure logic, unit-testable.
func charStep(st *charRep, s string, now int64) string {
	if s == "" {
		return ""
	}
	if s == st.last && now == st.tick {
		return ""
	}
	st.last, st.tick = s, now
	return s
}

// Polling input (G2). Key/mouse queries hit ebiten directly (thread-safe);
// click edges are latched in Update because "just pressed" needs per-tick
// observation.

// keyCodes maps script key codes to ebiten keys, following Windows
// virtual-key numbering: 8 backspace, 9 tab, 13 enter, 16 shift, 17
// ctrl, 18 alt, 27 esc, 32 space, 33-36 pageup/pagedown/end/home,
// 37-40 arrows, 45 insert, 46 delete, 48-57 digits, 65-90 A-Z,
// 112-123 F1-F12, 186-192/219-222 punctuation.
func keyFor(code int) (ebiten.Key, bool) {
	switch code {
	case 8:
		return ebiten.KeyBackspace, true
	case 9:
		return ebiten.KeyTab, true
	case 13:
		return ebiten.KeyEnter, true
	case 16:
		return ebiten.KeyShiftLeft, true
	case 17:
		return ebiten.KeyControlLeft, true
	case 18:
		return ebiten.KeyAltLeft, true
	case 27:
		return ebiten.KeyEscape, true
	case 32:
		return ebiten.KeySpace, true
	case 33:
		return ebiten.KeyPageUp, true
	case 34:
		return ebiten.KeyPageDown, true
	case 35:
		return ebiten.KeyEnd, true
	case 36:
		return ebiten.KeyHome, true
	case 37:
		return ebiten.KeyArrowLeft, true
	case 38:
		return ebiten.KeyArrowUp, true
	case 39:
		return ebiten.KeyArrowRight, true
	case 40:
		return ebiten.KeyArrowDown, true
	case 45:
		return ebiten.KeyInsert, true
	case 46:
		return ebiten.KeyDelete, true
	case 186:
		return ebiten.KeySemicolon, true
	case 187:
		return ebiten.KeyEqual, true
	case 188:
		return ebiten.KeyComma, true
	case 189:
		return ebiten.KeyMinus, true
	case 190:
		return ebiten.KeyPeriod, true
	case 191:
		return ebiten.KeySlash, true
	case 192:
		return ebiten.KeyBackquote, true
	case 219:
		return ebiten.KeyBracketLeft, true
	case 220:
		return ebiten.KeyBackslash, true
	case 221:
		return ebiten.KeyBracketRight, true
	case 222:
		return ebiten.KeyQuote, true
	}
	if code >= 48 && code <= 57 {
		return ebiten.Key0 + ebiten.Key(code-48), true
	}
	if code >= 65 && code <= 90 {
		return ebiten.KeyA + ebiten.Key(code-65), true
	}
	if code >= 112 && code <= 123 {
		return ebiten.KeyF1 + ebiten.Key(code-112), true
	}
	return 0, false
}

// shiftHeld reports either shift key; ctrlHeld / altHeld likewise.
func shiftHeld() bool {
	return ebiten.IsKeyPressed(ebiten.KeyShiftLeft) || ebiten.IsKeyPressed(ebiten.KeyShiftRight)
}

// clickButton maps script button numbers to ebiten buttons.
func clickButton(btn int) (ebiten.MouseButton, bool) {
	switch btn {
	case 0:
		return ebiten.MouseButtonLeft, true
	case 1:
		return ebiten.MouseButtonMiddle, true
	case 2:
		return ebiten.MouseButtonRight, true
	}
	return 0, false
}

// pumpInput latches click rising edges and accumulates wheel motion.
// Called from Update. ebiten.Wheel reports per-tick deltas (0 when
// idle), so plain accumulation never loses motion.
func (b *WindowBackend) pumpInput() {
	pressed := [3]bool{
		ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft),
		ebiten.IsMouseButtonPressed(ebiten.MouseButtonMiddle),
		ebiten.IsMouseButtonPressed(ebiten.MouseButtonRight),
	}
	_, wy := ebiten.Wheel()
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range pressed {
		if pressed[i] && !b.mouseDown[i] {
			b.clickFlag[i] = true
		}
		b.mouseDown[i] = pressed[i]
	}
	b.wheelAccum += wy
}

// MouseWheel returns pending vertical wheel detents since the last
// call (consumed; fractions carry over).
func (b *WindowBackend) MouseWheel() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	v := int(b.wheelAccum)
	b.wheelAccum -= float64(v)
	return v
}

// MousePos returns the cursor position in window pixels.
func (b *WindowBackend) MousePos() (int, int) {
	return ebiten.CursorPosition()
}

// MouseDown reports whether a button is held right now (0 left,
// 1 middle, 2 right). Unlike ClickedButton it never consumes: pairing
// it with a latched drag flag is how scripts implement scrollbar
// dragging (click starts it, release ends it).
func (b *WindowBackend) MouseDown(btn int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if btn < 0 || btn >= len(b.mouseDown) {
		return false, fmt.Errorf("不明なボタン %d です", btn)
	}
	return b.mouseDown[btn], nil
}

// PadCount reports connected gamepads.
func (b *WindowBackend) PadCount() int {
	return len(ebiten.AppendGamepadIDs(nil))
}

// TouchCount reports active touches.
func (b *WindowBackend) TouchCount() int {
	return len(ebiten.AppendTouchIDs(nil))
}

// TouchPos reports the i-th touch position (window pixels).
func (b *WindowBackend) TouchPos(i int) (int, int, error) {
	ids := ebiten.AppendTouchIDs(nil)
	if i < 0 || i >= len(ids) {
		return 0, 0, fmt.Errorf("不明なタッチ %d です（接触数 %d）", i, len(ids))
	}
	x, y := ebiten.TouchPosition(ids[i])
	return x, y, nil
}

// padID resolves a script pad index or reports an error.
func (b *WindowBackend) padID(id int) (ebiten.GamepadID, error) {
	ids := ebiten.AppendGamepadIDs(nil)
	if id < 0 || id >= len(ids) {
		return 0, fmt.Errorf("不明なパッド %d です（接続数 %d）", id, len(ids))
	}
	return ids[id], nil
}

// PadButton reports a standard-layout button (0-16: A,B,X,Y,LB,RB,
// LT,RT,Select,Start,L3,R3,Up,Down,Left,Right,Home).
func (b *WindowBackend) PadButton(id, btn int) (bool, error) {
	if btn < 0 || btn > 16 {
		return false, fmt.Errorf("パッドボタンは 0 から 16 の範囲で指定してください。%d が指定されました", btn)
	}
	gid, err := b.padID(id)
	if err != nil {
		return false, err
	}
	return ebiten.StandardGamepadButtonValue(gid, ebiten.StandardGamepadButton(btn)) > 0.5, nil
}

// PadAxis reports a stick axis 0-3 (left X/Y, right X/Y) in -1..1.
func (b *WindowBackend) PadAxis(id, axis int) (float64, error) {
	if axis < 0 || axis > 3 {
		return 0, fmt.Errorf("パッド軸は 0 から 3 の範囲で指定してください。%d が指定されました", axis)
	}
	gid, err := b.padID(id)
	if err != nil {
		return 0, err
	}
	return ebiten.StandardGamepadAxisValue(gid, ebiten.StandardGamepadAxis(axis)), nil
}

// PadName reports a gamepad name.
func (b *WindowBackend) PadName(id int) (string, error) {
	gid, err := b.padID(id)
	if err != nil {
		return "", err
	}
	return ebiten.GamepadName(gid), nil
}

// Clicked reports a click since the last call (consumed).
// Kept for compatibility; ClickedButton(0) is the same.
func (b *WindowBackend) Clicked() bool {
	c, _ := b.ClickedButton(0)
	return c
}

// ClickedButton reports button 0/1/2 (left/middle/right) since the
// last call (consumed).
func (b *WindowBackend) ClickedButton(btn int) (bool, error) {
	if _, ok := clickButton(btn); !ok {
		return false, fmt.Errorf("マウスボタンは 0 から 2 の範囲で指定してください。%d が指定されました", btn)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.clickFlag[btn]
	b.clickFlag[btn] = false
	return c, nil
}

// CursorVisible reports whether the system cursor is shown.
func (b *WindowBackend) CursorVisible() bool {
	return ebiten.CursorMode() == ebiten.CursorModeVisible
}

// SetCursorVisible shows (true) or hides (false) the system cursor.
func (b *WindowBackend) SetCursorVisible(on bool) {
	if on {
		ebiten.SetCursorMode(ebiten.CursorModeVisible)
	} else {
		ebiten.SetCursorMode(ebiten.CursorModeHidden)
	}
}

// ReadImmediate returns characters typed since the previous call, for
// the GUI input() builtin (immediate mode: never blocks). Text comes
// from the OS (locale-dependent Unicode translation: layout, shift,
// and caps-correct, e.g. Shift+A is "A"), including IME-committed
// text drained from the pending stream. Text (including OS
// key-repeat arrivals) fires at once, once per tick;
// "" when nothing fires. Control keys are synthesized as their ASCII
// characters (backspace "\x08", tab "\x09", enter "\r", esc "\x1b",
// delete "\x7f") with synthesized repeat timing; use asc() for the
// codes, and break accumulation loops on "\r". While a focused
// inputbox owns the key stream it reports "" so keystrokes are not
// processed twice. While the IME field is focused it owns the whole
// stream: raw characters are skipped (the field already holds them)
// and only drained commits plus synthesized controls are reported.
// Other non-character keys (arrows, F-keys) never appear here; use
// getkey() for those. Every-frame polling is expected.
func (b *WindowBackend) ReadImmediate() string {
	b.mu.Lock()
	if b.focusedLocked() != nil {
		b.mu.Unlock()
		return ""
	}
	imeOn := b.imeField.IsFocused()
	pend := b.imePending
	b.imePending = ""
	comp := b.imeComposing
	b.mu.Unlock()
	var sb strings.Builder
	sb.WriteString(pend)
	if !imeOn {
		sb.WriteString(string(ebiten.AppendInputChars(nil)))
	}
	now := b.Tick()
	down := make([]bool, len(ctrlKeys))
	for i, ck := range ctrlKeys {
		down[i] = ebiten.IsKeyPressed(ck.key)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.ctrlSt) != len(ctrlKeys) {
		b.ctrlSt = make([]ctrlState, len(ctrlKeys))
	}
	// Mid-conversion every control key belongs to the IME (editing
	// or committing the composition); synthesizing here would apply
	// keystrokes twice.
	if imeOn && comp != "" {
		return charStep(&b.charSt, sb.String(), now)
	}
	for i, ck := range ctrlKeys {
		if ctrlStep(&b.ctrlSt[i], down[i], now) {
			sb.WriteString(ck.ch)
		}
	}
	return charStep(&b.charSt, sb.String(), now)
}

// KeyDown reports whether a script key code is held. Modifier codes
// 16/17/18 match either side (left or right).
func (b *WindowBackend) KeyDown(code int) (bool, bool) {
	switch code {
	case 16:
		return shiftHeld(), true
	case 17:
		return ebiten.IsKeyPressed(ebiten.KeyControlLeft) || ebiten.IsKeyPressed(ebiten.KeyControlRight), true
	case 18:
		return ebiten.IsKeyPressed(ebiten.KeyAltLeft) || ebiten.IsKeyPressed(ebiten.KeyAltRight), true
	}
	k, ok := keyFor(code)
	if !ok {
		return false, false
	}
	return ebiten.IsKeyPressed(k), true
}
