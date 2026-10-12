package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestVersionWiring(t *testing.T) {
	// usage と REPL バナーが単一の版定数から組み立てられていること。
	if !strings.Contains(usage, "v"+golashVersion) {
		t.Fatalf("usage %q lacks version v%s", usage, golashVersion)
	}
}

// TestVersionPinnedEverywhere keeps version bumps cheap: the only
// source of truth is golashVersion, and this test names every place
// that must follow it. Bump procedure: change the one constant, run
// go test, update each file this test flags.
func TestVersionPinnedEverywhere(t *testing.T) {
	want := "v" + golashVersion
	// Pinned docs: titles/preambles that state the language version.
	for _, f := range []string{
		"docs/MANUAL.md", "docs/DESIGN_VM.md", "README.md", "CHANGELOG.md",
	} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if !strings.Contains(string(b), want) {
			t.Fatalf("%s lacks version %q (update the doc with golashVersion)", f, want)
		}
	}
	// No version literals in code: file headers once carried a stale
	// version while the runtime had moved on, so headers stay
	// version-free and this scan rejects any reintroduction.
	ver := regexp.MustCompile(`v[0-9]+\.[0-9]+`)
	var files []string
	for _, dir := range []string{".", "gui"} {
		ms, err := filepath.Glob(dir + "/*.go")
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		files = append(files, ms...)
	}
	if len(files) == 0 {
		t.Fatal("no Go files found")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if m := ver.Find(b); m != nil {
			t.Fatalf("%s embeds version literal %q (keep versions in golashVersion only)", f, m)
		}
	}
}

func TestEnginePanicMsg(t *testing.T) {
	// Only the first line survives (no stack dump).
	got := enginePanicMsg("atlas: too big\n goroutine 23 [running]:\n ...")
	if got != "atlas: too big" {
		t.Fatalf("got %q", got)
	}
	// Non-string panics stringify.
	if got := enginePanicMsg(errors.New("glfw: boom")); got != "glfw: boom" {
		t.Fatalf("got %q", got)
	}
	// Empty/blank panics get a placeholder, never an empty error.
	for _, r := range []any{"", "  \n  "} {
		if got := enginePanicMsg(r); got == "" {
			t.Fatalf("empty panic rendered empty")
		}
	}
}
