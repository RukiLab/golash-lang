package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runFile はスクリプトファイルを（include の展開込みで）解析して実行し、
func runFile(t *testing.T, path string) (string, error) {
	t.Helper()
	prog, err := ParseFile(path)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	in := NewInterp(&buf)
	vprog, verr := Compile(prog)
	if verr != nil {
		return buf.String(), verr
	}
	err = newVmachine(in).runMain(vprog)
	return buf.String(), err
}

func writeFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o666); err != nil {
		t.Fatal(err)
	}
}

// runFileSrc 縺ｯ荳譎ゅヵ繧｡繧､繝ｫ縺ｫ src 繧呈嶌縺・※螳溯｡後＠縺ｾ縺吶・
func runFileSrc(t *testing.T, src string) (string, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "main.gsh")
	writeFile(t, p, src)
	return runFile(t, p)
}

func TestIncludeBasic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lib.gsh"), "def double(x)\nreturn x * 2\nend\nanswer = 20\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "include \"lib.gsh\"\nmes(double(21))\nmes(answer + 1)\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "42\n21\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIncludeNestedDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sub", "util.gsh"), "def inc(x)\nreturn x + 1\nend\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "include \"sub/util.gsh\"\nmes(inc(41))\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "42\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIncludeOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "base.gsh"), "def f()\nreturn 7\nend\n")
	writeFile(t, filepath.Join(dir, "mid.gsh"), "include \"base.gsh\"\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "include \"base.gsh\"\ninclude \"mid.gsh\"\nmes(f())\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "7\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIncludeCircular(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.gsh"), "include \"b.gsh\"\n")
	writeFile(t, filepath.Join(dir, "b.gsh"), "include \"a.gsh\"\n")
	_, err := runFile(t, filepath.Join(dir, "a.gsh"))
	if err == nil || !strings.Contains(err.Error(), "循環する include") {
		t.Fatalf("err = %v", err)
	}
}

func TestIncludeMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.gsh"), "include \"nope.gsh\"\n")
	_, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err == nil || !strings.Contains(err.Error(), "ファイルが見つかりません") {
		t.Fatalf("err = %v", err)
	}
}

func TestIncludeMalformed(t *testing.T) {
	dir := t.TempDir()
	cases := []struct{ src, want string }{
		{"include\n", "include \"file\" が必要"},
		{"include \"a.gsh\" extra\n", "末尾に"},
		{"include \"\"\n", "パスが空"},
	}
	for i, c := range cases {
		name := filepath.Join(dir, "bad"+string(rune('0'+i))+".gsh")
		writeFile(t, name, c.src)
		_, err := runFile(t, name)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%q: err = %v, want %q", c.src, err, c.want)
		}
	}
}

func TestIncludeErrorPosition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lib.gsh"), "def boom()\nreturn nosuchvar\nend\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "include \"lib.gsh\"\nmes(boom())\n")
	_, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err == nil || !strings.Contains(err.Error(), "lib.gsh:2:8") {
		t.Fatalf("err = %v", err)
	}
	// 讒区枚繧ｨ繝ｩ繝ｼ縺ｮ菴咲ｽｮ繧ょ叙繧願ｾｼ縺ｿ蜈医ヵ繧｡繧､繝ｫ繧呈欠縺吶・
	writeFile(t, filepath.Join(dir, "bad.gsh"), "if {\n}\n")
	writeFile(t, filepath.Join(dir, "main2.gsh"), "include \"bad.gsh\"\n")
	if _, err := runFile(t, filepath.Join(dir, "main2.gsh")); err == nil ||
		!strings.Contains(err.Error(), "bad.gsh:1:4") {
		t.Fatalf("parse err = %v", err)
	}
}

func TestIncludeArrayAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.gsh"), "point = [3, 4]\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "include \"types.gsh\"\nmes(point[0])\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "3\n" {
		t.Fatalf("got %q", got)
	}
}

func TestModeResolution(t *testing.T) {
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
	// GUI 邨・ｾｼ縺ｿ縺後↑縺代ｌ縺ｰ CUI縲・
	if m := modeOf("a.gsh", "mes(\"hi\")\n"); m != "cli" {
		t.Fatalf("auto plain = %q", m)
	}
	// 蛻ｰ驕泌庄閭ｽ縺ｪ GUI 邨・ｾｼ縺ｿ縺後≠繧後・ GUI縲・
	if m := modeOf("b.gsh", "screen(320, 240)\n"); m != "gui" {
		t.Fatalf("auto gui = %q", m)
	}
	// 蜻ｼ縺ｰ繧後↑縺・未謨ｰ縺ｮ GUI 蜻ｼ縺ｳ蜃ｺ縺励・辟｡隕悶☆繧具ｼ・3d 縺ｮ繝倥Ν繝大ｯｾ遲厄ｼ峨・
	if m := modeOf("c.gsh", "def helper()\nscreen(320, 240)\nend\nmes(\"hi\")\n"); m != "cli" {
		t.Fatalf("unreachable gui = %q", m)
	}
	if m := modeOf("d.gsh", "def helper()\nscreen(320, 240)\nend\nhelper()\n"); m != "gui" {
		t.Fatalf("reachable gui = %q", m)
	}
	if m := modeOf("d2.gsh", "def outer()\ninner()\nend\ndef inner()\nscreen(320, 240)\nend\nouter()\n"); m != "gui" {
		t.Fatalf("nested gui = %q", m)
	}
	// mode 螳｣險縺ｯ蟒・ｭ｢縺輔ｌ縺ｦ縺・ｋ縲・
	writeFile(t, filepath.Join(dir, "m.gsh"), "mode cli\nmes(1)\n")
	if _, _, err := ParseFileMode(filepath.Join(dir, "m.gsh")); err == nil ||
		!strings.Contains(err.Error(), "mode は廃止されました") {
		t.Fatalf("mode declaration err = %v", err)
	}
}

func TestLegacyDirectiveErrors(t *testing.T) {
	for _, src := range []string{"#define X 1\n", "#include \"a.gsh\"\n", "#mode cli\n", "#ifdef X\n#endif\n"} {
		_, err := runFileSrc(t, src)
		if err == nil || !strings.Contains(err.Error(), "# ディレクティブは廃止されました") {
			t.Fatalf("%q: err = %v", src, err)
		}
	}
}
