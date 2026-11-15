// GUI 組込（G4 描画拡張: font）。
// 常に登録される。GUI 呼出にはウィンドウ（GUI バックエンド）が必要で、
// なければエラーを報告する。
package main

import "os"

func init() {
	// font(): 現在の [spec, size] を返す。
	// font(size) / font(spec) / font(spec, size): 文字フェイスの
	// サイズをピクセルで（セルグリッドもこれに応じてスケールする）。
	// spec はフォントファイルのパスまたはシステムフォント名
	// （例: "msgothic.ttc", "YuGothR.ttc"）。
	register("font", 0, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "font")
		if err != nil {
			return Null(), err
		}
		if len(args) == 0 {
			return ArrayOf([]Value{Str(wb.FontFile()), Int(int64(wb.FontSize()))}), nil
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
