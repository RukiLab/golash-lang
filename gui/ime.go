package gui

import (
	"image"
	"unicode/utf8"
)

// ebiten/exp/textinput 経由の IME（Windows/macOS/Web。他のプラット
// フォームはこれまでどおり AppendInputChars 経由で確定テキストのみを
// 得る）。
//
// バックエンド全体で1つの Field: ime(1) がそれをフォーカスし、ime(0)
// がぼかす。フォーカス中は Update が毎ティックそれを汲み上げる。確定
// テキストは input() の行エディタへ流れ、変換中の文字列は固定の左上の
// バー用に映される。バー/描画のコードは Field 自体がゲームスレッドで
// しか安全でないため、mu で保護されたミラーを読む。

// IMEState は IME フィールドがフォーカスされていれば 1、なければ 0 を
// 報告する。
func (b *WindowBackend) IMEState() int {
	if b.imeField.IsFocused() {
		return 1
	}
	return 0
}

// IMESet は IME フィールドをフォーカス（on != 0）またはぼかし、状態を
// 返す。
func (b *WindowBackend) IMESet(on bool) int {
	if on {
		b.mu.Lock()
		b.imeOff = false
		b.mu.Unlock()
		b.imeField.Focus()
	} else {
		b.imeField.Blur()
		// ぼかしは変換中のテキストを破棄する: バーが次の pumpIME ティック
		// まで古い合成を表示しないよう、すぐにミラーをクリアする
		// （非フォーカス時に早期リターンする pumpIME はそれを決して
		// クリアしない）。
		// 注意: Focus/Blur は意図的に mu の外に置いている。Blur は
		// メインスレッドへの同期ディスパッチで OS のセッションを終了する
		// ため、背後で mu に関してブロックしてはならない（Draw は mu の
		// 下でスナップショットを取る）。ここで mu を保持するとウィンドウ
		// 全体がデッドロックする。
		b.mu.Lock()
		b.imeOff = true
		b.imeComposing = ""
		b.imeClause = ""
		b.imeClauseStart, b.imeClauseEnd = 0, 0
		b.mu.Unlock()
	}
	return b.IMEState()
}

// IMEComposition は変換中（未確定）のテキストを返す。
func (b *WindowBackend) IMEComposition() string {
	if !b.imeField.IsFocused() {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.imeComposing
}

// IMEClause returns the target conversion clause (文節) inside the
// 変換中のテキストを、合成文字列内でのルーンオフセットとともに返す:
// (text, runeStart, runeEnd)。非フォーカス、待機中、または
// プラットフォームが句の範囲を報告しない場合はすべてゼロ。1回の呼出は
// 1ティックのスナップショットであるため、オフセットは常にテキストと
// 一致する（2回に分けて呼ぶとティックをまたぎうる）。
// スクリプトスレッド安全。
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

// imeClauseOf は合成テキストから対象の句を切り出す: cs/ce は合成の
// 先頭からの ebiten の CompositionSelection のバイトオフセット
// （ok は有効な範囲を報告する）。句を comp 内でのルーンオフセットと
// ともに返すため、繰返される語も区別できる。範囲外または非 UTF-8 の
// 切り出し（および死んだ範囲）は ("", 0, 0) を返す。純粋な論理で、
// ユニットテスト可能。
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

// imeCandidateBounds は候補ウィンドウを合成バーの直下に固定する: OS は
// 与えられた矩形の下に一覧を開くため、矩形はバーの高さ
// （Max.Y = 行の高さ）を張り、一覧の上端がバーの直下に来る。lh は
// ピクセルでの行の高さ（バーが自身の高さに使うのと同じ値）。純粋な
// 論理で、ユニットテスト可能。
func imeCandidateBounds(lh float64) image.Rectangle {
	h := int(lh + 0.5)
	return image.Rect(0, 0, 1, h)
}

// pumpIME は IME フィールドを進める。ゲームスレッドのみ（Update から
// 呼ばれる）。
func (b *WindowBackend) pumpIME() {
	if !b.imeField.IsFocused() {
		// 非フォーカスは合成が無いことを意味する
		// （UncommittedTextLengthInBytes は定義により 0）: 古いミラーを
		// 破棄し、未確定文字列が空になったらバーを消す。下記の更新に
		// 届かないぼかし/取り消し経路も含む。
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
	// 候補ウィンドウは合成バーの直下に置く（バーが使うのと同じ高さ。
	// これにより一覧の上端がその直下に来る）。
	b.mu.Lock()
	lh := b.lineH
	b.mu.Unlock()
	bounds := imeCandidateBounds(lh)
	if _, err := b.imeField.HandleInputWithBounds(bounds); err != nil {
		// エラー時は合成は使えない: バーに古いミラーを残してはならない。
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
	// ebiten の Windows バックエンドは空になった合成を決して報告しない
	// （長さゼロの COMPSTR は無音で破棄され、取り消しは何も送らない）
	// ため、未確定文字列が空になった後も comp は古いままになりうる。
	// OS の真実が優先される: 空はバーにとってクリアを意味する。
	comp = reconcileComposition(comp)
	if comp == "" {
		clause = ""
		cstart, cend = 0, 0
	}
	b.mu.Lock()
	// フィールドを保留ストリームに映す: 入力されたテキストは追加され、
	// フィールド内の削除（バックスペース等）はストリームの末尾を切り詰める。
	// 末尾は未排出の保留を先に、次に行バッファまたはフォーカス中の
	// エディタを覆う。消費者は毎ティック排出して既に古いテキストを保持
	// しているためである。
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
	// 排出済みでアイドル: 次のティックがクリーンで追記のみの状態から
	// 始まるようフィールドをリセットする（見えないフィールドのキャレット
	// のずれがティックをまたいで蓄積しないように）。
	if ulen == 0 && b.imePending == "" && b.imePrev != "" {
		b.imeField.SetTextAndSelection("", 0, 0)
		b.imePrev = ""
	}
	b.mu.Unlock()
}

// reconcileComposition は ebiten のミラーより OS の真実を優先する:
// ebiten がまだ合成を報告しているが OS 側の未確定文字列が空（0文字）の
// 場合、合成は空になったのでバーを消さなければならない。未知の OS 状態
// では ebiten の値を保つ。純粋な論理で、ユニットテスト可能
// （osLen はテスト用に注入される）。
func reconcileComposition(ebitenComp string) string {
	if ebitenComp == "" {
		return ""
	}
	n, ok := osIMECompositionLen()
	return reconcileCompositionLen(ebitenComp, n, ok)
}

// reconcileCompositionLen は OS の長さを注入できる reconcileComposition
// （n==0 かつ ok は未確定文字列が空であることを意味する）。純粋な論理で、
// ユニットテスト可能。
func reconcileCompositionLen(ebitenComp string, n int, ok bool) string {
	if ebitenComp != "" && ok && n == 0 {
		return ""
	}
	return ebitenComp
}

// imeDiff は新たに確定したフィールドのテキストを直前のティックと
// 突き合わせる: drop は消費側の末尾から取り除くルーン数（フィールド内の
// 削除）、add は新たにタイプされたテキスト。純粋な論理で、
// ユニットテスト可能。
func imeDiff(prev, full string) (drop int, add string) {
	or, nr := []rune(prev), []rune(full)
	i := 0
	for i < len(or) && i < len(nr) && or[i] == nr[i] {
		i++
	}
	return len(or) - i, string(nr[i:])
}

// dropLastLocked は IME の消費ストリームの末尾から n 個のルーンを
// 取り除く: 未排出の保留を先に、次にフォーカス中のエディタ。呼出し側は
// mu を保持する（ゲームスレッド。pumpIME から）。
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
