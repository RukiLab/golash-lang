// Runtime value model for HSP successor language.
package main

import (
	"strconv"
	"strings"
)

// Kind classifies a Value.
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
		// Internal null surfaces to scripts as void: the word null
		// never appears in user-visible output.
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

// Value is a dynamically typed value. Arrays hold shared pointers
// so `b = a` shares elements (reference semantics). Functions are not
// values: def registers a callable name, never a value.
type Value struct {
	K   Kind
	I   int64
	F   float64
	S   string
	B   bool
	Arr *Array
}

// Array is a heterogeneous resizable sequence.
type Array struct {
	Elems []Value
}

// FuncVal is a user function: top-level only, no closure. Calls run
// with the globals as the parent scope.
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

// Stringify renders a value for mes() output and error messages.
// Strings print raw; floats use the shortest round-trip form.
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

// typeNameOf reports the type name of a value for diagnostics.
func typeNameOf(v Value) string {
	return v.K.String()
}

// valuesEqual implements ==/!=. Arrays compare by reference (identity);
// deep comparison is abolished.
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
