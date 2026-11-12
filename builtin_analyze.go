// エディタツール向けの組込（CUI + GUI）。
//
// 字句解析器は Golash 自体で書かれており（GolashProjects/golash-editor/lex.gsh
// の lx_lex）、Go 側では公開しない。strwidth() は自作エディタでの
// キャレット位置決め用に表示幅を測る。
package main

func init() {
	// strwidth(s): s の表示幅 — GUI の現在のフェイスではピクセル、
	// CUI ではルーン数。エディタでのキャレット位置決め用。
	register("strwidth", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("strwidth", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Int(int64(in.be.TextWidth(s))), nil
	})
}
