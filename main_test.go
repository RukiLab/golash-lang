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

// semverValue は SemVer（X.Y.Z[-prerelease][+build]）の厳密な形です。
// 版は3桁に揃えることで、比較・タグ・package.json との突き合わせが
// 機械的にでき、0.3 のような2桁表記で起きる曖昧さを避けます。
var semverValue = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// TestVersionPinnedEverywhere は版上げを低コストに保つための検査です：唯一の
// 正本は VERSION ファイル（と同値を保つ golashVersion）であり、このテストは
// 追随すべき箇所をすべて挙げます。版上げ手順：
//
//	tools/bumpver.go -set 0.9.0     # VERSION/コード/package.json/docs を一括更新
//	go test ./...
func TestVersionPinnedEverywhere(t *testing.T) {
	want := "v" + golashVersion
	// SemVer の形であること（2桁の 0.3 などを拒否する）。
	if !semverValue.MatchString(golashVersion) {
		t.Fatalf("golashVersion %q is not SemVer (use X.Y.Z)", golashVersion)
	}
	// VERSION ファイルが正本。git のタグ・CI・ldflags はここを読みます。
	vb, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	if strings.TrimSpace(string(vb)) != golashVersion {
		t.Fatalf("VERSION %q != golashVersion %q (update VERSION or run go run ./tools/bumpver.go -set %s)",
			strings.TrimSpace(string(vb)), golashVersion, golashVersion)
	}
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
	// package.json の version も golashVersion に一致させる（VS Code 拡張は
	// その値がストアの版になるため、処理系と別版にならないようにする）。
	for _, f := range []string{"package.json", "editors/vscode/package.json"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		m := regexp.MustCompile(`"version"\s*:\s*"([^"]+)"`).FindSubmatch(b)
		if m == nil {
			t.Fatalf("%s lacks a version field", f)
		}
		if string(m[1]) != golashVersion {
			t.Fatalf("%s version %q != golashVersion %q", f, m[1], golashVersion)
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

// captureStdout は f の実行中に標準出力へ書かれた内容をまとめて返します。
// コマンド関数は fmt.Println で os.Stdout に直接書くため、出力を検証する
// には差し替えが必要です（終了コードの検査をする関数には使えません）。
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()
	f()
	w.Close()
	os.Stdout = orig
	out := <-done
	r.Close()
	return out
}

// TestVersionCommand は `gsh version` が SemVer の版を1行で出すことを確かめます。
// 既定出力は `Golash v<X.Y.Z> (SemVer)`、`-json` は {"version":"..."} です。
func TestVersionCommand(t *testing.T) {
	out := captureStdout(t, func() { cmdVersion(nil) })
	if !strings.Contains(out, "Golash v"+golashVersion) {
		t.Fatalf("version output %q lacks Golash v%s", out, golashVersion)
	}
	if !strings.Contains(out, "SemVer") {
		t.Fatalf("version output %q lacks SemVer marker", out)
	}
	out = captureStdout(t, func() { cmdVersion([]string{"-json"}) })
	if !strings.Contains(out, `"version":"`+golashVersion+`"`) {
		t.Fatalf("json output %q lacks version %s", out, golashVersion)
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
