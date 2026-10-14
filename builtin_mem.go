// P6 組み込み: 整数配列をバイトバッファとしての peek/poke (リトルエンディアン)。
package main

func init() {
	// peek(arr, i): 1バイト (0-255)。
	register("peek", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, err := peekArgs("peek", args, at, 1)
		if err != nil {
			return Null(), err
		}
		return Int(arr.Elems[i].I), nil
	})

	// wpeek(arr, i): 2バイト、リトルエンディアン (0-65535)。
	register("wpeek", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, err := peekArgs("wpeek", args, at, 2)
		if err != nil {
			return Null(), err
		}
		return Int(arr.Elems[i].I | arr.Elems[i+1].I<<8), nil
	})

	// lpeek(arr, i): 4バイト、リトルエンディアン。
	register("lpeek", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, err := peekArgs("lpeek", args, at, 4)
		if err != nil {
			return Null(), err
		}
		v := arr.Elems[i].I | arr.Elems[i+1].I<<8 |
			arr.Elems[i+2].I<<16 | arr.Elems[i+3].I<<24
		return Int(v), nil
	})

	// poke(arr, i, v): 1バイト書き込み (0-255)。
	register("poke", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, v, err := pokeArgs("poke", args, at, 1)
		if err != nil {
			return Null(), err
		}
		arr.Elems[i] = Int(v & 0xFF)
		return Null(), nil
	})

	// wpoke(arr, i, v): 2バイト書き込み、リトルエンディアン (0-65535)。
	register("wpoke", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, v, err := pokeArgs("wpoke", args, at, 2)
		if err != nil {
			return Null(), err
		}
		arr.Elems[i] = Int(v & 0xFF)
		arr.Elems[i+1] = Int((v >> 8) & 0xFF)
		return Null(), nil
	})

	// lpoke(arr, i, v): 4バイト書き込み、リトルエンディアン (0..2^32-1)。
	register("lpoke", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, v, err := pokeArgs("lpoke", args, at, 4)
		if err != nil {
			return Null(), err
		}
		for k := 0; k < 4; k++ {
			arr.Elems[i+k] = Int((v >> (8 * k)) & 0xFF)
		}
		return Null(), nil
	})
}

// peekArgs は (配列, インデックス) を検証し、n バイト分の境界を
// 確認する。要素はあらかじめバイト値である必要があり、範囲外の
// 読み出しはエラーになる。
func peekArgs(name string, args []Value, at Pos, n int) (*Array, int, error) {
	arr, err := needArray(name, args, 0, at)
	if err != nil {
		return nil, 0, err
	}
	i, err := needInt(name, args, 1, at)
	if err != nil {
		return nil, 0, err
	}
	// int64 で比較する: int(i)+n は巨大な i で桁あふれし、検査を飛ばす
	// 可能性がある。
	if i < 0 || i > int64(len(arr.Elems))-int64(n) {
		return nil, 0, argErr(name, 1, at, "バイト %d から %d は範囲外です（長さ %d）", i, i+int64(n)-1, len(arr.Elems))
	}
	for k := 0; k < n; k++ {
		e := arr.Elems[int(i)+k]
		if e.K != KInt || e.I < 0 || e.I > 255 {
			return nil, 0, rtErrf(at, "%s：要素 %d はバイト（0 から 255）ではありません", name, int(i)+k)
		}
	}
	return arr, int(i), nil
}

// pokeArgs は (配列, インデックス, 値) を n バイト書き込み用に検証する。
func pokeArgs(name string, args []Value, at Pos, n int) (*Array, int, int64, error) {
	arr, err := needArray(name, args, 0, at)
	if err != nil {
		return nil, 0, 0, err
	}
	i, err := needInt(name, args, 1, at)
	if err != nil {
		return nil, 0, 0, err
	}
	v, err := needInt(name, args, 2, at)
	if err != nil {
		return nil, 0, 0, err
	}
	max := int64(1)<<(8*n) - 1
	if v < 0 || v > max {
		return nil, 0, 0, argErr(name, 2, at, "値は 0 から %d の範囲で指定してください。%d が指定されました", max, v)
	}
	// int64 で比較する: int(i)+n は巨大な i で桁あふれし、検査を飛ばす
	// 可能性がある。
	if i < 0 || i > int64(len(arr.Elems))-int64(n) {
		return nil, 0, 0, argErr(name, 1, at, "バイト %d から %d は範囲外です（長さ %d）", i, i+int64(n)-1, len(arr.Elems))
	}
	return arr, int(i), v, nil
}
