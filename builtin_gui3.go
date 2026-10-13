// GUI builtins (G3 widgets + dialog). Always registered; GUI calls need
// a window (a GUI backend), otherwise they report an error.
package main

func init() {
	// button(id, label, x, y, w, h [, imgIdle [, imgHover
	// [, imgPressed [, imgDisabled]]]]): place a button. Images are
	// picload/dropload buffer ids shown as icons per state
	// (normal/hover/pressed/disabled); omitted states reuse the
	// normal image (disabled falls back to a darkened copy).
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

	// pressed(id): click since the last call (consumed).
	register("pressed", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "pressed")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("pressed", args, 0, at)
		if err != nil {
			return Null(), err
		}
		p, err := wb.Pressed(int(id))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Bool(p), nil
	})

	// inputbox(id, x, y, w, h [, text]): place a text input.
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

	// gettext(id): current input content.
	register("gettext", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gettext")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("gettext", args, 0, at)
		if err != nil {
			return Null(), err
		}
		s, err := wb.InputText(int(id))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Str(s), nil
	})

	// ime([mode]): query (no args) or set (nonzero = on) IME input.
	// Returns 1 when focused, else 0.
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

	// listbox(id, x, y, w, h, items): place a list; items is an array.
	// Items wider than the list are shortened by the backend.
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

	// listbox_set(id, items): replace the entries of an existing listbox
	// in place (widget.List.SetEntries). Unlike re-calling listbox(), this
	// keeps the same ebitenui widget, so its scroll position and focus are
	// preserved — recreating the widget every keystroke reset them and made
	// the list visibly jump ("shake"). Items are shortened to the width
	// recorded by listbox().
	register("listbox_set", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "listbox_set")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("listbox_set", args, 0, at)
		if err != nil {
			return Null(), err
		}
		arr, err := needArray("listbox_set", args, 1, at)
		if err != nil {
			return Null(), err
		}
		items := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			if e.K != KString {
				return Null(), argErr("listbox_set", 1, at, "items は文字列である必要があります。要素 %d は %s です", i, typeNameOf(e))
			}
			items[i] = e.S
		}
		if err := wb.UpdateList(int(id), items); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// listbox_sel(id, index): set a listbox's selected entry by index
	// (0-based) without firing the selected handler (programmatic select,
	// e.g. syncing the editor outline after a "go to definition").
	register("listbox_sel", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "listbox_sel")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("listbox_sel", args, 0, at)
		if err != nil {
			return Null(), err
		}
		idx, err := needInt("listbox_sel", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.SelectList(int(id), int(idx)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// selected(id): list selection index (-1 when none).
	register("selected", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "selected")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("selected", args, 0, at)
		if err != nil {
			return Null(), err
		}
		idx, err := wb.SelectedIndex(int(id))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(idx)), nil
	})

	// clrobj([id]): remove one widget, or all without arguments.
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

	// dialog(msg [, mode]): modal choice. mode ok/okcancel/yesno.
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
