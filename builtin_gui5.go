// GUI組み込み関数（G5フォーム部品：chkbox、combox、mesbox、objprm
// およびchecked/getstr取得系）。常に登録される。GUI呼び出しには
// ウィンドウ（GUIバックエンド）が必要で、無い場合はエラーを返す。
package main

func init() {
	// chkbox(id, label, x, y [, checked])：チェックボックスを配置する。
	// 大きさはフォントとラベルから自動決定される。w/hは廃止された。
	register("chkbox", 4, 5, func(in *Interp, args []Value, at Pos) (Value, error) {
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
		xy := make([]int, 2)
		for i := range xy {
			v, err := needInt("chkbox", args, i+2, at)
			if err != nil {
				return Null(), err
			}
			xy[i] = int(v)
		}
		checked := false
		if len(args) == 5 {
			f, err := needInt("chkbox", args, 4, at)
			if err != nil {
				return Null(), err
			}
			checked = f != 0
		}
		if err := wb.AddCheck(int(id), label, xy[0], xy[1], 0, 0, checked); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// checked(id)：チェックボックスの状態。
	register("checked", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "checked")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("checked", args, 0, at)
		if err != nil {
			return Null(), err
		}
		p, err := wb.Checked(int(id))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Bool(p), nil
	})

	// combox(id, x, y, w, h, items [, sel])：ドロップダウンリストを配置する。
	register("combox", 6, 7, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "combox")
		if err != nil {
			return Null(), err
		}
		nums := make([]int, 5)
		for i := range nums {
			v, err := needInt("combox", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = int(v)
		}
		arr, err := needArray("combox", args, 5, at)
		if err != nil {
			return Null(), err
		}
		items := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			if e.K != KString {
				return Null(), argErr("combox", 5, at, "items は文字列である必要があります。要素 %d は %s です", i, typeNameOf(e))
			}
			items[i] = e.S
		}
		sel := -1
		if len(items) > 0 {
			sel = 0
		}
		if len(args) == 7 {
			s, err := needInt("combox", args, 6, at)
			if err != nil {
				return Null(), err
			}
			sel = int(s)
		}
		if err := wb.AddCombo(nums[0], nums[1], nums[2], nums[3], nums[4], items, sel); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// mesbox(id, x, y, w, h [, text])：複数行テキストボックスを配置する。
	register("mesbox", 5, 6, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mesbox")
		if err != nil {
			return Null(), err
		}
		nums := make([]int, 5)
		for i := range nums {
			v, err := needInt("mesbox", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = int(v)
		}
		text := ""
		if len(args) == 6 {
			var err error
			text, err = needString("mesbox", args, 5, at)
			if err != nil {
				return Null(), err
			}
		}
		if err := wb.AddArea(nums[0], nums[1], nums[2], nums[3], nums[4], text); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// getstr(id)：複数行ボックスの内容。
	register("getstr", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "getstr")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("getstr", args, 0, at)
		if err != nil {
			return Null(), err
		}
		s, err := wb.AreaText(int(id))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Str(s), nil
	})

	// setstr(id, s)：複数行ボックスの内容を置き換える。mesboxは
	// 表示専用（ebitenuiのTextAreaには編集機能がない）なので、getstr()と
	// 組み合わせてプログラム駆動のログ表示用として使う。
	register("setstr", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "setstr")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("setstr", args, 0, at)
		if err != nil {
			return Null(), err
		}
		s, err := needString("setstr", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.SetAreaText(int(id), s); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// objprm(id, key, value)：部品のパラメータ。キー「enable」で
	// 操作可否を切り替える（値0/1）。部品のテーマは固定
	//（Windows標準：白地・灰枠・黒文字）のため、色系の
	// キーは存在しない。
	register("objprm", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "objprm")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("objprm", args, 0, at)
		if err != nil {
			return Null(), err
		}
		key, err := needString("objprm", args, 1, at)
		if err != nil {
			return Null(), err
		}
		v, err := needInt("objprm", args, 2, at)
		if err != nil {
			return Null(), err
		}
		switch key {
		case "enable":
			if err := wb.SetEnabled(int(id), v != 0); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Null(), nil
		default:
			return Null(), rtErrf(at, "objprm：不明なキー %q です（\"enable\" が必要です）", key)
		}
	})
}
