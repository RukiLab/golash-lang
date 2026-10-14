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
		if err != nil || v.K != KBool || v.B {
			t.Fatalf("%s: got %v, %v (want false)", src, v, err)
		}
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
	v, err = guiEval(t, "clicked()")
	if err != nil || v.K != KBool || v.B {
		t.Fatalf("clicked(): got %v, %v", v, err)
	}
	v, err = guiEval(t, "clicked(2)")
	if err != nil || v.K != KBool || v.B {
		t.Fatalf("clicked(2): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "clicked(9)"); err == nil {
		t.Fatal("clicked bad button should error")
	}
	v, err = guiEval(t, "cursor()")
	if err != nil || v.K != KBool {
		t.Fatalf("cursor(): got %v, %v", v, err)
	}
	if v, err := guiEval(t, "cursor(1)"); err != nil || v.K != KNull {
		t.Fatalf("cursor(1): got %v, %v", v, err)
	}
	v, err = guiEval(t, "mousewheel()")
	if err != nil || v.K != KInt || v.I != 0 {
		t.Fatalf("mousewheel(): got %v, %v", v, err)
	}
	v, err = guiEval(t, "fullscreen()")
	if err != nil || v.K != KBool {
		t.Fatalf("fullscreen(): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "fullscreen(\"x\")"); err == nil {
		t.Fatal("fullscreen string should error")
	}
	v, err = guiEval(t, "closing()")
	if err != nil || v.K != KBool || v.B {
		t.Fatalf("closing(): got %v, %v", v, err)
	}
	// screensize()はモニタではなくOSウィンドウを報告する：ヘッドレス実行で
	// 値が確定するようウィンドウサイズを固定する。
	ebiten.SetWindowSize(320, 200)
	v, err = guiEval(t, "screensize()")
	if err != nil || v.K != KArray || len(v.Arr.Elems) != 2 {
		t.Fatalf("screensize(): got %v, %v", v, err)
	}
	if v.Arr.Elems[0].I != 320 || v.Arr.Elems[1].I != 200 {
		t.Fatalf("screensize() = %v, want [320 200]", v.Arr.Elems)
	}
	v, err = guiEval(t, "dropfiles()")
	if err != nil || v.K != KArray || len(v.Arr.Elems) != 0 {
		t.Fatalf("dropfiles(): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "dropload(\"nope.png\")"); err == nil {
		t.Fatal("dropload without drops should error")
	}
	if _, err := guiEval(t, "winmove(\"x\", 0)"); err == nil {
		t.Fatal("winmove string should error")
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
	v, err = guiEval(t, "touchcount()")
	if err != nil || v.K != KInt || v.I != 0 {
		t.Fatalf("touchcount(): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "touchx(0)"); err == nil {
		t.Fatal("touchx without touches should error")
	}
	if _, err := guiEval(t, "touchy(0)"); err == nil {
		t.Fatal("touchy without touches should error")
	}
	v, err = guiEval(t, "mmstop()")
	if err != nil || v.K != KNull {
		t.Fatalf("mmstop(): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "stick()"); err == nil {
		t.Fatal("stick should be undefined")
	}
}

// 巨大画像はループgoroutine内のエンジン内部でパニックになるため
//（捕捉不可）、Ebitenに届く前に組み込み関数側でスクリプトエラーとして
// 拒否しなければならない。ヘッドレス安全：GPU/ウィンドウ呼び出しの
// 前に拒否される。
func TestGuiImageLimits(t *testing.T) {
	if _, err := guiEval(t, "screen(4097, 1)"); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("screen width over limit: got %v", err)
	}
	if _, err := guiEval(t, "screen(100000, 100000)"); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("screen huge: got %v", err)
	}
	// buffer()は上限と正サイズ規則を共有する。
	if _, err := guiEval(t, "buffer(1, 4097, 1)"); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("buffer width over limit: got %v", err)
	}
	if _, err := guiEval(t, "buffer(1, 0, 8)"); err == nil || !strings.Contains(err.Error(), "正の値") {
		t.Fatalf("buffer zero size: got %v", err)
	}
	// 表示キャンバスはscreen()専用：画面外IDのみ。
	if _, err := guiEval(t, "buffer(0, 8, 8)"); err == nil || !strings.Contains(err.Error(), "1 から") {
		t.Fatalf("buffer(0, ...): got %v", err)
	}
	if _, err := guiEval(t, "buffer(16, 8, 8)"); err == nil {
		t.Fatal("buffer(16, ...) should error")
	}
	if _, err := guiEval(t, "buffer(1, 8)"); err == nil {
		t.Fatal("buffer with 2 args should error")
	}
	if _, err := guiEval(t, "buffer(\"x\", 8, 8)"); err == nil {
		t.Fatal("buffer string id should error")
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

func TestGuiWidgetValidation(t *testing.T) {
	if _, err := guiEval(t, "dialog(\"x\", \"bogus\")"); err == nil {
		t.Fatal("dialog bogus mode should error")
	}
	if _, err := guiEval(t, "pressed(99)"); err == nil {
		t.Fatal("pressed unknown should error")
	}
	if _, err := guiEval(t, "gettext(99)"); err == nil {
		t.Fatal("gettext unknown should error")
	}
	if _, err := guiEval(t, "selected(99)"); err == nil {
		t.Fatal("selected unknown should error")
	}
	if _, err := guiEval(t, "clrobj(99)"); err == nil {
		t.Fatal("clrobj unknown should error")
	}
	if _, err := guiEval(t, "button(1, \"x\", 0, 0, 0, 10)"); err == nil {
		t.Fatal("button zero width should error")
	}
	// 引数順：(id, label, x, y, w, h)。文字列idはブロック前に
	// 引数0として失敗しなければならない。
	if _, err := guiEval(t, "button(\"x\", \"y\", 0, 0, 10, 10)"); err == nil {
		t.Fatal("button string id should error")
	}
	// 成功系はguiEvalが汲み上げないゲームスレッド作業をキューに入れる
	//（ブロックしてしまう）。バックエンド側の収まりは
	// TestListboxItemsFitAndStayUnique / TestButtonLabelFitsで扱う。
	if _, err := guiEval(t, "listbox_set(1)"); err == nil {
		t.Fatal("listbox_set arity should error")
	}
	if _, err := guiEval(t, "listbox_set(1, [1])"); err == nil {
		t.Fatal("listbox_set non-string item should error")
	}
	if _, err := guiEval(t, "listbox_sel(1, \"x\")"); err == nil {
		t.Fatal("listbox_sel non-int index should error")
	}
}

func TestGuiFormValidation(t *testing.T) {
	// chkboxは自動サイズ：w/h形式は廃止（4〜5引数）。注意：guiEvalは
	// 描画キューを汲み上げないため、ここではループ前のエラーのみ検証する。
	// 成功系はヘッドレスのTestFormWidgetsHeadlessで扱う。
	if _, err := guiEval(t, "chkbox(1, \"x\", 0, 0, 0, 10)"); err == nil {
		t.Fatal("old 6-arg chkbox should error")
	}
	if _, err := guiEval(t, "chkbox(\"x\", \"y\", 0, 0)"); err == nil {
		t.Fatal("chkbox string id should error")
	}
	if _, err := guiEval(t, "chkbox(1, 0, 0, 0)"); err == nil {
		t.Fatal("chkbox non-string label should error")
	}
	if _, err := guiEval(t, "checked(99)"); err == nil {
		t.Fatal("checked unknown should error")
	}
	if _, err := guiEval(t, "combox(1, 0, 0, 100, 30, [1])"); err == nil {
		t.Fatal("combox non-string item should error")
	}
	if _, err := guiEval(t, "combox(1, 0, 0, 100, 30, [\"a\"], 5)"); err == nil {
		t.Fatal("combox out-of-range sel should error")
	}
	if _, err := guiEval(t, "mesbox(1, 0, 0, 100, 0)"); err == nil {
		t.Fatal("mesbox zero height should error")
	}
	if _, err := guiEval(t, "getstr(99)"); err == nil {
		t.Fatal("getstr unknown should error")
	}
	if _, err := guiEval(t, "objprm(99, \"enable\", 1)"); err == nil {
		t.Fatal("objprm unknown should error")
	}
	if _, err := guiEval(t, "objprm(1, \"bogus\", 1)"); err == nil {
		t.Fatal("objprm bogus key should error")
	}
	// 廃止された色キーは不明キーとして報告される（テーマ固定）。
	for _, src := range []string{
		"objprm(99, \"textcolor\", 255)",
		"objprm(1, \"textcolor\", 16777216)",
		"objprm(1, \"backcolor\", -2)",
		"objprm(99, \"backcolor\", 255)",
		"objprm(1, \"color\", 255)",
		"objprm(1, \"bgcolor\", 255)",
	} {
		if _, err := guiEval(t, src); err == nil || !strings.Contains(err.Error(), "不明なキー") {
			t.Fatalf("%s: got %v, want unknown-key error", src, err)
		}
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
	if _, err := guiEval(t, "paint(-1, 0)"); err == nil {
		t.Fatal("paint out-of-range seed should error")
	}
	if _, err := guiEval(t, "paint(\"x\", 0)"); err == nil {
		t.Fatal("paint string x should error")
	}
	if v, err := guiEval(t, "paint(10, 10)"); err != nil || v.K != KNull {
		t.Fatalf("paint(10, 10): got %v, %v", v, err)
	}
	if _, err := guiEval(t, "gcopy(99, 0, 0, 10, 10)"); err == nil {
		t.Fatal("gcopy unknown buffer should error")
	}
	if _, err := guiEval(t, "gcopy(99, 0, 0, 10, 10, 1, 1)"); err == nil {
		t.Fatal("gcopy scaled unknown buffer should error")
	}
	if _, err := guiEval(t, "gcopy(0, 0, 0, 10, 10, 0, 1)"); err == nil {
		t.Fatal("gcopy zero scale should error")
	}
	if _, err := guiEval(t, "gcopy(0, \"x\", 0, 10, 10, 1, 1)"); err == nil {
		t.Fatal("gcopy string arg should error")
	}
	if _, err := guiEval(t, "gcopy(99, 0, 0, 10, 10, 1, 1, 30)"); err == nil {
		t.Fatal("gcopy rotated unknown buffer should error")
	}
	if _, err := guiEval(t, "gcopy(0, 0, 0, 10, 10, 1, 0, 30)"); err == nil {
		t.Fatal("gcopy rotated zero scale should error")
	}
	if _, err := guiEval(t, "gcopy(0, 0, 0, 10, 10, 1)"); err == nil {
		t.Fatal("gcopy 6 args should error")
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
	mustErrIO(t, "pget(10, 10)\n", "pget は GUI ビルドが必要です")
	// ヘッドレスウィンドウバックエンド（320x200）：検証は
	// ゲームスレッド読み戻しより先に実行される。
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
	if _, err := guiEval(t, "pget(\"x\", 0)"); err == nil {
		t.Fatal("pget string x should error")
	}
	if _, err := guiEval(t, "pget(0, 1.5)"); err == nil {
		t.Fatal("pget float y should error")
	}
	if _, err := guiEval(t, "pget(1)"); err == nil {
		t.Fatal("pget arity should error")
	}
}
