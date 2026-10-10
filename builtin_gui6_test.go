package main

import (
	"strings"
	"testing"
)

// gsquare/gtritex builtin validation (headless: every failure returns
// before touching the GPU, like the gcopy checks).
func TestPerspBuiltinValidation(t *testing.T) {
	if _, err := guiEval(t, "gsquare(99, 0, 0, 10, 10, 0, 0, 10, 0, 10, 10, 0, 10)"); err == nil {
		t.Fatal("gsquare unknown buffer should error")
	}
	if _, err := guiEval(t, "gsquare(0, 0, 0, 0, 10, 0, 0, 10, 0, 10, 10, 0, 10)"); err == nil {
		t.Fatal("gsquare zero size should error")
	}
	if _, err := guiEval(t, "gsquare(0, 0, 0, 10, 10, 0, 0, 10, 0, 10, 10, 0)"); err == nil {
		t.Fatal("gsquare 12 args should error")
	}
	if _, err := guiEval(t, "gsquare(0, \"x\", 0, 10, 10, 0, 0, 10, 0, 10, 10, 0, 10)"); err == nil {
		t.Fatal("gsquare string arg should error")
	}
	if _, err := guiEval(t, "gtritex(99, 0, 0, 10, 0, 0, 10, 0, 0, 20, 0, 0, 20)"); err == nil {
		t.Fatal("gtritex unknown buffer should error")
	}
	if _, err := guiEval(t, "gtritex(0, 0, 0, 10, 0, 0, 10, 0, 0, 20, 0, 0, 20, 1)"); err == nil ||
		!strings.Contains(err.Error(), "13 個または 16 個") {
		t.Fatalf("gtritex 14 args: got %v", err)
	}
	if _, err := guiEval(t, "gtritex(0, 0, 0, 10, 0, 0, 10, 0, 0, 20, 0, 0, 20, 1, 1)"); err == nil {
		t.Fatal("gtritex 15 args should error")
	}
	if _, err := guiEval(t, "gtritex(0, 0, 0, 10, 0, 0, 10, 0, 0, 20, 0, 0, \"x\")"); err == nil {
		t.Fatal("gtritex string arg should error")
	}
	if _, err := guiEval(t, "gtri(0)"); err == nil {
		t.Fatal("gtri should be undefined")
	}
}
