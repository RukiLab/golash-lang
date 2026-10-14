package gui

// ebitenui がクリッピングなしで描くウィジェット用のテキスト収め処理:
// ボタン幅を超えるラベルやリスト幅を超える項目は枠からはみ出すため、
// ここで省略記号付きに短縮する。描画と同じフェイスで測る。

const (
	// listSliderW は表示される垂直スライダーの幅
	// (MinHandleSize 既定 16 + トラック余白ゼロ)。水平スライダーは
	// 非表示のため予約しない。
	listSliderW = 16

	// listPadX はリストボックス内の水平余白:
	// ScrollContainerPadding(1) + EntryTextPadding(2) の両側分。
	listPadX = 8

	// buttonPadX はボタン内の水平余白:
	// TextPadding(2) の両側分に安全分を足したもの。
	buttonPadX = 6

	// ell は省略マーカー。
	ell = "..."
)

// longestEllFit は ell を後ろに付けたときに maxw に収まる r のルーン数を返し、ell 単体でも収まらない場合は -1 を返す。
func longestEllFit(r []rune, maxw int, measure func(string) int) int {
	if measure(ell) > maxw {
		return -1
	}
	lo, hi := 0, len(r)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if measure(string(r[:mid]))+measure(ell) <= maxw {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// fitText は s を省略記号付きで短縮し、幅が最大で maxw になるようにする。
// ルーン単位で保持し（CJK の文字化けなし）、既に収まっている文字列はそのまま返す。
func fitText(s string, maxw int, measure func(string) int) string {
	if maxw <= 0 || measure(s) <= maxw {
		return s
	}
	r := []rune(s)
	n := longestEllFit(r, maxw, measure)
	if n < 0 {
		return ell
	}
	return string(r[:n]) + ell
}

// fitUniqueText は fitText と同様に s を短縮するが、先行する項目が既に使った形は避ける。リスト選択はインデックス基準だが、
// EntrySelectedHandler は選ばれた項目を文字列比較でインデックスに戻すため、2つの項目が同じ短縮形になると
// どちらも最初のものに解決されてしまう。2行目をクリックすると誤った定義に飛ぶことになる。
func fitUniqueText(s string, maxw int, measure func(string) int, used map[string]bool) string {
	r := []rune(s)
	for k := longestEllFit(r, maxw, measure); k >= 0; k-- {
		cand := string(r[:k]) + ell
		if !used[cand] {
			return cand
		}
	}
	// 短縮形がすべて使われ尽くした場合は元の全文を保持する。枠からはみ出すが、スクロールマスクで切り取られ、インデックスは正しく保たれる。
	// （全文まで重複している場合はスクリプト側が重複を渡したことになり、どの表示でも区別できない。）
	return s
}

// fitItems は全項目を maxw に短縮し、順序と件数を保つことで選択インデックスが同じ項目を指し続けるようにする。異なる入力は短縮後も異なるままになり、入力スライス自体は変更しない。
func fitItems(items []string, maxw int, measure func(string) int) []string {
	out := make([]string, len(items))
	used := make(map[string]bool, len(items))
	for i, s := range items {
		if measure(s) <= maxw {
			out[i] = s
		} else {
			out[i] = fitUniqueText(s, maxw, measure, used)
		}
		used[out[i]] = true
	}
	return out
}
