package gui

import (
	"image"
	"testing"
)

// TestIMECandidateBoundsOrigin は固定の候補ウィンドウ配置を固定する:
// 矩形が変換バーの高さに広がるため、リスト上端がバーの真下に来る。
func TestIMECandidateBoundsOrigin(t *testing.T) {
	if got, want := imeCandidateBounds(20), image.Rect(0, 0, 1, 20); got != want {
		t.Fatalf("bounds = %v, want %v", got, want)
	}
	if got := imeCandidateBounds(20); got.Max.Y != 20 {
		t.Fatalf("list opens at Max.Y = %d, want 20", got.Max.Y)
	}
}

// TestReconcileCompositionLen は空変換の修正を固定する: ebiten の
// Windows バックエンドは未確定文字列が空になった後も古い変換を
// 報告し続ける (長さゼロの COMPSTR は黙って捨てられ、キャンセルは
// 何も送らない) ため、OS 側の空で固定バー裏のミラーを消す必要がある。
func TestReconcileCompositionLen(t *testing.T) {
	for _, tc := range []struct {
		name string
		comp string
		n    int
		ok   bool
		want string
	}{
		// バグ: 古い変換 + OS 側の空 -> 消す。
		{"stale cleared", "か", 0, true, ""},
		// 変換中を OS が裏付け -> 維持。
		{"active kept", "か", 6, true, "か"},
		// OS 状態不明 (非 Windows のスタブ、失敗時) -> 維持。
		{"unknown kept", "か", 0, false, "か"},
		// 既に空なら OS 状態によらず空のまま。
		{"empty stays", "", 0, true, ""},
		{"empty unknown stays", "", 6, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reconcileCompositionLen(tc.comp, tc.n, tc.ok); got != tc.want {
				t.Fatalf("reconcileCompositionLen(%q, %d, %v) = %q, want %q",
					tc.comp, tc.n, tc.ok, got, tc.want)
			}
		})
	}
}

// TestImeClauseOf は文節切り出しを固定する: ebiten の CompositionSelection
// バイトオフセット (変換先頭からの相対) で対象の文節を選び、無効な
// 範囲や不正な切断では "" を返す。
func TestImeClauseOf(t *testing.T) {
	comp := "きょうはいい天気"
	// "いい" = bytes 12..18, runes 4..6 in the composition
	// (き0-3 ょ3-6 う6-9 は9-12 い12-15 い15-18 天18-21 気21-24).
	if got, rs, re := imeClauseOf(comp, 12, 18, true); got != "いい" || rs != 4 || re != 6 {
		t.Fatalf("clause = (%q,%d,%d), want (いい,4,6)", got, rs, re)
	}
	// 全体を文節とする場合。
	if got, rs, re := imeClauseOf(comp, 0, len(comp), true); got != comp || rs != 0 || re != 8 {
		t.Fatalf("full = (%q,%d,%d), want full,0,8", got, rs, re)
	}
	// 同じ語の繰り返しは位置で区別する: 2つ目の
	// "い" 単独はルーン 5..6 であり 4..5 ではない。
	if got, rs, re := imeClauseOf(comp, 15, 18, true); got != "い" || rs != 5 || re != 6 {
		t.Fatalf("second = (%q,%d,%d), want (い,5,6)", got, rs, re)
	}
	for _, tc := range []struct {
		name string
		cs   int
		ce   int
		ok   bool
	}{
		{"dead range", 0, 3, false},
		{"empty selection", 3, 3, true},
		{"negative start", -1, 3, true},
		{"reversed", 6, 3, true},
		{"past end", 0, len(comp) + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, rs, re := imeClauseOf(comp, tc.cs, tc.ce, tc.ok); got != "" || rs != 0 || re != 0 {
				t.Fatalf("imeClauseOf(%q, %d, %d, %v) = (%q,%d,%d), want empty",
					comp, tc.cs, tc.ce, tc.ok, got, rs, re)
			}
		})
	}
	// 文字途中での切断は正しい UTF-8 にならない: 文字化けさせない。
	if got, _, _ := imeClauseOf(comp, 0, 1, true); got != "" {
		t.Fatalf("split cut = %q, want empty", got)
	}
	if got, _, _ := imeClauseOf("", 0, 0, true); got != "" {
		t.Fatalf("empty comp = %q, want empty", got)
	}
}

// TestIMEClause は句スナップショットを固定する: 合成文字列への
// ルーンオフセット付きのテキスト。非フォーカス時の読み出しはゼロの
// まま。
func TestIMEClause(t *testing.T) {
	b := mustNew(t)
	b.imeField.Focus()
	b.mu.Lock()
	b.imeComposing = "あいう"
	b.imeClause = "い"
	b.imeClauseStart, b.imeClauseEnd = 1, 2
	b.mu.Unlock()
	text, rs, re := b.IMEClause()
	if text != "い" || rs != 1 || re != 2 {
		t.Fatalf("clause = (%q,%d,%d), want (い,1,2)", text, rs, re)
	}
	b.imeField.Blur()
	if text, rs, re := b.IMEClause(); text != "" || rs != 0 || re != 0 {
		t.Fatalf("blurred = (%q,%d,%d), want zeros", text, rs, re)
	}
}

// TestPlanEditFocus はボックスフォーカスと IME フィールドの結合を固定する:
// inputbox 内で変換が死んだ経路 (ボックスクリックでフィールドに
// フォーカスが当たらなかった) と、ime(0) を sticky に保ち、古い
// クリックレベルで変換中のフォーカスを外さないガード。
func TestPlanEditFocus(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		press, hit, hadFocus, box, field bool
		off                              bool
		wantFocus, wantBlur, wantEnable  bool
	}{
		// ボックス内クリックはボックス外で変換を走らせる。
		{"click in box", true, true, false, true, false, false, true, false, true},
		// ボックスにフォーカスがあるのにフィールドにない: 粘着的に再フォーカス。
		{"self heal", false, false, false, true, false, false, true, false, false},
		// ボックス内クリックは ime(0) の後で変換を再有効化する。
		{"click re-enables ime(0)", true, true, false, true, false, true, true, false, true},
		// ボックスフォーカスの遷移がなければ ime(0) は sticky のまま。
		{"ime(0) sticks", false, false, false, true, false, true, false, false, false},
		// フォーカス済みボックスの外側クリックで入力を終える。
		{"click outside blurs", true, false, true, false, true, false, false, true, false},
		// ime(0) ではフィールドはぼけたまま: 落とすものはない。
		{"ime(0) no blur", true, false, true, false, true, true, false, false, false},
		// フォーカス済みボックスなしでの外側クリックはフィールドに触れない
		// (即時モードの ime(1) 変換は迷いクリックでも生き残る)。
		{"outside idle", true, false, false, false, true, false, false, false, false},
		// 押下なし・ボックスフォーカスなしのフレームは何も変えない。
		{"no press", false, false, false, false, true, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := planEditFocus(tc.press, tc.hit, tc.hadFocus, tc.box, tc.field, tc.off)
			if p.focus != tc.wantFocus || p.blur != tc.wantBlur || p.enable != tc.wantEnable {
				t.Fatalf("plan = %+v, want focus=%v blur=%v enable=%v",
					p, tc.wantFocus, tc.wantBlur, tc.wantEnable)
			}
		})
	}
}

// TestEditKeysOwnedByField は Backspace/Delete/矢印の所有者を固定する:
// IME フィールドは変換中か未排出テキスト保持中のみ所有する。
// それ以外ではフィールドは空の板で飲み込んでしまう
// (空フィールドでは Backspace が無操作になる) ためボックス側が扱う。
// フォーカス済み inputbox 内で Backspace が死んだ経路がこれ。
func TestEditKeysOwnedByField(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		imeOn                    bool
		comp, fieldText, pending string
		want                     bool
	}{
		{"field off", false, "", "", "", false},
		{"idle focused box", true, "", "", "", false},
		{"converting", true, "かく", "", "", true},
		{"undrained field text", true, "", "a", "", true},
		{"undrained pending", true, "", "", "a", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := editKeysOwnedByField(tc.imeOn, tc.comp, tc.fieldText, tc.pending); got != tc.want {
				t.Fatalf("editKeysOwnedByField(%v, %q, %q, %q) = %v, want %v",
					tc.imeOn, tc.comp, tc.fieldText, tc.pending, got, tc.want)
			}
		})
	}
}

// TestCompSpans は変換の下線分割を固定する: 太い変換対象文節の
// 前後にある細い範囲、空は飛ばし、範囲外の端は丸める。
func TestCompSpans(t *testing.T) {
	spans := compSpans(5, 1, 3)
	want := []compSpan{{0, 1, false}, {1, 3, true}, {3, 5, false}}
	if len(spans) != len(want) {
		t.Fatalf("spans = %v, want %v", spans, want)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Fatalf("spans[%d] = %v, want %v", i, spans[i], want[i])
		}
	}
	// 空の文節: 細い範囲ひとつ。
	if spans := compSpans(3, 0, 0); len(spans) != 1 || spans[0] != (compSpan{0, 3, false}) {
		t.Fatalf("empty clause spans = %v", spans)
	}
	// 文字列全体が文節。
	if spans := compSpans(2, 0, 2); len(spans) != 1 || !spans[0].thick {
		t.Fatalf("full clause spans = %v", spans)
	}
	// 丸め。
	if spans := compSpans(4, -2, 99); len(spans) != 1 || spans[0] != (compSpan{0, 4, true}) {
		t.Fatalf("clamped spans = %v", spans)
	}
}

// TestReconcileCompositionIdleSkipsOS は待機時の近道を固定する:
// ebiten 側の変換がなければミラーは既に空なので OS 照会は不要で
// "" をそのまま返す。
func TestReconcileCompositionIdleSkipsOS(t *testing.T) {
	if got := reconcileComposition(""); got != "" {
		t.Fatalf("reconcileComposition(%q) = %q, want empty", "", got)
	}
}

// TestOSIMECompositionLenSmoke は OS 正解値の照会をウィンドウなしで
// 動かす: テストプロセスはトップレベルウィンドウを持たないため、長さは
// 不明でなければならない (生きた変換を消す偽の「空」も、偽の長さも出ない)。
// 非 Windows は同じ契約のスタブを使う。
func TestOSIMECompositionLenSmoke(t *testing.T) {
	if hwnds := ourWindowHwnds(); len(hwnds) != 0 {
		t.Fatalf("ourWindowHwnds() = %v, want none in tests", hwnds)
	}
	if n, ok := osIMECompositionLen(); n != 0 || ok {
		t.Fatalf("osIMECompositionLen() = (%d, %v), want (0, false) without windows", n, ok)
	}
}
