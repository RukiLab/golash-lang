// GUI 組込（G3 ウィジェット＋ダイアログ）。常に登録される。GUI 呼出には
// ウィンドウ（GUI バックエンド）が必要で、なければエラーを報告する。
package main

import (
	"errors"

	"github.com/sqweek/dialog"
	"gsh/gui"
)

func init() {
	// button(id, label, w, h [, buf, sx, sy
	// [, buf, sx, sy [, buf, sx, sy [, buf, sx, sy]]]]): pos() の位置に
	// ボタンを配置する。配置後は自動で改行する (x=0, y+h)。画像は最大4状態 (通常/ホバー/押下/無効) で、各状態は
	// (buf, sx, sy) の3つ組で指定する。buf は picload/dropload のバッファ id
	// (0 は空き)、sx/sy は切出し左上。切出しサイズはボタンの w/h で原寸コピー
	// する (拡縮しない)。省略されたホバー・押下は通常画像、省略された無効は
	// 通常画像の減光コピーになる。
	register("button", 4, 16, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "button")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("button", args, 0, at)
		if err != nil {
			return Null(), err
		}
		label, err := needString("button", args, 1, at)
		if err != nil {
			return Null(), err
		}
		size := make([]int, 2)
		for i := range size {
			v, err := needInt("button", args, i+2, at)
			if err != nil {
				return Null(), err
			}
			size[i] = int(v)
		}
		if (len(args)-4)%3 != 0 {
			return Null(), argErr("button", len(args)-1, at, "画像は (buf, sx, sy) の組で指定してください。引数が %d 個余分です", len(args)-4)
		}
		var imgs []gui.ButtonImage
		for i := 4; i < len(args); i += 3 {
			vals := make([]int, 3)
			for k := 0; k < 3; k++ {
				v, err := needInt("button", args, i+k, at)
				if err != nil {
					return Null(), err
				}
				vals[k] = int(v)
			}
			imgs = append(imgs, gui.ButtonImage{Buf: vals[0], SX: vals[1], SY: vals[2]})
		}
		x, y := wb.CursorPixels()
		if err := wb.AddButton(int(id), label, x, y, size[0], size[1], imgs...); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// inputbox(id, w, h [, text]): pos() の位置にテキスト入力を配置する。配置後は自動で改行する (x=0, y+h)。
	register("inputbox", 3, 4, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "inputbox")
		if err != nil {
			return Null(), err
		}
		nums := make([]int, 3)
		for i := range nums {
			v, err := needInt("inputbox", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = int(v)
		}
		text := ""
		if len(args) == 4 {
			var err error
			text, err = needString("inputbox", args, 3, at)
			if err != nil {
				return Null(), err
			}
		}
		x, y := wb.CursorPixels()
		if err := wb.AddInput(nums[0], x, y, nums[1], nums[2], text); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// ime([v]): IME 入力を照会（引数なし）または設定（0=オフ、
	// 非0=オン）する。フォーカスがあれば 1、なければ 0 を返す。
	register("ime", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "ime")
		if err != nil {
			return Null(), err
		}
		if len(args) == 1 {
			v, err := needTruthy("ime", args, 0, at)
			if err != nil {
				return Null(), err
			}
			return IntOfBool(wb.IMESet(v)), nil
		}
		return IntOfBool(wb.IMEState()), nil
	})

	// listbox(id, w, h, items): pos() の位置にリストを配置する。配置後は自動で改行する (x=0, y+h)。
	// items は配列。リスト幅より広い項目はバックエンドが短縮する。
	// 項目の差し替えは objset(id, 配列)、選択は objset(id, 整数) で行う。
	register("listbox", 4, 4, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "listbox")
		if err != nil {
			return Null(), err
		}
		nums := make([]int, 3)
		for i := range nums {
			v, err := needInt("listbox", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = int(v)
		}
		arr, err := needArray("listbox", args, 3, at)
		if err != nil {
			return Null(), err
		}
		items := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			if e.K != KString {
				return Null(), argErr("listbox", 3, at, "items は文字列である必要があります。要素 %d は %s です", i, typeNameOf(e))
			}
			items[i] = e.S
		}
		x, y := wb.CursorPixels()
		if err := wb.AddList(nums[0], x, y, nums[1], nums[2], items); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// clrobj([id]): 1つのウィジェットを削除する。引数なしならすべて。
	// 結び付け（wbind/wbind_items）も同時に外す。
	register("clrobj", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "clrobj")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			wb.ClearWidgets()
			in.wbinds = map[int]string{}
			in.wbindItems = map[int]string{}
			in.wlast = map[int]Value{}
			in.wlastItems = map[int][]string{}
			return Null(), nil
		}
		id, err := needInt("clrobj", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.RemoveWidget(int(id)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		if in.wbinds != nil {
			delete(in.wbinds, int(id))
		}
		if in.wbindItems != nil {
			delete(in.wbindItems, int(id))
		}
		if in.wlast != nil {
			delete(in.wlast, int(id))
		}
		if in.wlastItems != nil {
			delete(in.wlastItems, int(id))
		}
		return Null(), nil
	})

	// dialog(text [, mode]): mode は 0=OK、1=はい・いいえ、3=開く、4=保存（省略時は 0）。
	// 0/1 は選択結果を 1/0（OK=1、Yes=1/No=0）で、3/4 は OS のファイルダイアログを開いて
	// 選ばれたパスを文字列で返す（キャンセル時は ""）。3/4 は CUI でも動く。
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
		case 0, 1:
			wb, err := guiBE(in, at, "dialog")
			if err != nil {
				return Null(), err
			}
			v, err := wb.Dialog(text, int(mode))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return IntOfBool(v), nil
		case 3, 4:
			// text は「名前|*.png;*.jpg」形式のフィルタ（"" なら無指定）。
			var path string
			if mode == 3 {
				path, err = dlgFileBuilder(in, "開く", text, "").Load()
			} else {
				path, err = dlgFileBuilder(in, "保存", text, "").Save()
			}
			if err != nil {
				if errors.Is(err, dialog.ErrCancelled) {
					return Str(""), nil
				}
				return Null(), rtErrf(at, "dialog：%s", err.Error())
			}
			return Str(path), nil
		default:
			return Null(), argErr("dialog", 1, at, "0・1・3・4 である必要があります。%d が指定されました", mode)
		}
	})
}
