// Array builtins: stack/queue and reshaping operations.
//
// Arrays are reference values: push/pop/insert/remove/sort/reverse
// mutate in place (as poke does). Query-style functions (join/slice)
// return new values.
package main

import (
	"fmt"
	"sort"
	"strings"
)

// maxDimTotal caps the total element count (product of sizes) for dim().
// Without a cap, a huge size panics in make() instead of returning a
// script error.
const maxDimTotal = 1 << 20

// makeDim builds nested zero-filled elements for dim().
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
	// dim(n1 [, n2, ...]): allocate a zero-filled array.
	// One size makes a flat array; more sizes nest (dim(2, 3) is 2x3).
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
			// Division keeps the product check overflow-free; a zero
			// size stays valid (dim(0) is an empty array).
			if a.I != 0 && total > maxDimTotal/a.I {
				return Null(), rtErrf(at, "dim：要素数が上限 %d を超えています", maxDimTotal)
			}
			total *= a.I
			sizes[i] = int(a.I)
		}
		return ArrayOf(makeDim(sizes)), nil
	})

	// push(arr, v...): append values, return the new length.
	register("push", 1, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("push", args, 0, at)
		if err != nil {
			return Null(), err
		}
		arr.Elems = append(arr.Elems, args[1:]...)
		return Int(int64(len(arr.Elems))), nil
	})

	// pop(arr): remove and return the last element. Empty is an error.
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

	// join(arr [, delim]): concatenate stringified elements.
	// The delimiter defaults to "" (no separator; pass "," for commas).
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

	// sort(arr): sort in place (ints or strings; mixed is an error).
	// Returns the array for chaining.
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

	// reverse(arr): reverse in place. Returns the array for chaining.
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

	// insert(arr, i, v...): insert values at index i (0..length).
	register("insert", 2, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("insert", args, 0, at)
		if err != nil {
			return Null(), err
		}
		i, err := needInt("insert", args, 1, at)
		if err != nil {
			return Null(), err
		}
		// Compare in int64: int(i) can wrap on huge i and skip the check.
		if i < 0 || i > int64(len(arr.Elems)) {
			return Null(), argErr("insert", 1, at, "添字 %d は範囲外です（長さ %d）", i, len(arr.Elems))
		}
		arr.Elems = append(arr.Elems[:int(i):int(i)], append(append([]Value{}, args[2:]...), arr.Elems[int(i):]...)...)
		return Int(int64(len(arr.Elems))), nil
	})

	// remove(arr, i [, count]): delete count elements at i, return them.
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
		// Compare in int64: i+count can wrap on huge values and skip
		// the check. Splitting keeps both comparisons overflow-free.
		if i < 0 || count < 0 || i > int64(len(arr.Elems)) || count > int64(len(arr.Elems))-i {
			return Null(), argErr("remove", 1, at, "範囲 %d から %d は範囲外です（長さ %d）", i, i+count, len(arr.Elems))
		}
		out := append([]Value(nil), arr.Elems[int(i):int(i)+int(count)]...)
		arr.Elems = append(arr.Elems[:int(i)], arr.Elems[int(i)+int(count):]...)
		return ArrayOf(out), nil
	})

	// slice(arr, start [, end]): copy of elements [start, end).
	// Negative indexes count from the end.
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

	// find(arr, v): index of v with ==, or -1 when absent.
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

// sortElems sorts numeric (int/float mixed) or all-string elements
// (stable). Anything else is an error.
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
