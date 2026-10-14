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

// カスタムの単一行エディタ（inputbox）: キャレット・水平スクロール・
// OS の IME 変換を備えた自己描画のボックス。変換中のテキストは固定の
// 左上のバーに描画される（drawIMEComp を参照）。OS の候補ウィンドウも
// 同様に画面原点に固定される。キャレット位置を知るために ebitenui の
// TextInput を置き換える。
//
// フォーカスモデル: 左クリックは1つのボックスをフォーカスし（他は
// ぼかす）。他の場所をクリックするとボックスからフォーカスが外れる。
// IME フィールドはボックスのフォーカスに追従するため、明示的な ime(1)
// なしで変換が動く（ゲームスレッドのみ）: 新しいフォーカスはフィールド
// をフォーカスし、ボックスのフォーカス外れ（他の場所をクリック、
// Enter/Escape）はそれをぼかす。それ以外ではフォーカスは粘着的である:
// 即時モードのスクリプトはどのボックスもフォーカスされずに input() を
// ポーリングし、アイドル時の自動ぼかしはキー入力の間の変換を殺して
// しまう。ime(0) は ime(1) または次のボックスフォーカスのエッジまで
// 変換を強制的に無効にする。

// inputState は1つのエディタ。バックエンドの mu で保護され、ゲーム
// スレッドの pumpInputs で変更され、スクリプトスレッドの InputText で
// 読まれる。
type inputState struct {
	rect    image.Rectangle
	text    []rune
	caret   int // ルーンインデックス 0..len(text)
	focused bool
	scroll  int // 可視テキストの x ピクセルオフセット
	// 押下キーの繰返し（バックスペース/デリート/矢印をゲームスレッドで）。
	repKey  ebiten.Key
	repNext int64
}

// insert はキャレット位置にルーンを追加する。
func (e *inputState) insert(rs []rune) {
	if len(rs) == 0 {
		return
	}
	e.text = append(e.text[:e.caret], append(rs, e.text[e.caret:]...)...)
	e.caret += len(rs)
}

// backspace はキャレットの前を削除し、del はキャレット位置を削除する。
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

// repFire は押下中の編集キーを制御する: 押した瞬間に発火して繰返しを
// 武装し、別のキーが引き継ぎ、離すと武装解除する。純粋な論理で、
// ユニットテスト可能。
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

// editAction は1つの編集キーの効果。pumpInputs が適用する。
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

// focusedLocked はフォーカス中のエディタを返す。呼出し側は mu を保持。
func (b *WindowBackend) focusedLocked() *inputState {
	for _, e := range b.widgets {
		if e.kind == wInput && e.edit != nil && e.edit.focused {
			return e.edit
		}
	}
	return nil
}

// AddInput は初期テキスト（キャレットは末尾）付きのテキスト入力を
// 作成（または置換）する。カスタム描画: ebitenui の子は持たない。
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

// InputText は現在の入力内容を返す（スクリプトスレッド安全、決して
// ブロックしない: 状態自体が mu で保護されている）。
func (b *WindowBackend) InputText(id int) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wInput || e.edit == nil {
		return "", fmt.Errorf("gettext：不明な inputbox %d です", id)
	}
	return string(e.edit.text), nil
}

// blurEditsLocked はすべてのエディタからフォーカスを外す。呼出し側は
// mu を保持。
func (b *WindowBackend) blurEditsLocked() {
	for _, e := range b.widgets {
		if e.kind == wInput && e.edit != nil && e.edit.focused {
			e.edit.focused = false
			e.edit.repKey = 0
		}
	}
}

// editFocusPlan は1回の pumpInputs の押下に対する IME フィールドの
// 判断: フィールドをフォーカス/ぼかすべきか、粘着的な ime(0) の
// 無効化を解除する必要があるか。純粋な論理で、ユニットテスト可能。
type editFocusPlan struct {
	focus  bool // IME フィールドをフォーカス（ボックスフォーカス、または ime(0) 後の再有効化）
	blur   bool // IME フィールドをぼかす（フォーカス中のボックスの外をクリック）
	enable bool // 粘着的な ime(0) の無効化を解除
}

// planEditFocus は1回の押下/観測に対する IME フィールドの動きを決める。
// press は実際の押下エッジ（ラッチされた clicked() レベルではない）。
// hit は押下が有効な inputbox の内側に入ったか、hadFocus は押下前に
// ボックスがフォーカスされていたか、boxFocused は今フォーカス中のものが
// あるか、fieldFocused/imeOff は現在の IME フィールドの状態。純粋な
// 論理で、ユニットテスト可能。
//
// 規則: ボックス内の押下はフィールドをフォーカスし ime(0) 後の変換を
// 再有効化する。ボックスの外の押下は、ボックスがフォーカスされていた
// （入力が終わった）ときのみ、かつ ime(0) が設定されていないときのみ
// フィールドをぼかす。ボックスがフォーカスされているがフィールドは
// そうでない（他のあらゆるぼかし）場合、変換が動き続けるよう
// フィールドを再フォーカスする。
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

// editKeysOwnedByField は編集キー（Backspace/Delete/矢印/Home/End）が
// このティックで IME フィールドに属するかを報告する。変換中、または
// フィールドが未排出のテキストを保持している間は、フィールドがそれらを
// 所有し、ボックスは関与してはならない。さもないとキー入力が2重に
// 適用される（フィールドとボックス両方）。それ以外の場合、フィールドは
// それらを飲み込んでしまう（空のフィールドでは動作しない Backspace が
// 顕著）からのっぺらぼうなので、ボックスがそれらを扱う。純粋な論理で、
// ユニットテスト可能。
func editKeysOwnedByField(imeOn bool, comp, fieldText, pending string) bool {
	return imeOn && (comp != "" || fieldText != "" || pending != "")
}

// pumpInputs はゲームスレッドでフォーカスと編集を処理する（pumpIME の
// 後の Update から呼ばれる）。クリックは消費されず観測される。
func (b *WindowBackend) pumpInputs() {
	x, y := ebiten.CursorPosition()
	// ラッチされた clicked() フラグではなく押下エッジ: clickFlag は
	// スクリプトが消費するまで設定されたままなので、ラッチされた
	// レベルだと、マウスが外にある間の後続フレームでボックス（と IME
	// フィールド）をぼかし、ime(0) の直後に変換を再有効化してしまう。
	// エッジは押下を観測した Update の間だけ持続し、それは必ず実行
	// されるため、スクリプトがその間に clicked() を消費しても
	// クリックは見逃されない。
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
		// どのボックスの外をクリック: 入力は終わった。進行中の変換は
		// ぼかしとともに破棄される（下記の Enter/Escape 経路と一致）。
		// 粘着的な ime(0) はミラーに触れずにフィールドをぼかしたまま
		// 保つ。
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

	// ボックスがフォーカスされている間はそれがキー列を所有する:
	// input() は "" を報告し、Update は確定した文字をキャレットへ
	// 導く。フォーカス中のボックスがない場合、候補ウィンドウは原点に
	// フォールバックする（アンカーにするキャレットがない）。

	if ed == nil {
		return
	}
	now := b.Tick()
	// 確定した文字はキャレット位置に入る。IME フィールドがフォーカス
	// されている間は pumpIME 経由で、それ以外は素のまま届く。
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
	// 変換中は Enter/Escape は IME に属する（それらが合成を確定または
	// 取り消す）。それ以外の場合はボックスをぼかす。
	if !imeOn || comp == "" {
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyKPEnter) ||
			inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			b.mu.Lock()
			b.blurEditsLocked()
			// 行エディタが確認時に行うようにフィールドの残りを破棄
			// する: 入力は終わった。
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
	// フィールドが入力を所有している間は、下記の編集キーも所有する。
	// フィールドが空の場合はボックスがそれらを扱う
	// （editKeysOwnedByField を参照）: 見えないフィールドがそれらを
	// 飲み込んでしまう。
	fieldText := ""
	if imeOn {
		fieldText = b.imeField.Text()
	}
	if editKeysOwnedByField(imeOn, comp, fieldText, pending) {
		return
	}
	// 編集キー（ゲームスレッド: ここでは inpututil は安全）。
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

// drawInputs はカスタムエディタを UI の上に描画する（ゲームスレッド。
// Draw から呼ばれる）。テキストはサブイメージのビューポートでボックス
// にクリップされる。
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

// compSpan は合成上の1つの下線の走り: ルーンで [start, end)。変換対象
// の句では太い。
type compSpan struct {
	start, end int
	thick      bool
}

// compSpans は n 個の合成ルーンを句の範囲から下線の走りに分割し、
// 空を飛ばし範囲内に切り詰める。純粋な論理で、ユニットテスト可能。
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

// drawIMEComp は変換中のテキストをウィンドウの隅にぴったり合った
// 左上のバー（白い面、灰色のリング、黒い文字）に、すべての上に描く:
// 変換対象の句は太い下線、残りは細い。ボックスのキャレットと違い、
// 合成の表示は決して動かない: inputbox と即時の input() の両方に
// とって予測可能な1つの場所である。ゲームスレッド（Draw）。
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
	// imeCompExtraH はテキスト行を超えてバーを埋める: 16px の行
	// ボックスはベースライン + ディセントの半分で終わるため、3px の
	// 句の下線が枠からはみ出てしまう。
	const imeCompExtraH = 2.0
	w := measureInput(face, rsRunes)
	bw, bh := w+pad*2, lineH+imeCompExtraH
	const x, y = 0.0, 0.0
	vector.DrawFilledRect(screen, float32(x), float32(y), float32(bw), float32(bh), themeWhite, false)
	vector.StrokeRect(screen, float32(x)+0.5, float32(y)+0.5, float32(bw)-1, float32(bh)-1, 1, themeFrame, false)
	op := &text.DrawOptions{}
	tx := x + pad
	// 領域の上端はバーの上端に乗る（drawTextFrag と同じ text/v2 の
	// 意味論）。ベースラインはその ascent 下に位置する。
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

// inputFaceColors はエディタの面を選ぶ: フォーカスは背景を青枠の淡青
// に染め、無効は灰色化する（フォーカスより優先）。純粋な論理で、
// ユニットテスト済み。
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

// drawInput は1つのエディタを描画する: 枠、クリップされたテキスト、
// キャレット。変換中のテキストはここでは描かれず、固定の左上のバーに
// 描かれる（drawIMEComp）。固定の Windows 標準色（白、灰色の枠、黒い
// 文字。フォーカスは淡青/青の面）。純粋な描画（バックエンド非依存）
// で、注記のある箇所はユニットテスト可能。
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
	// キャレットを見えるままに保つ。
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
	// 垂直方向は中央揃え（領域の上端は原点に乗るため、残った内側の
	// 高さの半分だけずらす）。
	op.GeoM.Translate(float64(inner.Min.X)-float64(st.scroll),
		float64(inner.Min.Y)+(float64(inner.Dy())-(ascent+descent))/2)
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(fg)
	text.Draw(dst, string(st.text), face, op)
	// キャレットの点滅（約2Hz）。
	if st.focused && !disabled && tick/30%2 == 0 {
		cx := float32(inner.Min.X) + float32(visX)
		if cx >= float32(inner.Min.X) && cx <= float32(inner.Max.X) {
			vector.DrawFilledRect(screen, cx, float32(inner.Min.Y), 2, float32(inner.Dy()),
				themeInk, false)
		}
	}
}

// measureInput はフェイスでルーンの接頭辞の幅を測る（キャレット位置
// 決め用）。純粋で、ユニットテスト可能。
func measureInput(face *text.GoTextFace, prefix []rune) float64 {
	if face == nil || len(prefix) == 0 {
		return 0
	}
	w, _ := text.Measure(string(prefix), face, 0)
	return w
}
