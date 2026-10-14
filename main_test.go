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

// TestVersionPinnedEverywhere は版上げを低コストに保つための検査です：唯一の正本は
// golashVersion であり、このテストは追随すべき箇所をすべて挙げます。版上げ手順：
// 定数を1か所変えて go test を実行し、このテストが指摘した各ファイルを更新します。
func TestVersionPinnedEverywhere(t *testing.T) {
	want := "v" + golashVersion
	// 固定ドキュメント：言語版を明記するタイトルや前文。
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
	// コード内の版リテラル禁止：ファイル先頭はかつて実行時と食い違う古い版を
	// 掲げていたため、先頭は版なしに保ち、この走査で再発を拒否します。
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
	// 先頭行だけが残ります（スタックダンプなし）。
	got := enginePanicMsg("atlas: too big\n goroutine 23 [running]:\n ...")
	if got != "atlas: too big" {
		t.Fatalf("got %q", got)
	}
	// 文字列でないpanicは文字列化されます。
	if got := enginePanicMsg(errors.New("glfw: boom")); got != "glfw: boom" {
		t.Fatalf("got %q", got)
	}
	// 空や空白だけのpanicには置換文が入り、空エラーにはなりません。
	for _, r := range []any{"", "  \n  "} {
		if got := enginePanicMsg(r); got == "" {
			t.Fatalf("empty panic rendered empty")
		}
	}
}
