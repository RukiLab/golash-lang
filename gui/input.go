package gui

import (
	"fmt"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

// 合成キー向けのリピート間隔 (tick 単位、@60fps): 新規押下では即時発火し、
// 次は repDelay tick 後、その後は repInterval tick ごとに発火する。印字可能文字や
// IME 確定テキストは OS のキーリピートを既に運ぶため、そのまま通す (charStep 参照)。
const (
	repDelay    = 24
	repInterval = 2
)

// charRep は1ティックの即時入力スナップショットを重複除去する。
// 印字可能な文字と IME で確定したテキストは既に OS のキー繰返しを
// 持つため、そのまま通過する。これは同じティック内で input() が複数回
// ポーリングされたときの重複報告のみを抑える。
type charRep struct {
	last string
	tick int64 // 最後の配送のティック
}

// ctrlKeys は物理的なコントロールキーを input() がそれらに対して
// 報告する文字（ASCII コードを1文字の文字列にしたもの）に写像する。
// AppendInputChars はこれらを決して生成しない。それらは下記と同じ
// 繰返しタイミングで合成される。
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

// ctrlState は1つのコントロールキーの繰返しセッションを壁時計の
// ティックで追跡する。
type ctrlState struct {
	down  bool
	start int64 // 現在の押下が始まったティック
	last  int64 // 最後に発火したティック
}

// ctrlStep は新規の押下で発火し、その後 repDelay/repInterval の
// タイミングで押下中に発火する。純粋な論理で、ユニットテスト可能。
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

// charStep は1フレームのルーンスナップショットを発火する文字列に
// 写像する。テキストは1ティックに1度配送される: 新しいテキストは
// 即座に発火し、後のティックでの OS の自動繰返し到着はそのまま再発火
// する。同じティック内での繰返しポーリングは "" を報告するため、
// スクリプトが同じキー入力を2度見ることはない。純粋な論理で、
// ユニットテスト可能。
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

// 入力のポーリング（G2）。キー/マウスの問い合わせは ebiten に直接
// 当たる（スレッド安全）。クリックのエッジは「押した瞬間」が1ティック
// ごとの観測を必要とするため、Update でラッチされる。

// keyCodes はスクリプトのキーコードを Windows の仮想キー番号に従って
// ebiten のキーに写像する: 8 バックスペース、9 タブ、13 エンター、
// 16 シフト、17 コントロール、18 アルト、27 エスケープ、32 スペース、
// 33-36 pageup/pagedown/end/home、37-40 矢印、45 インサート、
// 46 デリート、48-57 数字、65-90 A-Z、112-123 F1-F12、
// 186-192/219-222 句読点。
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

// shiftHeld はいずれかのシフトキーを報告する。ctrlHeld / altHeld も
// 同様。
func shiftHeld() bool {
	return ebiten.IsKeyPressed(ebiten.KeyShiftLeft) || ebiten.IsKeyPressed(ebiten.KeyShiftRight)
}

// clickButton はスクリプトのボタン番号を ebiten のボタンに写像する。
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

// pumpInput はクリックの立ち上がりエッジをラッチし、ホイールの動きを
// 蓄積する。Update から呼ばれる。ebiten.Wheel は1ティックごとの差分
// （アイドル時は 0）を報告するため、単純な蓄積で動きを失わない。
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

// MouseWheel は最後の呼出以降の保留中の垂直ホイールのデテントを返す
// （消費される。端数は持ち越される）。
func (b *WindowBackend) MouseWheel() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	v := int(b.wheelAccum)
	b.wheelAccum -= float64(v)
	return v
}

// MousePos はカーソル位置をウィンドウのピクセルで返す。
func (b *WindowBackend) MousePos() (int, int) {
	return ebiten.CursorPosition()
}

// MouseDown はボタンが今押されているかを報告する（0 左、1 中央、
// 2 右）。ClickedButton と違い決して消費しない: ラッチされたドラッグ
// フラグと組み合わせることで、スクリプトはスクロールバーのドラッグを
// 実装できる（クリックが開始し、リリースが終了する）。
func (b *WindowBackend) MouseDown(btn int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if btn < 0 || btn >= len(b.mouseDown) {
		return false, fmt.Errorf("不明なボタン %d です", btn)
	}
	return b.mouseDown[btn], nil
}

// PadCount は接続されたゲームパッド数を報告する。
func (b *WindowBackend) PadCount() int {
	return len(ebiten.AppendGamepadIDs(nil))
}

// TouchCount は有効なタッチ数を報告する。
func (b *WindowBackend) TouchCount() int {
	return len(ebiten.AppendTouchIDs(nil))
}

// TouchPos は i 番目のタッチ位置を報告する（ウィンドウのピクセル）。
func (b *WindowBackend) TouchPos(i int) (int, int, error) {
	ids := ebiten.AppendTouchIDs(nil)
	if i < 0 || i >= len(ids) {
		return 0, 0, fmt.Errorf("不明なタッチ %d です（接触数 %d）", i, len(ids))
	}
	x, y := ebiten.TouchPosition(ids[i])
	return x, y, nil
}

// padID はスクリプトのパッドインデックスを解決するか、エラーを報告する。
func (b *WindowBackend) padID(id int) (ebiten.GamepadID, error) {
	ids := ebiten.AppendGamepadIDs(nil)
	if id < 0 || id >= len(ids) {
		return 0, fmt.Errorf("不明なパッド %d です（接続数 %d）", id, len(ids))
	}
	return ids[id], nil
}

// PadButton は標準レイアウトのボタンを報告する（0-16: A,B,X,Y,LB,RB,
// LT,RT,Select,Start,L3,R3,Up,Down,Left,Right,Home）。
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

// PadAxis はスティックの軸 0-3（左 X/Y、右 X/Y）を -1..1 で報告する。
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

// PadName はゲームパッドの名前を報告する。
func (b *WindowBackend) PadName(id int) (string, error) {
	gid, err := b.padID(id)
	if err != nil {
		return "", err
	}
	return ebiten.GamepadName(gid), nil
}

// Clicked は最後の呼出以降のクリックを報告する（消費される）。
// 互換性のために残している。ClickedButton(0) と同じである。
func (b *WindowBackend) Clicked() bool {
	c, _ := b.ClickedButton(0)
	return c
}

// ClickedButton は最後の呼出以降のボタン 0/1/2（左/中央/右）を報告
// する（消費される）。
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

// CursorVisible はシステムカーソルが表示されているかを報告する。
func (b *WindowBackend) CursorVisible() bool {
	return ebiten.CursorMode() == ebiten.CursorModeVisible
}

// SetCursorVisible はシステムカーソルを表示（true）または非表示
// （false）にする。
func (b *WindowBackend) SetCursorVisible(on bool) {
	if on {
		ebiten.SetCursorMode(ebiten.CursorModeVisible)
	} else {
		ebiten.SetCursorMode(ebiten.CursorModeHidden)
	}
}

// ReadImmediate は前回の呼出以降に入力された文字を、GUI の input()
// 組込み（即時モード: 決してブロックしない）のために返す。テキストは
// OS から来る（ロケール依存の Unicode 変換: 配列、シフト、大文字小字
// を正しく扱う。例: Shift+A は "A"）。保留ストリームから排出された
// IME 確定テキストを含む。テキスト（OS のキー繰返し到着を含む）は
// 1ティックに1度即座に発火する。何も発火しなければ ""。コントロール
// キーは ASCII 文字として合成される（バックスペース "\x08"、
// タブ "\x09"、エンター "\r"、エスケープ "\x1b"、デリート "\x7f"）。
// 繰返しタイミングは合成される。コードには asc() を使い、累積ループは
// "\r" で打ち切ること。フォーカス中の inputbox がキー列を所有している
// 間はキー入力が2重処理されないよう "" を報告する。IME フィールドが
// フォーカスされている間はそれが列全体を所有する: 生の文字は
// スキップされ（フィールドが既に保持している）、排出された確定と
// 合成されたコントロールのみが報告される。
// その他の非文字キー（矢印、Fキー）はここに現れない。それらには
// getkey() を使う。毎フレームのポーリングを想定している。
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
	// 変換中はあらゆるコントロールキーは IME に属する（合成の編集または
	// 確定）。ここで合成するとキー入力が2重に適用される。
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

// KeyDown はスクリプトのキーコードが押されているかを報告する。
// 修飾キーのコード 16/17/18 はいずれかの側（左右）に一致する。
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
