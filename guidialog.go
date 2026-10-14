package main

import (
	"fmt"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/sqweek/dialog"
	"gsh/gui"
)

// guiErrorDialog はネイティブのエラーダイアログを表示する
// （Windows: メッセージボックス、Linux/macOS: sqweek/dialog 経由の
// zenity/osascript）。変数にしてあるのはテストでスタブ化できるように
// するため: 実際のダイアログはヘッドレス実行をブロックしてしまう。
var guiErrorDialog = func(title, msg string) {
	dialog.Message("%s", msg).Title(title).Error()
}

// restoreIfMinimized は最小化されたウィンドウを復元する（切り離された
// 子プロセスは最小化状態で起動し、気づかれないことがある）。状態の検査と
// 復元はアトミックでないため、並行するユーザーの復元操作が panic を
// 起こしうる。エラー報告の途中で死ぬのではなく、それを飲み込む。
func restoreIfMinimized() {
	defer func() { _ = recover() }()
	if ebiten.IsWindowMinimized() {
		ebiten.RestoreWindow()
	}
}

// showErrorInWindow は msg をウィンドウ内のモーダルダイアログに表示する
// （dialog() 組込と同じ外観）。これによりゲームウィンドウの陰に隠れる
// ことが決してない。dialog() と同じく応答があるまでブロックする。
// 尋問中は最小化されていればウィンドウを復元し、フローティングにする:
// 切り離された GUI 子プロセスは他のウィンドウの陰や最小化状態で
// 起動しうるため、ユーザーからエラーウィンドウに気づかれないという
// 報告があった。待機中にウィンドウが閉じられると ebiten.RunGame が
// 戻り、メインスレッドがプロセスを終了する（まだ動作中のスクリプトが
// ある閉じたウィンドウは、runGUIWith の仕様どおりエラーなしと解釈される）。
func showErrorInWindow(wb *gui.WindowBackend, msg string) {
	restoreIfMinimized()
	ebiten.SetWindowFloating(true)
	defer ebiten.SetWindowFloating(false)
	_, _ = wb.Dialog(msg, "ok")
}

// reportGUIError は GUI モードの失敗を標準エラー出力とネイティブ
// ダイアログの両方に報告する。これはウィンドウが無い場合の失敗
// （解析エラー、ウィンドウ生成失敗）向けである: GUI プログラムは
// しばしば切り離されて起動される（エディタの execgui、ダブルクリック
// された windowsgui 実行ファイル）ためコンソールがなく、標準エラー
// 出力だけでは見えない。ダイアログが確実な伝達経路である。
func reportGUIError(msg string) {
	fmt.Fprintln(os.Stderr, "エラー:", msg)
	guiErrorDialog("Golash 実行エラー", msg)
}
