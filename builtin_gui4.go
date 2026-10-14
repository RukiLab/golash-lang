// GUI 組込（G4 描画拡張: pngsave, paint, font）。
// 常に登録される。GUI 呼出にはウィンドウ（GUI バックエンド）が必要で、
// なければエラーを報告する。
package main

import "os"

func init() {
	// pngsave(path [, id]): 描画バッファを PNG として保存する（id は
	// 既定で現在のターゲット）。
	register("pngsave", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "pngsave")
		if err != nil {
			return Null(), err
		}
		p, err := needString("pngsave", args, 0, at)
		if err != nil {
			return Null(), err
		}
		p = in.resolvePath(p)
		id := int64(wb.CurrentSel())
		if len(args) == 2 {
			id, err = needInt("pngsave", args, 1, at)
			if err != nil {
				return Null(), err
			}
		}
		if err := wb.SavePNG(p, int(id)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// paint(x, y): シード領域を現在の色で塗りつぶす（ flood-fill ）。
	register("paint", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "paint")
		if err != nil {
			return Null(), err
		}
		x, err := needInt("paint", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("paint", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.FloodFill(int(x), int(y), wb.Foreground()); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// galpha(a): 描画全体のアルファ 0-255（図形とブリット。文字は
	// 不透明なまま）。galpha(255) で完全な不透明度に戻る。
	register("galpha", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "galpha")
		if err != nil {
			return Null(), err
		}
		a, err := needInt("galpha", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.SetAlpha(int(a)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// font(size) / font(spec) / font(spec, size): 文字フェイスの
	// サイズをピクセルで（セルグリッドもこれに応じてスケールする）。
	// spec はフォントファイルのパスまたはシステムフォント名
	// （例: "msgothic.ttc", "YuGothR.ttc"）。
	register("font", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "font")
		if err != nil {
			return Null(), err
		}
		if len(args) == 1 && args[0].K == KInt {
			if err := wb.SetFontSize(int(args[0].I)); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Null(), nil
		}
		spec, err := needString("font", args, 0, at)
		if err != nil {
			return Null(), err
		}
		// スクリプトディレクトリと作業ディレクトリが最初に検索される
		// （完全なファイル名、拡張子補完、前方一致）。次にシステムの
		// フォントディレクトリ。裸の名前はどれも一致しないと通過する。
		dirs := []string{}
		if d := in.ScriptDir(); d != "" {
			dirs = append(dirs, d)
		}
		if cwd, err := os.Getwd(); err == nil {
			dirs = append(dirs, cwd)
		}
		size := 0
		if len(args) == 2 {
			v, err := needInt("font", args, 1, at)
			if err != nil {
				return Null(), err
			}
			size = int(v)
		} else {
			size = wb.FontSize()
		}
		if err := wb.SetFontFile(spec, size, dirs...); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
