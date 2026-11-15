// P5 組み込み: ファイル・ディレクトリ入出力。
//
// HSP との違い (文書化済み、現代化):
//   - dirlist は名前の配列を返す (HSP は出力引数に書き込む)。
//   - テキストは txtfile、バイナリ (0-255 の整数配列) は binfile に分離。
//   - 改行は \n に正規化; テキスト読み込みは \r\n を変換する。
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func init() {
	// curdir([path])：カレントディレクトリの取得・変更を統合する。path が
	// あれば変更し、なければ取得する。
	register("curdir", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		if len(args) == 0 {
			cwd, err := os.Getwd()
			if err != nil {
				return Null(), rtErrf(at, "curdir：%s", err.Error())
			}
			return Str(cwd), nil
		}
		p, err := needString("curdir", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := os.Chdir(in.resolvePath(p)); err != nil {
			return Null(), rtErrf(at, "curdir：%s", err.Error())
		}
		return Null(), nil
	})

	// exedir(): 実行中ファイルのあるディレクトリ。
	register("exedir", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		exe, err := os.Executable()
		if err != nil {
			return Null(), rtErrf(at, "exedir：%s", err.Error())
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

	// fileinfo(path): ファイルの情報を 1 回の stat 呼び出しで取得し、
	// [size, mtime_ms, isdir] として返す。size はバイト数、mtime は
	// Unix ミリ秒、isdir はディレクトリ判別用。旧 filetime(path) 相当は
	// fileinfo(path)[1]（存在しなければ 0）、旧 exist(path) 相当は
	// fileinfo(path)[0] != -1。
	// 存在しない場合はエラーの代わりに [-1, 0, 0] を返す
	// (言語に try/catch がないため、エディタは例外なしでポーリングする)。
	// ディレクトリのサイズはOS 依存; 別に stat せず 3 項目を組み合わせること。
	register("fileinfo", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("fileinfo", args, 0, at)
		if err != nil {
			return Null(), err
		}
		fi, err := os.Stat(in.resolvePath(p))
		if err != nil {
			if os.IsNotExist(err) {
				return ArrayOf([]Value{Int(-1), Int(0), Int(0)}), nil
			}
			return Null(), rtErrf(at, "fileinfo：%s", err.Error())
		}
		return ArrayOf([]Value{Int(fi.Size()), Int(fi.ModTime().UnixMilli()), IntOfBool(fi.IsDir())}), nil
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

	// filedel(path): ファイルを削除する。
	register("filedel", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("filedel", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := os.Remove(in.resolvePath(p)); err != nil {
			return Null(), rtErrf(at, "filedel：%s", err.Error())
		}
		return Null(), nil
	})

	// dirmake(path): ディレクトリを 1 階層作成する。
	register("dirmake", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("dirmake", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := os.Mkdir(in.resolvePath(p), 0o777); err != nil {
			return Null(), rtErrf(at, "dirmake：%s", err.Error())
		}
		return Null(), nil
	})

	// filecopy(src, dst): ファイルをコピーする。
	register("filecopy", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		src, err := needString("filecopy", args, 0, at)
		if err != nil {
			return Null(), err
		}
		dst, err := needString("filecopy", args, 1, at)
		if err != nil {
			return Null(), err
		}
		data, err := os.ReadFile(in.resolvePath(src))
		if err != nil {
			return Null(), rtErrf(at, "filecopy：%s", err.Error())
		}
		if err := os.WriteFile(in.resolvePath(dst), data, 0o666); err != nil {
			return Null(), rtErrf(at, "filecopy：%s", err.Error())
		}
		return Null(), nil
	})

	// txtfile(path): テキストとして読み込む (\r\n は \n に正規化)。
	// txtfile(path, s): テキストを書き込む。
	register("txtfile", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("txtfile", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if len(args) == 1 {
			return txtFileLoad(in, p, at)
		}
		s, err := needString("txtfile", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := os.WriteFile(in.resolvePath(p), []byte(s), 0o666); err != nil {
			return Null(), rtErrf(at, "txtfile：%s", err.Error())
		}
		return Null(), nil
	})

	// binfile(path): ファイルのバイト列を整数 (0-255) の配列として読み込む。
	// binfile(path, arr [, size]): 整数 (0-255) をバイト列として書き込む。
	// size は配列の先頭から書き込むバイト数。
	register("binfile", 1, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("binfile", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if len(args) == 1 {
			data, err := os.ReadFile(in.resolvePath(p))
			if err != nil {
				return Null(), rtErrf(at, "binfile：%s", err.Error())
			}
			elems := make([]Value, len(data))
			for i, b := range data {
				elems[i] = Int(int64(b))
			}
			return ArrayOf(elems), nil
		}
		arr, err := needArray("binfile", args, 1, at)
		if err != nil {
			return Null(), err
		}
		data := make([]byte, 0, len(arr.Elems))
		for i, e := range arr.Elems {
			if e.K != KInt || e.I < 0 || e.I > 255 {
				return Null(), rtErrf(at, "binfile：要素 %d は 0 から 255 の整数である必要があります。%s が指定されました", i, Stringify(e))
			}
			data = append(data, byte(e.I))
		}
		if len(args) == 3 {
			size, err := needInt("binfile", args, 2, at)
			if err != nil {
				return Null(), err
			}
			// int64 で比較する: 巨大な size では int(size) が折り返して
			// 検査をすり抜け、下のスライスで異常終了するため。
			if size < 0 || size > int64(len(data)) {
				return Null(), argErr("binfile", 2, at, "サイズが範囲外です（配列の長さ %d）", len(data))
			}
			data = data[:size]
		}
		if err := os.WriteFile(in.resolvePath(p), data, 0o666); err != nil {
			return Null(), rtErrf(at, "binfile：%s", err.Error())
		}
		return Null(), nil
	})
}

// txtFileLoad はファイルをテキストとして読み込み、\r\n を \n に正規化する。
func txtFileLoad(in *Interp, p string, at Pos) (Value, error) {
	data, err := os.ReadFile(in.resolvePath(p))
	if err != nil {
		return Null(), rtErrf(at, "txtfile：%s", err.Error())
	}
	return Str(strings.ReplaceAll(string(data), "\r\n", "\n")), nil
}
