package main

import (
	"strings"
	"testing"
)

// gflat組み込みの検証（ヘッドレス：失敗はGPUに届く前に返り、有効な呼び出しはコマンドをためるだけ）。
func TestGflatValidation(t *testing.T) {
	// 面の並び：xs/ysは宛先の4隅を順番に並べたもの、colsは面ごとに1つの0xRRGGBB。
	const four = "0,16,16,0"
	// xs が配列でない。
	if _, err := guiEval(t, "gflat("+four+", ["+four+"], [16777215])"); err == nil {
		t.Fatal("gflat with a bare number should error")
	}
	// 3/4: 4の倍数ではない。
	if _, err := guiEval(t, "gflat([0,16,16], [0,0,16], [0])"); err == nil ||
		!strings.Contains(err.Error(), "4 の倍数") {
		t.Fatalf("gflat length 3: got %v", err)
	}
	// 空配列は何もしない。
	if _, err := guiEval(t, "gflat([], [], [])"); err != nil {
		t.Fatalf("gflat empty batch: got %v", err)
	}
	// cols は面数 (xs/4) と一致すること。4N 展開は受け付けない。
	if _, err := guiEval(t, "gflat(["+four+"], ["+four+"], [16777215,16777215])"); err == nil ||
		!strings.Contains(err.Error(), "cols は面数") {
		t.Fatalf("gflat cols mismatch: got %v", err)
	}
	if _, err := guiEval(t, "gflat(["+four+"], [0,0], [16777215])"); err == nil ||
		!strings.Contains(err.Error(), "4 の倍数") {
		t.Fatalf("gflat ys mismatch: got %v", err)
	}
	// 数値でない座標要素。
	if _, err := guiEval(t, "gflat([0,16,\"x\",0], ["+four+"], [16777215])"); err == nil ||
		!strings.Contains(err.Error(), "要素 2 は数値") {
		t.Fatalf("gflat string element: got %v", err)
	}
	// 0xRRGGBB の範囲外の色。
	if _, err := guiEval(t, "gflat(["+four+"], ["+four+"], [16777216])"); err == nil ||
		!strings.Contains(err.Error(), "要素 0") {
		t.Fatalf("gflat out-of-range color: got %v", err)
	}
	if _, err := guiEval(t, "gflat(["+four+"], ["+four+"], [-1])"); err == nil {
		t.Fatal("gflat negative color should error")
	}
	// 引数の数は厳密: 3個。
	if _, err := guiEval(t, "gflat([0,16], [0,0])"); err == nil {
		t.Fatal("gflat with 2 args should error")
	}
	// 1コマンドで3面のまとまり（ヘッドレスでも dst のみで通る）。
	batch := "gflat([0,16,16,0, 32,48,48,32, 64,80,80,64], " +
		"[0,0,16,16, 0,0,16,16, 0,0,16,16], " +
		"[16777215, 16711680, 65280])"
	if _, err := guiEval(t, batch); err != nil {
		t.Fatalf("gflat batch: got %v", err)
	}
	// 1コマンドあたりの面上限を超過。
	if _, err := guiEval(t, "gflat(dim(65537*4), dim(65537*4), dim(65537))"); err == nil ||
		!strings.Contains(err.Error(), "65536") {
		t.Fatalf("gflat over the cap: got %v", err)
	}
}
