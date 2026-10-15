// GUI 組込（G3 ウィジェット＋ダイアログ）。常に登録される。GUI 呼出には
// ウィンドウ（GUI バックエンド）が必要で、なければエラーを報告する。
package main

func init() {
	// button(id, label, x, y, w, h [, imgIdle [, imgHover
	// [, imgPressed [, imgDisabled]]]]): ボタンを配置する。画像は
	// picload/dropload のバッファ id で、状態ごと
	// （通常/ホバー/押下/無効）にアイコンとして表示される。
	// 省略された状態は通常の画像を再利用する（無効は暗くした複製に
	// フォールバックする）。
	register("button", 6, 10, func(in *Interp, args []Value, at Pos) (Value, error) {
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
		rect := make([]int, 4)
		for i := range rect {
			v, err := needInt("button", args, i+2, at)
			if err != nil {
				return Null(), err
			}
			rect[i] = int(v)
		}
		var imgs []int
		for i := 6; i < len(args); i++ {
			v, err := needInt("button", args, i, at)
			if err != nil {
				return Null(), err
			}
			imgs = append(imgs, int(v))
		}
		if err := wb.AddButton(int(id), label, rect[0], rect[1], rect[2], rect[3], imgs...); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// inputbox(id, x, y, w, h [, text]): テキスト入力を配置する。
	register("inputbox", 5, 6, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "inputbox")
		if err != nil {
			return Null(), err
		}
		nums := make([]int, 5)
		for i := range nums {
			v, err := needInt("inputbox", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = int(v)
		}
		text := ""
		if len(args) == 6 {
			var err error
			text, err = needString("inputbox", args, 5, at)
			if err != nil {
				return Null(), err
			}
		}
		if err := wb.AddInput(nums[0], nums[1], nums[2], nums[3], nums[4], text); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// ime([mode]): IME 入力を照会（引数なし）または設定（非ゼロ=オン）
	// する。フォーカスがあれば 1、なければ 0 を返す。
	register("ime", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "ime")
		if err != nil {
			return Null(), err
		}
		if len(args) == 1 {
			v, err := needInt("ime", args, 0, at)
			if err != nil {
				return Null(), err
			}
			return Int(int64(wb.IMESet(v != 0))), nil
		}
		return Int(int64(wb.IMEState())), nil
	})

	// listbox(id, x, y, w, h, items): リストを配置する。items は配列。
	// リスト幅より広い項目はバックエンドが短縮する。項目の差し替えは
	// objset(id, 配列)、選択は objset(id, 整数) で行う。
	register("listbox", 6, 6, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "listbox")
		if err != nil {
			return Null(), err
		}
		nums := make([]int, 5)
		for i := range nums {
			v, err := needInt("listbox", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = int(v)
		}
		arr, err := needArray("listbox", args, 5, at)
		if err != nil {
			return Null(), err
		}
		items := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			if e.K != KString {
				return Null(), argErr("listbox", 5, at, "items は文字列である必要があります。要素 %d は %s です", i, typeNameOf(e))
			}
			items[i] = e.S
		}
		if err := wb.AddList(nums[0], nums[1], nums[2], nums[3], nums[4], items); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// clrobj([id]): 1つのウィジェットを削除する。引数なしならすべて。
	register("clrobj", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "clrobj")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			wb.ClearWidgets()
			return Null(), nil
		}
		id, err := needInt("clrobj", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.RemoveWidget(int(id)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// dialog(msg [, mode]): モーダルな選択。mode は ok/okcancel/yesno。
	register("dialog", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "dialog")
		if err != nil {
			return Null(), err
		}
		msg, err := needString("dialog", args, 0, at)
		if err != nil {
			return Null(), err
		}
		mode := "ok"
		if len(args) == 2 {
			mode, err = needString("dialog", args, 1, at)
			if err != nil {
				return Null(), err
			}
		}
		v, err := wb.Dialog(msg, mode)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(v)), nil
	})
}
