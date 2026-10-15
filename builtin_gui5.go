// GUI組み込み関数（G5フォーム部品：chkbox、combox、mesbox）。
// 状態の取得・変更は objget/objset、有効・無効は objenable で行う。
// 常に登録される。GUI呼び出しには
// ウィンドウ（GUIバックエンド）が必要で、無い場合はエラーを返す。
package main

func init() {
	// chkbox(id, label [, checked])：pos() の位置にチェックボックスを配置する。
	// 大きさはフォントとラベルから自動決定される。
	register("chkbox", 2, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "chkbox")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("chkbox", args, 0, at)
		if err != nil {
			return Null(), err
		}
		label, err := needString("chkbox", args, 1, at)
		if err != nil {
			return Null(), err
		}
		checked := false
		if len(args) == 3 {
			f, err := needInt("chkbox", args, 2, at)
			if err != nil {
				return Null(), err
			}
			checked = f != 0
		}
		x, y := wb.CursorPixels()
		if err := wb.AddCheck(int(id), label, x, y, 0, 0, checked); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// combox(id, w, h, items [, sel])：pos() の位置にドロップダウンリストを配置する。
	register("combox", 4, 5, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "combox")
		if err != nil {
			return Null(), err
		}
		nums := make([]int, 3)
		for i := range nums {
			v, err := needInt("combox", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = int(v)
		}
		arr, err := needArray("combox", args, 3, at)
		if err != nil {
			return Null(), err
		}
		items := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			if e.K != KString {
				return Null(), argErr("combox", 3, at, "items は文字列である必要があります。要素 %d は %s です", i, typeNameOf(e))
			}
			items[i] = e.S
		}
		sel := -1
		if len(items) > 0 {
			sel = 0
		}
		if len(args) == 5 {
			s, err := needInt("combox", args, 4, at)
			if err != nil {
				return Null(), err
			}
			sel = int(s)
		}
		x, y := wb.CursorPixels()
		if err := wb.AddCombo(nums[0], x, y, nums[1], nums[2], items, sel); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// mesbox(id, w, h [, text])：pos() の位置に複数行テキストボックスを配置する。
	register("mesbox", 3, 4, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mesbox")
		if err != nil {
			return Null(), err
		}
		nums := make([]int, 3)
		for i := range nums {
			v, err := needInt("mesbox", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = int(v)
		}
		text := ""
		if len(args) == 4 {
			var err error
			text, err = needString("mesbox", args, 3, at)
			if err != nil {
				return Null(), err
			}
		}
		x, y := wb.CursorPixels()
		if err := wb.AddArea(nums[0], x, y, nums[1], nums[2], text); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
