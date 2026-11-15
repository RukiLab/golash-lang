// 使い方:
//
//	go run ./tools/bumpver.go -set 0.9   # 版を 0.9 に上げる
//	go run ./tools/bumpver.go -bump minor # 0.9 → 0.10
//	go run ./tools/bumpver.go -bump major # 0.9 → 1.0
//	go run ./tools/bumpver.go -show        # 現在の版だけ表示
//
// 版（MAJOR.MINOR のドット1つ）をリポジトリ全体へ一括反映します。
// 対象は VERSION（正本）・main.go の golashVersion・ルート package.json と
// VS Code 拡張の package.json・ドキュメント（MANUAL/DESIGN_VM/README）の
// `v<X.Y>` 表記・CHANGELOG の見出しです。
//
// 版の判断基準は README の「バージョン管理」に従います（MAJOR=破壊的変更、
// MINOR=追加・修正）。テスト TestVersionPinnedEverywhere が
// 反映漏れを検出するので、実行後に go test ./... を回してください。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// versionRe は MAJOR.MINOR（先頭ゼロなし、ドット1つ）のみを受けます。
var versionRe = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

func main() {
	set := flag.String("set", "", "この版に設定する（例: 0.9）")
	bump := flag.String("bump", "", "major/minor のいずれかを1つ上げる")
	show := flag.Bool("show", false, "現在の版を表示する")
	flag.Parse()

	cur, err := readVersion()
	if err != nil {
		fatal(err)
	}
	if *show {
		fmt.Println(cur)
		return
	}
	next := cur
	switch {
	case *set != "":
		next = *set
	case *bump != "":
		next, err = bumpVersion(cur, *bump)
		if err != nil {
			fatal(err)
		}
	default:
		fmt.Fprintf(os.Stderr, "使い方: go run ./tools/bumpver.go -set 0.9 | -bump major|minor | -show\n")
		os.Exit(2)
	}
	if !versionRe.MatchString(next) {
		fatal(fmt.Errorf("版 %q は MAJOR.MINOR（ドット1つ）ではありません", next))
	}
	// ドキュメントは `v<X.Y>` 表記なので v を付け、package.json は版番号のみ。
	oldTag, newTag := "v"+cur, "v"+next
	for _, f := range docFiles() {
		if err := rewrite(f, oldTag, newTag); err != nil {
			fatal(err)
		}
	}
	if err := rewrite("package.json", cur, next); err != nil {
		fatal(err)
	}
	if err := rewrite(filepath.Join("editors", "vscode", "package.json"), cur, next); err != nil {
		fatal(err)
	}
	if err := rewriteGolashVersion(next); err != nil {
		fatal(err)
	}
	if err := writeVersion(next); err != nil {
		fatal(err)
	}
	fmt.Printf("%s → %s に更新しました。go test ./... で確認し、CHANGELOG を見出し「## [%s]」に整えてください。\n", oldTag, newTag, next)
}

// readVersion は VERSION（正本）を読みます。
func readVersion() (string, error) {
	b, err := os.ReadFile("VERSION")
	if err != nil {
		return "", fmt.Errorf("VERSION の読み込み: %w", err)
	}
	v := strings.TrimSpace(string(b))
	if !versionRe.MatchString(v) {
		return "", fmt.Errorf("VERSION %q は MAJOR.MINOR（ドット1つ）ではありません", v)
	}
	return v, nil
}

// writeVersion は VERSION を書き換えます。
func writeVersion(v string) error {
	return os.WriteFile("VERSION", []byte(v+"\n"), 0o644)
}

// bumpVersion は版の桁を1つ上げます。
func bumpVersion(cur, kind string) (string, error) {
	parts := strings.Split(cur, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("版 %q は MAJOR.MINOR ではありません", cur)
	}
	n := make([]int, 2)
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil {
			return "", err
		}
		n[i] = x
	}
	switch kind {
	case "major":
		n[0], n[1] = n[0]+1, 0
	case "minor":
		n[1]++
	default:
		return "", fmt.Errorf("-bump は major/minor のいずれかを指定してください（%q）", kind)
	}
	return fmt.Sprintf("%d.%d", n[0], n[1]), nil
}

// docFiles は `v<X.Y>` 表記を持つドキュメントです。
func docFiles() []string {
	return []string{
		filepath.Join("docs", "MANUAL.md"),
		filepath.Join("docs", "DESIGN_VM.md"),
		"README.md",
		"CHANGELOG.md",
	}
}

// rewrite はファイル内の old を to に置換します。1件も無ければエラーにし、
// 取りこぼしを黙って通さないようにします。
func rewrite(path, old, to string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	s := string(b)
	if !strings.Contains(s, old) {
		return fmt.Errorf("%s に %q がありません（手で確認してください）", path, old)
	}
	return os.WriteFile(path, []byte(strings.ReplaceAll(s, old, to)), 0o644)
}

// golashVersionRe は main.go の版代入を捕捉します。コメント内の版リテラルは
// 禁止されている（TestVersionPinnedEverywhere）ため、代入の値だけを置換します。
var golashVersionRe = regexp.MustCompile(`(var golashVersion = ")([^"]+)(")`)

// rewriteGolashVersion は main.go の golashVersion の値を差し替えます。
func rewriteGolashVersion(v string) error {
	b, err := os.ReadFile("main.go")
	if err != nil {
		return err
	}
	m := golashVersionRe.FindSubmatchIndex(b)
	if m == nil {
		return fmt.Errorf("main.go に var golashVersion の代入がありません")
	}
	out := append([]byte{}, b[:m[4]]...)
	out = append(out, []byte(v)...)
	out = append(out, b[m[5]:]...)
	return os.WriteFile("main.go", out, 0o644)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "エラー:", err)
	os.Exit(1)
}
