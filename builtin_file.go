// P5 組み込み: ファイルとメモ帳 (note) 操作。
//
// HSP との違い (文書化済み、現代化):
//   - exist は真偽値を返す (HSP は strsize 経由でサイズを報告)。
//   - dirlist は名前の配列を返す (HSP は出力引数に書き込む)。
//   - note 系関数は純粋: notesel 選択の全体バッファではなく、
//     テキストを受け取り結果を返す。
//   - 改行は \n に正規化; noteload は読み込み時に \r\n を変換する。
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func init() {
	// getcwd(): 現在の作業ディレクトリ。
	register("getcwd", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		cwd, err := os.Getwd()
		if err != nil {
			return Null(), rtErrf(at, "getcwd：%s", err.Error())
		}
		return Str(cwd), nil
	})

	// direxe(): 実行中ファイルのあるディレクトリ。
	register("direxe", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		exe, err := os.Executable()
		if err != nil {
			return Null(), rtErrf(at, "direxe：%s", err.Error())
		}
		return Str(filepath.Dir(exe)), nil
	})

	// homedir(): 現在のユーザーのホームディレクトリ。
	register("homedir", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return Null(), rtErrf(at, "homedir：%s", err.Error())
		}
		return Str(home), nil
	})

	// tmpdir(): システムの一時ディレクトリ。
	register("tmpdir", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		return Str(os.TempDir()), nil
	})

	// exist(path): ファイルが存在するかどうか。
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

	// filetime(path): ファイルの更新時刻を Unix
	// ミリ秒で表す (存在しない場合は 0)。ミリ秒単位に丸めることで
	// プラットフォーム間で比較可能にし、スクリプトが外部編集に
	// 気づくためのポーリングに使える (例: 変更されたファイルを
	// 読み直すエディタ)。
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

	// fileinfo(path): ファイルの情報を 1 回の stat 呼び出しで取得し、
	// [size, mtime_ms, isdir] として返す。size はバイト数、mtime は
	// filetime() と同じ Unix ミリ秒、isdir はディレクトリ判別用。
	// 存在しない場合はエラーの代わりに [-1, 0, false] を返す
	// (filetime と同じ考え方: 言語に try/catch がないため、
	// エディタは例外なしでポーリングする)。ディレクトリのサイズは
	// OS 依存; exist()/filetime() を別々に呼ばず 3 項目を組み合わせること。
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

	// dirlist([mask]): スクリプト用ディレクトリ内の名前一覧 (REPL では作業
	// ディレクトリ内)、ソート済み。ディレクトリ部分を含むマスクは
	// 通常のスクリプト優先規則で解決される。
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

	// delete(path): ファイルを削除する。
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

	// mkdir(path): ディレクトリを 1 階層作成する。
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

	// chdir(path): 作業ディレクトリを変更する。
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

	// bcopy(src, dst): ファイルをコピーする。
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

	// bload(path): ファイルのバイト列を整数 (0-255) の配列として読み込む。
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

	// bsave(path, arr [, size]): 整数 (0-255) をバイト列として書き込む。
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
			// int64 で比較する: 巨大な size では int(size) が折り返して
			// 検査をすり抜け、下のスライスで異常終了するため。
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

	// notemax(s): 行数 ("" は 0 行)。
	register("notemax", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("notemax", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Int(int64(len(noteLines(s)))), nil
	})

	// noteget(s, i): i 番目の行 (0 始まり)。
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

	// noteadd(s, i, line): i 行目を置換、i が行数と等しい場合は追加。
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

	// notedel(s, i): i 行目を除いたテキスト。
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

	// noteload(path): ファイル全体をテキストとして読み込む。
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

	// notesave(path, s): テキストをファイルに書き込む。
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

// noteLines はテキストを行に分割する; "" は行なしになる。
func noteLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
