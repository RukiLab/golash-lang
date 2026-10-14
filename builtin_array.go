// 配列の組込: スタック/キューと形状変更の操作。
//
// 配列は参照値である: push/pop/insert/remove/sort/reverse は
// （poke と同じく）その場で変更する。問い合わせ系の関数
// （join/slice）は新しい値を返す。
package main

import (
	"fmt"
	"sort"
	"strings"
)

// maxDimTotal は dim() の総要素数（サイズの積）の上限を制限する。
// 上限がないと、巨大なサイズで make() が panic し、スクリプトの
// エラーを返せなくなる。
const maxDimTotal = 1 << 20

// makeDim は dim() 用にゼロ埋めされた入れ子の要素を構築する。
func makeDim(sizes []int) []Value {
	elems := make([]Value, sizes[0])
	if len(sizes) == 1 {
		for i := range elems {
			elems[i] = Int(0)
		}
		return elems
	}
	for i := range elems {
		elems[i] = ArrayOf(makeDim(sizes[1:]))
	}
	return elems
}

func init() {
	// dim(n1 [, n2, ...]): ゼロ埋めされた配列を確保する。
	// サイズが1つなら平坦な配列、複数なら入れ子になる（dim(2, 3) は 2x3）。
	register("dim", 1, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		sizes := make([]int, len(args))
		total := int64(1)
		for i, a := range args {
			if a.K != KInt {
				return Null(), rtErrf(at, "dim：要素数は整数である必要があります。%s が指定されました", typeNameOf(a))
			}
			if a.I < 0 {
				return Null(), rtErrf(at, "dim：要素数は 0 以上である必要があります。%d が指定されました", a.I)
			}
			// 除算で積の検査を桁あふれさせない。ゼロサイズは有効な
			// まま（dim(0) は空配列）。
			if a.I != 0 && total > maxDimTotal/a.I {
				return Null(), rtErrf(at, "dim：要素数が上限 %d を超えています", maxDimTotal)
			}
			total *= a.I
			sizes[i] = int(a.I)
		}
		return ArrayOf(makeDim(sizes)), nil
	})

	// push(arr, v...): 値を追加し、新しい長さを返す。
	register("push", 1, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("push", args, 0, at)
		if err != nil {
			return Null(), err
		}
		arr.Elems = append(arr.Elems, args[1:]...)
		return Int(int64(len(arr.Elems))), nil
	})

	// pop(arr): 最後の要素を削除して返す。空の場合はエラー。
	register("pop", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("pop", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if len(arr.Elems) == 0 {
			return Null(), rtErrf(at, "pop：配列が空です")
		}
		v := arr.Elems[len(arr.Elems)-1]
		arr.Elems = arr.Elems[:len(arr.Elems)-1]
		return v, nil
	})

	// join(arr [, delim]): 要素を文字列化して連結する。
	// 区切りは既定で ""（区切りなし。カンマにするには "," を渡す）。
	register("join", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("join", args, 0, at)
		if err != nil {
			return Null(), err
		}
		delim := ""
		if len(args) == 2 {
			var err error
			delim, err = needString("join", args, 1, at)
			if err != nil {
				return Null(), err
			}
		}
		parts := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			parts[i] = Stringify(e)
		}
		return Str(strings.Join(parts, delim)), nil
	})

	// sort(arr): その場でソート（整数か文字列。混合はエラー）。
	// 連鎖のため配列を返す。
	register("sort", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("sort", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := sortElems(arr.Elems); err != nil {
			return Null(), argErr("sort", 0, at, "%s", err.Error())
		}
		return args[0], nil
	})

	// reverse(arr): その場で反転。連鎖のため配列を返す。
	register("reverse", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("reverse", args, 0, at)
		if err != nil {
			return Null(), err
		}
		for i, j := 0, len(arr.Elems)-1; i < j; i, j = i+1, j-1 {
			arr.Elems[i], arr.Elems[j] = arr.Elems[j], arr.Elems[i]
		}
		return args[0], nil
	})

	// insert(arr, i, v...): インデックス i (0..length) に値を挿入する。
	register("insert", 2, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("insert", args, 0, at)
		if err != nil {
			return Null(), err
		}
		i, err := needInt("insert", args, 1, at)
		if err != nil {
			return Null(), err
		}
		// int64 で比較する: 巨大な i で int(i) が桁あふれし、検査を
		// 飛ばしてしまう可能性がある。
		if i < 0 || i > int64(len(arr.Elems)) {
			return Null(), argErr("insert", 1, at, "添字 %d は範囲外です（長さ %d）", i, len(arr.Elems))
		}
		arr.Elems = append(arr.Elems[:int(i):int(i)], append(append([]Value{}, args[2:]...), arr.Elems[int(i):]...)...)
		return Int(int64(len(arr.Elems))), nil
	})

	// remove(arr, i [, count]): i から count 個の要素を削除し、それらを返す。
	register("remove", 2, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("remove", args, 0, at)
		if err != nil {
			return Null(), err
		}
		i, err := needInt("remove", args, 1, at)
		if err != nil {
			return Null(), err
		}
		count := int64(1)
		if len(args) == 3 {
			var err error
			count, err = needInt("remove", args, 2, at)
			if err != nil {
				return Null(), err
			}
		}
		// int64 で比較する: 巨大な値で i+count が桁あふれし、検査を
		// 飛ばしてしまう可能性がある。分割することで両方の比較を
		// 桁あふれなしに保つ。
		if i < 0 || count < 0 || i > int64(len(arr.Elems)) || count > int64(len(arr.Elems))-i {
			return Null(), argErr("remove", 1, at, "範囲 %d から %d は範囲外です（長さ %d）", i, i+count, len(arr.Elems))
		}
		out := append([]Value(nil), arr.Elems[int(i):int(i)+int(count)]...)
		arr.Elems = append(arr.Elems[:int(i)], arr.Elems[int(i)+int(count):]...)
		return ArrayOf(out), nil
	})

	// slice(arr, start [, end]): 要素 [start, end) の複製。
	// 負のインデックスは末尾から数える。
	register("slice", 2, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("slice", args, 0, at)
		if err != nil {
			return Null(), err
		}
		n := int64(len(arr.Elems))
		start, err := needInt("slice", args, 1, at)
		if err != nil {
			return Null(), err
		}
		end := n
		if len(args) == 3 {
			var err error
			end, err = needInt("slice", args, 2, at)
			if err != nil {
				return Null(), err
			}
		}
		if start < 0 {
			start += n
		}
		if end < 0 {
			end += n
		}
		if start < 0 || end < 0 || start > n || end > n || start > end {
			return Null(), argErr("slice", 1, at, "範囲 %d から %d は範囲外です（長さ %d）", args[1].I, end, n)
		}
		return ArrayOf(append([]Value(nil), arr.Elems[int(start):int(end)]...)), nil
	})

	// find(arr, v): == による v のインデックス。無ければ -1。
	register("find", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("find", args, 0, at)
		if err != nil {
			return Null(), err
		}
		for i, e := range arr.Elems {
			if valuesEqual(e, args[1]) {
				return Int(int64(i)), nil
			}
		}
		return Int(-1), nil
	})
}

// sortElems は数値（int/float 混合）またはすべて文字列の要素を
// （安定）ソートする。それ以外はエラー。
func sortElems(elems []Value) error {
	if len(elems) == 0 {
		return nil
	}
	allInt, allNum, allString := true, true, true
	for _, e := range elems {
		if e.K != KInt {
			allInt = false
		}
		if e.K != KInt && e.K != KFloat {
			allNum = false
		}
		if e.K != KString {
			allString = false
		}
	}
	switch {
	case allInt:
		sort.SliceStable(elems, func(i, j int) bool { return elems[i].I < elems[j].I })
	case allNum:
		sort.SliceStable(elems, func(i, j int) bool { return toFloat(elems[i]) < toFloat(elems[j]) })
	case allString:
		sort.SliceStable(elems, func(i, j int) bool { return elems[i].S < elems[j].S })
	default:
		return fmt.Errorf("要素はすべて数値かすべて文字列である必要があります")
	}
	return nil
}
