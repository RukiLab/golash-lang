// GUI組み込み関数（objset/objget/objenable）。常に登録される。
// GUI呼び出しにはウィンドウ（GUIバックエンド）が必要で、無ければエラーを返す。
package main

import "strconv"

func init() {
	// objset(id, val)：ウィジェットの内容を変える。val は値そのもの
	// （変数参照ではない）で、型で動作が決まる。
	// button: 文字列/数値→表示文字列の変更
	// chkbox: 整数→チェックのオン・オフ（0=オフ、非0=オン）、文字列→表示文字列の変更
	// slider: 整数→値の変更（範囲外は丸める）
	// inputbox/msgbox: 文字列/数値→内容の変更
	// listbox/combox: 整数→選択項目の変更（-1 は未選択）、配列→項目全体の変更
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
			case KString:
				if err := wb.SetCheckLabel(int(id), v.S); err != nil {
					return Null(), rtErrf(at, "%s", err.Error())
				}
				return Null(), nil
			default:
				return Null(), argErr("objset", 1, at, "chkbox には整数（0=false、非0=true）・文字列が必要です。%s が指定されました", typeNameOf(v))
			}
		case "slider":
			if v.K != KInt {
				return Null(), argErr("objset", 1, at, "slider には整数が必要です。%s が指定されました", typeNameOf(v))
			}
			if err := wb.SetSliderValue(int(id), int(v.I)); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Null(), nil
		case "inputbox", "msgbox":
			var s string
			switch v.K {
			case KString:
				s = v.S
			case KInt:
				s = strconv.FormatInt(v.I, 10)
			case KFloat:
				s = strconv.FormatFloat(v.F, 'f', -1, 64)
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
	// button: 押下イベント（呼び出しで消費、1/0）、chkbox: チェック状態（1/0）、
	// slider: 現在値、inputbox/msgbox: 内容文字列、listbox/combox: 選択インデックス。
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
			return IntOfBool(p), nil
		case "chkbox":
			p, err := wb.Checked(int(id))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return IntOfBool(p), nil
		case "slider":
			v, err := wb.SliderValue(int(id))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Int(int64(v)), nil
		case "inputbox":
			s, err := wb.InputText(int(id))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Str(s), nil
		case "msgbox":
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

	// objenable(id): ウィジェットの有効状態（1/0）を返す。
	// objenable(id, flag): ウィジェットの有効・無効を切り替える。
	// flag は整数。0 で無効、非0 で有効。
	register("objenable", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "objenable")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("objenable", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if len(args) == 1 {
			on, err := wb.Enabled(int(id))
			if err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return IntOfBool(on), nil
		}
		on, err := needTruthy("objenable", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.SetEnabled(int(id), on); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
