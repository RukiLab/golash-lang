// P5 builtins: files and memo-pad (note) operations.
//
// Differences from HSP (documented, modernized):
//   - exist returns bool (HSP reports the size via strsize).
//   - dirlist returns an array of names (HSP writes to an out-param).
//   - note functions are pure: they take the text and return the result
//     instead of using notesel-selected global buffers.
//   - Line endings normalize to \n; noteload converts \r\n on the way in.
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func init() {
	// getcwd(): current working directory.
	register("getcwd", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		cwd, err := os.Getwd()
		if err != nil {
			return Null(), rtErrf(at, "getcwd：%s", err.Error())
		}
		return Str(cwd), nil
	})

	// direxe(): directory holding the running executable.
	register("direxe", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		exe, err := os.Executable()
		if err != nil {
			return Null(), rtErrf(at, "direxe：%s", err.Error())
		}
		return Str(filepath.Dir(exe)), nil
	})

	// homedir(): current user's home directory.
	register("homedir", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return Null(), rtErrf(at, "homedir：%s", err.Error())
		}
		return Str(home), nil
	})

	// tmpdir(): system temporary directory.
	register("tmpdir", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		return Str(os.TempDir()), nil
	})

	// exist(path): whether the file exists.
	register("exist", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("exist", args, 0, at)
		if err != nil {
			return Null(), err
		}
		_, err = os.Stat(in.resolvePath(p))
		if err == nil {
			return Bool(true), nil
		}
		if os.IsNotExist(err) {
			return Bool(false), nil
		}
		return Null(), rtErrf(at, "exist：%s", err.Error())
	})

	// filetime(path): the file's modification time as Unix
	// milliseconds (0 when the file does not exist). Rounding to whole
	// milliseconds keeps the value comparable across platforms, so a
	// script can poll it to notice an external edit (an editor that
	// reloads a changed file, for instance).
	register("filetime", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("filetime", args, 0, at)
		if err != nil {
			return Null(), err
		}
		fi, err := os.Stat(in.resolvePath(p))
		if err != nil {
			if os.IsNotExist(err) {
				return Int(0), nil
			}
			return Null(), rtErrf(at, "filetime：%s", err.Error())
		}
		return Int(fi.ModTime().UnixMilli()), nil
	})

	// fileinfo(path): file metadata in one stat call, as
	// [size, mtime_ms, isdir]. Size is bytes, mtime is Unix
	// milliseconds like filetime(), isdir tells directories apart.
	// A missing path reports [-1, 0, false] instead of an error
	// (same philosophy as filetime: editors poll without try/catch,
	// which the language does not have). Directory sizes are
	// OS-dependent; combine the three fields instead of calling
	// exist()/filetime() separately.
	register("fileinfo", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("fileinfo", args, 0, at)
		if err != nil {
			return Null(), err
		}
		fi, err := os.Stat(in.resolvePath(p))
		if err != nil {
			if os.IsNotExist(err) {
				return ArrayOf([]Value{Int(-1), Int(0), Bool(false)}), nil
			}
			return Null(), rtErrf(at, "fileinfo：%s", err.Error())
		}
		return ArrayOf([]Value{Int(fi.Size()), Int(fi.ModTime().UnixMilli()), Bool(fi.IsDir())}), nil
	})

	// dirlist([mask]): names in the script directory (or the working
	// directory in the REPL), sorted. A mask with a directory part is
	// resolved with the usual script-first rule.
	register("dirlist", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		mask := "*"
		if len(args) == 1 {
			var err error
			mask, err = needString("dirlist", args, 0, at)
			if err != nil {
				return Null(), err
			}
		}
		dir, file := filepath.Split(mask)
		if dir == "" {
			if in.scriptDir != "" {
				dir = in.scriptDir
			} else {
				dir = "."
			}
		} else if !filepath.IsAbs(dir) {
			if q := in.lookupPath(dir); q != "" {
				dir = q
			}
		}
		if file == "" {
			file = "*"
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return Null(), rtErrf(at, "dirlist：%s", err.Error())
		}
		mask = file
		var names []string
		for _, e := range entries {
			ok, merr := filepath.Match(mask, e.Name())
			if merr != nil {
				return Null(), argErr("dirlist", 0, at, "不正なマスク %q です", mask)
			}
			if ok {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		elems := make([]Value, len(names))
		for i, n := range names {
			elems[i] = Str(n)
		}
		return ArrayOf(elems), nil
	})

	// delete(path): remove a file.
	register("delete", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("delete", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := os.Remove(in.resolvePath(p)); err != nil {
			return Null(), rtErrf(at, "delete：%s", err.Error())
		}
		return Null(), nil
	})

	// mkdir(path): create one directory level.
	register("mkdir", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("mkdir", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := os.Mkdir(in.resolvePath(p), 0o777); err != nil {
			return Null(), rtErrf(at, "mkdir：%s", err.Error())
		}
		return Null(), nil
	})

	// chdir(path): change the working directory.
	register("chdir", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("chdir", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := os.Chdir(in.resolvePath(p)); err != nil {
			return Null(), rtErrf(at, "chdir：%s", err.Error())
		}
		return Null(), nil
	})

	// bcopy(src, dst): copy a file.
	register("bcopy", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		src, err := needString("bcopy", args, 0, at)
		if err != nil {
			return Null(), err
		}
		dst, err := needString("bcopy", args, 1, at)
		if err != nil {
			return Null(), err
		}
		data, err := os.ReadFile(in.resolvePath(src))
		if err != nil {
			return Null(), rtErrf(at, "bcopy：%s", err.Error())
		}
		if err := os.WriteFile(in.resolvePath(dst), data, 0o666); err != nil {
			return Null(), rtErrf(at, "bcopy：%s", err.Error())
		}
		return Null(), nil
	})

	// bload(path): file bytes as an array of ints (0-255).
	register("bload", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("bload", args, 0, at)
		if err != nil {
			return Null(), err
		}
		data, err := os.ReadFile(in.resolvePath(p))
		if err != nil {
			return Null(), rtErrf(at, "bload：%s", err.Error())
		}
		elems := make([]Value, len(data))
		for i, b := range data {
			elems[i] = Int(int64(b))
		}
		return ArrayOf(elems), nil
	})

	// bsave(path, arr [, size]): write ints (0-255) as bytes.
	register("bsave", 2, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("bsave", args, 0, at)
		if err != nil {
			return Null(), err
		}
		arr, err := needArray("bsave", args, 1, at)
		if err != nil {
			return Null(), err
		}
		data := make([]byte, 0, len(arr.Elems))
		for i, e := range arr.Elems {
			if e.K != KInt || e.I < 0 || e.I > 255 {
				return Null(), rtErrf(at, "bsave：要素 %d は 0 から 255 の整数である必要があります。%s が指定されました", i, Stringify(e))
			}
			data = append(data, byte(e.I))
		}
		if len(args) == 3 {
			size, err := needInt("bsave", args, 2, at)
			if err != nil {
				return Null(), err
			}
			// Compare in int64: int(size) can wrap on huge size and
			// skip the check, panicking the slice below.
			if size < 0 || size > int64(len(data)) {
				return Null(), argErr("bsave", 2, at, "サイズが範囲外です（配列の長さ %d）", len(data))
			}
			data = data[:size]
		}
		if err := os.WriteFile(in.resolvePath(p), data, 0o666); err != nil {
			return Null(), rtErrf(at, "bsave：%s", err.Error())
		}
		return Null(), nil
	})

	// notemax(s): line count ("" has 0 lines).
	register("notemax", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("notemax", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Int(int64(len(noteLines(s)))), nil
	})

	// noteget(s, i): the i-th line (0-based).
	register("noteget", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("noteget", args, 0, at)
		if err != nil {
			return Null(), err
		}
		i, err := needInt("noteget", args, 1, at)
		if err != nil {
			return Null(), err
		}
		lines := noteLines(s)
		if i < 0 || int(i) >= len(lines) {
			return Null(), argErr("noteget", 1, at, "行 %d は範囲外です（%d 行）", i, len(lines))
		}
		return Str(lines[i]), nil
	})

	// noteadd(s, i, line): replace line i, or append when i == line count.
	register("noteadd", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("noteadd", args, 0, at)
		if err != nil {
			return Null(), err
		}
		i, err := needInt("noteadd", args, 1, at)
		if err != nil {
			return Null(), err
		}
		line, err := needString("noteadd", args, 2, at)
		if err != nil {
			return Null(), err
		}
		lines := noteLines(s)
		if i < 0 || int(i) > len(lines) {
			return Null(), argErr("noteadd", 1, at, "行 %d は範囲外です（%d 行）", i, len(lines))
		}
		if int(i) == len(lines) {
			lines = append(lines, line)
		} else {
			lines[i] = line
		}
		return Str(strings.Join(lines, "\n")), nil
	})

	// notedel(s, i): text without line i.
	register("notedel", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("notedel", args, 0, at)
		if err != nil {
			return Null(), err
		}
		i, err := needInt("notedel", args, 1, at)
		if err != nil {
			return Null(), err
		}
		lines := noteLines(s)
		if i < 0 || int(i) >= len(lines) {
			return Null(), argErr("notedel", 1, at, "行 %d は範囲外です（%d 行）", i, len(lines))
		}
		lines = append(lines[:i], lines[i+1:]...)
		return Str(strings.Join(lines, "\n")), nil
	})

	// noteload(path): whole file as text.
	register("noteload", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("noteload", args, 0, at)
		if err != nil {
			return Null(), err
		}
		data, err := os.ReadFile(in.resolvePath(p))
		if err != nil {
			return Null(), rtErrf(at, "noteload：%s", err.Error())
		}
		return Str(strings.ReplaceAll(string(data), "\r\n", "\n")), nil
	})

	// notesave(path, s): write text to a file.
	register("notesave", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("notesave", args, 0, at)
		if err != nil {
			return Null(), err
		}
		s, err := needString("notesave", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := os.WriteFile(in.resolvePath(p), []byte(s), 0o666); err != nil {
			return Null(), rtErrf(at, "notesave：%s", err.Error())
		}
		return Null(), nil
	})
}

// noteLines splits text into lines; "" yields no lines.
func noteLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
