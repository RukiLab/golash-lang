package gui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// システムフォントの探索。
//
// 文字は OS のシステムフォントで描画される。フォントファイルは
// バンドルされない。GOULASH_FONT（.ttf/.otf/.ttc ファイルへのパス）が
// 探索を上書きする。さもなくば現在の OS に対する読み込めて解析できる
// 最初の候補が採用される。候補は行のピッチへの適合を優先して並ぶ:
// テキスト1行は lineHeight の高さ（既定の16pxフェイスで16px）である
// ため、ascent+descent がちょうど16pxのフォントは行を重なりも隙間も
// なく詰める（MS ゴシック、MS 明朝、BIZ UD ゴシック）。一方、行ボックス
// が高いフォント（游ゴシック 約17.6px、Noto 約19.2px、メイリオ 24px）
// は行を窮屈にする。文字の送り幅はフォント自身の幅である —
// pos() は文字を正確なピクセルに置くため、プロポーショナルなフェイス
// は自然に流れ、等幅よりは端末らしく見えないだけである。マシンに
// 無いフォントは飛ばされ、次に OS の UI フォント、最後にラテン文字
// のみのフォールバックが使われる。
const fontEnvOverride = "GOULASH_FONT"

// systemFontCandidates は優先順にフォントファイルを列挙する。
func systemFontCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		dir := filepath.Join(root, "Fonts")
		names := []string{
			"msgothic.ttc",      // MS ゴシック（既定: 16px の行ボックス = 行のピッチ）
			"YuGothR.ttc",       // 游ゴシック Regular
			"meiryo.ttc",        // メイリオ
			"NotoSansJP-VF.ttf", // Noto Sans JP（可変フォント、既定インスタンス）
			"BIZ-UDGothicR.ttc", // BIZ UD ゴシック
			"msmincho.ttc",      // MS 明朝
			"YuGothM.ttc",       // 游ゴシック Medium
			"segoeui.ttf",       // ラテン文字のみの最終手段
		}
		out := make([]string, 0, len(names))
		for _, n := range names {
			out = append(out, filepath.Join(dir, n))
		}
		return out
	case "darwin":
		// Osaka は多くの macOS リリースに同梱され、8x16 のセルグリッドに
		// 一致する。ProN ファイルは新しいリリースのローマ字名のヒラギノ
		// フェイスである。無いファイルは単に飛ばされる。
		return []string{
			"/System/Library/Fonts/Osaka.ttf",
			"/System/Library/Fonts/ヒラギノ角ゴ ProN W3.ttc",
			"/System/Library/Fonts/ヒラギノ角ゴシック W3.ttc",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
			"/Library/Fonts/Hiragino Sans GB.ttc",
			"/System/Library/Fonts/Supplemental/NotoSansJP-Regular.otf",
			"/System/Library/Fonts/Helvetica.ttc",
		}
	default: // linux およびその他の unix 系
		// グリッドに適合する等幅の日本語フォントを先に（フォント
		// パッケージがインストールされているときのみ存在）、次に
		// Noto CJK/JP。これは一般的な既定だがプロポーショナルである。
		return []string{
			"/usr/share/fonts/truetype/migu/migu-1m-regular.ttf",
			"/usr/share/fonts/truetype/vlgothic/VL-Gothic-Regular.ttf",
			"/usr/share/fonts/opentype/ipafont-gothic/ipag.ttf",
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/opentype/noto/NotoSerifCJK-Regular.ttc",
			"/usr/share/fonts/truetype/noto/NotoSansJP-Regular.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		}
	}
}

var (
	fontMu   sync.Mutex
	fontSrc  *text.GoTextFaceSource
	fontPath string
	fontErr  error
	fontDone bool
)

// systemFontSource はシステムフォントのソースを読み込んでキャッシュする。
func systemFontSource() (*text.GoTextFaceSource, string, error) {
	fontMu.Lock()
	defer fontMu.Unlock()
	if fontDone {
		return fontSrc, fontPath, fontErr
	}
	fontDone = true
	if p := os.Getenv(fontEnvOverride); p != "" {
		src, err := loadFontFile(p)
		if err != nil {
			fontErr = fmt.Errorf("font: GOULASH_FONT %q: %w", p, err)
			return nil, "", fontErr
		}
		fontSrc, fontPath = src, p
		return fontSrc, fontPath, nil
	}
	for _, p := range systemFontCandidates() {
		src, err := loadFontFile(p)
		if err != nil {
			continue
		}
		fontSrc, fontPath = src, p
		return fontSrc, fontPath, nil
	}
	fontErr = fmt.Errorf("font: no usable system font found (set GOULASH_FONT to a .ttf/.otf/.ttc file)")
	return nil, "", fontErr
}

// loadFontFile はフォントファイル1つを解析する。.ttc コレクションは
// 最初のフェイス（慣例的に Regular ウェイト）を返す。
func loadFontFile(path string) (*text.GoTextFaceSource, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".ttc") {
		srcs, err := text.NewGoTextFaceSourcesFromCollection(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		if len(srcs) == 0 {
			return nil, fmt.Errorf("font: empty collection %q", path)
		}
		return srcs[0], nil
	}
	return text.NewGoTextFaceSource(bytes.NewReader(b))
}

// loadFace はキャッシュされたシステムフォントから指定ピクセルサイズの
// テキストフェイスを構築する。
func loadFace(size float64) (*text.GoTextFace, error) {
	src, _, err := systemFontSource()
	if err != nil {
		return nil, err
	}
	return &text.GoTextFace{Source: src, Size: size}, nil
}

// fontDirs は裸のフォントファイル名に対して検索されるディレクトリを
// 列挙する。Windows ではユーザーごとのインストール先
// （%LOCALAPPDATA%\\Microsoft\\Windows\\Fonts、非管理者インストールの
// 既定）がシステムディレクトリの後に検索される。他の OS はユーザー
// ごとのディレクトリを既に下記に列挙している。
func fontDirs() []string {
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		dirs := []string{filepath.Join(root, "Fonts")}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "Microsoft", "Windows", "Fonts"))
		}
		return dirs
	case "darwin":
		return []string{
			"/System/Library/Fonts",
			"/System/Library/Fonts/Supplemental",
			"/Library/Fonts",
			filepath.Join(os.Getenv("HOME"), "Library/Fonts"),
		}
	default:
		home := os.Getenv("HOME")
		return []string{
			"/usr/share/fonts",
			"/usr/local/share/fonts",
			filepath.Join(home, ".fonts"),
			filepath.Join(home, ".local/share/fonts"),
		}
	}
}

// fontExts は拡張子なしのフォント指定に対して試される拡張子（優先順）。
var fontExts = []string{".ttc", ".ttf", ".otf"}

// resolveFontSpec は font() の書体指定をファイルパスに解決する:
//   - 存在するパスはそのまま使う。
//   - さもなくば拡張子なしの名前は各検索ディレクトリで補完される
//     （extraDirs が先、次にシステムのフォントディレクトリ）:
//     正確なファイル名（省略時に拡張子を試す）、次に前方一致
//     （例: "PlemolJP" は "PlemolJP-Regular.ttf" を見つける）で、
//     Regular ウェイトを優先する。さもなくばアルファベット順で
//     最初の名前が採用される。ディレクトリは優先順を保つ。
//
// どのファイルも指さない拡張子付きの名前はディレクトリ検索へ通り
// 抜けるため、"Foo.ttf" は "Foo.ttf" またはその Regular フェイスを
// パスなしで見つけられる。
// extraDirs は呼出し側が所有する検索ルート（スクリプトディレクトリ、
// 作業ディレクトリ）。システムディレクトリは常に検索順に残る。
func resolveFontSpec(spec string, extraDirs ...string) (string, error) {
	if spec == "" {
		return "", fmt.Errorf("font: 空のフォント指定です")
	}
	if isFontPath(spec) {
		if _, err := os.Stat(spec); err != nil {
			return "", fmt.Errorf("font: フォントファイル %q が見つかりません", spec)
		}
		return spec, nil
	}
	dirs := append([]string{}, extraDirs...)
	dirs = append(dirs, fontDirs()...)
	base := filepath.Base(spec)
	if base == "" || strings.HasSuffix(spec, "/") || strings.HasSuffix(spec, "\\") {
		return "", fmt.Errorf("font: フォント %q が見つかりません", spec)
	}
	for _, wantBase := range candidateBases(base) {
		for _, dir := range dirs {
			ents, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range ents {
				if e.IsDir() {
					continue
				}
				if strings.EqualFold(e.Name(), wantBase) {
					return filepath.Join(dir, e.Name()), nil
				}
			}
		}
	}
	if filepath.Ext(base) == "" {
		if path, ok := prefixFontMatchIn(spec, dirs); ok {
			return path, nil
		}
	}
	return "", fmt.Errorf("font: フォント %q が見つかりません", spec)
}

// candidateBases はディレクトリ内の正確な一致に対して試される
// ファイル名を列挙する: 名前自身が先、次に指定に拡張子が無い場合は
// 既知のフォント拡張子。
func candidateBases(base string) []string {
	out := []string{base}
	if filepath.Ext(base) == "" {
		for _, ext := range fontExts {
			out = append(out, base+ext)
		}
	}
	return out
}

// prefixFontMatch はベース名が spec で始まるフォントファイルを
// 見つける（大文字小文字無別。拡張子なしの指定のみ。正確な一致は
// これより前に処理される）。ディレクトリを順に検索し、各ディレクトリ内
// では Regular ウェイトが採用され、さもなくばアルファベット順で最初の
// 名前が採用される（ReadDir はソート済み）。prefixFontMatch は
// システムのフォントディレクトリに対する従来の挙動を保つ。
func prefixFontMatch(spec string) (string, bool) {
	return prefixFontMatchIn(spec, fontDirs())
}

// prefixFontMatchIn は明示的なディレクトリに対する prefixFontMatch。
func prefixFontMatchIn(spec string, dirs []string) (string, bool) {
	var first string
	rs := []rune(spec)
	for _, dir := range dirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		dirFirst := ""
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			base, ok := fontBaseNoExt(e.Name())
			if !ok {
				continue
			}
			rb := []rune(base)
			if len(rb) <= len(rs) || !strings.EqualFold(string(rb[:len(rs)]), spec) {
				continue
			}
			cand := filepath.Join(dir, e.Name())
			if dirFirst == "" {
				dirFirst = cand
			}
			if rem := string(rb[len(rs):]); strings.EqualFold(rem, "-regular") || strings.EqualFold(rem, "regular") {
				return cand, true
			}
		}
		if first == "" {
			first = dirFirst
		}
	}
	return first, first != ""
}

// fontBaseNoExt は既知のフォント拡張子を除去し、その名前がフォント
// ファイルであるかを報告する。
func fontBaseNoExt(name string) (string, bool) {
	for _, ext := range fontExts {
		if len(name) > len(ext) && strings.EqualFold(name[len(name)-len(ext):], ext) {
			return name[:len(name)-len(ext)], true
		}
	}
	return "", false
}

// isFontPath は spec がファイルパスのように見えるか、裸のフォント名
// かを報告する: 絶対パス、区切りを含むパス、または作業ディレクトリに
// 対して存在する拡張子付きの名前。
func isFontPath(spec string) bool {
	if filepath.IsAbs(spec) || strings.ContainsAny(spec, `/\`) {
		return true
	}
	if filepath.Ext(spec) != "" {
		if _, err := os.Stat(spec); err == nil {
			return true
		}
	}
	return false
}

// loadFontSpec は解決済みまたは裸のフォント指定をフェイスのソースに
// 解析する。extraDirs はシステムのフォントディレクトリより先に検索
// される（スクリプトディレクトリ、作業ディレクトリ）。
func loadFontSpec(spec string, extraDirs ...string) (*text.GoTextFaceSource, string, error) {
	path, err := resolveFontSpec(spec, extraDirs...)
	if err != nil {
		return nil, "", err
	}
	src, err := loadFontFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("font: %q を読み込めません: %w", path, err)
	}
	return src, path, nil
}

// FontPath は使用中のシステムフォントファイルを報告する（読み込まれて
// いなければ ""）。診断や、探索が動くことをテストで確認するのに便利。
func FontPath() string {
	src, p, _ := systemFontSource()
	if src == nil {
		return ""
	}
	return p
}
