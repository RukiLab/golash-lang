// DLL呼び出し (dlload/dllcall/dllfree) と連続バッファ (bufnewf/bufnewi/...)。
//
// C ABI の DLL を purego 経由で呼ぶ。CGO 不要 (Windows/Linux/macOS 共通)。
// 呼び出しは one-way（スクリプト→DLL。コールバックは v1 の対象外）。
// fastgen 廃止後のネイティブ実行の担い手。g3d の収集部は外部 DLL
// (GolashProjects/g3d/g3daccel) からこの経路で呼ぶ。
//
// 値の受け渡し: スクリプトの値は動的型、DLL 側は C ABI の固定型のため、
// 署名で橋渡しを宣言する。書式は "戻り値(引数1,引数2,...)"。型は
// int(32bit)・float(64bit)・string(UTF-8+NUL)・bool・buf(ポインタ。
// 長さは別引数で渡す)・void(戻り値のみ)。例:
//
//	h = dlload("user32.dll")
//	r = dllcall(h, "MessageBoxA", "int(int,string,string,int)", 0, "本文", "標題", 0)
//
// buf は f64/i32 の連続バッファ (id 参照)。配列との一括変換
// (bufpackf/bufpacki/bufunpack) で境界を越える。DLL 側はポインタを
// 呼び出し中だけ使える (保持しないこと)。
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

var dllMu sync.Mutex
var dllLibs = map[int64]*dllLib{}
var dllNext int64 = 1

// dllPaths はパス文字列からハンドルへの対応表（#dlldef の
// 宣言ごとに開き直さないためのキャッシュ。完全一致で引く）。
var dllPaths = map[string]int64{}

// dllBuf は f64/i32 の連続バッファ (id 参照で共有)。
type dllBuf struct {
	isF bool
	f64 []float64
	i32 []int32
}

var bufMu sync.Mutex
var dllBufs = map[int64]*dllBuf{}
var bufNext int64 = 1

// dllSig は解釈済み署名 (戻り値 + 引数型列)。
type dllSig struct {
	ret  string
	args []string
}

// parseDllSig は "ret(t1,t2,...)" を解釈する。型名の前後の空白は許す。
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
	types := map[string]bool{"int": true, "float": true, "string": true, "bool": true, "buf": true}
	if ret == "void" {
	} else if types[ret] {
	} else {
		return fail("戻り値の型 %q は使えません (int/float/string/bool/buf/void)", ret)
	}
	var args []string
	if body != "" {
		for _, t := range strings.Split(body, ",") {
			t = strings.TrimSpace(t)
			if types[t] {
				args = append(args, t)
			} else {
				return fail("引数の型 %q は使えません (int/float/string/bool/buf)", t)
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
	return "", rtErrf(at, "dllcall：戻り文字列に NUL 終端がありません")
}

func init() {
	// dlload(path) -> ハンドル (整数)。同じパスは開き直さず
	// 既存のハンドルを返す (#dlldef の宣言が毎回開かないため)。
	// 存在しない・開けない場合はエラー。
	register("dlload", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		path, err := needString("dlload", args, 0, at)
		if err != nil {
			return Null(), err
		}
		dllMu.Lock()
		if id, ok := dllPaths[path]; ok {
			if _, ok := dllLibs[id]; ok {
				dllMu.Unlock()
				return Int(id), nil
			}
			delete(dllPaths, path)
		}
		dllMu.Unlock()
		handle, err := dllOpen(path)
		if err != nil {
			return Null(), rtErrf(at, "dlload：%q を開けません: %s", path, err.Error())
		}
		dllMu.Lock()
		id := dllNext
		dllNext++
		dllLibs[id] = &dllLib{path: path, impl: handle, syms: map[string]uintptr{}}
		dllPaths[path] = id
		dllMu.Unlock()
		return Int(id), nil
	})

	// dllfree(h) -> Null。開いていないハンドルはエラー。
	// 閉じた後に同じパスを開くと新しいハンドルになる。
	register("dllfree", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		id, err := needInt("dllfree", args, 0, at)
		if err != nil {
			return Null(), err
		}
		dllMu.Lock()
		lib, ok := dllLibs[id]
		if ok {
			delete(dllLibs, id)
			if dllPaths[lib.path] == id {
				delete(dllPaths, lib.path)
			}
		}
		dllMu.Unlock()
		if !ok {
			return Null(), rtErrf(at, "dllfree：dlload で開いたハンドルではありません (%d)", id)
		}
		if err := dllClose(lib.impl); err != nil {
			return Null(), rtErrf(at, "dllfree：%q を閉じられません: %s", lib.path, err.Error())
		}
		return Null(), nil
	})

	// dllcall(h, name, sig, args...) -> 戻り値。
	register("dllcall", 3, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		id, err := needInt("dllcall", args, 0, at)
		if err != nil {
			return Null(), err
		}
		name, err := needString("dllcall", args, 1, at)
		if err != nil {
			return Null(), err
		}
		sigstr, err := needString("dllcall", args, 2, at)
		if err != nil {
			return Null(), err
		}
		dllMu.Lock()
		lib, ok := dllLibs[id]
		dllMu.Unlock()
		if !ok {
			return Null(), rtErrf(at, "dllcall：dlload で開いたハンドルではありません (%d)", id)
		}
		sig, err := parseDllSig(sigstr)
		if err != nil {
			return Null(), rtErrf(at, "dllcall：%s", err.Error())
		}
		got := args[3:]
		if len(got) != len(sig.args) {
			return Null(), rtErrf(at, "dllcall：署名は引数 %d 個ですが %d 個が渡されました", len(sig.args), len(got))
		}
		fn, ok := lib.syms[name]
		if !ok {
			addr, err := dllSym(lib.impl, name)
			if err != nil {
				return Null(), rtErrf(at, "dllcall：%q に %q がありません: %s", lib.path, name, err.Error())
			}
			fn = addr
			dllMu.Lock()
			lib.syms[name] = fn
			dllMu.Unlock()
		}
		words := make([]uintptr, 0, len(got))
		var keep [][]byte
		for i, t := range sig.args {
			v := got[i]
			switch t {
			case "int":
				if v.K != KInt {
					return Null(), argErr("dllcall", 3+i, at, "整数である必要があります。%s が指定されました", typeNameOf(v))
				}
				if v.I > 2147483647 || v.I < -2147483648 {
					return Null(), argErr("dllcall", 3+i, at, "32bit に収まりません (%d)", v.I)
				}
				words = append(words, uintptr(int64(int32(v.I))))
			case "float":
				f, ok := numValue(v)
				if !ok {
					return Null(), argErr("dllcall", 3+i, at, "数値である必要があります。%s が指定されました", typeNameOf(v))
				}
				words = append(words, uintptr(math.Float64bits(f)))
			case "string":
				if v.K != KString {
					return Null(), argErr("dllcall", 3+i, at, "文字列である必要があります。%s が指定されました", typeNameOf(v))
				}
				b := append([]byte(v.S), 0)
				keep = append(keep, b)
				words = append(words, uintptr(unsafe.Pointer(&b[0])))
			case "bool":
				if v.K != KBool {
					return Null(), argErr("dllcall", 3+i, at, "真偽値（bool）である必要があります。%s が指定されました", typeNameOf(v))
				}
				if v.B {
					words = append(words, 1)
				} else {
					words = append(words, 0)
				}
			case "buf":
				if v.K != KInt {
					return Null(), argErr("dllcall", 3+i, at, "バッファ id（整数）である必要があります。%s が指定されました", typeNameOf(v))
				}
				bufMu.Lock()
				b, ok := dllBufs[v.I]
				bufMu.Unlock()
				if !ok {
					return Null(), argErr("dllcall", 3+i, at, "開いているバッファ id ではありません (%d)", v.I)
				}
				if b.isF {
					if len(b.f64) == 0 {
						words = append(words, 0)
					} else {
						words = append(words, uintptr(unsafe.Pointer(&b.f64[0])))
					}
				} else {
					if len(b.i32) == 0 {
						words = append(words, 0)
					} else {
						words = append(words, uintptr(unsafe.Pointer(&b.i32[0])))
					}
				}
			}
		}
		r1, _, _ := purego.SyscallN(fn, words...)
		runtime.KeepAlive(keep)
		switch sig.ret {
		case "void":
			return Null(), nil
		case "int":
			return Int(int64(int32(r1))), nil
		case "float":
			return Float(math.Float64frombits(uint64(r1))), nil
		case "bool":
			return Bool(r1&0xFF != 0), nil
		case "string":
			s, err := dllCString(r1, at)
			if err != nil {
				return Null(), err
			}
			return Str(s), nil
		case "buf":
			return Null(), rtErrf(at, "dllcall：戻り値に buf は使えません")
		}
		return Null(), rtErrf(at, "dllcall：内部エラー（未知の戻り値型）")
	})

	// bufnewf(n)/bufnewi(n) -> 空バッファの id。
	register("bufnewf", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		n, err := needInt("bufnewf", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if n < 0 {
			return Null(), argErr("bufnewf", 0, at, "0 以上である必要があります (%d)", n)
		}
		bufMu.Lock()
		id := bufNext
		bufNext++
		dllBufs[id] = &dllBuf{isF: true, f64: make([]float64, n)}
		bufMu.Unlock()
		return Int(id), nil
	})
	register("bufnewi", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		n, err := needInt("bufnewi", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if n < 0 {
			return Null(), argErr("bufnewi", 0, at, "0 以上である必要があります (%d)", n)
		}
		bufMu.Lock()
		id := bufNext
		bufNext++
		dllBufs[id] = &dllBuf{isF: false, i32: make([]int32, n)}
		bufMu.Unlock()
		return Int(id), nil
	})

	// buflen(b) -> 要素数。
	register("buflen", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		b, err := dllBufOf("buflen", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if b.isF {
			return Int(int64(len(b.f64))), nil
		}
		return Int(int64(len(b.i32))), nil
	})

	// bufget(b, i) -> 要素。bufset(b, i, v) -> Null。
	register("bufget", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		b, err := dllBufOf("bufget", args, 0, at)
		if err != nil {
			return Null(), err
		}
		i, err := needInt("bufget", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if b.isF {
			if i < 0 || i >= int64(len(b.f64)) {
				return Null(), argErr("bufget", 1, at, "範囲外です（長さ %d に %d）", len(b.f64), i)
			}
			return Float(b.f64[i]), nil
		}
		if i < 0 || i >= int64(len(b.i32)) {
			return Null(), argErr("bufget", 1, at, "範囲外です（長さ %d に %d）", len(b.i32), i)
		}
		return Int(int64(b.i32[i])), nil
	})
	register("bufset", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		b, err := dllBufOf("bufset", args, 0, at)
		if err != nil {
			return Null(), err
		}
		i, err := needInt("bufset", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if b.isF {
			if i < 0 || i >= int64(len(b.f64)) {
				return Null(), argErr("bufset", 1, at, "範囲外です（長さ %d に %d）", len(b.f64), i)
			}
			f, ok := numValue(args[2])
			if !ok {
				return Null(), argErr("bufset", 2, at, "数値である必要があります。%s が指定されました", typeNameOf(args[2]))
			}
			b.f64[i] = f
			return Null(), nil
		}
		if i < 0 || i >= int64(len(b.i32)) {
			return Null(), argErr("bufset", 1, at, "範囲外です（長さ %d に %d）", len(b.i32), i)
		}
		if args[2].K != KInt {
			return Null(), argErr("bufset", 2, at, "整数である必要があります。%s が指定されました", typeNameOf(args[2]))
		}
		if args[2].I > 2147483647 || args[2].I < -2147483648 {
			return Null(), argErr("bufset", 2, at, "32bit に収まりません (%d)", args[2].I)
		}
		b.i32[i] = int32(args[2].I)
		return Null(), nil
	})

	// bufpackf(arr)/bufpacki(arr) -> 平坦な数値配列を詰めた新バッファの id。
	register("bufpackf", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		a, err := needArray("bufpackf", args, 0, at)
		if err != nil {
			return Null(), err
		}
		out := make([]float64, len(a.Elems))
		for i, e := range a.Elems {
			f, ok := numValue(e)
			if !ok {
				return Null(), rtErrf(at, "bufpackf：要素 %d は数値である必要があります。%s が指定されました", i, typeNameOf(e))
			}
			out[i] = f
		}
		bufMu.Lock()
		id := bufNext
		bufNext++
		dllBufs[id] = &dllBuf{isF: true, f64: out}
		bufMu.Unlock()
		return Int(id), nil
	})
	register("bufpacki", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		a, err := needArray("bufpacki", args, 0, at)
		if err != nil {
			return Null(), err
		}
		out := make([]int32, len(a.Elems))
		for i, e := range a.Elems {
			if e.K != KInt {
				return Null(), rtErrf(at, "bufpacki：要素 %d は整数である必要があります。%s が指定されました", i, typeNameOf(e))
			}
			if e.I > 2147483647 || e.I < -2147483648 {
				return Null(), rtErrf(at, "bufpacki：要素 %d は 32bit に収まりません (%d)", i, e.I)
			}
			out[i] = int32(e.I)
		}
		bufMu.Lock()
		id := bufNext
		bufNext++
		dllBufs[id] = &dllBuf{isF: false, i32: out}
		bufMu.Unlock()
		return Int(id), nil
	})

	// bufunpack(b) -> バッファの全要素配列。
	register("bufunpack", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		b, err := dllBufOf("bufunpack", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if b.isF {
			out := make([]Value, len(b.f64))
			for i, f := range b.f64 {
				out[i] = Float(f)
			}
			return ArrayOf(out), nil
		}
		out := make([]Value, len(b.i32))
		for i, v := range b.i32 {
			out[i] = Int(int64(v))
		}
		return ArrayOf(out), nil
	})
}

// dllBufOf はバッファ id を解決する。
func dllBufOf(name string, args []Value, i int, at Pos) (*dllBuf, error) {
	id, err := needInt(name, args, i, at)
	if err != nil {
		return nil, err
	}
	bufMu.Lock()
	b, ok := dllBufs[id]
	bufMu.Unlock()
	if !ok {
		return nil, argErr(name, i, at, "開いているバッファ id ではありません (%d)", id)
	}
	return b, nil
}
