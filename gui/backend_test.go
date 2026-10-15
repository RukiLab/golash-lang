package gui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// mustNew はバックエンドを構築する。システムフォントが無い場合はスキップする
// （フォント無しのCIコンテナ用）。フォントファイルは同梱されていないため
// GUIテキストにはOSフォントが必要。
func mustNew(t *testing.T) *WindowBackend {
	t.Helper()
	b, err := New(640, 480)
	if err != nil {
		t.Skipf("system font unavailable: %v", err)
	}
	return b
}

func TestSystemFontLoads(t *testing.T) {
	src, p, err := systemFontSource()
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	if src == nil || p == "" {
		t.Fatal("want cached source and path")
	}
	face, err := loadFace(16)
	if err != nil || face == nil {
		t.Fatalf("loadFace: %v", err)
	}
	if FontPath() != p {
		t.Fatalf("FontPath = %q, want %q", FontPath(), p)
	}
}

func TestSetAreaTextHeadless(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		defer close(done)
		addErr = b.AddArea(30, 0, 0, 200, 100, "one\ntwo")
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddArea never completed")
	}
	if addErr != nil {
		t.Fatalf("AddArea: %v", addErr)
	}
	set := make(chan struct{})
	var setErr error
	go func() {
		defer close(set)
		setErr = b.SetAreaText(30, "three\nfour")
	}()
	if !pumpUntil(b, set, 10*time.Second) {
		t.Fatal("SetAreaText never completed")
	}
	if setErr != nil {
		t.Fatalf("SetAreaText: %v", setErr)
	}
	if s, _ := b.AreaText(30); s != "three\nfour" {
		t.Fatalf("AreaText(30) = %q, want three\\nfour", s)
	}
	if err := b.SetAreaText(99, "x"); err == nil {
		t.Fatal("SetAreaText unknown should error")
	}
}

func TestWrapDialogText(t *testing.T) {
	face, err := loadFace(16)
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	if got := wrapDialogText("short", face, 500); len(got) != 1 || got[0] != "short" {
		t.Fatalf("short = %q", got)
	}
	long := "あいうえおかきくけこさしすせそたちつてとなにぬねの"
	lines := wrapDialogText(long, face, 200)
	if len(lines) < 2 {
		t.Fatalf("long should wrap, got %q", lines)
	}
	for _, ln := range lines {
		if w, _ := text.Measure(ln, face, 0); w > 201 {
			t.Fatalf("line %q too wide: %v", ln, w)
		}
	}
	if strings.Join(lines, "") != long {
		t.Fatalf("wrap lost text: %q", lines)
	}
	multi := wrapDialogText("a b c\nd e", face, 500)
	if len(multi) != 2 || multi[0] != "a b c" || multi[1] != "d e" {
		t.Fatalf("explicit breaks = %q", multi)
	}
}

func TestCharStep(t *testing.T) {
	var st charRep
	if got := charStep(&st, "", 0); got != "" {
		t.Fatalf("idle = %q, want empty", got)
	}
	// 新しいテキストは即座に発火する。
	if got := charStep(&st, "a", 0); got != "a" {
		t.Fatalf("new = %q, want a", got)
	}
	// 同一ティック内の再ポーリングは沈黙を保つ（二重報告なし）。
	if got := charStep(&st, "a", 0); got != "" {
		t.Fatalf("dup = %q, want empty", got)
	}
	// 後のティックで届くOSのオートリピートはそのまま通過させる。
	if got := charStep(&st, "a", 1); got != "a" {
		t.Fatalf("repeat = %q, want a", got)
	}
	if got := charStep(&st, "a", 2); got != "a" {
		t.Fatalf("repeat2 = %q, want a", got)
	}
	// 異なるテキストはティック途中でも常に即座に発火する。
	if got := charStep(&st, "b", 2); got != "b" {
		t.Fatalf("switch = %q, want b", got)
	}
	if got := charStep(&st, "b", 2); got != "" {
		t.Fatalf("switch dup = %q, want empty", got)
	}
	// 空白期間は沈黙を保つ。後の到着分はそれでも発火する。
	if got := charStep(&st, "", 3); got != "" {
		t.Fatalf("gap = %q, want empty", got)
	}
	if got := charStep(&st, "a", 1000); got != "a" {
		t.Fatalf("late = %q, want a", got)
	}
}

// TestCtrlStep はゲームループなしで制御キーのリピートを検証する：
// 押下で発火、遅延中は沈黙、間隔ごとに再発火、
// 解放で即時発火に戻る。
func TestCtrlStep(t *testing.T) {
	var st ctrlState
	if ctrlStep(&st, false, 0) {
		t.Fatal("idle should not fire")
	}
	if !ctrlStep(&st, true, 0) {
		t.Fatal("press should fire")
	}
	if ctrlStep(&st, true, 10) {
		t.Fatal("hold below delay should not fire")
	}
	if !ctrlStep(&st, true, repDelay) {
		t.Fatal("delay should refire")
	}
	if !ctrlStep(&st, true, repDelay+repInterval) {
		t.Fatal("interval should refire")
	}
	if ctrlStep(&st, true, repDelay+repInterval+1) {
		t.Fatal("off-beat hold should not fire")
	}
	if ctrlStep(&st, false, repDelay+repInterval+2) {
		t.Fatal("release should not fire")
	}
	if !ctrlStep(&st, true, 1000) {
		t.Fatal("re-press should fire at once")
	}
	if len(ctrlKeys) != 6 {
		t.Fatalf("ctrlKeys = %d entries, want 6", len(ctrlKeys))
	}
	want := map[string]bool{"\x08": true, "\x09": true, "\r": true, "\x1b": true, "\x7f": true}
	for _, ck := range ctrlKeys {
		if !want[ck.ch] {
			t.Fatalf("unexpected control char %q", ck.ch)
		}
	}
}

// TestImeDiff はフィールドミラーを検証する：追記は通過し、
// フィールド内削除は末尾削除になり、同一テキストは無操作。
func TestImeDiff(t *testing.T) {
	if d, a := imeDiff("", "あ"); d != 0 || a != "あ" {
		t.Fatalf("type = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あ", "あい"); d != 0 || a != "い" {
		t.Fatalf("append = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あい", "あ"); d != 1 || a != "" {
		t.Fatalf("backspace = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あいう", "あ"); d != 2 || a != "" {
		t.Fatalf("multi-drop = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あい", "あう"); d != 1 || a != "う" {
		t.Fatalf("replace = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あ", "あ"); d != 0 || a != "" {
		t.Fatalf("idle = (%d,%q)", d, a)
	}
	// 削除後の確定は飲み込まれない（古いマークのバグ）。
	if d, a := imeDiff("あ", "あい"); d != 0 || a != "い" {
		t.Fatalf("retype = (%d,%q)", d, a)
	}
}

// ime_test.go 内の TestCaretPixelsHeadless がキャレット位置を検証する；
// backend_test.go 側は input/edit の範囲を保持する。

// TestDropLastLocked はコンシューマ連鎖をまたぐ末尾切り詰めを検証する：
// まず保留分、次にフォーカス中のエディタ。
func TestDropLastLocked(t *testing.T) {
	b := mustNew(t)
	b.mu.Lock()
	b.imePending = "abcde"
	b.dropLastLocked(2)
	if b.imePending != "abc" {
		t.Fatalf("pending = %q, want abc", b.imePending)
	}
	b.imePending = "ab"
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	b.widgets[3] = &widgetEntry{id: 3, kind: wInput, selIdx: -1,
		edit: &inputState{text: []rune("XY"), caret: 2, focused: true}}
	b.dropLastLocked(3)
	if b.imePending != "" || string(b.widgets[3].edit.text) != "X" {
		t.Fatalf("edit = %q/%q", b.imePending, string(b.widgets[3].edit.text))
	}
	b.widgets[3].edit.focused = false
	b.widgets[3].edit.text = []rune("hello")
	b.widgets[3].edit.caret = 5
	b.imePending = ""
	b.dropLastLocked(2)
	e := b.widgets[3].edit
	if string(e.text) != "hello" || e.caret != 5 {
		t.Fatalf("unfocused edit = %q/%d (must be untouched)", string(e.text), e.caret)
	}
	b.mu.Unlock()
}

func TestReadImmediateHeadless(t *testing.T) {
	b := mustNew(t)
	// ヘッドレス: ループなし、何も入力されていない。
	if got := b.ReadImmediate(); got != "" {
		t.Fatalf("ReadImmediate = %q, want empty", got)
	}
	// フォーカス中の inputbox がストリームを所有している間、入力は無音の
	// ままである。
	b.mu.Lock()
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	b.widgets[4] = &widgetEntry{id: 4, kind: wInput, selIdx: -1,
		edit: &inputState{focused: true}}
	b.mu.Unlock()
	if got := b.ReadImmediate(); got != "" {
		t.Fatalf("ReadImmediate during edit = %q, want empty", got)
	}
	// 排出された IME の確定が input() を通って流れる。
	b.mu.Lock()
	b.widgets[4].edit.focused = false
	b.imePending = "あ"
	b.mu.Unlock()
	if got := b.ReadImmediate(); got != "あ" {
		t.Fatalf("ReadImmediate pending = %q, want あ", got)
	}
	b.mu.Lock()
	if b.imePending != "" {
		t.Fatalf("pending not drained: %q", b.imePending)
	}
	b.mu.Unlock()
}

func TestKeyForExtended(t *testing.T) {
	for _, c := range []int{8, 9, 16, 17, 18, 33, 34, 35, 36, 45, 46, 112, 123, 186, 192, 219, 222} {
		if _, ok := keyFor(c); !ok {
			t.Fatalf("keyFor(%d) should be known", c)
		}
	}
	if _, ok := keyFor(999); ok {
		t.Fatal("keyFor(999) should be unknown")
	}
	if _, ok := keyFor(124); ok {
		t.Fatal("keyFor(124) should be unknown")
	}
}

func TestSetFontFileHeadless(t *testing.T) {
	b := mustNew(t)
	base := FontPath()
	if base == "" {
		t.Skip("no system font")
	}
	// 裸のベース名の解決が同じファイルに当たる。
	name := base
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	got, err := resolveFontSpec(name)
	if err != nil {
		t.Fatalf("resolveFontSpec(%q): %v", name, err)
	}
	if !strings.EqualFold(got, base) {
		t.Fatalf("resolved = %q, want %q", got, base)
	}
	if err := b.SetFontFile(name, 24); err != nil {
		t.Fatalf("SetFontFile: %v", err)
	}
	if b.FontSize() != 24 {
		t.Fatalf("FontSize = %d, want 24", b.FontSize())
	}
	if b.FontFile() == "" {
		t.Fatal("FontFile should be set after SetFontFile")
	}
	if err := b.SetFontFile("no-such-font-xyz-123", 16); err == nil {
		t.Fatal("bogus font should error")
	}
	if err := b.SetFontFile(name, 7); err == nil {
		t.Fatal("size 7 should error")
	}
}

// TestFontPerUserDir はユーザーごとのフォントディレクトリ（非管理者
// インストールでの Windows の既定）での裸の名前の解決を扱う。スキャンは
// ファイル名のみを必要とするため、ダミーのファイルで十分である
// （解析は関与しない）。
func TestFontPerUserDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("per-user Fonts dir is a Windows concept")
	}
	dir := filepath.Join(t.TempDir(), "Microsoft", "Windows", "Fonts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	const name = "TestDummyPlemolJP-Regular.ttf"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", filepath.Dir(filepath.Dir(filepath.Dir(dir))))
	got, err := resolveFontSpec(name)
	if err != nil {
		t.Fatalf("resolveFontSpec(%q): %v", name, err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, name)) {
		t.Fatalf("resolved = %q, want under per-user Fonts", got)
	}
}

// TestFontExtraDirs は呼出し側が所有する検索ルート（スクリプト
// ディレクトリ、作業ディレクトリ）を扱う: 正確なファイル名、拡張子なし
// の名前（拡張子補完付き）、拡張子なしの前方一致は、いかなる OS でも
// システムディレクトリより先にそこへ解決される。スキャンはファイル名
// のみを必要とするため、ダミーのファイルで十分である（解析は関与しない）。
func TestFontExtraDirs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"ZZZSibExact.ttf",
		"ZZZSibExt.ttf",
		"ZZZSibPrefix-Regular.ttf",
		"ZZZSibPrefix-Bold.ttf",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("dummy"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// 正確なファイル名が追加ディレクトリ内で優先される。
	got, err := resolveFontSpec("ZZZSibExact.ttf", dir)
	if err != nil {
		t.Fatalf("resolveFontSpec(exact, dir): %v", err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, "ZZZSibExact.ttf")) {
		t.Fatalf("exact resolved = %q, want the sibling file", got)
	}
	// 拡張子なしの名前が追加ディレクトリ内で補完される。
	got, err = resolveFontSpec("zzzsibext", dir)
	if err != nil {
		t.Fatalf("resolveFontSpec(extless, dir): %v", err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, "ZZZSibExt.ttf")) {
		t.Fatalf("extless resolved = %q, want the sibling file", got)
	}
	// 拡張子なしの前方一致は追加ディレクトリ内で Regular フェイスを
	// 優先する。
	got, err = resolveFontSpec("zzzsibprefix", dir)
	if err != nil {
		t.Fatalf("resolveFontSpec(prefix, dir): %v", err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, "ZZZSibPrefix-Regular.ttf")) {
		t.Fatalf("prefix resolved = %q, want the Regular face", got)
	}
	// 追加ディレクトリがなければ同じ名前は未知である（兄弟は呼出し側が
	// それらを先に置いたときだけシステム検索に勝つ）。
	if _, err := resolveFontSpec("ZZZSibExact.ttf"); err == nil {
		t.Fatal("sibling name without its dir should error")
	}
}

// printFrags は実際の mes()/print() 経路をヘッドレスで駆動し、それが
// キャンバス層用に配置する断片（セル原点、テキスト、スタイル）を呼出順
// で返す。テキストは他の描画と同じくキャンバスバッファに焼き込まれる
// ため、これらの断片は Draw が最終的に示す配置状態である。呼出し側が
// それをラスタライズしたい場合は、準備されたまとまりを先に
// （Update または b.drainQueue() で）汲み上げなければならない。
func printFrags(t *testing.T, b *WindowBackend, s string, st TextStyle, newline bool) []textOp {
	t.Helper()
	d := b.printRaw(s, st, newline)
	if d == nil {
		return nil
	}
	out := make([]textOp, 0, len(d.ops))
	for _, op := range d.ops {
		if op.frag {
			out = append(out, op)
		}
	}
	return out
}

// fragLines は各行の断片を（左から右へ）読み手が見るテキストに結合し
// 直す: 各 print() 呼出はそれぞれ独自の断片だが、連続する呼出は同じ行を
// 続けるため、可視の構成がテストが確認するものである。
func fragLines(ops []textOp) []string {
	idx := map[int]int{}
	var out []string
	for _, op := range ops {
		if op.s == "" {
			continue
		}
		i, ok := idx[op.y]
		if !ok {
			i = len(out)
			idx[op.y] = i
			out = append(out, "")
		}
		out[i] += op.s
	}
	return out
}

// queuedLens は準備済み（pending）と準備完了（queue）の描画命令数を、
// つまりフレームのどれだけが確定したかを報告する。まだクロージャ用に
// フラッシュされていないまとめたテキストは準備済みとして数えられる。
func queuedLens(b *WindowBackend) (pending, queue int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	staged := len(b.pending)
	if !b.textBatch.empty() {
		staged++
	}
	return staged, len(b.queue)
}

func TestPrintlnState(t *testing.T) {
	b := mustNew(t)
	frags := append(printFrags(t, b, "Hello", 0, true), printFrags(t, b, "World", 0, true)...)
	if len(frags) != 2 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].s != "Hello" || frags[1].s != "World" {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].y != 0 || frags[1].y != 16 {
		t.Fatalf("rows: %+v", frags)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.curY != 32 {
		t.Fatalf("curY = %v, want 32", b.curY)
	}
}

// TestPrintlnHonorsPos は pos() + mes()/print() の位置決めを扱う:
// テキストの原点は pos() のピクセル自体（1px 刻み）であり、継続の
// print() は前に来たものの隣に着地する。
func TestPrintlnHonorsPos(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "plain", 0, true)
	b.MoveTo(37, 41) // 奇数のピクセル: セルへの丸めはしない
	frags = append(frags, printFrags(t, b, "hi", 0, true)...)
	if len(frags) != 2 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].x != 0 || frags[0].y != 0 || frags[0].s != "plain" {
		t.Fatalf("frags[0]: %+v", frags[0])
	}
	if frags[1].x != 37 || frags[1].y != 41 || frags[1].s != "hi" {
		t.Fatalf("frags[1]: %+v", frags[1])
	}
}

// TestPrintlnMultilinePos は継続行が行が始まったアンカー（pos() の
// 原点）に、1行のピッチ下がって戻ることを確認する。
func TestPrintlnMultilinePos(t *testing.T) {
	b := mustNew(t)
	b.MoveTo(24, 64)
	frags := printFrags(t, b, "a\nb", 0, true)
	if len(frags) != 2 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].x != 24 || frags[0].y != 64 || frags[0].s != "a" {
		t.Fatalf("frags[0]: %+v", frags[0])
	}
	if frags[1].x != 24 || frags[1].y != 80 || frags[1].s != "b" {
		t.Fatalf("frags[1]: %+v", frags[1])
	}
}

// TestPrintPosContinue は1つの層での print/pos/print の合成を固定する:
// 最初の print() の断片はそれが配置されたピクセルに留まり、継続は
// それらの隣に着地する。
func TestPrintPosContinue(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "ab", 0, false)
	b.MoveTo(32, 16)
	frags = append(frags, printFrags(t, b, "cd", 0, false)...)
	frags = append(frags, printFrags(t, b, "ef", 0, true)...)
	if len(frags) != 3 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].x != 0 || frags[0].y != 0 || frags[0].s != "ab" {
		t.Fatalf("frags[0]: %+v", frags[0])
	}
	if frags[1].x != 32 || frags[1].s != "cd" {
		t.Fatalf("frags[1]: %+v", frags[1])
	}
	// "cd" は 16px 描画したため、"ef" はセルではなくその 16px 後から
	// 始まる。
	if frags[2].x != 48 || frags[2].s != "ef" {
		t.Fatalf("frags[2]: %+v", frags[2])
	}
	if got := fragLines(frags); len(got) != 2 || got[0] != "ab" || got[1] != "cdef" {
		t.Fatalf("lines: %q, want [ab cdef]", got)
	}
}

// TestPrintWideContinue は CJK の継続の送りを固定する: 1回の print()
// 呼出で1文字を印字する（IME のエコーループがそうする）と重なっては
// ならない。
func TestPrintWideContinue(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "あ", 0, false)
	frags = append(frags, printFrags(t, b, "い", 0, false)...)
	if len(frags) != 2 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].x != 0 || frags[1].x != 16 {
		t.Fatalf("frag x = %d,%d, want 0,16", frags[0].x, frags[1].x)
	}
}

// TestTextScrollCarry は焼き込みテキストのスクロール計算を固定する:
// テキストはキャンバスバッファをずらすことでスクロールし、ずらした
// 各行は scrollExact に蓄積されるため、将来の lineHeight 比が小数でも
// ずらしたピクセルは正確なままである。単なる配置状態で、ゲームループは
// 不要。
func TestTextScrollCarry(t *testing.T) {
	b := mustNew(t)
	// 10px フォント: 行のピッチは 10px（lineHeight はサイズに応じて
	// スケールする）。
	if err := b.SetFontSize(10); err != nil {
		t.Fatalf("SetFontSize(10): %v", err)
	}
	if b.lineH != 10 {
		t.Fatalf("lineH = %v, want 10", b.lineH)
	}
	lines := int(float64(b.h)/b.lineH) + 1
	var scrolls []int
	for i := 0; i < lines; i++ {
		d := b.printRaw("x", 0, true)
		if d == nil {
			t.Fatalf("line %d laid out nothing", i)
		}
		for _, op := range d.ops {
			if op.scrollPx > 0 {
				scrolls = append(scrolls, op.scrollPx)
			}
		}
	}
	if len(scrolls) == 0 {
		t.Fatal("no scroll after filling the screen")
	}
	b.mu.Lock()
	exact, done, curX, curY := b.scrollExact, b.scrollDone, b.curX, b.curY
	lastY := float64(b.h) - b.lineH
	b.mu.Unlock()
	if curY != lastY || curX != 0 {
		t.Fatalf("cursor = (%v,%v), want (0,%v)", curX, curY, lastY)
	}
	sum := 0
	for _, px := range scrolls {
		sum += px
	}
	if sum != done {
		t.Fatalf("scroll pixels = %d, scrollDone = %d", sum, done)
	}
	if diff := exact - float64(done); diff < -0.5 || diff > 0.5 {
		t.Fatalf("scroll carry off by %v px (exact %v, shifted %d)", diff, exact, done)
	}
}

// TestPrintTabExpands はタブの描画を固定する: mes()/print() のタブは
// 半角セル4つ分の空白を送る（固定の送り幅でありタブストップではない）
// ため、断片はその空白を運び、カーソルはその後ろに着地する。
func TestPrintTabExpands(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "a\tb", 0, false)
	if len(frags) != 1 || frags[0].s != "a    b" {
		t.Fatalf("frags: %+v", frags)
	}
	b.mu.Lock()
	if b.curX != 48 { // "a" + 4×8px + "b"
		t.Fatalf("curX = %v, want 48", b.curX)
	}
	// タブは左端から離れていても固定の 32px 送りである。
	b.curX = 3
	b.mu.Unlock()
	frags = printFrags(t, b, "\tx", 0, false)
	if len(frags) != 1 || frags[0].s != "    x" || frags[0].x != 3 {
		t.Fatalf("offset frags: %+v", frags)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.curX != 43 { // 3 + 32 + "x"
		t.Fatalf("curX = %v, want 43", b.curX)
	}
}

// TestTextWidthTabs は strwidth() を mes() の描画と同じグリッドに保つ:
// そこでもタブは半角セル4つ分のコストである。
func TestTextWidthTabs(t *testing.T) {
	b := mustNew(t)
	if got, want := b.TextWidth("\t"), b.TextWidth("    "); got != want {
		t.Fatalf("tab width = %d, want %d", got, want)
	}
	if got, want := b.TextWidth("a\tb"), b.TextWidth("a    b"); got != want {
		t.Fatalf("a-tab-b width = %d, want %d", got, want)
	}
}

// TestPrintNewlineSplit は埋め込み改行を含む print() を扱う: 空でない
// 各行はそれぞれ独自の断片（1行に1つ）になり、カーソルは末尾の塊の
// 後ろで終わる。
func TestPrintNewlineSplit(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "a\nb\nc", 0, false)
	if len(frags) != 3 || frags[0].s != "a" || frags[1].s != "b" || frags[2].s != "c" {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].y != 0 || frags[1].y != 16 || frags[2].y != 32 {
		t.Fatalf("rows: %+v", frags)
	}
	// 左端の行: 継続行は x=0 に戻る。
	if frags[0].x != 0 || frags[1].x != 0 || frags[2].x != 0 {
		t.Fatalf("cols: %+v", frags)
	}
	if got := fragLines(frags); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("lines: %q", got)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.curY != 32 || b.curX != 8 { // "c" は幅 8px
		t.Fatalf("cursor = (%v,%v), want (8,32)", b.curX, b.curY)
	}
}

// TestPrintNewlineKeepsAnchor は左端から離れて印字された複数行の
// 文字列を扱う: 各埋め込み改行はカーソルを行が始まった x（pos() の
// アンカー）に、1行のピッチ下がって戻すため、mes() のブロックは矩形に
// 印字される。
func TestPrintNewlineKeepsAnchor(t *testing.T) {
	b := mustNew(t)
	b.MoveTo(80, 40) // 任意のピクセル原点。セルへの丸めはしない
	frags := printFrags(t, b, "ab\ncd", 0, false)
	if len(frags) != 2 || frags[0].s != "ab" || frags[1].s != "cd" {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].x != 80 || frags[0].y != 40 {
		t.Fatalf("frags[0] at (%d,%d), want (80,40)", frags[0].x, frags[0].y)
	}
	if frags[1].x != 80 || frags[1].y != 56 {
		t.Fatalf("frags[1] at (%d,%d), want (80,56)", frags[1].x, frags[1].y)
	}
	// 埋め込み改行を含む2回目の呼出も同じアンカーを保つ（カーソルは
	// 依然前のブロックが終わった行にある）。
	frags = printFrags(t, b, "ef\ngh", 0, false)
	if len(frags) != 2 || frags[1].x != 80 || frags[1].y != 72 {
		t.Fatalf("second block: %+v", frags)
	}
	// ハードな改行（mes）は次の行のアンカーを x=0 に戻す。
	_ = printFrags(t, b, "", 0, true)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.curY != 88 || b.curX != 0 || b.lineStartX != 0 {
		t.Fatalf("cursor = (%v,%v) anchor %v, want (0,88) anchor 0", b.curX, b.curY, b.lineStartX)
	}
}

// TestPrintJoinAcrossNewline は埋め込み改行をまたいだ print/print/mes の
// 合成を確認する: 部分は行ごとに1行として読める。
func TestPrintJoinAcrossNewline(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "ab", 0, false)
	frags = append(frags, printFrags(t, b, "c\nd", 0, false)...)
	frags = append(frags, printFrags(t, b, "e", 0, true)...)
	if got := fragLines(frags); len(got) != 2 || got[0] != "abc" || got[1] != "de" {
		t.Fatalf("lines: %q, want [abc de]", got)
	}
	if frags[len(frags)-1].y != 16 {
		t.Fatalf("last fragment row = %d, want 16", frags[len(frags)-1].y)
	}
}

// TestPrintTrailingNewline はカーソルを次の行できれいに残す（空の塊は
// 断片を発しない）。
func TestPrintTrailingNewline(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "a\n", 0, false)
	if len(frags) != 1 || frags[0].s != "a" {
		t.Fatalf("frags: %+v", frags)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.curY != 16 || b.curX != 0 {
		t.Fatalf("cursor = (%v,%v), want (0,16)", b.curX, b.curY)
	}
}

// TestPrintNewlineStyleChange は分割をまたいで断片ごとのスタイルを保つ:
// 各呼出はそれぞれ独自のスタイルを持つ独自の断片である。
func TestPrintNewlineStyleChange(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "a", StyleBold, false)
	frags = append(frags, printFrags(t, b, "b\nc", 0, false)...)
	if len(frags) != 3 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].s != "a" || frags[0].st != StyleBold {
		t.Fatalf("frags[0]: %+v", frags[0])
	}
	if frags[1].s != "b" || frags[1].st != 0 {
		t.Fatalf("frags[1]: %+v", frags[1])
	}
	if frags[2].s != "c" || frags[2].st != 0 || frags[2].y != 16 || frags[2].x != 0 {
		t.Fatalf("frags[2]: %+v", frags[2])
	}
}

func TestPrintStyles(t *testing.T) {
	b := mustNew(t)
	frags := printFrags(t, b, "a", StyleBold, true)
	frags = append(frags, printFrags(t, b, "b", StyleItalic, false)...)
	// 行の途中でのスタイル変更は問題ない: 次の呼出は独自の断片なので、
	// "b" はイタリックを保つ。
	frags = append(frags, printFrags(t, b, "c", 0, true)...)
	// 同じスタイルの print() 呼出も同じ行内の別々の断片のままである。
	frags = append(frags, printFrags(t, b, "x", StyleBold, false)...)
	frags = append(frags, printFrags(t, b, "y", StyleBold, false)...)
	frags = append(frags, printFrags(t, b, "z", StyleBold, true)...)
	if len(frags) != 6 {
		t.Fatalf("frags: %+v", frags)
	}
	if frags[0].st != StyleBold || frags[1].st != StyleItalic || frags[2].st != 0 {
		t.Fatalf("styles: %+v", frags)
	}
	if frags[0].s != "a" || frags[1].s != "b" || frags[2].s != "c" {
		t.Fatalf("texts: %+v", frags)
	}
	if frags[3].s != "x" || frags[4].s != "y" || frags[5].s != "z" {
		t.Fatalf("texts: %+v", frags)
	}
	if frags[3].st != StyleBold || frags[5].st != StyleBold {
		t.Fatalf("styles: %+v", frags)
	}
	// a が最初の行。b/c は2番目を共有。xyz は3番目を共有。
	if frags[0].y != 0 || frags[1].y != 16 || frags[2].y != 16 || frags[3].y != 32 || frags[5].y != 32 {
		t.Fatalf("rows: %+v", frags)
	}
	if got := fragLines(frags); len(got) != 3 || got[0] != "a" || got[1] != "bc" || got[2] != "xyz" {
		t.Fatalf("lines: %q, want [a bc xyz]", got)
	}
}

// TestTextStagedWithCanvas は mes()/print() がキャンバスのフレームに
// 乗ることを固定する: 連続する呼出は1つの準備済み命令にまとめられ、
// 境界でフレームの残りとともに適用されるため、Draw は入力途中のテキスト
// を決して見ず、テキストがその周りに描かれた図形を追い越したり
// 遅れたりすることはない。
func TestTextStagedWithCanvas(t *testing.T) {
	b := mustNew(t)
	// まだ yield がない: 準備されたまとまりはフラッシュされると直接
	// ゲームキューへ行く。
	b.Println("first", 0)
	if p, q := queuedLens(b); q != 0 || p != 1 {
		t.Fatalf("before yield: pending=%d queue=%d, want 1/0", p, q)
	}
	b.drainQueue()
	if p, q := queuedLens(b); q != 0 || p != 0 {
		t.Fatalf("after drain: pending=%d queue=%d, want 0/0", p, q)
	}
	// 最初の yield が準備モードに切り替わる: まとめたテキストは
	// フレームの残りとともに準備される。
	b.Await(0)
	b.Print("one\ntwo", 0)
	if p, q := queuedLens(b); q != 0 || p != 1 {
		t.Fatalf("after yield: pending=%d queue=%d, want 1/0", p, q)
	}
	// Update 単独では準備されたフレームを公開してはならない。
	if err := b.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p, q := queuedLens(b); q != 0 || p != 1 {
		t.Fatalf("Update flushed the staged text: pending=%d queue=%d", p, q)
	}
	// 次の境界がそれをフレームの残りとともに確定する。
	b.Await(0)
	if p, q := queuedLens(b); q != 1 || p != 0 {
		t.Fatalf("after boundary: pending=%d queue=%d, want 0/1", p, q)
	}
	b.drainQueue()
	if p, q := queuedLens(b); q != 0 || p != 0 {
		t.Fatalf("after drainQueue: pending=%d queue=%d, want 0/0", p, q)
	}
}

// TestTextAppliesWithoutYield: 一度も await/sleep しないスクリプトには
// フレーム境界がないため、Update がそのキュー済みテキストを適用する
// （画面外に留まってはならない）。スクリプトが yield した後は、
// yield 地点だけが確定する。

// TestTextBatchMergesBurst はまとめ処理の契約を固定する: 連続する
// mes()/print() 呼出は隣接する繰返しスクロールを畳み、1つのフラッシュ
// 地点を共有し、まとまりのすべてと統合されてもスクリプト順に適用される。
func TestTextBatchMergesBurst(t *testing.T) {
	b := mustNew(t)
	b.Println("one", 0)
	b.Print("two", 0)
	b.Println("three", 0)
	b.mu.Lock()
	if len(b.queue) != 0 {
		b.mu.Unlock()
		t.Fatalf("burst went straight to the game queue: %d", len(b.queue))
	}
	batch := b.textBatch
	if batch == nil || len(batch.ops) == 0 {
		b.mu.Unlock()
		t.Fatal("want a staged text batch")
	}
	scrolls, frags := 0, 0
	for _, op := range batch.ops {
		if op.scrollPx > 0 {
			scrolls++
		}
		if op.frag {
			frags++
		}
	}
	sel := batch.sel
	b.mu.Unlock()
	if sel != 0 {
		t.Fatalf("batch target = %d, want 0", sel)
	}
	if frags != 3 {
		t.Fatalf("batched fragments = %d, want 3 (one per call)", frags)
	}
	if scrolls > 1 {
		t.Fatalf("adjacent scrolls = %d, want none repeated", scrolls)
	}
	b.drainQueue()
	b.mu.Lock()
	left := len(b.queue)
	b.textBatch = nil
	b.mu.Unlock()
	if left != 0 {
		t.Fatalf("batched closures = %d, want 0", left)
	}
	if p, q := queuedLens(b); p != 0 || q != 0 {
		t.Fatalf("after drain: pending=%d queue=%d, want 0/0", p, q)
	}
}

// TestTextBatchFlushesBeforeShapes は順序の契約を固定する: まとめた
// テキストの後に描かれた図形はそれを先にフラッシュするため、共有の
// キャンバス層では呼出順が勝つ。
func TestTextBatchFlushesBeforeShapes(t *testing.T) {
	b := mustNew(t)
	// 準備モード: まとまりは図形より前に pending（生のキューではなく）
	// に着地しなければならない。これにより1つの境界が呼出順でそれらを
	// 確定する。
	b.Await(0)
	b.Println("first", 0)
	b.FillRect(0, 0, 10, 10, [4]int{255, 0, 0, 255})
	b.Println("second", 0)
	b.mu.Lock()
	staged := len(b.queue)
	pend := len(b.pending)
	batched := !b.textBatch.empty()
	b.textBatch = nil
	b.mu.Unlock()
	if staged != 0 {
		t.Fatalf("batched text went straight to the game queue: %d", staged)
	}
	if pend != 2 {
		t.Fatalf("pending = %d, want 2 (text, shape)", pend)
	}
	if !batched {
		t.Fatal("second println must remain batched after the shape")
	}
}

func TestTextAppliesWithoutYield(t *testing.T) {
	b := mustNew(t)
	b.Println("no await", 0)
	if err := b.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p, q := queuedLens(b); q != 0 || p != 0 {
		t.Fatalf("Update should apply the immediate text draw: pending=%d queue=%d", p, q)
	}
	// yield の後、Update は準備されたフレームをそのままにする。
	b.Await(0)
	b.Println("later", 0)
	if err := b.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p, q := queuedLens(b); q != 0 || p != 1 {
		t.Fatalf("Update published after a yield: pending=%d queue=%d", p, q)
	}
	b.Await(0)
	if p, q := queuedLens(b); q != 1 || p != 0 {
		t.Fatalf("await should commit the frame: pending=%d queue=%d", p, q)
	}
}

// TestCanvasFrameBoundary はキャンバスのちらつき修正を固定する: 最初の
// yield の後、描画命令は pending に準備され await()/sleep()/end で1つの
// フレームとしてフラッシュされ、Draw は確定した drawSel のみを観測する。
// 進行中の cls()->描画 列がキャンバスを1フレーム空白にしてはならない。
func TestCanvasFrameBoundary(t *testing.T) {
	b := mustNew(t)
	// まだ yield がない: enqueue は直接ゲームキューへ行く。
	b.FillRect(0, 0, 10, 10, [4]int{255, 0, 0, 255})
	b.mu.Lock()
	if len(b.pending) != 0 || len(b.queue) != 1 {
		b.mu.Unlock()
		t.Fatalf("before yield: pending=%d queue=%d, want 0/1", len(b.pending), len(b.queue))
	}
	b.mu.Unlock()
	b.drainQueue()
	b.mu.Lock()
	if len(b.queue) != 0 {
		b.mu.Unlock()
		t.Fatal("drainQueue should apply immediate commands")
	}
	b.mu.Unlock()
	// 最初の yield が準備モードに切り替わる。
	b.Await(0)
	b.FillRect(0, 0, 10, 10, [4]int{255, 0, 0, 255})
	b.mu.Lock()
	if len(b.pending) != 1 || len(b.queue) != 0 {
		pend, q := len(b.pending), len(b.queue)
		b.mu.Unlock()
		t.Fatalf("after yield: pending=%d queue=%d, want 1/0", pend, q)
	}
	b.mu.Unlock()
	// Update 単独では準備されたフレームを公開してはならない。
	if err := b.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	b.mu.Lock()
	if len(b.pending) != 1 {
		b.mu.Unlock()
		t.Fatal("Update must not flush a staged frame")
	}
	b.mu.Unlock()
	// 次の境界がそれを1つのまとまりとしてフラッシュする。
	b.Await(0)
	b.mu.Lock()
	if len(b.pending) != 0 || len(b.queue) != 1 {
		pend, q := len(b.pending), len(b.queue)
		b.mu.Unlock()
		t.Fatalf("after boundary: pending=%d queue=%d, want 0/1", pend, q)
	}
	b.mu.Unlock()
	b.drainQueue()
	// フレーム途中の gsel() は確定まで可視バッファを切り替えては
	// ならない。
	if err := b.SelectTarget(1); err != nil {
		t.Fatalf("SelectTarget: %v", err)
	}
	b.mu.Lock()
	if b.drawSel != 0 {
		b.mu.Unlock()
		t.Fatalf("gsel leaked to Draw before the boundary: drawSel=%d", b.drawSel)
	}
	b.mu.Unlock()
	b.Await(0)
	b.mu.Lock()
	if b.drawSel != 1 {
		b.mu.Unlock()
		t.Fatalf("drawSel=%d, want 1 after boundary", b.drawSel)
	}
	b.mu.Unlock()
}

// TestWidgetRegistryHeadless は手動の汲み上げ（ゲームループなし）で
// AddButton を駆動する: Draw/Update が実行されるまで生成経路は純粋な
// Go である。
func TestWidgetRegistryHeadless(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		addErr = b.AddButton(11, "T", 10, 420, 100, 36, 0)
		close(done)
	}()
	// 完了まで汲み上げる: 補助のゴルーチンはスケジューリングの時間を
	// 必要としうるため、固定の反復回数ではなく期限を設けてポーリングする。
	deadline := time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done:
			goto pumped
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("AddButton never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped:
	if addErr != nil {
		t.Fatalf("AddButton: %v", addErr)
	}
	if !b.HasWidget(11) {
		t.Fatal("button 11 missing after headless add")
	}
}

// TestIMEStateHeadless は汲み上げなしでフォーカス/問い合わせ/合成の
// ミラーを扱う（状態にはゲームループを必要としない）。
func TestIMEStateHeadless(t *testing.T) {
	b := mustNew(t)
	// 別のバックエンドのフォーカスが漏れてはならない: クリーンに始める。
	b.IMESet(false)
	if got := b.IMEState(); got != 0 {
		t.Fatalf("IMEState = %d, want 0", got)
	}
	if got := b.IMESet(true); got != 1 {
		t.Fatalf("IMESet(true) = %d, want 1", got)
	}
	if got := b.IMEState(); got != 1 {
		t.Fatalf("IMEState = %d, want 1", got)
	}
	if got := b.IMEComposition(); got != "" {
		t.Fatalf("IMEComposition = %q, want empty", got)
	}
	if got := b.IMESet(false); got != 0 {
		t.Fatalf("IMESet(false) = %d, want 0", got)
	}
}

// TestReadImmediateIdleHeadless: 実行中のループがなければ何も入力
// されないため、input は "" を報告しセッションを武装しない。
func TestReadImmediateIdleHeadless(t *testing.T) {
	b := mustNew(t)
	if got := b.ReadImmediate(); got != "" {
		t.Fatalf("ReadImmediate = %q, want empty", got)
	}
	if b.charSt != (charRep{}) {
		t.Fatal("charSt should be idle")
	}
}

// TestPicLoadFirst は回帰テスト: PicLoad が最初の画像操作である場合、
// nil の targets マップで panic してはならない（グローバルな picload）。
func TestPicLoadFirst(t *testing.T) {
	b := mustNew(t)
	if b.targets == nil || b.bufs == nil {
		t.Fatal("image maps must be initialized by New")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	done := make(chan struct{})
	var id int
	var loadErr error
	go func() {
		id, loadErr = b.PicLoad(p)
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done:
			goto pumped
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("PicLoad never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped:
	if loadErr != nil {
		t.Fatalf("PicLoad: %v", loadErr)
	}
	if id <= 0 {
		t.Fatalf("id = %d, want > 0", id)
	}
	if b.targets[id] == nil {
		t.Fatal("uploaded image missing from targets")
	}
}

// TestButtonImageHeadless はアイコンの経路を扱う: 未知のバッファは
// ループなしでエラーになり、picload バッファは汲み上げにより
// ヘッドレスで装着できる。
func TestButtonImageHeadless(t *testing.T) {
	b := mustNew(t)
	if err := b.AddButton(11, "T", 10, 420, 100, 36, 99); err == nil {
		t.Fatal("unknown image buffer should error")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "icon.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	done := make(chan struct{})
	var imgID int
	var loadErr error
	go func() {
		imgID, loadErr = b.PicLoad(p)
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done:
			goto pumped
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("PicLoad never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped:
	if loadErr != nil {
		t.Fatalf("PicLoad: %v", loadErr)
	}
	done2 := make(chan struct{})
	var addErr error
	go func() {
		addErr = b.AddButton(12, "Icon", 120, 420, 100, 36, imgID)
		close(done2)
	}()
	deadline = time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done2:
			goto pumped2
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("AddButton never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped2:
	if addErr != nil {
		t.Fatalf("AddButton with image: %v", addErr)
	}
	if !b.HasWidget(12) {
		t.Fatal("image button 12 missing after headless add")
	}
}

// TestButtonImageStatesHeadless はゲームループなしで4つの画像スロット
// （通常/ホバー/押下/無効）を検証する。
func TestButtonImageStatesHeadless(t *testing.T) {
	b := mustNew(t)
	if err := b.AddButton(21, "T", 10, 10, 100, 36, 1, 2, 3, 4, 5); err == nil {
		t.Fatal("5 images should error")
	}
	if err := b.AddButton(21, "T", 10, 10, 100, 36, 99); err == nil {
		t.Fatal("unknown buffer should error")
	}
	if err := b.AddButton(21, "T", 10, 10, 100, 36, 0, 99); err == nil {
		t.Fatal("unknown hover buffer should error")
	}
}

// renderGame はすべてのピクセル探りを1回の RunGame で実行する: Ebiten
// は1プロセスにつき1つのゲームループしか許さず、描画命令は1フレーム
// 後ろにまとまるため、各段階は2フレーム描画し次のフレームでサンプリング
// する。pxRes は1回の GetPixel の往復結果を運ぶ。
type pxRes struct {
	c   [4]int
	err error
}

type renderGame struct {
	b                              *WindowBackend
	face                           *text.GoTextFace
	frames                         int
	fillLit, textLit, backLit      int
	rectLit, circleLit, anyLit     int
	minX, minY, maxX, maxY         int
	keyIdle, mouseX, mouseY        int
	clickedVal                     bool
	probedInput                    bool
	filled, texted, backed, shaped bool
	done                           bool
	wLaunched, wPhase2Launched     bool
	wChecked1, wChecked2           bool
	wPhase1, wPhase2               chan struct{}
	saveLaunched, saveChecked      bool
	savePath                       string
	saveDone                       chan struct{}
	saveErr                        error
	gzChecked                      bool
	pfChecked                      bool
	pxLaunched, pxChecked          bool
	pxDone                         chan pxRes
	sliderLaunched, sliderChecked  bool
	sliderDone                     chan struct{}
	sliderErr                      error
	sliderSettle, sliderBlueMax    int
	edLaunched, edMeasured         bool
	edRow                          float64
	edFrames, edDistinct           int
	edHashes                       []uint64
	textAlone, textCovered, textOn int
	imeCompChecked                 bool
	bufChecked                     bool
	resized                        bool
	resizeFail                     string
	wFail                          string
	wText                          string
	wErrs                          []string
}

func (g *renderGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	// 実際のループと同じようにバックエンドを汲み上げる（描画キューを
	// 排出する）。
	_ = g.b.Update()
	return nil
}

func (g *renderGame) Draw(screen *ebiten.Image) {
	g.frames++
	switch {
	case g.frames <= 2:
		screen.Fill(color.NRGBA{0xFF, 0, 0, 0xFF})
		if g.frames == 2 {
			if r, _, _, _ := screen.At(100, 100).RGBA(); r > 0 {
				g.fillLit = 1
			}
			g.filled = true
		}
	case g.frames <= 4:
		screen.Fill(color.Black)
		g.drawDirectText(screen)
		if g.frames == 4 {
			g.textLit = countLit(screen, 0, 0, 160, 40)
			g.texted = true
		}
	case g.frames <= 6:
		g.b.Draw(screen)
		if g.frames == 6 {
			// 白いウィンドウ: ループ前の mes() のテキストは黒いインク
			// なので、行の探りは点灯したピクセルではなく暗いピクセルを
			// 数える。ゼロはバックエンドが何も描かなかったことを意味し、
			// 全体が埋まった領域は黒い画面を意味する（どちらも失敗で、
			// 最後に検査される）。
			g.backLit = countDark(screen, 0, 0, 320, 40)
			g.backed = true
		}
	case g.frames == 7:
		// スクリプトがするようにエンキューし、それから同期的に排出する:
		// ebiten は Update/Draw の 1:1 対応を保証しないため、テストは
		// 次の Update が実行済みであることに依存してはならない。
		g.b.FillRect(300, 100, 40, 30, [4]int{255, 0, 0, 255})
		g.b.Circle(100, 300, 20, true, [4]int{0, 255, 0, 255})
		g.b.drainQueue()
		g.b.Draw(screen)
	case g.frames == 8:
		// テキスト/図形の重なり探り（テキストはキャンバス層を共有する）。
		g.checkTextLayer(screen)
	case g.frames >= 9:
		g.b.Draw(screen)
		if r, _, _, _ := screen.At(310, 110).RGBA(); r > 0x8000 {
			g.rectLit = 1
		}
		if _, gg, _, _ := screen.At(100, 300).RGBA(); gg > 0x8000 {
			g.circleLit = 1
		}
		g.anyLit = countDark(screen, 0, 30, 640, 480)
		g.minX, g.minY, g.maxX, g.maxY = 9999, 9999, -1, -1
		for y := 0; y < 480; y++ {
			for x := 0; x < 640; x++ {
				r, gg, bl, _ := screen.At(x, y).RGBA()
				// 白いウィンドウ上のインクの境界（診断ログのみ）。
				if r < 0x8000 && gg < 0x8000 && bl < 0x8000 {
					if x < g.minX {
						g.minX = x
					}
					if x > g.maxX {
						g.maxX = x
					}
					if y < g.minY {
						g.minY = y
					}
					if y > g.maxY {
						g.maxY = y
					}
				}
			}
		}
		g.shaped = true
		// ループ内の入力問い合わせ（ループ外では安全でない）。
		// アイドル状態の矢印は削除された stick() の探りの代役である。
		if left, ok := g.b.KeyDown(37); ok && left {
			g.keyIdle = 1
		}
		if up, ok := g.b.KeyDown(38); ok && up {
			g.keyIdle = 1
		}
		g.mouseX, g.mouseY = g.b.MousePos()
		g.clickedVal = g.b.Clicked()
		g.probedInput = true
		// ウィジェット段階: ブロックするバックエンドの呼出は補助の
		// ゴルーチンで実行される（Update を待つため、決してこのスレッドで
		// 呼んではならない）。フェーズ1が作成し、フェーズ2はゲームスレッドの
		// 検査後に削除する。
		if !g.wLaunched {
			g.wLaunched = true
			g.wPhase1 = make(chan struct{})
			go g.makeWidgets()
		}
		select {
		case <-g.wPhase1:
			// 閉じたチャネルは準備完了のまま: ちょうど一度だけ確認する。
			if !g.wChecked1 {
				g.wChecked1 = true
				g.checkWidgetsPhase1()
				if g.wFail == "" && !g.wPhase2Launched {
					g.wPhase2Launched = true
					g.wPhase2 = make(chan struct{})
					go g.removeWidgets()
				}
			}
		default:
		}
		select {
		case <-g.wPhase2:
			if !g.wChecked2 {
				g.wChecked2 = true
				g.checkWidgetsPhase2()
			}
		default:
		}
		// 保存段階: 補助のゴルーチンでバッファ 0 を PNG にスナップ
		// ショットする（SavePNG は Update を待つため、決してこのスレッドで
		// 呼んではならない）。
		if g.wChecked2 && g.wFail == "" && !g.saveChecked {
			if !g.saveLaunched {
				g.saveLaunched = true
				g.saveDone = make(chan struct{})
				go func() {
					if err := g.b.SavePNG(g.savePath, 0); err != nil {
						g.saveErr = err
					}
					close(g.saveDone)
				}()
			}
			select {
			case <-g.saveDone:
				g.saveChecked = true
				g.checkSaved()
			default:
			}
		}
		// 拡大段階: ゲームスレッドで 2 倍にブリットし、それから再標本化
		// する。保存のスナップショットより後に実行されるため、
		// バッファ 0 はそれに対して元のままである。
		if g.saveChecked && g.wFail == "" && !g.gzChecked {
			g.gzChecked = true
			g.checkZoom(screen)
		}
		// 塗り段階: 白い四角形、それから塗りつぶし、それから再標本化。
		if g.gzChecked && g.wFail == "" && !g.pfChecked {
			g.pfChecked = true
			g.checkPaint(screen)
		}
		// ピクセル段階: pget の往復（スクリプトのように SetPixel し、
		// 補助のゴルーチンで GetPixel で読み戻す — それは Update を待つ
		// ため、決してこのスレッドで呼んではならない）。塗り段階の後に
		// 実行されるため、先行する塗りが探りのピクセルを上書きできない。
		if g.pfChecked && g.wFail == "" && !g.pxChecked {
			if !g.pxLaunched {
				g.pxLaunched = true
				g.b.SetPixel(500, 200, [4]int{255, 0, 0, 255})
				g.pxDone = make(chan pxRes, 1)
				go func() {
					c, err := g.b.GetPixel(500, 200)
					g.pxDone <- pxRes{c, err}
				}()
			}
			select {
			case r := <-g.pxDone:
				g.pxChecked = true
				if r.err != nil {
					g.wFail = "pget: " + r.err.Error()
				} else if r.c != [4]int{255, 0, 0, 255} {
					g.wFail = fmt.Sprintf("pget roundtrip = %v, want opaque red", r.c)
				}
			default:
			}
		}
		// IME バー段階: 合成ボックスの高さ（太い句の下線が枠の内側に
		// 留まらなければならない）。同じ左上の隅を占有する ReadLine の
		// プロンプト段階より前に、そしてクリーンなキャンバスから始まる
		// バッファ段階の後にのみ実行される。
		if g.bufChecked && g.wFail == "" && !g.imeCompChecked {
			g.imeCompChecked = true
			g.checkIMEComp(screen)
		}
		// リスト・スライダー段階: listbox は垂直スライダーを保つため、
		// 項目（および選択行のハイライト）はスライダーの帯が始まる位置で
		// 終わり、その下に染み出してはならない。これは描画側から
		// 16px の収拾予算を証明する。ピクセル段階の後に実行されるため、
		// 先行する塗りが探りの領域（左下、他のすべての段階から離れた
		// 位置）を上書きできない。
		if g.pxChecked && g.wFail == "" && !g.sliderChecked {
			if !g.sliderLaunched {
				g.sliderLaunched = true
				g.sliderDone = make(chan struct{})
				go func() {
					defer close(g.sliderDone)
					if err := g.b.AddList(71, 20, 360, 200, 100, []string{"alpha", "beta", "gamma", "delta"}); err != nil {
						g.sliderErr = err
						return
					}
					if err := g.b.SelectList(71, 0); err != nil {
						g.sliderErr = err
					}
				}()
			}
			select {
			case <-g.sliderDone:
				if g.sliderErr != nil {
					g.wFail = "list slider setup: " + g.sliderErr.Error()
					g.sliderChecked = true
				} else if g.sliderSettle < 2 {
					// サンプリング前にレイアウトと2回の描画を落ち着かせる。
					g.sliderSettle++
				} else {
					g.sliderChecked = true
					g.checkListSlider(screen)
				}
			default:
			}
		}
		// バッファ段階: buffer(id, w, h) し、それからオフスクリーン画像を
		// キャンバスに gcopy で戻す。ピクセル段階の後に実行されるため、
		// 先行する段階の塗りがそのまま残る。空いている隅
		// (500,100)-(600,180) を探る。
		if g.sliderChecked && g.wFail == "" && !g.bufChecked {
			g.bufChecked = true
			g.checkBuffer(screen)
		}
		// エディタ段階: 有効な ReadLine のプロンプトを保持し、毎フレーム
		// その行をハッシュする。安定したプロンプトはちょうど1つの異なる
		// ハッシュを生じる。
		if g.imeCompChecked && g.wFail == "" {
			if !g.edLaunched {
				g.edLaunched = true
				go func() { _, _ = g.b.ReadLine("name? ") }()
				g.b.mu.Lock()
				// curY は今やプロンプト行のピクセル原点である。
				g.edRow = g.b.curY
				g.b.mu.Unlock()
			} else if g.edFrames < 90 {
				g.edHashes = append(g.edHashes, hashRow(screen, int(g.edRow)))
				g.edFrames++
			} else if !g.edMeasured {
				g.edMeasured = true
				seen := map[uint64]bool{}
				for _, h := range g.edHashes {
					seen[h] = true
				}
				g.edDistinct = len(seen)
				// Resize 段階は最後: キャンバスを大きくするため、
				// エディタ段階がハッシュした行の帯を乱れてはならない。
				g.checkResize(screen)
				g.done = true
			}
		}
		if g.frames > 1200 && !g.done {
		// 最初の失敗を保持する: 上書きするとそれを隠してしまう。
			if g.wFail == "" {
				g.wFail = "widget stage timed out"
			}
			g.done = true
		}
	}
}

// absDiff32 は2つの16ビット標本のチャネルごとの距離である。
func absDiff32(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// checkListSlider は探り用の listbox を走査して選択青の範囲を調べる:
// 16px のスライダー帯が予約されているため、選択行はリストの右端
// （x=220）に大きく届かないところで止まらなければならない。削除された
// スライダーの下に染み出たハイライトは約 212 を超えてしまう。
func (g *renderGame) checkListSlider(screen *ebiten.Image) {
	const lx, ly, lw, lh = 20, 360, 200, 100
	pr, pg, pb, _ := color.NRGBA{204, 228, 247, 0xFF}.RGBA()
	maxX := -1
	for y := ly; y < ly+lh; y++ {
		for x := lx; x < lx+lw; x++ {
			r, gg, bb, _ := screen.At(x, y).RGBA()
			if absDiff32(r, pr) < 0x1000 && absDiff32(gg, pg) < 0x1000 && absDiff32(bb, pb) < 0x1000 {
				if x > maxX {
					maxX = x
				}
			}
		}
	}
	g.sliderBlueMax = maxX
	if maxX < 60 {
		g.wFail = "list selected row missing (no select-blue found)"
		return
	}
	if maxX > lx+lw-12 {
		g.wFail = fmt.Sprintf("list highlight bleeds into the slider strip (blue to x=%d)", maxX)
	}
}

// hashRow はピクセル y0 から始まる20pxのテキスト帯（4個おきのx、
// 2個おきのy）を FNV-1a でハッシュする。y0 は画面内に切り詰められる
// ため、キャンバスの大きく下（または外）にあるカーソルでも範囲外を
// 読めない。
func hashRow(screen *ebiten.Image, y0 int) uint64 {
	const basis, prime = uint64(14695981039346656037), uint64(1099511628211)
	h := basis
	if y0 < 0 {
		y0 = 0
	}
	if y0 > 480-20 {
		y0 = 480 - 20
	}
	for y := y0; y < y0+20; y += 2 {
		for x := 0; x < 640; x += 4 {
			r, gg, bl, _ := screen.At(x, y).RGBA()
			h ^= uint64(r>>8) + 1
			h *= prime
			h ^= uint64(gg>>8) + 1
			h *= prime
			h ^= uint64(bl>>8) + 1
			h *= prime
		}
	}
	return h
}

// makeWidgets はスクリプトがするようにウィジェットの生成を実行する。
func (g *renderGame) makeWidgets() {
	defer close(g.wPhase1)
	if err := g.b.AddButton(11, "T", 10, 420, 100, 36, 0); err != nil {
		g.wErrs = append(g.wErrs, "button: "+err.Error())
	}
	if err := g.b.AddInput(12, 120, 420, 160, 36, "init"); err != nil {
		g.wErrs = append(g.wErrs, "input: "+err.Error())
	}
	if err := g.b.AddList(13, 290, 380, 150, 80, []string{"a", "b"}); err != nil {
		g.wErrs = append(g.wErrs, "list: "+err.Error())
	}
	t, err := g.b.InputText(12)
	if err != nil {
		g.wErrs = append(g.wErrs, "inputtext: "+err.Error())
	}
	g.wText = t
	if err := g.b.AddCheck(21, "Agree", 10, 300, 200, 32, true); err != nil {
		g.wErrs = append(g.wErrs, "chkbox: "+err.Error())
	}
	if err := g.b.AddCombo(22, 220, 300, 200, 36, []string{"x", "y"}, 1); err != nil {
		g.wErrs = append(g.wErrs, "combox: "+err.Error())
	}
	if err := g.b.AddArea(23, 430, 300, 190, 60, "l1"); err != nil {
		g.wErrs = append(g.wErrs, "mesbox: "+err.Error())
	}
}

// checkSaved は pngsave のスナップショット（ゲームスレッド、純粋な Go の
// デコード）を復号し、バッファ 0 が往復した（サイズおよび段階7の図形）
// を検証する。
func (g *renderGame) checkSaved() {
	if g.saveErr != nil {
		g.wFail = "pngsave: " + g.saveErr.Error()
		return
	}
	f, err := os.Open(g.savePath)
	if err != nil {
		g.wFail = "pngsave missing: " + err.Error()
		return
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		g.wFail = "pngsave decode: " + err.Error()
		return
	}
	if w, h := img.Bounds().Dx(), img.Bounds().Dy(); w != 640 || h != 480 {
		g.wFail = fmt.Sprintf("pngsave size = %dx%d, want 640x480", w, h)
		return
	}
	r, _, _, _ := img.At(310, 110).RGBA()
	if r < 0x8000 {
		g.wFail = "pngsave lost the red rect"
		return
	}
	_, gg, _, _ := img.At(100, 300).RGBA()
	if gg < 0x8000 {
		g.wFail = "pngsave lost the green circle"
	}
}

// checkZoom はバッファ 1 に20pxの赤い四角形を描き、それを2倍に
// ブリットしてバッファ 0 に写し、再描画してスケールされたピクセルを
// 検証する（ゲームスレッド）。変換先はウィジェットの矩形を避ける
// （ui はキャンバスの上に描画する）。
func (g *renderGame) checkZoom(screen *ebiten.Image) {
	b := g.b
	b.SelectTarget(1)
	b.FillRect(0, 0, 20, 20, [4]int{255, 0, 0, 255})
	b.drainQueue()
	b.SelectTarget(0)
	if err := b.BlitScaled(1, 0, 0, 20, 20, 2, 2, 460, 200); err != nil {
		g.wFail = "gzoom: " + err.Error()
		return
	}
	b.drainQueue()
	b.Draw(screen)
	if r, _, _, _ := screen.At(470, 210).RGBA(); r < 0x8000 {
		g.wFail = "gzoom pixel not scaled"
		return
	}
	// 対象の外ではキャンバスは透明なので、白いウィンドウが透ける。
	// それより暗いものは漏れた。
	if r, gg, bl, _ := screen.At(455, 195).RGBA(); r < 0xFFFF || gg < 0xFFFF || bl < 0xFFFF {
		g.wFail = "gzoom leaked outside the target rect"
	}
}

// checkPaint は白い四角形を赤で塗り潰し、塗りと漏れがないことを検証
// する（ゲームスレッド）。四角形はウィジェットの矩形を避ける
// （ui はキャンバスの上に描画する）。
func (g *renderGame) checkPaint(screen *ebiten.Image) {
	b := g.b
	b.FillRect(460, 360, 40, 40, [4]int{255, 255, 255, 255})
	b.drainQueue()
	if err := b.FloodFill(480, 380, [4]int{255, 0, 0, 255}); err != nil {
		g.wFail = "paint: " + err.Error()
		return
	}
	b.drainQueue()
	b.Draw(screen)
	if r, gg, bl, _ := screen.At(480, 380).RGBA(); r < 0x8000 || gg > 0 || bl > 0 {
		g.wFail = "paint fill missing"
		return
	}
	// 領域の外ではキャンバスは透明（白いウィンドウ）。
	if r, gg, bl, _ := screen.At(455, 395).RGBA(); r < 0xFFFF || gg < 0xFFFF || bl < 0xFFFF {
		g.wFail = "paint leaked outside the region"
	}
}

// removeWidgets はフェーズ1の検査の後に実行される。
func (g *renderGame) removeWidgets() {
	defer close(g.wPhase2)
	if err := g.b.RemoveWidget(11); err != nil {
		g.wErrs = append(g.wErrs, "remove: "+err.Error())
	}
}

// checkWidgetsPhase1 はゲームスレッドで実行される（ここではミューテックスの
// みの呼出が安全）。
func (g *renderGame) checkWidgetsPhase1() {
	if len(g.wErrs) > 0 {
		g.wFail = g.wErrs[0]
		return
	}
	if !g.b.HasWidget(11) || !g.b.HasWidget(12) || !g.b.HasWidget(13) {
		g.b.mu.Lock()
		keys := make([]int, 0)
		kinds := map[int]int{}
		for k, v := range g.b.widgets {
			keys = append(keys, k)
			kinds[k] = int(v.kind)
		}
		nkids := 0
		var rects int
		if g.b.root != nil {
			nkids = len(g.b.root.Children())
		}
		if g.b.fixLayout != nil {
			rects = len(g.b.fixLayout.rects)
		}
		g.b.mu.Unlock()
		g.wFail = fmt.Sprintf("widgets should exist (keys=%v kinds=%v kids=%d rects=%d errs=%v text=%q)",
			keys, kinds, nkids, rects, g.wErrs, g.wText)
		return
	}
	if g.wText != "init" {
		g.wFail = "inputtext mismatch: " + g.wText
		return
	}
	if !g.b.HasWidget(21) || !g.b.HasWidget(22) || !g.b.HasWidget(23) {
		g.wFail = "form widgets should exist"
		return
	}
	if p, err := g.b.Checked(21); err != nil || !p {
		g.wFail = "fresh checkbox must be checked"
		return
	}
	if idx, err := g.b.SelectedIndex(22); err != nil || idx != 1 {
		g.wFail = "fresh combo selection must be 1"
		return
	}
	if s, err := g.b.AreaText(23); err != nil || s != "l1" {
		g.wFail = "mesbox text mismatch: " + s
		return
	}
	if p, err := g.b.Pressed(11); err != nil || p {
		g.wFail = "fresh button must be unpressed"
		return
	}
	if _, err := g.b.Pressed(12); err == nil {
		g.wFail = "pressed on inputbox should error"
		return
	}
	idx, err := g.b.SelectedIndex(13)
	if err != nil || idx != -1 {
		g.wFail = "fresh list selection must be -1"
		return
	}
}

// checkWidgetsPhase2 は削除後にゲームスレッドで実行される。
func (g *renderGame) checkWidgetsPhase2() {
	if len(g.wErrs) > 0 && g.wFail == "" {
		g.wFail = g.wErrs[0]
		return
	}
	if g.b.HasWidget(11) {
		g.wFail = "button 11 should be removed"
		return
	}
	if _, err := g.b.Pressed(11); err == nil {
		g.wFail = "pressed on removed button should error"
		return
	}
	if _, err := g.b.SelectedIndex(99); err == nil {
		g.wFail = "selected on unknown id should error"
		return
	}
}

func (g *renderGame) drawDirectText(screen *ebiten.Image) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(0, g.face.Metrics().HAscent)
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
	text.Draw(screen, "Hello", g.face, op)
}

func countLit(screen *ebiten.Image, x0, y0, x1, y1 int) int {
	lit := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, gg, bl, _ := screen.At(x, y).RGBA()
			if r > 0 || gg > 0 || bl > 0 {
				lit++
			}
		}
	}
	return lit
}

// countDark はほぼ黒のピクセルを数える: 白いウィンドウでは、バックエンド
// のインク（既定の黒文字）は明るい背景に暗い色として現れ、countLit の
// 逆である。
func countDark(screen *ebiten.Image, x0, y0, x1, y1 int) int {
	dark := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, gg, bl, _ := screen.At(x, y).RGBA()
			if r < 0x8000 && gg < 0x8000 && bl < 0x8000 {
				dark++
			}
		}
	}
	return dark
}

// countWhite は矩形内のほぼ白の（グリフの）ピクセルを数えるため、探りが
// 色付きの図形の上に描かれたテキストを図形自身と区別できる。
func countWhite(screen *ebiten.Image, x0, y0, x1, y1 int) int {
	lit := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, gg, bl, _ := screen.At(x, y).RGBA()
			if r > 0x8000 && gg > 0x8000 && bl > 0x8000 {
				lit++
			}
		}
	}
	return lit
}

// checkTextLayer はループ内で mes()/print() のピクセルが rect()/pset() と
// 同じキャンバス層にあることを検証する: 呼出順がどちらがどちらを覆うか
// を決める。探りは他の段階が触れる図形やウィジェットから離れた画面領域
// (440,100)-(480,120) を使う。
func (g *renderGame) checkTextLayer(screen *ebiten.Image) {
	b := g.b
	const x0, y0 = 440, 80 // 探りの領域（図形とウィジェットから離れて）
	x1, y1 := x0+40, y0+20
	// 明示的な白いインク: 探りは重なり（呼出順）をテストするため、
	// 既定の描画色ではなく、fg の既定に依存してはならない。
	b.SetColor(255, 255, 255, 255)
	// rect() が探りの領域を消去し、それから mes() がその上に描く。
	b.FillRect(x0, y0, x1-x0, y1-y0, [4]int{0, 0, 0, 255})
	b.mu.Lock()
	b.curX, b.curY = float64(x0), float64(y0)
	b.mu.Unlock()
	b.Println("HHHH", 0)
	b.drainQueue()
	b.Draw(screen)
	g.textAlone = countLit(screen, x0, y0, x1, y1)
	// 後ろの rect() がテキストを覆う: 1つの層、呼出順が勝つ。
	b.FillRect(x0, y0, x1-x0, y1-y0, [4]int{0, 0, 0, 255})
	b.drainQueue()
	b.Draw(screen)
	g.textCovered = countLit(screen, x0, y0, x1, y1)
	// 図形の後のテキストはそれの上に描かれる（緑の上に白いグリフ）。
	b.FillRect(x0, y0, x1-x0, y1-y0, [4]int{0, 128, 0, 255})
	b.mu.Lock()
	b.curX, b.curY = float64(x0), float64(y0)
	b.mu.Unlock()
	b.Println("HHHH", 0)
	b.drainQueue()
	b.Draw(screen)
	g.textAlone = countLit(screen, x0, y0, x1, y1)
	// 後ろの rect() がテキストを覆う: 1つの層、呼出順が勝つ。
	b.FillRect(x0, y0, x1-x0, y1-y0, [4]int{0, 0, 0, 255})
	b.drainQueue()
	b.Draw(screen)
	g.textCovered = countLit(screen, x0, y0, x1, y1)
	// 図形の後のテキストはそれの上に描かれる（緑の上に白いグリフ）。
	b.FillRect(x0, y0, x1-x0, y1-y0, [4]int{0, 128, 0, 255})
	b.mu.Lock()
	b.curX, b.curY = float64(x0), float64(y0)
	b.mu.Unlock()
	b.Println("HHHH", 0)
	b.drainQueue()
	b.Draw(screen)
	g.textOn = countWhite(screen, x0, y0, x1, y1)
}

// checkBuffer はループ内で buffer(id, w, h) を端から端まで実行する
// （ピクセルの読み戻しにはゲームの実行が必要）: オフスクリーンの
// バッファを作成し、gsel を通じてそれに描画し、それからキャンバスに
// gcopy で戻してピクセルを探る。
func (g *renderGame) checkBuffer(screen *ebiten.Image) {
	b := g.b
	if err := b.NewBuffer(2, 100, 80); err != nil {
		g.wFail = "buffer: " + err.Error()
		return
	}
	if !b.hasBuf(2) {
		g.wFail = "buffer(2, ...) did not allocate id 2"
		return
	}
	b.SelectTarget(2)
	b.FillRect(0, 0, 100, 80, [4]int{255, 0, 0, 255})
	b.SelectTarget(0)
	b.drainQueue()
	if err := b.Blit(2, 0, 0, 100, 80, 500, 100); err != nil {
		g.wFail = "gcopy from buffer 2: " + err.Error()
		return
	}
	b.drainQueue()
	b.Draw(screen)
	if r, gg, bl, _ := screen.At(520, 140).RGBA(); r < 0x8000 || gg > 0 || bl > 0 {
		g.wFail = "gcopy from the buffer() image is missing"
		return
	}
	// ブリットの外ではキャンバスはそのまま（ここでは透明なので、白い
	// ウィンドウが透ける）。
	if r, gg, bl, _ := screen.At(480, 140).RGBA(); r < 0xFFFF || gg < 0xFFFF || bl < 0xFFFF {
		g.wFail = "gcopy from the buffer() image leaked"
	}
}

// checkIMEComp は合成バーの高さをゲームスレッドで固定する（ピクセルの
// 読み戻しにはループが必要）: ボックスは1つのテキスト行 + 2px なので、
// 枠は行とその下の行をなお通り、3px の句の下線が枠の内側に留まる。
func (g *renderGame) checkIMEComp(screen *ebiten.Image) {
	b := g.b
	b.Clear() // 探りがバーのみを見るようクリーンなキャンバスに
	b.mu.Lock()
	b.imeComposing = "あいう"
	b.imeClause = "い"
	b.imeClauseStart, b.imeClauseEnd = 1, 2
	lh := b.lineH
	b.mu.Unlock()
	b.drainQueue()
	b.Draw(screen)
	b.mu.Lock()
	b.imeComposing = ""
	b.imeClause = ""
	b.imeClauseStart, b.imeClauseEnd = 0, 0
	b.mu.Unlock()
	// bh = lineH + 2. The composition text ("あいう", clause "い") is
	// 3つの全角セルの幅なので、右の枠の列は最後のグリフのすぐ後ろに
	// 来る: 枠は lineH のところを通り続け、その下端は lineH+1 にある
	// （余分な 2px がなければ lineH-1 にあり、3px の句の下線が枠から
	// はみ出す）。その下には何も点灯しない。白いウィンドウ: 枠は灰色
	// （背景より暗い）で、バーの下はただの背景の白である。
	tw, _ := text.Measure("あいう", b.face, 0)
	probeX := int(tw+2*2) - 1
	if r, _, _, _ := screen.At(probeX, int(lh)).RGBA(); r == 0xFFFF {
		g.wFail = fmt.Sprintf("IME bar frame missing at y=%d", int(lh))
		return
	}
	if r, _, _, _ := screen.At(probeX, int(lh)+1).RGBA(); r == 0xFFFF {
		g.wFail = fmt.Sprintf("IME bar has no bottom frame at y=%d: the bar is only lineH tall", int(lh)+1)
		return
	}
	if r, gg, bl, _ := screen.At(probeX, int(lh)+2).RGBA(); r < 0xFFFF || gg < 0xFFFF || bl < 0xFFFF {
		g.wFail = fmt.Sprintf("IME bar leaks past y=%d", int(lh)+2)
	}
}

// checkResize はループ内で resizable() の描画解像度の契約を固定する
// （ReadPixels はゲームの実行中にのみ合法）: 描かれた探りはウィンドウ
// サイズの Layout に左上揃えで生き残り、キャンバスはウィンドウサイズを
// 採用し、新たに現れた帯は透明なままである。
func (g *renderGame) checkResize(screen *ebiten.Image) {
	b := g.b
	g.resized = true
	b.FillRect(100, 100, 40, 40, [4]int{255, 255, 255, 255})
	b.drainQueue()
	b.Draw(screen)
	if r, _, _, _ := screen.At(120, 120).RGBA(); r < 0x8000 {
		g.resizeFail = "resize probe paint missing"
		return
	}
	b.SetResizable(true)
	if w, h := b.Layout(800, 600); w != 800 || h != 600 {
		g.resizeFail = fmt.Sprintf("Layout = %dx%d, want 800x600 (window size)", w, h)
		return
	}
	b.drainQueue()
	b.Draw(screen)
	img := b.targets[0]
	if dx, dy := img.Bounds().Dx(), img.Bounds().Dy(); dx != 800 || dy != 600 {
		g.resizeFail = fmt.Sprintf("canvas = %dx%d, want 800x600", dx, dy)
		return
	}
	if r, _, _, _ := img.At(120, 120).RGBA(); r < 0x8000 {
		g.resizeFail = "resize lost the previous pixels"
		return
	}
	if r, _, _, _ := img.At(799, 599).RGBA(); r != 0 {
		g.resizeFail = "newly exposed strip should stay transparent"
		return
	}
	b.SetResizable(false)
	if w, h := b.Layout(1024, 768); w != 800 || h != 600 {
		g.resizeFail = fmt.Sprintf("frozen Layout = %dx%d, want 800x600", w, h)
	}
}

func (g *renderGame) Layout(outsideWidth, outsideHeight int) (int, int) {
	return 640, 480
}

// pumpUntil は done が閉じるまで描画キューを排出する（ゲームループの
// Update に代わるヘッドレスの代替）。
func pumpUntil(b *WindowBackend, done chan struct{}, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		b.drainQueue()
		select {
		case <-done:
			b.drainQueue()
			return true
		default:
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
}

// TestInputTextNoStall: InputText はキューの排出でブロックしてはならない。
// ブロックする読み出しはフレーム途中で cls/mes の再描画を止め、
// ウィジェットのデモの mes("input="+objget(id)) の行がちらついた。
func TestInputTextNoStall(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		addErr = b.AddInput(12, 120, 420, 160, 36, "にほんご")
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddInput never completed")
	}
	if addErr != nil {
		t.Fatalf("AddInput: %v", addErr)
	}
	// ここから先は排出しない: InputText がまだエンキューしていれば
	// これはタイムアウトする。
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := b.InputText(12)
		ch <- res{s, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("InputText: %v", r.err)
		}
		if r.s != "にほんご" {
			t.Fatalf("InputText = %q, want %q", r.s, "にほんご")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("InputText blocked waiting for queue drain (flicker hazard)")
	}
}

// TestInputTextJapaneseRefresh: ゲームスレッドでの編集は即座に表面化
// する（カスタムエディタの状態が真の情報源。ミラーは不要）。
func TestInputTextJapaneseRefresh(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	go func() {
		_ = b.AddInput(12, 120, 420, 160, 36, "にほんご")
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddInput never completed")
	}
	set := make(chan struct{})
	b.enqueue(func() {
		b.mu.Lock()
		e := b.widgets[12]
		e.edit.text = []rune("日本語テスト")
		e.edit.caret = len(e.edit.text)
		b.mu.Unlock()
		close(set)
	})
	if !pumpUntil(b, set, 10*time.Second) {
		t.Fatal("edit never applied")
	}
	s, err := b.InputText(12)
	if err != nil {
		t.Fatalf("InputText: %v", err)
	}
	if s != "日本語テスト" {
		t.Fatalf("InputText = %q, want %q", s, "日本語テスト")
	}
	if _, err := b.InputText(99); err == nil {
		t.Fatal("InputText on unknown id should error")
	}
}

// TestInputEditOps はゲームループなしでキャレットの編集操作を確認する。
func TestInputEditOps(t *testing.T) {
	e := &inputState{text: []rune("abc"), caret: 3}
	e.insert([]rune("で"))
	if string(e.text) != "abcで" || e.caret != 4 {
		t.Fatalf("insert = %q/%d", string(e.text), e.caret)
	}
	e.moveLeft()
	e.moveLeft()
	e.backspace()
	if string(e.text) != "acで" || e.caret != 1 {
		t.Fatalf("backspace = %q/%d", string(e.text), e.caret)
	}
	e.del()
	if string(e.text) != "aで" || e.caret != 1 {
		t.Fatalf("del = %q/%d", string(e.text), e.caret)
	}
	e.moveRight()
	e.moveRight()
	e.moveRight() // 末尾で切り詰める
	if e.caret != 2 {
		t.Fatalf("caret = %d, want 2", e.caret)
	}
	e.backspace()
	e.backspace()
	e.backspace() // 先頭で切り詰める
	if string(e.text) != "" || e.caret != 0 {
		t.Fatalf("clear = %q/%d", string(e.text), e.caret)
	}
}

// TestInputRepFire はゲームループなしで押下キーの繰返し制御を確認する。
func TestInputRepFire(t *testing.T) {
	e := &inputState{}
	if e.repFire(ebiten.KeyBackspace, false, false, 0) {
		t.Fatal("released should not fire")
	}
	if !e.repFire(ebiten.KeyBackspace, true, true, 0) {
		t.Fatal("just-pressed should fire")
	}
	if e.repFire(ebiten.KeyBackspace, false, true, 10) {
		t.Fatal("hold below delay should not fire")
	}
	if !e.repFire(ebiten.KeyBackspace, false, true, repDelay) {
		t.Fatal("delay should refire")
	}
	if !e.repFire(ebiten.KeyBackspace, false, true, repDelay+repInterval) {
		t.Fatal("interval should refire")
	}
	if e.repFire(ebiten.KeyBackspace, false, false, repDelay+repInterval+1) {
		t.Fatal("release should not fire")
	}
	if !e.repFire(ebiten.KeyBackspace, true, true, 1000) {
		t.Fatal("re-press should fire at once")
	}
}

// TestInputFocusModel はゲームループなしでクリックのフォーカス/ぼかしを
// 確認する。
func TestInputFocusModel(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	go func() {
		_ = b.AddInput(1, 0, 0, 100, 30, "a")
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddInput never completed")
	}
	b.mu.Lock()
	e := b.widgets[1]
	if e == nil || e.edit == nil {
		b.mu.Unlock()
		t.Fatal("entry missing")
	}
	e.edit.focused = true
	if got := b.focusedLocked(); got != e.edit {
		b.mu.Unlock()
		t.Fatal("focusedLocked should find the editor")
	}
	b.blurEditsLocked()
	if b.focusedLocked() != nil {
		b.mu.Unlock()
		t.Fatal("blur should clear focus")
	}
	b.mu.Unlock()
	if got, err := b.InputText(1); err != nil || got != "a" {
		t.Fatalf("InputText = %q, %v", got, err)
	}
}

// 繰返しの実行（go test -count=N）は panic せずスキップする。
// TestListEntryColorsComplete: すべてのリスト状態は背景を必要とする。
// nil の Pressed/Hover 背景は透明として描画され、マウスボタンが押され
// ている間ハイライトを隠していた（選択状態は切り替わっていたが、
// 青はリリース時にしか現れなかった）。
func TestListEntryColorsComplete(t *testing.T) {
	c := listEntryColors()
	type named struct {
		name string
		v    interface {
			RGBA() (uint32, uint32, uint32, uint32)
		}
	}
	nils := []string{}
	for _, e := range []named{
		{"Unselected", c.Unselected},
		{"Selected", c.Selected},
		{"DisabledUnselected", c.DisabledUnselected},
		{"DisabledSelected", c.DisabledSelected},
		{"SelectingBackground", c.SelectingBackground},
		{"SelectedBackground", c.SelectedBackground},
		{"FocusedBackground", c.FocusedBackground},
		{"SelectingFocusedBackground", c.SelectingFocusedBackground},
		{"SelectedFocusedBackground", c.SelectedFocusedBackground},
		{"DisabledSelectedBackground", c.DisabledSelectedBackground},
	} {
		if e.v == nil {
			nils = append(nils, e.name)
		}
	}
	if len(nils) > 0 {
		t.Fatalf("nil entry colors (transparent states): %v", nils)
	}
}

// TestListEntryTextPos は共有の項目配置を固定する: 左上寄せで、
// Golash エディタの手描きアウトラインに一致する。中央寄せにすると、
// スクロールオフセットの int() 丸め（グリフ原点 =
// int((rectH - lineH) / 2)）に加えて2回目の丸めが乗り、リストの
// スクロール中に行のベースラインが1ピクセルずれて見える
// （ちらつきとして現れる）。
func TestListEntryTextPos(t *testing.T) {
	h, v := listEntryTextPos()
	if h != widget.TextPositionStart {
		t.Fatalf("horizontal = %v, want TextPositionStart", h)
	}
	if v != widget.TextPositionStart {
		t.Fatalf("vertical = %v, want TextPositionStart", v)
	}
}

// TestWidgetFocusPressColors はフォーカス/押下のテーマの対応を固定する:
// 選択中/選択済みのリスト項目は押下時の青を使い、フォーカス中
// （未選択）の項目はフォーカスの淡青を使い、カスタムエディタの面は
// 同じ組に従う（無効がフォーカスに勝つ）。純粋な値であるため
// ヘッドレス安全（ボタン/チェックボックスの見た目は構築上これらの変数を
// 共有する。それらの面は ebiten 画像であり、ループなしではピクセル
// テストできない）。
func TestWidgetFocusPressColors(t *testing.T) {
	c := listEntryColors()
	for _, tc := range []struct {
		name string
		got  interface {
			RGBA() (uint32, uint32, uint32, uint32)
		}
		want color.NRGBA
	}{
		{"SelectingBackground", c.SelectingBackground, themePressBg},
		{"SelectedBackground", c.SelectedBackground, themePressBg},
		{"FocusedBackground", c.FocusedBackground, themeFocusBg},
		{"SelectingFocusedBackground", c.SelectingFocusedBackground, themePressBg},
		{"SelectedFocusedBackground", c.SelectedFocusedBackground, themePressBg},
	} {
		if got, ok := tc.got.(color.NRGBA); !ok || got != tc.want {
			t.Fatalf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	for _, tc := range []struct {
		name            string
		focused, dis    bool
		wantBg, wantFrm color.NRGBA
	}{
		{"idle", false, false, themeWhite, themeFrame},
		{"focused", true, false, themeFocusBg, themeFocusFrame},
		{"disabled", false, true, themeFaceDis, themeFrame},
		{"focused+disabled", true, true, themeFaceDis, themeFrame},
	} {
		if bg, fr := inputFaceColors(tc.focused, tc.dis); bg != tc.wantBg || fr != tc.wantFrm {
			t.Fatalf("inputFaceColors(%v, %v) = %v/%v, want %v/%v",
				tc.focused, tc.dis, bg, fr, tc.wantBg, tc.wantFrm)
		}
	}
}

// TestSavePNGErrors はゲームループなしで pngsave の検証を扱う
// （すべての経路はループに触れる前に戻る）。
func TestSavePNGErrors(t *testing.T) {
	b := mustNew(t)
	for _, tc := range []struct {
		path string
		id   int
	}{
		{"x.png", -1},
		{"x.png", 99},
		{"", 0},
		{"x.png", 3}, // 決して確保されない
	} {
		if err := b.SavePNG(tc.path, tc.id); err == nil {
			t.Fatalf("SavePNG(%q, %d) should error", tc.path, tc.id)
		}
	}
}

// TestBlitScaledErrors はゲームループなしで gzoom の検証を扱う
// （すべての経路はエンキュー前に戻る）。
func TestBlitScaledErrors(t *testing.T) {
	b := mustNew(t)
	for _, tc := range []struct {
		src, w, h int
		zx, zy    float64
	}{
		{99, 10, 10, 1, 1}, // 未知のバッファ
		{0, 0, 10, 1, 1},
		{0, 10, -1, 1, 1},
		{0, 10, 10, 0, 1},
		{0, 10, 10, 1, -2},
	} {
		if err := b.BlitScaled(tc.src, 0, 0, tc.w, tc.h, tc.zx, tc.zy, 0, 0); err == nil {
			t.Fatalf("BlitScaled(%+v) should error", tc)
		}
	}
}

// TestFlood は背景を通り抜けて漏れないよう四角形を塗り潰す。
func TestFlood(t *testing.T) {
	w, h := 8, 8
	px := make([]byte, 4*w*h)
	set := func(x, y int, c [4]byte) {
		i := (y*w + x) * 4
		px[i], px[i+1], px[i+2], px[i+3] = c[0], c[1], c[2], c[3]
	}
	at := func(x, y int) [4]byte {
		i := (y*w + x) * 4
		return [4]byte{px[i], px[i+1], px[i+2], px[i+3]}
	}
	white := [4]byte{0xFF, 0xFF, 0xFF, 0xFF}
	red := color.NRGBA{0xFF, 0, 0, 0xFF}
	for y := 2; y < 6; y++ {
		for x := 2; x < 6; x++ {
			set(x, y, white)
		}
	}
	if !flood(px, w, h, 3, 3, red) {
		t.Fatal("fill should report a change")
	}
	for _, p := range [][2]int{{2, 2}, {3, 3}, {5, 5}, {2, 5}} {
		if got := at(p[0], p[1]); got != [4]byte{0xFF, 0, 0, 0xFF} {
			t.Fatalf("pixel %v = %v, want red", p, got)
		}
	}
	for _, p := range [][2]int{{1, 1}, {6, 3}, {0, 7}, {3, 6}} {
		if got := at(p[0], p[1]); got != [4]byte{} {
			t.Fatalf("pixel %v = %v, want transparent", p, got)
		}
	}
	if flood(px, w, h, 3, 3, red) {
		t.Fatal("seed already in fill color should be a no-op")
	}
}

// TestFloodEnclosed は塗りを 1px の枠の内側に保つ。
func TestFloodEnclosed(t *testing.T) {
	w, h := 8, 8
	px := make([]byte, 4*w*h)
	set := func(x, y int, c [4]byte) {
		i := (y*w + x) * 4
		px[i], px[i+1], px[i+2], px[i+3] = c[0], c[1], c[2], c[3]
	}
	at := func(x, y int) [4]byte {
		i := (y*w + x) * 4
		return [4]byte{px[i], px[i+1], px[i+2], px[i+3]}
	}
	black := [4]byte{0, 0, 0, 0xFF}
	white := [4]byte{0xFF, 0xFF, 0xFF, 0xFF}
	for x := 2; x < 6; x++ {
		set(x, 2, black)
		set(x, 5, black)
		set(2, x, black)
		set(5, x, black)
	}
	set(3, 3, white)
	set(4, 4, white)
	red := color.NRGBA{0xFF, 0, 0, 0xFF}
	if !flood(px, w, h, 3, 3, red) {
		t.Fatal("fill should report a change")
	}
	if got := at(3, 3); got != [4]byte{0xFF, 0, 0, 0xFF} {
		t.Fatalf("interior = %v, want red", got)
	}
	if got := at(4, 4); got != white {
		t.Fatalf("disconnected interior = %v, want white", got)
	}
	for _, p := range [][2]int{{2, 3}, {3, 2}, {5, 4}} {
		if got := at(p[0], p[1]); got != black {
			t.Fatalf("border %v = %v, want black", p, got)
		}
	}
	if got := at(1, 1); got != [4]byte{} {
		t.Fatalf("exterior = %v, want transparent", got)
	}
}

// TestFloodFillBounds はゲームループなしで paint のシード検証を扱う。
func TestFloodFillBounds(t *testing.T) {
	b := mustNew(t)
	if err := b.FloodFill(-1, 0, [4]int{255, 0, 0, 255}); err == nil {
		t.Fatal("negative seed should error")
	}
	if err := b.FloodFill(0, 480, [4]int{255, 0, 0, 255}); err == nil {
		t.Fatal("out-of-range seed should error")
	}
	// 有効なシードはエンキューのみ（Update が適用する）。したがって
	// ループは不要。
	if err := b.FloodFill(10, 10, [4]int{255, 0, 0, 255}); err != nil {
		t.Fatalf("valid seed: %v", err)
	}
}

// TestResizableFollowsWindow はゲームループなしで resizable() のサイズの
// 契約を固定する: リサイズが禁止されていると Layout は screen() の論理
// サイズを固定し、許可されていると Layout はウィンドウサイズを報告し
// メインバッファがそれに入れ替わり、再び禁止すると現在の解像度が
// 凍結される。（入れ替えをまたいだピクセルの保持には実行中のループが
// 必要なので、TestGuiRender のリサイズ段階で扱われる。）
func TestResizableFollowsWindow(t *testing.T) {
	b := mustNew(t)
	// 固定: Layout はウィンドウサイズを無視し screen() のサイズを保つ。
	if w, h := b.Layout(320, 240); w != 640 || h != 480 {
		t.Fatalf("Layout = %dx%d, want 640x480 (screen() size)", w, h)
	}
	b.SetResizable(true)
	if w, h := b.Layout(800, 600); w != 800 || h != 600 {
		t.Fatalf("Layout = %dx%d, want 800x600 (window size)", w, h)
	}
	b.mu.Lock()
	w, h := b.w, b.h
	b.mu.Unlock()
	if w != 800 || h != 600 {
		t.Fatalf("canvas = %dx%d, want 800x600", w, h)
	}
	b.enqueue(func() {
		if dx, dy := b.targets[0].Bounds().Dx(), b.targets[0].Bounds().Dy(); dx != 800 || dy != 600 {
			t.Fatalf("main buffer = %dx%d, want 800x600", dx, dy)
		}
	})
	b.drainQueue()
	// 再び禁止: 現在の解像度が凍結される。
	b.SetResizable(false)
	if w, h := b.Layout(1024, 768); w != 800 || h != 600 {
		t.Fatalf("Layout = %dx%d, want 800x600 frozen", w, h)
	}
}

// TestTabFocusDisabled は ensureUI を固定する: Tab/Shift+Tab は
// ウィジェットのフォーカスを動かしてはならない（ebitenui の既定の
// 巡回操作）。これにより Tab はキー入力のままである（keypush 9、
// カスタムエディタのタブ挿入）。クリックのフォーカスとプログラム的な
// Focus() は影響を受けない — ゲートされるのは既定の Tab の処理のみ。
func TestTabFocusDisabled(t *testing.T) {
	b := mustNew(t)
	b.ensureUI()
	if b.ui == nil {
		t.Fatal("ensureUI built no UI")
	}
	if !b.ui.DisableDefaultFocus {
		t.Fatal("UI allows Tab focus traversal, want it disabled")
	}
}

// TestWhiteWindowDefaults はゲームループなしで明るいテーマの既定を
// 固定する: 初期の描画色は不透明な黒（白いウィンドウ）であり、
// color()（ResetColor）は白ではなく黒に戻る。CUI は影響を受けない
// （そこでは ANSI リセット）。
func TestWhiteWindowDefaults(t *testing.T) {
	b := mustNew(t)
	black := color.NRGBA{0x00, 0x00, 0x00, 0xFF}
	b.mu.Lock()
	fg := b.fg
	b.mu.Unlock()
	if fg != black {
		t.Fatalf("initial fg = %v, want %v", fg, black)
	}
	b.SetColor(255, 0, 0, 255)
	b.ResetColor()
	b.mu.Lock()
	fg = b.fg
	b.mu.Unlock()
	if fg != black {
		t.Fatalf("fg after ResetColor = %v, want %v", fg, black)
	}
}

// TestNewBuffer はゲームループなしで buffer(id, w, h) を扱う: id は
// 確保済みと記録され、画像はゲームスレッドでちょうどそのサイズに
// 着地し、不正な id やサイズはプールに触れずに失敗する。
func TestNewBuffer(t *testing.T) {
	b := mustNew(t)
	if err := b.NewBuffer(1, 320, 240); err != nil {
		t.Fatalf("NewBuffer(1, 320, 240): %v", err)
	}
	if !b.hasBuf(1) {
		t.Fatal("buffer 1 not marked allocated")
	}
	b.drainQueue()
	var size [2]int
	b.enqueue(func() {
		img, ok := b.targets[1]
		if !ok || img == nil {
			return
		}
		size = [2]int{img.Bounds().Dx(), img.Bounds().Dy()}
	})
	b.drainQueue()
	if size != [2]int{320, 240} {
		t.Fatalf("buffer 1 size = %v, want [320 240]", size)
	}
	// 置き換えは許可される（描画ループはスクラッチバッファを作り直す）:
	// 新しいサイズが勝つ。
	if err := b.NewBuffer(1, 64, 48); err != nil {
		t.Fatalf("NewBuffer(1, 64, 48): %v", err)
	}
	b.drainQueue()
	size = [2]int{}
	b.enqueue(func() {
		if img := b.targets[1]; img != nil {
			size = [2]int{img.Bounds().Dx(), img.Bounds().Dy()}
		}
	})
	b.drainQueue()
	if size != [2]int{64, 48} {
		t.Fatalf("replaced buffer size = %v, want [64 48]", size)
	}
	// エラー: 可視キャンバス、不正な id、正でないサイズ、上限を超える
	// 片辺。どれも確保してはならない。
	if err := b.NewBuffer(0, 8, 8); err == nil {
		t.Fatal("buffer(0, ...) should error (the canvas belongs to screen())")
	}
	if err := b.NewBuffer(-1, 8, 8); err == nil {
		t.Fatal("buffer(-1, ...) should error")
	}
	if err := b.NewBuffer(16, 8, 8); err == nil {
		t.Fatal("buffer(16, ...) should error")
	}
	if err := b.NewBuffer(2, 0, 8); err == nil {
		t.Fatal("buffer(2, 0, 8) should error")
	}
	if err := b.NewBuffer(2, 8, -8); err == nil {
		t.Fatal("buffer(2, 8, -8) should error")
	}
	if err := b.NewBuffer(2, MaxImageDim+1, 8); err == nil {
		t.Fatal("oversized buffer should error")
	}
	if b.hasBuf(2) {
		t.Fatal("a failed buffer() must not allocate the id")
	}
}

// TestGetPixelBounds はゲームループなしで pget の座標検証を扱う
// （ゲームスレッドの読み戻しより前に境界を確認する）。
func TestGetPixelBounds(t *testing.T) {
	b := mustNew(t)
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {640, 0}, {0, 480}, {1000000, 1000000}} {
		if _, err := b.GetPixel(p[0], p[1]); err == nil {
			t.Fatalf("GetPixel%v should error", p)
		}
	}
}

// 注意: GetPixel の往復には実行中のゲームループが必要（ebiten は
// 開始前の読み戻しを禁じる）。したがってヘッドレステストではなく
// 下記の TestGuiRender の pget 段階で扱われる。

// TestSetFontSize はゲームループなしで font() の状態を扱う
// （フェイスの再読み込みは純粋な Go。画像は関与しない）。
func TestSetFontSize(t *testing.T) {
	b := mustNew(t)
	if b.lineH != 16 || b.fontSize != 16 {
		t.Fatalf("defaults = %v/%v, want 16/16", b.lineH, b.fontSize)
	}
	if err := b.SetFontSize(32); err != nil {
		t.Fatalf("SetFontSize(32): %v", err)
	}
	if b.lineH != 32 || b.fontSize != 32 {
		t.Fatalf("scaled = %v/%v, want 32/32", b.lineH, b.fontSize)
	}
	if b.uiFace() == nil {
		t.Fatal("uiFace should follow the resized face")
	}
	// pos() はピクセル原点自体である: セルへの丸めはしない。
	b.MoveTo(10, 5)
	b.mu.Lock()
	gx, gy := b.gx, b.gy
	tx, ty := b.curX, b.curY
	b.mu.Unlock()
	if gx != 10 || gy != 5 {
		t.Fatalf("graphics cursor = (%d,%d), want (10,5)", gx, gy)
	}
	if tx != 10 || ty != 5 {
		t.Fatalf("text cursor = (%v,%v), want (10,5)", tx, ty)
	}
	for _, size := range []int{-1, 0, 7, 65, 100} {
		if err := b.SetFontSize(size); err == nil {
			t.Fatalf("SetFontSize(%d) should error", size)
		}
	}
}

// TestFontDrawSmoke は実際のキャンバス経路を通じてリサイズされた
// テキストをヘッドレスでラスタライズする（読み戻しはしない:
// ReadPixels はループ外で panic するが、単なる描画はしない）。
func TestFontDrawSmoke(t *testing.T) {
	b := mustNew(t)
	if err := b.SetFontSize(32); err != nil {
		t.Fatal(err)
	}
	d := b.printRaw("Hello, GUI! あいうえお", 0, true)
	if d == nil {
		t.Fatal("printRaw laid out nothing")
	}
	b.applyTextDraw(d)
	b.Draw(ebiten.NewImage(640, 480))
}

// TestFormWidgetsHeadless は手動の汲み上げ（ゲームループなし）で
// chkbox/combox/mesbox を駆動する: 作成時のミラーと Update 時の更新。
func TestFormWidgetsHeadless(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		defer close(done)
		if err := b.AddCheck(21, "Agree", 10, 300, 0, 0, false); err != nil {
			addErr = err
			return
		}
		if err := b.AddCombo(22, 220, 300, 200, 36, []string{"x", "y"}, 0); err != nil {
			addErr = err
			return
		}
		if err := b.AddArea(23, 430, 300, 190, 60, "l1\nl2"); err != nil {
			addErr = err
			return
		}
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("form widget creation never completed")
	}
	if addErr != nil {
		t.Fatalf("add: %v", addErr)
	}
	// 作成時のミラー。
	if p, err := b.Checked(21); err != nil || p {
		t.Fatalf("Checked(21) = %v, %v; want false", p, err)
	}
	if idx, err := b.SelectedIndex(22); err != nil || idx != 0 {
		t.Fatalf("SelectedIndex(22) = %v, %v; want 0", idx, err)
	}
	if s, err := b.AreaText(23); err != nil || s != "l1\nl2" {
		t.Fatalf("AreaText(23) = %q, %v", s, err)
	}
	// ゲームスレッドがするように変更し、それからミラーを更新する。
	set := make(chan struct{})
	b.enqueue(func() {
		b.mu.Lock()
		e21, e22, e23 := b.widgets[21], b.widgets[22], b.widgets[23]
		b.mu.Unlock()
		e21.check.SetState(widget.WidgetChecked)
		e22.combo.SetSelectedEntry("y")
		e23.area.SetText("changed")
		close(set)
	})
	if !pumpUntil(b, set, 10*time.Second) {
		t.Fatal("mutation never applied")
	}
	b.syncWidgetCache() // Update が毎ティック行うこと
	if p, _ := b.Checked(21); !p {
		t.Fatal("Checked(21) should be true after SetState")
	}
	if idx, _ := b.SelectedIndex(22); idx != 1 {
		t.Fatalf("SelectedIndex(22) = %d, want 1", idx)
	}
	if s, _ := b.AreaText(23); s != "changed" {
		t.Fatalf("AreaText(23) = %q, want changed", s)
	}
	// 正でない w/h はフォントとラベルから自動でサイズ決めされる（汲み上げ
	// られる: 成功した追加は他のすべてのウィジェットと同じく runOnLoop で
	// ブロックする）。
	auto := make(chan struct{})
	var autoErr error
	go func() {
		defer close(auto)
		autoErr = b.AddCheck(24, "x", 0, 0, 0, 0, false)
	}()
	if !pumpUntil(b, auto, 10*time.Second) {
		t.Fatal("auto-size chkbox never completed")
	}
	if autoErr != nil {
		t.Fatalf("auto-size chkbox: %v", autoErr)
	}
	// placeChild は矩形を同期的に記録する（SetLocation 自体は次の
	// 実際のループのレイアウトで実行される）。
	b.mu.Lock()
	r, ok := b.fixLayout.rects[b.widgets[24].child]
	b.mu.Unlock()
	if !ok || r.Dx() <= 0 || r.Dy() <= 0 {
		t.Fatalf("auto-size chkbox rect = %v, want positive size", r)
	}
	// 検証（すべてループに触れる前に戻る）。
	if err := b.AddCombo(24, 0, 0, 100, 30, []string{"a"}, 5); err == nil {
		t.Fatal("combox out-of-range sel should error")
	}
	if err := b.AddArea(24, 0, 0, 100, 0, ""); err == nil {
		t.Fatal("mesbox zero height should error")
	}
	if _, err := b.Checked(99); err == nil {
		t.Fatal("checked unknown should error")
	}
	if _, err := b.AreaText(21); err == nil {
		t.Fatal("AreaText on a checkbox should error")
	}
	if _, err := b.SelectedIndex(21); err == nil {
		t.Fatal("SelectedIndex on a checkbox should error")
	}
	if err := b.SetEnabled(99, true); err == nil {
		t.Fatal("SetEnabled unknown should error")
	}
	// SetEnabled はループで適用される（ここでは手動で汲み上げられる）。
	en := make(chan struct{})
	var enErr error
	go func() {
		defer close(en)
		enErr = b.SetEnabled(21, false)
	}()
	if !pumpUntil(b, en, 10*time.Second) {
		t.Fatal("SetEnabled never applied")
	}
	if enErr != nil {
		t.Fatalf("SetEnabled: %v", enErr)
	}
	b.mu.Lock()
	dis := b.widgets[21].child.GetWidget().Disabled
	b.mu.Unlock()
	if !dis {
		t.Fatal("widget 21 should be disabled")
	}
}

// TestCheckImageSized はチェックボックスのラベルの回帰を防ぐ: 単色の
// ナインスライスは MinSize 0 を報告し、ebitenui はラベル付きボックスを
// checkboxPreferredSize で描画するため、ボックスは 0x0 で描画されて
// いた（ラベル文字のみ）。
func TestCheckImageSized(t *testing.T) {
	b := mustNew(t)
	img := b.checkImage()
	faces := map[string]interface{ MinSize() (int, int) }{
		"Unchecked":         img.Unchecked,
		"UncheckedHovered":  img.UncheckedHovered,
		"UncheckedDisabled": img.UncheckedDisabled,
		"Checked":           img.Checked,
		"CheckedHovered":    img.CheckedHovered,
		"CheckedDisabled":   img.CheckedDisabled,
	}
	for name, f := range faces {
		if f == nil {
			t.Fatalf("%s face is nil", name)
		}
		if w, h := f.MinSize(); w < 12 || h < 12 {
			t.Fatalf("%s MinSize = %dx%d, want >= 12 (invisible box)", name, w, h)
		}
	}
	// 見た目の中身: 白い本体、灰色のリング、黒いグリフ。
	body := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	fr := color.NRGBA{0x80, 0x80, 0x80, 0xFF}
	mark := color.NRGBA{0x00, 0x00, 0x00, 0xFF}
	plain := checkArtRGBA(20, body, fr, false, mark)
	if got := plain.NRGBAAt(0, 0); got != fr {
		t.Fatalf("unchecked corner = %v, want frame %v", got, fr)
	}
	if got := plain.NRGBAAt(10, 10); got != body {
		t.Fatalf("unchecked center = %v, want body %v", got, body)
	}
	ticked := checkArtRGBA(20, body, fr, true, mark)
	if got := ticked.NRGBAAt(9, 14); got != mark {
		t.Fatalf("checked glyph joint = %v, want mark %v", got, mark)
	}
	// ヘッドレスの描画スモーク: ラベル付きチェックボックスの Draw は
	// panic してはならない（単なる Draw はゲームループなしで動く。
	// 読み戻しは動かない）。
	done := make(chan struct{})
	go func() {
		_ = b.AddCheck(21, "Agree", 10, 300, 200, 32, false)
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddCheck never completed")
	}
	b.Draw(ebiten.NewImage(640, 480))
}

// TestComboClickRepro はクリック時のコンボの経路をヘッドレスで駆動する:
// ドロップダウンを開き、Update、Draw、項目を選択し、再び Update。
// これはウィンドウなしで combox クリックの panic を再現する。
func TestComboClickRepro(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := b.AddCombo(22, 220, 300, 200, 36, []string{"x", "y", "z"}, 0); err != nil {
			t.Errorf("AddCombo: %v", err)
		}
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddCombo never completed")
	}
	open := make(chan struct{})
	go func() {
		defer close(open)
		b.runOnLoop(func() {
			b.widgets[22].combo.SetContentVisible(true)
		})
	}()
	if !pumpUntil(b, open, 10*time.Second) {
		t.Fatal("SetContentVisible never applied")
	}
	if err := b.Update(); err != nil {
		t.Fatalf("Update with open combo: %v", err)
	}
	b.Draw(ebiten.NewImage(640, 480))
	sel := make(chan struct{})
	go func() {
		defer close(sel)
		b.runOnLoop(func() {
			b.widgets[22].combo.SetSelectedEntry("y")
		})
	}()
	if !pumpUntil(b, sel, 10*time.Second) {
		t.Fatal("SetSelectedEntry never applied")
	}
	if err := b.Update(); err != nil {
		t.Fatalf("Update after select: %v", err)
	}
	b.Draw(ebiten.NewImage(640, 480))
	if idx, err := b.SelectedIndex(22); err != nil || idx != 1 {
		t.Fatalf("SelectedIndex(22) = %d, %v; want 1", idx, err)
	}
	// 端の経路: 空の項目と重複した項目も開く+選択で生き残らなければ
	// ならない。
	for _, tc := range []struct {
		id    int
		items []string
		sel   string
		want  int
	}{
		{31, []string{}, "", -1},
		{32, []string{"a", "a", "b"}, "a", 0},
	} {
		d := make(chan struct{})
		id, items := tc.id, tc.items
		go func() {
			defer close(d)
			_ = b.AddCombo(id, 220, 300, 200, 36, items, -1)
		}()
		if !pumpUntil(b, d, 10*time.Second) {
			t.Fatalf("AddCombo(%d) never completed", id)
		}
		o := make(chan struct{})
		go func() {
			defer close(o)
			b.runOnLoop(func() {
				b.widgets[id].combo.SetContentVisible(true)
			})
		}()
		if !pumpUntil(b, o, 10*time.Second) {
			t.Fatalf("SetContentVisible(%d) never applied", id)
		}
		if err := b.Update(); err != nil {
			t.Fatalf("Update with open combo %d: %v", id, err)
		}
		b.Draw(ebiten.NewImage(640, 480))
		if tc.sel != "" {
			s := make(chan struct{})
			go func() {
				defer close(s)
				b.runOnLoop(func() {
					b.widgets[id].combo.SetSelectedEntry(tc.sel)
				})
			}()
			if !pumpUntil(b, s, 10*time.Second) {
				t.Fatalf("SetSelectedEntry(%d) never applied", id)
			}
			if err := b.Update(); err != nil {
				t.Fatalf("Update after select %d: %v", id, err)
			}
		}
		if idx, err := b.SelectedIndex(id); err != nil || idx != tc.want {
			t.Fatalf("SelectedIndex(%d) = %d, %v; want %d", id, idx, err, tc.want)
		}
	}
}

// TestAwaitTick はゲームループなしでフレームカウンタを駆動する:
// advanceTick は Update が呼ぶものなので、それをヘッドレスで待つことは
// ループ内の挙動と一致する。
func TestAwaitTick(t *testing.T) {
	b := mustNew(t)
	if b.Tick() != 0 {
		t.Fatalf("initial tick = %d, want 0", b.Tick())
	}
	b.Await(0)
	b.Await(-5) // 何もせず、直ちに戻らなければならない
	done := make(chan struct{})
	go func() {
		b.Await(2)
		close(done)
	}()
	// 寛大な間隔: 待機側はマイクロ秒で起床を再登録する。
	for i := 0; i < 3; i++ {
		time.Sleep(20 * time.Millisecond)
		b.advanceTick()
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Await(2) never returned after 3 ticks")
	}
	if b.Tick() != 3 {
		t.Fatalf("tick = %d, want 3", b.Tick())
	}
	// 既にティックが過ぎている遅れてきた待機側は直ちに戻る。
	later := make(chan struct{})
	go func() {
		b.Await(1)
		close(later)
	}()
	time.Sleep(50 * time.Millisecond) // 待機側に登録させる
	b.advanceTick()
	select {
	case <-later:
	case <-time.After(10 * time.Second):
		t.Fatal("Await(1) never returned")
	}
}

// TestRotateTransform はゲームループなしで grotate の幾何を確認する:
// 領域の中心はどんな角度でもカーソルに着地する。
func TestRotateTransform(t *testing.T) {
	m := rotateTransform(20, 10, 1, 1, 0, 100, 50)
	if x, y := m.Apply(10, 5); x != 100 || y != 50 {
		t.Fatalf("center = (%v,%v), want (100,50)", x, y)
	}
	if x, y := m.Apply(0, 0); x != 90 || y != 45 {
		t.Fatalf("corner = (%v,%v), want (90,45)", x, y)
	}
	// 90度: 右端の中点 (20,5) が下端の中点に回転する。
	m = rotateTransform(20, 10, 1, 1, 90, 100, 50)
	if x, y := m.Apply(20, 5); diff(x, 100) || diff(y, 60) {
		t.Fatalf("rotated = (%v,%v), want (100,60)", x, y)
	}
	// 拡大が合成される: 中心は依然カーソルに着地する。
	m = rotateTransform(20, 10, 2, 3, 45, 7, 9)
	if x, y := m.Apply(10, 5); diff(x, 7) || diff(y, 9) {
		t.Fatalf("scaled center = (%v,%v), want (7,9)", x, y)
	}
}

func diff(a, b float64) bool {
	d := a - b
	return d > 1e-9 || d < -1e-9
}

// TestBlitRotateErrors はゲームループなしで grotate の検証を扱う。
func TestBlitRotateErrors(t *testing.T) {
	b := mustNew(t)
	for _, tc := range []struct {
		src, w, h int
		zx, zy    float64
	}{
		{99, 10, 10, 1, 1}, // 未知のバッファ
		{0, 0, 10, 1, 1},
		{0, 10, -1, 1, 1},
		{0, 10, 10, 0, 1},
		{0, 10, 10, 1, -2},
	} {
		if err := b.BlitRotate(tc.src, 0, 0, tc.w, tc.h, 30, tc.zx, tc.zy, 0, 0); err == nil {
			t.Fatalf("BlitRotate(%+v) should error", tc)
		}
	}
}

// TestAlphaState はゲームループなしで galpha を扱う。
func TestAlphaState(t *testing.T) {
	b := mustNew(t)
	if b.alphaScale() != 1 {
		t.Fatalf("default alpha scale = %v, want 1", b.alphaScale())
	}
	if err := b.SetAlpha(128); err != nil {
		t.Fatalf("SetAlpha(128): %v", err)
	}
	if s := b.alphaScale(); s < 0.5 || s > 0.51 {
		t.Fatalf("alpha scale = %v, want ~0.502", s)
	}
	for _, a := range []int{-1, 256, 1000} {
		if err := b.SetAlpha(a); err == nil {
			t.Fatalf("SetAlpha(%d) should error", a)
		}
	}
	if err := b.SetAlpha(255); err != nil {
		t.Fatalf("SetAlpha(255): %v", err)
	}
}

// TestMouseWheelConsume はホイールのラッチを確認する: デテントの全体が
// 一度報告され、端数は次の呼出に持ち越される。
func TestMouseWheelConsume(t *testing.T) {
	b := mustNew(t)
	if got := b.MouseWheel(); got != 0 {
		t.Fatalf("idle wheel = %d, want 0", got)
	}
	b.mu.Lock()
	b.wheelAccum = 2.7
	b.mu.Unlock()
	if got := b.MouseWheel(); got != 2 {
		t.Fatalf("wheel = %d, want 2", got)
	}
	if got := b.MouseWheel(); got != 0 {
		t.Fatalf("second read = %d, want 0 (0.7 carries)", got)
	}
	b.mu.Lock()
	b.wheelAccum += 0.5
	b.mu.Unlock()
	if got := b.MouseWheel(); got != 1 {
		t.Fatalf("carried wheel = %d, want 1", got)
	}
	b.mu.Lock()
	b.wheelAccum = -1.7
	b.mu.Unlock()
	if got := b.MouseWheel(); got != -1 {
		t.Fatalf("negative wheel = %d, want -1", got)
	}
}

// TestWidgetThemeFixed は Windows 標準のウィジェットテーマを固定する:
// すべてのウィジェット種別は色の上書き API なしにヘッドレスで構築でき、
// チェックボックスの見た目は白いボックスに灰色のリング、黒いグリフを
// 運ぶ。
// 注意: TestGuiRender より前に定義される。Ebiten は1プロセスにつき
// 1つの RunGame しか許さず、その後は NewImage が panic するため、
// 画像を生成するテストは最初に実行されなければならない
// （上の他のウィジェットテストと同じ）。
func TestWidgetThemeFixed(t *testing.T) {
	b := mustNew(t)
	white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	fr := color.NRGBA{0x80, 0x80, 0x80, 0xFF}
	mark := color.NRGBA{0x00, 0x00, 0x00, 0xFF}
	plain := checkArtRGBA(20, white, fr, false, mark)
	if got := plain.NRGBAAt(0, 0); got != fr {
		t.Fatalf("unchecked corner = %v, want frame %v", got, fr)
	}
	if got := plain.NRGBAAt(10, 10); got != white {
		t.Fatalf("unchecked center = %v, want body %v", got, white)
	}
	ticked := checkArtRGBA(20, white, fr, true, mark)
	if got := ticked.NRGBAAt(9, 14); got != mark {
		t.Fatalf("checked glyph joint = %v, want mark %v", got, mark)
	}
	if got := ticked.NRGBAAt(0, 0); got != fr {
		t.Fatalf("checked corner = %v, want frame %v", got, fr)
	}
	// すべてのウィジェット種別が固定のテーマで構築される（色の引数なし）。
	for _, add := range []func() error{
		func() error { return b.AddButton(1, "OK", 10, 10, 120, 40) },
		func() error { return b.AddInput(2, 10, 60, 160, 36, "hi") },
		func() error { return b.AddList(3, 10, 110, 160, 80, []string{"a", "b"}) },
		func() error { return b.AddCheck(4, "agree", 10, 200, 0, 0, true) },
		func() error { return b.AddCombo(5, 10, 240, 160, 36, []string{"x", "y"}, 1) },
		func() error { return b.AddArea(6, 10, 290, 200, 80, "memo") },
	} {
		if err := runWidgetOp(t, b, add); err != nil {
			t.Fatalf("add widget: %v", err)
		}
	}
	// リストの項目の色は明るいテーマでも完全（背景が非 nil）なまま。
	c := listEntryColors()
	if c.Unselected != themeInk || c.Selected != themeInk {
		t.Fatalf("entry text = %v/%v, want black", c.Unselected, c.Selected)
	}
	if c.SelectedBackground != themePressBg {
		t.Fatalf("selection bg = %v, want pressed %v", c.SelectedBackground, themePressBg)
	}
}

// TestClearKeepsWidgets: cls() はキャンバス/テキストのみを消去する。
// ウィジェットは clrobj() まで生き残る（回帰: cls はそれらを落とし、
// ウィジェットと毎フレームの cls() を組み合わせたすべての描画ループを
// 壊していた）。
func TestClearKeepsWidgets(t *testing.T) {
	b := mustNew(t)
	if err := runWidgetOp(t, b, func() error { return b.AddInput(2, 10, 60, 160, 36, "hi") }); err != nil {
		t.Fatalf("AddInput: %v", err)
	}
	b.Clear()
	if !b.HasWidget(2) {
		t.Fatal("cls() must not remove widgets")
	}
	if s, err := b.InputText(2); err != nil || s != "hi" {
		t.Fatalf("InputText after cls() = %q, %v", s, err)
	}
}

// loopRan は1プロセスにつき1ループという Ebiten の制限を守る:
// 繰返しの実行（go test -count=N）は panic せずスキップする。
var loopRan = false

// TestListboxVerticalSlider は可視のスクロールバーの経路を守る: 空の
// リストと背の高いリストはどちらもスライダーを構築した状態で Update/Draw
// を生き残らなければならない（そのページ計算は内容の高さで除算する）。
// またスライダーの画像は配線されていなければならない（画像なしの
// ハンドルは描画時に panic する。ドロップダウンと同じ）。スライダーが
// その帯を予約するというピクセルによる証明は下記の renderGame の
// スライダー段階にある。
// 注意: TestGuiRender より前 — Ebiten は1プロセスにつき1つのゲーム
// ループしか許さない。
func TestListboxVerticalSlider(t *testing.T) {
	b := mustNew(t)
	if err := runWidgetOp(t, b, func() error {
		return b.AddList(61, 0, 0, 120, 60, []string{"a", "b", "c"})
	}); err != nil {
		t.Fatalf("AddList: %v", err)
	}
	// スライダーの画像は完全に配線されていなければならない（nil の面なし）。
	sp := b.comboSliderParams()
	if sp.TrackImage == nil || sp.TrackImage.Idle == nil {
		t.Fatal("slider track image missing")
	}
	if sp.HandleImage == nil {
		t.Fatal("slider handle image missing")
	}
	// 空のリスト + 可視のスライダーは Update/Draw で爆発してはならない。
	if err := runWidgetOp(t, b, func() error {
		return b.AddList(62, 0, 0, 120, 60, nil)
	}); err != nil {
		t.Fatalf("AddList empty: %v", err)
	}
	if err := b.Update(); err != nil {
		t.Fatalf("Update with empty list: %v", err)
	}
	b.Draw(ebiten.NewImage(640, 480))
	// 多くの項目: 背の高い内容に対するスライダーの計算も生き残らなければ
	// ならない。
	many := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		many = append(many, "item number "+strings.Repeat("x", i%37))
	}
	if err := runWidgetOp(t, b, func() error {
		return b.UpdateList(62, many)
	}); err != nil {
		t.Fatalf("UpdateList many: %v", err)
	}
	if err := b.Update(); err != nil {
		t.Fatalf("Update with 200 items: %v", err)
	}
	b.Draw(ebiten.NewImage(640, 480))
}

// TestListboxAddListKeepsWidget は2つの listbox のフォーカス修正を守る:
// 同じ id/矩形/項目での繰返しの listbox() 呼出はウィジェットを再利用
// しなければならない（それによりそのスクロールオフセットと ebitenui の
// フォーカスが生き残る）。そして異なる項目での同じ矩形の呼出は
// 作り直さず（毎フレームスクロールをリセットしていた）objset の
// ように項目をその場で入れ替えなければならない。
func TestListboxAddListKeepsWidget(t *testing.T) {
	b := mustNew(t)
	items := []string{"a", "b", "c"}
	if err := runWidgetOp(t, b, func() error {
		return b.AddList(63, 0, 0, 120, 60, items)
	}); err != nil {
		t.Fatalf("AddList: %v", err)
	}
	b.mu.Lock()
	first := b.widgets[63].list
	b.mu.Unlock()
	if first == nil {
		t.Fatal("list widget missing")
	}
	if err := runWidgetOp(t, b, func() error {
		return b.AddList(63, 0, 0, 120, 60, items)
	}); err != nil {
		t.Fatalf("AddList repeat: %v", err)
	}
	b.mu.Lock()
	kept := b.widgets[63].list == first
	b.mu.Unlock()
	if !kept {
		t.Fatal("same listbox() call recreated the widget (scroll/focus lost)")
	}
	other := []string{"a", "b", "d"}
	if err := runWidgetOp(t, b, func() error {
		return b.AddList(63, 0, 0, 120, 60, other)
	}); err != nil {
		t.Fatalf("AddList changed items: %v", err)
	}
	b.mu.Lock()
	kept = b.widgets[63].list == first
	b.mu.Unlock()
	if !kept {
		t.Fatal("same-rect listbox() call rebuilt the widget (scroll/focus lost)")
	}
	// 2つ目の listbox は独自のウィジェットを保つ: それをフォーカスして
	// 最初のリストのスクロールを動かしてはならない（各 List は自身の
	// ScrollContainer を所有する）。
	if err := runWidgetOp(t, b, func() error {
		return b.AddList(64, 200, 0, 120, 60, items)
	}); err != nil {
		t.Fatalf("AddList second: %v", err)
	}
	b.mu.Lock()
	second := b.widgets[64].list
	b.mu.Unlock()
	if second == nil || second == first {
		t.Fatal("second listbox must own a distinct widget")
	}
}

// TestAreaVerticalScrollbar はコンソールのログビューアを守る: 通常、
// 空、背の高い内容はどれもスクロールバーを構築した状態で Update/Draw を
// 生き残らなければならず、繰返しの SetText（追記の経路）は動き続け
// なければならない。下端への自動の張り付きは ebitenui 内部
// （ScrollEnd）である。
// 注意: TestGuiRender より前 — Ebiten は1プロセスにつき1つのゲーム
// ループしか許さない。
func TestAreaVerticalScrollbar(t *testing.T) {
	b := mustNew(t)
	if err := runWidgetOp(t, b, func() error {
		return b.AddArea(71, 0, 0, 300, 110, "line1\nline2")
	}); err != nil {
		t.Fatalf("AddArea: %v", err)
	}
	if s, err := b.AreaText(71); err != nil || s != "line1\nline2" {
		t.Fatalf("AreaText = %q, %v", s, err)
	}
	if err := runWidgetOp(t, b, func() error {
		return b.AddArea(72, 0, 0, 300, 110, "")
	}); err != nil {
		t.Fatalf("AddArea empty: %v", err)
	}
	many := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		many = append(many, "log line number "+strings.Repeat("=", i%41))
	}
	if err := runWidgetOp(t, b, func() error {
		return b.AddArea(73, 0, 0, 300, 110, strings.Join(many, "\n"))
	}); err != nil {
		t.Fatalf("AddArea many: %v", err)
	}
	// エディタのコンソールのように頻繁に更新（追記 = 読み出し + 書き換え）。
	for i := 0; i < 5; i++ {
		if err := runWidgetOp(t, b, func() error {
			s, err := b.AreaText(73)
			if err != nil {
				return err
			}
			return b.SetAreaText(73, s+"\nmore "+strings.Repeat("x", i))
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := b.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	b.Draw(ebiten.NewImage(640, 480))
	if err := runWidgetOp(t, b, func() error {
		return b.SetAreaText(73, "")
	}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := b.Update(); err != nil {
		t.Fatalf("Update after clear: %v", err)
	}
	b.Draw(ebiten.NewImage(640, 480))
}

// TestListboxItemsFitAndStayUnique は listbox/objset の組込みが
// するようにバックエンドを駆動し、格納された項目（選択がそれを参照して
// 解決する）がリストの幅に収められ、かつ重複しないことを確認する。
// これにより、似た名前の2つの定義の2つ目をクリックしても正しいものに
// 飛べる。
// 注意: TestGuiRender より前 — Ebiten は1プロセスにつき1つのゲーム
// ループしか許さない。
func TestListboxItemsFitAndStayUnique(t *testing.T) {
	b := mustNew(t)
	w := 104
	items := []string{
		"myVeryLongFunctionName  (1)",
		"myVeryLongFunctionName  (2)",
		"myVeryLongFunctionName  (100)",
	}
	if err := runWidgetOp(t, b, func() error { return b.AddList(41, 0, 0, w, 60, items) }); err != nil {
		t.Fatalf("AddList: %v", err)
	}
	b.mu.Lock()
	got := append([]string(nil), b.widgets[41].items...)
	b.mu.Unlock()
	if len(got) != len(items) {
		t.Fatalf("stored %d items, want %d", len(got), len(items))
	}
	for i, s := range got {
		if b.textWidthLocked(s) > w-listSliderW-listPadX {
			t.Errorf("item %d %q wider than %d", i, s, w-listSliderW-listPadX)
		}
		for j := 0; j < i; j++ {
			if got[j] == s {
				t.Fatalf("item %d %q duplicates item %d: objget() would report the earlier index", i, s, j)
			}
		}
	}
	// objset は記録された幅に再び収める。
	if err := runWidgetOp(t, b, func() error { return b.UpdateList(41, items) }); err != nil {
		t.Fatalf("UpdateList: %v", err)
	}
	b.mu.Lock()
	got2 := append([]string(nil), b.widgets[41].items...)
	b.mu.Unlock()
	if len(got2) != len(items) {
		t.Fatalf("after update: stored %d items, want %d", len(got2), len(items))
	}
	for i := range got {
		if got[i] != got2[i] {
			t.Fatalf("item %d changed after objset: %q -> %q", i, got[i], got2[i])
		}
	}
}

// TestButtonLabelFits はボタンより広いラベルが ebitenui が描画する前に
// 短縮されることを確認する: ebitenui はボタンのテキストをクリップしない
// ため、生のラベルは枠からあふれてしまう。
// 注意: TestGuiRender より前 — Ebiten は1プロセスにつき1つのゲーム
// ループしか許さない。
func TestButtonLabelFits(t *testing.T) {
	b := mustNew(t)
	w := 60
	long := strings.Repeat("ラ", 20)
	short := "OK"
	if err := runWidgetOp(t, b, func() error {
		if err := b.AddButton(51, long, 0, 0, w, 24); err != nil {
			return err
		}
		return b.AddButton(52, short, 0, 0, w, 24)
	}); err != nil {
		t.Fatalf("AddButton: %v", err)
	}
	b.mu.Lock()
	gotLong, gotShort := b.widgets[51].label, b.widgets[52].label
	b.mu.Unlock()
	if gotLong == long {
		t.Fatal("long button label was not shortened")
	}
	if b.textWidthLocked(gotLong) > w-buttonPadX {
		t.Errorf("label %q measures %d, over %d", gotLong, b.textWidthLocked(gotLong), w-buttonPadX)
	}
	if gotShort != short {
		t.Fatalf("short label changed to %q", gotShort)
	}
}

// TestGuiRender は端から端のピクセル出力を検証する: 環境の読み戻し、
// 直接のテキスト、バックエンドの経路（"黒い画面"の回帰を捕らえる）。
func TestGuiRender(t *testing.T) {
	if loopRan {
		t.Skip("ebiten allows a single game loop per process")
	}
	loopRan = true
	b := mustNew(t)
	b.Println("Hello, GUI! あいうえお", 0)
	ebiten.SetWindowSize(640, 480)
	ebiten.SetWindowTitle("Golash test")
	g := &renderGame{b: b, face: b.face, savePath: filepath.Join(t.TempDir(), "snap.png")}
	// 注意: 一部のマシンではウィンドウの解体時に
	// "glfw: DestroyWindow failed" が報告される。重要なのは
	// サンプリングの結果である。
	_ = ebiten.RunGame(g)
	t.Logf("frames=%d filled=%v texted=%v backed=%v", g.frames, g.filled, g.texted, g.backed)
	if !g.filled || g.fillLit == 0 {
		t.Fatal("ReadPixels sees nothing even for Fill: environment issue")
	}
	if !g.texted || g.textLit == 0 {
		t.Fatal("direct text.Draw renders nothing")
	}
	if !g.backed {
		t.Fatal("backend stage never ran")
	}
	t.Logf("fill=%d text=%d backend=%d rect=%d circle=%d any=%d shaped=%v bbox=(%d,%d)-(%d,%d)", g.fillLit, g.textLit, g.backLit, g.rectLit, g.circleLit, g.anyLit, g.shaped, g.minX, g.minY, g.maxX, g.maxY)
	if g.backLit == 0 {
		t.Fatal("no backend pixels in the first text row: backend drew nothing")
	}
	if g.backLit == 320*40 {
		t.Fatal("first text row is all dark: black screen, want a white window")
	}
	if !g.shaped || g.rectLit == 0 {
		t.Fatal("FillRect pixel not visible")
	}
	if g.circleLit == 0 {
		t.Fatal("Circle pixel not visible")
	}
	// テキストは図形とキャンバス層を共有する: mes() のピクセルは
	// バッファに現れ、後ろの rect() がそれらを覆い、rect() の後の
	// mes() は図形の上に描かれる。
	t.Logf("text layer: alone=%d covered=%d over-shape=%d", g.textAlone, g.textCovered, g.textOn)
	if g.textAlone == 0 {
		t.Fatal("mes() pixels missing from the canvas layer")
	}
	if g.textCovered != 0 {
		t.Fatalf("rect() after mes() left %d text pixels: text is not on the canvas layer", g.textCovered)
	}
	if g.textOn == 0 {
		t.Fatal("mes() after rect() drew nothing on top of the shape")
	}
	if !g.probedInput {
		t.Fatal("input stage never ran")
	}
	if !g.pxChecked {
		t.Fatal("pget stage never ran")
	}
	if !g.sliderChecked {
		t.Fatal("list slider stage never ran")
	}
	t.Logf("list slider: select-blue maxX=%d", g.sliderBlueMax)
	if g.wFail != "" {
		t.Fatalf("stage failure before completion: %s", g.wFail)
	}
	if !g.bufChecked {
		t.Fatal("buffer stage never ran")
	}
	t.Logf("arrows=%d mouse=(%d,%d) clicked=%v", g.keyIdle, g.mouseX, g.mouseY, g.clickedVal)
	if g.keyIdle != 0 {
		t.Fatalf("idle arrows = %d, want 0", g.keyIdle)
	}
	if g.clickedVal {
		t.Fatal("idle clicked = true, want false")
	}
	// 注意: 実際のカーソルはデスクトップのどこにでもありうるため、
	// ウィンドウの外にもありうる。マウス位置は記録されるが範囲は
	// 確認されない。
	if g.wFail != "" {
		t.Fatalf("widget stage: %s", g.wFail)
	}
	t.Log("widget stage ok")
	if !g.edMeasured {
		t.Fatal("editor stage never ran")
	}
	t.Logf("editor row=%v frames=%d distinct=%d", g.edRow, g.edFrames, g.edDistinct)
	if g.edDistinct != 1 {
		t.Fatalf("input() prompt flickers: %d distinct frames", g.edDistinct)
	}
	if !g.resized {
		t.Fatal("resize stage never ran")
	}
	if g.resizeFail != "" {
		t.Fatalf("resize stage: %s", g.resizeFail)
	}
}

// runWidgetOp は runOnLoop に束縛されたウィジェット操作をヘッドレスで
// 汲み上げる。
func runWidgetOp(t *testing.T, b *WindowBackend, op func() error) error {
	t.Helper()
	ch := make(chan struct{})
	var opErr error
	go func() {
		defer close(ch)
		opErr = op()
	}()
	if !pumpUntil(b, ch, 10*time.Second) {
		t.Fatal("widget op never completed")
	}
	return opErr
}
