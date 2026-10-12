package gui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// mustNew builds a backend or skips when no system font is available
// (font-less CI containers); GUI text needs an OS font since no font
// file is bundled.
func mustNew(t *testing.T) *WindowBackend {
	t.Helper()
	b, err := New(640, 480)
	if err != nil {
		t.Skipf("system font unavailable: %v", err)
	}
	return b
}

func TestSystemFontLoads(t *testing.T) {
	src, p, err := systemFontSource()
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	if src == nil || p == "" {
		t.Fatal("want cached source and path")
	}
	face, err := loadFace(16)
	if err != nil || face == nil {
		t.Fatalf("loadFace: %v", err)
	}
	if FontPath() != p {
		t.Fatalf("FontPath = %q, want %q", FontPath(), p)
	}
}

func TestSetAreaTextHeadless(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		defer close(done)
		addErr = b.AddArea(30, 0, 0, 200, 100, "one\ntwo")
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddArea never completed")
	}
	if addErr != nil {
		t.Fatalf("AddArea: %v", addErr)
	}
	set := make(chan struct{})
	var setErr error
	go func() {
		defer close(set)
		setErr = b.SetAreaText(30, "three\nfour")
	}()
	if !pumpUntil(b, set, 10*time.Second) {
		t.Fatal("SetAreaText never completed")
	}
	if setErr != nil {
		t.Fatalf("SetAreaText: %v", setErr)
	}
	if s, _ := b.AreaText(30); s != "three\nfour" {
		t.Fatalf("AreaText(30) = %q, want three\\nfour", s)
	}
	if err := b.SetAreaText(99, "x"); err == nil {
		t.Fatal("SetAreaText unknown should error")
	}
}

func TestWrapDialogText(t *testing.T) {
	face, err := loadFace(16)
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	if got := wrapDialogText("short", face, 500); len(got) != 1 || got[0] != "short" {
		t.Fatalf("short = %q", got)
	}
	long := "あいうえおかきくけこさしすせそたちつてとなにぬねの"
	lines := wrapDialogText(long, face, 200)
	if len(lines) < 2 {
		t.Fatalf("long should wrap, got %q", lines)
	}
	for _, ln := range lines {
		if w, _ := text.Measure(ln, face, 0); w > 201 {
			t.Fatalf("line %q too wide: %v", ln, w)
		}
	}
	if strings.Join(lines, "") != long {
		t.Fatalf("wrap lost text: %q", lines)
	}
	multi := wrapDialogText("a b c\nd e", face, 500)
	if len(multi) != 2 || multi[0] != "a b c" || multi[1] != "d e" {
		t.Fatalf("explicit breaks = %q", multi)
	}
}

func TestCharStep(t *testing.T) {
	var st charRep
	if got := charStep(&st, "", 0); got != "" {
		t.Fatalf("idle = %q, want empty", got)
	}
	// New text fires at once.
	if got := charStep(&st, "a", 0); got != "a" {
		t.Fatalf("new = %q, want a", got)
	}
	// Same-tick re-poll stays quiet (no double report).
	if got := charStep(&st, "a", 0); got != "" {
		t.Fatalf("dup = %q, want empty", got)
	}
	// OS auto-repeat arrivals on later ticks pass through untouched.
	if got := charStep(&st, "a", 1); got != "a" {
		t.Fatalf("repeat = %q, want a", got)
	}
	if got := charStep(&st, "a", 2); got != "a" {
		t.Fatalf("repeat2 = %q, want a", got)
	}
	// Different text always fires at once, even mid-tick.
	if got := charStep(&st, "b", 2); got != "b" {
		t.Fatalf("switch = %q, want b", got)
	}
	if got := charStep(&st, "b", 2); got != "" {
		t.Fatalf("switch dup = %q, want empty", got)
	}
	// Gaps stay quiet; later arrivals still fire.
	if got := charStep(&st, "", 3); got != "" {
		t.Fatalf("gap = %q, want empty", got)
	}
	if got := charStep(&st, "a", 1000); got != "a" {
		t.Fatalf("late = %q, want a", got)
	}
}

// TestCtrlStep checks control-key repeat without a game loop:
// fire on press, quiet during the delay, refire on the interval,
// release resets to fire-at-once.
func TestCtrlStep(t *testing.T) {
	var st ctrlState
	if ctrlStep(&st, false, 0) {
		t.Fatal("idle should not fire")
	}
	if !ctrlStep(&st, true, 0) {
		t.Fatal("press should fire")
	}
	if ctrlStep(&st, true, 10) {
		t.Fatal("hold below delay should not fire")
	}
	if !ctrlStep(&st, true, repDelay) {
		t.Fatal("delay should refire")
	}
	if !ctrlStep(&st, true, repDelay+repInterval) {
		t.Fatal("interval should refire")
	}
	if ctrlStep(&st, true, repDelay+repInterval+1) {
		t.Fatal("off-beat hold should not fire")
	}
	if ctrlStep(&st, false, repDelay+repInterval+2) {
		t.Fatal("release should not fire")
	}
	if !ctrlStep(&st, true, 1000) {
		t.Fatal("re-press should fire at once")
	}
	if len(ctrlKeys) != 6 {
		t.Fatalf("ctrlKeys = %d entries, want 6", len(ctrlKeys))
	}
	want := map[string]bool{"\x08": true, "\x09": true, "\r": true, "\x1b": true, "\x7f": true}
	for _, ck := range ctrlKeys {
		if !want[ck.ch] {
			t.Fatalf("unexpected control char %q", ck.ch)
		}
	}
}

// TestImeDiff checks the field mirror: appends pass through,
// in-field deletions become tail drops, identical text is a no-op.
func TestImeDiff(t *testing.T) {
	if d, a := imeDiff("", "あ"); d != 0 || a != "あ" {
		t.Fatalf("type = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あ", "あい"); d != 0 || a != "い" {
		t.Fatalf("append = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あい", "あ"); d != 1 || a != "" {
		t.Fatalf("backspace = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あいう", "あ"); d != 2 || a != "" {
		t.Fatalf("multi-drop = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あい", "あう"); d != 1 || a != "う" {
		t.Fatalf("replace = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あ", "あ"); d != 0 || a != "" {
		t.Fatalf("idle = (%d,%q)", d, a)
	}
	// Post-delete commits are not swallowed (the stale-mark bug).
	if d, a := imeDiff("あ", "あい"); d != 0 || a != "い" {
		t.Fatalf("retype = (%d,%q)", d, a)
	}
}

// TestCaretPixelsHeadless in ime_test.go covers the caret anchor;
// backend_test.go keeps input/edit coverage.

// TestDropLastLocked checks tail truncation across the consumer
// chain: pending first, then the focused editor.
func TestDropLastLocked(t *testing.T) {
	b := mustNew(t)
	b.mu.Lock()
	b.imePending = "abcde"
	b.dropLastLocked(2)
	if b.imePending != "abc" {
		t.Fatalf("pending = %q, want abc", b.imePending)
	}
	b.imePending = "ab"
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	b.widgets[3] = &widgetEntry{id: 3, kind: wInput, selIdx: -1,
		edit: &inputState{text: []rune("XY"), caret: 2, focused: true}}
	b.dropLastLocked(3)
	if b.imePending != "" || string(b.widgets[3].edit.text) != "X" {
		t.Fatalf("edit = %q/%q", b.imePending, string(b.widgets[3].edit.text))
	}
	b.widgets[3].edit.focused = false
	b.widgets[3].edit.text = []rune("hello")
	b.widgets[3].edit.caret = 5
	b.imePending = ""
	b.dropLastLocked(2)
	e := b.widgets[3].edit
	if string(e.text) != "hello" || e.caret != 5 {
		t.Fatalf("unfocused edit = %q/%d (must be untouched)", string(e.text), e.caret)
	}
	b.mu.Unlock()
}

func TestReadImmediateHeadless(t *testing.T) {
	b := mustNew(t)
	// Headless: no loop, nothing typed.
	if got := b.ReadImmediate(); got != "" {
		t.Fatalf("ReadImmediate = %q, want empty", got)
	}
	// While a focused inputbox owns the stream, input stays silent.
	b.mu.Lock()
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	b.widgets[4] = &widgetEntry{id: 4, kind: wInput, selIdx: -1,
		edit: &inputState{focused: true}}
	b.mu.Unlock()
	if got := b.ReadImmediate(); got != "" {
		t.Fatalf("ReadImmediate during edit = %q, want empty", got)
	}
	// Drained IME commits flow through input().
	b.mu.Lock()
	b.widgets[4].edit.focused = false
	b.imePending = "あ"
	b.mu.Unlock()
	if got := b.ReadImmediate(); got != "あ" {
		t.Fatalf("ReadImmediate pending = %q, want あ", got)
	}
	b.mu.Lock()
	if b.imePending != "" {
		t.Fatalf("pending not drained: %q", b.imePending)
	}
	b.mu.Unlock()
}

func TestKeyForExtended(t *testing.T) {
	for _, c := range []int{8, 9, 16, 17, 18, 33, 34, 35, 36, 45, 46, 112, 123, 186, 192, 219, 222} {
		if _, ok := keyFor(c); !ok {
			t.Fatalf("keyFor(%d) should be known", c)
		}
	}
	if _, ok := keyFor(999); ok {
		t.Fatal("keyFor(999) should be unknown")
	}
	if _, ok := keyFor(124); ok {
		t.Fatal("keyFor(124) should be unknown")
	}
}

func TestSetFontFileHeadless(t *testing.T) {
	b := mustNew(t)
	base := FontPath()
	if base == "" {
		t.Skip("no system font")
	}
	// Bare base-name resolution hits the same file.
	name := base
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	got, err := resolveFontSpec(name)
	if err != nil {
		t.Fatalf("resolveFontSpec(%q): %v", name, err)
	}
	if !strings.EqualFold(got, base) {
		t.Fatalf("resolved = %q, want %q", got, base)
	}
	if err := b.SetFontFile(name, 24); err != nil {
		t.Fatalf("SetFontFile: %v", err)
	}
	if b.FontSize() != 24 {
		t.Fatalf("FontSize = %d, want 24", b.FontSize())
	}
	if b.FontFile() == "" {
		t.Fatal("FontFile should be set after SetFontFile")
	}
	if err := b.SetFontFile("no-such-font-xyz-123", 16); err == nil {
		t.Fatal("bogus font should error")
	}
	if err := b.SetFontFile(name, 7); err == nil {
		t.Fatal("size 7 should error")
	}
}

// TestFontPerUserDir covers bare-name resolution in the per-user font
// directory (Windows default for non-admin installs). The scan only
// needs the file name, so a dummy file suffices (no parsing involved).
func TestFontPerUserDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("per-user Fonts dir is a Windows concept")
	}
	dir := filepath.Join(t.TempDir(), "Microsoft", "Windows", "Fonts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	const name = "TestDummyPlemolJP-Regular.ttf"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", filepath.Dir(filepath.Dir(filepath.Dir(dir))))
	got, err := resolveFontSpec(name)
	if err != nil {
		t.Fatalf("resolveFontSpec(%q): %v", name, err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, name)) {
		t.Fatalf("resolved = %q, want under per-user Fonts", got)
	}
}

// TestFontExtraDirs covers caller-owned search roots (script directory,
// working directory): exact file names, extension-less names (with
// extension completion), and extension-less prefix matches all resolve
// there before the system directories, on any OS. The scan only needs
// the file name, so dummy files suffice (no parsing involved).
func TestFontExtraDirs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"ZZZSibExact.ttf",
		"ZZZSibExt.ttf",
		"ZZZSibPrefix-Regular.ttf",
		"ZZZSibPrefix-Bold.ttf",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("dummy"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Exact file name wins inside the extra dir.
	got, err := resolveFontSpec("ZZZSibExact.ttf", dir)
	if err != nil {
		t.Fatalf("resolveFontSpec(exact, dir): %v", err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, "ZZZSibExact.ttf")) {
		t.Fatalf("exact resolved = %q, want the sibling file", got)
	}
	// Extension-less name is completed inside the extra dir.
	got, err = resolveFontSpec("zzzsibext", dir)
	if err != nil {
		t.Fatalf("resolveFontSpec(extless, dir): %v", err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, "ZZZSibExt.ttf")) {
		t.Fatalf("extless resolved = %q, want the sibling file", got)
	}
	// Extension-less prefix prefers the Regular face inside the extra dir.
	got, err = resolveFontSpec("zzzsibprefix", dir)
	if err != nil {
		t.Fatalf("resolveFontSpec(prefix, dir): %v", err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, "ZZZSibPrefix-Regular.ttf")) {
		t.Fatalf("prefix resolved = %q, want the Regular face", got)
	}
	// Without the extra dir the same names are unknown (siblings beat the
	// system search only when the caller puts them first).
	if _, err := resolveFontSpec("ZZZSibExact.ttf"); err == nil {
		t.Fatal("sibling name without its dir should error")
	}
}

// printFrags drives the real mes()/print() path headless and returns the
// fragments it lays out for the canvas layer (cell origin, text, style) in
// call order. Text is baked into the canvas buffer like any other drawing,
// so these fragments are the layout state Draw ends up showing; callers
// must pump (Update) or b.drainQueue() for the staged batch first if they
// want it rasterized.
func printFrags(t *testing.T, b *WindowBackend, s string, st TextStyle, newline bool) []textOp {
	t.Helper()
	d := b.printRaw(s, st, newline)
	if d == nil {
		return nil
	}
	out := make([]textOp, 0, len(d.ops))
	for _, op := range d.ops {
		if op.frag {
			out = append(out, op)
		}
	}
	return out
}

// fragLines joins the fragments of each row (left to right) back into the
// text a reader sees: every print() call is its own fragment, but
// consecutive calls continue the same line, so the visible composition is
// what tests assert.
func fragLines(ops []textOp) []string {
	idx := map[int]int{}
	var out []string
	for _, op := range ops {
		if op.s == "" {
			continue
		}
		i, ok := idx[op.y]
		if !ok {
			i = len(out)
			idx[op.y] = i
			out = append(out, "")
		}
		out[i] += op.s
	}
	return out
}

// queuedLens reports the staged (pending) and ready (queue) draw command
// counts, i.e. how much of the frame has been committed. Batched text
// that has not been flushed for a closure yet counts as staged.
func queuedLens(b *WindowBackend) (pending, queue int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	staged := len(b.pending)
	if !b.textBatch.empty() {
		staged++
	}
	return staged, len(b.queue)
}

func TestPrintlnState(t *testing.T) {
	b := mustNew(t)
	frags := append(printFrags(t, b, "Hello", 0, true), printFrags(t, b, "World", 0, true)...)
	if len(frags) != 2 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].s != "Hello" || frags[1].s != "World" {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].y != 0 || frags[1].y != 16 {
		t.Fatalf("rows: %+v", frags)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.curY != 32 {
		t.Fatalf("curY = %v, want 32", b.curY)
	}
}

// TestPrintlnHonorsPos covers pos() + mes()/print() positioning: the text
// origin is the pos() pixel itself (1px granularity), and a continuation
// print() lands beside what came before.
func TestPrintlnHonorsPos(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "plain", 0, true)
	b.MoveTo(37, 41) // odd pixels: no cell snapping
	frags = append(frags, printFrags(t, b, "hi", 0, true)...)
	if len(frags) != 2 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].x != 0 || frags[0].y != 0 || frags[0].s != "plain" {
		t.Fatalf("frags[0]: %+v", frags[0])
	}
	if frags[1].x != 37 || frags[1].y != 41 || frags[1].s != "hi" {
		t.Fatalf("frags[1]: %+v", frags[1])
	}
}

// TestPrintlnMultilinePos checks continuation lines come back to the
// anchor the line started at (the pos() origin), one line pitch down.
func TestPrintlnMultilinePos(t *testing.T) {
	b := mustNew(t)
	b.MoveTo(24, 64)
	frags := printFrags(t, b, "a\nb", 0, true)
	if len(frags) != 2 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].x != 24 || frags[0].y != 64 || frags[0].s != "a" {
		t.Fatalf("frags[0]: %+v", frags[0])
	}
	if frags[1].x != 24 || frags[1].y != 80 || frags[1].s != "b" {
		t.Fatalf("frags[1]: %+v", frags[1])
	}
}

// TestPrintPosContinue locks the print/pos/print composition on one
// layer: the fragments of the first print() stay at the pixel they were
// laid out on, and the continuation lands beside them.
func TestPrintPosContinue(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "ab", 0, false)
	b.MoveTo(32, 16)
	frags = append(frags, printFrags(t, b, "cd", 0, false)...)
	frags = append(frags, printFrags(t, b, "ef", 0, true)...)
	if len(frags) != 3 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].x != 0 || frags[0].y != 0 || frags[0].s != "ab" {
		t.Fatalf("frags[0]: %+v", frags[0])
	}
	if frags[1].x != 32 || frags[1].s != "cd" {
		t.Fatalf("frags[1]: %+v", frags[1])
	}
	// "cd" drew 16px, so "ef" starts 16px after it, not on a cell.
	if frags[2].x != 48 || frags[2].s != "ef" {
		t.Fatalf("frags[2]: %+v", frags[2])
	}
	if got := fragLines(frags); len(got) != 2 || got[0] != "ab" || got[1] != "cdef" {
		t.Fatalf("lines: %q, want [ab cdef]", got)
	}
}

// TestPrintWideContinue locks continuation advances for CJK: printing
// one char per print() call (IME echo loops do this) must not overlap.
func TestPrintWideContinue(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "あ", 0, false)
	frags = append(frags, printFrags(t, b, "い", 0, false)...)
	if len(frags) != 2 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].x != 0 || frags[1].x != 16 {
		t.Fatalf("frag x = %d,%d, want 0,16", frags[0].x, frags[1].x)
	}
}

// TestTextScrollCarry locks the baked-text scroll math: text scrolls by
// shifting the canvas buffer, and each line shifted accumulates in
// scrollExact so the shifted pixels stay exact even if a future
// lineHeight ratio is fractional. Plain layout state, no game loop.
func TestTextScrollCarry(t *testing.T) {
	b := mustNew(t)
	// 10px font: the line pitch is 10px (lineHeight scales with the size).
	if err := b.SetFontSize(10); err != nil {
		t.Fatalf("SetFontSize(10): %v", err)
	}
	if b.lineH != 10 {
		t.Fatalf("lineH = %v, want 10", b.lineH)
	}
	lines := int(float64(b.h)/b.lineH) + 1
	var scrolls []int
	for i := 0; i < lines; i++ {
		d := b.printRaw("x", 0, true)
		if d == nil {
			t.Fatalf("line %d laid out nothing", i)
		}
		for _, op := range d.ops {
			if op.scrollPx > 0 {
				scrolls = append(scrolls, op.scrollPx)
			}
		}
	}
	if len(scrolls) == 0 {
		t.Fatal("no scroll after filling the screen")
	}
	b.mu.Lock()
	exact, done, curX, curY := b.scrollExact, b.scrollDone, b.curX, b.curY
	lastY := float64(b.h) - b.lineH
	b.mu.Unlock()
	if curY != lastY || curX != 0 {
		t.Fatalf("cursor = (%v,%v), want (0,%v)", curX, curY, lastY)
	}
	sum := 0
	for _, px := range scrolls {
		sum += px
	}
	if sum != done {
		t.Fatalf("scroll pixels = %d, scrollDone = %d", sum, done)
	}
	if diff := exact - float64(done); diff < -0.5 || diff > 0.5 {
		t.Fatalf("scroll carry off by %v px (exact %v, shifted %d)", diff, exact, done)
	}
}

// TestPrintTabExpands locks the tab rendering: a tab in mes()/print()
// advances 4 half-width cells of blank (a fixed advance, not a tab stop),
// so the fragment carries the blank and the cursor lands after it.
func TestPrintTabExpands(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "a\tb", 0, false)
	if len(frags) != 1 || frags[0].s != "a    b" {
		t.Fatalf("frags: %+v", frags)
	}
	b.mu.Lock()
	if b.curX != 48 { // "a" + 4×8px + "b"
		t.Fatalf("curX = %v, want 48", b.curX)
	}
	// A tab is a fixed 32px advance even away from the left edge.
	b.curX = 3
	b.mu.Unlock()
	frags = printFrags(t, b, "\tx", 0, false)
	if len(frags) != 1 || frags[0].s != "    x" || frags[0].x != 3 {
		t.Fatalf("offset frags: %+v", frags)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.curX != 43 { // 3 + 32 + "x"
		t.Fatalf("curX = %v, want 43", b.curX)
	}
}

// TestTextWidthTabs keeps strwidth() on the same grid as the mes()
// rendering: a tab costs four half-width cells there too.
func TestTextWidthTabs(t *testing.T) {
	b := mustNew(t)
	if got, want := b.TextWidth("\t"), b.TextWidth("    "); got != want {
		t.Fatalf("tab width = %d, want %d", got, want)
	}
	if got, want := b.TextWidth("a\tb"), b.TextWidth("a    b"); got != want {
		t.Fatalf("a-tab-b width = %d, want %d", got, want)
	}
}

// TestPrintNewlineSplit covers print() with embedded newlines: every
// non-empty line becomes its own fragment (one per line), and the cursor
// ends after the trailing chunk.
func TestPrintNewlineSplit(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "a\nb\nc", 0, false)
	if len(frags) != 3 || frags[0].s != "a" || frags[1].s != "b" || frags[2].s != "c" {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].y != 0 || frags[1].y != 16 || frags[2].y != 32 {
		t.Fatalf("rows: %+v", frags)
	}
	// Left-edge line: continuation lines come back to x=0.
	if frags[0].x != 0 || frags[1].x != 0 || frags[2].x != 0 {
		t.Fatalf("cols: %+v", frags)
	}
	if got := fragLines(frags); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("lines: %q", got)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.curY != 32 || b.curX != 8 { // "c" is 8px wide
		t.Fatalf("cursor = (%v,%v), want (8,32)", b.curX, b.curY)
	}
}

// TestPrintNewlineKeepsAnchor covers a multi-line string printed away from
// the left edge: each embedded newline returns the cursor to the x the
// line started at (the pos() anchor), one line pitch down, so a mes()
// block prints as a rectangle.
func TestPrintNewlineKeepsAnchor(t *testing.T) {
	b := mustNew(t)
	b.MoveTo(80, 40) // any pixel origin, no cell snapping
	frags := printFrags(t, b, "ab\ncd", 0, false)
	if len(frags) != 2 || frags[0].s != "ab" || frags[1].s != "cd" {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].x != 80 || frags[0].y != 40 {
		t.Fatalf("frags[0] at (%d,%d), want (80,40)", frags[0].x, frags[0].y)
	}
	if frags[1].x != 80 || frags[1].y != 56 {
		t.Fatalf("frags[1] at (%d,%d), want (80,56)", frags[1].x, frags[1].y)
	}
	// A second call with an embedded newline keeps the same anchor (the
	// cursor is still on the row the previous block ended on).
	frags = printFrags(t, b, "ef\ngh", 0, false)
	if len(frags) != 2 || frags[1].x != 80 || frags[1].y != 72 {
		t.Fatalf("second block: %+v", frags)
	}
	// A hard newline (mes) then anchors the next line back at x=0.
	_ = printFrags(t, b, "", 0, true)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.curY != 88 || b.curX != 0 || b.lineStartX != 0 {
		t.Fatalf("cursor = (%v,%v) anchor %v, want (0,88) anchor 0", b.curX, b.curY, b.lineStartX)
	}
}

// TestPrintJoinAcrossNewline checks print/print/mes composition across an
// embedded newline: the pieces still read as one line per row.
func TestPrintJoinAcrossNewline(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "ab", 0, false)
	frags = append(frags, printFrags(t, b, "c\nd", 0, false)...)
	frags = append(frags, printFrags(t, b, "e", 0, true)...)
	if got := fragLines(frags); len(got) != 2 || got[0] != "abc" || got[1] != "de" {
		t.Fatalf("lines: %q, want [abc de]", got)
	}
	if frags[len(frags)-1].y != 16 {
		t.Fatalf("last fragment row = %d, want 16", frags[len(frags)-1].y)
	}
}

// TestPrintTrailingNewline leaves the cursor clean on the next line (an
// empty chunk emits no fragment).
func TestPrintTrailingNewline(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "a\n", 0, false)
	if len(frags) != 1 || frags[0].s != "a" {
		t.Fatalf("frags: %+v", frags)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.curY != 16 || b.curX != 0 {
		t.Fatalf("cursor = (%v,%v), want (0,16)", b.curX, b.curY)
	}
}

// TestPrintNewlineStyleChange keeps per-fragment styles across the split:
// each call is its own fragment with its own style.
func TestPrintNewlineStyleChange(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "a", StyleBold, false)
	frags = append(frags, printFrags(t, b, "b\nc", 0, false)...)
	if len(frags) != 3 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].s != "a" || frags[0].st != StyleBold {
		t.Fatalf("frags[0]: %+v", frags[0])
	}
	if frags[1].s != "b" || frags[1].st != 0 {
		t.Fatalf("frags[1]: %+v", frags[1])
	}
	if frags[2].s != "c" || frags[2].st != 0 || frags[2].y != 16 || frags[2].x != 0 {
		t.Fatalf("frags[2]: %+v", frags[2])
	}
}

func TestPrintStyles(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "a", StyleBold, true)
	frags = append(frags, printFrags(t, b, "b", StyleItalic, false)...)
	// A style change mid-line is fine: the next call is its own fragment,
	// so "b" keeps italic.
	frags = append(frags, printFrags(t, b, "c", 0, true)...)
	// Same-style print() calls stay separate fragments in the same row.
	frags = append(frags, printFrags(t, b, "x", StyleBold, false)...)
	frags = append(frags, printFrags(t, b, "y", StyleBold, false)...)
	frags = append(frags, printFrags(t, b, "z", StyleBold, true)...)
	if len(frags) != 6 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].st != StyleBold || frags[1].st != StyleItalic || frags[2].st != 0 {
		t.Fatalf("styles: %+v", frags)
	}
	if frags[0].s != "a" || frags[1].s != "b" || frags[2].s != "c" {
		t.Fatalf("texts: %+v", frags)
	}
	if frags[3].s != "x" || frags[4].s != "y" || frags[5].s != "z" {
		t.Fatalf("texts: %+v", frags)
	}
	if frags[3].st != StyleBold || frags[5].st != StyleBold {
		t.Fatalf("styles: %+v", frags)
	}
	// a is the first line; b/c share the second; xyz share the third.
	if frags[0].y != 0 || frags[1].y != 16 || frags[2].y != 16 || frags[3].y != 32 || frags[5].y != 32 {
		t.Fatalf("rows: %+v", frags)
	}
	if got := fragLines(frags); len(got) != 3 || got[0] != "a" || got[1] != "bc" || got[2] != "xyz" {
		t.Fatalf("lines: %q, want [a bc xyz]", got)
	}
}

// TestTextStagedWithCanvas locks that mes()/print() ride the canvas
// frame: consecutive calls are batched into one staged command and applied
// together with the rest of the frame at a boundary, so Draw never sees
// text half entered and text cannot overtake or lag the shapes drawn
// around it.
func TestTextStagedWithCanvas(t *testing.T) {
	b := mustNew(t)
	// No yield yet: the staged batch goes straight to the game queue
	// when flushed.
	b.Println("first", 0)
	if p, q := queuedLens(b); q != 0 || p != 1 {
		t.Fatalf("before yield: pending=%d queue=%d, want 1/0", p, q)
	}
	b.drainQueue()
	if p, q := queuedLens(b); q != 0 || p != 0 {
		t.Fatalf("after drain: pending=%d queue=%d, want 0/0", p, q)
	}
	// First yield switches to staged mode: batched text stages with the
	// rest of the frame.
	b.Await(0)
	b.Print("one\ntwo", 0)
	if p, q := queuedLens(b); q != 0 || p != 1 {
		t.Fatalf("after yield: pending=%d queue=%d, want 1/0", p, q)
	}
	// Update alone must not publish the staged frame.
	if err := b.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p, q := queuedLens(b); q != 0 || p != 1 {
		t.Fatalf("Update flushed the staged text: pending=%d queue=%d", p, q)
	}
	// The next boundary commits it with the rest of the frame.
	b.Await(0)
	if p, q := queuedLens(b); q != 1 || p != 0 {
		t.Fatalf("after boundary: pending=%d queue=%d, want 0/1", p, q)
	}
	b.drainQueue()
	if p, q := queuedLens(b); q != 0 || p != 0 {
		t.Fatalf("after drainQueue: pending=%d queue=%d, want 0/0", p, q)
	}
}

// TestTextAppliesWithoutYield: a script that never awaits/sleeps has no
// frame boundary, so Update applies its queued text (it must not be stuck
// off screen). Once the script has yielded, only yield points commit.

// TestTextBatchMergesBurst locks the batching contract: consecutive
// mes()/print() calls collapse adjacent repeated scrolls, share one flush
// point, and still apply in script order when merged with everything in
// the burst.
func TestTextBatchMergesBurst(t *testing.T) {
	b := mustNew(t)
	b.Println("one", 0)
	b.Print("two", 0)
	b.Println("three", 0)
	b.mu.Lock()
	if len(b.queue) != 0 {
		b.mu.Unlock()
		t.Fatalf("burst went straight to the game queue: %d", len(b.queue))
	}
	batch := b.textBatch
	if batch == nil || len(batch.ops) == 0 {
		b.mu.Unlock()
		t.Fatal("want a staged text batch")
	}
	scrolls, frags := 0, 0
	for _, op := range batch.ops {
		if op.scrollPx > 0 {
			scrolls++
		}
		if op.frag {
			frags++
		}
	}
	sel := batch.sel
	b.mu.Unlock()
	if sel != 0 {
		t.Fatalf("batch target = %d, want 0", sel)
	}
	if frags != 3 {
		t.Fatalf("batched fragments = %d, want 3 (one per call)", frags)
	}
	if scrolls > 1 {
		t.Fatalf("adjacent scrolls = %d, want none repeated", scrolls)
	}
	b.drainQueue()
	b.mu.Lock()
	left := len(b.queue)
	b.textBatch = nil
	b.mu.Unlock()
	if left != 0 {
		t.Fatalf("batched closures = %d, want 0", left)
	}
	if p, q := queuedLens(b); p != 0 || q != 0 {
		t.Fatalf("after drain: pending=%d queue=%d, want 0/0", p, q)
	}
}

// TestTextBatchFlushesBeforeShapes locks the ordering contract: a shape
// drawn after batched text flushes it first, so call order wins on the
// shared canvas layer.
func TestTextBatchFlushesBeforeShapes(t *testing.T) {
	b := mustNew(t)
	// Staged mode: the batch must land in pending (not the live queue)
	// ahead of the shape, so one boundary commits them in call order.
	b.Await(0)
	b.Println("first", 0)
	b.FillRect(0, 0, 10, 10, [4]int{255, 0, 0, 255})
	b.Println("second", 0)
	b.mu.Lock()
	staged := len(b.queue)
	pend := len(b.pending)
	batched := !b.textBatch.empty()
	b.textBatch = nil
	b.mu.Unlock()
	if staged != 0 {
		t.Fatalf("batched text went straight to the game queue: %d", staged)
	}
	if pend != 2 {
		t.Fatalf("pending = %d, want 2 (text, shape)", pend)
	}
	if !batched {
		t.Fatal("second println must remain batched after the shape")
	}
}

func TestTextAppliesWithoutYield(t *testing.T) {
	b := mustNew(t)
	b.Println("no await", 0)
	if err := b.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p, q := queuedLens(b); q != 0 || p != 0 {
		t.Fatalf("Update should apply the immediate text draw: pending=%d queue=%d", p, q)
	}
	// After a yield, Update leaves the staged frame alone.
	b.Await(0)
	b.Println("later", 0)
	if err := b.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p, q := queuedLens(b); q != 0 || p != 1 {
		t.Fatalf("Update published after a yield: pending=%d queue=%d", p, q)
	}
	b.Await(0)
	if p, q := queuedLens(b); q != 1 || p != 0 {
		t.Fatalf("await should commit the frame: pending=%d queue=%d", p, q)
	}
}

// TestCanvasFrameBoundary locks the canvas flicker fix: after the first
// yield, draw commands are staged in pending and flushed as one frame at
// await()/sleep()/end, and Draw observes only the committed drawSel.
// A cls()->draws sequence in progress must not blank the canvas for a frame.
func TestCanvasFrameBoundary(t *testing.T) {
	b := mustNew(t)
	// No yield yet: enqueue goes straight to the game queue.
	b.FillRect(0, 0, 10, 10, [4]int{255, 0, 0, 255})
	b.mu.Lock()
	if len(b.pending) != 0 || len(b.queue) != 1 {
		b.mu.Unlock()
		t.Fatalf("before yield: pending=%d queue=%d, want 0/1", len(b.pending), len(b.queue))
	}
	b.mu.Unlock()
	b.drainQueue()
	b.mu.Lock()
	if len(b.queue) != 0 {
		b.mu.Unlock()
		t.Fatal("drainQueue should apply immediate commands")
	}
	b.mu.Unlock()
	// First yield switches to staged mode.
	b.Await(0)
	b.FillRect(0, 0, 10, 10, [4]int{255, 0, 0, 255})
	b.mu.Lock()
	if len(b.pending) != 1 || len(b.queue) != 0 {
		pend, q := len(b.pending), len(b.queue)
		b.mu.Unlock()
		t.Fatalf("after yield: pending=%d queue=%d, want 1/0", pend, q)
	}
	b.mu.Unlock()
	// Update alone must not publish the staged frame.
	if err := b.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	b.mu.Lock()
	if len(b.pending) != 1 {
		b.mu.Unlock()
		t.Fatal("Update must not flush a staged frame")
	}
	b.mu.Unlock()
	// The next boundary flushes it as one batch.
	b.Await(0)
	b.mu.Lock()
	if len(b.pending) != 0 || len(b.queue) != 1 {
		pend, q := len(b.pending), len(b.queue)
		b.mu.Unlock()
		t.Fatalf("after boundary: pending=%d queue=%d, want 0/1", pend, q)
	}
	b.mu.Unlock()
	b.drainQueue()
	// gsel() mid-frame must not switch the visible buffer until commit.
	if err := b.SelectTarget(1); err != nil {
		t.Fatalf("SelectTarget: %v", err)
	}
	b.mu.Lock()
	if b.drawSel != 0 {
		b.mu.Unlock()
		t.Fatalf("gsel leaked to Draw before the boundary: drawSel=%d", b.drawSel)
	}
	b.mu.Unlock()
	b.Await(0)
	b.mu.Lock()
	if b.drawSel != 1 {
		b.mu.Unlock()
		t.Fatalf("drawSel=%d, want 1 after boundary", b.drawSel)
	}
	b.mu.Unlock()
}

// TestWidgetRegistryHeadless drives AddButton with a manual pump
// (no game loop): creation path is pure Go until Draw/Update run.
func TestWidgetRegistryHeadless(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		addErr = b.AddButton(11, "T", 10, 420, 100, 36, 0)
		close(done)
	}()
	// Pump until done: the helper goroutine may need scheduling time,
	// so poll with a deadline instead of a fixed iteration count.
	deadline := time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done:
			goto pumped
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("AddButton never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped:
	if addErr != nil {
		t.Fatalf("AddButton: %v", addErr)
	}
	if !b.HasWidget(11) {
		t.Fatal("button 11 missing after headless add")
	}
}

// TestIMEStateHeadless covers focus/query/composition mirrors
// without pumping (no game loop needed for state).
func TestIMEStateHeadless(t *testing.T) {
	b := mustNew(t)
	// Another backend's focus must not leak: start clean.
	b.IMESet(false)
	if got := b.IMEState(); got != 0 {
		t.Fatalf("IMEState = %d, want 0", got)
	}
	if got := b.IMESet(true); got != 1 {
		t.Fatalf("IMESet(true) = %d, want 1", got)
	}
	if got := b.IMEState(); got != 1 {
		t.Fatalf("IMEState = %d, want 1", got)
	}
	if got := b.IMEComposition(); got != "" {
		t.Fatalf("IMEComposition = %q, want empty", got)
	}
	if got := b.IMESet(false); got != 0 {
		t.Fatalf("IMESet(false) = %d, want 0", got)
	}
}

// TestReadImmediateIdleHeadless: with no loop running nothing is
// typed, so input reports "" and arms no session.
func TestReadImmediateIdleHeadless(t *testing.T) {
	b := mustNew(t)
	if got := b.ReadImmediate(); got != "" {
		t.Fatalf("ReadImmediate = %q, want empty", got)
	}
	if b.charSt != (charRep{}) {
		t.Fatal("charSt should be idle")
	}
}

// TestPicLoadFirst is a regression test: PicLoad as the very first
// image op must not panic on a nil targets map (global picload).
func TestPicLoadFirst(t *testing.T) {
	b := mustNew(t)
	if b.targets == nil || b.bufs == nil {
		t.Fatal("image maps must be initialized by New")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	done := make(chan struct{})
	var id int
	var loadErr error
	go func() {
		id, loadErr = b.PicLoad(p)
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done:
			goto pumped
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("PicLoad never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped:
	if loadErr != nil {
		t.Fatalf("PicLoad: %v", loadErr)
	}
	if id <= 0 {
		t.Fatalf("id = %d, want > 0", id)
	}
	if b.targets[id] == nil {
		t.Fatal("uploaded image missing from targets")
	}
}

// TestButtonImageHeadless covers the icon path: unknown buffer errors
// without a loop, and a picload buffer attaches headless with a pump.
func TestButtonImageHeadless(t *testing.T) {
	b := mustNew(t)
	if err := b.AddButton(11, "T", 10, 420, 100, 36, 99); err == nil {
		t.Fatal("unknown image buffer should error")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "icon.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	done := make(chan struct{})
	var imgID int
	var loadErr error
	go func() {
		imgID, loadErr = b.PicLoad(p)
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done:
			goto pumped
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("PicLoad never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped:
	if loadErr != nil {
		t.Fatalf("PicLoad: %v", loadErr)
	}
	done2 := make(chan struct{})
	var addErr error
	go func() {
		addErr = b.AddButton(12, "Icon", 120, 420, 100, 36, imgID)
		close(done2)
	}()
	deadline = time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done2:
			goto pumped2
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("AddButton never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped2:
	if addErr != nil {
		t.Fatalf("AddButton with image: %v", addErr)
	}
	if !b.HasWidget(12) {
		t.Fatal("image button 12 missing after headless add")
	}
}

// TestButtonImageStatesHeadless validates the 4 image slots
// (normal/hover/pressed/disabled) without a game loop.
func TestButtonImageStatesHeadless(t *testing.T) {
	b := mustNew(t)
	if err := b.AddButton(21, "T", 10, 10, 100, 36, 1, 2, 3, 4, 5); err == nil {
		t.Fatal("5 images should error")
	}
	if err := b.AddButton(21, "T", 10, 10, 100, 36, 99); err == nil {
		t.Fatal("unknown buffer should error")
	}
	if err := b.AddButton(21, "T", 10, 10, 100, 36, 0, 99); err == nil {
		t.Fatal("unknown hover buffer should error")
	}
}

// renderGame performs all pixel probes in a single RunGame: Ebiten allows
// only one game loop per process, and draw commands batch a frame behind,
// so each stage draws for two frames and samples on the next one.
// pxRes carries one GetPixel round-trip result.
type pxRes struct {
	c   [4]int
	err error
}

type renderGame struct {
	b                              *WindowBackend
	face                           *text.GoTextFace
	frames                         int
	fillLit, textLit, backLit      int
	rectLit, circleLit, anyLit     int
	minX, minY, maxX, maxY         int
	keyIdle, mouseX, mouseY        int
	clickedVal                     bool
	probedInput                    bool
	filled, texted, backed, shaped bool
	done                           bool
	wLaunched, wPhase2Launched     bool
	wChecked1, wChecked2           bool
	wPhase1, wPhase2               chan struct{}
	saveLaunched, saveChecked      bool
	savePath                       string
	saveDone                       chan struct{}
	saveErr                        error
	gzChecked                      bool
	pfChecked                      bool
	pxLaunched, pxChecked          bool
	pxDone                         chan pxRes
	edLaunched, edMeasured         bool
	edRow                          float64
	edFrames, edDistinct           int
	edHashes                       []uint64
	textAlone, textCovered, textOn int
	imeCompChecked                 bool
	bufChecked                     bool
	resized                        bool
	resizeFail                     string
	wFail                          string
	wText                          string
	wErrs                          []string
}

func (g *renderGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	// Pump the backend like the real loop does (drains the draw queue).
	_ = g.b.Update()
	return nil
}

func (g *renderGame) Draw(screen *ebiten.Image) {
	g.frames++
	switch {
	case g.frames <= 2:
		screen.Fill(color.NRGBA{0xFF, 0, 0, 0xFF})
		if g.frames == 2 {
			if r, _, _, _ := screen.At(100, 100).RGBA(); r > 0 {
				g.fillLit = 1
			}
			g.filled = true
		}
	case g.frames <= 4:
		screen.Fill(color.Black)
		g.drawDirectText(screen)
		if g.frames == 4 {
			g.textLit = countLit(screen, 0, 0, 160, 40)
			g.texted = true
		}
	case g.frames <= 6:
		g.b.Draw(screen)
		if g.frames == 6 {
			// White window: the pre-loop mes() text is black ink, so
			// the row probe counts dark pixels, not lit ones. Zero
			// means the backend drew nothing; a full region means a
			// black screen (both are failures, asserted at the end).
			g.backLit = countDark(screen, 0, 0, 320, 40)
			g.backed = true
		}
	case g.frames == 7:
		// Enqueue like a script would, then drain synchronously:
		// ebiten does not guarantee 1:1 Update/Draw pairing, so a test
		// must not depend on the next Update having run.
		g.b.FillRect(300, 100, 40, 30, [4]int{255, 0, 0, 255})
		g.b.Circle(100, 300, 20, true, [4]int{0, 255, 0, 255})
		g.b.drainQueue()
		g.b.Draw(screen)
	case g.frames == 8:
		// Text/shape layering probe (text shares the canvas layer).
		g.checkTextLayer(screen)
	case g.frames >= 9:
		g.b.Draw(screen)
		if r, _, _, _ := screen.At(310, 110).RGBA(); r > 0x8000 {
			g.rectLit = 1
		}
		if _, gg, _, _ := screen.At(100, 300).RGBA(); gg > 0x8000 {
			g.circleLit = 1
		}
		g.anyLit = countDark(screen, 0, 30, 640, 480)
		g.minX, g.minY, g.maxX, g.maxY = 9999, 9999, -1, -1
		for y := 0; y < 480; y++ {
			for x := 0; x < 640; x++ {
				r, gg, bl, _ := screen.At(x, y).RGBA()
				// Ink bounds on the white window (diagnostic log only).
				if r < 0x8000 && gg < 0x8000 && bl < 0x8000 {
					if x < g.minX {
						g.minX = x
					}
					if x > g.maxX {
						g.maxX = x
					}
					if y < g.minY {
						g.minY = y
					}
					if y > g.maxY {
						g.maxY = y
					}
				}
			}
		}
		g.shaped = true
		// In-loop input queries (unsafe outside the loop).
		// Idle arrows stand in for the removed stick() probe.
		if left, ok := g.b.KeyDown(37); ok && left {
			g.keyIdle = 1
		}
		if up, ok := g.b.KeyDown(38); ok && up {
			g.keyIdle = 1
		}
		g.mouseX, g.mouseY = g.b.MousePos()
		g.clickedVal = g.b.Clicked()
		g.probedInput = true
		// Widget stage: blocking backend calls run on a helper goroutine
		// (they wait for Update, so never call them on this thread).
		// Phase 1 creates; phase 2 removes after game-thread checks.
		if !g.wLaunched {
			g.wLaunched = true
			g.wPhase1 = make(chan struct{})
			go g.makeWidgets()
		}
		select {
		case <-g.wPhase1:
			// Closed channels stay ready: check exactly once.
			if !g.wChecked1 {
				g.wChecked1 = true
				g.checkWidgetsPhase1()
				if g.wFail == "" && !g.wPhase2Launched {
					g.wPhase2Launched = true
					g.wPhase2 = make(chan struct{})
					go g.removeWidgets()
				}
			}
		default:
		}
		select {
		case <-g.wPhase2:
			if !g.wChecked2 {
				g.wChecked2 = true
				g.checkWidgetsPhase2()
			}
		default:
		}
		// Save stage: snapshot buffer 0 to PNG on a helper goroutine
		// (SavePNG waits for Update, so never call it on this thread).
		if g.wChecked2 && g.wFail == "" && !g.saveChecked {
			if !g.saveLaunched {
				g.saveLaunched = true
				g.saveDone = make(chan struct{})
				go func() {
					if err := g.b.SavePNG(g.savePath, 0); err != nil {
						g.saveErr = err
					}
					close(g.saveDone)
				}()
			}
			select {
			case <-g.saveDone:
				g.saveChecked = true
				g.checkSaved()
			default:
			}
		}
		// Zoom stage: 2x blit on the game thread, then resample.
		// Runs after the save snapshot so buffer 0 stays pristine for it.
		if g.saveChecked && g.wFail == "" && !g.gzChecked {
			g.gzChecked = true
			g.checkZoom(screen)
		}
		// Paint stage: white square then flood-fill, then resample.
		if g.gzChecked && g.wFail == "" && !g.pfChecked {
			g.pfChecked = true
			g.checkPaint(screen)
		}
		// Pixel stage: pget round-trip (SetPixel like a script, read
		// back via GetPixel on a helper goroutine — it waits for
		// Update, so never call it on this thread). Runs after paint
		// so earlier fills cannot clobber the probe pixel.
		if g.pfChecked && g.wFail == "" && !g.pxChecked {
			if !g.pxLaunched {
				g.pxLaunched = true
				g.b.SetPixel(500, 200, [4]int{255, 0, 0, 255})
				g.pxDone = make(chan pxRes, 1)
				go func() {
					c, err := g.b.GetPixel(500, 200)
					g.pxDone <- pxRes{c, err}
				}()
			}
			select {
			case r := <-g.pxDone:
				g.pxChecked = true
				if r.err != nil {
					g.wFail = "pget: " + r.err.Error()
				} else if r.c != [4]int{255, 0, 0, 255} {
					g.wFail = fmt.Sprintf("pget roundtrip = %v, want opaque red", r.c)
				}
			default:
			}
		}
		// IME bar stage: composition box height (the thick clause
		// underline must stay inside the frame). Runs before the ReadLine
		// prompt stage, which claims the same top-left corner, and only
		// after the buffer stage (it starts from a clean canvas).
		if g.bufChecked && g.wFail == "" && !g.imeCompChecked {
			g.imeCompChecked = true
			g.checkIMEComp(screen)
		}
		// Buffer stage: buffer(id, w, h) then gcopy the off-screen image
		// back onto the canvas. Runs after the pixel stage so the earlier
		// stages' paints stay put; probes a free corner (500,100)-(600,180).
		if g.pxChecked && g.wFail == "" && !g.bufChecked {
			g.bufChecked = true
			g.checkBuffer(screen)
		}
		// Editor stage: hold an active ReadLine prompt and hash its row
		// every frame. A steady prompt yields exactly one distinct hash.
		if g.imeCompChecked && g.wFail == "" {
			if !g.edLaunched {
				g.edLaunched = true
				go func() { _, _ = g.b.ReadLine("name? ") }()
				g.b.mu.Lock()
				// curY is the pixel origin of the prompt line now.
				g.edRow = g.b.curY
				g.b.mu.Unlock()
			} else if g.edFrames < 90 {
				g.edHashes = append(g.edHashes, hashRow(screen, int(g.edRow)))
				g.edFrames++
			} else if !g.edMeasured {
				g.edMeasured = true
				seen := map[uint64]bool{}
				for _, h := range g.edHashes {
					seen[h] = true
				}
				g.edDistinct = len(seen)
				// Resize stage last: it grows the canvas, so it must not
				// disturb the row bands the editor stage hashed.
				g.checkResize(screen)
				g.done = true
			}
		}
		if g.frames > 1200 && !g.done {
			// Preserve the first failure: overwriting would mask it.
			if g.wFail == "" {
				g.wFail = "widget stage timed out"
			}
			g.done = true
		}
	}
}

// hashRow hashes a 20px text band starting at pixel y0 (every 4th x,
// every 2nd y) via FNV-1a. y0 is clamped into the screen so a cursor far
// down the canvas (or off it) cannot read out of bounds.
func hashRow(screen *ebiten.Image, y0 int) uint64 {
	const basis, prime = uint64(14695981039346656037), uint64(1099511628211)
	h := basis
	if y0 < 0 {
		y0 = 0
	}
	if y0 > 480-20 {
		y0 = 480 - 20
	}
	for y := y0; y < y0+20; y += 2 {
		for x := 0; x < 640; x += 4 {
			r, gg, bl, _ := screen.At(x, y).RGBA()
			h ^= uint64(r>>8) + 1
			h *= prime
			h ^= uint64(gg>>8) + 1
			h *= prime
			h ^= uint64(bl>>8) + 1
			h *= prime
		}
	}
	return h
}

// makeWidgets exercises widget creation like a script would.
func (g *renderGame) makeWidgets() {
	defer close(g.wPhase1)
	if err := g.b.AddButton(11, "T", 10, 420, 100, 36, 0); err != nil {
		g.wErrs = append(g.wErrs, "button: "+err.Error())
	}
	if err := g.b.AddInput(12, 120, 420, 160, 36, "init"); err != nil {
		g.wErrs = append(g.wErrs, "input: "+err.Error())
	}
	if err := g.b.AddList(13, 290, 380, 150, 80, []string{"a", "b"}); err != nil {
		g.wErrs = append(g.wErrs, "list: "+err.Error())
	}
	t, err := g.b.InputText(12)
	if err != nil {
		g.wErrs = append(g.wErrs, "gettext: "+err.Error())
	}
	g.wText = t
	if err := g.b.AddCheck(21, "Agree", 10, 300, 200, 32, true); err != nil {
		g.wErrs = append(g.wErrs, "chkbox: "+err.Error())
	}
	if err := g.b.AddCombo(22, 220, 300, 200, 36, []string{"x", "y"}, 1); err != nil {
		g.wErrs = append(g.wErrs, "combox: "+err.Error())
	}
	if err := g.b.AddArea(23, 430, 300, 190, 60, "l1"); err != nil {
		g.wErrs = append(g.wErrs, "mesbox: "+err.Error())
	}
}

// checkSaved decodes the pngsave snapshot (game thread; pure Go decode)
// and verifies buffer 0 round-tripped: size plus the stage-7 shapes.
func (g *renderGame) checkSaved() {
	if g.saveErr != nil {
		g.wFail = "pngsave: " + g.saveErr.Error()
		return
	}
	f, err := os.Open(g.savePath)
	if err != nil {
		g.wFail = "pngsave missing: " + err.Error()
		return
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		g.wFail = "pngsave decode: " + err.Error()
		return
	}
	if w, h := img.Bounds().Dx(), img.Bounds().Dy(); w != 640 || h != 480 {
		g.wFail = fmt.Sprintf("pngsave size = %dx%d, want 640x480", w, h)
		return
	}
	r, _, _, _ := img.At(310, 110).RGBA()
	if r < 0x8000 {
		g.wFail = "pngsave lost the red rect"
		return
	}
	_, gg, _, _ := img.At(100, 300).RGBA()
	if gg < 0x8000 {
		g.wFail = "pngsave lost the green circle"
	}
}

// checkZoom draws a 20px red square on buffer 1, blits it 2x onto
// buffer 0, redraws, and verifies scaled pixels (game thread).
// Destination avoids widget rects (ui draws over the canvas).
func (g *renderGame) checkZoom(screen *ebiten.Image) {
	b := g.b
	b.SelectTarget(1)
	b.FillRect(0, 0, 20, 20, [4]int{255, 0, 0, 255})
	b.drainQueue()
	b.SelectTarget(0)
	if err := b.BlitScaled(1, 0, 0, 20, 20, 2, 2, 460, 200); err != nil {
		g.wFail = "gzoom: " + err.Error()
		return
	}
	b.drainQueue()
	b.Draw(screen)
	if r, _, _, _ := screen.At(470, 210).RGBA(); r < 0x8000 {
		g.wFail = "gzoom pixel not scaled"
		return
	}
	// Outside the target the canvas is transparent, so the white window
	// shows through; anything darker leaked.
	if r, gg, bl, _ := screen.At(455, 195).RGBA(); r < 0xFFFF || gg < 0xFFFF || bl < 0xFFFF {
		g.wFail = "gzoom leaked outside the target rect"
	}
}

// checkPaint fills a white square red and verifies fill plus no leak
// (game thread). The square avoids widget rects (ui draws over canvas).
func (g *renderGame) checkPaint(screen *ebiten.Image) {
	b := g.b
	b.FillRect(460, 360, 40, 40, [4]int{255, 255, 255, 255})
	b.drainQueue()
	if err := b.FloodFill(480, 380, [4]int{255, 0, 0, 255}); err != nil {
		g.wFail = "paint: " + err.Error()
		return
	}
	b.drainQueue()
	b.Draw(screen)
	if r, gg, bl, _ := screen.At(480, 380).RGBA(); r < 0x8000 || gg > 0 || bl > 0 {
		g.wFail = "paint fill missing"
		return
	}
	// Outside the region the canvas is transparent (white window).
	if r, gg, bl, _ := screen.At(455, 395).RGBA(); r < 0xFFFF || gg < 0xFFFF || bl < 0xFFFF {
		g.wFail = "paint leaked outside the region"
	}
}

// removeWidgets runs after phase-1 checks.
func (g *renderGame) removeWidgets() {
	defer close(g.wPhase2)
	if err := g.b.RemoveWidget(11); err != nil {
		g.wErrs = append(g.wErrs, "remove: "+err.Error())
	}
}

// checkWidgetsPhase1 runs on the game thread (mutex-only calls safe here).
func (g *renderGame) checkWidgetsPhase1() {
	if len(g.wErrs) > 0 {
		g.wFail = g.wErrs[0]
		return
	}
	if !g.b.HasWidget(11) || !g.b.HasWidget(12) || !g.b.HasWidget(13) {
		g.b.mu.Lock()
		keys := make([]int, 0)
		kinds := map[int]int{}
		for k, v := range g.b.widgets {
			keys = append(keys, k)
			kinds[k] = int(v.kind)
		}
		nkids := 0
		var rects int
		if g.b.root != nil {
			nkids = len(g.b.root.Children())
		}
		if g.b.fixLayout != nil {
			rects = len(g.b.fixLayout.rects)
		}
		g.b.mu.Unlock()
		g.wFail = fmt.Sprintf("widgets should exist (keys=%v kinds=%v kids=%d rects=%d errs=%v text=%q)",
			keys, kinds, nkids, rects, g.wErrs, g.wText)
		return
	}
	if g.wText != "init" {
		g.wFail = "gettext mismatch: " + g.wText
		return
	}
	if !g.b.HasWidget(21) || !g.b.HasWidget(22) || !g.b.HasWidget(23) {
		g.wFail = "form widgets should exist"
		return
	}
	if p, err := g.b.Checked(21); err != nil || !p {
		g.wFail = "fresh checkbox must be checked"
		return
	}
	if idx, err := g.b.SelectedIndex(22); err != nil || idx != 1 {
		g.wFail = "fresh combo selection must be 1"
		return
	}
	if s, err := g.b.AreaText(23); err != nil || s != "l1" {
		g.wFail = "mesbox text mismatch: " + s
		return
	}
	if p, err := g.b.Pressed(11); err != nil || p {
		g.wFail = "fresh button must be unpressed"
		return
	}
	if _, err := g.b.Pressed(12); err == nil {
		g.wFail = "pressed on inputbox should error"
		return
	}
	idx, err := g.b.SelectedIndex(13)
	if err != nil || idx != -1 {
		g.wFail = "fresh list selection must be -1"
		return
	}
}

// checkWidgetsPhase2 runs on the game thread after removal.
func (g *renderGame) checkWidgetsPhase2() {
	if len(g.wErrs) > 0 && g.wFail == "" {
		g.wFail = g.wErrs[0]
		return
	}
	if g.b.HasWidget(11) {
		g.wFail = "button 11 should be removed"
		return
	}
	if _, err := g.b.Pressed(11); err == nil {
		g.wFail = "pressed on removed button should error"
		return
	}
	if _, err := g.b.SelectedIndex(99); err == nil {
		g.wFail = "selected on unknown id should error"
		return
	}
}

func (g *renderGame) drawDirectText(screen *ebiten.Image) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(0, g.face.Metrics().HAscent)
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
	text.Draw(screen, "Hello", g.face, op)
}

func countLit(screen *ebiten.Image, x0, y0, x1, y1 int) int {
	lit := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, gg, bl, _ := screen.At(x, y).RGBA()
			if r > 0 || gg > 0 || bl > 0 {
				lit++
			}
		}
	}
	return lit
}

// countDark counts near-black pixels: with a white window, backend ink
// (default black text) shows as dark-on-light, the inverse of countLit.
func countDark(screen *ebiten.Image, x0, y0, x1, y1 int) int {
	dark := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, gg, bl, _ := screen.At(x, y).RGBA()
			if r < 0x8000 && gg < 0x8000 && bl < 0x8000 {
				dark++
			}
		}
	}
	return dark
}

// countWhite counts near-white (glyph) pixels in a rect, so a probe can
// tell text drawn over a colored shape from the shape itself.
func countWhite(screen *ebiten.Image, x0, y0, x1, y1 int) int {
	lit := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, gg, bl, _ := screen.At(x, y).RGBA()
			if r > 0x8000 && gg > 0x8000 && bl > 0x8000 {
				lit++
			}
		}
	}
	return lit
}

// checkTextLayer verifies in-loop that mes()/print() pixels live in the
// same canvas layer as boxf()/pset(): the call order decides which one
// covers the other. The probe uses a screen area (440,100)-(480,120) free
// of the shapes and widgets the other stages touch.
func (g *renderGame) checkTextLayer(screen *ebiten.Image) {
	b := g.b
	const x0, y0 = 440, 80 // probe area (away from shapes and widgets)
	x1, y1 := x0+40, y0+20
	// Explicit white ink: the probe tests layering (call order), not the
	// default draw color, so it must not depend on the fg default.
	b.SetColor(255, 255, 255, 255)
	// boxf() clears the probe area, then mes() paints onto it.
	b.FillRect(x0, y0, x1-x0, y1-y0, [4]int{0, 0, 0, 255})
	b.mu.Lock()
	b.curX, b.curY = float64(x0), float64(y0)
	b.mu.Unlock()
	b.Println("HHHH", 0)
	b.drainQueue()
	b.Draw(screen)
	g.textAlone = countLit(screen, x0, y0, x1, y1)
	// A later boxf() covers the text: one layer, call order wins.
	b.FillRect(x0, y0, x1-x0, y1-y0, [4]int{0, 0, 0, 255})
	b.drainQueue()
	b.Draw(screen)
	g.textCovered = countLit(screen, x0, y0, x1, y1)
	// Text after a shape draws on top of it (white glyphs over green).
	b.FillRect(x0, y0, x1-x0, y1-y0, [4]int{0, 128, 0, 255})
	b.mu.Lock()
	b.curX, b.curY = float64(x0), float64(y0)
	b.mu.Unlock()
	b.Println("HHHH", 0)
	b.drainQueue()
	b.Draw(screen)
	g.textAlone = countLit(screen, x0, y0, x1, y1)
	// A later boxf() covers the text: one layer, call order wins.
	b.FillRect(x0, y0, x1-x0, y1-y0, [4]int{0, 0, 0, 255})
	b.drainQueue()
	b.Draw(screen)
	g.textCovered = countLit(screen, x0, y0, x1, y1)
	// Text after a shape draws on top of it (white glyphs over green).
	b.FillRect(x0, y0, x1-x0, y1-y0, [4]int{0, 128, 0, 255})
	b.mu.Lock()
	b.curX, b.curY = float64(x0), float64(y0)
	b.mu.Unlock()
	b.Println("HHHH", 0)
	b.drainQueue()
	b.Draw(screen)
	g.textOn = countWhite(screen, x0, y0, x1, y1)
}

// checkBuffer exercises buffer(id, w, h) end to end inside the loop
// (pixel readback needs the game running): create an off-screen buffer,
// draw into it through gsel, then gcopy it back to the canvas and probe
// the pixels.
func (g *renderGame) checkBuffer(screen *ebiten.Image) {
	b := g.b
	if err := b.NewBuffer(2, 100, 80); err != nil {
		g.wFail = "buffer: " + err.Error()
		return
	}
	if !b.hasBuf(2) {
		g.wFail = "buffer(2, ...) did not allocate id 2"
		return
	}
	b.SelectTarget(2)
	b.FillRect(0, 0, 100, 80, [4]int{255, 0, 0, 255})
	b.SelectTarget(0)
	b.drainQueue()
	if err := b.Blit(2, 0, 0, 100, 80, 500, 100); err != nil {
		g.wFail = "gcopy from buffer 2: " + err.Error()
		return
	}
	b.drainQueue()
	b.Draw(screen)
	if r, gg, bl, _ := screen.At(520, 140).RGBA(); r < 0x8000 || gg > 0 || bl > 0 {
		g.wFail = "gcopy from the buffer() image is missing"
		return
	}
	// Outside the blit the canvas stays as it was (transparent here,
	// so the white window shows through).
	if r, gg, bl, _ := screen.At(480, 140).RGBA(); r < 0xFFFF || gg < 0xFFFF || bl < 0xFFFF {
		g.wFail = "gcopy from the buffer() image leaked"
	}
}

// checkIMEComp locks the composition bar's height on the game thread
// (pixel readback needs the loop): the box is one text line plus 2px, so
// the frame still runs past the line and the row below it is empty,
// keeping the 3px clause underline inside the frame.
func (g *renderGame) checkIMEComp(screen *ebiten.Image) {
	b := g.b
	b.Clear() // clean canvas so the probe sees only the bar
	b.mu.Lock()
	b.imeComposing = "あいう"
	b.imeClause = "い"
	b.imeClauseStart, b.imeClauseEnd = 1, 2
	lh := b.lineH
	b.mu.Unlock()
	b.drainQueue()
	b.Draw(screen)
	b.mu.Lock()
	b.imeComposing = ""
	b.imeClause = ""
	b.imeClauseStart, b.imeClauseEnd = 0, 0
	b.mu.Unlock()
	// bh = lineH + 2. The composition text ("あいう", clause "い") is
	// 3 full-width cells wide, so its right frame column sits just past
	// the last glyph: the frame still runs at lineH, its bottom edge is
	// at lineH+1 (it would sit at lineH-1 without the extra 2px, where
	// the 3px clause underline overhangs the frame), and nothing is lit
	// below that. White window: the frame is gray (darker than the
	// background), and below the bar is plain background white.
	tw, _ := text.Measure("あいう", b.face, 0)
	probeX := int(tw+2*2) - 1
	if r, _, _, _ := screen.At(probeX, int(lh)).RGBA(); r == 0xFFFF {
		g.wFail = fmt.Sprintf("IME bar frame missing at y=%d", int(lh))
		return
	}
	if r, _, _, _ := screen.At(probeX, int(lh)+1).RGBA(); r == 0xFFFF {
		g.wFail = fmt.Sprintf("IME bar has no bottom frame at y=%d: the bar is only lineH tall", int(lh)+1)
		return
	}
	if r, gg, bl, _ := screen.At(probeX, int(lh)+2).RGBA(); r < 0xFFFF || gg < 0xFFFF || bl < 0xFFFF {
		g.wFail = fmt.Sprintf("IME bar leaks past y=%d", int(lh)+2)
	}
}

// checkResize locks the resizable() drawing-resolution contract inside
// the loop (ReadPixels is only legal with a game running): a painted
// probe survives the window-size Layout top-left aligned, the canvas
// adopts the window size, and the newly exposed strip stays transparent.
func (g *renderGame) checkResize(screen *ebiten.Image) {
	b := g.b
	g.resized = true
	b.FillRect(100, 100, 40, 40, [4]int{255, 255, 255, 255})
	b.drainQueue()
	b.Draw(screen)
	if r, _, _, _ := screen.At(120, 120).RGBA(); r < 0x8000 {
		g.resizeFail = "resize probe paint missing"
		return
	}
	b.SetResizable(true)
	if w, h := b.Layout(800, 600); w != 800 || h != 600 {
		g.resizeFail = fmt.Sprintf("Layout = %dx%d, want 800x600 (window size)", w, h)
		return
	}
	b.drainQueue()
	b.Draw(screen)
	img := b.targets[0]
	if dx, dy := img.Bounds().Dx(), img.Bounds().Dy(); dx != 800 || dy != 600 {
		g.resizeFail = fmt.Sprintf("canvas = %dx%d, want 800x600", dx, dy)
		return
	}
	if r, _, _, _ := img.At(120, 120).RGBA(); r < 0x8000 {
		g.resizeFail = "resize lost the previous pixels"
		return
	}
	if r, _, _, _ := img.At(799, 599).RGBA(); r != 0 {
		g.resizeFail = "newly exposed strip should stay transparent"
		return
	}
	b.SetResizable(false)
	if w, h := b.Layout(1024, 768); w != 800 || h != 600 {
		g.resizeFail = fmt.Sprintf("frozen Layout = %dx%d, want 800x600", w, h)
	}
}

func (g *renderGame) Layout(outsideWidth, outsideHeight int) (int, int) {
	return 640, 480
}

// pumpUntil drains the draw queue until done closes (headless stand-in
// for the game loop's Update).
func pumpUntil(b *WindowBackend, done chan struct{}, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		b.drainQueue()
		select {
		case <-done:
			b.drainQueue()
			return true
		default:
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
}

// TestInputTextNoStall: gettext must not block on queue drain. A blocking
// read stalls cls/mes redraws mid-frame, which flickered the
// mes("input="+gettext(id)) line in the widgets demo.
func TestInputTextNoStall(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		addErr = b.AddInput(12, 120, 420, 160, 36, "にほんご")
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddInput never completed")
	}
	if addErr != nil {
		t.Fatalf("AddInput: %v", addErr)
	}
	// No drain from here on: if InputText still enqueues, this times out.
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := b.InputText(12)
		ch <- res{s, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("InputText: %v", r.err)
		}
		if r.s != "にほんご" {
			t.Fatalf("InputText = %q, want %q", r.s, "にほんご")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("InputText blocked waiting for queue drain (flicker hazard)")
	}
}

// TestInputTextJapaneseRefresh: game-thread edits surface immediately
// (the custom editor state is the source of truth; no mirror needed).
func TestInputTextJapaneseRefresh(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	go func() {
		_ = b.AddInput(12, 120, 420, 160, 36, "にほんご")
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddInput never completed")
	}
	set := make(chan struct{})
	b.enqueue(func() {
		b.mu.Lock()
		e := b.widgets[12]
		e.edit.text = []rune("日本語テスト")
		e.edit.caret = len(e.edit.text)
		b.mu.Unlock()
		close(set)
	})
	if !pumpUntil(b, set, 10*time.Second) {
		t.Fatal("edit never applied")
	}
	s, err := b.InputText(12)
	if err != nil {
		t.Fatalf("InputText: %v", err)
	}
	if s != "日本語テスト" {
		t.Fatalf("InputText = %q, want %q", s, "日本語テスト")
	}
	if _, err := b.InputText(99); err == nil {
		t.Fatal("InputText on unknown id should error")
	}
}

// TestInputEditOps checks caret editing ops without a game loop.
func TestInputEditOps(t *testing.T) {
	e := &inputState{text: []rune("abc"), caret: 3}
	e.insert([]rune("で"))
	if string(e.text) != "abcで" || e.caret != 4 {
		t.Fatalf("insert = %q/%d", string(e.text), e.caret)
	}
	e.moveLeft()
	e.moveLeft()
	e.backspace()
	if string(e.text) != "acで" || e.caret != 1 {
		t.Fatalf("backspace = %q/%d", string(e.text), e.caret)
	}
	e.del()
	if string(e.text) != "aで" || e.caret != 1 {
		t.Fatalf("del = %q/%d", string(e.text), e.caret)
	}
	e.moveRight()
	e.moveRight()
	e.moveRight() // clamps at the end
	if e.caret != 2 {
		t.Fatalf("caret = %d, want 2", e.caret)
	}
	e.backspace()
	e.backspace()
	e.backspace() // clamps at the start
	if string(e.text) != "" || e.caret != 0 {
		t.Fatalf("clear = %q/%d", string(e.text), e.caret)
	}
}

// TestInputRepFire checks held-key repeat gating without a game loop.
func TestInputRepFire(t *testing.T) {
	e := &inputState{}
	if e.repFire(ebiten.KeyBackspace, false, false, 0) {
		t.Fatal("released should not fire")
	}
	if !e.repFire(ebiten.KeyBackspace, true, true, 0) {
		t.Fatal("just-pressed should fire")
	}
	if e.repFire(ebiten.KeyBackspace, false, true, 10) {
		t.Fatal("hold below delay should not fire")
	}
	if !e.repFire(ebiten.KeyBackspace, false, true, repDelay) {
		t.Fatal("delay should refire")
	}
	if !e.repFire(ebiten.KeyBackspace, false, true, repDelay+repInterval) {
		t.Fatal("interval should refire")
	}
	if e.repFire(ebiten.KeyBackspace, false, false, repDelay+repInterval+1) {
		t.Fatal("release should not fire")
	}
	if !e.repFire(ebiten.KeyBackspace, true, true, 1000) {
		t.Fatal("re-press should fire at once")
	}
}

// TestInputFocusModel checks click focus/blur without a game loop.
func TestInputFocusModel(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	go func() {
		_ = b.AddInput(1, 0, 0, 100, 30, "a")
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddInput never completed")
	}
	b.mu.Lock()
	e := b.widgets[1]
	if e == nil || e.edit == nil {
		b.mu.Unlock()
		t.Fatal("entry missing")
	}
	e.edit.focused = true
	if got := b.focusedLocked(); got != e.edit {
		b.mu.Unlock()
		t.Fatal("focusedLocked should find the editor")
	}
	b.blurEditsLocked()
	if b.focusedLocked() != nil {
		b.mu.Unlock()
		t.Fatal("blur should clear focus")
	}
	b.mu.Unlock()
	if got, err := b.InputText(1); err != nil || got != "a" {
		t.Fatalf("InputText = %q, %v", got, err)
	}
}

// repeat runs (go test -count=N) skip instead of panicking.
// TestListEntryColorsComplete: every list state needs a background.
// A nil Pressed/Hover background renders transparent, which hid the
// highlight while the mouse button was held (selected() had flipped,
// but blue only appeared on release).
func TestListEntryColorsComplete(t *testing.T) {
	c := listEntryColors()
	type named struct {
		name string
		v    interface {
			RGBA() (uint32, uint32, uint32, uint32)
		}
	}
	nils := []string{}
	for _, e := range []named{
		{"Unselected", c.Unselected},
		{"Selected", c.Selected},
		{"DisabledUnselected", c.DisabledUnselected},
		{"DisabledSelected", c.DisabledSelected},
		{"SelectingBackground", c.SelectingBackground},
		{"SelectedBackground", c.SelectedBackground},
		{"FocusedBackground", c.FocusedBackground},
		{"SelectingFocusedBackground", c.SelectingFocusedBackground},
		{"SelectedFocusedBackground", c.SelectedFocusedBackground},
		{"DisabledSelectedBackground", c.DisabledSelectedBackground},
	} {
		if e.v == nil {
			nils = append(nils, e.name)
		}
	}
	if len(nils) > 0 {
		t.Fatalf("nil entry colors (transparent states): %v", nils)
	}
}

// TestListEntryTextPos pins the shared entry alignment: left/top,
// matching the hand-drawn outline in the Golash editor. Centering adds
// a second int() rounding on top of the scroll offset's (glyph origin
// = int((rectH - lineH) / 2)), which can leave row baselines a pixel
// apart while a list scrolls (it reads as jitter).
func TestListEntryTextPos(t *testing.T) {
	h, v := listEntryTextPos()
	if h != widget.TextPositionStart {
		t.Fatalf("horizontal = %v, want TextPositionStart", h)
	}
	if v != widget.TextPositionStart {
		t.Fatalf("vertical = %v, want TextPositionStart", v)
	}
}

// TestWidgetFocusPressColors pins the focus/pressed theme mapping:
// selecting/selected list entries use the pressed blue, the focused
// (unselected) entry the focus pale blue, and the custom editor face
// follows the same pairs (disabled wins over focus). Pure values, so
// headless-safe (button/checkbox art shares these vars by
// construction; their faces are ebiten images, not pixel-testable
// without a loop).
func TestWidgetFocusPressColors(t *testing.T) {
	c := listEntryColors()
	for _, tc := range []struct {
		name string
		got  interface {
			RGBA() (uint32, uint32, uint32, uint32)
		}
		want color.NRGBA
	}{
		{"SelectingBackground", c.SelectingBackground, themePressBg},
		{"SelectedBackground", c.SelectedBackground, themePressBg},
		{"FocusedBackground", c.FocusedBackground, themeFocusBg},
		{"SelectingFocusedBackground", c.SelectingFocusedBackground, themePressBg},
		{"SelectedFocusedBackground", c.SelectedFocusedBackground, themePressBg},
	} {
		if got, ok := tc.got.(color.NRGBA); !ok || got != tc.want {
			t.Fatalf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	for _, tc := range []struct {
		name            string
		focused, dis    bool
		wantBg, wantFrm color.NRGBA
	}{
		{"idle", false, false, themeWhite, themeFrame},
		{"focused", true, false, themeFocusBg, themeFocusFrame},
		{"disabled", false, true, themeFaceDis, themeFrame},
		{"focused+disabled", true, true, themeFaceDis, themeFrame},
	} {
		if bg, fr := inputFaceColors(tc.focused, tc.dis); bg != tc.wantBg || fr != tc.wantFrm {
			t.Fatalf("inputFaceColors(%v, %v) = %v/%v, want %v/%v",
				tc.focused, tc.dis, bg, fr, tc.wantBg, tc.wantFrm)
		}
	}
}

// TestSavePNGErrors covers pngsave validation without a game loop
// (all paths return before touching the loop).
func TestSavePNGErrors(t *testing.T) {
	b := mustNew(t)
	for _, tc := range []struct {
		path string
		id   int
	}{
		{"x.png", -1},
		{"x.png", 99},
		{"", 0},
		{"x.png", 3}, // never allocated
	} {
		if err := b.SavePNG(tc.path, tc.id); err == nil {
			t.Fatalf("SavePNG(%q, %d) should error", tc.path, tc.id)
		}
	}
}

// TestBlitScaledErrors covers gzoom validation without a game loop
// (all paths return before enqueueing).
func TestBlitScaledErrors(t *testing.T) {
	b := mustNew(t)
	for _, tc := range []struct {
		src, w, h int
		zx, zy    float64
	}{
		{99, 10, 10, 1, 1}, // unknown buffer
		{0, 0, 10, 1, 1},
		{0, 10, -1, 1, 1},
		{0, 10, 10, 0, 1},
		{0, 10, 10, 1, -2},
	} {
		if err := b.BlitScaled(tc.src, 0, 0, tc.w, tc.h, tc.zx, tc.zy, 0, 0); err == nil {
			t.Fatalf("BlitScaled(%+v) should error", tc)
		}
	}
}

// TestFlood fills a square without leaking through the background.
func TestFlood(t *testing.T) {
	w, h := 8, 8
	px := make([]byte, 4*w*h)
	set := func(x, y int, c [4]byte) {
		i := (y*w + x) * 4
		px[i], px[i+1], px[i+2], px[i+3] = c[0], c[1], c[2], c[3]
	}
	at := func(x, y int) [4]byte {
		i := (y*w + x) * 4
		return [4]byte{px[i], px[i+1], px[i+2], px[i+3]}
	}
	white := [4]byte{0xFF, 0xFF, 0xFF, 0xFF}
	red := color.NRGBA{0xFF, 0, 0, 0xFF}
	for y := 2; y < 6; y++ {
		for x := 2; x < 6; x++ {
			set(x, y, white)
		}
	}
	if !flood(px, w, h, 3, 3, red) {
		t.Fatal("fill should report a change")
	}
	for _, p := range [][2]int{{2, 2}, {3, 3}, {5, 5}, {2, 5}} {
		if got := at(p[0], p[1]); got != [4]byte{0xFF, 0, 0, 0xFF} {
			t.Fatalf("pixel %v = %v, want red", p, got)
		}
	}
	for _, p := range [][2]int{{1, 1}, {6, 3}, {0, 7}, {3, 6}} {
		if got := at(p[0], p[1]); got != [4]byte{} {
			t.Fatalf("pixel %v = %v, want transparent", p, got)
		}
	}
	if flood(px, w, h, 3, 3, red) {
		t.Fatal("seed already in fill color should be a no-op")
	}
}

// TestFloodEnclosed keeps the fill inside a 1px border.
func TestFloodEnclosed(t *testing.T) {
	w, h := 8, 8
	px := make([]byte, 4*w*h)
	set := func(x, y int, c [4]byte) {
		i := (y*w + x) * 4
		px[i], px[i+1], px[i+2], px[i+3] = c[0], c[1], c[2], c[3]
	}
	at := func(x, y int) [4]byte {
		i := (y*w + x) * 4
		return [4]byte{px[i], px[i+1], px[i+2], px[i+3]}
	}
	black := [4]byte{0, 0, 0, 0xFF}
	white := [4]byte{0xFF, 0xFF, 0xFF, 0xFF}
	for x := 2; x < 6; x++ {
		set(x, 2, black)
		set(x, 5, black)
		set(2, x, black)
		set(5, x, black)
	}
	set(3, 3, white)
	set(4, 4, white)
	red := color.NRGBA{0xFF, 0, 0, 0xFF}
	if !flood(px, w, h, 3, 3, red) {
		t.Fatal("fill should report a change")
	}
	if got := at(3, 3); got != [4]byte{0xFF, 0, 0, 0xFF} {
		t.Fatalf("interior = %v, want red", got)
	}
	if got := at(4, 4); got != white {
		t.Fatalf("disconnected interior = %v, want white", got)
	}
	for _, p := range [][2]int{{2, 3}, {3, 2}, {5, 4}} {
		if got := at(p[0], p[1]); got != black {
			t.Fatalf("border %v = %v, want black", p, got)
		}
	}
	if got := at(1, 1); got != [4]byte{} {
		t.Fatalf("exterior = %v, want transparent", got)
	}
}

// TestFloodFillBounds covers paint seed validation without a game loop.
func TestFloodFillBounds(t *testing.T) {
	b := mustNew(t)
	if err := b.FloodFill(-1, 0, [4]int{255, 0, 0, 255}); err == nil {
		t.Fatal("negative seed should error")
	}
	if err := b.FloodFill(0, 480, [4]int{255, 0, 0, 255}); err == nil {
		t.Fatal("out-of-range seed should error")
	}
	// A valid seed only enqueues (applied by Update), so no loop needed.
	if err := b.FloodFill(10, 10, [4]int{255, 0, 0, 255}); err != nil {
		t.Fatalf("valid seed: %v", err)
	}
}

// TestResizableFollowsWindow locks the resizable() size contract without
// a game loop: with resizing forbidden Layout pins the screen() logical
// size, with resizing allowed Layout reports the window size and the main
// buffer is swapped to it, and forbidding it again freezes the current
// resolution. (Pixel preservation across the swap needs a running loop,
// so it is covered by the resize stage in TestGuiRender.)
func TestResizableFollowsWindow(t *testing.T) {
	b := mustNew(t)
	// Pinned: Layout ignores the window size and keeps the screen() size.
	if w, h := b.Layout(320, 240); w != 640 || h != 480 {
		t.Fatalf("Layout = %dx%d, want 640x480 (screen() size)", w, h)
	}
	b.SetResizable(true)
	if w, h := b.Layout(800, 600); w != 800 || h != 600 {
		t.Fatalf("Layout = %dx%d, want 800x600 (window size)", w, h)
	}
	b.mu.Lock()
	w, h := b.w, b.h
	b.mu.Unlock()
	if w != 800 || h != 600 {
		t.Fatalf("canvas = %dx%d, want 800x600", w, h)
	}
	b.enqueue(func() {
		if dx, dy := b.targets[0].Bounds().Dx(), b.targets[0].Bounds().Dy(); dx != 800 || dy != 600 {
			t.Fatalf("main buffer = %dx%d, want 800x600", dx, dy)
		}
	})
	b.drainQueue()
	// Forbidden again: the current resolution freezes.
	b.SetResizable(false)
	if w, h := b.Layout(1024, 768); w != 800 || h != 600 {
		t.Fatalf("Layout = %dx%d, want 800x600 frozen", w, h)
	}
}

// TestTabFocusDisabled pins ensureUI: Tab/Shift+Tab must not move widget
// focus (ebitenui's default traversal), so Tab stays a key input (keypush
// 9, the custom editor's tab insert). Click focus and programmatic
// Focus() are unaffected — only the default Tab handling is gated.
func TestTabFocusDisabled(t *testing.T) {
	b := mustNew(t)
	b.ensureUI()
	if b.ui == nil {
		t.Fatal("ensureUI built no UI")
	}
	if !b.ui.DisableDefaultFocus {
		t.Fatal("UI allows Tab focus traversal, want it disabled")
	}
}

// TestWhiteWindowDefaults pins the light-theme defaults without a game
// loop: the initial draw color is opaque black (white window), and
// color() (ResetColor) returns to black, not white. CUI is unaffected
// (ANSI reset there).
func TestWhiteWindowDefaults(t *testing.T) {
	b := mustNew(t)
	black := color.NRGBA{0x00, 0x00, 0x00, 0xFF}
	b.mu.Lock()
	fg := b.fg
	b.mu.Unlock()
	if fg != black {
		t.Fatalf("initial fg = %v, want %v", fg, black)
	}
	b.SetColor(255, 0, 0, 255)
	b.ResetColor()
	b.mu.Lock()
	fg = b.fg
	b.mu.Unlock()
	if fg != black {
		t.Fatalf("fg after ResetColor = %v, want %v", fg, black)
	}
}

// TestNewBuffer covers buffer(id, w, h) without a game loop: the id is
// marked allocated, the image lands on the game thread at exactly that
// size, and a bad id or size fails without touching the pool.
func TestNewBuffer(t *testing.T) {
	b := mustNew(t)
	if err := b.NewBuffer(1, 320, 240); err != nil {
		t.Fatalf("NewBuffer(1, 320, 240): %v", err)
	}
	if !b.hasBuf(1) {
		t.Fatal("buffer 1 not marked allocated")
	}
	b.drainQueue()
	var size [2]int
	b.enqueue(func() {
		img, ok := b.targets[1]
		if !ok || img == nil {
			return
		}
		size = [2]int{img.Bounds().Dx(), img.Bounds().Dy()}
	})
	b.drainQueue()
	if size != [2]int{320, 240} {
		t.Fatalf("buffer 1 size = %v, want [320 240]", size)
	}
	// Replacing is allowed (draw loops re-create a scratch buffer): the
	// new size wins.
	if err := b.NewBuffer(1, 64, 48); err != nil {
		t.Fatalf("NewBuffer(1, 64, 48): %v", err)
	}
	b.drainQueue()
	size = [2]int{}
	b.enqueue(func() {
		if img := b.targets[1]; img != nil {
			size = [2]int{img.Bounds().Dx(), img.Bounds().Dy()}
		}
	})
	b.drainQueue()
	if size != [2]int{64, 48} {
		t.Fatalf("replaced buffer size = %v, want [64 48]", size)
	}
	// Errors: the visible canvas, a bad id, a non-positive size, and one
	// side over the cap. None of them may allocate.
	if err := b.NewBuffer(0, 8, 8); err == nil {
		t.Fatal("buffer(0, ...) should error (the canvas belongs to screen())")
	}
	if err := b.NewBuffer(-1, 8, 8); err == nil {
		t.Fatal("buffer(-1, ...) should error")
	}
	if err := b.NewBuffer(16, 8, 8); err == nil {
		t.Fatal("buffer(16, ...) should error")
	}
	if err := b.NewBuffer(2, 0, 8); err == nil {
		t.Fatal("buffer(2, 0, 8) should error")
	}
	if err := b.NewBuffer(2, 8, -8); err == nil {
		t.Fatal("buffer(2, 8, -8) should error")
	}
	if err := b.NewBuffer(2, MaxImageDim+1, 8); err == nil {
		t.Fatal("oversized buffer should error")
	}
	if b.hasBuf(2) {
		t.Fatal("a failed buffer() must not allocate the id")
	}
}

// TestGetPixelBounds covers pget coordinate validation without a game
// loop (bounds are checked before any game-thread readback).
func TestGetPixelBounds(t *testing.T) {
	b := mustNew(t)
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {640, 0}, {0, 480}, {1000000, 1000000}} {
		if _, err := b.GetPixel(p[0], p[1]); err == nil {
			t.Fatalf("GetPixel%v should error", p)
		}
	}
}

// NOTE: GetPixel round-trip needs a running game loop (ebiten forbids
// readback before it starts), so it is covered by the pget stage in
// TestGuiRender below instead of a headless test.

// TestSetFontSize covers font() state without a game loop
// (face reload is pure Go; no images involved).
func TestSetFontSize(t *testing.T) {
	b := mustNew(t)
	if b.lineH != 16 || b.fontSize != 16 {
		t.Fatalf("defaults = %v/%v, want 16/16", b.lineH, b.fontSize)
	}
	if err := b.SetFontSize(32); err != nil {
		t.Fatalf("SetFontSize(32): %v", err)
	}
	if b.lineH != 32 || b.fontSize != 32 {
		t.Fatalf("scaled = %v/%v, want 32/32", b.lineH, b.fontSize)
	}
	if b.uiFace() == nil {
		t.Fatal("uiFace should follow the resized face")
	}
	// pos() is the pixel origin itself: no cell rounding.
	b.MoveTo(10, 5)
	b.mu.Lock()
	gx, gy := b.gx, b.gy
	tx, ty := b.curX, b.curY
	b.mu.Unlock()
	if gx != 10 || gy != 5 {
		t.Fatalf("graphics cursor = (%d,%d), want (10,5)", gx, gy)
	}
	if tx != 10 || ty != 5 {
		t.Fatalf("text cursor = (%v,%v), want (10,5)", tx, ty)
	}
	for _, size := range []int{-1, 0, 7, 65, 100} {
		if err := b.SetFontSize(size); err == nil {
			t.Fatalf("SetFontSize(%d) should error", size)
		}
	}
}

// TestFontDrawSmoke rasterizes resized text headless through the real
// canvas path (no readback: ReadPixels panics outside the loop, plain
// drawing does not).
func TestFontDrawSmoke(t *testing.T) {
	b := mustNew(t)
	if err := b.SetFontSize(32); err != nil {
		t.Fatal(err)
	}
	d := b.printRaw("Hello, GUI! あいうえお", 0, true)
	if d == nil {
		t.Fatal("printRaw laid out nothing")
	}
	b.applyTextDraw(d)
	b.Draw(ebiten.NewImage(640, 480))
}

// TestFormWidgetsHeadless drives chkbox/combox/mesbox with a manual pump
// (no game loop): creation mirrors plus Update-time refreshes.
func TestFormWidgetsHeadless(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		defer close(done)
		if err := b.AddCheck(21, "Agree", 10, 300, 0, 0, false); err != nil {
			addErr = err
			return
		}
		if err := b.AddCombo(22, 220, 300, 200, 36, []string{"x", "y"}, 0); err != nil {
			addErr = err
			return
		}
		if err := b.AddArea(23, 430, 300, 190, 60, "l1\nl2"); err != nil {
			addErr = err
			return
		}
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("form widget creation never completed")
	}
	if addErr != nil {
		t.Fatalf("add: %v", addErr)
	}
	// Creation-time mirrors.
	if p, err := b.Checked(21); err != nil || p {
		t.Fatalf("Checked(21) = %v, %v; want false", p, err)
	}
	if idx, err := b.SelectedIndex(22); err != nil || idx != 0 {
		t.Fatalf("SelectedIndex(22) = %v, %v; want 0", idx, err)
	}
	if s, err := b.AreaText(23); err != nil || s != "l1\nl2" {
		t.Fatalf("AreaText(23) = %q, %v", s, err)
	}
	// Mutate like the game thread would, then refresh mirrors.
	set := make(chan struct{})
	b.enqueue(func() {
		b.mu.Lock()
		e21, e22, e23 := b.widgets[21], b.widgets[22], b.widgets[23]
		b.mu.Unlock()
		e21.check.SetState(widget.WidgetChecked)
		e22.combo.SetSelectedEntry("y")
		e23.area.SetText("changed")
		close(set)
	})
	if !pumpUntil(b, set, 10*time.Second) {
		t.Fatal("mutation never applied")
	}
	b.syncWidgetCache() // what Update does every tick
	if p, _ := b.Checked(21); !p {
		t.Fatal("Checked(21) should be true after SetState")
	}
	if idx, _ := b.SelectedIndex(22); idx != 1 {
		t.Fatalf("SelectedIndex(22) = %d, want 1", idx)
	}
	if s, _ := b.AreaText(23); s != "changed" {
		t.Fatalf("AreaText(23) = %q, want changed", s)
	}
	// Non-positive w/h auto-sizes from the font and label (pumped:
	// successful adds block on runOnLoop like every other widget).
	auto := make(chan struct{})
	var autoErr error
	go func() {
		defer close(auto)
		autoErr = b.AddCheck(24, "x", 0, 0, 0, 0, false)
	}()
	if !pumpUntil(b, auto, 10*time.Second) {
		t.Fatal("auto-size chkbox never completed")
	}
	if autoErr != nil {
		t.Fatalf("auto-size chkbox: %v", autoErr)
	}
	// placeChild records the rect synchronously (SetLocation itself
	// runs on the next real-loop layout).
	b.mu.Lock()
	r, ok := b.fixLayout.rects[b.widgets[24].child]
	b.mu.Unlock()
	if !ok || r.Dx() <= 0 || r.Dy() <= 0 {
		t.Fatalf("auto-size chkbox rect = %v, want positive size", r)
	}
	// Validation (all return before touching the loop).
	if err := b.AddCombo(24, 0, 0, 100, 30, []string{"a"}, 5); err == nil {
		t.Fatal("combox out-of-range sel should error")
	}
	if err := b.AddArea(24, 0, 0, 100, 0, ""); err == nil {
		t.Fatal("mesbox zero height should error")
	}
	if _, err := b.Checked(99); err == nil {
		t.Fatal("checked unknown should error")
	}
	if _, err := b.AreaText(21); err == nil {
		t.Fatal("getstr on a checkbox should error")
	}
	if _, err := b.SelectedIndex(21); err == nil {
		t.Fatal("selected on a checkbox should error")
	}
	if err := b.SetEnabled(99, true); err == nil {
		t.Fatal("objprm unknown should error")
	}
	// objprm applies on the loop (pumped here manually).
	en := make(chan struct{})
	var enErr error
	go func() {
		defer close(en)
		enErr = b.SetEnabled(21, false)
	}()
	if !pumpUntil(b, en, 10*time.Second) {
		t.Fatal("SetEnabled never applied")
	}
	if enErr != nil {
		t.Fatalf("SetEnabled: %v", enErr)
	}
	b.mu.Lock()
	dis := b.widgets[21].child.GetWidget().Disabled
	b.mu.Unlock()
	if !dis {
		t.Fatal("widget 21 should be disabled")
	}
}

// TestCheckImageSized guards the checkbox-label regression: solid-color
// nine slices report MinSize 0, and ebitenui draws the labeled box at
// checkboxPreferredSize, so the box rendered 0x0 (label text only).
func TestCheckImageSized(t *testing.T) {
	b := mustNew(t)
	img := b.checkImage()
	faces := map[string]interface{ MinSize() (int, int) }{
		"Unchecked":         img.Unchecked,
		"UncheckedHovered":  img.UncheckedHovered,
		"UncheckedDisabled": img.UncheckedDisabled,
		"Checked":           img.Checked,
		"CheckedHovered":    img.CheckedHovered,
		"CheckedDisabled":   img.CheckedDisabled,
	}
	for name, f := range faces {
		if f == nil {
			t.Fatalf("%s face is nil", name)
		}
		if w, h := f.MinSize(); w < 12 || h < 12 {
			t.Fatalf("%s MinSize = %dx%d, want >= 12 (invisible box)", name, w, h)
		}
	}
	// Art content: white body, gray ring, black glyph.
	body := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	fr := color.NRGBA{0x80, 0x80, 0x80, 0xFF}
	mark := color.NRGBA{0x00, 0x00, 0x00, 0xFF}
	plain := checkArtRGBA(20, body, fr, false, mark)
	if got := plain.NRGBAAt(0, 0); got != fr {
		t.Fatalf("unchecked corner = %v, want frame %v", got, fr)
	}
	if got := plain.NRGBAAt(10, 10); got != body {
		t.Fatalf("unchecked center = %v, want body %v", got, body)
	}
	ticked := checkArtRGBA(20, body, fr, true, mark)
	if got := ticked.NRGBAAt(9, 14); got != mark {
		t.Fatalf("checked glyph joint = %v, want mark %v", got, mark)
	}
	// Headless render smoke: labeled checkbox Draw must not panic
	// (plain Draw works without a game loop; readback does not).
	done := make(chan struct{})
	go func() {
		_ = b.AddCheck(21, "Agree", 10, 300, 200, 32, false)
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddCheck never completed")
	}
	b.Draw(ebiten.NewImage(640, 480))
}

// TestComboClickRepro drives the click-time combo path headless:
// open the dropdown, Update, Draw, select an entry, Update again.
// This reproduces the combox click panic without a window.
func TestComboClickRepro(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := b.AddCombo(22, 220, 300, 200, 36, []string{"x", "y", "z"}, 0); err != nil {
			t.Errorf("AddCombo: %v", err)
		}
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddCombo never completed")
	}
	open := make(chan struct{})
	go func() {
		defer close(open)
		b.runOnLoop(func() {
			b.widgets[22].combo.SetContentVisible(true)
		})
	}()
	if !pumpUntil(b, open, 10*time.Second) {
		t.Fatal("SetContentVisible never applied")
	}
	if err := b.Update(); err != nil {
		t.Fatalf("Update with open combo: %v", err)
	}
	b.Draw(ebiten.NewImage(640, 480))
	sel := make(chan struct{})
	go func() {
		defer close(sel)
		b.runOnLoop(func() {
			b.widgets[22].combo.SetSelectedEntry("y")
		})
	}()
	if !pumpUntil(b, sel, 10*time.Second) {
		t.Fatal("SetSelectedEntry never applied")
	}
	if err := b.Update(); err != nil {
		t.Fatalf("Update after select: %v", err)
	}
	b.Draw(ebiten.NewImage(640, 480))
	if idx, err := b.SelectedIndex(22); err != nil || idx != 1 {
		t.Fatalf("SelectedIndex(22) = %d, %v; want 1", idx, err)
	}
	// Edge paths: empty and duplicate items must survive open+select too.
	for _, tc := range []struct {
		id    int
		items []string
		sel   string
		want  int
	}{
		{31, []string{}, "", -1},
		{32, []string{"a", "a", "b"}, "a", 0},
	} {
		d := make(chan struct{})
		id, items := tc.id, tc.items
		go func() {
			defer close(d)
			_ = b.AddCombo(id, 220, 300, 200, 36, items, -1)
		}()
		if !pumpUntil(b, d, 10*time.Second) {
			t.Fatalf("AddCombo(%d) never completed", id)
		}
		o := make(chan struct{})
		go func() {
			defer close(o)
			b.runOnLoop(func() {
				b.widgets[id].combo.SetContentVisible(true)
			})
		}()
		if !pumpUntil(b, o, 10*time.Second) {
			t.Fatalf("SetContentVisible(%d) never applied", id)
		}
		if err := b.Update(); err != nil {
			t.Fatalf("Update with open combo %d: %v", id, err)
		}
		b.Draw(ebiten.NewImage(640, 480))
		if tc.sel != "" {
			s := make(chan struct{})
			go func() {
				defer close(s)
				b.runOnLoop(func() {
					b.widgets[id].combo.SetSelectedEntry(tc.sel)
				})
			}()
			if !pumpUntil(b, s, 10*time.Second) {
				t.Fatalf("SetSelectedEntry(%d) never applied", id)
			}
			if err := b.Update(); err != nil {
				t.Fatalf("Update after select %d: %v", id, err)
			}
		}
		if idx, err := b.SelectedIndex(id); err != nil || idx != tc.want {
			t.Fatalf("SelectedIndex(%d) = %d, %v; want %d", id, idx, err, tc.want)
		}
	}
}

// TestAwaitTick drives the frame counter without a game loop:
// advanceTick is what Update calls, so awaiting it headless matches
// the in-loop behavior.
func TestAwaitTick(t *testing.T) {
	b := mustNew(t)
	if b.Tick() != 0 {
		t.Fatalf("initial tick = %d, want 0", b.Tick())
	}
	b.Await(0)
	b.Await(-5) // no-ops, must return immediately
	done := make(chan struct{})
	go func() {
		b.Await(2)
		close(done)
	}()
	// Generous gaps: wakeups re-register within microseconds.
	for i := 0; i < 3; i++ {
		time.Sleep(20 * time.Millisecond)
		b.advanceTick()
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Await(2) never returned after 3 ticks")
	}
	if b.Tick() != 3 {
		t.Fatalf("tick = %d, want 3", b.Tick())
	}
	// Late waiter with ticks already past returns at once.
	later := make(chan struct{})
	go func() {
		b.Await(1)
		close(later)
	}()
	time.Sleep(50 * time.Millisecond) // let the waiter register
	b.advanceTick()
	select {
	case <-later:
	case <-time.After(10 * time.Second):
		t.Fatal("Await(1) never returned")
	}
}

// TestRotateTransform checks grotate geometry without a game loop:
// the region center lands on the cursor at any angle.
func TestRotateTransform(t *testing.T) {
	m := rotateTransform(20, 10, 1, 1, 0, 100, 50)
	if x, y := m.Apply(10, 5); x != 100 || y != 50 {
		t.Fatalf("center = (%v,%v), want (100,50)", x, y)
	}
	if x, y := m.Apply(0, 0); x != 90 || y != 45 {
		t.Fatalf("corner = (%v,%v), want (90,45)", x, y)
	}
	// 90 degrees: right-middle (20,5) rotates to bottom-middle.
	m = rotateTransform(20, 10, 1, 1, 90, 100, 50)
	if x, y := m.Apply(20, 5); diff(x, 100) || diff(y, 60) {
		t.Fatalf("rotated = (%v,%v), want (100,60)", x, y)
	}
	// Zoom composes: center still lands on the cursor.
	m = rotateTransform(20, 10, 2, 3, 45, 7, 9)
	if x, y := m.Apply(10, 5); diff(x, 7) || diff(y, 9) {
		t.Fatalf("scaled center = (%v,%v), want (7,9)", x, y)
	}
}

func diff(a, b float64) bool {
	d := a - b
	return d > 1e-9 || d < -1e-9
}

// TestBlitRotateErrors covers grotate validation without a game loop.
func TestBlitRotateErrors(t *testing.T) {
	b := mustNew(t)
	for _, tc := range []struct {
		src, w, h int
		zx, zy    float64
	}{
		{99, 10, 10, 1, 1}, // unknown buffer
		{0, 0, 10, 1, 1},
		{0, 10, -1, 1, 1},
		{0, 10, 10, 0, 1},
		{0, 10, 10, 1, -2},
	} {
		if err := b.BlitRotate(tc.src, 0, 0, tc.w, tc.h, 30, tc.zx, tc.zy, 0, 0); err == nil {
			t.Fatalf("BlitRotate(%+v) should error", tc)
		}
	}
}

// TestAlphaState covers galpha without a game loop.
func TestAlphaState(t *testing.T) {
	b := mustNew(t)
	if b.alphaScale() != 1 {
		t.Fatalf("default alpha scale = %v, want 1", b.alphaScale())
	}
	if err := b.SetAlpha(128); err != nil {
		t.Fatalf("SetAlpha(128): %v", err)
	}
	if s := b.alphaScale(); s < 0.5 || s > 0.51 {
		t.Fatalf("alpha scale = %v, want ~0.502", s)
	}
	for _, a := range []int{-1, 256, 1000} {
		if err := b.SetAlpha(a); err == nil {
			t.Fatalf("SetAlpha(%d) should error", a)
		}
	}
	if err := b.SetAlpha(255); err != nil {
		t.Fatalf("SetAlpha(255): %v", err)
	}
}

// TestMouseWheelConsume checks the wheel latch: whole detents are
// reported once, fractions carry over to the next call.
func TestMouseWheelConsume(t *testing.T) {
	b := mustNew(t)
	if got := b.MouseWheel(); got != 0 {
		t.Fatalf("idle wheel = %d, want 0", got)
	}
	b.mu.Lock()
	b.wheelAccum = 2.7
	b.mu.Unlock()
	if got := b.MouseWheel(); got != 2 {
		t.Fatalf("wheel = %d, want 2", got)
	}
	if got := b.MouseWheel(); got != 0 {
		t.Fatalf("second read = %d, want 0 (0.7 carries)", got)
	}
	b.mu.Lock()
	b.wheelAccum += 0.5
	b.mu.Unlock()
	if got := b.MouseWheel(); got != 1 {
		t.Fatalf("carried wheel = %d, want 1", got)
	}
	b.mu.Lock()
	b.wheelAccum = -1.7
	b.mu.Unlock()
	if got := b.MouseWheel(); got != -1 {
		t.Fatalf("negative wheel = %d, want -1", got)
	}
}

// TestWidgetThemeFixed locks the Windows-standard widget theme: every
// widget kind builds headless without a color override API, and the
// checkbox art carries a gray ring on a white box with a black glyph.
// NOTE: defined before TestGuiRender: Ebiten allows a single RunGame per
// process, after which NewImage panics, so image-creating tests must run
// first (like the other widget tests above).
func TestWidgetThemeFixed(t *testing.T) {
	b := mustNew(t)
	white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	fr := color.NRGBA{0x80, 0x80, 0x80, 0xFF}
	mark := color.NRGBA{0x00, 0x00, 0x00, 0xFF}
	plain := checkArtRGBA(20, white, fr, false, mark)
	if got := plain.NRGBAAt(0, 0); got != fr {
		t.Fatalf("unchecked corner = %v, want frame %v", got, fr)
	}
	if got := plain.NRGBAAt(10, 10); got != white {
		t.Fatalf("unchecked center = %v, want body %v", got, white)
	}
	ticked := checkArtRGBA(20, white, fr, true, mark)
	if got := ticked.NRGBAAt(9, 14); got != mark {
		t.Fatalf("checked glyph joint = %v, want mark %v", got, mark)
	}
	if got := ticked.NRGBAAt(0, 0); got != fr {
		t.Fatalf("checked corner = %v, want frame %v", got, fr)
	}
	// Every widget kind builds with the fixed theme (no color args).
	for _, add := range []func() error{
		func() error { return b.AddButton(1, "OK", 10, 10, 120, 40) },
		func() error { return b.AddInput(2, 10, 60, 160, 36, "hi") },
		func() error { return b.AddList(3, 10, 110, 160, 80, []string{"a", "b"}) },
		func() error { return b.AddCheck(4, "agree", 10, 200, 0, 0, true) },
		func() error { return b.AddCombo(5, 10, 240, 160, 36, []string{"x", "y"}, 1) },
		func() error { return b.AddArea(6, 10, 290, 200, 80, "memo") },
	} {
		if err := runWidgetOp(t, b, add); err != nil {
			t.Fatalf("add widget: %v", err)
		}
	}
	// List entry colors stay complete (non-nil backgrounds) in the
	// light theme too.
	c := listEntryColors()
	if c.Unselected != themeInk || c.Selected != themeInk {
		t.Fatalf("entry text = %v/%v, want black", c.Unselected, c.Selected)
	}
	if c.SelectedBackground != themePressBg {
		t.Fatalf("selection bg = %v, want pressed %v", c.SelectedBackground, themePressBg)
	}
}

// TestClearKeepsWidgets: cls() wipes canvas/text only; widgets survive
// until clrobj() (regression: cls used to drop them, breaking every
// draw loop that combined widgets with per-frame cls()).
func TestClearKeepsWidgets(t *testing.T) {
	b := mustNew(t)
	if err := runWidgetOp(t, b, func() error { return b.AddInput(2, 10, 60, 160, 36, "hi") }); err != nil {
		t.Fatalf("AddInput: %v", err)
	}
	b.Clear()
	if !b.HasWidget(2) {
		t.Fatal("cls() must not remove widgets")
	}
	if s, err := b.InputText(2); err != nil || s != "hi" {
		t.Fatalf("InputText after cls() = %q, %v", s, err)
	}
}

// loopRan guards the one-loop-per-process Ebiten limit:
// repeat runs (go test -count=N) skip instead of panicking.
var loopRan = false

// TestGuiRender verifies end-to-end pixel output: environment readback,
// direct text, and the backend path (catches "black screen" regressions).
func TestGuiRender(t *testing.T) {
	if loopRan {
		t.Skip("ebiten allows a single game loop per process")
	}
	loopRan = true
	b := mustNew(t)
	b.Println("Hello, GUI! あいうえお", 0)
	ebiten.SetWindowSize(640, 480)
	ebiten.SetWindowTitle("Golash test")
	g := &renderGame{b: b, face: b.face, savePath: filepath.Join(t.TempDir(), "snap.png")}
	// NOTE: window teardown on some machines reports
	// "glfw: DestroyWindow failed"; sampling results are what matter.
	_ = ebiten.RunGame(g)
	t.Logf("frames=%d filled=%v texted=%v backed=%v", g.frames, g.filled, g.texted, g.backed)
	if !g.filled || g.fillLit == 0 {
		t.Fatal("ReadPixels sees nothing even for Fill: environment issue")
	}
	if !g.texted || g.textLit == 0 {
		t.Fatal("direct text.Draw renders nothing")
	}
	if !g.backed {
		t.Fatal("backend stage never ran")
	}
	t.Logf("fill=%d text=%d backend=%d rect=%d circle=%d any=%d shaped=%v bbox=(%d,%d)-(%d,%d)", g.fillLit, g.textLit, g.backLit, g.rectLit, g.circleLit, g.anyLit, g.shaped, g.minX, g.minY, g.maxX, g.maxY)
	if g.backLit == 0 {
		t.Fatal("no backend pixels in the first text row: backend drew nothing")
	}
	if g.backLit == 320*40 {
		t.Fatal("first text row is all dark: black screen, want a white window")
	}
	if !g.shaped || g.rectLit == 0 {
		t.Fatal("FillRect pixel not visible")
	}
	if g.circleLit == 0 {
		t.Fatal("Circle pixel not visible")
	}
	// Text shares the canvas layer with the shapes: mes() pixels show up
	// in the buffer, a later boxf() covers them, and mes() after boxf()
	// draws on top of the shape.
	t.Logf("text layer: alone=%d covered=%d over-shape=%d", g.textAlone, g.textCovered, g.textOn)
	if g.textAlone == 0 {
		t.Fatal("mes() pixels missing from the canvas layer")
	}
	if g.textCovered != 0 {
		t.Fatalf("boxf() after mes() left %d text pixels: text is not on the canvas layer", g.textCovered)
	}
	if g.textOn == 0 {
		t.Fatal("mes() after boxf() drew nothing on top of the shape")
	}
	if !g.probedInput {
		t.Fatal("input stage never ran")
	}
	if !g.pxChecked {
		t.Fatal("pget stage never ran")
	}
	if g.wFail != "" {
		t.Fatalf("stage failure before completion: %s", g.wFail)
	}
	if !g.bufChecked {
		t.Fatal("buffer stage never ran")
	}
	t.Logf("arrows=%d mouse=(%d,%d) clicked=%v", g.keyIdle, g.mouseX, g.mouseY, g.clickedVal)
	if g.keyIdle != 0 {
		t.Fatalf("idle arrows = %d, want 0", g.keyIdle)
	}
	if g.clickedVal {
		t.Fatal("idle clicked = true, want false")
	}
	// NOTE: the real cursor can sit anywhere on the desktop, even outside
	// the window, so mouse position is logged but not range-checked.
	if g.wFail != "" {
		t.Fatalf("widget stage: %s", g.wFail)
	}
	t.Log("widget stage ok")
	if !g.edMeasured {
		t.Fatal("editor stage never ran")
	}
	t.Logf("editor row=%v frames=%d distinct=%d", g.edRow, g.edFrames, g.edDistinct)
	if g.edDistinct != 1 {
		t.Fatalf("input() prompt flickers: %d distinct frames", g.edDistinct)
	}
	if !g.resized {
		t.Fatal("resize stage never ran")
	}
	if g.resizeFail != "" {
		t.Fatalf("resize stage: %s", g.resizeFail)
	}
}

// runWidgetOp pumps runOnLoop-bound widget ops headless.
func runWidgetOp(t *testing.T, b *WindowBackend, op func() error) error {
	t.Helper()
	ch := make(chan struct{})
	var opErr error
	go func() {
		defer close(ch)
		opErr = op()
	}()
	if !pumpUntil(b, ch, 10*time.Second) {
		t.Fatal("widget op never completed")
	}
	return opErr
}
