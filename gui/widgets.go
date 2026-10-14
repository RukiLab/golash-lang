package gui

import (
	"fmt"
	"image"
	"image/color"
	"slices"
	"strings"
	"sync"

	"github.com/ebitenui/ebitenui"
	euimage "github.com/ebitenui/ebitenui/image"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// ウィジェット（G3）を ebitenui 上に: ボタン、テキスト入力、リスト、
// モーダルダイアログ。スクリプトスレッドは変更をエンキューし、
// ゲームスレッドが適用する。HSP 風の絶対位置決めを fixedLayout で行う。

// fixedLayout は子を正確な矩形（HSP 風の座標）に配置する。
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
	// disabled は操作を凍結する（objprm "enable"。mu で保護）。
	disabled bool
	// ボタンのラッチ＋リスト状態（バックエンドの mu で保護）。
	pressed bool
	items   []string
	selIdx  int
	// listW は listbox が配置された幅。listbox_set は幅を持たないため、
	// 項目はこれに合わせて収められる。代わりにウィジェットの矩形を
	// 読むと、最初のレイアウト通過まで 0 に見えてしまう。
	listW int
	// listX/listY/listH は配置された矩形を完成させ、listbox() の
	// 繰返し呼出（典型的な即時モードのスクリプトは毎フレーム呼ぶ）が
	// 同じウィジェットとして認識され、作り直されないようにする。
	listX int
	listY int
	listH int
	// label は描画されるボタンのテキスト（ボタンのフェイスに短縮）。
	// ebitenui の内部に触れずに検査できるよう保持される。
	label string
	// cached はウィジェットのテキストを映し、毎 Update でゲームスレッド
	// が更新するため、InputText がスクリプトをブロックすることはない
	// （フレーム途中でブロックする読み出しは cls/mes の再描画を止め、
	// ちらつきを招く）。
	cached string
	// フォームウィジェット（G5）: チェックボックス、コンボ、テキスト
	// エリアのミラー。
	check   *widget.Checkbox
	combo   *widget.ListComboButton
	area    *widget.TextArea
	checked bool
	list    *widget.List
	// カスタム描画の単一行エディタ（wInput。ebitenui の子は持たない）。
	edit *inputState
}

// ensureUI はゲームスレッドで保持中の UI を構築する。
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

// placeChild はゲームスレッドで子を絶対矩形に追加する。
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

// placeCustom はカスタム描画エントリ（トグル/入力: ebitenui の子は
// 持たない）を登録し、id 配下の既存エントリを置き換える。
// ゲームスレッドのみ。
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

// removeWidgetLocked はウィジェットを落とす。呼出し側はゲームスレッド
// で実行される。
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

// ---------- テーマ（Windows 標準: 白、灰色の枠、黒文字） ----------

// 固定パレット: 白い面、1px の灰色の枠、黒い文字。フォーカスは
// 青枠の淡青の面。押下/選択は青い面に濃青の枠。ホバーは固有の
// 淡青を保つ。ウィジェットごとの上書きはない: objprm の "textcolor"/
// "backcolor" は暗いテーマとともに削除された。
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

// frame は伸縮可能な面を構築する: bg の周りに 1px の枠。1px の内余白
// を持つ 3x3 のソースは枠を歪めずに伸びる。ゲームスレッドのみ
// （画像を生成する。ebiten.NewImage はテストではヘッドレスでも動く）。
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

// nineImage は src をウィジェット全体に伸ばす（純ストレッチの
// ナインスライス、枠はゼロ）。スナップショットは既にボタンサイズ
// なので、ぴったり一致する。
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

// uiFace は我々のフェイスを ebitenui の *text.Face（インタフェース
// へのポインタ）に適合させる。フェイスは font() で入れ替わるため、
// mu の下で読む。
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

// checkBoxSize はフォントに追従するチェックボックスの見た目の辺長。
// 偶数かつ 12 以上にして、下記のナインスライス面がピクセル単位で
// 組み立て直せるようにする。
func (b *WindowBackend) checkBoxSize() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.checkBoxSizeLocked()
}

// checkBoxSizeLocked はロックを取らない checkBoxSize（呼出し側が mu を
// 保持）。
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

// checkArtRGBA は size×size のチェックボックス面を1枚、素のピクセルに
// 描画する: 本体の塗り、1px の枠リング、任意のチェックのグリフ。
// 純粋な Go なので、テストはゲームループなしで見た目を検査できる。
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

// checkArt はチェックボックス面を1枚アップロードする。ゲームスレッドで
// 生成される（他のすべての画像と同じ。ebiten.NewImage はテストでは
// ヘッドレスでも動く）。リングはテーマの枠を使い、白いボックスが
// どんなキャンバスでも読めるようにする。
func checkArt(size int, body color.NRGBA, check bool, mark color.NRGBA) *ebiten.Image {
	return ebiten.NewImageFromImage(checkArtRGBA(size, body, themeFrame, check, mark))
}

// drawCheckMark は四角いブラシで
// (0.28,0.55)->(0.45,0.72)->(0.74,0.30) のチェックを描き、
// どんなボックスサイズでもチェック状態が読めるようにする。
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

// checkImage は実際の最小サイズ（白いボックス、灰色のリング、黒い
// チェックグリフ）でチェックボックスの面を構築する。
//
// 注意: 単色のナインスライス（NewNineSliceColor）は MinSize 0 を報告し、
// ebitenui はラベル付きボックスを checkboxPreferredSize で描画するため、
// 古い面は 0x0 で描画されていた（見えないボックス、ラベル文字のみ）。
// サイズを指定した見た目は MinSize == size でピクセル単位で組み立て
// 直され、これを修正する。
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

// listEntryColors はすべてのリスト状態を定義する。押下/ホバーの背景が
// nil（透明）のままになってはならない: SelectPressed では押下で項目が
// 選択され、nil の Pressed 背景だと selected() が既に切り替わっていても
// マウスを離すまでハイライトが隠れてしまう。選択中と選択済みの項目は
// 押下時の青を使う。フォーカス中（ただし未選択）の項目はフォーカスの
// 淡青を使う（項目は枠なしの平坦な色）。
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

// ---------- スクリプトスレッド API（変更は適用までブロック） ----------

// AddButton はボタンを作成（または置換）する。imgs は
// Idle/Hover/Pressed/Disabled 状態用の picload/dropload バッファ id を
// 最大4つ保持する（0 = スロット未使用）: Hover/Pressed が無ければ
// Idle に、Disabled が無ければ暗くした Idle にフォールバックする。
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
	// ebitenui はラベル全体を描画するため、広いものはボタンの枠から
	// あふれる。まずボタン幅に短縮する。
	label = fitText(label, w-buttonPadX, b.TextWidth)
	b.runOnLoop(func() {
		face := b.uiFace()
		e := &widgetEntry{id: id, kind: wButton, selIdx: -1, label: label}
		bi := b.buttonImage()
		bt := buttonTextColor()
		opts := []widget.ButtonOpt{
			widget.ButtonOpts.Image(bi),
			// 狭い内余白にして、短いタブバー（16px フォントで 18px）が
			// ラベルを面の内側に保てるようにする。
			widget.ButtonOpts.TextPadding(widget.NewInsetsSimple(2)),
			widget.ButtonOpts.ClickedHandler(func(args *widget.ButtonClickedEventArgs) {
				b.mu.Lock()
				e.pressed = true
				b.mu.Unlock()
			}),
		}
		if len(imgs) > 0 && imgs[0] != 0 {
			// 各状態をボタンサイズの画像にスナップショットする:
			// Graphic は画像を中央寄せするため、生のウィンドウサイズの
			// バッファでは間違った（ふつう透明な）領域が表示される。
			// 正確に一致するバッファは生きた参照を保つ。
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
				// ラベルあり: アイコン行は設計上中央寄せで、
				// TextAndImage は ebitenui の状態ごとの差し替えを
				// 有効にする（Text+Graphic を別々の opts にすると
				// 凍結してしまう）。
				opts = append(opts, widget.ButtonOpts.TextAndImage(label, face, &widget.GraphicImage{
					Idle: idle, Hover: hover, Pressed: pressed, Disabled: disabled,
				}, bt))
			} else {
				// アイコンのみ: 状態の面がボタンをちょうど満たす。
				// （Graphic 行は空文字のメンバを追加し、アイコンを
				// 中心から数ピクセルずらしてしまう。）
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

// dimImage は Disabled 状態用の暗くした複製を返す。
// ゲームスレッドで呼ぶ（画像を生成する）。
func dimImage(src *ebiten.Image) *ebiten.Image {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	out := ebiten.NewImage(w, h)
	op := &ebiten.DrawImageOptions{}
	op.ColorScale.Scale(0.45, 0.45, 0.45, 1)
	out.DrawImage(src, op)
	return out
}

// Pressed は最後の呼出以降のクリックを報告する（消費される）。
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

// AddInput は edit.go にある（カスタム描画エディタ）。

// syncWidgetCache はウィジェットの実行時状態を映す（ゲームスレッドのみ。
// Update から呼ばれる）。スクリプトの読み出しはこのミラーを使うため、
// ゲッターがフレーム途中でスクリプトをブロックすることはない
// （gettext のちらつき修正を参照）。
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

// InputText は edit.go にある（カスタムエディタの状態）。

// AddList はリストボックスを作成（または置換）する。リストより広い
// 項目は省略記号で短縮し、枠からあふれないようにする。同じ id、矩形、
// 項目での繰返し呼出は既存ウィジェットをその場で再利用する:
// 毎フレーム作り直す（典型的な即時モードのスクリプトは repeat の中で
// listbox() を呼ぶ）とスクロールオフセットと ebitenui のフォーカスが
// リセットされ、スクロール済みのリストが飛び戻り、新たにフォーカス
// されたリストが直前にフォーカスされていたものの可視オフセットを
// 引き継いでしまう。
func (b *WindowBackend) AddList(id, x, y, w, h int, items []string) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("listbox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	items = fitItems(items, w-listSliderW-listPadX, b.TextWidth)
	b.mu.Lock()
	old, sameID := b.widgets[id]
	// 同じ id/矩形/項目: ウィジェットを保ち、スクロールとフォーカスが
	// 生き残るようにする。（典型的なスクリプトは毎フレーム listbox()
	// を呼ぶ。作り直すとスクロールオフセットと ebitenui のフォーカスが
	// リセットされ、スクロール済みのリストが飛び戻り、新たに
	// フォーカスされたリストが古いオフセットを表示した。）
	if sameID && old.kind == wList && old.list != nil &&
		old.listW == w && old.listH == h && old.listX == x && old.listY == y &&
		slices.Equal(old.items, items) {
		b.mu.Unlock()
		return nil
	}
	// 同じ id/矩形、異なる項目: listbox_set と同じように項目をその場で
	// 入れ替え、リストごとのスクロールとフォーカスが生き残るようにする
	// （ウィジェットを作り直すと毎フレーム失われた）。
	if sameID && old.kind == wList && old.list != nil &&
		old.listW == w && old.listH == h && old.listX == x && old.listY == y {
		old.items = append([]string(nil), items...)
		old.selIdx = -1
		list := old.list
		b.mu.Unlock()
		entries := make([]any, len(items))
		for i, s := range items {
			entries[i] = s
		}
		b.runOnLoop(func() {
			list.SetEntries(entries)
		})
		return nil
	}
	b.mu.Unlock()
	entries := make([]any, len(items))
	for i, s := range items {
		entries[i] = s
	}
	b.runOnLoop(func() {
		face := b.uiFace()
		e := &widgetEntry{id: id, kind: wList, selIdx: -1, listW: w, listX: x, listY: y, listH: h}
		e.items = append([]string(nil), items...)
		list := newListWidget(b, face, e, entries)
		b.placeChild(id, e, list, x, y, w, h)
		e.list = list
	})
	return nil
}

// UpdateList は widget.List.SetEntries 経由で既存の listbox の項目を
// その場で置き換える。同じ ebitenui ウィジェットを保つため、リストの
// スクロールオフセットとフォーカスが更新後も生き残る。ウィジェットを
// 作り直す（2回目の listbox() 呼出がするように）とその状態が捨てられ、
// それがエディタのアウトラインがキー入力ごとに目に見えて飛んでいた
// 原因だった。
func (b *WindowBackend) UpdateList(id int, items []string) error {
	b.mu.Lock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wList || e.list == nil {
		b.mu.Unlock()
		return nil
	}
	// listbox_set は幅を持たない: listbox() が記録した幅に合わせる
	// （listbox() は再レイアウトのたび、例: ウィンドウリサイズ、に
	// 再び呼ばれる）。
	items = fitItems(items, e.listW-listSliderW-listPadX, b.textWidthLocked)
	// 新しい項目を映し、キャッシュされた選択を（mu の下で）同期的に
	// クリアし、同じフレーム内の selected() 読み出しが更新前の
	// インデックスを決して見ないようにする。ウィジェット自体は
	// ゲームスレッドでボタンを入れ替える。
	e.items = append([]string(nil), items...)
	e.selIdx = -1
	list := e.list
	b.mu.Unlock()
	entries := make([]any, len(items))
	for i, s := range items {
		entries[i] = s
	}
	b.runOnLoop(func() {
		list.SetEntries(entries)
	})
	return nil
}

// SelectList は EntrySelectedHandler を発火させずに listbox の選択項目
// をインデックス（0 始まり）で設定する。これによりプログラム的な選択
// （例: "定義へ移動" 後にエディタがアウトラインを同期する）がユーザーの
// クリックのように見えない。キャッシュされたインデックスは一貫した
// 読み出しのために mu の下で更新される。
func (b *WindowBackend) SelectList(id, index int) error {
	b.mu.Lock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wList || e.list == nil {
		b.mu.Unlock()
		return nil
	}
	if index < 0 || index >= len(e.items) {
		b.mu.Unlock()
		return nil
	}
	entry := e.items[index]
	e.selIdx = index
	list := e.list
	b.mu.Unlock()
	b.runOnLoop(func() {
		list.SetSelectedEntry(entry)
	})
	return nil
}

// listEntryTextPos は listbox と combox で共有される項目テキストの
// 配置: 左上寄せ。Golash エディタの手描きアウトラインと同じ。
// 中央寄せにすると、スクロールオフセットの int() 丸め
// （グリフ原点 = int((rectH - lineH) / 2)）に加えて2回目の丸めが乗り、
// リストのスクロール中に行のベースラインが1ピクセルずれて見える
// （ちらつきとして現れる）。
func listEntryTextPos() (widget.TextPosition, widget.TextPosition) {
	start := widget.TextPositionStart
	return start, start
}

// newListWidget は固定の Windows 標準テーマでリストを構築する。
func newListWidget(b *WindowBackend, face *text.Face, e *widgetEntry, entries []any) *widget.List {
	list := widget.NewList(
		widget.ListOpts.ContainerOpts(widget.ContainerOpts.BackgroundImage(
			frame(themeWhite, themeFrame))),
		widget.ListOpts.ScrollContainerImage(&widget.ScrollContainerImage{
			Idle: frame(themeWhite, themeFrame),
			Mask: frame(themeWhite, themeFrame),
		}),
		// 項目は枠の 1px 内側に置く: 内余白がないと選択行が 1px の
		// 灰色の枠を塗り潰してしまう。
		widget.ListOpts.ScrollContainerPadding(widget.NewInsetsSimple(1)),
		// 左上寄せで、Golash エディタの手描きアウトラインに合わせる:
		// 中央寄せテキストは2つの int() 丸め（スクロールオフセット、
		// 次に中央寄せの偏り）に依存し、ベースラインが1ピクセルずれて
		// しまい、スクロール中にちらつきとして現れる。
		widget.ListOpts.EntryTextPosition(widget.TextPositionStart, widget.TextPositionStart),
		widget.ListOpts.EntryTextPadding(widget.NewInsetsSimple(2)),
		// スクリプトがあらゆるキーを所有する（矢印は自身のキャレットを
		// 動かす）: フォーカスされたリストもそれらを消費してはならない。
		// さもないと1回のキー押下でリストとスクリプトの両方が同時に
		// 動いてしまう。
		widget.ListOpts.DisableDefaultKeys(true),
		// 垂直スライダーは見えるままにする（ホイールのみのスクロール
		// では、キーボード/タッチユーザーが手がかりなしに取り残される）。
		// その画像はドロップダウンと同じく固定テーマから来る:
		// 画像なしのハンドルボタンは描画時に panic する。
		widget.ListOpts.SliderParams(b.comboSliderParams()),
		widget.ListOpts.HideHorizontalSlider(),
		// 押下時に選択する（リリース時ではない）: ネイティブの
		// リストボックスはボタンが下がった瞬間にハイライトする。
		// リリースで選択すると遅いクリックのように感じられる。
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

// SelectedIndex はリスト/コンボの選択を返す（無選択なら -1）。
func (b *WindowBackend) SelectedIndex(id int) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || (e.kind != wList && e.kind != wCombo) {
		return 0, fmt.Errorf("selected：不明な listbox %d です", id)
	}
	return e.selIdx, nil
}

// AddCheck はチェックボックスを作成（または置換）する。正でない
// w/h は推奨サイズ（ボックス＋ラベル）にフォールバックする。ボックス
// の見た目はもはやレイアウト矩形に応じてスケールしないため。
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
		// 今すぐ検証する（早期失敗）: PreferredSize は computedParams を
		// 必要とし、それは ebitenui がさもなくば最初の UI 更新で遅延に
		// 埋める。
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

// Checked はチェックボックスの状態を報告する（スクリプトスレッド安全、
// 決してブロックしない: Update 時のミラーを読む）。
func (b *WindowBackend) Checked(id int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wCheck {
		return false, fmt.Errorf("checked：不明な checkbox %d です", id)
	}
	return e.checked, nil
}

// comboSliderParams はドロップダウンのスクロールバーを固定テーマで
// 描画する。
func (b *WindowBackend) comboSliderParams() *widget.SliderParams {
	track := frame(themeWhite, themeFrame)
	return &widget.SliderParams{
		TrackImage:  &widget.SliderTrackImage{Idle: track, Hover: track, Disabled: track},
		HandleImage: b.buttonImage(),
	}
}

// AddCombo はドロップダウンリストを作成（または置換）する。sel は初期
// インデックス（無しなら -1）。
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

// newComboWidget は固定の Windows 標準テーマでドロップダウンを構築する。
func (b *WindowBackend) newComboWidget(face *text.Face, e *widgetEntry, entries []any, labelOf func(any) string, h int) *widget.ListComboButton {
	pressed := true
	// 左上の項目テキスト。newListWidget と同じ。
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
			// listbox と同じ 1px の内余白: 選択行がドロップダウンの枠を
			// 塗り潰してはならない。
			ScrollContainerPadding: widget.NewInsetsSimple(1),
			SelectPressed:          &pressed,
			ScrollContainerImage: &widget.ScrollContainerImage{
				Idle: frame(themeWhite, themeFrame),
				Mask: frame(themeWhite, themeFrame),
			},
			// ドロップダウンは垂直スライダーを保つ: 画像がないと
			// スライダーのハンドルボタンが nil の Image で描画され、
			// 開いたときに panic する（Button.draw は Image.Idle を
			// 参照外しする）。
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

// AddArea は複数行のテキストボックスを作成（または置換）する。
func (b *WindowBackend) AddArea(id, x, y, w, h int, text string) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("mesbox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	b.runOnLoop(func() {
		face := b.uiFace()
		e := &widgetEntry{id: id, kind: wArea, selIdx: -1, cached: text}
		ta := newAreaWidget(b, face, text)
		e.area = ta
		b.placeChild(id, e, ta, x, y, w, h)
	})
	return nil
}

// newAreaWidget は固定の Windows 標準テーマで複数行ボックスを構築する:
// 毎回の更新で下端に張り付く垂直スクロールバーを持つログビューア
// （エディタは結果を追記し、末尾が見えるままでいることを期待する）。
// スライダー画像は必須（画像なしのハンドルは描画時に panic する。
// listbox/コンボのスライダーと同じ）。
func newAreaWidget(b *WindowBackend, face *text.Face, content string) *widget.TextArea {
	return widget.NewTextArea(
		widget.TextAreaOpts.ContainerOpts(widget.ContainerOpts.BackgroundImage(
			frame(themeWhite, themeFrame))),
		widget.TextAreaOpts.ScrollContainerImage(&widget.ScrollContainerImage{
			Idle: frame(themeWhite, themeFrame),
			Mask: frame(themeWhite, themeFrame),
		}),
		widget.TextAreaOpts.SliderParams(b.comboSliderParams()),
		widget.TextAreaOpts.ShowVerticalScrollbar(),
		widget.TextAreaOpts.VerticalScrollMode(widget.ScrollEnd),
		widget.TextAreaOpts.FontFace(face),
		widget.TextAreaOpts.FontColor(themeInk),
		widget.TextAreaOpts.TextPadding(*widget.NewInsetsSimple(4)),
		widget.TextAreaOpts.Text(content),
	)
}

// AreaText は複数行ボックスの内容を返す（スクリプトスレッド安全、
// 決してブロックしない: Update 時のミラーを読む）。
func (b *WindowBackend) AreaText(id int) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wArea || e.area == nil {
		return "", fmt.Errorf("getstr：不明な mesbox %d です", id)
	}
	return e.cached, nil
}

// SetAreaText は複数行ボックスの内容を置き換える（ゲームスレッド）。
// ebitenui の TextArea は表示専用（キーボード編集なし）のため、mesbox
// はプログラム駆動のログビューアとして機能する: setstr() が書き、
// getstr() が読む。ミラーはインラインで更新されるため、続く
// getstr() は正確になる。
func (b *WindowBackend) SetAreaText(id int, text string) error {
	// 存在確認を先頭で行う（AddButton/AddArea と同じように）ため、未知の
	// id はゲームスレッドでブロックせずに失敗する。
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

// SetEnabled はウィジェットの操作可否を切り替える。ゲームスレッドで
// 適用される。カスタム描画エントリ（トグル/入力）は灰色化する
// ebitenui の子を持たないため、e.disabled でそれを追跡する。
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

// RemoveWidget は1つのウィジェットを落とす。
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

// ClearWidgets はすべてのウィジェットを落とす。
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

// HasWidget は登録簿に存在するかを報告する（テスト用）。
func (b *WindowBackend) HasWidget(id int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.widgets[id]
	return ok
}

// Dialog はモーダルな選択を表示し、応答があるまでブロックする。
// mode: "ok" -> 1、"okcancel" -> 1/0、"yesno" -> 1/0。
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

// isCJKBreakable はどこでも折り返して行を始められるルーン
// （日本語の仮名/漢字、ハングル、CJK 記号、全角形）を報告する。
func isCJKBreakable(r rune) bool {
	switch {
	case r >= 0x3040 && r <= 0x30FF: // ひらがな＋カタカナ
		return true
	case r >= 0x3400 && r <= 0x9FFF: // CJK 拡張A .. 漢字
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // ハングル音節
		return true
	case r >= 0xFF00 && r <= 0xFFEF: // 全角形
		return true
	}
	return false
}

// wrapDialogText は s を maxW ピクセル（face で測定）に収まる行に
// 分割する。明示的な改行は尊重される。ラテン文字は語境界を好み、
// CJK はどこでも折り返す。
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
				n = 1 // ウィンドウより広い単一ルーン
			}
			if n < len(rs) {
				// ラテン語の単語の途中で切るとき、両側が非空白・非CJK
				// なら最後の空白まで戻る。
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
