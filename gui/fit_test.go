package gui

import (
	"strings"
	"testing"
)

// px is a deterministic stand-in for TextWidth: 8px per rune.
func px(s string) int { return 8 * len([]rune(s)) }

// Pure text fitting (no window needed). The backend-level tests that
// build widgets live in backend_test.go before TestGuiRender: Ebiten
// allows a single game loop per process, after which NewImage panics.

func TestFitTextShortUnchanged(t *testing.T) {
	if got := fitText("abc", 100, px); got != "abc" {
		t.Fatalf("short text changed: %q", got)
	}
	// Non-positive budget disables fitting (keep the text visible).
	if got := fitText("abcdef", 0, px); got != "abcdef" {
		t.Fatalf("maxw 0 should pass through, got %q", got)
	}
}

func TestFitTextTruncatesToMaxw(t *testing.T) {
	got := fitText("abcdefghij", 40, px) // 40px = 5 runes; ell = 24px -> budget 2 runes
	if got != "ab..." {
		t.Fatalf("got %q, want %q", got, "ab...")
	}
	if px(got) > 40 {
		t.Fatalf("%q measures %d, over maxw 40", got, px(got))
	}
	// Whatever the length, the result must fit the budget.
	for n := 1; n <= 10; n++ {
		s := strings.Repeat("x", n)
		g := fitText(s, 40, px)
		if px(g) > 40 {
			t.Fatalf("fitText(%q) = %q measures %d, over 40", s, g, px(g))
		}
	}
}

func TestFitTextKeepsRunesWhole(t *testing.T) {
	// CJK runes must not be cut mid-character.
	got := fitText("あいうがぎぐ", 40, px) // each 8px; ell 3 runes = 24 -> budget 2 runes
	if got != "あい..." {
		t.Fatalf("got %q, want %q", got, "あい...")
	}
}

func TestFitTextEllOnlyWhenNoRoom(t *testing.T) {
	if got := fitText("abcdef", 3, px); got != ell {
		t.Fatalf("got %q, want %q", got, ell)
	}
}

// TestFitItemsUniqueness pins the invariant list selection relies on:
// the EntrySelectedHandler maps an entry back to its index by string
// equality, so two items that shortened to the same text would make
// selected() report the first one and the editor would jump to the
// wrong definition.
func TestFitItemsUniqueness(t *testing.T) {
	w := 104
	maxw := w - listPadX
	items := []string{
		"myVeryLongFunctionName  (1)",
		"myVeryLongFunctionName  (2)",
		"myVeryLongFunctionName  (100)",
	}
	out := fitItems(items, maxw, px)
	if len(out) != len(items) {
		t.Fatalf("length = %d, want %d", len(out), len(items))
	}
	seen := map[string]bool{}
	for i, s := range out {
		if px(s) > maxw {
			t.Errorf("item %d %q measures %d, over maxw %d", i, s, px(s), maxw)
		}
		if seen[s] {
			t.Fatalf("item %d = %q duplicates an earlier entry: selected() would report the earlier index", i, s)
		}
		seen[s] = true
	}
}

// TestFitItemsReservesPaddingOnly pins the width budget. The listbox
// keeps its vertical slider (HideHorizontalSlider only), so the slider
// width is reserved separately by the caller; an item that fits the
// remaining box must not be shortened.
func TestFitItemsReservesPaddingOnly(t *testing.T) {
	w := 104
	maxw := w - listPadX
	s := strings.Repeat("a", maxw/8) // exactly maxw wide
	if px(s) != maxw {
		t.Fatalf("test setup: width %d != %d", px(s), maxw)
	}
	if got := fitItems([]string{s}, maxw, px); got[0] != s {
		t.Fatalf("item shortened to %q although it fits the padding box", got[0])
	}
	// One rune wider must be shortened.
	if got := fitItems([]string{s + "a"}, maxw, px); got[0] == s+"a" {
		t.Fatalf("item %q was not shortened", got[0])
	}
}

func TestFitItemsKeepsOrderAndLength(t *testing.T) {
	items := []string{"short", "a very long item name here", "x", "another rather long one"}
	out := fitItems(items, 104-listPadX, px)
	if len(out) != len(items) {
		t.Fatalf("length = %d, want %d", len(out), len(items))
	}
	if out[0] != "short" || out[2] != "x" {
		t.Fatalf("order not preserved: %q", out)
	}
}

func TestFitItemsInputNotMutated(t *testing.T) {
	items := []string{"a very long item name here", "b"}
	copyOf := append([]string(nil), items...)
	fitItems(items, 104-listPadX, px)
	for i := range items {
		if items[i] != copyOf[i] {
			t.Fatalf("input mutated at %d: %q -> %q", i, copyOf[i], items[i])
		}
	}
}

func TestFitItemsEmpty(t *testing.T) {
	if got := fitItems(nil, 104, px); len(got) != 0 {
		t.Fatalf("nil items -> %v", got)
	}
}
