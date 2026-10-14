// HSP 後継言語の実行時値モデル。
package main

import (
	"strconv"
	"strings"
)

// Kind は Value を分類する。
type Kind int

const (
	KNull Kind = iota
	KInt
	KFloat
	KString
	KBool
	KArray
)

func (k Kind) String() string {
	switch k {
	case KNull:
		// 内部的な null はスクリプトからは void として見える。語 null は
		// ユーザーに見える出力に現れることはない。
		return "void"
	case KInt:
		return "int"
	case KFloat:
		return "float"
	case KString:
		return "string"
	case KBool:
		return "bool"
	case KArray:
		return "array"
	}
	return "unknown"
}

// Value は動的型付けの値。配列は共有ポインタを保持するため
// `b = a` は要素を共有する（参照意味論）。関数は値ではない:
// def は呼出し可能な名前を登録するだけで、値を登録することはない。
type Value struct {
	K   Kind
	I   int64
	F   float64
	S   string
	B   bool
	Arr *Array
}

// Array は異種混合の可変長列。
type Array struct {
	Elems []Value
}

// FuncVal はユーザー定義関数: トップレベルのみで、クロージャは持たない。
// 呼出はグローバルを親スコープとして実行される。
type FuncVal struct {
	Name   string
	Params []Param
	Body   *BlockStmt
}

func Null() Value             { return Value{K: KNull} }
func Int(v int64) Value       { return Value{K: KInt, I: v} }
func Float(v float64) Value   { return Value{K: KFloat, F: v} }
func Str(v string) Value      { return Value{K: KString, S: v} }
func Bool(v bool) Value       { return Value{K: KBool, B: v} }
func ArrayOf(e []Value) Value { return Value{K: KArray, Arr: &Array{Elems: e}} }

// Stringify は値を mes() 出力やエラーメッセージ用に描画する。
// 文字列はそのまま印字され、浮動小数点は最短のラウンドトリップ形式を使う。
func Stringify(v Value) string {
	switch v.K {
	case KNull:
		return "null"
	case KInt:
		return strconv.FormatInt(v.I, 10)
	case KFloat:
		return strconv.FormatFloat(v.F, 'g', -1, 64)
	case KString:
		return v.S
	case KBool:
		if v.B {
			return "true"
		}
		return "false"
	case KArray:
		parts := make([]string, len(v.Arr.Elems))
		for i, e := range v.Arr.Elems {
			parts[i] = Stringify(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return "unknown"
}

// typeNameOf は診断用に値の型名を返す。
func typeNameOf(v Value) string {
	return v.K.String()
}

// valuesEqual は ==/!= を実装する。配列は参照（同一性）で比較され、
// 深い比較は廃止された。
func valuesEqual(a, b Value) bool {
	if a.K == KInt && b.K == KFloat {
		return float64(a.I) == b.F
	}
	if a.K == KFloat && b.K == KInt {
		return a.F == float64(b.I)
	}
	if a.K != b.K {
		return false
	}
	switch a.K {
	case KNull:
		return true
	case KInt:
		return a.I == b.I
	case KFloat:
		return a.F == b.F
	case KString:
		return a.S == b.S
	case KBool:
		return a.B == b.B
	case KArray:
		return a.Arr == b.Arr
	}
	return false
}
