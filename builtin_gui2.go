// GUI builtins (G2 input + audio). Always registered; GUI calls need a
// window (a GUI backend), otherwise they report an error.
package main

import (
	"gsh/gui"
)

func init() {
	// getkey(code): whether the key is held. Codes follow Windows
	// virtual keys: 8 backspace, 9 tab, 13 enter, 16 shift, 17 ctrl,
	// 18 alt, 27 esc, 32 space, 33-36 pageup/pagedown/end/home, 37-40
	// arrows, 45 insert, 46 delete, 48-57 digits, 65-90 A-Z, 112-123
	// F1-F12, 186-192/219-222 punctuation.
	register("getkey", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "getkey")
		if err != nil {
			return Null(), err
		}
		code, err := needInt("getkey", args, 0, at)
		if err != nil {
			return Null(), err
		}
		down, ok := wb.KeyDown(int(code))
		if !ok {
			return Null(), argErr("getkey", 0, at, "不明なキーコード %d です", code)
		}
		return Bool(down), nil
	})

	// mousex()/mousey(): cursor position in window pixels.
	register("mousex", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mousex")
		if err != nil {
			return Null(), err
		}
		x, _ := wb.MousePos()
		return Int(int64(x)), nil
	})
	register("mousey", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mousey")
		if err != nil {
			return Null(), err
		}
		_, y := wb.MousePos()
		return Int(int64(y)), nil
	})

	// clicked([btn]): click since the last call (consumed).
	// btn is 0 left (default), 1 middle, 2 right.
	register("clicked", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "clicked")
		if err != nil {
			return Null(), err
		}
		btn := int64(0)
		if len(args) == 1 {
			var err error
			btn, err = needInt("clicked", args, 0, at)
			if err != nil {
				return Null(), err
			}
		}
		c, err := wb.ClickedButton(int(btn))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Bool(c), nil
	})

	// mousedown([btn]): whether the button is held right now (0 left,
	// 1 middle, 2 right). Never consumed, unlike clicked(): latch it
	// with a press to implement dragging (scrollbars, sliders).
	register("mousedown", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mousedown")
		if err != nil {
			return Null(), err
		}
		btn := int64(0)
		if len(args) == 1 {
			btn, err = needInt("mousedown", args, 0, at)
			if err != nil {
				return Null(), err
			}
		}
		d, err := wb.MouseDown(int(btn))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Bool(d), nil
	})

	// cursor([v]): query (no args) or set (nonzero = on) IME input.
	register("cursor", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "cursor")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			return Bool(wb.CursorVisible()), nil
		}
		v, err := needInt("cursor", args, 0, at)
		if err != nil {
			return Null(), err
		}
		wb.SetCursorVisible(v != 0)
		return Null(), nil
	})

	// mousewheel(): vertical wheel detents since the last call
	// (consumed; positive is up/away).
	register("mousewheel", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mousewheel")
		if err != nil {
			return Null(), err
		}
		return Int(int64(wb.MouseWheel())), nil
	})

	// padcount(): connected gamepads.
	register("padcount", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "padcount")
		if err != nil {
			return Null(), err
		}
		return Int(int64(wb.PadCount())), nil
	})

	// padbtn(id, btn): standard-layout button (0-16).
	register("padbtn", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "padbtn")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("padbtn", args, 0, at)
		if err != nil {
			return Null(), err
		}
		btn, err := needInt("padbtn", args, 1, at)
		if err != nil {
			return Null(), err
		}
		down, err := wb.PadButton(int(id), int(btn))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Bool(down), nil
	})

	// padaxis(id, axis): stick axis 0-3 in -1..1.
	register("padaxis", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "padaxis")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("padaxis", args, 0, at)
		if err != nil {
			return Null(), err
		}
		axis, err := needInt("padaxis", args, 1, at)
		if err != nil {
			return Null(), err
		}
		v, err := wb.PadAxis(int(id), int(axis))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Float(v), nil
	})

	// padname(id): gamepad name.
	register("padname", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "padname")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("padname", args, 0, at)
		if err != nil {
			return Null(), err
		}
		name, err := wb.PadName(int(id))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Str(name), nil
	})

	// touchcount(): active touches.
	register("touchcount", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "touchcount")
		if err != nil {
			return Null(), err
		}
		return Int(int64(wb.TouchCount())), nil
	})

	// touchx(i)/touchy(i): i-th touch position (window pixels).
	register("touchx", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "touchx")
		if err != nil {
			return Null(), err
		}
		i, err := needInt("touchx", args, 0, at)
		if err != nil {
			return Null(), err
		}
		x, _, err := wb.TouchPos(int(i))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(x)), nil
	})
	register("touchy", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "touchy")
		if err != nil {
			return Null(), err
		}
		i, err := needInt("touchy", args, 0, at)
		if err != nil {
			return Null(), err
		}
		_, y, err := wb.TouchPos(int(i))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(y)), nil
	})

	// dropfiles(): names dropped onto the window since the last call
	// (consumed; empty when none).
	register("dropfiles", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "dropfiles")
		if err != nil {
			return Null(), err
		}
		names := wb.DropFiles()
		elems := make([]Value, len(names))
		for i, s := range names {
			elems[i] = Str(s)
		}
		return ArrayOf(elems), nil
	})

	// dropload(name): decode a dropped file into a buffer, return its id.
	register("dropload", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "dropload")
		if err != nil {
			return Null(), err
		}
		p, err := needString("dropload", args, 0, at)
		if err != nil {
			return Null(), err
		}
		id, err := wb.DropLoad(p)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(id)), nil
	})

	// screensize(): [window width, height] in pixels — the real OS
	// window, not the monitor and not the logical screen() canvas. While
	// resizable() is allowed, the canvas follows the window, so these
	// are also the drawing pixels.
	register("screensize", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		if _, err := guiBE(in, at, "screensize"); err != nil {
			return Null(), err
		}
		w, h := gui.WindowSize()
		return ArrayOf([]Value{Int(int64(w)), Int(int64(h))}), nil
	})

	// winmove(x, y): move the window (desktop pixels from the
	// monitor's upper-left corner).
	register("winmove", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "winmove")
		if err != nil {
			return Null(), err
		}
		x, err := needInt("winmove", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("winmove", args, 1, at)
		if err != nil {
			return Null(), err
		}
		wb.MoveWindow(int(x), int(y))
		return Null(), nil
	})

	// closing(): whether the close button was pressed. The first call
	// opts in: closing stops terminating the loop, so poll this,
	// save, then end().
	register("closing", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "closing")
		if err != nil {
			return Null(), err
		}
		return Bool(wb.Closing()), nil
	})

	// fullscreen([v]): without arguments reports fullscreen state
	// (1/0); with v toggles it (nonzero = on).
	register("fullscreen", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "fullscreen")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			return Bool(wb.IsFullscreen()), nil
		}
		v, err := needInt("fullscreen", args, 0, at)
		if err != nil {
			return Null(), err
		}
		wb.SetFullscreen(v != 0)
		return Null(), nil
	})

	// resizable([v]): without arguments reports whether the
	// window can be dragged to resize (1/0); with v sets it
	// (nonzero = on). The canvas keeps its logical size; a larger
	// window scales the view.
	register("resizable", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "resizable")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			return Bool(wb.IsResizable()), nil
		}
		v, err := needInt("resizable", args, 0, at)
		if err != nil {
			return Null(), err
		}
		wb.SetResizable(v != 0)
		return Null(), nil
	})

	// mmload(path): decode wav/mp3/ogg, return a sound id.
	register("mmload", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mmload")
		if err != nil {
			return Null(), err
		}
		p, err := needString("mmload", args, 0, at)
		if err != nil {
			return Null(), err
		}
		id, err := wb.MmLoad(in.resolvePath(p))
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Int(int64(id)), nil
	})

	// mmplay(id [, loop]): play a sound, restarting any current play.
	register("mmplay", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mmplay")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("mmplay", args, 0, at)
		if err != nil {
			return Null(), err
		}
		loop := false
		if len(args) == 2 {
			l, err := needInt("mmplay", args, 1, at)
			if err != nil {
				return Null(), err
			}
			loop = l != 0
		}
		if err := wb.MmPlay(int(id), loop); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// mmstop([id]): stop one sound, or all without arguments.
	// Unknown ids are a silent no-op.
	register("mmstop", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mmstop")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			wb.MmStop(nil)
			return Null(), nil
		}
		id, err := needInt("mmstop", args, 0, at)
		if err != nil {
			return Null(), err
		}
		i := int(id)
		wb.MmStop(&i)
		return Null(), nil
	})

	// mmvol(id, vol): sound volume in percent (0..100, default 100).
	register("mmvol", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "mmvol")
		if err != nil {
			return Null(), err
		}
		id, err := needInt("mmvol", args, 0, at)
		if err != nil {
			return Null(), err
		}
		vol, err := needInt("mmvol", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.MmVolume(int(id), int(vol)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
