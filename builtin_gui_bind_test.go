package main

// Wbind validation tests (no widget creation, no game loop).
import (
	"strings"
	"testing"

	"gsh/gui"
)

func TestParseVarRef(t *testing.T) {
	b, i, h, ok := parseVarRef("x")
	if !ok || h || b != "x" {
		t.Fatalf("x: %q %d %v %v", b, i, h, ok)
	}
	b, i, h, ok = parseVarRef("arr[0]")
	if !ok || !h || b != "arr" || i != 0 {
		t.Fatalf("arr[0]: %q %d %v %v", b, i, h, ok)
	}
	b, i, h, ok = parseVarRef("a[12]")
	if !ok || !h || b != "a" || i != 12 {
		t.Fatalf("a[12]: %q %d %v %v", b, i, h, ok)
	}
	for _, s := range []string{"", "a[", "a]", "a[]", "[0]", "a[x]", "a[1", "a]"} {
		if _, _, _, ok := parseVarRef(s); ok {
			t.Fatalf("%q should fail", s)
		}
	}
}

func guiWbindEval(t *testing.T, src string) error {
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
	m := newVmachine(in)
	vprog, err := Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return m.runMain(vprog)
}

func TestWbindValidation(t *testing.T) {
	if err := guiWbindEval(t, "x = 1\nwbind(999, \"x\")"); err == nil {
		t.Fatal("wbind unknown widget should error")
	}
	if err := guiWbindEval(t, "wbind(1, \"\")"); err == nil {
		t.Fatal("wbind empty varname should error")
	}
	if err := guiWbindEval(t, "wbind(\"a\", \"x\")"); err == nil {
		t.Fatal("wbind non-int id should error")
	}
	if err := guiWbindEval(t, "wsync()"); err != nil {
		t.Fatalf("wsync empty: %v", err)
	}
	if err := guiWbindEval(t, "wunbind(999)"); err != nil {
		t.Fatalf("wunbind: %v", err)
	}
}
