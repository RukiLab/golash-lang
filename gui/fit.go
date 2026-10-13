package gui

// Text fitting for widgets that ebitenui draws without clipping: a
// label wider than its button, or a list entry wider than the list,
// would spill past the frame. Both are shortened with an ellipsis here
// instead, measured with the same face that renders them.

const (
	// listSliderW is the visible vertical slider's width
	// (MinHandleSize default 16 + zero track padding). The
	// horizontal slider stays hidden, so nothing is reserved for it.
	listSliderW = 16

	// listPadX is the horizontal inset inside a listbox:
	// ScrollContainerPadding(1) + EntryTextPadding(2) per side.
	listPadX = 8

	// buttonPadX is the horizontal inset inside a button:
	// TextPadding(2) per side plus a little safety.
	buttonPadX = 6

	// ell is the truncation marker.
	ell = "..."
)

// longestEllFit returns how many runes of r fit maxw when followed by
// ell, or -1 when even ell alone does not fit.
func longestEllFit(r []rune, maxw int, measure func(string) int) int {
	if measure(ell) > maxw {
		return -1
	}
	lo, hi := 0, len(r)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if measure(string(r[:mid]))+measure(ell) <= maxw {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// fitText shortens s with an ellipsis so it measures at most maxw.
// Runes are kept whole (no half characters for CJK), and a string that
// already fits is returned unchanged.
func fitText(s string, maxw int, measure func(string) int) string {
	if maxw <= 0 || measure(s) <= maxw {
		return s
	}
	r := []rune(s)
	n := longestEllFit(r, maxw, measure)
	if n < 0 {
		return ell
	}
	return string(r[:n]) + ell
}

// fitUniqueText shortens s like fitText but skips any form already
// claimed by an earlier item. List selection is index-based, yet the
// EntrySelectedHandler resolves a chosen entry back to its index by
// comparing strings, so two items that shortened to the same text would
// both resolve to the first one: clicking the second row would jump to
// the wrong definition.
func fitUniqueText(s string, maxw int, measure func(string) int, used map[string]bool) string {
	r := []rune(s)
	for k := longestEllFit(r, maxw, measure); k >= 0; k-- {
		cand := string(r[:k]) + ell
		if !used[cand] {
			return cand
		}
	}
	// Every shortened form is taken: keep the full text. It spills past
	// the frame, but the scroll mask clips it and the index stays
	// correct. (A full text that is also taken means the script passed
	// duplicates, which no display can tell apart.)
	return s
}

// fitItems shortens every item to maxw, keeping order and length so a
// selection index still points at the same item. Distinct inputs stay
// distinct after fitting; the input slice is never modified.
func fitItems(items []string, maxw int, measure func(string) int) []string {
	out := make([]string, len(items))
	used := make(map[string]bool, len(items))
	for i, s := range items {
		if measure(s) <= maxw {
			out[i] = s
		} else {
			out[i] = fitUniqueText(s, maxw, measure, used)
		}
		used[out[i]] = true
	}
	return out
}
