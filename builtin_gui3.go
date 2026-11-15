// GUI 組込（G3 ウィジェット）。常に登録される。GUI 呼出には
// ウィンドウ（GUI バックエンド）が必要で、なければエラーを報告する。
// OS ネイティブの dialog() は builtin_dialog.go にある。
package main

func init() {
	// button(id, w, h, label): pos() の位置にボタンを配置する。
	// 配置後は自動で改行する (x=0, y+h)。
	register("button", 4, 4, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "button")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("button", args, 0, at)
		if err != nil {
			return Null(), err
		}
		size := make([]int, 2)
		for i := range size {
			v, err := needInt("button", args, i+1, at)
			if err != nil {
				return Null(), err
			}
			size[i] = int(v)
		}
		label, err := needString("button", args, 3, at)
		if err != nil {
			return Null(), err
		}
		x, y := wb.CursorPixels()
		if err := wb.AddButton(int(id), label, x, y, size[0], size[1]); err != nil {
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

	// objdel([id]): 1つのウィジェットを削除する。引数なしならすべて。
	register("objdel", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "objdel")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			wb.ClearWidgets()
			return Null(), nil
		}
		id, err := needInt("objdel", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.RemoveWidget(int(id)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

}
