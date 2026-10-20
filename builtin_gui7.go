// GUI組み込み関数（G7単色 quad 高速ラスタ）。
// 常に登録される。GUI呼び出しにはウィンドウ（GUIバックエンド）が
// 必要で、無い場合はエラーを返す。
package main

import (
	"gsh/gui"
)

func init() {
	// gflat(xs, ys, cols)：単色四角形を一括で塗る（透視補正なし）。
	//
	// xs/ysは1面あたり4値を持つ平坦配列：4隅を順番に
	// （時計回りまたは反時計回り）並べる。colsは面ごとに1つの
	// 0xRRGGBB色（gquad の lights と違い4隅展開は要らない。
	// 面内で単色に塗られる）。
	// 4N/4N/N要素の配列でN面を1命令で描画する（上限65536）。
	// 面積のない面は何も描画しない。折り返し四角は2つの三角形として
	// 描く（gquad と違いエラーにしない。呼出し側で背面カリングすること）。
	register("gflat", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gflat")
		if err != nil {
			return Null(), err
		}
		xs, err := numArrArg("gflat", args, 0, at)
		if err != nil {
			return Null(), err
		}
		ys, err := numArrArg("gflat", args, 1, at)
		if err != nil {
			return Null(), err
		}
		cols, err := colorArrArg("gflat", args, 2, at)
		if err != nil {
			return Null(), err
		}
		// すべて空は無操作。面数は xs から決まり、cols は面数と一致する。
		if len(xs) == 0 && len(ys) == 0 && len(cols) == 0 {
			return Null(), nil
		}
		n := len(xs) / 4
		if n == 0 || len(xs)%4 != 0 || len(ys) != len(xs) || len(cols) != n {
			return Null(), rtErrf(at, "gflat：xs/ys は 4 の倍数の同じ長さ、cols は面数 (%d 面) にしてください。xs=%d ys=%d cols=%d",
				n, len(xs), len(ys), len(cols))
		}
		quads := make([]gui.FlatQuad, n)
		for i := 0; i < n; i++ {
			k := i * 4
			quads[i] = gui.FlatQuad{
				X:   [4]float64{xs[k], xs[k+1], xs[k+2], xs[k+3]},
				Y:   [4]float64{ys[k], ys[k+1], ys[k+2], ys[k+3]},
				Col: rgb01(cols[i]),
			}
		}
		if err := wb.DrawFlatQuads(quads); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
