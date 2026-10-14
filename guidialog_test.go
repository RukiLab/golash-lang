package main

import (
	"os"
	"strings"
	"testing"
)

// reportGUIError は両方の経路に届く必要があります：stderr（端末実行用）
// およびネイティブダイアログ（分離起動した GUI 子プロセスにはコンソールがありません）。
// ダイアログはスタブ化されているため、ヘッドレスなテスト実行がブロックされることはありません。
func TestReportGUIErrorBothChannels(t *testing.T) {
	old := guiErrorDialog
	defer func() { guiErrorDialog = old }()
	var gotTitle, gotMsg string
	guiErrorDialog = func(title, msg string) {
		gotTitle, gotMsg = title, msg
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	olderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = olderr }()

	reportGUIError("zz_t1.gsh:4:6: インデックス 5 は範囲外です")

	_ = w.Close()
	os.Stderr = olderr
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	serr := string(buf[:n])

	if !strings.Contains(serr, "インデックス 5 は範囲外です") {
		t.Fatalf("stderr lacks message: %q", serr)
	}
	if gotTitle == "" || !strings.Contains(gotMsg, "インデックス 5 は範囲外です") {
		t.Fatalf("dialog not called properly: title=%q msg=%q", gotTitle, gotMsg)
	}
}
