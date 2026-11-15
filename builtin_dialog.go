// OSネイティブの汎用ダイアログ（メッセージボックス／ファイル選択）。
//
// dialog() は GUI ウィンドウを必要としないため、CUI からも呼べる。
package main

import (
	"errors"

	"github.com/sqweek/dialog"
)

// showOSFileDialog はテストで差し替えるためのフック。
var showOSFileDialog = func(in *Interp, title, filter string, save bool) (string, error) {
	if save {
		return dlgFileBuilder(in, title, filter, "").Save()
	}
	return dlgFileBuilder(in, title, filter, "").Load()
}

func init() {
	// dialog(text [, mode])：OS のネイティブダイアログを表示する。
	// 0/1 は標準／警告メッセージ＋OK、2/3 は標準／警告＋はい・いいえ、
	// 4/5 はファイルを開く／保存。0/1 は 1、2/3 は Yes=1/No=0、
	// 4/5 は選択パス（キャンセル時は ""）を返す。
	register("dialog", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		text, err := needString("dialog", args, 0, at)
		if err != nil {
			return Null(), err
		}
		mode := int64(0)
		if len(args) == 2 {
			mode, err = needInt("dialog", args, 1, at)
			if err != nil {
				return Null(), err
			}
		}
		switch mode {
		case 0, 1, 2, 3:
			warning := mode == 1 || mode == 3
			yesNo := mode == 2 || mode == 3
			ok, err := showOSMessageBox("Golash", text, warning, yesNo)
			if err != nil {
				return Null(), rtErrf(at, "dialog：%s", err.Error())
			}
			return IntOfBool(ok), nil
		case 4, 5:
			title := "開く"
			if mode == 5 {
				title = "保存"
			}
			path, err := showOSFileDialog(in, title, text, mode == 5)
			if err != nil {
				if errors.Is(err, dialog.ErrCancelled) {
					return Str(""), nil
				}
				return Null(), rtErrf(at, "dialog：%s", err.Error())
			}
			return Str(path), nil
		default:
			return Null(), argErr("dialog", 1, at, "0 から 5 である必要があります。%d が指定されました", mode)
		}
	})
}
