package main

import (
	"strings"
	"testing"
)

// gquad組み込みの検証（ヘッドレス：失敗はGPUに届く前に返り、有効な呼び出しはコマンドをためるだけ）。
func TestGquadValidation(t *testing.T) {
	// 面の並び：xs/ysは宛先の4隅を順番に並べたもの、
	// us/vsは転送元の4画素、lightsは隅ごとに1つの0xRRGGBB。
	const four = "0,16,16,0"
	if _, err := guiEval(t, "gquad(99, ["+four+"], ["+four+"], [0,8,8,0], [0,0,8,8], [16777215,16777215,16777215,16777215])"); err == nil {
		t.Fatal("gquad unknown buffer should error")
	}
	if _, err := guiEval(t, "gquad(1, "+four+", ["+four+"], [0,8,8,0], [0,0,8,8], [16777215,16777215,16777215,16777215])"); err == nil {
		t.Fatal("gquad with a bare number should error")
	}
	// 3/4: 4の倍数ではない。
	if _, err := guiEval(t, "gquad(0, [0,16,16], [0,0,16], [0,8,8], [0,0,8], [0,0,0])"); err == nil ||
		!strings.Contains(err.Error(), "4 の倍数") {
		t.Fatalf("gquad length 3: got %v", err)
	}
	// 空配列は何もしない（すべての面を間引いたループは何も描かない）。
	// バッファすら参照されない。
	if _, err := guiEval(t, "gquad(99, [], [], [], [], [])"); err != nil {
		t.Fatalf("gquad empty batch: got %v", err)
	}
	// 長さの不一致（1つ足りない面だけ照光される）。
	if _, err := guiEval(t, "gquad(0, ["+four+"], ["+four+"], ["+four+"], ["+four+"], [16777215,16777215])"); err == nil ||
		!strings.Contains(err.Error(), "4 の倍数") {
		t.Fatalf("gquad mismatched lengths: got %v", err)
	}
	// 数値でない座標要素。
	if _, err := guiEval(t, "gquad(0, [0,16,\"x\",0], ["+four+"], [0,8,8,0], [0,0,8,8], [16777215,16777215,16777215,16777215])"); err == nil ||
		!strings.Contains(err.Error(), "要素 2 は数値") {
		t.Fatalf("gquad string element: got %v", err)
	}
	// 0xRRGGBB の範囲外の色。
	if _, err := guiEval(t, "gquad(0, ["+four+"], ["+four+"], [0,8,8,0], [0,0,8,8], [16777216,0,0,0])"); err == nil ||
		!strings.Contains(err.Error(), "要素 0") {
		t.Fatalf("gquad out-of-range light: got %v", err)
	}
	if _, err := guiEval(t, "gquad(0, ["+four+"], ["+four+"], [0,8,8,0], [0,0,8,8], [-1,0,0,0])"); err == nil {
		t.Fatal("gquad negative light should error")
	}
	// int でないライト（文字列、浮動小数点数）。
	if _, err := guiEval(t, "gquad(0, ["+four+"], ["+four+"], [0,8,8,0], [0,0,8,8], [\"x\",0,0,0])"); err == nil {
		t.Fatal("gquad string light should error")
	}
	// 引数の数は厳密: 6個。
	if _, err := guiEval(t, "gquad(0, [0,16], [0,0], [0,8])"); err == nil {
		t.Fatal("gquad with 4 args should error")
	}
	// 1コマンドで3面のまとまり。guiEval のバックエンドには確保済み
	// バッファがないため、形状として有効な呼出はバッファ検査で
	// 終わる:
	// reaching "不明なバッファ" proves every length/color rule passed.
	batch := "gquad(1, [0,16,16,0, 32,48,48,32, 64,80,80,64], " +
		"[0,0,16,16, 0,0,16,16, 0,0,16,16], " +
		"[0,8,8,0, 0,8,8,0, 0,8,8,0], " +
		"[0,0,8,8, 8,8,16,16, 16,16,24,24], " +
		"[16777215,16777215,16777215,16777215, 16711680,16711680,16711680,16711680, 65280,65280,65280,65280])"
	if _, err := guiEval(t, batch); err == nil || !strings.Contains(err.Error(), "不明なバッファ") {
		t.Fatalf("gquad batch: got %v", err)
	}
	if _, err := guiEval(t, "gquad(1, ["+four+"], ["+four+"], [0,8,8,0], [0,0,8,8], [16777215,16777215,16777215,16777215])"); err == nil ||
		!strings.Contains(err.Error(), "不明なバッファ") {
		t.Fatalf("gquad single: got %v", err)
	}
	// 1コマンドあたりの面上限を超過（バッファ検索より前に検査される）。
	// dim(4*n) は平坦形式: 65537面 = 262148要素。
	if _, err := guiEval(t, "gquad(1, dim(65537*4), dim(65537*4), dim(65537*4), dim(65537*4), dim(65537*4))"); err == nil ||
		!strings.Contains(err.Error(), "65536") {
		t.Fatalf("gquad over the cap: got %v", err)
	}
}
