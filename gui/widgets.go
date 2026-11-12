// Package gui — 自前描画ウィジェット (G3)。
// ebitenui を使わず、canvas と同じく矩形・文字を直接描く。
// スクリプトスレッドは mu 下で状態だけを更新し、ブロックしない
// (画像アップロード等の game スレッド必須処理を除く)。
// ゲームスレッドが Update で入力を処理し、Draw で描画する。
package gui

import (
	"fmt"
	"image"
	"image/color"
	"slices"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

type widgetKind int

const (
	wButton widgetKind = iota
	wInput
	wList
	wCheck
	wCombo
	wArea
)

// ButtonImage は button の1状態分の画像指定。Buf は picload 等の
// バッファ id (0 は空きスロット)、SX/SY は切出し左上。切出しサイズは
// ボタンの w/h (原寸コピー、拡縮しない)。
type ButtonImage struct {
	Buf, SX, SY int
}

// widgetEntry は自前描画ウィジェットの状態。mu で保護される。
// ebitenui の子は持たない。配置は Add* で同期的に更新され、
// ゲームスレッドはスナップショットして描く。
type widgetEntry struct {
	id   int
	kind widgetKind
	gx   int
	gy   int
	gw   int
	gh   int
	// disabled は操作を凍結する (objenable)。
	disabled bool
	// ボタンのラッチ (objget で消費) と押下開始追跡。
	pressed    bool
	downInside bool
	// label は描画用の短縮済み、rawLabel は呼出時の原文。
	label    string
	rawLabel string
	// gimgs は button の画像指定 (最大4: 通常/ホバー/押下/無効)。
	// 各要素は (buf, sx, sy) で、ボタンサイズ w x h で切り出す。
	gimgs []ButtonImage
	// chkbox。
	checked bool
	// list/combo。items は描画用の短縮済み、rawItems は原文。
	items    []string
	rawItems []string
	selIdx   int
	// scroll は list: 先頭可視行、area: 先頭可視折返し行。
	scroll int
	// combo の展開状態とドロップダウン用スクロール。
	expanded   bool
	dropScroll int
	// area の本文ミラーと折返しキャッシュ。
	cached  string
	wrapped []string
	wrapW   int
	// area の下端張付き (ログビューア)。
	stickBottom bool
	// スクロールバーのドラッグ中。
	dragging   bool
	dragStartY int
	dragStart  int
	// カスタム単一行エディタ (wInput)。
	edit *inputState
	// inputInit は AddInput の初期テキスト引数。
	inputInit string
}

// ---------- テーマ (Windows 標準: 白、灰色の枠、黒文字) ----------

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
	themeScrollThumb       = color.NRGBA{0xC1, 0xC1, 0xC1, 0xFF}
	themeScrollThumbHover  = color.NRGBA{0xA6, 0xA6, 0xA6, 0xFF}
	themeScrollThumbPress  = color.NRGBA{0x78, 0x78, 0x78, 0xFF}
	themeScrollThumbBorder = color.NRGBA{0x9E, 0x9E, 0x9E, 0xFF}
)

// ---------- ダイアログ状態 (モーダル、自前描画) ----------

type dialogState struct {
	active   bool
	lines    []string
	labels   []string
	values   []bool
	winRect  image.Rectangle
	btnRects []image.Rectangle
	result   chan bool
}

// dialogActiveLocked はモーダルダイアログが表示中かを報告する。
// 呼出し側は mu を保持すること。
func (b *WindowBackend) dialogActiveLocked() bool {
	return b.dlg != nil && b.dlg.active
}

// isDialogActive は mu を取得して表示中かを報告する (ゲームスレッド用)。
func (b *WindowBackend) isDialogActive() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dialogActiveLocked()
}

// ---------- ヘルパー ----------

func (b *WindowBackend) checkBoxSize() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.checkBoxSizeLocked()
}

func (b *WindowBackend) checkBoxSizeLocked() int {
	return 16
}

// rowH は list/combo の1行高。フォントの行ピッチを使う。
func (b *WindowBackend) rowHLocked() int {
	h := int(b.lineH + 0.5)
	if h < 12 {
		h = 12
	}
	return h
}

// listVisForLocked は高さ gh での可視行数を返す。
// clampListScrollLocked/ensureListVisibleLocked と同じ式。
func (b *WindowBackend) listVisForLocked(gh int) int {
	vis := gh / b.rowHLocked()
	if vis < 1 {
		vis = 1
	}
	return vis
}

// listNeedsScrollLocked は項目数 n が高さ gh にあふれて
// 縦スクロールバーが必要かを報告する。
func (b *WindowBackend) listNeedsScrollLocked(gh, n int) bool {
	return n > b.listVisForLocked(gh)
}

// listTextMaxW は項目テキストの収め幅を返す。
// あふれたときだけバー幅を差し引き、それ以外は枠いっぱいを使う。
func listTextMaxW(gw int, needScroll bool) int {
	if needScroll {
		return gw - listSliderW - listPadX
	}
	return gw - listPadX
}

// advanceWidgetCursorLocked はウィジェット配置後の自動改行:
// テキスト・グラフィックス両カーソルを x=0, y+h へ移す。
// 呼出し側は mu を保持すること。
func (b *WindowBackend) advanceWidgetCursorLocked(y, h int) {
	b.gx = 0
	b.gy = y + h
	b.curX = 0
	b.curY = float64(y + h)
	b.lineStartX = 0
}

// areaWrapLocked は mesbox 本文を折り返す。
// あふれたときだけバー幅を差し引き、それ以外は枠いっぱいを使う。
func (b *WindowBackend) areaWrapLocked(text string, gw, gh int) []string {
	rh := b.rowHLocked()
	vis := gh / rh
	if vis < 1 {
		vis = 1
	}
	full := b.wrapLinesLocked(text, gw-8)
	if len(full) <= vis {
		return full
	}
	return b.wrapLinesLocked(text, gw-8-listSliderW)
}

// wrapLines は text を maxW に収まるよう折り返す。改行は尊重する。
func (b *WindowBackend) wrapLinesLocked(s string, maxW int) []string {
	if s == "" {
		return []string{""}
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		r := []rune(para)
		start := 0
		for start < len(r) {
			lo, hi := start, len(r)
			// 二分で収まる最大を取る。
			for lo < hi {
				mid := (lo + hi + 1) / 2
				w := b.measureFaceLocked(string(r[start:mid]))
				if int(w+0.5) <= maxW {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			if lo <= start {
				// 1文字も入らない: 1文字だけ出す (はみ出し)。
				lo = start + 1
			}
			out = append(out, string(r[start:lo]))
			start = lo
		}
	}
	return out
}

// ---------- スクリプトスレッド API (同期、ブロックなし) ----------

// AddButton はボタンを作成 (または置換) する。同期的に状態だけを更新し、
// ゲームスレッドへの往復はしないため配置は軽い。
// imgs は最大4つ (通常/ホバー/押下/無効) の (buf, sx, sy)。切出しサイズは
// ボタンの w/h で原寸コピーする (拡縮しない)。buf==0 は空きスロットで
// sx==sy==0 が必要。省略されたホバー・押下は通常画像、省略された無効は
// 通常画像の減光コピーになる。
func (b *WindowBackend) AddButton(id int, label string, x, y, w, h int, imgs ...ButtonImage) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("button のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	if len(imgs) > 4 {
		return fmt.Errorf("button：画像は最大 4 状態（通常/ホバー/押下/無効）です。%d 状態が指定されました", len(imgs))
	}
	for i, im := range imgs {
		if im.Buf == 0 {
			if im.SX != 0 || im.SY != 0 {
				return fmt.Errorf("button：画像なし(0)の場合は座標も 0 を指定してください。状態 %d が %d, %d です", i, im.SX, im.SY)
			}
			continue
		}
		if !b.hasBuf(im.Buf) {
			return fmt.Errorf("button：不明な画像バッファ %d です", im.Buf)
		}
		if im.SX < 0 || im.SY < 0 {
			return fmt.Errorf("button：切出し座標は 0 以上である必要があります。状態 %d が %d, %d です", i, im.SX, im.SY)
		}
	}
	fitted := fitText(label, w-buttonPadX, b.TextWidth)
	b.mu.Lock()
	defer b.mu.Unlock()
	if old, ok := b.widgets[id]; ok && old.kind == wButton &&
		old.gx == x && old.gy == y && old.gw == w && old.gh == h &&
		old.rawLabel == label && slices.Equal(old.gimgs, imgs) {
		b.advanceWidgetCursorLocked(y, h)
		return nil
	}
	if old, ok := b.widgets[id]; ok && old.kind == wButton {
		old.gx, old.gy, old.gw, old.gh = x, y, w, h
		old.label, old.rawLabel = fitted, label
		old.gimgs = append([]ButtonImage(nil), imgs...)
		b.advanceWidgetCursorLocked(y, h)
		return nil
	}
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	b.widgets[id] = &widgetEntry{
		id: id, kind: wButton,
		gx: x, gy: y, gw: w, gh: h,
		label: fitted, rawLabel: label,
		gimgs:  append([]ButtonImage(nil), imgs...),
		selIdx: -1,
	}
	// disabled は新規でも引き継がない (新規 id は有効)。既存の別種別からの
	// 置換も有効始まりにする (旧 ebitenui 版は引き継いだが、自前では単純化)。
	// 配置後は自動で改行する (x=0, y+h)。
	b.advanceWidgetCursorLocked(y, h)
	return nil
}

// Pressed は最後の呼出以降のクリックを報告する (消費される)。
func (b *WindowBackend) Pressed(id int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wButton {
		return false, fmt.Errorf("objget：不明なボタン %d です", id)
	}
	p := e.pressed
	e.pressed = false
	return p, nil
}

// SetButtonLabel はボタンの表示文字列を変える。
func (b *WindowBackend) SetButtonLabel(id int, label string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wButton {
		return fmt.Errorf("objset：不明なボタン %d です", id)
	}
	if e.rawLabel == label {
		return nil
	}
	e.rawLabel = label
	e.label = fitText(label, e.gw-buttonPadX, b.textWidthLocked)
	return nil
}

// AddList はリストボックスを作成 (または置換) する。
func (b *WindowBackend) AddList(id, x, y, w, h int, items []string) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("listbox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	rawCopy := append([]string(nil), items...)
	b.mu.Lock()
	defer b.mu.Unlock()
	// あふれたときだけバー幅を確保し、それ以外は枠いっぱいを使う。
	needScroll := b.listNeedsScrollLocked(h, len(items))
	fitted := fitItems(items, listTextMaxW(w, needScroll), b.textWidthLocked)
	if old, ok := b.widgets[id]; ok && old.kind == wList &&
		old.gx == x && old.gy == y && old.gw == w && old.gh == h &&
		slices.Equal(old.rawItems, rawCopy) {
		b.advanceWidgetCursorLocked(y, h)
		return nil
	}
	if old, ok := b.widgets[id]; ok && old.kind == wList &&
		old.gx == x && old.gy == y && old.gw == w && old.gh == h &&
		slices.Equal(old.items, fitted) {
		old.rawItems = rawCopy
		b.advanceWidgetCursorLocked(y, h)
		return nil
	}
	// 同一矩形で項目のみ変化: その場で入替、選択とスクロールは維持の試み。
	// 項目数が変わって選択が範囲外ならクリアする。
	if old, ok := b.widgets[id]; ok && old.kind == wList &&
		old.gx == x && old.gy == y && old.gw == w && old.gh == h {
		old.items = append([]string(nil), fitted...)
		old.rawItems = rawCopy
		if old.selIdx < 0 || old.selIdx >= len(fitted) {
			old.selIdx = -1
		}
		b.clampListScrollLocked(old)
		b.advanceWidgetCursorLocked(y, h)
		return nil
	}
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	b.widgets[id] = &widgetEntry{
		id: id, kind: wList,
		gx: x, gy: y, gw: w, gh: h,
		items: append([]string(nil), fitted...),
		rawItems: rawCopy,
		selIdx: -1,
	}
	b.advanceWidgetCursorLocked(y, h)
	return nil
}

// UpdateList は既存 listbox の項目をその場で置き換える。
func (b *WindowBackend) UpdateList(id int, items []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wList {
		return nil
	}
	if slices.Equal(e.rawItems, items) {
		return nil
	}
	needScroll := b.listNeedsScrollLocked(e.gh, len(items))
	fitted := fitItems(items, listTextMaxW(e.gw, needScroll), b.textWidthLocked)
	if slices.Equal(e.items, fitted) {
		e.rawItems = append([]string(nil), items...)
		return nil
	}
	e.items = append([]string(nil), fitted...)
	e.rawItems = append([]string(nil), items...)
	if e.selIdx < 0 || e.selIdx >= len(fitted) {
		e.selIdx = -1
	}
	b.clampListScrollLocked(e)
	return nil
}

// SelectList は listbox の選択を変える (ハンドラは発火しない)。
// -1 は未選択状態にする。
func (b *WindowBackend) SelectList(id, index int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wList {
		return nil
	}
	if index == -1 {
		e.selIdx = -1
		return nil
	}
	if index < 0 || index >= len(e.items) {
		return nil
	}
	e.selIdx = index
	b.ensureListVisibleLocked(e)
	return nil
}

// SelectedIndex は list/combo の選択を返す (無選択 -1)。
func (b *WindowBackend) SelectedIndex(id int) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || (e.kind != wList && e.kind != wCombo) {
		return 0, fmt.Errorf("objget：不明な listbox %d です", id)
	}
	return e.selIdx, nil
}

// ListItems は listbox の項目原文を返す (wbind_items 用)。
func (b *WindowBackend) ListItems(id int) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wList {
		return nil, fmt.Errorf("不明な listbox %d です", id)
	}
	return append([]string(nil), e.rawItems...), nil
}

// ComboItems は combox の項目原文を返す (wbind_items 用)。
func (b *WindowBackend) ComboItems(id int) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wCombo {
		return nil, fmt.Errorf("不明な combox %d です", id)
	}
	return append([]string(nil), e.rawItems...), nil
}

func (b *WindowBackend) clampListScrollLocked(e *widgetEntry) {
	rh := b.rowHLocked()
	vis := e.gh / rh
	if vis < 1 {
		vis = 1
	}
	max := len(e.items) - vis
	if max < 0 {
		max = 0
	}
	if e.scroll > max {
		e.scroll = max
	}
	if e.scroll < 0 {
		e.scroll = 0
	}
}

func (b *WindowBackend) ensureListVisibleLocked(e *widgetEntry) {
	rh := b.rowHLocked()
	vis := e.gh / rh
	if vis < 1 {
		vis = 1
	}
	if e.selIdx < 0 {
		return
	}
	if e.selIdx < e.scroll {
		e.scroll = e.selIdx
	}
	if e.selIdx >= e.scroll+vis {
		e.scroll = e.selIdx - vis + 1
	}
	b.clampListScrollLocked(e)
}

// AddCheck はチェックボックスを作成 (または置換) する。
// 配置後は自動で改行する (x=0, y+h)。
func (b *WindowBackend) AddCheck(id int, label string, x, y, w, h int, checked bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	// w/h<=0 は推奨サイズ: ボックス+ラベルから決める。
	if w <= 0 || h <= 0 {
		bs := b.checkBoxSizeLocked()
		tw := b.textWidthLocked(label)
		w, h = bs+6+tw+8, bs+8
		if w < 32 {
			w = 32
		}
	}
	if old, ok := b.widgets[id]; ok && old.kind == wCheck &&
		old.gx == x && old.gy == y && old.gw == w && old.gh == h &&
		old.label == label && old.rawLabel == label && old.checked == checked {
		b.advanceWidgetCursorLocked(y, h)
		return nil
	}
	if old, ok := b.widgets[id]; ok && old.kind == wCheck {
		old.gx, old.gy, old.gw, old.gh = x, y, w, h
		old.label, old.rawLabel = label, label
		old.checked = checked
		b.advanceWidgetCursorLocked(y, h)
		return nil
	}
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	b.widgets[id] = &widgetEntry{
		id: id, kind: wCheck,
		gx: x, gy: y, gw: w, gh: h,
		label: label, rawLabel: label,
		checked: checked,
		selIdx: -1,
	}
	b.advanceWidgetCursorLocked(y, h)
	return nil
}

// Checked はチェック状態を報告する。
func (b *WindowBackend) Checked(id int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wCheck {
		return false, fmt.Errorf("objget：不明な checkbox %d です", id)
	}
	return e.checked, nil
}

// SetChecked はオン・オフを変える。
func (b *WindowBackend) SetChecked(id int, on bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wCheck {
		return fmt.Errorf("objset：不明な checkbox %d です", id)
	}
	e.checked = on
	return nil
}

// SetCheckLabel は表示文字列を変える。
func (b *WindowBackend) SetCheckLabel(id int, label string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wCheck {
		return fmt.Errorf("objset：不明な checkbox %d です", id)
	}
	e.label, e.rawLabel = label, label
	return nil
}

// WidgetType は objset/objget の振分け用に種別名を返す。
func (b *WindowBackend) WidgetType(id int) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok {
		return "", fmt.Errorf("不明なウィジェット %d です", id)
	}
	switch e.kind {
	case wButton:
		return "button", nil
	case wInput:
		return "inputbox", nil
	case wList:
		return "listbox", nil
	case wCheck:
		return "chkbox", nil
	case wCombo:
		return "combox", nil
	case wArea:
		return "mesbox", nil
	}
	return "", fmt.Errorf("不明なウィジェット %d です", id)
}

// SelectCombo は combox の選択を変える。-1 は未選択状態にする。
func (b *WindowBackend) SelectCombo(id, index int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wCombo {
		return nil
	}
	if index == -1 {
		e.selIdx = -1
		return nil
	}
	if index < 0 || index >= len(e.items) {
		return nil
	}
	e.selIdx = index
	return nil
}

// UpdateCombo は combox の項目を置き換える。選択はクリア。
func (b *WindowBackend) UpdateCombo(id int, items []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wCombo {
		return nil
	}
	if slices.Equal(e.items, items) {
		if len(e.rawItems) == 0 {
			e.rawItems = append([]string(nil), items...)
		}
		return nil
	}
	e.items = append([]string(nil), items...)
	e.rawItems = append([]string(nil), items...)
	e.selIdx = -1
	if len(e.items) > 0 && e.dropScroll > len(e.items)-1 {
		e.dropScroll = 0
	}
	return nil
}

// AddCombo はドロップダウンを作成 (または置換) する。
// 配置後は自動で改行する (x=0, y+h)。
func (b *WindowBackend) AddCombo(id, x, y, w, h int, items []string, sel int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("combox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	if sel < -1 || sel >= len(items) {
		return fmt.Errorf("combox：選択 %d は範囲外です（%d 項目）", sel, len(items))
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if old, ok := b.widgets[id]; ok && old.kind == wCombo &&
		old.gx == x && old.gy == y && old.gw == w && old.gh == h &&
		slices.Equal(old.items, items) {
		if sel != old.selIdx {
			if sel >= 0 {
				old.selIdx = sel
			} else if sel == -1 {
				old.selIdx = -1
			}
		}
		b.advanceWidgetCursorLocked(y, h)
		return nil
	}
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	e := &widgetEntry{
		id: id, kind: wCombo,
		gx: x, gy: y, gw: w, gh: h,
		items: append([]string(nil), items...),
		rawItems: append([]string(nil), items...),
		selIdx: -1,
	}
	if sel >= 0 {
		e.selIdx = sel
	} else {
		// sel == -1: 明示的な未選択。呼出し側が sel 省略時は
		// builtin が 0 を渡すため、ここではそのまま -1 を保つ。
		e.selIdx = -1
	}
	b.widgets[id] = e
	b.advanceWidgetCursorLocked(y, h)
	return nil
}

// AddArea は複数行テキストボックスを作成 (または置換) する。
// 配置後は自動で改行する (x=0, y+h)。
func (b *WindowBackend) AddArea(id, x, y, w, h int, text string) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("mesbox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if old, ok := b.widgets[id]; ok && old.kind == wArea &&
		old.gx == x && old.gy == y && old.gw == w && old.gh == h {
		if old.cached == text {
			b.advanceWidgetCursorLocked(y, h)
			return nil
		}
		wasBottom := b.areaAtBottomLocked(old)
		old.cached = text
		old.wrapped = b.areaWrapLocked(text, w, h)
		old.wrapW = w
		if wasBottom {
			b.areaToBottomLocked(old)
		} else {
			b.clampAreaScrollLocked(old)
		}
		b.advanceWidgetCursorLocked(y, h)
		return nil
	}
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	e := &widgetEntry{
		id: id, kind: wArea,
		gx: x, gy: y, gw: w, gh: h,
		cached: text,
		selIdx: -1,
		stickBottom: true,
	}
	e.wrapped = b.areaWrapLocked(text, w, h)
	e.wrapW = w
	b.widgets[id] = e
	b.areaToBottomLocked(e)
	b.advanceWidgetCursorLocked(y, h)
	return nil
}

// AreaText は複数行ボックスの内容を返す。
func (b *WindowBackend) AreaText(id int) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wArea {
		return "", fmt.Errorf("objget：不明な mesbox %d です", id)
	}
	return e.cached, nil
}

// SetAreaText は内容を置き換える。下端張付きなら末尾に留まる。
func (b *WindowBackend) SetAreaText(id int, text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wArea {
		return fmt.Errorf("objset：不明な mesbox %d です", id)
	}
	if e.cached == text {
		return nil
	}
	wasBottom := b.areaAtBottomLocked(e)
	e.cached = text
	if e.wrapW != e.gw {
		e.wrapped = b.areaWrapLocked(text, e.gw, e.gh)
		e.wrapW = e.gw
	} else {
		e.wrapped = b.areaWrapLocked(text, e.gw, e.gh)
	}
	if wasBottom {
		b.areaToBottomLocked(e)
	} else {
		b.clampAreaScrollLocked(e)
	}
	return nil
}

func (b *WindowBackend) areaVisibleLocked(e *widgetEntry) int {
	rh := b.rowHLocked()
	vis := e.gh / rh
	if vis < 1 {
		vis = 1
	}
	return vis
}

func (b *WindowBackend) areaAtBottomLocked(e *widgetEntry) bool {
	vis := b.areaVisibleLocked(e)
	return e.scroll+vis >= len(e.wrapped)
}

func (b *WindowBackend) areaToBottomLocked(e *widgetEntry) {
	vis := b.areaVisibleLocked(e)
	n := len(e.wrapped) - vis
	if n < 0 {
		n = 0
	}
	e.scroll = n
}

func (b *WindowBackend) clampAreaScrollLocked(e *widgetEntry) {
	vis := b.areaVisibleLocked(e)
	max := len(e.wrapped) - vis
	if max < 0 {
		max = 0
	}
	if e.scroll > max {
		e.scroll = max
	}
	if e.scroll < 0 {
		e.scroll = 0
	}
}

// SetEnabled は有効・無効を切替える。
func (b *WindowBackend) SetEnabled(id int, on bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok {
		return fmt.Errorf("objenable：不明なウィジェット %d です", id)
	}
	e.disabled = !on
	if e.disabled && e.edit != nil {
		e.edit.focused = false
	}
	return nil
}

// RemoveWidget は1つのウィジェットを落とす。
func (b *WindowBackend) RemoveWidget(id int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.widgets[id]; !ok {
		return fmt.Errorf("不明なウィジェット %d です", id)
	}
	delete(b.widgets, id)
	// 開いていた combo の展開が残らないよう、他も閉じる。
	for _, e := range b.widgets {
		if e.kind == wCombo {
			e.expanded = false
		}
	}
	return nil
}

// ClearWidgets はすべてのウィジェットを落とす。
func (b *WindowBackend) ClearWidgets() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.widgets = map[int]*widgetEntry{}
	if b.dlg != nil {
		// ダイアログ表示中の全消しはダイアログも閉じて false を返す。
		if b.dlg.active {
			b.dlg.active = false
			select {
			case b.dlg.result <- false:
			default:
			}
		}
	}
}

// HasWidget は登録簿に存在するかを報告する (テスト用)。
func (b *WindowBackend) HasWidget(id int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.widgets[id]
	return ok
}

// syncWidgetCache は互換のための no-op。自前描画ではミラーが本体で、
// ゲームスレッドの更新を待つ必要がない。
func (b *WindowBackend) syncWidgetCache() {}

// ---------- 入力処理 (ゲームスレッド、Update から) ----------

func ptIn(x, y, rx, ry, rw, rh int) bool {
	return x >= rx && x < rx+rw && y >= ry && y < ry+rh
}

// pumpWidgets は自前ウィジェットのクリック・ホイール・ドラッグを処理する。
// モーダルダイアログ表示中は背後のウィジェットを一切操作させない:
// ダイアログのボタンだけを処理して戻る (クリック・ホイール・ドラッグの
// 背後への波及と、ダイアログ後の誤作動を防ぐ)。
func (b *WindowBackend) pumpWidgets() {
	mx, my := ebiten.CursorPosition()
	justPressed := inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft)
	if b.isDialogActive() {
		b.mu.Lock()
		b.pumpDialogLocked(mx, my, justPressed)
		b.mu.Unlock()
		return
	}
	justReleased := inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft)
	down := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	_, wy := ebiten.Wheel()

	b.mu.Lock()
	defer b.mu.Unlock()
	// 展開中 combo のドロップダウンを最前面扱いで先に処理する。
	for _, e := range b.widgets {
		if e.kind != wCombo || !e.expanded || e.disabled {
			continue
		}
		b.pumpComboExpandedLocked(e, mx, my, justPressed, justReleased, down, wy)
		// 展開中は他のウィジェットに落とさない (外しクリックで閉じる)。
		if e.expanded {
			return
		}
	}
	// 通常ウィジェット。ID 順で安定化 (小さい方が背面、上書きなし)。
	ids := make([]int, 0, len(b.widgets))
	for id := range b.widgets {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		e, ok := b.widgets[id]
		if !ok {
			continue
		}
		if e.disabled {
			e.downInside = false
			continue
		}
		switch e.kind {
		case wButton:
			if justPressed && ptIn(mx, my, e.gx, e.gy, e.gw, e.gh) {
				e.downInside = true
			}
			if justReleased {
				if e.downInside && ptIn(mx, my, e.gx, e.gy, e.gw, e.gh) {
					e.pressed = true
				}
				e.downInside = false
			}
			if !down {
				// 押下中に外れても離したら解除 (引っかかり防止)。
				if !ptIn(mx, my, e.gx, e.gy, e.gw, e.gh) && !justReleased {
					// down 維持中は downInside を保つ (スライド戻りで押せる)。
				}
			}
		case wCheck:
			if justPressed && ptIn(mx, my, e.gx, e.gy, e.gw, e.gh) {
				e.downInside = true
			}
			if justReleased {
				if e.downInside && ptIn(mx, my, e.gx, e.gy, e.gw, e.gh) {
					e.checked = !e.checked
				}
				e.downInside = false
			}
		case wList:
			b.pumpListLocked(e, mx, my, justPressed, justReleased, down, wy)
		case wCombo:
			if justPressed && ptIn(mx, my, e.gx, e.gy, e.gw, e.gh) {
				e.downInside = true
			}
			if justReleased {
				if e.downInside && ptIn(mx, my, e.gx, e.gy, e.gw, e.gh) {
					e.expanded = !e.expanded
					if e.expanded {
						e.dropScroll = 0
						// 開いたら選択が見える位置へ。
						if e.selIdx >= 0 {
							e.dropScroll = e.selIdx - 1
							if e.dropScroll < 0 {
								e.dropScroll = 0
							}
						}
					}
				}
				e.downInside = false
			}
			// ホイールで閉じたままの選択を動かさない (誤操作防止)。
		case wArea:
			b.pumpAreaLocked(e, mx, my, justPressed, justReleased, down, wy)
		case wInput:
			// フォーカス・編集は pumpInputs 側。ここでは何もしない。
		}
	}
	// ダイアログが開いていればその入力を処理する。
	b.pumpDialogLocked(mx, my, justPressed)
}

func (b *WindowBackend) pumpListLocked(e *widgetEntry, mx, my int, justPressed, justReleased, down bool, wy float64) {
	rh := b.rowHLocked()
	// ホイール。
	if wy != 0 && ptIn(mx, my, e.gx, e.gy, e.gw, e.gh) {
		e.scroll -= int(wy) * 3
		b.clampListScrollLocked(e)
		e.dragging = false
	}
	// あふれたときだけバー幅を確保し、それ以外は枠いっぱいを使う。
	vis0 := e.gh / rh
	if vis0 < 1 {
		vis0 = 1
	}
	needScroll := len(e.items) > vis0
	// スクロールバー形状。
	sbW := 12
	listW := e.gw
	if needScroll {
		listW = e.gw - sbW
	}
	if listW < 0 {
		listW = 0
	}
	// バー上の操作 (あふれたときだけ)。
	if needScroll && justPressed && ptIn(mx, my, e.gx+e.gw-sbW, e.gy, sbW, e.gh) {
		vis := e.gh / rh
		if vis < 1 {
			vis = 1
		}
		n := len(e.items)
		thumbH, thumbY := listThumb(e.gh, n, vis, e.scroll, rh)
		thumbRect := image.Rect(e.gx+e.gw-sbW, e.gy+thumbY, e.gx+e.gw, e.gy+thumbY+thumbH)
		if ptIn(mx, my, thumbRect.Min.X, thumbRect.Min.Y, thumbRect.Dx(), thumbRect.Dy()) {
			e.dragging = true
			e.dragStartY = my
			e.dragStart = e.scroll
		} else {
			// トラッククリックはページ移動。
			if my < e.gy+thumbY {
				e.scroll -= vis
			} else {
				e.scroll += vis
			}
			b.clampListScrollLocked(e)
		}
		e.downInside = false
		return
	}
	if e.dragging {
		if !needScroll {
			e.dragging = false
		} else if !down {
			e.dragging = false
		} else {
			vis := e.gh / rh
			n := len(e.items)
			if n > vis {
				trackH := e.gh
				thumbH, _ := listThumb(e.gh, n, vis, 0, rh)
				free := trackH - thumbH
				if free > 0 {
					delta := my - e.dragStartY
					// 行単位に換算。
					e.scroll = e.dragStart + delta*(n-vis)/free
					b.clampListScrollLocked(e)
				}
			}
		}
		if e.dragging {
			return
		}
	}
	// 項目クリック。
	if justPressed && ptIn(mx, my, e.gx, e.gy, listW, e.gh) {
		e.downInside = true
	}
	if justReleased {
		if e.downInside && ptIn(mx, my, e.gx, e.gy, listW, e.gh) {
			rel := my - e.gy
			idx := e.scroll + rel/rh
			if idx >= 0 && idx < len(e.items) {
				e.selIdx = idx
			}
		}
		e.downInside = false
	}
}

func (b *WindowBackend) pumpAreaLocked(e *widgetEntry, mx, my int, justPressed, justReleased, down bool, wy float64) {
	if wy != 0 && ptIn(mx, my, e.gx, e.gy, e.gw, e.gh) {
		// 上が正の wheel: 上へ。
		e.scroll -= int(wy) * 3
		b.clampAreaScrollLocked(e)
		e.dragging = false
		// 手で動かしたら張付き解除、末尾に戻したら再張付き。
		e.stickBottom = b.areaAtBottomLocked(e)
	}
	sbW := 12
	vis := b.areaVisibleLocked(e)
	n := len(e.wrapped)
	needScroll := n > vis
	if needScroll && justPressed && ptIn(mx, my, e.gx+e.gw-sbW, e.gy, sbW, e.gh) {
		thumbH, thumbY := listThumb(e.gh, n, vis, e.scroll, b.rowHLocked())
		thumbRect := image.Rect(e.gx+e.gw-sbW, e.gy+thumbY, e.gx+e.gw, e.gy+thumbY+thumbH)
		if ptIn(mx, my, thumbRect.Min.X, thumbRect.Min.Y, thumbRect.Dx(), thumbRect.Dy()) {
			e.dragging = true
			e.dragStartY = my
			e.dragStart = e.scroll
		} else {
			if my < e.gy+thumbY {
				e.scroll -= vis
			} else {
				e.scroll += vis
			}
			b.clampAreaScrollLocked(e)
			e.stickBottom = b.areaAtBottomLocked(e)
		}
		return
	}
	if e.dragging {
		if !needScroll {
			e.dragging = false
			e.stickBottom = b.areaAtBottomLocked(e)
		} else if !down {
			e.dragging = false
			e.stickBottom = b.areaAtBottomLocked(e)
		} else {
			vis := b.areaVisibleLocked(e)
			n := len(e.wrapped)
			if n > vis {
				thumbH, _ := listThumb(e.gh, n, vis, 0, b.rowHLocked())
				free := e.gh - thumbH
				if free > 0 {
					delta := my - e.dragStartY
					e.scroll = e.dragStart + delta*(n-vis)/free
					b.clampAreaScrollLocked(e)
				}
			}
		}
	}
}

func (b *WindowBackend) pumpComboExpandedLocked(e *widgetEntry, mx, my int, justPressed, justReleased, down bool, wy float64) {
	rh := e.gh
	if rh < 8 {
		rh = 16
	}
	maxRows := 4
	n := len(e.items)
	rows := n
	if rows > maxRows {
		rows = maxRows
	}
	if rows < 1 {
		e.expanded = false
		e.dragging = false
		return
	}
	drop := image.Rect(e.gx, e.gy+e.gh, e.gx+e.gw, e.gy+e.gh+rows*rh)
	if wy != 0 && (ptIn(mx, my, drop.Min.X, drop.Min.Y, drop.Dx(), drop.Dy()) || ptIn(mx, my, e.gx, e.gy, e.gw, e.gh)) {
		e.dropScroll -= int(wy) * 2
		max := n - rows
		if max < 0 {
			max = 0
		}
		if e.dropScroll > max {
			e.dropScroll = max
		}
		if e.dropScroll < 0 {
			e.dropScroll = 0
		}
	}
	// ドロップダウンのスクロールバー (drawComboDrop と同じ sbW=10)。
	// listbox/mesbox と同様にドラッグとトラッククリックでスクロールし、
	// 閉じたり項目選択したりしない。
	const comboSbW = 10
	if n > rows {
		dh := drop.Dy()
		thumbH, thumbY := listThumb(dh, n, rows, e.dropScroll, rh)
		barX := drop.Max.X - comboSbW
		thumbRect := image.Rect(barX, drop.Min.Y+thumbY, drop.Max.X, drop.Min.Y+thumbY+thumbH)
		barRect := image.Rect(barX, drop.Min.Y, drop.Max.X, drop.Max.Y)
		if e.dragging {
			if !down {
				e.dragging = false
			} else {
				thumbH0, _ := listThumb(dh, n, rows, 0, rh)
				free := dh - thumbH0
				if free > 0 {
					delta := my - e.dragStartY
					e.dropScroll = e.dragStart + delta*(n-rows)/free
					max := n - rows
					if e.dropScroll > max {
						e.dropScroll = max
					}
					if e.dropScroll < 0 {
						e.dropScroll = 0
					}
				}
			}
			return
		}
		if justPressed && ptIn(mx, my, barRect.Min.X, barRect.Min.Y, barRect.Dx(), barRect.Dy()) {
			if ptIn(mx, my, thumbRect.Min.X, thumbRect.Min.Y, thumbRect.Dx(), thumbRect.Dy()) {
				e.dragging = true
				e.dragStartY = my
				e.dragStart = e.dropScroll
			} else {
				// トラッククリックはページ移動 (閉じない)。
				if my < drop.Min.Y+thumbY {
					e.dropScroll -= rows
				} else {
					e.dropScroll += rows
				}
				max := n - rows
				if e.dropScroll > max {
					e.dropScroll = max
				}
				if e.dropScroll < 0 {
					e.dropScroll = 0
				}
			}
			return
		}
	}
	if !justPressed {
		return
	}
	// 閉じるボタン自体はトグル側で処理されるため、ここではドロップ内のみ。
	if ptIn(mx, my, drop.Min.X, drop.Min.Y, drop.Dx(), drop.Dy()) {
		rel := my - drop.Min.Y
		idx := e.dropScroll + rel/rh
		if idx >= 0 && idx < n {
			e.selIdx = idx
		}
		e.expanded = false
		e.dragging = false
		return
	}
	// 外しクリックで閉じる (閉じたボタン自体のトグルと二重にならないよう、
	// ボタン内ならトグル側に任せてここでは閉じない)。
	if !ptIn(mx, my, e.gx, e.gy, e.gw, e.gh) {
		e.expanded = false
		e.dragging = false
	}
	_ = justReleased
}

func listThumb(trackH, n, vis, scroll, rh int) (thumbH, thumbY int) {
	if n <= vis {
		return 0, 0
	}
	thumbH = trackH * vis / n
	if thumbH < 20 {
		thumbH = 20
	}
	if thumbH > trackH {
		thumbH = trackH
	}
	free := trackH - thumbH
	max := n - vis
	if max <= 0 {
		return thumbH, 0
	}
	thumbY = free * scroll / max
	return thumbH, thumbY
}

// ---------- 描画 (ゲームスレッド、Draw から) ----------

func (b *WindowBackend) drawWidgets(screen *ebiten.Image) {
	b.mu.Lock()
	// スナップショット: 描画中にロックしない。
	type snap struct {
		e        widgetEntry
		face     *text.GoTextFace
		faceSize float64
		lineH    float64
		gdi      gdiSnap
	}
	var ss []snap
	for _, e := range b.widgets {
		cp := *e
		// スライスの別名回避。
		cp.items = append([]string(nil), e.items...)
		cp.rawItems = append([]string(nil), e.rawItems...)
		cp.gimgs = append([]ButtonImage(nil), e.gimgs...)
		cp.wrapped = append([]string(nil), e.wrapped...)
		ss = append(ss, snap{e: cp, face: b.face, faceSize: b.fontSize, lineH: b.lineH, gdi: b.gdiSnapLocked()})
	}
	targets := b.targets
	dlgActive := b.dialogActiveLocked()
	b.mu.Unlock()
	// ID 順で安定描画。
	slices.SortFunc(ss, func(a, c snap) int { return a.e.id - c.e.id })
	mx, my := ebiten.CursorPosition()
	down := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	if dlgActive {
		// モーダル中は背後にホバー・押下を見せない (操作不可の視覚化)。
		mx, my, down = -10000, -10000, false
	}
	for i := range ss {
		e := &ss[i].e
		switch e.kind {
		case wButton:
			drawButton(screen, e, ss[i].face, ss[i].gdi, mx, my, down, targets)
		case wCheck:
			drawCheck(screen, e, ss[i].face, ss[i].gdi, ss[i].lineH, mx, my, down)
		case wList:
			drawList(screen, e, ss[i].face, ss[i].gdi, ss[i].lineH, mx, my, down)
		case wCombo:
			// 閉じた分だけここで描く。展開ドロップは後で前面に。
			drawComboClosed(screen, e, ss[i].face, ss[i].gdi, mx, my, down, targets)
		case wArea:
			drawArea(screen, e, ss[i].face, ss[i].gdi, ss[i].lineH)
		case wInput:
			// input は drawInputs 側で描く。
		}
	}
	// combo ドロップダウンを前面に。
	for i := range ss {
		if ss[i].e.kind == wCombo && ss[i].e.expanded {
			drawComboDrop(screen, &ss[i].e, ss[i].face, ss[i].gdi, ss[i].lineH, mx, my)
		}
	}
	// ダイアログを最前面に。
	b.drawDialog(screen)
}

func drawButton(screen *ebiten.Image, e *widgetEntry, face *text.GoTextFace, gs gdiSnap, mx, my int, down bool, targets map[int]*ebiten.Image) {
	x, y, w, h := float32(e.gx), float32(e.gy), float32(e.gw), float32(e.gh)
	bg, bd := themeWhite, themeFrame
	hover := ptIn(mx, my, e.gx, e.gy, e.gw, e.gh)
	pressing := hover && down && !e.disabled
	if e.disabled {
		bg = themeFaceDis
		bd = themeFrame
	} else if pressing {
		bg, bd = themePressBg, themePressFrame
	} else if hover {
		bg = themeHover
	}
	vector.DrawFilledRect(screen, x, y, w, h, bg, false)
	vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, bd, false)
	// 画像ボタン: (buf, sx, sy) からボタンサイズ w x h を原寸で切り出す。
	// 状態ごとに選択し、省略時は通常に、無効の省略時は通常の減光に
	// フォールバックする。
	if len(e.gimgs) > 0 && e.gimgs[0].Buf != 0 {
		idle := e.gimgs[0]
		cur := idle
		dim := false
		if e.disabled {
			if len(e.gimgs) >= 4 && e.gimgs[3].Buf != 0 {
				cur = e.gimgs[3]
			} else {
				cur = idle
				dim = true
			}
		} else if pressing {
			if len(e.gimgs) >= 3 && e.gimgs[2].Buf != 0 {
				cur = e.gimgs[2]
			} else {
				cur = idle
			}
		} else if hover {
			if len(e.gimgs) >= 2 && e.gimgs[1].Buf != 0 {
				cur = e.gimgs[1]
			} else {
				cur = idle
			}
		}
		if src := targets[cur.Buf]; src != nil {
			sb := src.Bounds()
			sw, sh := sb.Dx(), sb.Dy()
			// 要求矩形 (sx, sy, sx+gw, sy+gh) を src 内でクリップ。
			x0, y0 := cur.SX, cur.SY
			x1, y1 := x0+e.gw, y0+e.gh
			if x0 < 0 {
				x0 = 0
			}
			if y0 < 0 {
				y0 = 0
			}
			if x1 > sw {
				x1 = sw
			}
			if y1 > sh {
				y1 = sh
			}
			if x0 < x1 && y0 < y1 {
				part := src.SubImage(image.Rect(x0, y0, x1, y1)).(*ebiten.Image)
				op := &ebiten.DrawImageOptions{}
				op.GeoM.Translate(float64(e.gx+(x0-cur.SX)), float64(e.gy+(y0-cur.SY)))
				if dim {
					op.ColorScale.Scale(0.6, 0.6, 0.6, 1)
				}
				screen.DrawImage(part, op)
				// 枠をもう一度。
				vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, bd, false)
			}
		}
	}
	if e.label == "" || face == nil {
		return
	}
	fg := themeInk
	if e.disabled {
		fg = themeGrey
	}
	tw := measureWithSnap(e.label, face, gs)
	tx := float64(e.gx) + (float64(e.gw)-tw)/2
	// 垂直中央。
	m := face.Metrics()
	th := m.HAscent + m.HDescent
	ty := float64(e.gy) + (float64(e.gh)-th)/2
	drawFaceText(screen, e.label, face, tx, ty, fg, gs)
}

func drawCheck(screen *ebiten.Image, e *widgetEntry, face *text.GoTextFace, gs gdiSnap, lineH float64, mx, my int, down bool) {
	const bs = 16
	bx, by := float32(e.gx), float32(e.gy)+(float32(e.gh)-float32(bs))/2
	hover := ptIn(mx, my, e.gx, e.gy, e.gw, e.gh)
	pressing := hover && down && !e.disabled
	bg := themeWhite
	if pressing {
		bg = themePressBg
	} else if hover && !e.disabled {
		bg = themeHover
	}
	if e.disabled {
		bg = themeFaceDis
	}
	vector.DrawFilledRect(screen, bx, by, float32(bs), float32(bs), bg, false)
	vector.StrokeRect(screen, bx+0.5, by+0.5, float32(bs)-1, float32(bs)-1, 1, themeFrame, false)
	if e.checked {
		// チェック (2本線)。
		f := float64(bs)
		x0, y0 := float64(bx)+f*0.28, float64(by)+f*0.55
		x1, y1 := float64(bx)+f*0.45, float64(by)+f*0.72
		x2, y2 := float64(bx)+f*0.74, float64(by)+f*0.30
		c := themeInk
		if e.disabled {
			c = themeGrey
		}
		vector.StrokeLine(screen, float32(x0), float32(y0), float32(x1), float32(y1), 2, c, false)
		vector.StrokeLine(screen, float32(x1), float32(y1), float32(x2), float32(y2), 2, c, false)
	}
	if e.label == "" || face == nil {
		return
	}
	fg := themeInk
	if e.disabled {
		fg = themeGrey
	}
	tx := float64(e.gx + bs + 6)
	m := face.Metrics()
	th := m.HAscent + m.HDescent
	ty := float64(e.gy) + (float64(e.gh)-th)/2
	drawFaceText(screen, e.label, face, tx, ty, fg, gs)
}

func drawList(screen *ebiten.Image, e *widgetEntry, face *text.GoTextFace, gs gdiSnap, lineH float64, mx, my int, down bool) {
	x, y, w, h := float32(e.gx), float32(e.gy), float32(e.gw), float32(e.gh)
	vector.DrawFilledRect(screen, x, y, w, h, themeWhite, false)
	vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, themeFrame, false)
	rh := int(lineH + 0.5)
	if rh < 12 {
		rh = 12
	}
	vis := e.gh / rh
	if vis < 1 {
		vis = 1
	}
	// あふれたときだけバー幅を確保し、それ以外は枠横幅いっぱいを使う。
	needScroll := len(e.items) > vis
	sbW := 12
	innerW := e.gw - 2
	if needScroll {
		innerW = e.gw - sbW
	}
	if innerW < 0 {
		innerW = 0
	}
	// 項目。選択・ホバーの青は枠と被らないよう内側に収める。
	for i := 0; i < vis; i++ {
		idx := e.scroll + i
		if idx < 0 || idx >= len(e.items) {
			break
		}
		ry := e.gy + i*rh
		// 内側にクリップ: 上下の枠 (1px) に掛からないよう。
		y0 := ry
		y1 := ry + rh
		if y0 < e.gy+1 {
			y0 = e.gy + 1
		}
		if y1 > e.gy+e.gh-1 {
			y1 = e.gy + e.gh - 1
		}
		bg := themeWhite
		if idx == e.selIdx {
			bg = themePressBg
		} else if y1 > y0 && ptIn(mx, my, e.gx+1, y0, innerW, y1-y0) && !e.disabled {
			bg = themeFocusBg
		}
		if bg != themeWhite && y1 > y0 {
			vector.DrawFilledRect(screen, float32(e.gx+1), float32(y0), float32(innerW), float32(y1-y0), bg, false)
		}
		if face == nil {
			continue
		}
		fg := themeInk
		if e.disabled {
			fg = themeGrey
		}
		m := face.Metrics()
		th := m.HAscent + m.HDescent
		// はみ出しは SubImage で切る代わりに、そのまま描く (枠外は枠で隠れないが、
		// 短縮済みのため収まるはず)。
		drawFaceText(screen, e.items[idx], face, float64(e.gx+1+2), float64(ry)+(float64(rh)-th)/2, fg, gs)
	}
	// スクロールバー。
	n := len(e.items)
	if n > vis {
		thumbH, thumbY := listThumb(e.gh, n, vis, e.scroll, rh)
		// 溝。
		vector.DrawFilledRect(screen, float32(e.gx+e.gw-sbW), float32(e.gy), float32(sbW), float32(e.gh), themeFaceDis, false)
		hoverThumb := ptIn(mx, my, e.gx+e.gw-sbW, e.gy+thumbY, sbW, thumbH)
		c := themeScrollThumb
		if e.dragging {
			c = themeScrollThumbPress
		} else if hoverThumb {
			c = themeScrollThumbHover
		}
		vector.DrawFilledRect(screen, float32(e.gx+e.gw-sbW), float32(e.gy+thumbY), float32(sbW), float32(thumbH), c, false)
		vector.StrokeRect(screen, float32(e.gx+e.gw-sbW)+0.5, float32(e.gy+thumbY)+0.5, float32(sbW)-1, float32(thumbH)-1, 1, themeScrollThumbBorder, false)
	}
	// 選択・溝が枠に被らないよう枠を描き直す。
	vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, themeFrame, false)
}

func drawComboClosed(screen *ebiten.Image, e *widgetEntry, face *text.GoTextFace, gs gdiSnap, mx, my int, down bool, targets map[int]*ebiten.Image) {
	x, y, w, h := float32(e.gx), float32(e.gy), float32(e.gw), float32(e.gh)
	hover := ptIn(mx, my, e.gx, e.gy, e.gw, e.gh)
	pressing := (hover && down) || e.expanded
	bg := themeWhite
	if e.disabled {
		bg = themeFaceDis
	} else if pressing {
		bg = themePressBg
	} else if hover {
		bg = themeHover
	}
	vector.DrawFilledRect(screen, x, y, w, h, bg, false)
	vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, themeFrame, false)
	sel := ""
	if e.selIdx >= 0 && e.selIdx < len(e.items) {
		sel = e.items[e.selIdx]
	}
	if sel != "" && face != nil {
		fg := themeInk
		if e.disabled {
			fg = themeGrey
		}
		m := face.Metrics()
		th := m.HAscent + m.HDescent
		// 矢印分を空けて短縮済みのはず。
		drawFaceText(screen, sel, face, float64(e.gx+4), float64(e.gy)+(float64(e.gh)-th)/2, fg, gs)
	}
	// 矢印 (▼相当の三角)。
	ax := float32(e.gx+e.gw-16)
	ay := float32(e.gy + e.gh/2)
	sz := float32(5)
	var p vector.Path
	p.MoveTo(ax-sz, ay-3)
	p.LineTo(ax+sz, ay-3)
	p.LineTo(ax, ay+3)
	p.Close()
	fg := themeInk
	if e.disabled {
		fg = themeGrey
	}
	var cs ebiten.ColorScale
	cs.Reset()
	cs.ScaleWithColor(fg)
	vector.FillPath(screen, &p, &vector.FillOptions{}, &vector.DrawPathOptions{ColorScale: cs})
	_ = targets
}

func drawComboDrop(screen *ebiten.Image, e *widgetEntry, face *text.GoTextFace, gs gdiSnap, lineH float64, mx, my int) {
	rh := e.gh
	if rh < 12 {
		rh = 16
	}
	maxRows := 4
	n := len(e.items)
	rows := n
	if rows > maxRows {
		rows = maxRows
	}
	if rows < 1 {
		return
	}
	dx, dy, dw, dh := e.gx, e.gy+e.gh, e.gw, rows*rh
	vector.DrawFilledRect(screen, float32(dx), float32(dy), float32(dw), float32(dh), themeWhite, false)
	vector.StrokeRect(screen, float32(dx)+0.5, float32(dy)+0.5, float32(dw)-1, float32(dh)-1, 1, themeFrame, false)
	for i := 0; i < rows; i++ {
		idx := e.dropScroll + i
		if idx < 0 || idx >= n {
			break
		}
		ry := dy + i*rh
		// 内側にクリップ: 上下の枠 (1px) に掛からないよう。
		y0 := ry
		y1 := ry + rh
		if y0 < dy+1 {
			y0 = dy + 1
		}
		if y1 > dy+dh-1 {
			y1 = dy + dh - 1
		}
		bg := themeWhite
		hoverW := dw - 2
		if n > rows {
			hoverW = dw - 2 - 10
		}
		if hoverW < 0 {
			hoverW = 0
		}
		if idx == e.selIdx {
			bg = themePressBg
		} else if y1 > y0 && ptIn(mx, my, dx+1, y0, hoverW, y1-y0) {
			bg = themeFocusBg
		}
		if bg != themeWhite && y1 > y0 {
			selW := dw - 2
			if n > rows {
				selW = dw - 2 - 10
			}
			if selW < 0 {
				selW = 0
			}
			vector.DrawFilledRect(screen, float32(dx+1), float32(y0), float32(selW), float32(y1-y0), bg, false)
		}
		if face == nil {
			continue
		}
		m := face.Metrics()
		th := m.HAscent + m.HDescent
		drawFaceText(screen, e.items[idx], face, float64(dx+4), float64(ry)+(float64(rh)-th)/2, themeInk, gs)
	}
	// ドロップのスクロールバー (簡易)。
	if n > rows {
		sbW := 10
		thumbH, thumbY := listThumb(dh, n, rows, e.dropScroll, rh)
		vector.DrawFilledRect(screen, float32(dx+dw-sbW), float32(dy), float32(sbW), float32(dh), themeFaceDis, false)
		vector.DrawFilledRect(screen, float32(dx+dw-sbW), float32(dy+thumbY), float32(sbW), float32(thumbH), themeScrollThumb, false)
	}
	// 選択・溝が枠に被らないよう枠を描き直す。
	vector.StrokeRect(screen, float32(dx)+0.5, float32(dy)+0.5, float32(dw)-1, float32(dh)-1, 1, themeFrame, false)
}

func drawArea(screen *ebiten.Image, e *widgetEntry, face *text.GoTextFace, gs gdiSnap, lineH float64) {
	x, y, w, h := float32(e.gx), float32(e.gy), float32(e.gw), float32(e.gh)
	vector.DrawFilledRect(screen, x, y, w, h, themeWhite, false)
	vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, themeFrame, false)
	rh := int(lineH + 0.5)
	if rh < 12 {
		rh = 12
	}
	vis := e.gh / rh
	if vis < 1 {
		vis = 1
	}
	if face == nil {
		return
	}
	m := face.Metrics()
	th := m.HAscent + m.HDescent
	for i := 0; i < vis; i++ {
		idx := e.scroll + i
		if idx < 0 || idx >= len(e.wrapped) {
			break
		}
		ry := float32(e.gy + i*rh)
		drawFaceText(screen, e.wrapped[idx], face, float64(e.gx+4), float64(ry)+(float64(rh)-th)/2, themeInk, gs)
	}
	n := len(e.wrapped)
	if n > vis {
		sbW := 12
		thumbH, thumbY := listThumb(e.gh, n, vis, e.scroll, rh)
		vector.DrawFilledRect(screen, float32(e.gx+e.gw-sbW), float32(e.gy), float32(sbW), float32(e.gh), themeFaceDis, false)
		vector.DrawFilledRect(screen, float32(e.gx+e.gw-sbW), float32(e.gy+thumbY), float32(sbW), float32(thumbH), themeScrollThumb, false)
		vector.StrokeRect(screen, float32(e.gx+e.gw-sbW)+0.5, float32(e.gy+thumbY)+0.5, float32(sbW)-1, float32(thumbH)-1, 1, themeScrollThumbBorder, false)
		// 溝が枠に被らないよう枠を描き直す。
		vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, themeFrame, false)
	}
}

// ---------- ダイアログ (自前描画モーダル) ----------

// Dialog はモーダルな選択を表示し、応答があるまでブロックする。
// mode: 0 -> OK、1 -> Yes/No（はい・いいえ）。
// 戻り値は bool（OK=true、Yes=true/No=false）。
func (b *WindowBackend) Dialog(msg string, mode int) (bool, error) {
	var labels []string
	var values []bool
	switch mode {
	case 0:
		labels, values = []string{"OK"}, []bool{true}
	case 1:
		labels, values = []string{"Yes", "No"}, []bool{true, false}
	default:
		return false, fmt.Errorf("dialog：不明なモード %d です（0/1 が必要です）", mode)
	}
	b.mu.Lock()
	if b.dlg != nil && b.dlg.active {
		b.mu.Unlock()
		return false, fmt.Errorf("dialog：既に表示中です")
	}
	ww, wh := b.w, b.h
	face, lineH := b.face, b.lineH
	if lineH <= 0 {
		lineH = lineHeight
	}
	gs := b.gdiSnapLocked()
	b.mu.Unlock()
	maxW := float64(ww) - 128
	if maxW < 200 {
		maxW = 200
	}
	lines := wrapDialogText(msg, face, maxW, gs)
	needW := 0.0
	for _, ln := range lines {
		if w := measureWithSnap(ln, face, gs); w > needW {
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
	wx := (ww - bw) / 2
	wy := (wh - bh) / 2
	// ボタン配置: 下部中央。
	btnW, btnH, gap := 96, 30, 12
	total := len(labels)*btnW + (len(labels)-1)*gap
	bx := wx + (bw-total)/2
	by := wy + bh - btnH - 18
	rects := make([]image.Rectangle, len(labels))
	for i := range labels {
		rects[i] = image.Rect(bx+i*(btnW+gap), by, bx+i*(btnW+gap)+btnW, by+btnH)
	}
	res := make(chan bool, 1)
	b.mu.Lock()
	// モーダル化: 背後のウィジェット状態を凍結し、ダイアログ前の
	// 入力ラッチを捨てる (表示中のタイプ・クリックが閉じた後に
	// 背後へ流れ込まないようにする)。
	for _, e := range b.widgets {
		e.expanded = false
		e.dragging = false
		e.downInside = false
		if e.edit != nil {
			e.edit.repKey = 0
		}
	}
	for i := range b.clickFlag {
		b.clickFlag[i] = false
	}
	for k := range b.keyHit {
		b.keyHit[k] = false
	}
	b.wheelAccum = 0
	b.imePending = ""
	b.imeComposing = ""
	b.imeClause = ""
	b.imeClauseStart, b.imeClauseEnd = 0, 0
	b.charSt = charRep{}
	b.dlg = &dialogState{
		active: true, lines: lines,
		labels: append([]string(nil), labels...),
		values: append([]bool(nil), values...),
		winRect: image.Rect(wx, wy, wx+bw, wy+bh),
		btnRects: rects, result: res,
	}
	b.mu.Unlock()
	return <-res, nil
}

func (b *WindowBackend) pumpDialogLocked(mx, my int, justPressed bool) {
	d := b.dlg
	if d == nil || !d.active {
		return
	}
	if !justPressed {
		return
	}
	for i, r := range d.btnRects {
		if ptIn(mx, my, r.Min.X, r.Min.Y, r.Dx(), r.Dy()) {
			v := d.values[i]
			d.active = false
			select {
			case d.result <- v:
			default:
			}
			return
		}
	}
}

func (b *WindowBackend) drawDialog(screen *ebiten.Image) {
	b.mu.Lock()
	d := b.dlg
	if d == nil || !d.active {
		b.mu.Unlock()
		return
	}
	// スナップショット。
	lines := append([]string(nil), d.lines...)
	labels := append([]string(nil), d.labels...)
	win := d.winRect
	btns := append([]image.Rectangle(nil), d.btnRects...)
	face, lineH := b.face, b.lineH
	gs := b.gdiSnapLocked()
	b.mu.Unlock()
	// 背景の薄暗化はしない (要望により dim なし): ダイアログ窓だけを描く。
	//  window.
	vector.DrawFilledRect(screen, float32(win.Min.X), float32(win.Min.Y), float32(win.Dx()), float32(win.Dy()), themeWhite, false)
	vector.StrokeRect(screen, float32(win.Min.X)+0.5, float32(win.Min.Y)+0.5, float32(win.Dx())-1, float32(win.Dy())-1, 1, themeFrame, false)
	mx, my := ebiten.CursorPosition()
	down := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	if face != nil {
		if lineH <= 0 {
			lineH = lineHeight
		}
		ty := float64(win.Min.Y + 18)
		for _, ln := range lines {
			drawFaceText(screen, ln, face, float64(win.Min.X+18), ty, themeInk, gs)
			ty += lineH
		}
	}
	for i, r := range btns {
		hover := ptIn(mx, my, r.Min.X, r.Min.Y, r.Dx(), r.Dy())
		bg := themeWhite
		if hover && down {
			bg = themePressBg
		} else if hover {
			bg = themeHover
		}
		vector.DrawFilledRect(screen, float32(r.Min.X), float32(r.Min.Y), float32(r.Dx()), float32(r.Dy()), bg, false)
		vector.StrokeRect(screen, float32(r.Min.X)+0.5, float32(r.Min.Y)+0.5, float32(r.Dx())-1, float32(r.Dy())-1, 1, themeFrame, false)
		if face != nil && i < len(labels) {
			tw := measureWithSnap(labels[i], face, gs)
			m := face.Metrics()
			th := m.HAscent + m.HDescent
			tx := float64(r.Min.X) + (float64(r.Dx())-tw)/2
			ty := float64(r.Min.Y) + (float64(r.Dy())-th)/2
			drawFaceText(screen, labels[i], face, tx, ty, themeInk, gs)
		}
	}
}

// isCJKBreakable はどこでも折り返して行を始められるルーンを報告する。
func isCJKBreakable(r rune) bool {
	switch {
	case r >= 0x3040 && r <= 0x30FF:
		return true
	case r >= 0x3400 && r <= 0x9FFF:
		return true
	case r >= 0xAC00 && r <= 0xD7AF:
		return true
	case r >= 0xFF00 && r <= 0xFFEF:
		return true
	}
	return false
}

// wrapDialogText は s を maxW ピクセルに収まる行に分割する。
func wrapDialogText(s string, face *text.GoTextFace, maxW float64, gs gdiSnap) []string {
	if face == nil {
		return strings.Split(s, "\n")
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		r := []rune(para)
		start := 0
		for start < len(r) {
			// 語境界を優先し、CJK はどこでも折る。
			best := -1
			for end := start + 1; end <= len(r); end++ {
				w := measureWithSnap(string(r[start:end]), face, gs)
				if w > maxW {
					break
				}
				best = end
				// スペース直後は区切り候補。
				if end < len(r) && r[end-1] == ' ' {
					// そのまま続けて最長を取る (語境界優先の簡易)。
				}
				_ = isCJKBreakable
			}
			if best <= start {
				best = start + 1
			}
			// 後ろのスペースは落とす。
			seg := strings.TrimRight(string(r[start:best]), " ")
			if seg == "" {
				// スペースのみの塊は飛ばす。
				for start < len(r) && r[start] == ' ' {
					start++
				}
				continue
			}
			lines = append(lines, seg)
			start = best
			for start < len(r) && r[start] == ' ' {
				start++
			}
		}
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// checkArtRGBA は size×size のチェックボックス面を素のピクセルに描く (テスト用に保持)。
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
