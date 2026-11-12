package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"gsh/gui"
)

// GUI組み込み関数の妥当性検証。ヘッドレスWindowBackend（RunGameなし）が
// GPUに触れない呼び出しを処理する。実クエリはgui画素テストで実行する。
func guiEval(t *testing.T, src string) (Value, error) {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(prog.Stmts) != 1 {
		t.Fatalf("want one statement, got %d", len(prog.Stmts))
	}
	es, ok := prog.Stmts[0].(*ExprStmt)
	if !ok {
		t.Fatalf("want ExprStmt, got %T", prog.Stmts[0])
	}
	wb, err := gui.New(320, 200)
	if err != nil {
		t.Skipf("system font unavailable: %v", err)
	}
	in := NewInterpWithBackend(wb, strings.NewReader(""))
	return newVmachine(in).evalOne(es.X, es.X.Pos())
}

func TestGuiBuiltinValidation(t *testing.T) {
	if _, err := guiEval(t, "getkey(999)"); err == nil || !strings.Contains(err.Error(), "不明なキーコード") {
		t.Fatalf("getkey(999): got %v", err)
	}
	// 拡張キーは受け付ける（ヘッドレス：何も押されていない）。
	for _, src := range []string{"getkey(8)", "getkey(9)", "getkey(16)", "getkey(17)", "getkey(18)", "getkey(33)", "getkey(46)", "getkey(112)", "getkey(123)", "getkey(186)", "getkey(222)"} {
		v, err := guiEval(t, src)
		if err != nil || v.K != KInt || v.I != 0 {
			t.Fatalf("%s: got %v, %v (want 0)", src, v, err)
		}
	}
	// 第2引数 mode: 省略・false はレベル検出、true は立上り検出（消費型）。
	// ヘッドレスでは押下がないためいずれも false。
	for _, src := range []string{"getkey(37, false)", "getkey(37, true)", "getkey(16, true)"} {
		v, err := guiEval(t, src)
		if err != nil || v.K != KInt || v.I != 0 {
			t.Fatalf("%s: got %v, %v (want 0)", src, v, err)
		}
	}
	// 立上り検出は消費される：押下なしでは何度呼んでも false。
	if v, err := guiEval(t, "getkey(37, true)"); err != nil || v.K != KInt || v.I != 0 {
		t.Fatalf("getkey(37, true) repeat: got %v, %v", v, err)
	}
	if _, err := guiEval(t, "getkey(999, true)"); err == nil || !strings.Contains(err.Error(), "不明なキーコード") {
		t.Fatalf("getkey(999, true): got %v", err)
	}
	// マウスは code 0-2 に統合（0 左、1 中、2 右）。ヘッドレスでは false。
	for _, src := range []string{"getkey(0)", "getkey(1)", "getkey(2)", "getkey(0, false)", "getkey(2, false)", "getkey(0, true)", "getkey(1, true)", "getkey(2, true)"} {
		v, err := guiEval(t, src)
		if err != nil || v.K != KInt || v.I != 0 {
			t.Fatalf("%s: got %v, %v (want 0)", src, v, err)
		}
	}
	// 3 はマウスでもキーでもないためエラー。
	if _, err := guiEval(t, "getkey(3)"); err == nil || !strings.Contains(err.Error(), "不明なキーコード") {
		t.Fatalf("getkey(3): got %v", err)
	}
	for _, src := range []string{"getkey(37, 1)", "getkey(37, 0)", "getkey(37, 2)"} {
		v, err := guiEval(t, src)
		if err != nil || v.K != KInt || v.I != 0 {
			t.Fatalf("%s: got %v, %v (want 0)", src, v, err)
		}
	}
	if _, err := guiEval(t, "getkey(37, \"x\")"); err == nil {
		t.Fatal("getkey string mode should error")
	}
	if _, err := guiEval(t, "mmplay(999)"); err == nil || !strings.Contains(err.Error(), "不明なサウンド") {
		t.Fatalf("mmplay(999): got %v", err)
	}
	if _, err := guiEval(t, "mmload(\"no-such-file.wav\")"); err == nil || !strings.Contains(err.Error(), "mmload") {
		t.Fatalf("mmload missing: got %v", err)
	}
	if _, err := guiEval(t, "mmvol(9999, 50)"); err == nil {
		t.Fatal("mmvol unknown should error")
	}
	if _, err := guiEval(t, "mmvol(1, 200)"); err == nil {
		t.Fatal("mmvol out-of-range should error")
	}
	v, err := guiEval(t, "input()")
	if err != nil || v.K != KString || v.S != "" {
		t.Fatalf("input(): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "input(\"prompt\")"); err == nil {
		t.Fatal("GUI input with prompt should error")
	}
	if _, err := guiEval(t, "keychar()"); err == nil {
		t.Fatal("keychar should be undefined")
	}
	if _, err := guiEval(t, "clicked()"); err == nil {
		t.Fatal("clicked should be undefined (use getkey(0, true))")
	}
	if _, err := guiEval(t, "clicked(2)"); err == nil {
		t.Fatal("clicked should be undefined")
	}
	if _, err := guiEval(t, "mousedown()"); err == nil {
		t.Fatal("mousedown should be undefined (use getkey(0))")
	}
	if _, err := guiEval(t, "mousedown(2)"); err == nil {
		t.Fatal("mousedown should be undefined")
	}
	v, err = guiEval(t, "cursor()")
	if err != nil || v.K != KInt {
		t.Fatalf("cursor(): got %v, %v", v, err)
	}
	if v, err := guiEval(t, "cursor(true)"); err != nil || v.K != KNull {
		t.Fatalf("cursor(true): got %v, %v", v, err)
	}
	if v, err := guiEval(t, "cursor(false)"); err != nil || v.K != KNull {
		t.Fatalf("cursor(false): got %v, %v", v, err)
	}
	// 整数フラグ（0=非表示、非0=表示）を受け付ける。文字列は受け付けない。
	if _, err := guiEval(t, "cursor(1)"); err != nil {
		t.Fatalf("cursor(1): got %v", err)
	}
	if _, err := guiEval(t, "cursor(0)"); err != nil {
		t.Fatalf("cursor(0): got %v", err)
	}
	if _, err := guiEval(t, "cursor(\"x\")"); err == nil {
		t.Fatal("cursor string should error")
	}
	v, err = guiEval(t, "mousewheel()")
	if err != nil || v.K != KInt || v.I != 0 {
		t.Fatalf("mousewheel(): got %v, %v", v, err)
	}
	v, err = guiEval(t, "fullscreen()")
	if err != nil || v.K != KInt {
		t.Fatalf("fullscreen(): got %v, %v", v, err)
	}
	// 成功系はゲームスレッド待ち（runOnLoop）でヘッドレスでは
	// ブロックするため呼ばない。文字列フラグの拒否のみ検証する。
	if _, err := guiEval(t, "fullscreen(\"x\")"); err == nil {
		t.Fatal("fullscreen string should error")
	}
	v, err = guiEval(t, "resizable()")
	if err != nil || v.K != KInt {
		t.Fatalf("resizable(): got %v, %v", v, err)
	}
	if v, err := guiEval(t, "resizable(true)"); err != nil || v.K != KNull {
		t.Fatalf("resizable(true): got %v, %v", v, err)
	}
	if v, err := guiEval(t, "resizable(false)"); err != nil || v.K != KNull {
		t.Fatalf("resizable(false): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "resizable(1)"); err != nil {
		t.Fatalf("resizable(1): got %v", err)
	}
	if _, err := guiEval(t, "resizable(\"x\")"); err == nil {
		t.Fatal("resizable string should error")
	}
	v, err = guiEval(t, "ime()")
	if err != nil || v.K != KInt {
		t.Fatalf("ime(): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "ime(\"x\")"); err == nil {
		t.Fatal("ime string should error")
	}
	if _, err := guiEval(t, "mmplay(1, \"x\")"); err == nil {
		t.Fatal("mmplay string loop should error")
	}
	v, err = guiEval(t, "closing()")
	if err != nil || v.K != KInt || v.I != 0 {
		t.Fatalf("closing(): got %v, %v", v, err)
	}
	// screen()はモニタではなくOSウィンドウを報告する：ヘッドレス実行で
	// 値が確定するようウィンドウサイズを固定する。
	ebiten.SetWindowSize(320, 200)
	v, err = guiEval(t, "screen()")
	if err != nil || v.K != KArray || len(v.Arr.Elems) != 2 {
		t.Fatalf("screen(): got %v, %v", v, err)
	}
	if v.Arr.Elems[0].I != 320 || v.Arr.Elems[1].I != 200 {
		t.Fatalf("screen() = %v, want [320 200]", v.Arr.Elems)
	}
	// screen(id) は描画バッファのサイズ。0 はメインキャンバス。
	v, err = guiEval(t, "screen(0)")
	if err != nil || v.K != KArray || len(v.Arr.Elems) != 2 {
		t.Fatalf("screen(0): got %v, %v", v, err)
	}
	if v.Arr.Elems[0].I != 320 || v.Arr.Elems[1].I != 200 {
		t.Fatalf("screen(0) = %v, want [320 200]", v.Arr.Elems)
	}
	if _, err := guiEval(t, "screen(1)"); err == nil || !strings.Contains(err.Error(), "不明なバッファ") {
		t.Fatalf("screen(1) unallocated: got %v", err)
	}
	for _, src := range []string{"screen(-1)", "screen(16)"} {
		if _, err := guiEval(t, src); err == nil {
			t.Fatalf("%s out-of-range should error", src)
		}
	}
	if _, err := guiEval(t, `screen("x")`); err == nil {
		t.Fatal("screen string id should error")
	}
	if _, err := guiEval(t, "screen(0, 1, 2, 3)"); err == nil {
		t.Fatal("screen with 4 args should error")
	}
	// screen(id, w, h) で作ったバッファは自身のサイズを返す。
	v, err = guiEvalSeq(t, "screen(1, 64, 48)\nscreen(1)")
	if err != nil || v.K != KArray || len(v.Arr.Elems) != 2 {
		t.Fatalf("screen(1) after screen(1, 64, 48): got %v, %v", v, err)
	}
	if v.Arr.Elems[0].I != 64 || v.Arr.Elems[1].I != 48 {
		t.Fatalf("screen(1) = %v, want [64 48]", v.Arr.Elems)
	}
	// id 0 の3引数は screen(w, h) と同じくメインキャンバスをリサイズする。
	v, err = guiEvalSeq(t, "screen(0, 80, 60)\nscreen(0)")
	if err != nil || v.K != KArray || len(v.Arr.Elems) != 2 {
		t.Fatalf("screen(0) after screen(0, 80, 60): got %v, %v", v, err)
	}
	if v.Arr.Elems[0].I != 80 || v.Arr.Elems[1].I != 60 {
		t.Fatalf("screen(0) = %v, want [80 60]", v.Arr.Elems)
	}
	// gsel() だけで確保した id は遅延生成時のメインキャンバスサイズ。
	v, err = guiEvalSeq(t, "gsel(2)\nscreen(2)")
	if err != nil || v.K != KArray || len(v.Arr.Elems) != 2 {
		t.Fatalf("screen(2) after gsel: got %v, %v", v, err)
	}
	if v.Arr.Elems[0].I != 320 || v.Arr.Elems[1].I != 200 {
		t.Fatalf("screen(2) = %v, want [320 200]", v.Arr.Elems)
	}
	v, err = guiEval(t, "dropfiles()")
	if err != nil || v.K != KArray || len(v.Arr.Elems) != 0 {
		t.Fatalf("dropfiles(): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "dropload(\"nope.png\")"); err == nil {
		t.Fatal("dropload without drops should error")
	}
	v, err = guiEval(t, "padcount()")
	if err != nil || v.K != KInt || v.I != 0 {
		t.Fatalf("padcount(): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "padbtn(0, 0)"); err == nil {
		t.Fatal("padbtn without pads should error")
	}
	if _, err := guiEval(t, "padbtn(0, 99)"); err == nil {
		t.Fatal("padbtn out-of-range button should error")
	}
	if _, err := guiEval(t, "padaxis(0, 9)"); err == nil {
		t.Fatal("padaxis out-of-range axis should error")
	}
	if _, err := guiEval(t, "padname(0)"); err == nil {
		t.Fatal("padname without pads should error")
	}
	v, err = guiEval(t, "mmstop()")
	if err != nil || v.K != KNull {
		t.Fatalf("mmstop(): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "stick()"); err == nil {
		t.Fatal("stick should be undefined")
	}
}

// mouse()/touch() の統合 API。ヘッドレスでも型と空配列が確定する。
func TestGuiMouseTouch(t *testing.T) {
	// mouse() はカーソル位置を [x, y] で返す（ヘッドレスでは 0, 0）。
	v, err := guiEval(t, "mouse()")
	if err != nil || v.K != KArray || len(v.Arr.Elems) != 2 {
		t.Fatalf("mouse(): got %v, %v", v, err)
	}
	for _, e := range v.Arr.Elems {
		if e.K != KInt {
			t.Fatalf("mouse() element: got %v, want int", e)
		}
	}
	// touch() は接触中の座標を [[x, y], ...] で返す（ヘッドレスでは空）。
	v, err = guiEval(t, "touch()")
	if err != nil || v.K != KArray || len(v.Arr.Elems) != 0 {
		t.Fatalf("touch(): got %v, %v", v, err)
	}
	// 旧名は統合により未定義。
	for _, src := range []string{"mousex()", "mousey()", "touchcount()", "touchx(0)", "touchy(0)"} {
		if _, err := guiEval(t, src); err == nil {
			t.Fatalf("%s should be undefined", src)
		}
	}
}

// 巨大画像はループgoroutine内のエンジン内部でパニックになるため
// （捕捉不可）、Ebitenに届く前に組み込み関数側でスクリプトエラーとして
// 拒否しなければならない。ヘッドレス安全：GPU/ウィンドウ呼び出しの
// 前に拒否される。
func TestGuiImageLimits(t *testing.T) {
	if _, err := guiEval(t, "screen(4097, 1)"); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("screen width over limit: got %v", err)
	}
	if _, err := guiEval(t, "screen(100000, 100000)"); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("screen huge: got %v", err)
	}
	// screen(id, w, h) は screen(w, h) と同じ上限・正サイズ規則を共有する。
	if _, err := guiEval(t, "screen(1, 4097, 1)"); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("screen buffer width over limit: got %v", err)
	}
	if _, err := guiEval(t, "screen(1, 0, 8)"); err == nil || !strings.Contains(err.Error(), "正の値") {
		t.Fatalf("screen buffer zero size: got %v", err)
	}
	if _, err := guiEval(t, "screen(0, 4097, 1)"); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("screen(0, ...) width over limit: got %v", err)
	}
	// id 0 はメインキャンバスのリサイズ経路になる。
	if _, err := guiEval(t, "screen(0, 0, 8)"); err == nil || !strings.Contains(err.Error(), "正の値") {
		t.Fatalf("screen(0, 0, 8): got %v", err)
	}
	if _, err := guiEval(t, "screen(16, 8, 8)"); err == nil {
		t.Fatal("screen(16, ...) should error")
	}
	if _, err := guiEval(t, "screen(0, 1, 2, 3)"); err == nil {
		t.Fatal("screen with 4 args should error")
	}
	if _, err := guiEval(t, "screen(\"x\", 8, 8)"); err == nil {
		t.Fatal("screen string id should error")
	}
	if _, err := guiEval(t, `title("a\0b")`); err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("title NUL: got %v", err)
	}
	// 4097x1のPNGは復号できるが、転送前に失敗しなければならず（その際に
	// バッファIDを漏らさないこと）。
	dir := t.TempDir()
	p := filepath.Join(dir, "wide.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 4097, 1))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf("picload(%q)", filepath.ToSlash(p))
	if _, err := guiEval(t, src); err == nil || !strings.Contains(err.Error(), "大きすぎます") {
		t.Fatalf("picload oversize: got %v", err)
	}
}

// TestGuiScreenOverloads は screen の引数個数による分岐を検証する:
// 0個=OSウィンドウ、1個=バッファサイズ、2個=リサイズ、3個=バッファ作成。
func TestGuiScreenOverloads(t *testing.T) {
	ebiten.SetWindowSize(320, 200)
	defer ebiten.SetWindowSize(320, 200)
	if v, err := guiEval(t, "screen()"); err != nil || v.K != KArray || len(v.Arr.Elems) != 2 ||
		v.Arr.Elems[0].I != 320 || v.Arr.Elems[1].I != 200 {
		t.Fatalf("screen(): got %v, %v", v, err)
	}
	if v, err := guiEval(t, "screen(0)"); err != nil || v.K != KArray || len(v.Arr.Elems) != 2 ||
		v.Arr.Elems[0].I != 320 || v.Arr.Elems[1].I != 200 {
		t.Fatalf("screen(0): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "screen(1)"); err == nil || !strings.Contains(err.Error(), "不明なバッファ") {
		t.Fatalf("screen(1) unallocated: got %v", err)
	}
	if v, err := guiEvalSeq(t, "screen(1, 64, 48)\nscreen(1)"); err != nil || v.K != KArray ||
		len(v.Arr.Elems) != 2 || v.Arr.Elems[0].I != 64 || v.Arr.Elems[1].I != 48 {
		t.Fatalf("screen(1, 64, 48) then screen(1): got %v, %v", v, err)
	}
	if v, err := guiEvalSeq(t, "screen(0, 80, 60)\nscreen(0)"); err != nil || v.K != KArray ||
		len(v.Arr.Elems) != 2 || v.Arr.Elems[0].I != 80 || v.Arr.Elems[1].I != 60 {
		t.Fatalf("screen(0, 80, 60) then screen(0): got %v, %v", v, err)
	}
	// 旧名は統合により未定義。
	if _, err := guiEval(t, "screensize()"); err == nil {
		t.Fatal("screensize should be undefined")
	}
	if _, err := guiEval(t, "buffer(1, 8, 8)"); err == nil {
		t.Fatal("buffer should be undefined")
	}
}

func TestGuiWidgetValidation(t *testing.T) {
	if _, err := guiEval(t, "dialog(\"x\", 99)"); err == nil {
		t.Fatal("dialog bogus mode should error")
	}
	if _, err := guiEval(t, "dialog(\"x\", \"ok\")"); err == nil {
		t.Fatal("dialog string mode should error")
	}
	if _, err := guiEval(t, "objget(99)"); err == nil {
		t.Fatal("objget unknown should error")
	}
	if _, err := guiEval(t, "objset(99, 1)"); err == nil {
		t.Fatal("objset unknown should error")
	}
	if _, err := guiEval(t, "clrobj(99)"); err == nil {
		t.Fatal("clrobj unknown should error")
	}
	if _, err := guiEval(t, "button(1, \"x\", 0, 10)"); err == nil {
		t.Fatal("button zero width should error")
	}
	// 引数順：(id, label, w, h)。位置は pos() で指定する。
	// 文字列idはブロック前に引数0として失敗しなければならない。
	if _, err := guiEval(t, "button(\"x\", \"y\", 10, 10)"); err == nil {
		t.Fatal("button string id should error")
	}
	if _, err := guiEval(t, "button(1, \"x\")"); err == nil {
		t.Fatal("button arity should error")
	}
	if _, err := guiEval(t, "button(1, \"x\", 0, 0, 100, 30)"); err == nil {
		t.Fatal("old 6-arg button should error")
	}
	// 画像は (buf, sx, sy) の組で指定する。余りはエラー。
	if _, err := guiEval(t, "button(1, \"x\", 10, 10, 1)"); err == nil {
		t.Fatal("button single image arg should error")
	}
	if _, err := guiEval(t, "button(1, \"x\", 10, 10, 1, 0)"); err == nil {
		t.Fatal("button two image args should error")
	}
	if _, err := guiEval(t, "button(1, \"x\", 10, 10, 99, 0, 0)"); err == nil {
		t.Fatal("button unknown buffer should error")
	}
	if _, err := guiEval(t, "button(1, \"x\", 10, 10, 0, 1, 0)"); err == nil {
		t.Fatal("button empty slot with coords should error")
	}
	// 成功系はguiEvalが汲み上げないゲームスレッド作業をキューに入れる
	//（ブロックしてしまう）。バックエンド側の収まりは
	// TestListboxItemsFitAndStayUnique / TestButtonLabelFitsで扱う。
	if _, err := guiEval(t, "objset(1)"); err == nil {
		t.Fatal("objset arity should error")
	}
	if _, err := guiEval(t, "objget()"); err == nil {
		t.Fatal("objget arity should error")
	}
	if _, err := guiEval(t, "objenable(1, \"x\")"); err == nil {
		t.Fatal("objenable string flag should error")
	}
	// objvisible は削除済み: 未定義でなければならない。
	if _, err := guiEval(t, "objvisible(1, 1)"); err == nil {
		t.Fatal("objvisible should be undefined (removed)")
	}
}

func TestGuiFormValidation(t *testing.T) {
	// 位置は pos() で指定する（x/y 引数は廃止）。注意：guiEvalは
	// 描画キューを汲み上げないため、ここではループ前のエラーのみ検証する。
	// 成功系はヘッドレスのTestFormWidgetsHeadlessで扱う。
	if _, err := guiEval(t, "chkbox(1, \"x\", 0, 0)"); err == nil {
		t.Fatal("old 4-arg chkbox should error")
	}
	if _, err := guiEval(t, "chkbox(\"x\", \"y\")"); err == nil {
		t.Fatal("chkbox string id should error")
	}
	if _, err := guiEval(t, "chkbox(1, 0)"); err == nil {
		t.Fatal("chkbox non-string label should error")
	}
	if _, err := guiEval(t, "chkbox(1)"); err == nil {
		t.Fatal("chkbox arity should error")
	}
	if _, err := guiEval(t, "chkbox(1, \"x\", 1)"); err != nil {
		t.Fatalf("chkbox int flag should succeed: got %v", err)
	}
	if _, err := guiEval(t, "chkbox(1, \"x\", 0)"); err != nil {
		t.Fatalf("chkbox int flag should succeed: got %v", err)
	}
	if _, err := guiEval(t, "chkbox(1, \"x\", \"true\")"); err == nil {
		t.Fatal("chkbox string flag should error")
	}
	if _, err := guiEvalSeq(t, "chkbox(1, \"x\")\nobjset(1, 1)\n"); err != nil {
		t.Fatalf("objset chkbox int should succeed: got %v", err)
	}
	if _, err := guiEvalSeq(t, "chkbox(1, \"x\")\nobjset(1, 1.0)\n"); err == nil {
		t.Fatal("objset chkbox float should error")
	}
	if _, err := guiEvalSeq(t, "chkbox(1, \"x\")\nobjset(1, true)\n"); err != nil {
		t.Fatalf("objset chkbox int should succeed: got %v", err)
	}
	if _, err := guiEval(t, "chkbox(1, \"x\", true)"); err != nil {
		t.Fatalf("chkbox true flag should succeed: got %v", err)
	}
	if _, err := guiEval(t, "chkbox(1, \"x\", false)"); err != nil {
		t.Fatalf("chkbox false flag should succeed: got %v", err)
	}
	if _, err := guiEval(t, "inputbox(1, 2)"); err == nil {
		t.Fatal("inputbox arity should error")
	}
	if _, err := guiEval(t, "inputbox(0, 0, 0, 64, 24)"); err == nil {
		t.Fatal("old 5-arg inputbox should error")
	}
	if _, err := guiEval(t, "listbox(1, 100, 30, [1])"); err == nil {
		t.Fatal("listbox non-string item should error")
	}
	if _, err := guiEval(t, "listbox(1, 0, 0, 100, 30, [\"a\"])"); err == nil {
		t.Fatal("old 6-arg listbox should error")
	}
	if _, err := guiEval(t, "objget(99)"); err == nil {
		t.Fatal("objget unknown should error")
	}
	if _, err := guiEval(t, "combox(1, 100, 30, [1])"); err == nil {
		t.Fatal("combox non-string item should error")
	}
	if _, err := guiEval(t, "combox(1, 100, 30, [\"a\"], 5)"); err == nil {
		t.Fatal("combox out-of-range sel should error")
	}
	if _, err := guiEval(t, "combox(1, 0, 0, 100, 30, [\"a\"])"); err == nil {
		t.Fatal("old 6-arg combox should error")
	}
	if _, err := guiEval(t, "mesbox(1, 100, 0)"); err == nil {
		t.Fatal("mesbox zero height should error")
	}
	if _, err := guiEval(t, "mesbox(1, 0, 0, 100, \"x\")"); err == nil {
		t.Fatal("old 5-arg mesbox should error")
	}
	if _, err := guiEval(t, "objenable(99, true)"); err == nil {
		t.Fatal("objenable unknown should error")
	}
	// objvisible は削除済み: 未定義でなければならない。
	if _, err := guiEval(t, "objvisible(99, 1)"); err == nil {
		t.Fatal("objvisible should be undefined (removed)")
	}
}

func TestGuiDrawValidation(t *testing.T) {
	if _, err := guiEval(t, "font(7)"); err == nil {
		t.Fatal("font tiny size should error")
	}
	if _, err := guiEval(t, "font(65)"); err == nil {
		t.Fatal("font huge size should error")
	}
	if _, err := guiEval(t, "font(\"x\")"); err == nil {
		t.Fatal("font string size should error")
	}
	if v, err := guiEval(t, "font(24)"); err != nil || v.K != KNull {
		t.Fatalf("font(24): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "paint()"); err == nil {
		t.Fatal("paint should be undefined (removed)")
	}
	// gquad: src+頂点4配列。各頂点は [x, y, u, v]。透視補正付き2三角形で描く。
	// 内部バッファに貯めてまとめて描画する。空は無描画、縮退はアフィンにフォールバック。
	if _, err := guiEvalSeq(t, "screen(1, 20, 20)\ngquad(1, [0,0,0,0], [10,0,10,0], [10,10,10,10], [0,10,0,10])\n"); err != nil {
		t.Fatalf("gquad buffered: got %v", err)
	}
	// 空（変換先が直線）は沈黙の無描画（エラーにしない）。
	if _, err := guiEvalSeq(t, "screen(1, 20, 20)\ngquad(1, [0,0,0,0], [1,0,1,0], [2,0,2,0], [3,0,3,0])\n"); err != nil {
		t.Fatalf("gquad degenerate should be no-op: got %v", err)
	}
	// 縮退（参照元が直線で特異）はアフィンにフォールバックして描く（エラーにしない）。
	if _, err := guiEvalSeq(t, "screen(1, 20, 20)\ngquad(1, [0,0,0,0], [10,0,1,0], [10,10,2,0], [0,10,3,0])\n"); err != nil {
		t.Fatalf("gquad singular should fallback: got %v", err)
	}
	if _, err := guiEval(t, "gquad(1, [0,0,0,0])"); err == nil {
		t.Fatal("gquad short args should error")
	}
	if _, err := guiEvalSeq(t, "screen(1, 20, 20)\ngquad(1, [0,0,0], [10,0,10,0], [10,10,10,10], [0,10,0,10])\n"); err == nil {
		t.Fatal("gquad short vertex should error")
	}
	if _, err := guiEval(t, "gquad(99, [0,0,0,0], [10,0,10,0], [10,10,10,10], [0,10,0,10])"); err == nil {
		t.Fatal("gquad unknown buffer should error")
	}
	if _, err := guiEvalSeq(t, "screen(1, 20, 20)\ngquad(1, [0,0,0,0], [10,0,10,0], [10,10,10,10], [0,10,0,\"x\"])\n"); err == nil {
		t.Fatal("gquad string elem should error")
	}
	// tri: 6引数、現在の色で塗りつぶし。内部バッファでまとめて描く。
	if v, err := guiEval(t, "tri(0,0, 10,0, 0,10)"); err != nil || v.K != KNull {
		t.Fatalf("tri(): got %v, %v", v, err)
	}
	// 縮退は沈黙の無描画。
	if v, err := guiEval(t, "tri(0,0, 1,0, 2,0)"); err != nil || v.K != KNull {
		t.Fatalf("tri degenerate should be no-op: got %v, %v", v, err)
	}
	if _, err := guiEval(t, "tri(0,0, 10,0)"); err == nil {
		t.Fatal("tri short args should error")
	}
	if _, err := guiEval(t, "tri(0,0, 10,0, 0,\"x\")"); err == nil {
		t.Fatal("tri string arg should error")
	}
	if _, err := guiEval(t, "gcopy(99, 0, 0, 10, 10, 0, 0)"); err == nil {
		t.Fatal("gcopy unknown buffer should error")
	}
	if _, err := guiEval(t, "gcopy(99, 0, 0, 10, 10, 0, 0, 20, 20)"); err == nil {
		t.Fatal("gcopy scaled unknown buffer should error")
	}
	if _, err := guiEval(t, "gcopy(0, 0, 0, 10, 10, 0, 0, 0, 20)"); err == nil {
		t.Fatal("gcopy zero dest size should error")
	}
	if _, err := guiEval(t, "gcopy(0, 0, 0, 10, 10, 0, 0, 20, 0)"); err == nil {
		t.Fatal("gcopy zero dest height should error")
	}
	if _, err := guiEvalSeq(t, "screen(1, 20, 20)\ngcopy(1, 0, 0, 0, 10, 0, 0)\n"); err == nil {
		t.Fatal("gcopy zero src size should error")
	}
	if _, err := guiEvalSeq(t, "screen(1, 20, 20)\ngcopy(1, 10, 0, -10, 10, 50, 50)\n"); err != nil {
		t.Fatalf("gcopy negative src should mirror: got %v", err)
	}
	if _, err := guiEvalSeq(t, "screen(1, 20, 20)\ngcopy(1, 0, 0, 10, 10, 50, 50, -20, 20)\n"); err != nil {
		t.Fatalf("gcopy negative dest should mirror: got %v", err)
	}
	if _, err := guiEvalSeq(t, "screen(1, 20, 20)\ngcopy(1, 0, 0, 10, 10, 50, 50, 20, -20)\n"); err != nil {
		t.Fatalf("gcopy negative dest height should mirror: got %v", err)
	}
	if _, err := guiEval(t, "gcopy(0, \"x\", 0, 10, 10, 0, 0)"); err == nil {
		t.Fatal("gcopy string arg should error")
	}
	if _, err := guiEval(t, "gcopy(0, 0, 0, 10, 10, 0, 0, 20, \"x\")"); err == nil {
		t.Fatal("gcopy string dest size should error")
	}
	if _, err := guiEval(t, "gcopy(0, 0, 0, 10, 10, 0, 0, 20)"); err == nil {
		t.Fatal("gcopy 8 args should error")
	}
	if _, err := guiEval(t, "gcopy(0, 0, 0, 10, 10, 0)"); err == nil {
		t.Fatal("gcopy 6 args should error")
	}
	if _, err := guiEval(t, "gcopy(0, 0, 0, 10, 10)"); err == nil {
		t.Fatal("old 5-arg gcopy should error")
	}
	if _, err := guiEval(t, "gzoom(0, 0, 0, 10, 10, 1, 1)"); err == nil {
		t.Fatal("gzoom should be undefined")
	}
	if _, err := guiEval(t, "grotate(0, 0, 0, 10, 10, 30)"); err == nil {
		t.Fatal("grotate should be undefined")
	}
	if _, err := guiEval(t, "galpha(300)"); err == nil {
		t.Fatal("galpha out-of-range should error")
	}
	if _, err := guiEval(t, "galpha(\"x\")"); err == nil {
		t.Fatal("galpha string should error")
	}
	if v, err := guiEval(t, "galpha(128)"); err != nil || v.K != KNull {
		t.Fatalf("galpha(128): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "pngsave(\"\", 0)"); err == nil {
		t.Fatal("pngsave empty path should error")
	}
	if _, err := guiEval(t, "pngsave(\"x.png\", 99)"); err == nil {
		t.Fatal("pngsave bad id should error")
	}
	if _, err := guiEval(t, "pngsave(1, 0)"); err == nil {
		t.Fatal("pngsave numeric path should error")
	}
}

func TestGuiPgetValidation(t *testing.T) {
	// CUIバックエンドには読み取るキャンバスがない。
	mustErrIO(t, "pget(0, 0)\n", "pget は GUI ビルドが必要です")
	// ヘッドレスウィンドウバックエンド（320x200）：検証は
	// ゲームスレッド読み戻しより先に実行される。座標は引数で指定する。
	if _, err := guiEval(t, "pget(1)"); err == nil {
		t.Fatal("pget arity should error")
	}
	if _, err := guiEval(t, "pget()"); err == nil {
		t.Fatal("old 0-arg pget should error")
	}
	if _, err := guiEval(t, "pget(-1, 0)"); err == nil {
		t.Fatal("pget negative x should error")
	}
	if _, err := guiEval(t, "pget(0, -1)"); err == nil {
		t.Fatal("pget negative y should error")
	}
	if _, err := guiEval(t, "pget(320, 0)"); err == nil {
		t.Fatal("pget x == width should error")
	}
	if _, err := guiEval(t, "pget(0, 200)"); err == nil {
		t.Fatal("pget y == height should error")
	}
}

// guiEvalSeq は複文スクリプトを1つのヘッドレスバックエンド上で
// 順に評価する。成功系の描画はキューに入るだけでブロックしないが、pget() の成功系は
// ゲームスレッドの読み戻しを待つためヘッドレスでは扱わない。
func guiEvalSeq(t *testing.T, src string) (Value, error) {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	wb, err := gui.New(320, 200)
	if err != nil {
		t.Skipf("system font unavailable: %v", err)
	}
	in := NewInterpWithBackend(wb, strings.NewReader(""))
	vm := newVmachine(in)
	v := Null()
	for _, st := range prog.Stmts {
		es, ok := st.(*ExprStmt)
		if !ok {
			t.Fatalf("want ExprStmt, got %T", st)
		}
		v, err = vm.evalOne(es.X, es.X.Pos())
		if err != nil {
			return Null(), err
		}
	}
	return v, nil
}

func TestGuiPosDrawValidation(t *testing.T) {
	// pset(x, y): 起点座標を指定する。
	if v, err := guiEval(t, "pset(10, 20)"); err != nil || v.K != KNull {
		t.Fatalf("pset(10, 20): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "pset()"); err == nil {
		t.Fatal("old 0-arg pset should error")
	}
	if _, err := guiEval(t, "pset(1)"); err == nil {
		t.Fatal("pset arity should error")
	}
	if _, err := guiEval(t, "pset(1, 2, 3)"); err == nil {
		t.Fatal("pset too many args should error")
	}
	// line(x1, y1, x2, y2): 起点から終点へ引く。
	if _, err := guiEval(t, "line(10, 20, 30, 40)"); err != nil {
		t.Fatalf("line: got %v", err)
	}
	if _, err := guiEval(t, "line(1, 2, 3)"); err == nil {
		t.Fatal("line arity should error")
	}
	if _, err := guiEval(t, "line(30, 40)"); err == nil {
		t.Fatal("old 2-arg line should error")
	}
	if _, err := guiEval(t, "line(\"x\", 0, 0, 0)"); err == nil {
		t.Fatal("line string x should error")
	}
	// rect(x, y, w, h): (x, y) を左上として w x h を塗る。rect() は全画面。
	if v, err := guiEval(t, "rect()"); err != nil || v.K != KNull {
		t.Fatalf("rect(): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "rect(0, 0, 10, 20)"); err != nil {
		t.Fatalf("rect: got %v", err)
	}
	if _, err := guiEval(t, "rect(10, 10, -10, -20)"); err != nil {
		t.Fatalf("rect negative should normalize: got %v", err)
	}
	if _, err := guiEval(t, "rect(1)"); err == nil {
		t.Fatal("rect arity should error")
	}
	if _, err := guiEval(t, "rect(10, 20)"); err == nil {
		t.Fatal("old 2-arg rect should error")
	}
	if _, err := guiEval(t, "rect(1, 2, 3)"); err == nil {
		t.Fatal("rect too many args should error")
	}
	if _, err := guiEval(t, "rect(\"x\", 0, 1, 1)"); err == nil {
		t.Fatal("rect string x should error")
	}
	if _, err := guiEval(t, "boxf(10, 20)"); err == nil {
		t.Fatal("boxf should be undefined")
	}
	// ellipse(x, y, w, h [, fill]): (x, y) を中心に全幅 w・全高 h の楕円を描く。
	if _, err := guiEval(t, "ellipse(50, 50, 20, 20)"); err != nil {
		t.Fatalf("ellipse: got %v", err)
	}
	if _, err := guiEval(t, "ellipse(50, 50, 20, 10, true)"); err != nil {
		t.Fatalf("ellipse fill: got %v", err)
	}
	if _, err := guiEval(t, "ellipse(50, 50, 20, 10, false)"); err != nil {
		t.Fatalf("ellipse nofill: got %v", err)
	}
	if _, err := guiEval(t, "ellipse(50, 50, 20, 10, 1)"); err != nil {
		t.Fatalf("ellipse int fill should succeed: got %v", err)
	}
	if _, err := guiEval(t, "ellipse(10, 10)"); err == nil {
		t.Fatal("old 2-arg ellipse should error")
	}
	if _, err := guiEval(t, "ellipse(10)"); err == nil {
		t.Fatal("ellipse arity should error")
	}
	if _, err := guiEval(t, "ellipse(50, 50, 10, 10, 1, 1)"); err == nil {
		t.Fatal("ellipse too many args should error")
	}
	if _, err := guiEval(t, "ellipse(50, 50, -20, 10)"); err != nil {
		t.Fatalf("ellipse negative width should mirror: got %v", err)
	}
	if _, err := guiEval(t, "ellipse(50, 50, 20, -10, true)"); err != nil {
		t.Fatalf("ellipse negative height should mirror: got %v", err)
	}
	if v, err := guiEval(t, "ellipse(50, 50, 0, 10)"); err != nil || v.K != KNull {
		t.Fatalf("ellipse zero size should be no-op: got %v, %v", v, err)
	}
	if _, err := guiEval(t, "ellipse(\"x\", 0, 10, 10)"); err == nil {
		t.Fatal("ellipse string x should error")
	}
	if _, err := guiEval(t, "circle(10, 10)"); err == nil {
		t.Fatal("circle should be undefined")
	}
}
