// DLL呼び出し (dll/dlldef)。Golash の配列を直接渡しているように見える
// 連続配列引数 (int[]/float[]) を持つ。
//
// C ABI の DLL を purego 経由で呼ぶ。CGO 不要 (Windows/Linux/macOS 共通)。
// 呼び出しは one-way（スクリプト→DLL。コールバックは v1 の対象外）。
// fastgen 廃止後のネイティブ実行の担い手。g3d の収集部は外部 DLL
// (GolashProjects/g3d/g3daccel) からこの経路で呼ぶ。
//
// 値の受け渡し: スクリプトの値は動的型、DLL 側は C ABI の固定型のため、
// 署名で橋渡しを宣言する。書式は "戻り値(引数1,引数2,...)"。型は
// int(32bit)・float(64bit)・string(UTF-8+NUL)・bool・int[](i32 配列)・
// float[](f64 配列)・void(戻り値のみ)。例:
//
//	dll "user32.dll"
//	dlldef winmsgbox "MessageBoxA" "int(int,string,string,int)"
//	r = winmsgbox(0, "本文", "標題", 0)
//
// int[]/float[] は Golash の配列をそのまま渡す。呼び出し時に連続した
// C バッファへ写し、ポインタを DLL へ渡す。DLL が書き込んだ内容は
// 呼び出し後に元の配列へ書き戻す（空配列は NULL を渡す）。長さは別引数で
// 渡す。ポインタは呼び出し中だけ有効 (DLL 側で保持しないこと)。
// 確保は dim(n) など通常の配列で行う。buf 系組込は廃止した。
//
// 公開 API は dll/dlldef 宣言のみ。dlload/dllcall/dllfree は廃止した。
// dlldef はローダが薄い def 包みに展開し、実体は隠し組込 $dllinvoke
// (path, sym, sig, args...) が開く→引く→呼ぶを一括で行う。$ は字句で
// 書けないためスクリプトから直接は呼べない。
package main

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// dllLib は開いた DLL 1 本 (プラットフォーム別の実体 + シンボルキャッシュ)。
type dllLib struct {
	path string
	impl *dllHandle
	syms map[string]uintptr
}

// dllHiddenName は dlldef 展開だけが呼ぶ隠し組込。$ は字句で書けない
// ためスクリプトから直接は呼べない。dllPlaceholder はローダが生成する
// 一時名（字句可能。生成行だけが隠し名へ書換えられる）。
const dllHiddenName = "$dllinvoke"
const dllPlaceholder = "__dll_invoke__"

// legacyDllNames は廃止した公開組込。呼出しは移行エラーになる。
// buf 系 (bufnewf/bufnewi/buflen/bufget/bufset/bufpackf/bufpacki/bufunpack)
// も廃止し、直接配列 (int[]/float[]) へ移行した。
var legacyDllNames = map[string]bool{
	"dlload": true, "dllcall": true, "dllfree": true,
	"bufnewf": true, "bufnewi": true, "buflen": true,
	"bufget": true, "bufset": true,
	"bufpackf": true, "bufpacki": true, "bufunpack": true,
}

func isLegacyDllName(name string) bool { return legacyDllNames[name] }

func legacyDllErr(at Pos, name string) error {
	switch name {
	case "bufnewf", "bufnewi", "bufpackf", "bufpacki":
		return rtErrf(at, "%s は廃止されました。dim(n) などで配列を作り、dlldef の int[]/float[] 引数へ直接渡してください", name)
	case "buflen", "bufget", "bufset", "bufunpack":
		return rtErrf(at, "%s は廃止されました。配列と len()・添字で操作し、dlldef の int[]/float[] 引数へ直接渡してください", name)
	}
	return rtErrf(at, "%s は廃止されました。dll \"path\" / dlldef NAME \"symbol\" \"sig\" を使ってください", name)
}

var dllMu sync.Mutex

// dllLibs は解決後パスから開いた DLL への対応表（dlldef の宣言ごとに
// 開き直さないためのキャッシュ。プロセス終了まで保持する）。
var dllLibs = map[string]*dllLib{}

// dllSig は解釈済み署名 (戻り値 + 引数型列)。
type dllSig struct {
	ret  string
	args []string
}

// parseDllSig は "ret(t1,t2,...)" を解釈する。型名の前後の空白は許す。
// 引数型は int/float/string/bool/int[]/float[]、戻り値型はそれに void を
// 加えたもの（配列の戻り値は不可）。
func parseDllSig(sig string) (dllSig, error) {
	fail := func(format string, args ...any) (dllSig, error) {
		return dllSig{}, fmt.Errorf(format, args...)
	}
	open := strings.Index(sig, "(")
	close := strings.LastIndex(sig, ")")
	if open < 0 || close < 0 || close < open {
		return fail("署名は \"戻り値(引数1,引数2,...)\" の形である必要があります")
	}
	ret := strings.TrimSpace(sig[:open])
	body := strings.TrimSpace(sig[open+1 : close])
	if strings.TrimSpace(sig[close+1:]) != "" {
		return fail("署名は \"戻り値(引数1,引数2,...)\" の形である必要があります")
	}
	retTypes := map[string]bool{"int": true, "float": true, "string": true, "bool": true}
	argTypes := map[string]bool{"int": true, "float": true, "string": true, "bool": true, "int[]": true, "float[]": true}
	if ret == "void" {
	} else if retTypes[ret] {
	} else {
		return fail("戻り値の型 %q は使えません (int/float/string/bool/void)", ret)
	}
	var args []string
	if body != "" {
		for _, t := range strings.Split(body, ",") {
			t = strings.TrimSpace(t)
			if argTypes[t] {
				args = append(args, t)
			} else {
				return fail("引数の型 %q は使えません (int/float/string/bool/int[]/float[])", t)
			}
		}
	}
	if len(args) > 15 {
		return fail("引数は15個までです (%d 個)", len(args))
	}
	return dllSig{ret: ret, args: args}, nil
}

// dllCString は C が返した NUL 終端文字列（アドレス addr）を Go 文字列へ
// コピーする。1MiB 以内に NUL が無ければエラーにする。
func dllCString(addr uintptr, at Pos) (string, error) {
	if addr == 0 {
		return "", nil
	}
	// C の戻り値は任意のアドレスなので uintptr から unsafe.Pointer への
	// 変換が避けられない。go vet の unsafeptr 検査は uintptr の直接変換を
	// 拒否するため、purego と同じく一度アドレスを経由して取り出す。
	p := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	for n := 0; n < 1<<20; n++ {
		if *(*byte)(unsafe.Add(p, n)) == 0 {
			return string(unsafe.Slice((*byte)(p), n)), nil
		}
	}
	return "", rtErrf(at, "dlldef：戻り文字列に NUL 終端がありません")
}

// resolveDllPath は相対パスの DLL を、スクリプトディレクトリ、次に
// カレントディレクトリの順で実在するものに解決する。どちらにも
// 無い場合は元の名前を返し、システム DLL の OS 検索を妨げない。
func (in *Interp) resolveDllPath(p string) string {
	if q := in.lookupPath(p); q != "" {
		return q
	}
	return p
}

// dllIntWrite は int[] 引数の書き戻し先（DLL が書いた i32 を Golash 配列へ）。
type dllIntWrite struct {
	arr *Array
	buf []int32
}

// dllFloatWrite は float[] 引数の書き戻し先。
type dllFloatWrite struct {
	arr *Array
	buf []float64
}

// dllMarshalIntArr は Golash 配列を i32 連続バッファへ写す。
func dllMarshalIntArr(name string, argIdx int, v Value, at Pos) ([]int32, error) {
	if v.K != KArray {
		return nil, argErr(name, argIdx, at, "配列である必要があります。%s が指定されました", typeNameOf(v))
	}
	out := make([]int32, len(v.Arr.Elems))
	for i, e := range v.Arr.Elems {
		if e.K != KInt {
			return nil, argErr(name, argIdx, at, "要素 %d は整数である必要があります。%s が指定されました", i, typeNameOf(e))
		}
		if e.I > 2147483647 || e.I < -2147483648 {
			return nil, argErr(name, argIdx, at, "要素 %d は 32bit に収まりません (%d)", i, e.I)
		}
		out[i] = int32(e.I)
	}
	return out, nil
}

// dllMarshalFloatArr は Golash 配列を f64 連続バッファへ写す。
// int 要素は float へ昇格する。
func dllMarshalFloatArr(name string, argIdx int, v Value, at Pos) ([]float64, error) {
	if v.K != KArray {
		return nil, argErr(name, argIdx, at, "配列である必要があります。%s が指定されました", typeNameOf(v))
	}
	out := make([]float64, len(v.Arr.Elems))
	for i, e := range v.Arr.Elems {
		f, ok := numValue(e)
		if !ok {
			return nil, argErr(name, argIdx, at, "要素 %d は数値である必要があります。%s が指定されました", i, typeNameOf(e))
		}
		out[i] = f
	}
	return out, nil
}

func init() {
	// $dllinvoke(path, sym, sig, args...) -> 戻り値。dlldef 展開だけが呼ぶ
	// 隠し組込（$ は字句で書けないため直接は呼べない）。開く→引く→呼ぶを
	// 一括で行い、同じパスは開き直さず共用する。存在しない・開けない場合は
	// エラー。dllfree は無い（プロセス終了まで保持。Windows は元々常駐）。
	//
	// int[]/float[] は Golash 配列を連続バッファへ写してポインタを渡し、
	// 呼び出し後に配列へ書き戻す（DLL からの出力を受けられる）。
	register(dllHiddenName, 3, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		const name = "dlldef"
		path, err := needString(name, args, 0, at)
		if err != nil {
			return Null(), err
		}
		sym, err := needString(name, args, 1, at)
		if err != nil {
			return Null(), err
		}
		sigstr, err := needString(name, args, 2, at)
		if err != nil {
			return Null(), err
		}
		loadPath := in.resolveDllPath(path)
		dllMu.Lock()
		lib, ok := dllLibs[loadPath]
		dllMu.Unlock()
		if !ok {
			handle, err := dllOpen(loadPath)
			if err != nil {
				return Null(), rtErrf(at, "dlldef：%q を開けません: %s", loadPath, err.Error())
			}
			lib = &dllLib{path: loadPath, impl: handle, syms: map[string]uintptr{}}
			dllMu.Lock()
			dllLibs[loadPath] = lib
			dllMu.Unlock()
		}
		sig, err := parseDllSig(sigstr)
		if err != nil {
			return Null(), rtErrf(at, "dlldef：%s", err.Error())
		}
		got := args[3:]
		if len(got) != len(sig.args) {
			return Null(), rtErrf(at, "dlldef：署名は引数 %d 個ですが %d 個が渡されました", len(sig.args), len(got))
		}
		fn, ok := lib.syms[sym]
		if !ok {
			addr, err := dllSym(lib.impl, sym)
			if err != nil {
				return Null(), rtErrf(at, "dlldef：%q に %q がありません: %s", lib.path, sym, err.Error())
			}
			fn = addr
			dllMu.Lock()
			lib.syms[sym] = fn
			dllMu.Unlock()
		}
		words := make([]uintptr, 0, len(got))
		var keep [][]byte
		var intWrites []dllIntWrite
		var floatWrites []dllFloatWrite
		// intBufs/floatBufs はポインタの指す先を GC から守る（KeepAlive 用）。
		var intBufs [][]int32
		var floatBufs [][]float64
		for i, t := range sig.args {
			v := got[i]
			switch t {
			case "int":
				if v.K != KInt {
					return Null(), argErr(name, 3+i, at, "整数である必要があります。%s が指定されました", typeNameOf(v))
				}
				if v.I > 2147483647 || v.I < -2147483648 {
					return Null(), argErr(name, 3+i, at, "32bit に収まりません (%d)", v.I)
				}
				words = append(words, uintptr(int64(int32(v.I))))
			case "float":
				f, ok := numValue(v)
				if !ok {
					return Null(), argErr(name, 3+i, at, "数値である必要があります。%s が指定されました", typeNameOf(v))
				}
				words = append(words, uintptr(math.Float64bits(f)))
			case "string":
				if v.K != KString {
					return Null(), argErr(name, 3+i, at, "文字列である必要があります。%s が指定されました", typeNameOf(v))
				}
				b := append([]byte(v.S), 0)
				keep = append(keep, b)
				words = append(words, uintptr(unsafe.Pointer(&b[0])))
			case "bool":
				if v.K != KInt {
					return Null(), argErr(name, 3+i, at, "整数（0=false、非0=true）である必要があります。%s が指定されました", typeNameOf(v))
				}
				if v.I != 0 {
					words = append(words, 1)
				} else {
					words = append(words, 0)
				}
			case "int[]":
				buf, err := dllMarshalIntArr(name, 3+i, v, at)
				if err != nil {
					return Null(), err
				}
				if len(buf) == 0 {
					words = append(words, 0)
				} else {
					words = append(words, uintptr(unsafe.Pointer(&buf[0])))
				}
				intBufs = append(intBufs, buf)
				intWrites = append(intWrites, dllIntWrite{arr: v.Arr, buf: buf})
			case "float[]":
				buf, err := dllMarshalFloatArr(name, 3+i, v, at)
				if err != nil {
					return Null(), err
				}
				if len(buf) == 0 {
					words = append(words, 0)
				} else {
					words = append(words, uintptr(unsafe.Pointer(&buf[0])))
				}
				floatBufs = append(floatBufs, buf)
				floatWrites = append(floatWrites, dllFloatWrite{arr: v.Arr, buf: buf})
			}
		}
		r1, _, _ := purego.SyscallN(fn, words...)
		// DLL が書いた内容を元の配列へ書き戻す（出力バッファとして使える）。
		for _, w := range intWrites {
			for j, c := range w.buf {
				w.arr.Elems[j] = Int(int64(c))
			}
		}
		for _, w := range floatWrites {
			for j, f := range w.buf {
				w.arr.Elems[j] = Float(f)
			}
		}
		runtime.KeepAlive(keep)
		runtime.KeepAlive(intBufs)
		runtime.KeepAlive(floatBufs)
		runtime.KeepAlive(intWrites)
		runtime.KeepAlive(floatWrites)
		switch sig.ret {
		case "void":
			return Null(), nil
		case "int":
			return Int(int64(int32(r1))), nil
		case "float":
			return Float(math.Float64frombits(uint64(r1))), nil
		case "bool":
			return IntOfBool(r1&0xFF != 0), nil
		case "string":
			s, err := dllCString(r1, at)
			if err != nil {
				return Null(), err
			}
			return Str(s), nil
		}
		return Null(), rtErrf(at, "dlldef：内部エラー（未知の戻り値型）")
	})
}
