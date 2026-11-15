// 文字列と配列に共通する非破壊のシーケンス操作。
//
// 長さ・部分列・検索は文字列と配列で同じ概念のため、1つの関数で
// 両方を受け取る（len/slice/find）。文字列は rune 単位、配列は要素
// 単位で数える。slice は新しい値を作り、元の配列は変更しない。
package main

import "strings"

func init() {
	// len(s | arr)：文字列は rune 数、配列は要素数を返す。
	register("len", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		switch args[0].K {
		case KString:
			return Int(int64(len([]rune(args[0].S)))), nil
		case KArray:
			return Int(int64(len(args[0].Arr.Elems))), nil
		default:
			return Null(), argErr("len", 0, at, "文字列または配列である必要があります。%s が指定されました", typeNameOf(args[0]))
		}
	})

	// slice(s | arr, start [, end])：部分列 [start, end) を複製する。
	// 文字列は rune、配列は要素単位。start/end が負なら末尾から数える。
	// end 省略時は末尾まで。文字列は新しい文字列、配列は新しい配列を
	// 返し、元の配列は変更しない。
	register("slice", 2, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		if args[0].K != KString && args[0].K != KArray {
			return Null(), argErr("slice", 0, at, "文字列または配列である必要があります。%s が指定されました", typeNameOf(args[0]))
		}
		start, err := needInt("slice", args, 1, at)
		if err != nil {
			return Null(), err
		}
		var n int64
		if args[0].K == KString {
			n = int64(len([]rune(args[0].S)))
		} else {
			n = int64(len(args[0].Arr.Elems))
		}
		end := n
		rawEnd := end
		if len(args) == 3 {
			end, err = needInt("slice", args, 2, at)
			if err != nil {
				return Null(), err
			}
			rawEnd = end
			if end < 0 {
				end += n
			}
		}
		rawStart := start
		if start < 0 {
			start += n
		}
		if start < 0 || start > n || end < 0 || end > n || start > end {
			return Null(), argErr("slice", 1, at, "範囲 %d から %d は範囲外です（長さ %d）", rawStart, rawEnd, n)
		}
		if args[0].K == KString {
			r := []rune(args[0].S)
			return Str(string(r[int(start):int(end)])), nil
		}
		return ArrayOf(append([]Value(nil), args[0].Arr.Elems[int(start):int(end)]...)), nil
	})

	// find(s | arr, needle [, start])：最初に一致する位置を返す。
	// 文字列は rune 単位の絶対インデックス、配列は要素の添字。
	// 見つからない場合は -1。start は 0 以上で長さ以下。
	register("find", 2, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		if args[0].K != KString && args[0].K != KArray {
			return Null(), argErr("find", 0, at, "文字列または配列である必要があります。%s が指定されました", typeNameOf(args[0]))
		}
		start := int64(0)
		if len(args) == 3 {
			var err error
			start, err = needInt("find", args, 2, at)
			if err != nil {
				return Null(), err
			}
		}
		var n int64
		if args[0].K == KString {
			n = int64(len([]rune(args[0].S)))
		} else {
			n = int64(len(args[0].Arr.Elems))
		}
		if start < 0 || start > n {
			return Null(), argErr("find", 2, at, "start %d は範囲外です（長さ %d）", start, n)
		}
		if args[0].K == KString {
			sub, err := needString("find", args, 1, at)
			if err != nil {
				return Null(), err
			}
			r := []rune(args[0].S)
			rel := strings.Index(string(r[int(start):]), sub)
			if rel < 0 {
				return Int(-1), nil
			}
			return Int(start + int64(len([]rune(string(r[int(start):])[:rel])))), nil
		}
		for i := int(start); i < len(args[0].Arr.Elems); i++ {
			if valuesEqual(args[0].Arr.Elems[i], args[1]) {
				return Int(int64(i)), nil
			}
		}
		return Int(-1), nil
	})
}
