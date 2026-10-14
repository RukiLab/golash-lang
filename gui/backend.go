// Package gui は Ebiten の WindowBackend を実装する (G0: ウィンドウ、テキスト、
// 終了処理)。描画 (G1)、入力・音声・ダイアログのポーリング (G2)、および
// ウィジェット (G3) がこのファイル群を拡張する。
package gui

import (
	"errors"
	"image"
	"image/color"
	"io/fs"
	"sync"

	"github.com/ebitenui/ebitenui"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/exp/textinput"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	fontSize   = 16.0
	lineHeight = 16.0
	// italicShear は疑似イタリックのグリフを傾ける (約11度)。
	italicShear = 0.2
)

// TextStyle は mes()/print() 用の疑似テキスト装飾のビットマスク。
// 単一の読み込み済みフェイスを変形する (異体ファイルは不要):
// 太字は重ね打ち、斜体はせん断、下線はベースライン下に罫線を引く。
type TextStyle int

const (
	StyleBold TextStyle = 1 << iota
	StyleItalic
	StyleUnderline
)

// ParseTextStyle は mes()/print() の末尾キーワードをスタイルフラグに対応付ける。
// 完全な小文字一致のみなので、通常の単語が出力から消えることはない。
func ParseTextStyle(s string) (TextStyle, bool) {
	switch s {
	case "bold":
		return StyleBold, true
	case "italic":
		return StyleItalic, true
	case "bolditalic":
		return StyleBold | StyleItalic, true
	case "underline":
		return StyleUnderline, true
	}
	return 0, false
}

// WindowBackend は Ebiten ウィンドウへ描画する Backend。
// すべての状態は mu で保護され、Ebiten ループとスクリプト goroutine が
// 共有する。Canvas 画像はゲームスレッドでのみ触る:
// スクリプトは描画コマンドをキューに入れ、Update が処理する。
type WindowBackend struct {
	mu   sync.Mutex
	w, h int
	// followWindow は resizable() を反映する: ウィンドウをドラッグできる間、
	// Layout はウィンドウサイズを返すので、描画解像度がそれに追従する
	// (canvas はウィンドウサイズで作り直され、1:1 ピクセル)。screen() の
	// サイズに固定して表示を拡大する方式ではない。
	followWindow bool
	// テキスト配置状態 (mu で保護)。テキストは pset()/line()/boxf() と
	// まったく同じように canvas バッファ (targets[sel]) へラスタライズされるため、
	// テキストと図形は1つの層を共有し、呼び出し順が重なり順を決める (HSP風)。
	// これらのフィールドはスクリプト側カーソルにすぎない:
	// キュー済み描画コマンドが適用されるフレーム境界までは何も描かれない。
	// テキスト配置状態 (mu で保護)、単位はデバイスピクセル: pos() は
	// テキスト原点を正確に置き (1px 単位、セル丸めなし)、
	// 各 mes()/print() 断片は実際に描いた幅だけカーソルを進めるため、
	// プロポーショナル書体が自然に流れる。
	curX float64
	curY float64
	// lineStartX は現在のテキスト行が始まった x 座標: 全行改行後は 0、
	// それ以外は pos() の原点。埋め込み改行
	// (1つの文字列内の改行) は 0 ではなくここへカーソルを戻すため、
	// 複数行の mes() は起点からのブロックとして印字される。
	lineStartX float64
	// textBatch はスクリプト側で連続する mes()/print() 呼出を
	// スプールし、一塊の出力が1つのキュー済みクロージャを共有し、
	// 隣接する繰返しスクロールを畳めるようにする（mu で保護）。
	textBatch *textBatch
	// scrollExact は累積スクロール高の正確値、scrollDone は既に
	// ずらしたピクセル数。これにより行の高さが小数でも、スクロールが
	// 焼き込み済みのテキストをセルグリッドからずらすことがない。
	scrollExact float64
	scrollDone  int
	// yieldedOnce: スクリプトが少なくとも一度 await()/sleep() で
	// 待機したこと。以降、キャンバス命令（文字を含む）は pending に
	// 準備され、毎回の Update ではなく yield 地点（await/sleep/end）で
	// だけ公開される。これによりフレームはスクリプトが構築を終えてから
	// 初めて画面に届く（publishAtYieldLocked を参照）。
	yieldedOnce bool

	gx, gy int // グラフィックス用のピクセルカーソル（gcopy の変換先）
	fg     color.NRGBA
	// フォント状態（mu で保護）: フェイスと、そこから導出される
	// 行のピッチ（mes()/print() 1行は行高 lineH 分）。サイズは任意。
	face     *text.GoTextFace
	fontSize float64
	lineH    float64
	fontPath string // 使用中の解決済みフォントファイル（"" は探索の既定）
	wantDone bool   // スクリプトが終了。ウィンドウは開いたまま
	wantQuit bool   // end() が呼ばれた。ウィンドウを閉じる
	// キャンバス状態（画像はゲームスレッドのみ）。
	targets map[int]*ebiten.Image // id 別の描画バッファ。0 はメイン
	bufs    map[int]bool          // 確保済み id（mu で保護）
	sel     int                   // 現在の描画対象（mu で保護）
	// scrollScratch はキャンバス文字のスクロールに使う一時画像を
	// 再利用し、大量のスクロールで行ごとに新しい GPU 画像を確保しない
	// ようにする。ゲームスレッドのみ。下記のとおりバッファサイズ
	// ごとにキー化される。
	scrollScratch map[[2]int]*ebiten.Image
	blend         ebiten.CompositeMode
	// persp は gquad() 用の共有の遠近補正テクスチャシェーダ
	// （ゲームスレッドで一度だけコンパイル。persp.go を参照）。
	// perspErr はコンパイル失敗を固定し、以降のすべての描画が毎フレーム
	// 再試行せずアフィン・フォールバックを取るようにする。
	persp    *ebiten.Shader
	perspErr error
	queue    []func()
	// pending はフレーム構築中の canvas コマンド置き場。yieldedOnce 以後は
	// enqueue がここへ溜め、await()/sleep()/end のフレーム境界で queue へ
	// 一括移動する。これで cls() 直後の空 canvas や描きかけが1フレーム
	// 表示される撕裂 (ちらつき) が出ない。await 前のスクリプトは高速に
	// 全コマンドを積み、次の Update で原子的に適用される。
	pending []func()
	// drawSel は Draw が読む公開済み可視バッファ。gsel() の途中切替が
	// そのまま画面に出て裏バッファの描きかけが見えるのを防ぐ。
	drawSel int
	// 入力ラッチ＋音声（G2）。マウスボタンは 0 左、1 中央、2 右で
	// 番号付けされる（clicked() の番号付け）。
	mouseDown  [3]bool
	clickFlag  [3]bool
	wheelAccum float64 // 未消費の垂直ホイールのデテント
	audio      *audioState
	// 描画全体のアルファ 0-255（galpha。図形とブリット）。
	alpha uint8
	// await()/tick() 用のフレームカウンタ（mu で保護）。
	tick     int64
	tickWait []chan struct{}
	// closingArmed は閉じるボタンの処理を有効にする（closing()。保護あり）。
	closingArmed bool
	// ドロップされたファイル（dropfiles/dropload。mu で保護）。
	dropNames []string
	dropFS    fs.FS // 最後のスナップショット（dropload 用に保持）
	// 即時入力の繰返し状態（input()。保護あり）。
	charSt charRep
	ctrlSt []ctrlState // ctrlKeys の各エントリに1スロット
	// IME 状態（ime のみ。Field はゲームスレッドのみで汲み上げ、
	// ミラーはバー/描画の読み出し用に mu で保護される）。
	imeField   textinput.Field
	imePrev    string // 直前のティックで確定したフィールドテキスト（差分基準）
	imePending string // input() を待っている確定テキスト
	// imeComposing は未確定（変換中）テキストのミラー。
	imeComposing string
	// imeClause mirrors the target conversion clause (文節) inside
	// imeComposing（非フォーカス、待機中、またはプラットフォームが
	// 句の範囲を報告しない場合は ""）。imeClauseStart/End は
	// imeComposing 内でのそのルーンオフセット（句が空なら 0, 0）。
	imeClause      string
	imeClauseStart int
	imeClauseEnd   int
	// imeOff は ime(0) で設定される粘着的なスクリプト側の無効化。
	// 設定中はボックスのフォーカスで IME フィールドが自動フォーカス
	// されることはない。ime(1) または次のボックスフォーカスのエッジで
	// 解除される。mu で保護される。
	imeOff bool
	// 保持されるウィジェット（G3。注記のある箇所を除きゲームスレッドのみ）。
	ui        *ebitenui.UI
	root      *widget.Container
	fixLayout *fixedLayout
	widgets   map[int]*widgetEntry
	uiW, uiH  int // 適用されたルートの位置サイズ
}

// New は w x h 論理ピクセルのウィンドウバックエンドを生成する。
func New(w, h int) (*WindowBackend, error) {
	face, err := loadFace(fontSize)
	if err != nil {
		return nil, err
	}
	b := &WindowBackend{
		w: w, h: h,
		ctrlSt: make([]ctrlState, len(ctrlKeys)),
		// 白いウィンドウ: 既定のフェイスは白地に黒文字で、
		// Windows 標準のウィジェットテーマに合わせている。
		fg:       color.NRGBA{0x00, 0x00, 0x00, 0xFF},
		face:     face,
		fontSize: fontSize,
		lineH:    lineHeight,
		alpha:    0xFF,
		// 画像マップは最初の画像操作より前に存在する必要がある:
		// PicLoad と DropLoad は ensureTarget を通らずに書き込み先を
		// 書くため、遅延初期化では nil マップで panic する。
		targets: make(map[int]*ebiten.Image),
		bufs:    make(map[int]bool),
	}
	// 初期フレーム (空 canvas) も Draw に公開しておく。
	b.mu.Lock()
	b.publishFrameLocked()
	b.mu.Unlock()
	return b, nil
}

// RunLoop は run() をゴルーチンで、Ebiten ループをこのスレッドで
// 実行する。ウィンドウが閉じるかスクリプトが end() を呼ぶと戻る。
func RunLoop(be *WindowBackend, run func()) int {
	ebiten.SetWindowSize(be.w, be.h)
	ebiten.SetWindowTitle("Golash")
	ebiten.SetVsyncEnabled(true)
	go run()
	if err := ebiten.RunGame(be); err != nil && !errors.Is(err, ebiten.Termination) {
		return 1
	}
	return 0
}

// SetDone はスクリプトの自然な完了を印す（ウィンドウは開いたまま）。
// 最終フレームが公開され、完成した出力が画面に残る。
func (b *WindowBackend) SetDone() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wantDone = true
	b.publishFrameLocked()
	b.yieldedOnce = true
}

// RequestClose はゲームループに終了を求める（end() 組込み）。
// ウィンドウが消える前に最後のフレームが公開される。
func (b *WindowBackend) RequestClose() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wantQuit = true
	b.publishFrameLocked()
	b.yieldedOnce = true
}

// publishAtYieldLocked はスクリプトの yield 地点（await()/sleep()）
// でフレームを確定し、Update が公開するのを止める。
// 呼出し側は mu を保持してはならない。
func (b *WindowBackend) publishAtYieldLocked() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.publishFrameLocked()
	b.yieldedOnce = true
}

// Closed はループが終了すべきかを報告する。
func (b *WindowBackend) Closed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wantQuit
}

// publishFrameLocked は構築中のフレームを確定する: 準備された
// キャンバス命令（図形も mes()/print() の文字も同様）は1つの塊として
// ゲームキューへ移り、可視バッファ sel が drawSel にスナップショット
// される。呼出し側は mu を保持すること。
// Draw はスナップショットだけを読むため、進行中の cls()/mes() 列の
// 作業状態が決して描画されない: フレームはスクリプトの yield 地点
// （Await/Sleep、すなわちフレームが完成した一度）か、スクリプトがまだ
// yield していない間は Update によって確定される。
// 文字はキャンバスバッファに焼き込まれるため、それ自身のスナップ
// ショットを必要としない: フレームの残りとともに確定され、確定された
// バッファ（drawSel）はそれまで直前のフレームを保つ。
func (b *WindowBackend) publishFrameLocked() {
	b.flushTextBatchLocked()
	if len(b.pending) > 0 {
		b.queue = append(b.queue, b.pending...)
		b.pending = nil
	}
	b.drawSel = b.sel
}

// advanceTick はフレームカウンタを増やし、await() の呼出し側を
// 起こす。Update から呼ばれる（ゲームスレッド）。テストは直接駆動する。
func (b *WindowBackend) advanceTick() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tick++
	for _, ch := range b.tickWait {
		close(ch)
	}
	b.tickWait = nil
}

// Tick は Update カウンタ（スクリプトスレッドから安全）。
func (b *WindowBackend) Tick() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tick
}

// Await は n フレーム経過までブロックする。スクリプトのゴルーチン
// から来るべきで、決してゲームスレッドから来てはならない（起こせない）。
// この呼出がフレーム境界になる: 前回の await 以降に描かれたものすべて
// （図形も mes()/print() の文字も同様）がここで確定されるため、Draw は
// 半描画のフレームを決して観測しない。
// (cls() 直後や mes() 前の状態が画面に出るちらつきの防止).
// await(0) はそれゆえ「今すぐこのフレームを表示せよ」という明示的な
// 指示も兼ねる。
func (b *WindowBackend) Await(n int64) {
	b.publishAtYieldLocked()
	if n <= 0 {
		return
	}
	for {
		b.mu.Lock()
		target := b.tick + n
		// ロック下で再確認する: ティックは既に経過しているかもしれない。
		if b.tick >= target {
			b.mu.Unlock()
			return
		}
		ch := make(chan struct{})
		b.tickWait = append(b.tickWait, ch)
		b.mu.Unlock()
		<-ch
		n = target - b.Tick()
		if n <= 0 {
			return
		}
	}
}

// --- ebiten.Game ---

// Update は入力を汲み上げ、キュー済みの描画命令を適用し、ウィジェット
// を更新する。
func (b *WindowBackend) Update() error {
	b.drainQueue()
	b.pumpInput()
	b.pumpDrops()
	b.pumpIME()
	b.pumpInputs()
	// Root は window 全面。リサイズ反映は Update 側で行い、Draw 中の
	// SetLocation を避ける (Draw 中のレイアウト変更は ebitenui の描画を
	// 1フレーム乱し、GUI部品のちらつき・ずれに見えるため)。
	if b.ui != nil {
		b.mu.Lock()
		ww, hh := b.w, b.h
		needReloc := b.root != nil && (b.uiW != ww || b.uiH != hh)
		b.mu.Unlock()
		if needReloc {
			b.mu.Lock()
			b.uiW, b.uiH = ww, hh
			b.mu.Unlock()
			b.root.SetLocation(image.Rect(0, 0, ww, hh))
		}
		if b.fixLayout != nil {
			b.fixLayout.w, b.fixLayout.h = ww, hh
		}
		b.ui.Update()
	}
	b.syncWidgetCache()
	// フレーム確定は原則ここでは行わない (Draw は確定済みフレームのみ描く)。
	// await()/sleep() を一度も使わないスクリプトはフレーム境界が無いので、
	// その場合だけ Update で確定して描画がいつまでも出ないのを防ぐ。
	b.mu.Lock()
	if !b.yieldedOnce {
		b.publishFrameLocked()
	}
	b.mu.Unlock()
	b.advanceTick()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.wantQuit {
		return ebiten.Termination
	}
	return nil
}

// Draw は確定されたキャンバスフレームに加えウィジェットと入力ボックス
// を描画する。文字は図形と同じキャンバスバッファに置かれる
// （mes()/print() のピクセルは applyTextDraw によってそこへ
// ラスタライズされる）ため、Draw は1層だけをブリットする:
// スクリプトで最後に描かれたものが勝つ。これは HSP とまったく同じ。
func (b *WindowBackend) Draw(screen *ebiten.Image) {
	// 白いウィンドウ: 消去（透明）されたキャンバスは白く見える。
	screen.Fill(color.White)
	// Visible canvas: the committed buffer (drawSel). gsel() の途中切替が
	// そのまま出ると裏バッファの描きかけが1フレーム見えるため、frame 境界で
	// 公開された面だけを描く。targets 自体は game thread のみが触る。
	b.mu.Lock()
	sel := b.drawSel
	b.mu.Unlock()
	if img := b.targets[sel]; img != nil {
		screen.DrawImage(img, nil)
	}

	if b.ui != nil {
		b.ui.Draw(screen)
	}
	b.drawInputs(screen)
	b.drawIMEComp(screen)
}

// Layout は論理画面サイズを固定する。resizable() がオンの間は
// 描画解像度がウィンドウに追従する: キャンバスはウィンドウサイズで
// 作り直される（ピクセル 1:1、スケーリングなし）ため、ウィンドウを
// ドラッグすると固定キャンバスを拡大するのではなく描画領域が広がる。
func (b *WindowBackend) Layout(outsideWidth, outsideHeight int) (int, int) {
	b.mu.Lock()
	if !b.followWindow || outsideWidth <= 0 || outsideHeight <= 0 ||
		(outsideWidth == b.w && outsideHeight == b.h) {
		defer b.mu.Unlock()
		return b.w, b.h
	}
	b.w, b.h = outsideWidth, outsideHeight
	b.mu.Unlock()
	// Layout は同じフレームの Update/Draw より前にゲームスレッドで
	// 実行されるため、キャンバスの差し替えは大きくなったフレーム
	// バッファとともに反映される。
	b.resizeMainCanvas(outsideWidth, outsideHeight, true)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.w, b.h
}
