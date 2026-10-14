//go:build !windows

package gui

// osIMECompositionLen は OS 側の未確定文字列長を返す。
// Windows 以外では空の変換が ebiten のチャネル経由で通知されるため
// OS 側の正解値は不要: 不明の場合は ebiten 側の値を維持する。
func osIMECompositionLen() (n int, ok bool) {
	return 0, false
}

// ourWindowHwnds はこのプロセスの可視トップレベルウィンドウを返す。
// Windows ビルドでのみ実際のウィンドウを列挙する; それ以外では
// 検出対象がない (OS 照会スモークテスト用のテストフック)。
func ourWindowHwnds() []uintptr {
	return nil
}
