// ビルトイン関数レジストリと Backend 抽象化（P0）。
//
// HSP 由来の関数はすべてここ（または builtin_*.go）にあり、evalCall から
// ディスパッチされる。命名は HSP 流（短縮形）を踏襲し、シグネチャは近代化している：
// out パラメータは戻り値になり、述語は整数 1/0（true=1、false=0）を返し、
// 失敗は実行時エラーになる。
//
// GUI ワードは builtin_gui*.go（G1-G6）にある：
// screen, gsel, pset, pget, line, rect, ellipse, gcopy, gblit, gmode,
// picload, pngsave, font,
// gquad, tri,
// getkey, mouse, touch,
// mmload, mmplay, mmstop, mmvol, mmpitch, button, inputbox,
// listbox, clrobj, dialog,
// chkbox, combox, mesbox, objset, objget, objenable。
// palette は意図的に未対応（truecolor バックエンドにパレットがないため）。
// 将来用に予約：mplay。
package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Backend は入出力を抽象化し、将来 GUI バックエンドに差し替え可能にする。
type Backend interface {
	Print(s string)
	Println(s string)
	Out() io.Writer
	ReadLine(prompt string) (line string, ok bool)
	Clear()
	SetColor(r, g, b, a int)
	ResetColor()
	SetTitle(s string)
	MoveTo(x, y int)
	Sleep(ms int64)
	// Awaitはnフレーム分待機する（GUI：Update刻み、CUI：1回約16ms）。
	Await(n int64)
	// Tickはフレームカウンタ（GUI：Update回数、CUI：壁時計／16ms）。
	Tick() int64
	// TextWidthは表示幅を測る。GUI見出しではピクセル、
	// CUIではルーン数（エディタのキャレット配置用）。
	TextWidth(s string) int
}

// immediateReaderは即時入力モードを持つバックエンドが実装する
// （GUI用）。ReadImmediateは前回呼び出し以降に入力された文字を
// ブロックせずに返す（IME確定済みテキストを含む）。
// input()ビルトインはブロッキング式のReadLineよりこちらを優先する。
type immediateReader interface {
	ReadImmediate() string
}

// ConsoleBackendはBackendのCUI実装。
type ConsoleBackend struct {
	out io.Writer
	in  *bufio.Reader
}

// NewConsoleBackendはスクリプト入出力用にout/inを包む。
func NewConsoleBackend(out io.Writer, in io.Reader) *ConsoleBackend {
	return &ConsoleBackend{out: out, in: bufio.NewReader(in)}
}

func (b *ConsoleBackend) Print(s string) {
	fmt.Fprint(b.out, s)
}
func (b *ConsoleBackend) Println(s string) {
	fmt.Fprint(b.out, s+"\n")
}

// Outは生の出力ストリームを公開する（exec受渡し用）。
func (b *ConsoleBackend) Out() io.Writer { return b.out }

// ReadLineはプロンプトを表示し、1行読んで末尾の\r\nを除去する。
// データなしのEOFではokが偽になる。
func (b *ConsoleBackend) ReadLine(prompt string) (string, bool) {
	if prompt != "" {
		fmt.Fprint(b.out, prompt)
	}
	line, err := b.in.ReadString('\n')
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	if err != nil {
		if len(line) == 0 {
			return "", false
		}
		return line, true
	}
	return line, true
}

// Clearは画面を消去してカーソルを先頭に戻す。
func (b *ConsoleBackend) Clear() { fmt.Fprint(b.out, "\x1b[2J\x1b[H") }

// SetColorは以降のテキスト色を設定する（チャンネル毎0-255）。
// 端末にテキストの透明度はないため、aは受け取って無視する。
func (b *ConsoleBackend) SetColor(r, g, b2, a int) {
	_ = a
	fmt.Fprintf(b.out, "\x1b[38;2;%d;%d;%dm", clamp8(r), clamp8(g), clamp8(b2))
}

// ResetColorは既定のテキスト色に戻す。
func (b *ConsoleBackend) ResetColor() { fmt.Fprint(b.out, "\x1b[0m") }

// SetTitleは端末にタイトル変更を依頼する（ベストエフォート）。
func (b *ConsoleBackend) SetTitle(s string) { fmt.Fprintf(b.out, "\x1b]0;%s\x07", s) }

// MoveToはカーソルを0始まりの(x, y)に置く。
func (b *ConsoleBackend) MoveTo(x, y int) { fmt.Fprintf(b.out, "\x1b[%d;%dH", y+1, x+1) }

// Sleepはmsミリ秒だけ停止する。
func (b *ConsoleBackend) Sleep(ms int64) { time.Sleep(time.Duration(ms) * time.Millisecond) }

// Awaitはnフレーム経過まで阻む（壁時計で1フレーム約16ms）。
func (b *ConsoleBackend) Await(n int64) {
	if n <= 0 {
		return
	}
	target := b.Tick() + n
	for b.Tick() < target {
		time.Sleep(time.Duration((target-b.Tick())*16) * time.Millisecond)
	}
}

// Tickは壁時計ベースのフレームカウンタ（UNIXミリ秒／16）。
func (b *ConsoleBackend) Tick() int64 { return time.Now().UnixMilli() / 16 }

// TextWidthはルーン数を数える（CUIにピクセル指標はない）。
func (b *ConsoleBackend) TextWidth(s string) int { return len([]rune(s)) }

func clamp8(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// ---------- レジストリ ----------

// builtinFnは1つのビルトインを実装する。argsは評価済み。
type builtinFn func(in *Interp, args []Value, at Pos) (Value, error)

type builtinDecl struct {
	minArgs int
	maxArgs int // -1は可変長
	fn      builtinFn
}

// builtinsはビルトイン名から宣言への対応表。
var builtins = map[string]builtinDecl{}

// registerはビルトインを追加する。builtin_*.go内のinit()から呼ばれる。
func register(name string, minArgs, maxArgs int, fn builtinFn) {
	builtins[name] = builtinDecl{minArgs: minArgs, maxArgs: maxArgs, fn: fn}
}

// isBuiltinはnameがビルトインで予約済みかを報告する。
func isBuiltin(name string) bool {
	_, ok := builtins[name]
	return ok
}

// builtinNamesは登録済みビルトイン名をすべてソートして返す。
func builtinNames() []string {
	names := make([]string, 0, len(builtins))
	for name := range builtins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// callBuiltinは引数個数を検査してからビルトインを呼び出す。
func callBuiltin(name string, in *Interp, args []Value, at Pos) (Value, error) {
	d := builtins[name]
	if len(args) < d.minArgs || (d.maxArgs >= 0 && len(args) > d.maxArgs) {
		if d.minArgs == d.maxArgs {
			return Null(), rtErrf(at, "%s は引数を %d 個必要としますが、%d 個が渡されました", name, d.minArgs, len(args))
		}
		if d.maxArgs < 0 {
			return Null(), rtErrf(at, "%s は少なくとも %d 個の引数が必要ですが、%d 個が渡されました", name, d.minArgs, len(args))
		}
		return Null(), rtErrf(at, "%s は %d から %d 個の引数が必要ですが、%d 個が渡されました", name, d.minArgs, d.maxArgs, len(args))
	}
	return d.fn(in, args, at)
}

// ---------- 引数ヘルパー（エラー内位置は1始まり） ----------

func argErr(name string, i int, at Pos, format string, args ...any) *RuntimeError {
	return rtErrf(at, "%s：引数 %d %s", name, i+1, fmt.Sprintf(format, args...))
}

func needInt(name string, args []Value, i int, at Pos) (int64, error) {
	if args[i].K != KInt {
		return 0, argErr(name, i, at, "整数である必要があります。%s が指定されました", typeNameOf(args[i]))
	}
	return args[i].I, nil
}

func needFloat(name string, args []Value, i int, at Pos) (float64, error) {
	switch args[i].K {
	case KFloat:
		return args[i].F, nil
	case KInt:
		return float64(args[i].I), nil
	}
	return 0, argErr(name, i, at, "数値である必要があります。%s が指定されました", typeNameOf(args[i]))
}

func needString(name string, args []Value, i int, at Pos) (string, error) {
	if args[i].K != KString {
		return "", argErr(name, i, at, "文字列である必要があります。%s が指定されました", typeNameOf(args[i]))
	}
	return args[i].S, nil
}

func needArray(name string, args []Value, i int, at Pos) (*Array, error) {
	if args[i].K != KArray {
		return nil, argErr(name, i, at, "配列である必要があります。%s が指定されました", typeNameOf(args[i]))
	}
	return args[i].Arr, nil
}

// mes は基本出力関数（単一引数＋改行）。
// void 引数は無音でスキップされる（改行のみ出力）。
func init() {
	register("mes", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		if args[0].K == KNull {
			in.be.Println("")
			return Null(), nil
		}
		in.be.Println(Stringify(args[0]))
		return Null(), nil
	})
}
