// GUI builtins (G4 drawing extension: pngsave, paint, font).
// Always registered; GUI calls need a window (a GUI backend),
// otherwise they report an error.
package main

import "os"

func init() {
	// pngsave(path [, id]): save a draw buffer as PNG (id defaults to
	// the current target).
	register("pngsave", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "pngsave")
		if err != nil {
			return Null(), err
		}
		p, err := needString("pngsave", args, 0, at)
		if err != nil {
			return Null(), err
		}
		p = in.resolvePath(p)
		id := int64(wb.CurrentSel())
		if len(args) == 2 {
			id, err = needInt("pngsave", args, 1, at)
			if err != nil {
				return Null(), err
			}
		}
		if err := wb.SavePNG(p, int(id)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// paint(x, y): flood-fill the seed region with the current color.
	register("paint", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "paint")
		if err != nil {
			return Null(), err
		}
		x, err := needInt("paint", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("paint", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.FloodFill(int(x), int(y), wb.Foreground()); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// galpha(a): global drawing alpha 0-255 (shapes and blits; text stays
	// opaque). galpha(255) restores full opacity.
	register("galpha", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "galpha")
		if err != nil {
			return Null(), err
		}
		a, err := needInt("galpha", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.SetAlpha(int(a)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// font(size) / font(spec) / font(spec, size): text face size in
	// pixels (cell grid scales with it). spec is a font file path or a
	// system font name (e.g. "msgothic.ttc", "YuGothR.ttc").
	register("font", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "font")
		if err != nil {
			return Null(), err
		}
		if len(args) == 1 && args[0].K == KInt {
			if err := wb.SetFontSize(int(args[0].I)); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Null(), nil
		}
		spec, err := needString("font", args, 0, at)
		if err != nil {
			return Null(), err
		}
		// The script directory and the working directory are searched
		// first (exact file names, extension completion, and prefix
		// matches), then the system font directories. A bare name falls
		// through when nothing matches.
		dirs := []string{}
		if d := in.ScriptDir(); d != "" {
			dirs = append(dirs, d)
		}
		if cwd, err := os.Getwd(); err == nil {
			dirs = append(dirs, cwd)
		}
		size := 0
		if len(args) == 2 {
			v, err := needInt("font", args, 1, at)
			if err != nil {
				return Null(), err
			}
			size = int(v)
		} else {
			size = wb.FontSize()
		}
		if err := wb.SetFontFile(spec, size, dirs...); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
