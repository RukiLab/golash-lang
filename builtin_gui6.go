// GUI組み込み関数（G6透視補正つきテクスチャラスタ）。
// 常に登録される。GUI呼び出しにはウィンドウ（GUIバックエンド）が
// 必要で、無い場合はエラーを返す。
package main

import (
	"gsh/gui"
)

func init() {
	// gquad(tex, xs, ys, us, vs, lights)：転送先四角形へ元画像の
	// 四角形を透視補正つきで貼り付ける（ホモグラフィは面ごとに
	// 解くため、スクリプト側が重みを渡す必要はない）。
	//
	// xs/ys/us/vsは1面あたり4値を持つ平坦配列：転送先の角を
	// 順番に（時計回りまたは反時計回り）と、それが参照する
	// 元画像画素を並べる。lightsは角ごとに1つの0xRRGGBB色を持ち、
	// 面内でグーロー補間される（4角同色ならフラットシェーディング）。
	// 4N要素の配列でN面を1命令で描画する（上限65536）。
	// 面積のない面は何も描画しない。
	register("gquad", 6, 6, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gquad")
		if err != nil {
			return Null(), err
		}
		src, err := needInt("gquad", args, 0, at)
		if err != nil {
			return Null(), err
		}
		xs, err := numArrArg("gquad", args, 1, at)
		if err != nil {
			return Null(), err
		}
		ys, err := numArrArg("gquad", args, 2, at)
		if err != nil {
			return Null(), err
		}
		us, err := numArrArg("gquad", args, 3, at)
		if err != nil {
			return Null(), err
		}
		vs, err := numArrArg("gquad", args, 4, at)
		if err != nil {
			return Null(), err
		}
		lights, err := colorArrArg("gquad", args, 5, at)
		if err != nil {
			return Null(), err
		}
		// 1面あたり4値、面数は全体で共通：単一面も一括も同じ
		// 規則で扱う。すべて空は無操作（全除外ループは何も描画せず、
		// バッファも含む）で、縮退面と同様。それ以外の不揃いはエラー。
		if len(xs) == 0 && len(ys) == 0 && len(us) == 0 && len(vs) == 0 && len(lights) == 0 {
			return Null(), nil
		}
		n := len(xs) / 4
		if n == 0 || len(xs)%4 != 0 || len(ys) != len(xs) || len(us) != len(xs) ||
			len(vs) != len(xs) || len(lights) != len(xs) {
			return Null(), rtErrf(at, "gquad：xs/ys/us/vs/lights は 4 の倍数の同じ長さにしてください（1面あたり4要素）。xs=%d ys=%d us=%d vs=%d lights=%d",
				len(xs), len(ys), len(us), len(vs), len(lights))
		}
		quads := make([]gui.Quad, n)
		for i := 0; i < n; i++ {
			k := i * 4
			q := gui.Quad{
				X: [4]float64{xs[k], xs[k+1], xs[k+2], xs[k+3]},
				Y: [4]float64{ys[k], ys[k+1], ys[k+2], ys[k+3]},
				U: [4]float64{us[k], us[k+1], us[k+2], us[k+3]},
				V: [4]float64{vs[k], vs[k+1], vs[k+2], vs[k+3]},
			}
			for j := 0; j < 4; j++ {
				q.Light[j] = rgb01(lights[k+j])
			}
			quads[i] = q
		}
		if err := wb.DrawQuads(int(src), quads); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}

// numArrArgはgquad()の座標配列を1つ読み取る：int/float要素の配列。
// 他の要素型は要素番号つきで拒否するため、混在バッチ
//（ループ出力に紛れた1つの文字列など）の特定が容易。
func numArrArg(name string, args []Value, i int, at Pos) ([]float64, error) {
	arr, err := needArray(name, args, i, at)
	if err != nil {
		return nil, err
	}
	out := make([]float64, len(arr.Elems))
	for j, e := range arr.Elems {
		v, ok := numValue(e)
		if !ok {
			return nil, rtErrf(at, "%s：配列の要素 %d は数値である必要があります。%s が指定されました", name, j, typeNameOf(e))
		}
		out[j] = v
	}
	return out, nil
}

// colorArrArgはgquad()のlights配列を1つ読み取る：0xRRGGBB整数の配列。
// 色はpget()の慣例に従う（color()/objprm()も同様）。
func colorArrArg(name string, args []Value, i int, at Pos) ([]int64, error) {
	arr, err := needArray(name, args, i, at)
	if err != nil {
		return nil, err
	}
	out := make([]int64, len(arr.Elems))
	for j, e := range arr.Elems {
		if e.K != KInt {
			return nil, rtErrf(at, "%s：配列の要素 %d は 0x000000〜0xFFFFFF の整数である必要があります。%s が指定されました", name, j, typeNameOf(e))
		}
		if e.I < 0 || e.I > 0xFFFFFF {
			return nil, rtErrf(at, "%s：配列の要素 %d は 0x000000〜0xFFFFFF の整数である必要があります。%d が指定されました", name, j, e.I)
		}
		out[j] = e.I
	}
	return out, nil
}

// numValueはint/floatのValueを取り出す（needFloatは引数用。配列
// 要素も引数スライスなしで同じ受け入れが必要）。
func numValue(v Value) (float64, bool) {
	switch v.K {
	case KFloat:
		return v.F, true
	case KInt:
		return float64(v.I), true
	}
	return 0, false
}

// rgb01は0xRRGGBB色を0〜1の三つ組に分解する（pgetの慣例）。
func rgb01(c int64) [3]float64 {
	return [3]float64{
		float64((c>>16)&0xFF) / 255,
		float64((c>>8)&0xFF) / 255,
		float64(c&0xFF) / 255,
	}
}
