package main

import (
	"path/filepath"
	"testing"
)

func TestGuiModeBuiltinsRegistered(t *testing.T) {
	for name := range guiModeBuiltins {
		if !isBuiltin(name) {
			t.Errorf("gui mode builtin %q は登録されていません", name)
		}
	}
	// guiBuiltinNames は GUI 専用組込みの列挙。全て guiModeBuiltins に
	// 入っていることを検査する。
	for _, name := range guiBuiltinNames {
		if !guiModeBuiltins[name] {
			t.Errorf("GUI 専用組込み %q が guiModeBuiltins にありません", name)
		}
	}
	// CUI でも動く組込みを GUI 判定に混ぜない。
	for _, name := range []string{"mes", "print", "input", "pos", "color", "clip"} {
		if guiModeBuiltins[name] {
			t.Errorf("%q は GUI 専用ではありません", name)
		}
	}
}

func TestDialogFileModeDetection(t *testing.T) {
	dir := t.TempDir()
	modeOf := func(name, src string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		writeFile(t, p, src)
		_, mode, err := ParseFileMode(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return mode
	}
	// ファイル選択 (3/4) は CUI のまま。
	if m := modeOf("f3.gsh", "mes(dialog(\"G|*.gsh\", 3))\n"); m != "cli" {
		t.Fatalf("dialog 3 = %q, want cli", m)
	}
	if m := modeOf("f4.gsh", "mes(dialog(\"G|*.gsh\", 4))\n"); m != "cli" {
		t.Fatalf("dialog 4 = %q, want cli", m)
	}
	// メッセージ系（省略・0・1・非リテラル）は GUI。
	for _, src := range []string{
		"dialog(\"hi\")\n",
		"dialog(\"hi\", 0)\n",
		"dialog(\"hi\", 1)\n",
		"dialog(\"f\", 1 + 2)\n",
	} {
		if m := modeOf("g.gsh", src); m != "gui" {
			t.Fatalf("src %q: mode = %q, want gui", src, m)
		}
	}
}

func TestParseErrorDefaultsToCLI(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.gsh")
	writeFile(t, p, "if {\n}\n")
	_, mode, err := ParseFileMode(p)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if mode != "cli" {
		t.Fatalf("mode = %q, want cli", mode)
	}
}
