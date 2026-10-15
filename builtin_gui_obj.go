// GUI組み込み関数（objset/objget/objenable）。常に登録される。
// GUI呼び出しにはウィンドウ（GUIバックエンド）が必要で、無ければエラーを返す。
package main

import "strconv"

func init() {
	// objset(id, val)：ウィジェットの内容を変える。val の型で動作が決まる。
	// button: 文字列/数値→表示文字列の変更
	// chkbox: 整数/真偽値→チェックのオン・オフ、文字列→表示文字列の変更
	// inputbox/mesbox: 文字列/数値→内容の変更
	// listbox/combox: 整数→選択項目の変更、配列→項目全体の変更
	register("objset", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "objset")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("objset", args, 0, at)
		if err != nil {
			return Null(), err
		}
		kind, err := wb.WidgetType(int(id))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		v := args[1]
		switch kind {
		case "button":
			var label string
			switch v.K {
			case KString:
				label = v.S
			case KInt:
				label = strconv.FormatInt(v.I, 10)
			case KFloat:
				label = strconv.FormatFloat(v.F, 'f', -1, 64)
			default:
				return Null(), argErr("objset", 1, at, "button には文字列か数値が必要です。%s が指定されました", typeNameOf(v))
			}
			if err := wb.SetButtonLabel(int(id), label); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Null(), nil
		case "chkbox":
			switch v.K {
			case KInt:
				if err := wb.SetChecked(int(id), v.I != 0); err != nil {
					return Null(), rtErrf(at, "%s", err.Error())
				}
				return Null(), nil
			case KBool:
				if err := wb.SetChecked(int(id), v.B); err != nil {
					return Null(), rtErrf(at, "%s", err.Error())
				}
				return Null(), nil
			case KFloat:
				if err := wb.SetChecked(int(id), v.F != 0); err != nil {
					return Null(), rtErrf(at, "%s", err.Error())
				}
				return Null(), nil
			case KString:
				if err := wb.SetCheckLabel(int(id), v.S); err != nil {
					return Null(), rtErrf(at, "%s", err.Error())
				}
				return Null(), nil
			default:
				return Null(), argErr("objset", 1, at, "chkbox には整数・真偽値・文字列が必要です。%s が指定されました", typeNameOf(v))
			}
		case "inputbox", "mesbox":
			var s string
			switch v.K {
			case KString:
				s = v.S
			case KInt:
				s = strconv.FormatInt(v.I, 10)
			case KFloat:
				s = strconv.FormatFloat(v.F, 'f', -1, 64)
			case KBool:
				if v.B {
					s = "1"
				} else {
					s = "0"
				}
			default:
				return Null(), argErr("objset", 1, at, "%s には文字列か数値が必要です。%s が指定されました", kind, typeNameOf(v))
			}
			var setErr error
			if kind == "inputbox" {
				setErr = wb.SetInputText(int(id), s)
			} else {
				setErr = wb.SetAreaText(int(id), s)
			}
			if setErr != nil {
				return Null(), rtErrf(at, "%s", setErr.Error())
			}
			return Null(), nil
		case "listbox", "combox":
			switch v.K {
			case KInt:
				var setErr error
				if kind == "listbox" {
					setErr = wb.SelectList(int(id), int(v.I))
				} else {
					setErr = wb.SelectCombo(int(id), int(v.I))
				}
				if setErr != nil {
					return Null(), rtErrf(at, "%s", setErr.Error())
				}
				return Null(), nil
			case KArray:
				items := make([]string, len(v.Arr.Elems))
				for i, e := range v.Arr.Elems {
					if e.K != KString {
						return Null(), argErr("objset", 1, at, "項目は文字列である必要があります。要素 %d は %s です", i, typeNameOf(e))
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
				return Null(), nil
			default:
				return Null(), argErr("objset", 1, at, "%s には整数か配列が必要です。%s が指定されました", kind, typeNameOf(v))
			}
		}
		return Null(), rtErrf(at, "objset：不明なウィジェット %d です", int(id))
	})

	// objget(id)：ウィジェットの内容を返す。
	// button: 押下イベント（呼び出しで消費）、chkbox: チェック状態、
	// inputbox/mesbox: 内容文字列、listbox/combox: 選択インデックス。
	register("objget", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "objget")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("objget", args, 0, at)
		if err != nil {
			return Null(), err
		}
		kind, err := wb.WidgetType(int(id))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		switch kind {
		case "button":
			p, err := wb.Pressed(int(id))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Bool(p), nil
		case "chkbox":
			p, err := wb.Checked(int(id))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Bool(p), nil
		case "inputbox":
			s, err := wb.InputText(int(id))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Str(s), nil
		case "mesbox":
			s, err := wb.AreaText(int(id))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Str(s), nil
		case "listbox", "combox":
			idx, err := wb.SelectedIndex(int(id))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Int(int64(idx)), nil
		}
		return Null(), rtErrf(at, "objget：不明なウィジェット %d です", int(id))
	})

	// objenable(id, flag)：ウィジェットの有効・無効を切り替える。
	// flag は 0 で無効、0 以外で有効。真偽値も受け付ける。
	register("objenable", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "objenable")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("objenable", args, 0, at)
		if err != nil {
			return Null(), err
		}
		var on bool
		switch args[1].K {
		case KInt:
			on = args[1].I != 0
		case KBool:
			on = args[1].B
		default:
			return Null(), argErr("objenable", 1, at, "整数か真偽値が必要です。%s が指定されました", typeNameOf(args[1]))
		}
		if err := wb.SetEnabled(int(id), on); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
