// P1 組み込み: 変換、数学、乱数。
package main

import (
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

func init() {
	// int(v): 浮動小数は 0 方向へ切り捨て (有限かつ int64 範囲内);
	// 文字列は 10 進整数であること; 真偽値は 1/0 に対応。
	// それ以外はエラー。
	register("int", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		switch args[0].K {
		case KInt:
			return args[0], nil
		case KFloat:
			f := args[0].F
			if math.IsNaN(f) || f >= 9223372036854775808.0 || f < -9223372036854775808.0 {
				return Null(), argErr("int", 0, at, "%g を整数に変換できません", f)
			}
			return Int(int64(f)), nil
		case KBool:
			if args[0].B {
				return Int(1), nil
			}
			return Int(0), nil
		case KString:
			s := strings.TrimSpace(args[0].S)
			if v, err := strconv.ParseInt(s, 10, 64); err == nil {
				return Int(v), nil
			}
			return Null(), argErr("int", 0, at, "%q を整数に変換できません", args[0].S)
		}
		return Null(), argErr("int", 0, at, "%s を整数に変換できません", typeNameOf(args[0]))
	})

	// float(v): 整数は拡大変換; 数値文字列を解釈 (有限のみ);
	// 真偽値は 1.0/0.0 に対応。
	register("float", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		switch args[0].K {
		case KFloat:
			return args[0], nil
		case KInt:
			return Float(float64(args[0].I)), nil
		case KBool:
			if args[0].B {
				return Float(1), nil
			}
			return Float(0), nil
		case KString:
			s := strings.TrimSpace(args[0].S)
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				if math.IsNaN(f) || math.IsInf(f, 0) {
					return Null(), argErr("float", 0, at, "%q を浮動小数に変換できません", args[0].S)
				}
				return Float(f), nil
			}
			return Null(), argErr("float", 0, at, "%q を浮動小数に変換できません", args[0].S)
		}
		return Null(), argErr("float", 0, at, "%s を浮動小数に変換できません", typeNameOf(args[0]))
	})

	// str(v): mes() の表示と同じ形式で任意の値を描画する。
	register("str", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		return Str(Stringify(args[0])), nil
	})

	// vartype(v): 型名 ("int", "float", "string", "bool", "array",
	// "map", "function", "null")。
	register("vartype", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		return Str(typeNameOf(args[0])), nil
	})

	// abs(v): 絶対値、int/float を保持する。
	// abs(MinInt64) はエラー: 否定すると折り返し、表現可能な
	// 絶対値が存在しないため。
	register("abs", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		switch args[0].K {
		case KInt:
			if args[0].I < 0 {
				if args[0].I == math.MinInt64 {
					return Null(), argErr("abs", 0, at, "%d の絶対値を取得できません", args[0].I)
				}
				return Int(-args[0].I), nil
			}
			return args[0], nil
		case KFloat:
			return Float(math.Abs(args[0].F)), nil
		}
		return Null(), argErr("abs", 0, at, "数値である必要があります。%s が指定されました", typeNameOf(args[0]))
	})

	// sqrt(v): 平方根を float で返す; 負数はエラー。
	register("sqrt", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		f, err := needFloat("sqrt", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if f < 0 {
			return Null(), argErr("sqrt", 0, at, "%g の平方根を取得できません", f)
		}
		return Float(math.Sqrt(f)), nil
	})

	// 三角関数 (HSP 同様ラジアン)。
	for name, fn := range map[string]func(float64) float64{
		"sin": math.Sin, "cos": math.Cos, "tan": math.Tan,
	} {
		register(name, 1, 1, trigFn(name, fn))
	}

	// atan(y) または atan(y, x): 逆正接、引数 2 個では atan2。
	register("atan", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		y, err := needFloat("atan", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if len(args) == 2 {
			x, err := needFloat("atan", args, 1, at)
			if err != nil {
				return Null(), err
			}
			return Float(math.Atan2(y, x)), nil
		}
		return Float(math.Atan(y)), nil
	})

	// exp(v): e の v 乗。log(v): 自然対数 (v は正であること)。
	register("exp", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		f, err := needFloat("exp", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return finiteMath("exp", math.Exp(f), at)
	})
	register("log", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		f, err := needFloat("log", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if f <= 0 {
			return Null(), argErr("log", 0, at, "対数は正の数が必要です。%g が指定されました", f)
		}
		return Float(math.Log(f)), nil
	})

	// pow(base, exp): べき乗を float で返す。
	register("pow", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		b, err := needFloat("pow", args, 0, at)
		if err != nil {
			return Null(), err
		}
		e, err := needFloat("pow", args, 1, at)
		if err != nil {
			return Null(), err
		}
		return finiteMath("pow", math.Pow(b, e), at)
	})

	// limit(v, lo, hi): 範囲内に収める。全引数が int の場合は
	// int64 で収めて int を返す; それ以外は float64。
	register("limit", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		intPath := true
		for _, a := range args {
			if a.K != KInt {
				intPath = false
				break
			}
		}
		if intPath {
			lo, hi, c := args[1].I, args[2].I, args[0].I
			if lo > hi {
				return Null(), rtErrf(at, "limit：下限 %d が上限 %d を超えています", lo, hi)
			}
			if c < lo {
				c = lo
			}
			if c > hi {
				c = hi
			}
			return Int(c), nil
		}
		nums := make([]float64, 3)
		for i := range args {
			f, err := needFloat("limit", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = f
		}
		lo, hi, c := nums[1], nums[2], nums[0]
		if lo > hi {
			return Null(), rtErrf(at, "limit：下限 %g が上限 %g を超えています", lo, hi)
		}
		if c < lo {
			c = lo
		}
		if c > hi {
			c = hi
		}
		return Float(c), nil
	})

	// min(a, b, ...) / max(a, b, ...): 最小・最大の引数。
	// 全引数が int の場合は int を返す; それ以外は float64。
	for name, wantMin := range map[string]bool{"min": true, "max": false} {
		register(name, 2, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
			intPath := true
			for _, a := range args {
				if a.K != KInt {
					intPath = false
					break
				}
			}
			if intPath {
				best := args[0].I
				for _, a := range args[1:] {
					if wantMin {
						if a.I < best {
							best = a.I
						}
					} else if a.I > best {
						best = a.I
					}
				}
				return Int(best), nil
			}
			nums := make([]float64, len(args))
			for i := range args {
				f, err := needFloat(name, args, i, at)
				if err != nil {
					return Null(), err
				}
				nums[i] = f
			}
			best := nums[0]
			for _, f := range nums[1:] {
				if wantMin {
					if f < best {
						best = f
					}
				} else if f > best {
					best = f
				}
			}
			return Float(best), nil
		})
	}

	// length(a): 最上位の要素数。入れ子は length(a[i]) で取得。
	register("length", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("length", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Int(int64(len(arr.Elems))), nil
	})

	// rnd(n): 0..n-1 の整数。
	register("rnd", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		n, err := needInt("rnd", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if n <= 0 {
			return Null(), argErr("rnd", 0, at, "正の上限が必要です。%d が指定されました", n)
		}
		return Int(int64(scriptRand.IntN(int(n)))), nil
	})

	// randomize([seed]): 引数なしでは時計から種を設定; 整数の
	// 種を指定すると系列が再現可能になる。
	register("randomize", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		if len(args) == 0 {
			scriptRand = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))
			return Null(), nil
		}
		seed, err := needInt("randomize", args, 0, at)
		if err != nil {
			return Null(), err
		}
		scriptRand = rand.New(rand.NewPCG(uint64(seed), 0))
		return Null(), nil
	})
}

func trigFn(name string, fn func(float64) float64) builtinFn {
	return func(in *Interp, args []Value, at Pos) (Value, error) {
		f, err := needFloat(name, args, 0, at)
		if err != nil {
			return Null(), err
		}
		return finiteMath(name, fn(f), at)
	}
}

// finiteMath は有限でない浮動小数結果を拒否する: NaN と ±Inf は
// 値として現れ得ないため、生成は常にエラーになる。
func finiteMath(name string, f float64, at Pos) (Value, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return Null(), argErr(name, 0, at, "計算結果が有限の浮動小数ではありません")
	}
	return Float(f), nil
}

// scriptRand はスクリプト可視の乱数源 (randomize で種を再設定)。
var scriptRand = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))
