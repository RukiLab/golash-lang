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
	"math"
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
	wSlider
)

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
	// chkbox。
	checked bool
	// slider。minVal..maxVal の整数値。vert は縦向き。
	minVal        int
	maxVal        int
	value         int
	vert          bool
	dragStartAxis int
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

// スライダーのつまみ長 (px)。listbox/msgbox の最小つまみ長と同じ。
const sliderThumbLen = 20

// comboDropMaxRows は combox ドロップダウンの最大表示行数。
const comboDropMaxRows = 4

// ---------- テーマ (Windows 10 標準コントロール) ----------
//
// Windows 10 の Aero2 ビジュアルスタイルに合わせた固定色。数値は
// 96 DPI の標準コントロールから採取した値で、ハイコントラストや
// ダークテーマには追従しない (GUI は常にライトテーマ)。
var (
	// 入力欄・リスト・コンボの面。
	themeWhite = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	// ボタン・ダイアログの面 (Windows 10 の既定ボタン面)。
	themeFace          = color.NRGBA{0xF0, 0xF0, 0xF0, 0xFF}
	themeFrame         = color.NRGBA{0xAB, 0xAD, 0xB3, 0xFF} // 入力欄/リストの1px枠
	themeInk           = color.NRGBA{0x00, 0x00, 0x00, 0xFF}
	themeGrey          = color.NRGBA{0x6D, 0x6D, 0x6D, 0xFF} // 無効時の文字
	themeHover         = color.NRGBA{0xE5, 0xF1, 0xFB, 0xFF} // ホット時の面
	themeFocusBg       = themeWhite                          // 入力欄はフォーカスでも白のまま
	themeFocusFrame    = color.NRGBA{0x00, 0x78, 0xD7, 0xFF} // Windows 10 アクセント
	themePressBg       = color.NRGBA{0xCC, 0xE4, 0xF7, 0xFF}
	themePressFrame    = color.NRGBA{0x00, 0x54, 0x99, 0xFF}
	themeFaceDis       = color.NRGBA{0xF0, 0xF0, 0xF0, 0xFF} // 無効の入力欄
	themeButtonFaceDis = color.NRGBA{0xF5, 0xF5, 0xF5, 0xFF} // 無効のボタン

	// ボタンの外枠。Windows 10 のボタンは上下で陰影が付く。
	themeButtonFrame       = color.NRGBA{0xD0, 0xD0, 0xD0, 0xFF}
	themeButtonFrameBottom = color.NRGBA{0xBA, 0xBA, 0xBA, 0xFF}
	themeButtonHighlight   = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	themeFieldFrameDis     = color.NRGBA{0xD0, 0xD0, 0xD0, 0xFF}
	themeCheckFrame        = color.NRGBA{0x7A, 0x7A, 0x7A, 0xFF}

	// 選択行 (Windows 10 のアクセント青・白文字)。
	themeSelectBg  = color.NRGBA{0x00, 0x78, 0xD7, 0xFF}
	themeSelectInk = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}

	// スクロールバー (Windows 10: 灰色の溝と、枠なしのつまみ)。
	themeScrollTrack      = color.NRGBA{0xF0, 0xF0, 0xF0, 0xFF}
	themeScrollThumb      = color.NRGBA{0xCD, 0xCD, 0xCD, 0xFF}
	themeScrollThumbHover = color.NRGBA{0xA6, 0xA6, 0xA6, 0xFF}
	themeScrollThumbPress = color.NRGBA{0x81, 0x81, 0x81, 0xFF}
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

// comboDropRows は項目数 n に対する combox ドロップダウンの
// 表示行数を返す。
func comboDropRows(n int) int {
	if n > comboDropMaxRows {
		return comboDropMaxRows
	}
	if n < 0 {
		return 0
	}
	return n
}

// comboDropScrollFor はドロップダウンを開くときの先頭行を返す。
// 選択項目の1つ上を先頭にして見やすくしつつ、末尾側で空白行が
// 出ないよう n-rows に収める。
func comboDropScrollFor(sel, n int) int {
	rows := comboDropRows(n)
	if sel < 0 || rows < 1 {
		return 0
	}
	scroll := sel - 1
	if max := n - rows; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	return scroll
}

// clampComboDropScroll は先頭行を空白行が出ない範囲に収める。
func clampComboDropScroll(e *widgetEntry) {
	max := len(e.items) - comboDropRows(len(e.items))
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

// openComboLocked はドロップダウンを開き、選択が見える位置へ
// スクロールを合わせる。呼出し側は mu を保持すること。
func (b *WindowBackend) openComboLocked(e *widgetEntry) {
	e.expanded = true
	e.dropScroll = comboDropScrollFor(e.selIdx, len(e.items))
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

// areaWrapLocked は msgbox 本文を折り返す。
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
func (b *WindowBackend) AddButton(id int, label string, x, y, w, h int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("button のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	fitted := fitText(label, w-buttonPadX, b.TextWidth)
	b.mu.Lock()
	defer b.mu.Unlock()
	if old, ok := b.widgets[id]; ok && old.kind == wButton &&
		old.gx == x && old.gy == y && old.gw == w && old.gh == h &&
		old.rawLabel == label {
		b.advanceWidgetCursorLocked(y, h)
		return nil
	}
	if old, ok := b.widgets[id]; ok && old.kind == wButton {
		old.gx, old.gy, old.gw, old.gh = x, y, w, h
		old.label, old.rawLabel = fitted, label
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
		items:    append([]string(nil), fitted...),
		rawItems: rawCopy,
		selIdx:   -1,
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
		selIdx:  -1,
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

// AddSlider はスライダーを作成 (または置換) する。lo..hi が値域、
// value は初期値 (範囲外は丸める)。orient は 0=横、1=縦。つまみ長は
// 固定 (sliderThumbLen px)。同じ id で作り直したときは現在値を保つ
// (毎フレームの配置呼出しでドラッグ中の値が戻らないようにする)。
// 配置後は自動で改行する (x=0, y+h)。
func (b *WindowBackend) AddSlider(id, x, y, w, h, lo, hi, value, orient int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("slider のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	if hi < lo {
		return fmt.Errorf("slider の最大値 %d は最小値 %d 以上である必要があります", hi, lo)
	}
	if orient != 0 && orient != 1 {
		return fmt.Errorf("slider の向き %d は 0（横）または 1（縦）で指定してください", orient)
	}
	if value < lo {
		value = lo
	}
	if value > hi {
		value = hi
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if old, ok := b.widgets[id]; ok && old.kind == wSlider {
		// 毎フレーム呼ばれる配置でも、ユーザーが動かした値を維持する。
		old.gx, old.gy, old.gw, old.gh = x, y, w, h
		old.minVal, old.maxVal = lo, hi
		old.vert = orient == 1
		clampSliderValueLocked(old)
		b.advanceWidgetCursorLocked(y, h)
		return nil
	}
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	b.widgets[id] = &widgetEntry{
		id: id, kind: wSlider,
		gx: x, gy: y, gw: w, gh: h,
		minVal: lo, maxVal: hi, value: value,
		vert:   orient == 1,
		selIdx: -1,
	}
	b.advanceWidgetCursorLocked(y, h)
	return nil
}

// SliderValue は slider の現在値を返す。
func (b *WindowBackend) SliderValue(id int) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wSlider {
		return 0, fmt.Errorf("objget：不明な slider %d です", id)
	}
	return e.value, nil
}

// SetSliderValue は slider の値を変える。範囲外は丸める。
func (b *WindowBackend) SetSliderValue(id, value int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wSlider {
		return fmt.Errorf("objset：不明な slider %d です", id)
	}
	e.value = value
	clampSliderValueLocked(e)
	return nil
}

func clampSliderValueLocked(e *widgetEntry) {
	if e.value < e.minVal {
		e.value = e.minVal
	}
	if e.value > e.maxVal {
		e.value = e.maxVal
	}
}

// sliderTrackLen はつまみが動くトラックの長さ (px) を返す。
func sliderTrackLen(e *widgetEntry) int {
	if e.vert {
		return e.gh
	}
	return e.gw
}

// sliderThumbMetrics は現在値でのつまみ長と、トラック先頭からの位置を返す。
func sliderThumbMetrics(e *widgetEntry) (thumbLen, thumbPos int) {
	trackLen := sliderTrackLen(e)
	if trackLen <= 0 {
		return 0, 0
	}
	thumbLen = sliderThumbLen
	if thumbLen < 1 {
		thumbLen = 1
	}
	if thumbLen > trackLen {
		thumbLen = trackLen
	}
	span := e.maxVal - e.minVal
	if span <= 0 {
		return thumbLen, 0
	}
	free := trackLen - thumbLen
	pos := int(math.Round(float64(free) * float64(e.value-e.minVal) / float64(span)))
	if pos < 0 {
		pos = 0
	}
	if pos > free {
		pos = free
	}
	return thumbLen, pos
}

// sliderValueAt はトラック先頭から thumbStart px の位置に対応する値を返す。
// 通常スライダーのトラッククリック (ジャンプ) で使う。
func sliderValueAt(e *widgetEntry, thumbStart int) int {
	trackLen := sliderTrackLen(e)
	if trackLen <= 0 {
		return e.value
	}
	thumbLen, _ := sliderThumbMetrics(e)
	free := trackLen - thumbLen
	if free <= 0 {
		return e.value
	}
	span := float64(e.maxVal) - float64(e.minVal)
	if span <= 0 {
		return e.value
	}
	rel := thumbStart
	if rel < 0 {
		rel = 0
	}
	if rel > free {
		rel = free
	}
	v := float64(e.minVal) + span*float64(rel)/float64(free)
	if v < float64(e.minVal) {
		v = float64(e.minVal)
	}
	if v > float64(e.maxVal) {
		v = float64(e.maxVal)
	}
	return int(math.Round(v))
}

// sliderValueByDelta はドラッグ開始値から delta px 動かしたときの値を返す。
// 掴んだだけでは値が動かない (つまみ位置の丸めで値が飛ばない) ように、
// 絶対位置ではなく差分で換算する。
func sliderValueByDelta(e *widgetEntry, delta int) int {
	trackLen := sliderTrackLen(e)
	if trackLen <= 0 {
		return e.value
	}
	thumbLen, _ := sliderThumbMetrics(e)
	free := trackLen - thumbLen
	if free <= 0 {
		return e.value
	}
	span := float64(e.maxVal) - float64(e.minVal)
	if span <= 0 {
		return e.value
	}
	v := float64(e.dragStart) + span*float64(delta)/float64(free)
	if v < float64(e.minVal) {
		v = float64(e.minVal)
	}
	if v > float64(e.maxVal) {
		v = float64(e.maxVal)
	}
	return int(math.Round(v))
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
		return "msgbox", nil
	case wSlider:
		return "slider", nil
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
	clampComboDropScroll(e)
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
		items:    append([]string(nil), items...),
		rawItems: append([]string(nil), items...),
		selIdx:   -1,
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
		return fmt.Errorf("msgbox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
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
		cached:      text,
		selIdx:      -1,
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
		return "", fmt.Errorf("objget：不明な msgbox %d です", id)
	}
	return e.cached, nil
}

// SetAreaText は内容を置き換える。下端張付きなら末尾に留まる。
func (b *WindowBackend) SetAreaText(id int, text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wArea {
		return fmt.Errorf("objset：不明な msgbox %d です", id)
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
	if e.disabled {
		// 無効化中にドラッグ状態が残らないようにする。
		e.dragging = false
		if e.edit != nil {
			e.edit.focused = false
		}
	}
	return nil
}

// Enabled はウィジェットが有効か報告する。
func (b *WindowBackend) Enabled(id int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok {
		return false, fmt.Errorf("objenable：不明なウィジェット %d です", id)
	}
	return !e.disabled, nil
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
		case wSlider:
			b.pumpSliderLocked(e, mx, my, justPressed, justReleased, down)
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
						// 開いたら選択が見える位置へ。
						b.openComboLocked(e)
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
	sbW := listSliderW
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
	sbW := listSliderW
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

func (b *WindowBackend) pumpSliderLocked(e *widgetEntry, mx, my int, justPressed, justReleased, down bool) {
	_ = justReleased
	if justPressed && ptIn(mx, my, e.gx, e.gy, e.gw, e.gh) {
		axis, trackStart := mx, e.gx
		if e.vert {
			axis, trackStart = my, e.gy
		}
		thumbLen, thumbPos := sliderThumbMetrics(e)
		if axis >= trackStart+thumbPos && axis < trackStart+thumbPos+thumbLen {
			// つまみを掴んだ: 掴んだ位置のずれを保つ。
			e.dragStart = e.value
			e.dragStartAxis = axis
			e.dragging = e.maxVal > e.minVal
		} else {
			// 溝のクリック: クリック位置へジャンプしてドラッグ。
			e.dragStart = sliderValueAt(e, axis-trackStart-thumbLen/2)
			e.value = e.dragStart
			e.dragStartAxis = axis
			e.dragging = e.maxVal > e.minVal
		}
	}
	if !e.dragging {
		return
	}
	if !down {
		e.dragging = false
		return
	}
	axis := mx
	if e.vert {
		axis = my
	}
	e.value = sliderValueByDelta(e, axis-e.dragStartAxis)
}

func (b *WindowBackend) pumpComboExpandedLocked(e *widgetEntry, mx, my int, justPressed, justReleased, down bool, wy float64) {
	rh := e.gh
	if rh < 8 {
		rh = 16
	}
	n := len(e.items)
	rows := comboDropRows(n)
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
	// ドロップダウンのスクロールバー (drawComboDrop と同じ幅)。
	// listbox/msgbox と同様にドラッグとトラッククリックでスクロールし、
	// 閉じたり項目選択したりしない。
	comboSbW := listSliderW
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
		cp.wrapped = append([]string(nil), e.wrapped...)
		ss = append(ss, snap{e: cp, face: b.face, faceSize: b.fontSize, lineH: b.lineH, gdi: b.gdiSnapLocked()})
	}
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
			drawButton(screen, e, ss[i].face, ss[i].gdi, mx, my, down)
		case wCheck:
			drawCheck(screen, e, ss[i].face, ss[i].gdi, ss[i].lineH, mx, my, down)
		case wSlider:
			drawSlider(screen, e, mx, my)
		case wList:
			drawList(screen, e, ss[i].face, ss[i].gdi, ss[i].lineH, mx, my, down)
		case wCombo:
			// 閉じた分だけここで描く。展開ドロップは後で前面に。
			drawComboClosed(screen, e, ss[i].face, ss[i].gdi, mx, my, down)
		case wArea:
			drawArea(screen, e, ss[i].face, ss[i].gdi, ss[i].lineH, mx, my)
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

// drawButtonChrome は Windows 10 のプッシュボタン面を描く。外周1pxの
// 枠、その内側1pxの白ハイライト、平坦な面という構造で、上下の枠色を
// 変えて軽い浮き上がりを付ける。hover/pressed の色は呼出側が選ぶ。
func drawButtonChrome(screen *ebiten.Image, x, y, w, h int, bg, bd, bottom color.NRGBA, highlight bool) {
	if w <= 0 || h <= 0 {
		return
	}
	xf, yf, wf, hf := float32(x), float32(y), float32(w), float32(h)
	vector.DrawFilledRect(screen, xf, yf, wf, hf, bg, false)
	// 上・左・右が明るく、下が暗い Windows 10 のボタン枠。
	vector.DrawFilledRect(screen, xf, yf, wf, 1, bd, false)
	vector.DrawFilledRect(screen, xf, yf+hf-1, wf, 1, bottom, false)
	vector.DrawFilledRect(screen, xf, yf, 1, hf, bd, false)
	vector.DrawFilledRect(screen, xf+wf-1, yf, 1, hf, bd, false)
	if highlight && w > 4 && h > 4 {
		vector.StrokeRect(screen, xf+1.5, yf+1.5, wf-3, hf-3, 1, themeButtonHighlight, false)
	}
}

func drawButton(screen *ebiten.Image, e *widgetEntry, face *text.GoTextFace, gs gdiSnap, mx, my int, down bool) {
	hover := ptIn(mx, my, e.gx, e.gy, e.gw, e.gh)
	pressing := hover && down && !e.disabled
	bg, bd, bottom := themeFace, themeButtonFrame, themeButtonFrameBottom
	highlight := true
	if e.disabled {
		bg, bd, bottom = themeButtonFaceDis, themeButtonFrame, themeButtonFrame
		highlight = false
	} else if pressing {
		bg, bd, bottom = themePressBg, themePressFrame, themePressFrame
		highlight = false
	} else if hover {
		bg, bd, bottom = themeHover, themeFocusFrame, themeFocusFrame
	}
	drawButtonChrome(screen, e.gx, e.gy, e.gw, e.gh, bg, bd, bottom, highlight)
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
	// Windows 10 の標準チェックボックスは 13px 角。16px フォントの
	// 行に合わせて 14px にしている (13px だと小さく見えるため)。
	const bs = 14
	bx, by := float32(e.gx), float32(e.gy)+(float32(e.gh)-float32(bs))/2
	hover := ptIn(mx, my, e.gx, e.gy, e.gw, e.gh)
	pressing := hover && down && !e.disabled
	bg, bd := themeWhite, themeCheckFrame
	mark := themeInk
	if e.checked {
		bg, bd, mark = themeSelectBg, themeSelectBg, themeSelectInk
	}
	if e.disabled {
		bg, bd, mark = themeButtonFaceDis, themeButtonFrame, themeGrey
	} else if pressing {
		if e.checked {
			bg, bd = themePressFrame, themePressFrame
		} else {
			bg, bd = themePressBg, themePressFrame
		}
	} else if hover {
		bd = themeFocusFrame
		if e.checked {
			bg = themeFocusFrame
		}
	}
	vector.DrawFilledRect(screen, bx, by, float32(bs), float32(bs), bg, false)
	vector.StrokeRect(screen, bx+0.5, by+0.5, float32(bs)-1, float32(bs)-1, 1, bd, false)
	if e.checked {
		// チェック (2本線)。
		f := float64(bs)
		x0, y0 := float64(bx)+f*0.25, float64(by)+f*0.52
		x1, y1 := float64(bx)+f*0.43, float64(by)+f*0.70
		x2, y2 := float64(bx)+f*0.75, float64(by)+f*0.30
		vector.StrokeLine(screen, float32(x0), float32(y0), float32(x1), float32(y1), 2, mark, false)
		vector.StrokeLine(screen, float32(x1), float32(y1), float32(x2), float32(y2), 2, mark, false)
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

// drawScrollbar は listbox/msgbox/combox と slider が共有する溝とつまみを
// 描く。vertical のとき thumbPos は上端、そうでなければ左端 (トラック先頭
// からの相対位置)。色は固定テーマの灰色で、ホバー・押下だけ濃くなる。
func drawScrollbar(screen *ebiten.Image, x, y, w, h int, vertical bool, thumbPos, thumbLen int, hover, press bool) {
	if w <= 0 || h <= 0 {
		return
	}
	vector.DrawFilledRect(screen, float32(x), float32(y), float32(w), float32(h), themeScrollTrack, false)
	c := themeScrollThumb
	if press {
		c = themeScrollThumbPress
	} else if hover {
		c = themeScrollThumbHover
	}
	if vertical {
		vector.DrawFilledRect(screen, float32(x), float32(y+thumbPos), float32(w), float32(thumbLen), c, false)
	} else {
		vector.DrawFilledRect(screen, float32(x+thumbPos), float32(y), float32(thumbLen), float32(h), c, false)
	}
}

func drawSlider(screen *ebiten.Image, e *widgetEntry, mx, my int) {
	if e.gw <= 0 || e.gh <= 0 {
		return
	}
	thumbLen, thumbPos := sliderThumbMetrics(e)
	var hover bool
	if e.vert {
		hover = ptIn(mx, my, e.gx, e.gy+thumbPos, e.gw, thumbLen)
	} else {
		hover = ptIn(mx, my, e.gx+thumbPos, e.gy, thumbLen, e.gh)
	}
	drawScrollbar(screen, e.gx, e.gy, e.gw, e.gh, e.vert, thumbPos, thumbLen, !e.disabled && hover, e.dragging)
}

func drawList(screen *ebiten.Image, e *widgetEntry, face *text.GoTextFace, gs gdiSnap, lineH float64, mx, my int, down bool) {
	x, y, w, h := float32(e.gx), float32(e.gy), float32(e.gw), float32(e.gh)
	body := themeWhite
	frame := themeFrame
	if e.disabled {
		body, frame = themeFaceDis, themeFieldFrameDis
	}
	vector.DrawFilledRect(screen, x, y, w, h, body, false)
	vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, frame, false)
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
	sbW := listSliderW
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
		selected := idx == e.selIdx
		bg := themeWhite
		fg := themeInk
		if e.disabled {
			bg = themeFaceDis
			fg = themeGrey
		} else if selected {
			bg, fg = themeSelectBg, themeSelectInk
		} else if y1 > y0 && ptIn(mx, my, e.gx+1, y0, innerW, y1-y0) {
			bg = themeHover
		}
		if bg != themeWhite && y1 > y0 {
			vector.DrawFilledRect(screen, float32(e.gx+1), float32(y0), float32(innerW), float32(y1-y0), bg, false)
		}
		if face == nil {
			continue
		}
		m := face.Metrics()
		th := m.HAscent + m.HDescent
		// はみ出しは SubImage で切る代わりに、そのまま描く (枠外は枠で隠れないが、
		// 短縮済みのため収まるはず)。
		drawFaceText(screen, e.items[idx], face, float64(e.gx+1+2), float64(ry)+(float64(rh)-th)/2, fg, gs)
	}
	// スクロールバー。listbox/msgbox/slider と共通の溝＋つまみ。
	n := len(e.items)
	if n > vis {
		thumbH, thumbY := listThumb(e.gh, n, vis, e.scroll, rh)
		hoverThumb := !e.disabled && ptIn(mx, my, e.gx+e.gw-sbW, e.gy+thumbY, sbW, thumbH)
		drawScrollbar(screen, e.gx+e.gw-sbW, e.gy, sbW, e.gh, true, thumbY, thumbH, hoverThumb, e.dragging)
	}
	// 選択・溝が枠に被らないよう枠を描き直す。
	vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, frame, false)
}

func drawComboClosed(screen *ebiten.Image, e *widgetEntry, face *text.GoTextFace, gs gdiSnap, mx, my int, down bool) {
	x, y, w, h := float32(e.gx), float32(e.gy), float32(e.gw), float32(e.gh)
	hover := ptIn(mx, my, e.gx, e.gy, e.gw, e.gh)
	pressing := (hover && down) || e.expanded
	body, frame := themeWhite, themeFrame
	arrow := themeGrey
	if e.disabled {
		body, frame, arrow = themeFaceDis, themeFieldFrameDis, themeGrey
	} else if pressing {
		frame = themePressFrame
	} else if hover {
		frame = themeFocusFrame
	}
	vector.DrawFilledRect(screen, x, y, w, h, body, false)
	vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, frame, false)
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
	// Windows 10 のコンボボックスは塗りつぶし三角ではなく細い V 字。
	// 2本の StrokeLine に分けると左腕だけ1px高くラスタライズされて
	// 矢印が傾いて見えるため、1本のパスとして描く。
	ax := float32(e.gx + e.gw - 14)
	ay := float32(e.gy) + float32(e.gh)/2
	var chevron vector.Path
	chevron.MoveTo(ax-4, ay-2)
	chevron.LineTo(ax, ay+2)
	chevron.LineTo(ax+4, ay-2)
	var arrowCS ebiten.ColorScale
	arrowCS.ScaleWithColor(arrow)
	vector.StrokePath(screen, &chevron,
		&vector.StrokeOptions{Width: 2, LineCap: vector.LineCapButt, LineJoin: vector.LineJoinMiter, MiterLimit: 10},
		&vector.DrawPathOptions{AntiAlias: true, ColorScale: arrowCS})
}

func drawComboDrop(screen *ebiten.Image, e *widgetEntry, face *text.GoTextFace, gs gdiSnap, lineH float64, mx, my int) {
	rh := e.gh
	if rh < 12 {
		rh = 16
	}
	n := len(e.items)
	rows := comboDropRows(n)
	if rows < 1 {
		return
	}
	// 状態が範囲外でも空白行が出ないよう、描画側でも先頭行を丸める。
	scroll := e.dropScroll
	if max := n - rows; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	dx, dy, dw, dh := e.gx, e.gy+e.gh, e.gw, rows*rh
	vector.DrawFilledRect(screen, float32(dx), float32(dy), float32(dw), float32(dh), themeWhite, false)
	vector.StrokeRect(screen, float32(dx)+0.5, float32(dy)+0.5, float32(dw)-1, float32(dh)-1, 1, themeFrame, false)
	sbW := listSliderW
	for i := 0; i < rows; i++ {
		idx := scroll + i
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
		selW := dw - 2
		if n > rows {
			selW = dw - 2 - sbW
		}
		if selW < 0 {
			selW = 0
		}
		bg := themeWhite
		fg := themeInk
		if idx == e.selIdx {
			bg, fg = themeSelectBg, themeSelectInk
		} else if y1 > y0 && ptIn(mx, my, dx+1, y0, selW, y1-y0) {
			bg = themeHover
		}
		if bg != themeWhite && y1 > y0 {
			vector.DrawFilledRect(screen, float32(dx+1), float32(y0), float32(selW), float32(y1-y0), bg, false)
		}
		if face == nil {
			continue
		}
		m := face.Metrics()
		th := m.HAscent + m.HDescent
		drawFaceText(screen, e.items[idx], face, float64(dx+4), float64(ry)+(float64(rh)-th)/2, fg, gs)
	}
	// ドロップのスクロールバー。listbox/msgbox/slider と共通。
	if n > rows {
		thumbH, thumbY := listThumb(dh, n, rows, scroll, rh)
		barX := dx + dw - sbW
		hoverThumb := !e.disabled && ptIn(mx, my, barX, dy+thumbY, sbW, thumbH)
		drawScrollbar(screen, barX, dy, sbW, dh, true, thumbY, thumbH, hoverThumb, e.dragging)
	}
	// 選択・溝が枠に被らないよう枠を描き直す。
	vector.StrokeRect(screen, float32(dx)+0.5, float32(dy)+0.5, float32(dw)-1, float32(dh)-1, 1, themeFrame, false)
}

func drawArea(screen *ebiten.Image, e *widgetEntry, face *text.GoTextFace, gs gdiSnap, lineH float64, mx, my int) {
	x, y, w, h := float32(e.gx), float32(e.gy), float32(e.gw), float32(e.gh)
	body, frame, ink := themeWhite, themeFrame, themeInk
	if e.disabled {
		body, frame, ink = themeFaceDis, themeFieldFrameDis, themeGrey
	}
	vector.DrawFilledRect(screen, x, y, w, h, body, false)
	vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, frame, false)
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
		drawFaceText(screen, e.wrapped[idx], face, float64(e.gx+4), float64(ry)+(float64(rh)-th)/2, ink, gs)
	}
	n := len(e.wrapped)
	if n > vis {
		sbW := listSliderW
		thumbH, thumbY := listThumb(e.gh, n, vis, e.scroll, rh)
		hoverThumb := !e.disabled && ptIn(mx, my, e.gx+e.gw-sbW, e.gy+thumbY, sbW, thumbH)
		drawScrollbar(screen, e.gx+e.gw-sbW, e.gy, sbW, e.gh, true, thumbY, thumbH, hoverThumb, e.dragging)
		// 溝が枠に被らないよう枠を描き直す。
		vector.StrokeRect(screen, x+0.5, y+0.5, w-1, h-1, 1, frame, false)
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
		b.releaseFlag[i] = false
	}
	for k := range b.keyHit {
		b.keyHit[k] = false
	}
	for k := range b.keyRel {
		b.keyRel[k] = false
	}
	for k := range b.padHit {
		b.padHit[k] = false
	}
	for k := range b.padRel {
		b.padRel[k] = false
	}
	b.wheelAccum = 0
	b.imePending = ""
	b.imeComposing = ""
	b.imeClause = ""
	b.imeClauseStart, b.imeClauseEnd = 0, 0
	b.charSt = charRep{}
	b.dlg = &dialogState{
		active: true, lines: lines,
		labels:   append([]string(nil), labels...),
		values:   append([]bool(nil), values...),
		winRect:  image.Rect(wx, wy, wx+bw, wy+bh),
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
	// Windows 10 のダイアログと同じ、灰色の面と 1px の枠。
	vector.DrawFilledRect(screen, float32(win.Min.X), float32(win.Min.Y), float32(win.Dx()), float32(win.Dy()), themeFace, false)
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
		bg, bd, bottom := themeFace, themeButtonFrame, themeButtonFrameBottom
		highlight := true
		if hover && down {
			bg, bd, bottom = themePressBg, themePressFrame, themePressFrame
			highlight = false
		} else if hover {
			bg, bd, bottom = themeHover, themeFocusFrame, themeFocusFrame
		}
		if i == 0 {
			// 最初のボタン (OK/はい) は Windows 10 の既定ボタン。
			if !(hover && down) {
				bd, bottom = themeFocusFrame, themeFocusFrame
			}
		}
		drawButtonChrome(screen, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), bg, bd, bottom, highlight)
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
