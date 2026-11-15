//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// osaMessageBoxScript は osascript に渡す AppleScript。
// 本文・タイトルは環境変数で渡し、引用符やバックスラッシュの
// エスケープを Go 側で組み立てない（インジェクション対策）。
const osaMessageBoxScript = `
set msgText to system attribute "GOLASH_MSG_TEXT"
set msgTitle to system attribute "GOLASH_MSG_TITLE"
set isWarning to system attribute "GOLASH_MSG_WARNING"
set isYesNo to system attribute "GOLASH_MSG_YESNO"
if isYesNo is "1" then
    set buttonList to {"No", "Yes"}
    set defaultButton to "Yes"
else
    set buttonList to {"OK"}
    set defaultButton to "OK"
end if
if isWarning is "1" then
    set r to display dialog msgText with title msgTitle buttons buttonList default button defaultButton with icon caution
else
    set r to display dialog msgText with title msgTitle buttons buttonList default button defaultButton
end if
return button returned of r
`

// osMessageBox は macOS の標準ダイアログを osascript 経由で表示する。
// 警告は caution アイコン、標準はアイコン指定なしにする。
func osMessageBox(title, text string, warning, yesNo bool) (bool, error) {
	cmd := exec.Command("osascript", "-e", osaMessageBoxScript)
	cmd.Env = append(os.Environ(),
		"GOLASH_MSG_TEXT="+text,
		"GOLASH_MSG_TITLE="+title,
		"GOLASH_MSG_WARNING="+boolArg(warning),
		"GOLASH_MSG_YESNO="+boolArg(yesNo),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		// キャンセル（Esc / 閉じる）はエラーではなく「いいえ／閉じた」扱い。
		// OK のみのダイアログは閉じた時点で確認済みとみなす。
		if strings.Contains(msg, "User canceled") || strings.Contains(msg, "(-128)") {
			if yesNo {
				return false, nil
			}
			return true, nil
		}
		return false, fmt.Errorf("osascript：%v: %s", err, msg)
	}
	s := strings.TrimSpace(string(out))
	return strings.Contains(s, "Yes") || strings.Contains(s, "OK"), nil
}

func boolArg(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
