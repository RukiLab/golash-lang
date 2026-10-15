package gui

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Out は exec の透過用に os.Stdout を公開する（GUI モード）。
func (b *WindowBackend) Out() io.Writer {
	return os.Stdout
}

// tabSpaces は mes()/print() におけるタブの GUI 描画: 半角セル4つ分の
// 空白。固定の送り幅でありタブストップではないため、セルの計算は現在の
// 桁位置に依存しない。
const tabSpaces = "    "

// textOp は1回の mes()/print() 呼出の順序付き1ステップ: 任意の
// バッファ消去または上方スクロール、その後ろに任意のテキスト断片。
// 呼出1回全体は順序どおり1つのゲームスレッド・クロージャとして
// 適用されるため、文字は pset()/line()/rect() とキャンバス層を共有する:
// 後ろの rect() は前の文字を覆い、後ろの文字は前の rect() を覆う
// （呼出順が勝つ。HSP と同じ）。
type textOp struct {
	clear    bool // バッファを消去（全画面を超えるスクロールジャンプ）
	scrollPx int  // まずバッファ画像を上にずらす
	frag     bool // スクロール後に断片を描画する
	x, y     int  // 断片のピクセル原点（全ピクセル）
	s        string
	fg       color.NRGBA
	st       TextStyle
	face     *text.GoTextFace
	lineH    float64
	fontSize float64
}

// textDraw は対象バッファに結び付けられた1回の Print/Println 呼出。
type textDraw struct {
	sel int
	ops []textOp
}

// textBatch はゲームスレッドへ渡される前の連続する Print/Println 呼出
// を蓄える。mes()/print() 呼出の一塊は1つの順序付き操作列（通常は1つの
// キュー済みクロージャ）に統合されるが、他の描画・即時のフレーム読み
// 出し・yield・対象切り替えは先にフラッシュし、スクリプトの呼出順が
// 保たれるようにする。まとめた呼出はアトミックではない: 繰返しのための
// スプールにすぎず、Ebiten 自身の命令のバッチ化と同じ考え方である。
type textBatch struct {
	sel int
	ops []textOp
}

func (x *textBatch) empty() bool {
	return x == nil || len(x.ops) == 0
}

// merge は次のテキスト呼出を追加する。隣接する上方スクロールは合計
// できる: 間に断片や消去がなければ、続けて2回ずらすのは合計分を1回
// ずらすのと同じだからである。
func (x *textBatch) merge(sel int, ops []textOp) {
	if len(ops) == 0 {
		return
	}
	if x.sel != sel {
		x.sel = sel
	}
	for _, op := range ops {
		if n := len(x.ops); n > 0 && op.scrollPx > 0 && !op.clear && !op.frag &&
			x.ops[n-1].scrollPx > 0 && !x.ops[n-1].clear && !x.ops[n-1].frag {
			x.ops[n-1].scrollPx += op.scrollPx
			continue
		}
		x.ops = append(x.ops, op)
	}
}

// Print は改行なしでテキストを追加し、他の描画と同じく現在のキャンバス
// バッファにラスタライズする。
func (b *WindowBackend) Print(s string, st TextStyle) {
	b.printRaw(s, st, false)
}

// Println は末尾改行付きでテキストを追加し、他の描画と同じく現在の
// キャンバスバッファにラスタライズする。
func (b *WindowBackend) Println(s string, st TextStyle) {
	b.printRaw(s, st, true)
}

// printRaw は1回の呼出をセルグリッド上に配置し、保留中のテキスト
// バッチに準備する（何も見えない場合は nil）。描画を返すため、テストは
// 断片の位置とスタイルを検査できる。
func (b *WindowBackend) printRaw(s string, st TextStyle, newline bool) *textDraw {
	b.mu.Lock()
	d := b.layoutTextLocked(s, st, newline)
	if d != nil {
		if b.textBatch == nil {
			b.textBatch = &textBatch{}
		}
		b.textBatch.merge(d.sel, d.ops)
	}
	b.mu.Unlock()
	return d
}

// flushTextBatchLocked は蓄えた Print/Println 呼出を1つのゲームスレッド
// ・クロージャへ移す。呼出し側は mu を保持すること。
func (b *WindowBackend) flushTextBatchLocked() {
	if b.textBatch.empty() {
		return
	}
	d := &textDraw{sel: b.textBatch.sel, ops: b.textBatch.ops}
	b.textBatch = nil
	if b.yieldedOnce {
		b.pending = append(b.pending, func() { b.applyTextDraw(d) })
		return
	}
	b.queue = append(b.queue, func() { b.applyTextDraw(d) })
}

// layoutTextLocked は s を描画可能な断片に分割し、テキストカーソルを
// ピクセルで送る。カーソルが下端を過ぎると全画面がスクロールする
// （文字とグラフィックスは HSP と同じく1画面を共有する）。呼出し側は
// mu を保持すること。返される描画は描画状態をすべて捉えるため、後続の
// font()/color() 呼出が既にキュー済みの文字に影響することはない。
func (b *WindowBackend) layoutTextLocked(s string, st TextStyle, newline bool) *textDraw {
	sel := b.sel
	// 文字は自身のアルファに全体のアルファ（galpha）を掛けたものを
	// 尊重する。図形に対する fillColor() とまったく同じである。
	fg := color.NRGBA{R: b.fg.R, G: b.fg.G, B: b.fg.B,
		A: uint8(uint16(b.fg.A) * uint16(b.alpha) / 255)}
	face, lineH, fontSize := b.face, b.lineH, b.fontSize
	if lineH <= 0 {
		lineH = lineHeight
	}
	h := b.h
	// lastY はその行ボックスがまだキャンバスに収まる最下の原点。
	// それより下のカーソルはスクロールする（巨大なジャンプでは消去）。
	lastY := float64(h) - lineH
	d := &textDraw{sel: sel}
	// scrollForRow はカーソルを見えるままに保つ。全画面を超える
	// ジャンプ（例: pos() が大きく下）では、何百回もの全画面シフトを
	// 再現せず消去する: 古い内容はどうせスクロールして見えなくなる。
	scrollForRow := func() {
		if b.curY <= lastY {
			return
		}
		if b.curY-lastY > float64(h) {
			d.ops = append(d.ops, textOp{clear: true})
			b.curY = lastY
			return
		}
		for b.curY > lastY {
			// 現在の lineHeight 比は整数なので、1行はちょうど1ステップ。
			// 桁上げにより、小数のピッチ（lineHeight が変わった場合）も
			// 時間とともにずれない。
			b.scrollExact += lineH
			want := int(b.scrollExact + 0.5)
			px := want - b.scrollDone
			if px < 1 {
				// サブピクセルの高さはステップが0に丸められうるため、
				// 文字が停止しないよう常に少なくとも1ピクセル動かす。
				px = 1
				want = b.scrollDone + px
			}
			b.scrollDone = want
			d.ops = append(d.ops, textOp{scrollPx: px})
			b.curY -= lineH
		}
	}
	if s != "" {
		lines := strings.Split(s, "\n")
		for i, ln := range lines {
			if i > 0 {
				// 1行下がり、ブロックのアンカーに戻る: 継続行は画面の
				// 左端ではなく、この呼出が始まった x（pos() の原点）
				// から始まる。
				b.curY += lineH
				b.curX = b.lineStartX
			}
			// pos() はカーソルを画面外に置いたかもしれない: 描画後では
			// なく、描画前にスクロール（巨大なジャンプでは消去）する。
			scrollForRow()
			if ln == "" {
				continue
			}
			// タブは半角4桁送る。ここで展開することで、描かれる文字と
			// カーソルの送りが一致する。
			ln = expandTabs(ln)
			d.ops = append(d.ops, textOp{frag: true,
				x: pxRound(b.curX), y: pxRound(b.curY), s: ln,
				fg: fg, st: st, face: face, lineH: lineH, fontSize: fontSize})
			// 実際に描かれた幅で送るため、次の断片（継続の print()、
			// または1呼出1文字でエコーされる CJK）が、この断片が
			// 終わったちょうどその位置に、どのフェイスのメトリックでも
			// 収まるようにする。
			adv, _ := text.Measure(ln, face, 0)
			b.curX += adv
		}
	}
	if newline {
		b.curY += lineH
		b.curX, b.lineStartX = 0, 0
		scrollForRow()
	}
	if len(d.ops) == 0 {
		return nil
	}
	return d
}

// pxRound は浮動小数点のピクセル原点を全ピクセルに丸める: サブピクセル
// の原点ではラスタが柔らかく見え（グリフ画像がピクセル間に乗る）、
// 1px 刻みのカーソルには小数部分の使い道がない。
func pxRound(v float64) int {
	return int(math.Floor(v + 0.5))
}

// applyTextDraw は配置済みの呼出をゲームスレッドで順番に実行する。
func (b *WindowBackend) applyTextDraw(d *textDraw) {
	img := b.ensureTarget(d.sel)
	for _, op := range d.ops {
		if op.clear {
			img.Clear()
		}
		if op.scrollPx > 0 {
			b.scrollCanvasUp(img, op.scrollPx)
		}
		if op.frag {
			drawTextFrag(img, op)
		}
	}
}

// scrollCanvasUp はバッファ画像を px ピクセル上にずらし、下端の帯を
// 透明（黒が透ける）のままにする。スクラッチの複製により、重なり合う
// 自己ブリットが明確に定義される。スクラッチはサイズごとに再利用
// されるため、最初のシフト以降はスクロールが割当てなしで済む。
func (b *WindowBackend) scrollCanvasUp(img *ebiten.Image, px int) {
	if px <= 0 {
		return
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if px >= h {
		img.Clear()
		return
	}
	upper := img.SubImage(image.Rect(0, px, w, h)).(*ebiten.Image)
	if b.scrollScratch == nil {
		b.scrollScratch = map[[2]int]*ebiten.Image{}
	}
	key := [2]int{w, h - px}
	tmp, ok := b.scrollScratch[key]
	if !ok || tmp == nil {
		tmp = ebiten.NewImage(w, h-px)
		b.scrollScratch[key] = tmp
	}
	tmp.DrawImage(upper, nil)
	img.Clear()
	img.DrawImage(tmp, nil)
}

// drawTextFrag は断片1つをフォークスのテキストスタイル（単一の変形
// フェイス: ボールドは重ね打ち、イタリックはシアー、下線はベース
// ライン下の罫線）でキャンバスバッファにラスタライズする。キャンバスに
// 直接書き込むため、呼出順に応じて図形が文字を覆ったり、文字が図形を
// 覆ったりできる。
func drawTextFrag(img *ebiten.Image, op textOp) {
	if op.s == "" || op.face == nil || op.lineH <= 0 {
		return
	}
	baseX, baseY := float64(op.x), float64(op.y)
	italic := op.st&StyleItalic != 0
	draw := func(dx float64) {
		dop := &text.DrawOptions{}
		// 領域の上端は原点に乗る。グリフはその下にぶら下がる。
		dop.GeoM.Translate(baseX+dx, baseY)
		if italic {
			// 要素 (0,1) は x' = a*x + b*y + tx の b である:
			// ペンの位置を中心にグリフを右に傾斜させる。
			dop.GeoM.SetElement(0, 1, italicShear)
		}
		// ColorScale のゼロ値は透明。まず恒等にリセットする。
		dop.ColorScale.Reset()
		dop.ColorScale.ScaleWithColor(op.fg)
		text.Draw(img, op.s, op.face, dop)
	}
	draw(0)
	if op.st&StyleBold != 0 {
		dx := 1.0
		if op.fontSize >= 40 {
			dx = 2
		}
		draw(dx)
	}
	if op.st&StyleUnderline != 0 {
		w, _ := text.Measure(op.s, op.face, op.lineH)
		ascent := op.face.Metrics().HAscent
		descent := op.face.Metrics().HDescent
		th := float32(1)
		if op.fontSize >= 32 {
			th = 2
		}
		y := float32(baseY + ascent + descent*0.5) // ベースライン + ディセントの半分
		vector.StrokeLine(img, float32(baseX), y, float32(baseX+w), y, th, op.fg, false)
	}
}

// ReadLine は Backend インタフェースのスタブ: GUI にはブロックする
// 行エディタがない。GUI の input() は即時（ReadImmediate を参照）で、
// スクリプトのテキスト入力は inputbox ウィジェットを使う。偶発的な使用
// がハングせず速やかに終わるよう、常に EOF を報告する。
//
// IME: 変換入力（Windows/macOS/Web）を有効にするには inputbox を
// フォーカスするか ime(1) を呼ぶ。確定したテキストはボックスまたは
// input() ストリームに入り、変換中の文字列は固定のバーに表示される。
func (b *WindowBackend) ReadLine(prompt string) (string, bool) {
	_ = prompt
	return "", false
}

// Clear は現在のキャンバスバッファを消去する（文字は他の描画と同じく
// そこに焼き込まれるため、一緒に消える）。ウィジェットは生き残る:
// それらは clrobj() でのみ削除されるため、描画ループは生きた
// ウィジェットとともに毎フレーム cls() できる。キャンバスの削除は
// enqueue 経由でゲームスレッドで実行されるため、最初の yield 以降は
// pending に準備され、フレームの残りとともに
// await()/sleep()/end() で適用される（1フレームのちらつきなし）。
func (b *WindowBackend) Clear() {
	b.mu.Lock()
	b.curX, b.curY = 0, 0
	b.lineStartX = 0
	b.scrollExact, b.scrollDone = 0, 0
	sel := b.sel
	b.mu.Unlock()
	b.enqueue(func() {
		b.ensureTarget(sel).Clear()
	})
}

// SetColor は以降の文字/描画色を設定する（アルファを含む）。
func (b *WindowBackend) SetColor(r, g, bl, a int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fg = color.NRGBA{R: uint8(clamp(r)), G: uint8(clamp(g)), B: uint8(clamp(bl)), A: uint8(clamp(a))}
}

// ResetColor は黒文字（白いウィンドウの既定）に戻す。
func (b *WindowBackend) ResetColor() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fg = color.NRGBA{0x00, 0x00, 0x00, 0xFF}
}

// IsFullscreen は全画面状態を報告する（スクリプトスレッド安全）。
func (b *WindowBackend) IsFullscreen() bool {
	return ebiten.IsFullscreen()
}

// Closing はユーザーがウィンドウを閉じるよう求めたかを報告する。
// 最初の呼出がオプトインになる: 閉じるボタンはループの終了をやめるため、
// スクリプトは保存して自身で end() を呼べる。
func (b *WindowBackend) Closing() bool {
	b.mu.Lock()
	armed := b.closingArmed
	b.mu.Unlock()
	if !armed {
		ebiten.SetWindowClosingHandled(true)
		b.mu.Lock()
		b.closingArmed = true
		b.mu.Unlock()
	}
	return ebiten.IsWindowBeingClosed()
}

// SetFullscreen はゲームスレッドで全画面を切り替える。
func (b *WindowBackend) SetFullscreen(on bool) {
	b.runOnLoop(func() {
		ebiten.SetFullscreen(on)
	})
}

// IsResizable はウィンドウをドラッグしてリサイズできるかを報告する。
func (b *WindowBackend) IsResizable() bool {
	return ebiten.IsWindowResizable()
}

// SetResizable はウィンドウのドラッグによるリサイズを許可（または禁止）
// する。リサイズが許可されている間は描画解像度がウィンドウに追従する:
// キャンバスはウィンドウサイズで作り直される（1:1 ピクセル、スケーリング
// なし）ため、広げたウィンドウは固定キャンバスを拡大するのではなく
// 描画領域を広げる。オフにすると現在の解像度が凍結される。screen() が
// 論理サイズを再確立する。
func (b *WindowBackend) SetResizable(on bool) {
	b.mu.Lock()
	b.followWindow = on
	b.mu.Unlock()
	ebiten.SetWindowResizable(on)
}

// WindowSize は OS のウィンドウサイズをピクセルで報告する: 論理的な
// screen() キャンバスでもモニタでもなく、実際のウィンドウである。
// ドラッグによるリサイズ（SetResizable を参照）の後では、ウィンドウと
// キャンバスは同じピクセルになる。テストではヘッドレスでも安全に
// 呼べる（Ebiten は実行中のループがなくてもサイズを追跡する）。
func WindowSize() (w, h int) {
	return ebiten.WindowSize()
}

// MoveWindow はウィンドウを移動する（並行安全。デスクトップのみ）。
func (b *WindowBackend) MoveWindow(x, y int) {
	ebiten.SetWindowPosition(x, y)
}

// SetTitle はウィンドウのタイトルを変更する。
func (b *WindowBackend) SetTitle(s string) {
	ebiten.SetWindowTitle(s)
}

// SetFontSize は文字フェイスのサイズをピクセル（8..64）で変更する。
// 行のピッチもそれに応じてスケールするため、mes()/print() の行は
// 間隔を保つ。ウィジェットは uiFace 経由で追従する。スクリプトスレッド
// 安全（フェイス/バッファの入れ替えはミューテックスで保護される）。
func (b *WindowBackend) SetFontSize(size int) error {
	if size < 8 || size > 64 {
		return fmt.Errorf("font：サイズは 8 から 64 の範囲で指定してください。%d が指定されました", size)
	}
	face, err := loadFace(float64(size))
	if err != nil {
		return fmt.Errorf("font：%s", err.Error())
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.face = face
	b.fontSize = float64(size)
	b.lineH = lineHeight * float64(size) / fontSize
	return nil
}

// FontSize は現在のフェイスサイズをピクセルで報告する。
func (b *WindowBackend) FontSize() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int(b.fontSize + 0.5)
}

// FontFile は使用中の解決済みフォントファイルを報告する（探索の既定が
// 有効で変更されていない場合は ""）。
func (b *WindowBackend) FontFile() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fontPath != "" {
		return b.fontPath
	}
	return FontPath()
}

// TextWidth は s を現在のフェイスでピクセル測定する（文字列全体。
// エディタはふつうキャレット位置決め用に1行を渡す）。タブは
// mes()/print() のキャンバス描画とまったく同じく tabSpaces として数える。
func (b *WindowBackend) TextWidth(s string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.textWidthLocked(s)
}

// textWidthLocked はロックを取らない TextWidth: 呼出し側は既に b.mu を
// 保持している（またはゲームスレッドで実行されている）。
func (b *WindowBackend) textWidthLocked(s string) int {
	if b.face == nil {
		return len([]rune(expandTabs(s)))
	}
	w, _ := text.Measure(expandTabs(s), b.face, 0)
	return int(w + 0.5)
}

// expandTabs はすべてのタブを tabSpaces（mes()/print() が描画し、
// strwidth() が測定する固定の4セル送り）に置き換える。
func expandTabs(s string) string {
	if strings.ContainsRune(s, '\t') {
		return strings.ReplaceAll(s, "\t", tabSpaces)
	}
	return s
}

// SetFontFile は書体を spec（パスまたはシステムフォント名）に、指定の
// ピクセルサイズ（8..64）で切り替え、SetFontSize と同じく行のピッチを
// 再スケールする。スクリプトスレッド安全。extraDirs はシステムの
// フォントディレクトリより先に検索される（スクリプトディレクトリ、
// 作業ディレクトリ）。
func (b *WindowBackend) SetFontFile(spec string, size int, extraDirs ...string) error {
	if size < 8 || size > 64 {
		return fmt.Errorf("font：サイズは 8 から 64 の範囲で指定してください。%d が指定されました", size)
	}
	src, path, err := loadFontSpec(spec, extraDirs...)
	if err != nil {
		return err
	}
	face := &text.GoTextFace{Source: src, Size: float64(size)}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.face = face
	b.fontSize = float64(size)
	b.lineH = lineHeight * float64(size) / fontSize
	b.fontPath = path
	return nil
}

// MoveTo はカーソルを設定する。GUI では両方のカーソルがピクセル基準:
// グラフィックスカーソル（gcopy 等）と mes()/print() のテキスト原点で、
// 後者はピクセルをそのまま使う（1px 刻み。文字セルへの丸めはしない）。
// CUI ではコンソールバックエンドが文字セル単位で動かす。
func (b *WindowBackend) MoveTo(x, y int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gx = x
	b.gy = y
	b.curX, b.curY = float64(x), float64(y)
	// pos() 呼出は行を再アンカーする: これ以降の埋め込み改行は
	// この x に戻る。
	b.lineStartX = b.curX
}

// Sleep はスクリプトを一時停止する。ウィンドウは描画を続ける。
// await() と同じくフレーム境界である: それまでに構築したフレーム
// （文字も図形も、準備されたすべてのキャンバス命令）がまず公開される
// ため、一時停止中のフレームは完全である（半描画の内容が残らない）。
func (b *WindowBackend) Sleep(ms int64) {
	b.publishAtYieldLocked()
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
