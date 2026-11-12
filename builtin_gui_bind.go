// GUI変数バインド（wbind/wsync）。objget/objset の代わりに
// 内容を入れる変数を指定し、await 時に自動同期する。
// button: pressed(int 0/1, ラッチ) / chkbox: checked(int 0/1, 双方向) /
// inputbox/mesbox: text(string, 双方向) /
// listbox/combox: sel(int, 双方向) + items([string], 変数→主に同期)。
package main

import (
	"slices"
)

func boundBackend(in *Interp, at Pos, name string) (interface {
	WidgetType(int) (string, error)
	Pressed(int) (bool, error)
	Checked(int) (bool, error)
	SetChecked(int, bool) error
	InputText(int) (string, error)
	SetInputText(int, string) error
	AreaText(int) (string, error)
	SetAreaText(int, string) error
	SelectedIndex(int) (int, error)
	SelectList(int, int) error
	SelectCombo(int, int) error
	ListItems(int) ([]string, error)
	ComboItems(int) ([]string, error)
	UpdateList(int, []string) error
	UpdateCombo(int, []string) error
}, error) {
	wb, err := guiBE(in, at, name)
	if err != nil {
		return nil, err
	}
	return wb, nil
}

func ensureBindMaps(in *Interp) {
	if in.wbinds == nil {
		in.wbinds = map[int]string{}
	}
	if in.wbindItems == nil {
		in.wbindItems = map[int]string{}
	}
	if in.wlast == nil {
		in.wlast = map[int]Value{}
	}
	if in.wlastItems == nil {
		in.wlastItems = map[int][]string{}
	}
}

func checkVarName(name string, at Pos) error {
	if name == "" {
		return rtErrf(at, "変数名は空にできません")
	}
	if _, _, _, ok := parseVarRef(name); !ok {
		return rtErrf(at, "変数名 %q は不正です", name)
	}
	return nil
}

func init() {
	// wbind(id, varname:string)：ウィジェットの内容を変数へ結び付ける。
	// button: int（押下で 1 にラッチ、読んだら 0 へ戻す）/
	// chkbox: int（0=オフ、非0=オン）/ inputbox,mesbox: string /
	// listbox,combox: int（選択）。
	// 結び付け時は変数の値がウィジェットへ送られる（変数が正）。
	register("wbind", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := boundBackend(in, at, "wbind")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("wbind", args, 0, at)
		if err != nil {
			return Null(), err
		}
		vname, err := needString("wbind", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := checkVarName(vname, at); err != nil {
			return Null(), err
		}
		kind, err := wb.WidgetType(int(id))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		cur, ok := in.getBoundVar(vname)
		if !ok {
			return Null(), rtErrf(at, "wbind：未定義の変数 %q です", vname)
		}
		ensureBindMaps(in)
		switch kind {
		case "button":
			if cur.K != KInt {
				return Null(), argErr("wbind", 1, at, "button には整数（0=false、非0=true）の変数が必要です。%s が指定されました", typeNameOf(cur))
			}
			in.wbinds[int(id)] = vname
			in.wlast[int(id)] = cur
		case "chkbox":
			if cur.K != KInt {
				return Null(), argErr("wbind", 1, at, "chkbox には整数（0=false、非0=true）の変数が必要です。%s が指定されました", typeNameOf(cur))
			}
			if err := wb.SetChecked(int(id), cur.I != 0); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			in.wbinds[int(id)] = vname
			in.wlast[int(id)] = cur
		case "inputbox":
			if cur.K != KString {
				return Null(), argErr("wbind", 1, at, "inputbox には文字列の変数が必要です。%s が指定されました", typeNameOf(cur))
			}
			if err := wb.SetInputText(int(id), cur.S); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			in.wbinds[int(id)] = vname
			in.wlast[int(id)] = cur
		case "mesbox":
			if cur.K != KString {
				return Null(), argErr("wbind", 1, at, "mesbox には文字列の変数が必要です。%s が指定されました", typeNameOf(cur))
			}
			if err := wb.SetAreaText(int(id), cur.S); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			in.wbinds[int(id)] = vname
			in.wlast[int(id)] = cur
		case "listbox", "combox":
			if cur.K != KInt {
				return Null(), argErr("wbind", 1, at, "%s には整数の変数（選択）が必要です。%s が指定されました", kind, typeNameOf(cur))
			}
			var n int
			if kind == "listbox" {
				items, err := wb.ListItems(int(id))
				if err != nil {
					return Null(), rtErrf(at, "%s", err.Error())
				}
				n = len(items)
			} else {
				items, err := wb.ComboItems(int(id))
				if err != nil {
					return Null(), rtErrf(at, "%s", err.Error())
				}
				n = len(items)
			}
			if cur.I < -1 || cur.I >= int64(n) {
				return Null(), argErr("wbind", 1, at, "選択 %d は範囲外です（%d 項目）", cur.I, n)
			}
			var setErr error
			if kind == "listbox" {
				setErr = wb.SelectList(int(id), int(cur.I))
			} else {
				setErr = wb.SelectCombo(int(id), int(cur.I))
			}
			if setErr != nil {
				return Null(), rtErrf(at, "%s", setErr.Error())
			}
			in.wbinds[int(id)] = vname
			in.wlast[int(id)] = cur
		default:
			return Null(), rtErrf(at, "wbind：不明なウィジェット %d です", int(id))
		}
		return Null(), nil
	})

	// wbind_items(id, varname:string)：listbox/combox の項目配列を変数へ結び付ける。
	// 変数は文字列だけの配列であること。結び付け時は変数がウィジェットへ送られる。
	register("wbind_items", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := boundBackend(in, at, "wbind_items")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("wbind_items", args, 0, at)
		if err != nil {
			return Null(), err
		}
		vname, err := needString("wbind_items", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := checkVarName(vname, at); err != nil {
			return Null(), err
		}
		if _, _, hasIdx, _ := parseVarRef(vname); hasIdx {
			return Null(), argErr("wbind_items", 1, at, "配列全体の変数が必要です。要素指定はできません")
		}
		kind, err := wb.WidgetType(int(id))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		if kind != "listbox" && kind != "combox" {
			return Null(), argErr("wbind_items", 0, at, "listbox か combox が必要です。%s です", kind)
		}
		cur, ok := in.getBoundVar(vname)
		if !ok {
			return Null(), rtErrf(at, "wbind_items：未定義の変数 %q です", vname)
		}
		if cur.K != KArray {
			return Null(), argErr("wbind_items", 1, at, "配列の変数が必要です。%s が指定されました", typeNameOf(cur))
		}
		items := make([]string, len(cur.Arr.Elems))
		for i, e := range cur.Arr.Elems {
			if e.K != KString {
				return Null(), argErr("wbind_items", 1, at, "項目は文字列である必要があります。要素 %d は %s です", i, typeNameOf(e))
			}
			items[i] = e.S
		}
		var setErr error
		if kind == "listbox" {
			setErr = wb.UpdateList(int(id), items)
		} else {
			setErr = wb.UpdateCombo(int(id), items)
		}
		if setErr != nil {
			return Null(), rtErrf(at, "%s", setErr.Error())
		}
		ensureBindMaps(in)
		in.wbindItems[int(id)] = vname
		in.wlastItems[int(id)] = append([]string(nil), items...)
		return Null(), nil
	})

	// wunbind(id)：結び付けを外す（主要値・項目とも）。
	register("wunbind", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		id, err := needInt("wunbind", args, 0, at)
		if err != nil {
			return Null(), err
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

	// wsync()：結び付けた変数とウィジェットを両方向に同期する。
	// 変更検出で上書きを避ける：ウィジェット側が前回から変われば変数へ、
	// そうでなく変数側が変わればウィジェットへ。button は押下で 1 に
	// ラッチし、自動では 0 に戻さない（読んだ側が 0 へ戻す）。
	register("wsync", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		if err := syncBoundWidgets(in, at); err != nil {
			return Null(), err
		}
		return Null(), nil
	})
}

// syncBoundWidgets は wbind/wbind_items の全件を同期する。
// 項目を先に、選択・内容を後に扱う（項目縮小で選択が無効化された場合に
// 選択側が -1 を変数へ伝えるため）。
func syncBoundWidgets(in *Interp, at Pos) error {
	if len(in.wbinds) == 0 && len(in.wbindItems) == 0 {
		return nil
	}
	wb, err := boundBackend(in, at, "wsync")
	if err != nil {
		return err
	}
	// 項目（listbox/combox）。
	for id, vname := range in.wbindItems {
		kind, err := wb.WidgetType(id)
		if err != nil {
			continue
		}
		if kind != "listbox" && kind != "combox" {
			continue
		}
		var witems []string
		if kind == "listbox" {
			witems, err = wb.ListItems(id)
		} else {
			witems, err = wb.ComboItems(id)
		}
		if err != nil {
			continue
		}
		cur, ok := in.getBoundVar(vname)
		if !ok {
			return rtErrf(at, "wsync：未定義の変数 %q です", vname)
		}
		if cur.K != KArray {
			return rtErrf(at, "wsync：%q は配列である必要があります。%s です", vname, typeNameOf(cur))
		}
		velems := make([]string, len(cur.Arr.Elems))
		for i, e := range cur.Arr.Elems {
			if e.K != KString {
				return rtErrf(at, "wsync：%q の要素 %d は文字列である必要があります。%s です", vname, i, typeNameOf(e))
			}
			velems[i] = e.S
		}
		last := in.wlastItems[id]
		if !slices.Equal(witems, last) {
			// ウィジェット側が変化（objset 併用時など）：変数へ。
			elems := make([]Value, len(witems))
			for i, s := range witems {
				elems[i] = Str(s)
			}
			if err := in.setBoundVar(vname, ArrayOf(elems), at); err != nil {
				return err
			}
			in.wlastItems[id] = append([]string(nil), witems...)
		} else if !slices.Equal(velems, last) {
			// 変数側が変化：ウィジェットへ。
			var setErr error
			if kind == "listbox" {
				setErr = wb.UpdateList(id, velems)
			} else {
				setErr = wb.UpdateCombo(id, velems)
			}
			if setErr != nil {
				return rtErrf(at, "%s", setErr.Error())
			}
			in.wlastItems[id] = append([]string(nil), velems...)
		}
	}
	// 主要値。
	for id, vname := range in.wbinds {
		kind, err := wb.WidgetType(id)
		if err != nil {
			continue
		}
		switch kind {
		case "button":
			p, err := wb.Pressed(id)
			if err != nil {
				continue
			}
			if p {
				if err := in.setBoundVar(vname, IntOfBool(true), at); err != nil {
					return err
				}
				if in.wlast == nil {
					in.wlast = map[int]Value{}
				}
				in.wlast[id] = IntOfBool(true)
			} else {
				cur, ok := in.getBoundVar(vname)
				if !ok {
					return rtErrf(at, "wsync：未定義の変数 %q です", vname)
				}
				if cur.K != KInt {
					return rtErrf(at, "wsync：%q は整数（0=false、非0=true）である必要があります。%s です", vname, typeNameOf(cur))
				}
				last, ok := in.wlast[id]
				if !ok {
					if in.wlast == nil {
						in.wlast = map[int]Value{}
					}
					in.wlast[id] = cur
					continue
				}
				if last.K == KInt && (cur.I != 0) != (last.I != 0) {
					// 読んだ側が 0 へ戻した：受け入れる。
					in.wlast[id] = cur
				}
			}
		case "chkbox":
			wv, err := wb.Checked(id)
			if err != nil {
				continue
			}
			cur, ok := in.getBoundVar(vname)
			if !ok {
				return rtErrf(at, "wsync：未定義の変数 %q です", vname)
			}
			if cur.K != KInt {
				return rtErrf(at, "wsync：%q は整数（0=false、非0=true）である必要があります。%s です", vname, typeNameOf(cur))
			}
			last, ok := in.wlast[id]
			if !ok {
				if in.wlast == nil {
					in.wlast = map[int]Value{}
				}
				in.wlast[id] = cur
				continue
			}
			if last.K != KInt {
				in.wlast[id] = cur
				continue
			}
			if wv != (last.I != 0) {
				if err := in.setBoundVar(vname, IntOfBool(wv), at); err != nil {
					return err
				}
				in.wlast[id] = IntOfBool(wv)
			} else if (cur.I != 0) != (last.I != 0) {
				if err := wb.SetChecked(id, cur.I != 0); err != nil {
					return rtErrf(at, "%s", err.Error())
				}
				in.wlast[id] = cur
			}
		case "inputbox", "mesbox":
			var wv string
			if kind == "inputbox" {
				wv, err = wb.InputText(id)
			} else {
				wv, err = wb.AreaText(id)
			}
			if err != nil {
				continue
			}
			cur, ok := in.getBoundVar(vname)
			if !ok {
				return rtErrf(at, "wsync：未定義の変数 %q です", vname)
			}
			if cur.K != KString {
				return rtErrf(at, "wsync：%q は文字列である必要があります。%s です", vname, typeNameOf(cur))
			}
			last, ok := in.wlast[id]
			if !ok {
				if in.wlast == nil {
					in.wlast = map[int]Value{}
				}
				in.wlast[id] = cur
				continue
			}
			if last.K != KString {
				in.wlast[id] = cur
				continue
			}
			if wv != last.S {
				if err := in.setBoundVar(vname, Str(wv), at); err != nil {
					return err
				}
				in.wlast[id] = Str(wv)
			} else if cur.S != last.S {
				var setErr error
				if kind == "inputbox" {
					setErr = wb.SetInputText(id, cur.S)
				} else {
					setErr = wb.SetAreaText(id, cur.S)
				}
				if setErr != nil {
					return rtErrf(at, "%s", setErr.Error())
				}
				in.wlast[id] = cur
			}
		case "listbox", "combox":
			wv, err := wb.SelectedIndex(id)
			if err != nil {
				continue
			}
			cur, ok := in.getBoundVar(vname)
			if !ok {
				return rtErrf(at, "wsync：未定義の変数 %q です", vname)
			}
			if cur.K != KInt {
				return rtErrf(at, "wsync：%q は整数である必要があります。%s です", vname, typeNameOf(cur))
			}
			last, ok := in.wlast[id]
			if !ok {
				if in.wlast == nil {
					in.wlast = map[int]Value{}
				}
				in.wlast[id] = cur
				continue
			}
			if last.K != KInt {
				in.wlast[id] = cur
				continue
			}
			if int64(wv) != last.I {
				if err := in.setBoundVar(vname, Int(int64(wv)), at); err != nil {
					return err
				}
				in.wlast[id] = Int(int64(wv))
			} else if cur.I != last.I {
				var n int
				if kind == "listbox" {
					items, err := wb.ListItems(id)
					if err != nil {
						continue
					}
					n = len(items)
				} else {
					items, err := wb.ComboItems(id)
					if err != nil {
						continue
					}
					n = len(items)
				}
				if cur.I < -1 || cur.I >= int64(n) {
					return rtErrf(at, "wsync：%q の選択 %d は範囲外です（%d 項目）", vname, cur.I, n)
				}
				var setErr error
				if kind == "listbox" {
					setErr = wb.SelectList(id, int(cur.I))
				} else {
					setErr = wb.SelectCombo(id, int(cur.I))
				}
				if setErr != nil {
					return rtErrf(at, "%s", setErr.Error())
				}
				in.wlast[id] = cur
			}
		}
	}
	return nil
}
