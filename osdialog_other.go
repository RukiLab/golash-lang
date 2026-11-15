//go:build !windows && !darwin && !linux

package main

import "github.com/sqweek/dialog"

// osMessageBox は sqweek/dialog を使うフォールバック。
// Windows/macOS/Linux 以外では警告アイコンの指定ができないため、
// 警告はエラーアイコンで代用する。
func osMessageBox(title, text string, warning, yesNo bool) (bool, error) {
	b := dialog.Message("%s", text).Title(title)
	if yesNo {
		return b.YesNo(), nil
	}
	if warning {
		b.Error()
	} else {
		b.Info()
	}
	return true, nil
}
