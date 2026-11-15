//go:build windows

package main

import (
	"fmt"

	"github.com/TheTitanrain/w32"
)

const (
	// w32 の定数に MB_TOPMOST / MB_SETFOREGROUND が無いため自前で定義する。
	// ゲームウィンドウの背後に隠れないよう、常に前面に出す。
	mbTopmost       = 0x00040000
	mbSetForeground = 0x00010000
)

// osMessageBox は Windows の MessageBoxW を表示する。
// 標準はアイコンなし、警告は MB_ICONWARNING を使う。
func osMessageBox(title, text string, warning, yesNo bool) (bool, error) {
	flags := uint(mbTopmost | mbSetForeground)
	if warning {
		flags |= w32.MB_ICONWARNING
	}
	if yesNo {
		flags |= w32.MB_YESNO
	} else {
		flags |= w32.MB_OK
	}
	// w32.MessageBox の第2引数は本文、第3引数はキャプション。
	r := w32.MessageBox(w32.HWND(0), text, title, flags)
	if r == 0 {
		return false, fmt.Errorf("MessageBox を表示できません")
	}
	if yesNo {
		return r == w32.IDYES, nil
	}
	return true, nil
}
