package gui

import (
	"strings"
	"testing"
)

// px は TextWidth の決定的な代役で、1ルーンあたり8pxとして測る。
func px(s string) int { return 8 * len([]rune(s)) }

// 純粋なテキスト収め処理（ウィンドウ不要）。ウィジェットを作るバックエンド側のテストは TestGuiRender より前の backend_test.go にある。Ebiten は
// 1プロセスに1回だけゲームループを許すため、それ以降は NewImage がパニックする。

func TestFitTextShortUnchanged(t *testing.T) {
	if got := fitText("abc", 100, px); got != "abc" {
		t.Fatalf("short text changed: %q", got)
	}
	// 非正の予算では収め処理を無効化する（テキストを見えるままにする）。
	if got := fitText("abcdef", 0, px); got != "abcdef" {
		t.Fatalf("maxw 0 should pass through, got %q", got)
	}
}

func TestFitTextTruncatesToMaxw(t *testing.T) {
	got := fitText("abcdefghij", 40, px) // 40px = 5ルーン分、ell = 24px のため2ルーン分の予算
	if got != "ab..." {
		t.Fatalf("got %q, want %q", got, "ab...")
	}
	if px(got) > 40 {
		t.Fatalf("%q measures %d, over maxw 40", got, px(got))
	}
	// 長さによらず、結果は予算内に収めなければならない。
	for n := 1; n <= 10; n++ {
		s := strings.Repeat("x", n)
		g := fitText(s, 40, px)
		if px(g) > 40 {
			t.Fatalf("fitText(%q) = %q measures %d, over 40", s, g, px(g))
		}
	}
}

func TestFitTextKeepsRunesWhole(t *testing.T) {
	// CJK ルーンは文字の途中で切ってはならない。
	got := fitText("あいうがぎぐ", 40, px) // 各8px、ell 3ルーン = 24 のため2ルーン分の予算
	if got != "あい..." {
		t.Fatalf("got %q, want %q", got, "あい...")
	}
}

func TestFitTextEllOnlyWhenNoRoom(t *testing.T) {
	if got := fitText("abcdef", 3, px); got != ell {
		t.Fatalf("got %q, want %q", got, ell)
	}
}

// TestFitItemsUniqueness はリスト選択が依存する不変条件を固定する。
// EntrySelectedHandler は項目を文字列の等価性でインデックスに戻すため、2つの項目が同じ短縮形になると
// selected() は最初のものを報告し、エディタは誤った定義に飛んでしまう。
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

// TestFitItemsReservesPaddingOnly は幅の予算を固定する。リストボックスは
// 垂直スライダーを保持する（水平スライダーのみ非表示）ため、スライダー幅は呼び出し側が別に確保する。残りの枠に収まる項目は短縮してはならない。
func TestFitItemsReservesPaddingOnly(t *testing.T) {
	w := 104
	maxw := w - listPadX
	s := strings.Repeat("a", maxw/8) // ちょうど maxw 幅
	if px(s) != maxw {
		t.Fatalf("test setup: width %d != %d", px(s), maxw)
	}
	if got := fitItems([]string{s}, maxw, px); got[0] != s {
		t.Fatalf("item shortened to %q although it fits the padding box", got[0])
	}
	// 1ルーン分広いと短縮されなければならない。
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
