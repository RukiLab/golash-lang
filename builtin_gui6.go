// GUI 組込（G6 バッチ描画: gfxquad, tri）。
// 常に登録される。GUI 呼出にはウィンドウ（GUI バックエンド）が必要で、
// なければエラーを報告する。
package main

func init() {
	// gfxquad(src, v0, v1, v2, v3):
	// バッファ src の四角形を自由四角形へ透視補正付きで写す。vi は
	// [x, y, u, v] の4要素配列（変換先 x,y と参照元 u,v、絶対ピクセル）。
	// 内部でバッファに貯めて適切なタイミング（他描画・読戻し・フレーム境界・
	// 上限・src/sel/blend 切替）でまとめて描画するため、連打しても
	// Draw 呼出は数回に畳まれる。実装はテクスチャ付き三角形2枚
	// （0,1,2 と 0,2,3）を透視補正シェーダで描く。
	// 頂点は時計回り・反時計回りの順。空の面は何も描画しない。
	// 縮退（特異・折返し・非有限重み）はアフィン変換付き三角形
	// （w=1、2三角形ともアフィン補間）にフォールバックする。
	register("gfxquad", 5, 5, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "gfxquad")
		if err != nil {
			return Null(), err
		}
		src, err := needInt("gfxquad", args, 0, at)
		if err != nil {
			return Null(), err
		}
		var dx, dy, su, sv [4]float64
		for i := 0; i < 4; i++ {
			arr, err := needArray("gfxquad", args, i+1, at)
			if err != nil {
				return Null(), err
			}
			if len(arr.Elems) != 4 {
				return Null(), rtErrf(at, "gfxquad：頂点 %d は4要素 [x, y, u, v] である必要があります（指定 %d 要素）", i, len(arr.Elems))
			}
			nums := make([]float64, 4)
			for j, e := range arr.Elems {
				switch e.K {
				case KFloat:
					nums[j] = e.F
				case KInt:
					nums[j] = float64(e.I)
				default:
					return Null(), argErr("gfxquad", i+1, at, "頂点 %d の要素 %d は数値である必要があります。%s が指定されました", i, j, typeNameOf(e))
				}
			}
			dx[i], dy[i], su[i], sv[i] = nums[0], nums[1], nums[2], nums[3]
		}
		if err := wb.QuadBuffered(int(src), dx, dy, su, sv); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// tri(x0,y0, x1,y1, x2,y2): 現在の色で塗りつぶし三角形を描く。
	// 内部でバッファに貯めて適切なタイミングでまとめて描画する。
	// 座標は絶対ピクセル（pos() 無関係）。面積なしは何も描画しない。
	register("tri", 6, 6, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "tri")
		if err != nil {
			return Null(), err
		}
		nums := make([]float64, 6)
		for i := range nums {
			v, err := needFloat("tri", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = v
		}
		x := [3]float64{nums[0], nums[2], nums[4]}
		y := [3]float64{nums[1], nums[3], nums[5]}
		if err := wb.TriBuffered(x, y, wb.Foreground()); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
