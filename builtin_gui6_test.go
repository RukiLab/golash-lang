package main

import (
	"strings"
	"testing"
)

// gquad builtin validation (headless: every failure returns before
// anything reaches the GPU, and a valid call only stages a command).
func TestGquadValidation(t *testing.T) {
	// Face layout: xs/ys are the four destination corners in order,
	// us/vs the four source pixels, lights one 0xRRGGBB per corner.
	const four = "0,16,16,0"
	if _, err := guiEval(t, "gquad(99, ["+four+"], ["+four+"], [0,8,8,0], [0,0,8,8], [16777215,16777215,16777215,16777215])"); err == nil {
		t.Fatal("gquad unknown buffer should error")
	}
	if _, err := guiEval(t, "gquad(1, "+four+", ["+four+"], [0,8,8,0], [0,0,8,8], [16777215,16777215,16777215,16777215])"); err == nil {
		t.Fatal("gquad with a bare number should error")
	}
	// 3 of 4: not a multiple of four.
	if _, err := guiEval(t, "gquad(0, [0,16,16], [0,0,16], [0,8,8], [0,0,8], [0,0,0])"); err == nil ||
		!strings.Contains(err.Error(), "4 の倍数") {
		t.Fatalf("gquad length 3: got %v", err)
	}
	// Empty arrays are a no-op (a loop that culled every face draws
	// nothing): not even the buffer is looked at.
	if _, err := guiEval(t, "gquad(99, [], [], [], [], [])"); err != nil {
		t.Fatalf("gquad empty batch: got %v", err)
	}
	// Mismatched lengths (lights one face short).
	if _, err := guiEval(t, "gquad(0, ["+four+"], ["+four+"], ["+four+"], ["+four+"], [16777215,16777215])"); err == nil ||
		!strings.Contains(err.Error(), "4 の倍数") {
		t.Fatalf("gquad mismatched lengths: got %v", err)
	}
	// Non-numeric coordinate element.
	if _, err := guiEval(t, "gquad(0, [0,16,\"x\",0], ["+four+"], [0,8,8,0], [0,0,8,8], [16777215,16777215,16777215,16777215])"); err == nil ||
		!strings.Contains(err.Error(), "要素 2 は数値") {
		t.Fatalf("gquad string element: got %v", err)
	}
	// A color out of the 0xRRGGBB range.
	if _, err := guiEval(t, "gquad(0, ["+four+"], ["+four+"], [0,8,8,0], [0,0,8,8], [16777216,0,0,0])"); err == nil ||
		!strings.Contains(err.Error(), "要素 0") {
		t.Fatalf("gquad out-of-range light: got %v", err)
	}
	if _, err := guiEval(t, "gquad(0, ["+four+"], ["+four+"], [0,8,8,0], [0,0,8,8], [-1,0,0,0])"); err == nil {
		t.Fatal("gquad negative light should error")
	}
	// A light that is not an int (a string, a float).
	if _, err := guiEval(t, "gquad(0, ["+four+"], ["+four+"], [0,8,8,0], [0,0,8,8], [\"x\",0,0,0])"); err == nil {
		t.Fatal("gquad string light should error")
	}
	// Arity is exact: 6 arguments.
	if _, err := guiEval(t, "gquad(0, [0,16], [0,0], [0,8])"); err == nil {
		t.Fatal("gquad with 4 args should error")
	}
	// A batch of three faces in one command. guiEval's backend has no
	// allocated buffers, so a shape-valid call ends at the buffer check:
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
	// Over the per-command face cap (checked before the buffer lookup).
	// dim(4*n) is the flat form: 65537 faces = 262148 elements.
	if _, err := guiEval(t, "gquad(1, dim(65537*4), dim(65537*4), dim(65537*4), dim(65537*4), dim(65537*4))"); err == nil ||
		!strings.Contains(err.Error(), "65536") {
		t.Fatalf("gquad over the cap: got %v", err)
	}
}
