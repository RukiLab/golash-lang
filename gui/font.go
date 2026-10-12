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

// System font discovery.
//
// Text is rendered with an OS system font; no font file is bundled.
// GOULASH_FONT (path to a .ttf/.otf/.ttc file) overrides discovery.
// Otherwise the first readable, parseable candidate for the current OS
// wins. Candidates are ordered by line pitch fit first: one text line is
// lineHeight tall (16px at the default 16px face size), so a font whose
// ascent+descent is exactly 16px packs lines with neither overlap nor
// gap (MS Gothic, MS Mincho, BIZ UD Gothic), while a taller line box
// (Yu Gothic ~17.6px, Noto ~19.2px, Meiryo 24px) crowds them. Character
// advances are the font's own widths — pos() places text at an exact
// pixel, so a proportional face flows naturally, it just looks less
// terminal-like than a fixed-pitch one. Fonts missing on the machine
// are skipped, then the OS UI font, and last a Latin-only fallback.
const fontEnvOverride = "GOULASH_FONT"

// systemFontCandidates lists font files in preference order.
func systemFontCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		dir := filepath.Join(root, "Fonts")
		names := []string{
			"msgothic.ttc",      // MS Gothic (default: 16px line box = the line pitch)
			"YuGothR.ttc",       // Yu Gothic Regular
			"meiryo.ttc",        // Meiryo
			"NotoSansJP-VF.ttf", // Noto Sans JP (variable font, default instance)
			"BIZ-UDGothicR.ttc", // BIZ UD Gothic
			"msmincho.ttc",      // MS Mincho
			"YuGothM.ttc",       // Yu Gothic Medium
			"segoeui.ttf",       // Latin-only last resort
		}
		out := make([]string, 0, len(names))
		for _, n := range names {
			out = append(out, filepath.Join(dir, n))
		}
		return out
	case "darwin":
		// Osaka ships with many macOS releases and matches the 8x16 cell
		// grid; the ProN files are the Roman-named Hiragino faces of
		// newer releases. Missing files are just skipped.
		return []string{
			"/System/Library/Fonts/Osaka.ttf",
			"/System/Library/Fonts/ヒラギノ角ゴ ProN W3.ttc",
			"/System/Library/Fonts/ヒラギノ角ゴシック W3.ttc",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
			"/Library/Fonts/Hiragino Sans GB.ttc",
			"/System/Library/Fonts/Supplemental/NotoSansJP-Regular.otf",
			"/System/Library/Fonts/Helvetica.ttc",
		}
	default: // linux and other unix-likes
		// Grid-fitting monospace Japanese faces first (present only when
		// their font package is installed), then Noto CJK/JP, which is
		// the common default but proportional.
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

// systemFontSource loads and caches the system font source.
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

// loadFontFile parses one font file; .ttc collections yield their
// first face (conventionally the Regular weight).
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

// loadFace builds a text face of the given pixel size from the cached
// system font.
func loadFace(size float64) (*text.GoTextFace, error) {
	src, _, err := systemFontSource()
	if err != nil {
		return nil, err
	}
	return &text.GoTextFace{Source: src, Size: size}, nil
}

// fontDirs lists directories searched for bare font file names.
// On Windows, per-user installs (%LOCALAPPDATA%\Microsoft\Windows\Fonts,
// the default for non-admin installs) are searched after the system
// directory; other OSes already list their per-user directories below.
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

// fontExts are the tried extensions for extension-less font specs,
// in preference order.
var fontExts = []string{".ttc", ".ttf", ".otf"}

// resolveFontSpec resolves a font() typeface spec to a file path:
//   - an existing path is used as-is;
//   - otherwise extension-less names are completed in each search
//     directory (extraDirs first, then the system font directories):
//     exact file names (with an extension tried when omitted), then
//     prefix matches (e.g. "PlemolJP" finds "PlemolJP-Regular.ttf"),
//     preferring the Regular weight; otherwise the first name
//     alphabetically wins. Directories keep their priority order.
// An extension-bearing name that names no file falls through to the
// directory search, so "Foo.ttf" still finds "Foo.ttf" or its Regular
// face without a path.
// extraDirs are caller-owned search roots (script directory, working
// directory); the system directories always stay in the search order.
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

// candidateBases lists the file names tried for an exact directory
// match: the name itself first, then the known font extensions when the
// spec carries none.
func candidateBases(base string) []string {
	out := []string{base}
	if filepath.Ext(base) == "" {
		for _, ext := range fontExts {
			out = append(out, base+ext)
		}
	}
	return out
}

// prefixFontMatch finds a font file whose base name starts with spec
// (case-insensitive, extension-less specs only; exact matches are
// handled before this runs). It searches dirs in order; within each
// directory the Regular weight wins, otherwise the first name
// alphabetically wins (ReadDir sorts). prefixFontMatch keeps the
// original behavior over the system font directories.
func prefixFontMatch(spec string) (string, bool) {
	return prefixFontMatchIn(spec, fontDirs())
}

// prefixFontMatchIn is prefixFontMatch over explicit directories.
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

// fontBaseNoExt strips a known font extension, reporting whether the
// name is a font file at all.
func fontBaseNoExt(name string) (string, bool) {
	for _, ext := range fontExts {
		if len(name) > len(ext) && strings.EqualFold(name[len(name)-len(ext):], ext) {
			return name[:len(name)-len(ext)], true
		}
	}
	return "", false
}

// isFontPath reports whether spec looks like a file path rather than a
// bare font name: absolute paths, paths with separators, or an
// extension-bearing name that exists relative to the working directory.
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

// loadFontSpec parses a resolved-or-bare font spec into a face source.
// extraDirs are searched before the system font directories (script
// directory, working directory).
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

// FontPath reports the system font file in use ("" if none loaded).
// Handy for diagnostics and for tests to assert discovery works.
func FontPath() string {
	src, p, _ := systemFontSource()
	if src == nil {
		return ""
	}
	return p
}
