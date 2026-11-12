package gui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Canvas モデル (G1): id 別バッファ（0 = メイン、選択時に表示）、
// テキスト・ウィジェット配置カーソル (gx, gy)、ブレンドモードを持つ。スクリプトスレッドはコマンドをキューに入れ、
// Update がゲームスレッドで適用する。

// maxBuffers はウィンドウ id の上限（メモリ保護）。
const maxBuffers = 16

// MaxImageDim は screen()/picload 画像の一辺の上限。大きな画像は
// ループ goroutine 内のエンジン内部でパニックし（goroutine をまたいで捕捉できない）、スクリプト側は通常のエラーを受け取る。
const MaxImageDim = 4096

// enqueue は描画命令を追加する。最初の yield より前はゲームスレッドの
// キューへ直接行く（まだフレーム境界がない）。以降は pending に準備
// され、await()/sleep()/end で1つのフレームとしてフラッシュされるため、
// Draw が半完成のキャンバスを決して観測しない（cls 直後の空白1フレーム
// 防止）。保留中の mes()/print() の一塊・gquad()/tri() の貯めた分が先に
// フラッシュされ、スクリプトの呼出順（図形と文字）が保たれる。
func (b *WindowBackend) enqueue(cmd func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushQuadBatchLocked()
	b.flushTriBatchLocked()
	b.flushTextBatchLocked()
	if b.yieldedOnce {
		b.pending = append(b.pending, cmd)
		return
	}
	b.queue = append(b.queue, cmd)
}

// drainQueue は保留中の命令をゲームスレッドで実行する。
func (b *WindowBackend) drainQueue() {
	b.mu.Lock()
	b.flushQuadBatchLocked()
	b.flushTriBatchLocked()
	b.flushTextBatchLocked()
	q := b.queue
	b.queue = nil
	b.mu.Unlock()
	for _, cmd := range q {
		cmd()
	}
}

// runOnLoop は fn をゲームスレッドで実行して待つ（picload のアップロード
// 用）。順序が保たれるよう準備されたフレーム命令が先にフラッシュされる:
// 例: バッファ 1 に描画してから button(..., 1, 0, 0) すると新しいピクセルが
// スナップショットされ、pget/pngsave はそれまでに構築したフレームを
// 読み戻す。貯めた gquad()/tri() も先に流す。
func (b *WindowBackend) runOnLoop(fn func()) {
	done := make(chan struct{})
	b.mu.Lock()
	b.flushQuadBatchLocked()
	b.flushTriBatchLocked()
	b.flushTextBatchLocked()
	if len(b.pending) > 0 {
		b.queue = append(b.queue, b.pending...)
		b.pending = nil
	}
	b.queue = append(b.queue, func() {
		fn()
		close(done)
	})
	b.mu.Unlock()
	<-done
}

// ensureTarget はバッファを返し、ゲームスレッドで生成する。
func (b *WindowBackend) ensureTarget(id int) *ebiten.Image {
	if b.targets == nil {
		b.targets = map[int]*ebiten.Image{}
	}
	img, ok := b.targets[id]
	if !ok || img == nil {
		// w/h は script の screen() が mu 下で書くため、game thread からは
		// ロックして読む (ロックなし読みは競合し、確保サイズが撕裂く)。
		// 遅延生成した画像のサイズは screen(id) 用に記録する。
		b.mu.Lock()
		w, h := b.w, b.h
		if b.bufSizes == nil {
			b.bufSizes = map[int][2]int{}
		}
		b.bufSizes[id] = [2]int{w, h}
		b.mu.Unlock()
		img = ebiten.NewImage(w, h)
		b.targets[id] = img
	}
	return img
}

// hasBuf は確保済みの id を報告する（スクリプトスレッド安全）。
func (b *WindowBackend) hasBuf(id int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bufs[id]
}

// allocBuf は id を確保済みと記す（スクリプトスレッド安全）。
func (b *WindowBackend) allocBuf(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bufs == nil {
		b.bufs = map[int]bool{}
	}
	b.bufs[id] = true
}

// NewBuffer は w x h のオフスクリーン・バッファを作成（または置換）
// する: スクリプトが gsel() して描画できる（文字も含む）空の完全透明
// 画像。可視キャンバス（id 0）は screen(w, h) に属するため、NewBuffer は
// オフスクリーンの id のみを取る。id を置き換えるとそれが保持していた
// ものは破棄され、描画ループが毎フレームのスクラッチバッファを
// 作り直せる。スクリプトスレッド安全: 画像はゲームスレッドで作られる。
func (b *WindowBackend) NewBuffer(id, w, h int) error {
	if id <= 0 || id >= maxBuffers {
		return fmt.Errorf("screen：id は 1 から %d の範囲で指定してください。%d が指定されました", maxBuffers-1, id)
	}
	if w <= 0 || h <= 0 {
		return fmt.Errorf("screen のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	// 大きすぎる画像はループのゴルーチン内でエンジンが panic する
	// （捕まえられない）ため、ここでスクリプトエラーとして上限を設ける。
	if w > MaxImageDim || h > MaxImageDim {
		return fmt.Errorf("screen のサイズは上限 %d x %d までです", MaxImageDim, MaxImageDim)
	}
	b.mu.Lock()
	if b.bufs == nil {
		b.bufs = map[int]bool{}
	}
	if b.bufSizes == nil {
		b.bufSizes = map[int][2]int{}
	}
	b.bufs[id] = true
	b.bufSizes[id] = [2]int{w, h}
	b.mu.Unlock()
	b.enqueue(func() {
		if b.targets == nil {
			b.targets = map[int]*ebiten.Image{}
		}
		b.targets[id] = ebiten.NewImage(w, h)
	})
	return nil
}

// currentSel は選択中の対象を返す（スクリプトスレッド安全）。
func (b *WindowBackend) currentSel() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sel
}

// CurrentSel は選択中の対象をスクリプトの組込みに公開する。
func (b *WindowBackend) CurrentSel() int { return b.currentSel() }

// Foreground は現在の描画色をアルファ付きで返す（スクリプトスレッド
// 安全）。
func (b *WindowBackend) Foreground() [4]int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return [4]int{int(b.fg.R), int(b.fg.G), int(b.fg.B), int(b.fg.A)}
}

// SetAlpha は描画全体のアルファ 0-255（galpha）を設定する。
func (b *WindowBackend) SetAlpha(a int) error {
	if a < 0 || a > 255 {
		return fmt.Errorf("galpha：不透明度は 0 から 255 の範囲で指定してください。%d が指定されました", a)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.alpha = uint8(a)
	return nil
}

// alphaScale は ColorScale のアルファ係数（スクリプトスレッド安全）。
func (b *WindowBackend) alphaScale() float32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return float32(b.alpha) / 255
}

// fillColor は自身のアルファに全体のアルファ（galpha）を掛けた描画色。
func (b *WindowBackend) fillColor(fg [4]int) color.NRGBA {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := uint16(fg[3]) * uint16(b.alpha) / 255
	return color.NRGBA{R: uint8(fg[0]), G: uint8(fg[1]), B: uint8(fg[2]), A: uint8(a)}
}

// CanvasSize は論理キャンバスサイズを返す（スクリプトスレッド安全）。
func (b *WindowBackend) CanvasSize() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.w, b.h
}

// BufferSize は描画バッファ id のサイズを返す（スクリプトスレッド安全）。
// 0 はメインキャンバス（screen(w, h) の論理サイズ）。screen(id, w, h) で
// 作ったバッファと picload()/dropload() の画像は自身のサイズを、gsel()
// だけで確保した id は遅延生成されるメインキャンバスと同じサイズを返す。
func (b *WindowBackend) BufferSize(id int) (int, int, error) {
	if id < 0 || id >= maxBuffers {
		return 0, 0, fmt.Errorf("screen：id は 0 から %d の範囲で指定してください。%d が指定されました", maxBuffers-1, id)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if id == 0 {
		return b.w, b.h, nil
	}
	if !b.bufs[id] {
		return 0, 0, fmt.Errorf("screen：不明なバッファ %d です", id)
	}
	if size, ok := b.bufSizes[id]; ok {
		return size[0], size[1], nil
	}
	return b.w, b.h, nil
}

// CursorPixels はテキスト・ウィジェット配置カーソルを返す（スクリプトスレッド安全）。
func (b *WindowBackend) CursorPixels() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gx, b.gy
}

// SelectTarget は描画バッファを切り替える（gsel）。まとめたテキストが
// それが配置されたバッファに留まるよう、保留中のテキストが先に
// フラッシュされる。
func (b *WindowBackend) SelectTarget(id int) error {
	if id < 0 || id >= maxBuffers {
		return fmt.Errorf("gsel：id は 0 から %d の範囲で指定してください。%d が指定されました", maxBuffers-1, id)
	}
	b.allocBuf(id)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushTextBatchLocked()
	b.sel = id
	return nil
}

// ResizeCanvas は screen(w, h) のために再初期化する。ウィンドウの
// リサイズは直接適用される（Ebiten がキューに入れる）。キャンバスの
// リセットはゲームスレッドで実行される。メインバッファ（0）のみが
// 新しいサイズで作り直され、他のバッファ（picload 等）は保持される。
// そのサイズはウィンドウに依存せず、消すと読み込んだ画像が白紙の
// キャンバスになってしまうため。
func (b *WindowBackend) ResizeCanvas(w, h int) {
	ebiten.SetWindowSize(w, h)
	b.mu.Lock()
	b.w, b.h = w, h
	b.mu.Unlock()
	b.enqueue(func() { b.resizeMainCanvas(w, h, false) })
}

// resizeMainCanvas はメインバッファ（id 0）を w x h のものに入れ替える。
// 他のバッファは自身のサイズ（picload 画像）を保つため、動くのは id 0
// のみ。preserve はウィンドウのドラッグでキャンバスが大きくなったとき
// 古いピクセルを左上揃えで保持する。screen()/cls() 相当の新規開始では
// false を渡す。テキストカーソルとスクロール状態はリセットされる
// （セルグリッドが変わったため）。サイズごとのスクロール用スクラッチは
// 破棄される。ゲームスレッドのみ: 呼出し側は描画キュー内または Layout
// から実行される。
func (b *WindowBackend) resizeMainCanvas(w, h int, preserve bool) {
	if b.targets == nil {
		b.targets = map[int]*ebiten.Image{}
	}
	old := b.targets[0]
	delete(b.targets, 0)
	next := ebiten.NewImage(w, h)
	if preserve && old != nil {
		next.DrawImage(old, nil)
	}
	b.targets[0] = next
	b.mu.Lock()
	if b.bufs == nil {
		b.bufs = map[int]bool{}
	}
	b.bufs[0] = true
	b.curX, b.curY, b.gx, b.gy = 0, 0, 0, 0
	b.lineStartX = 0
	b.scrollExact, b.scrollDone = 0, 0
	// 古いサイズのスクラッチ画像は破棄し、後のスクロールがサイズの
	// 合わないバッファからブリットできないようにする。
	b.scrollScratch = nil
	b.mu.Unlock()
}

// SetBlend は合成モードを設定する: 0 通常、1 加算、2 乗算。
func (b *WindowBackend) SetBlend(mode int) error {
	var m ebiten.CompositeMode
	switch mode {
	case 0:
		m = ebiten.CompositeModeSourceOver
	case 1:
		m = ebiten.CompositeModeLighter
	case 2:
		m = ebiten.CompositeModeMultiply
	default:
		return fmt.Errorf("gmode：サポートされていないモード %d です（0、1、2 が必要です）", mode)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.blend = m
	return nil
}

// currentBlend はブレンドを読み出す（スクリプトスレッド安全）。
func (b *WindowBackend) currentBlend() ebiten.CompositeMode {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.blend
}

// SetPixel は現在の対象に1ピクセル描画する。
func (b *WindowBackend) SetPixel(x, y int, fg [4]int) {
	sel := b.currentSel()
	c := b.fillColor(fg)
	b.enqueue(func() {
		b.ensureTarget(sel).Set(x, y, c)
	})
}

// GetPixel は現在の対象から1ピクセル読み出す（pget）。エラーが
// ヘッドレスでも安全になるよう（RunGame 不要）、ゲームスレッドに触れる
// 前に境界を検証する。読み戻し自体はゲームスレッドが適用するまで
// ブロックする（PicLoad/SavePNG と同じ）。したがって Update/Draw から
// ではなく、スクリプトのゴルーチンからのみ呼ぶこと。
// 返されるチャネルは生の 0-255 バイト（不透明ピクセルは正確に
// ラウンドトリップする。半透明は ebiten の読み戻し規約に従う）。
func (b *WindowBackend) GetPixel(x, y int) ([4]int, error) {
	b.mu.Lock()
	w, h := b.w, b.h
	b.mu.Unlock()
	if x < 0 || y < 0 || x >= w || y >= h {
		return [4]int{}, fmt.Errorf("pget：座標 (%d, %d) は %d x %d の範囲外です", x, y, w, h)
	}
	sel := b.currentSel()
	var out [4]int
	var opErr error
	b.runOnLoop(func() {
		img := b.ensureTarget(sel)
		bw, bh := img.Bounds().Dx(), img.Bounds().Dy()
		if x >= bw || y >= bh {
			opErr = fmt.Errorf("pget：座標 (%d, %d) はバッファ %d (%d x %d) の範囲外です", x, y, sel, bw, bh)
			return
		}
		if c, ok := img.At(x, y).(color.RGBA); ok {
			out = [4]int{int(c.R), int(c.G), int(c.B), int(c.A)}
			return
		}
		r, g, bl, a := img.At(x, y).RGBA()
		out = [4]int{int(r >> 8), int(g >> 8), int(bl >> 8), int(a >> 8)}
	})
	if opErr != nil {
		return [4]int{}, opErr
	}
	return out, nil
}

// FillRect は現在の対象に塗り潰しの矩形を描画する。
// (x, y) を角とし、w/h が負なら逆向きに正規化する（rect の負辺対応）。
// 0 幅・0 高さは何も描画しない。
func (b *WindowBackend) FillRect(x, y, w, h int, fg [4]int) {
	if w < 0 {
		x += w
		w = -w
	}
	if h < 0 {
		y += h
		h = -h
	}
	sel := b.currentSel()
	c := b.fillColor(fg)
	b.enqueue(func() {
		vector.DrawFilledRect(b.ensureTarget(sel), float32(x), float32(y), float32(w), float32(h), c, false)
	})
}

// StrokeLine は現在の対象に 1px の線を描画する。
func (b *WindowBackend) StrokeLine(x1, y1, x2, y2 int, fg [4]int) {
	sel := b.currentSel()
	c := b.fillColor(fg)
	b.enqueue(func() {
		vector.StrokeLine(b.ensureTarget(sel), float32(x1), float32(y1), float32(x2), float32(y2), 1, c, false)
	})
}

// Circle は現在の対象に円を描画する（fill が真なら塗り潰し）。
func (b *WindowBackend) Circle(x, y, r int, fill bool, fg [4]int) {
	sel := b.currentSel()
	c := b.fillColor(fg)
	b.enqueue(func() {
		img := b.ensureTarget(sel)
		if fill {
			vector.DrawFilledCircle(img, float32(x), float32(y), float32(r), c, false)
		} else {
			vector.StrokeCircle(img, float32(x), float32(y), float32(r), 1, c, false)
		}
	})
}

// Ellipse は現在の対象に楕円を描画する（中心 cx, cy、全幅 w、全高 h。
// fill が真なら塗り潰し、偽なら1pxの輪郭）。w == h で真円になる。
// 中心基準のため負の w/h は絶対値と同じ形になる。
// 縮退（w または h が 0）は何も描画しない。
func (b *WindowBackend) Ellipse(cx, cy, w, h int, fill bool, fg [4]int) {
	if w < 0 {
		w = -w
	}
	if h < 0 {
		h = -h
	}
	if w == 0 || h == 0 {
		return
	}
	sel := b.currentSel()
	c := b.fillColor(fg)
	b.enqueue(func() {
		var path vector.Path
		ellipsePath(&path, float32(cx), float32(cy), float32(w)/2, float32(h)/2)
		var cs ebiten.ColorScale
		cs.ScaleWithColor(c)
		opts := &vector.DrawPathOptions{ColorScale: cs}
		if fill {
			vector.FillPath(b.ensureTarget(sel), &path, &vector.FillOptions{}, opts)
			return
		}
		vector.StrokePath(b.ensureTarget(sel), &path, &vector.StrokeOptions{Width: 1}, opts)
	})
}

// ellipsePath は中心 (cx, cy)・半径 (rx, ry) の楕円を
// 4つの3次ベジェ近似（kappa 法）で path に追加する。
func ellipsePath(path *vector.Path, cx, cy, rx, ry float32) {
	const kappa = 0.5522848
	kx, ky := kappa*rx, kappa*ry
	path.MoveTo(cx+rx, cy)
	path.CubicTo(cx+rx, cy+ky, cx+kx, cy+ry, cx, cy+ry)
	path.CubicTo(cx-kx, cy+ry, cx-rx, cy+ky, cx-rx, cy)
	path.CubicTo(cx-rx, cy-ky, cx-kx, cy-ry, cx, cy-ry)
	path.CubicTo(cx+kx, cy-ry, cx+rx, cy-ky, cx+rx, cy)
	path.Close()
}

// ClearCanvas は現在の対象を消去する（透明。黒が透ける）。
func (b *WindowBackend) ClearCanvas() {
	sel := b.currentSel()
	b.enqueue(func() {
		b.ensureTarget(sel).Clear()
	})
}

// Blit はバッファ src のある領域を現在の対象の (dx, dy) に複製する。
// (sx, sy) を角とした |w| x |h| 領域を (dx, dy) を角とした同サイズ領域へ
// 写す。負の w/h は逆向きに正規化した上で反転して描画する（HSP の
// gzoom コピー先負値の反転と同様）。w/h が 0 はエラー。
func (b *WindowBackend) Blit(src, sx, sy, w, h, dx, dy int) error {
	if !b.hasBuf(src) {
		return fmt.Errorf("gcopy：不明なバッファ %d です", src)
	}
	if w == 0 || h == 0 {
		return fmt.Errorf("gcopy のサイズは 0 以外である必要があります。%d x %d が指定されました", w, h)
	}
	// コピー先の角を正規化し、負辺は反転フラグにする。
	dxn, dyn := dx, dy
	flipX, flipY := false, false
	sw, sh := w, h
	if sw < 0 {
		dxn += sw
		sw = -sw
		flipX = true
	}
	if sh < 0 {
		dyn += sh
		sh = -sh
		flipY = true
	}
	dst := b.currentSel()
	blend := b.currentBlend()
	b.enqueue(func() {
		simg := b.ensureTarget(src)
		// image.Rect は Min/Max を正規化するため、負サイズでも
		// 同じ |w| x |h| 領域が切り出される。
		part := simg.SubImage(image.Rect(sx, sy, sx+w, sy+h)).(*ebiten.Image)
		op := &ebiten.DrawImageOptions{}
		sxScale, syScale := 1.0, 1.0
		tx, ty := float64(dxn), float64(dyn)
		if flipX {
			sxScale = -1
			tx = float64(dxn + sw)
		}
		if flipY {
			syScale = -1
			ty = float64(dyn + sh)
		}
		op.GeoM.Scale(sxScale, syScale)
		op.GeoM.Translate(tx, ty)
		op.ColorScale.Scale(1, 1, 1, b.alphaScale())
		op.CompositeMode = blend
		b.ensureTarget(dst).DrawImage(part, op)
	})
	return nil
}

// PicLoad は画像ファイルをデコードして新しいバッファに入れ、その id を
// 返す。アップロードはゲームスレッドで実行される。これはそれを待つ
// （1〜2フレーム程度）。
func (b *WindowBackend) PicLoad(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("picload：%s", err.Error())
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return 0, fmt.Errorf("picload：%s", err.Error())
	}
	id, err := b.allocUpload(src)
	if err != nil {
		return 0, fmt.Errorf("picload：%s", err.Error())
	}
	return id, nil
}

// allocImageID はバッファ id を予約する（mu で保護）。
func (b *WindowBackend) allocImageID() (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bufs == nil {
		b.bufs = map[int]bool{}
	}
	id := 1
	for b.bufs[id] {
		id++
		if id >= maxBuffers {
			return 0, fmt.Errorf("バッファが多すぎます（最大 %d）", maxBuffers-1)
		}
	}
	b.bufs[id] = true
	return id, nil
}

// allocUpload はバッファ id を予約し、src をゲームスレッドで
// アップロードする。
func (b *WindowBackend) allocUpload(src image.Image) (int, error) {
	// id を予約する前に検査する: 拒否が id を漏らしてはならない。
	size := src.Bounds().Size()
	if size.X > MaxImageDim || size.Y > MaxImageDim {
		return 0, fmt.Errorf("picload：画像が大きすぎます（上限 %d x %d）", MaxImageDim, MaxImageDim)
	}
	id, err := b.allocImageID()
	if err != nil {
		return 0, err
	}
	b.mu.Lock()
	if b.bufSizes == nil {
		b.bufSizes = map[int][2]int{}
	}
	b.bufSizes[id] = [2]int{size.X, size.Y}
	b.mu.Unlock()
	b.runOnLoop(func() {
		b.targets[id] = ebiten.NewImageFromImage(src)
	})
	return id, nil
}

// BlitScaled はバッファ src のある領域（w x h）をコピー先サイズ
// (dw x dh) にスケールして現在の対象の (dx, dy) に複製する。
// コピー元は (sx, sy) を角とした |w| x |h|（負は逆向きに正規化）、
// コピー先は (dx, dy) を角とした |dw| x |dh|（負は逆向きに正規化した上で
// 反転描画。HSP の gzoom と同様）。w/h/dw/dh が 0 はエラー。
// スクリプトスレッドはエンキューし、Update が適用する。
func (b *WindowBackend) BlitScaled(src, sx, sy, w, h, dw, dh, dx, dy int) error {
	if !b.hasBuf(src) {
		return fmt.Errorf("gcopy：不明なバッファ %d です", src)
	}
	if w == 0 || h == 0 {
		return fmt.Errorf("gcopy のサイズは 0 以外である必要があります。%d x %d が指定されました", w, h)
	}
	if dw == 0 || dh == 0 {
		return fmt.Errorf("gcopy のコピー先サイズは 0 以外である必要があります。%d x %d が指定されました", dw, dh)
	}
	// 絶対サイズで倍率を作り、コピー先の負辺は反転で表す。
	// コピー元の負辺は切り出しの正規化のみ（image.Rect が吸収する）。
	sw, sh := w, h
	if sw < 0 {
		sw = -sw
	}
	if sh < 0 {
		sh = -sh
	}
	ddw, ddh := dw, dh
	if ddw < 0 {
		ddw = -ddw
	}
	if ddh < 0 {
		ddh = -ddh
	}
	dxn, dyn := dx, dy
	if dw < 0 {
		dxn += dw
	}
	if dh < 0 {
		dyn += dh
	}
	zx := float64(ddw) / float64(sw)
	zy := float64(ddh) / float64(sh)
	if dw < 0 {
		zx = -zx
	}
	if dh < 0 {
		zy = -zy
	}
	// 反転時は描画先の対辺を起点にする。
	tx, ty := float64(dxn), float64(dyn)
	if dw < 0 {
		tx = float64(dxn + ddw)
	}
	if dh < 0 {
		ty = float64(dyn + ddh)
	}
	dst := b.currentSel()
	blend := b.currentBlend()
	b.enqueue(func() {
		simg := b.ensureTarget(src)
		part := simg.SubImage(image.Rect(sx, sy, sx+w, sy+h)).(*ebiten.Image)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(zx, zy)
		op.GeoM.Translate(tx, ty)
		op.ColorScale.Scale(1, 1, 1, b.alphaScale())
		op.CompositeMode = blend
		b.ensureTarget(dst).DrawImage(part, op)
	})
	return nil
}

// SavePNG はバッファ id を PNG ファイルとして書き出す。ゲームスレッドが
// 適用するまでブロックする（PicLoad と同じ）。Update/Draw からではなく
// スクリプトのゴルーチンからのみ呼ぶこと（待機側がループをデッドロック
// させる）。
func (b *WindowBackend) SavePNG(path string, id int) error {
	if id < 0 || id >= maxBuffers {
		return fmt.Errorf("pngsave：id は 0 から %d の範囲で指定してください。%d が指定されました", maxBuffers-1, id)
	}
	if path == "" {
		return fmt.Errorf("pngsave：パスが空です")
	}
	b.mu.Lock()
	allocated := id == 0 || b.bufs[id]
	b.mu.Unlock()
	if !allocated {
		return fmt.Errorf("pngsave：不明なバッファ %d です", id)
	}
	var opErr error
	b.runOnLoop(func() {
		img := b.ensureTarget(id)
		bw, bh := img.Bounds().Dx(), img.Bounds().Dy()
		px := make([]byte, 4*bw*bh)
		img.ReadPixels(px)
		var buf bytes.Buffer
		// 不透明なキャンバスピクセルは正確にラウンドトリップする。
		// 半透明は ebiten の読み戻し規約に従う。
		if err := png.Encode(&buf, &image.NRGBA{Pix: px, Stride: 4 * bw, Rect: image.Rect(0, 0, bw, bh)}); err != nil {
			opErr = fmt.Errorf("pngsave：エンコードエラー：%s", err.Error())
			return
		}
		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			opErr = fmt.Errorf("pngsave：%s", err.Error())
		}
	})
	return opErr
}
