// Builtin function registry and Backend abstraction (P0).
//
// All HSP-derived functions live here (or in builtin_*.go) and are dispatched
// from evalCall. Naming follows HSP (short); signatures are modernized:
// out-params become return values, predicates return bool, failures are
// runtime errors.
//
// GUI words live in builtin_gui*.go (G1-G6):
// screen, gsel, pset, pget, line, boxf, circle, gcopy, gsquare, gtritex, gmode,
// picload, pngsave, paint, font,
// getkey, mousex, mousey, clicked,
// mmload, mmplay, mmstop, mmvol, button, pressed, inputbox, gettext,
// listbox, selected, clrobj, dialog,
// chkbox, checked, combox, mesbox, getstr, objprm.
// palette is intentionally unsupported (truecolor backend has no palette).
// Still reserved for later: mplay.
package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"gsh/gui"
)

// Backend abstracts input/output so a future GUI backend can be swapped in.
// ConsoleBackend implements it with ANSI sequences + stdio.
type Backend interface {
	Print(s string, style gui.TextStyle)
	Println(s string, style gui.TextStyle)
	Out() io.Writer
	ReadLine(prompt string) (line string, ok bool)
	Clear()
	SetColor(r, g, b, a int)
	ResetColor()
	SetTitle(s string)
	MoveTo(x, y int)
	Sleep(ms int64)
	// Await blocks for n frames (GUI: Update ticks; CUI: ~16ms each).
	Await(n int64)
	// Tick is the frame counter (GUI: Update count; CUI: wall clock / 16ms).
	Tick() int64
	// TextWidth measures display width: pixels at the GUI face,
	// rune count on CUI (for editor caret placement).
	TextWidth(s string) int
}

// immediateReader is implemented by backends with immediate-mode
// input (GUI): ReadImmediate returns typed characters since the
// previous call (IME-committed text included) without blocking.
// The input() builtin prefers it over the blocking ReadLine.
type immediateReader interface {
	ReadImmediate() string
}

// ConsoleBackend is the CUI implementation of Backend.
type ConsoleBackend struct {
	out io.Writer
	in  *bufio.Reader
}

// NewConsoleBackend wraps out/in for script IO.
func NewConsoleBackend(out io.Writer, in io.Reader) *ConsoleBackend {
	return &ConsoleBackend{out: out, in: bufio.NewReader(in)}
}

func (b *ConsoleBackend) Print(s string, style gui.TextStyle) {
	fmt.Fprint(b.out, styledCUI(s, style))
}
func (b *ConsoleBackend) Println(s string, style gui.TextStyle) {
	fmt.Fprint(b.out, styledCUI(s, style)+"\n")
}

// styledCUI wraps s in SGR style codes, switching the style back off
// afterwards without touching the persistent text color (22/23/24
// clear intensity/italic/underline only). Style 0 is byte-identical
// to the old plain output.
func styledCUI(s string, style gui.TextStyle) string {
	if style == 0 {
		return s
	}
	codes := ""
	if style&gui.StyleBold != 0 {
		codes += "\x1b[1m"
	}
	if style&gui.StyleItalic != 0 {
		codes += "\x1b[3m"
	}
	if style&gui.StyleUnderline != 0 {
		codes += "\x1b[4m"
	}
	return codes + s + "\x1b[22;23;24m"
}

// Out exposes the raw output stream (for exec passthrough).
func (b *ConsoleBackend) Out() io.Writer { return b.out }

// ReadLine prints prompt, reads one line, strips trailing \r\n.
// ok is false on EOF with no data.
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

// Clear wipes the screen and homes the cursor.
func (b *ConsoleBackend) Clear() { fmt.Fprint(b.out, "\x1b[2J\x1b[H") }

// SetColor sets subsequent text color (0-255 per channel).
// Terminals have no text alpha, so a is accepted and ignored.
func (b *ConsoleBackend) SetColor(r, g, b2, a int) {
	_ = a
	fmt.Fprintf(b.out, "\x1b[38;2;%d;%d;%dm", clamp8(r), clamp8(g), clamp8(b2))
}

// ResetColor restores the default text color.
func (b *ConsoleBackend) ResetColor() { fmt.Fprint(b.out, "\x1b[0m") }

// SetTitle asks the terminal to change its title (best effort).
func (b *ConsoleBackend) SetTitle(s string) { fmt.Fprintf(b.out, "\x1b]0;%s\x07", s) }

// MoveTo places the cursor at 0-based (x, y).
func (b *ConsoleBackend) MoveTo(x, y int) { fmt.Fprintf(b.out, "\x1b[%d;%dH", y+1, x+1) }

// Sleep pauses for ms milliseconds.
func (b *ConsoleBackend) Sleep(ms int64) { time.Sleep(time.Duration(ms) * time.Millisecond) }

// Await blocks until n frames pass (~16ms per frame on the wall clock).
func (b *ConsoleBackend) Await(n int64) {
	if n <= 0 {
		return
	}
	target := b.Tick() + n
	for b.Tick() < target {
		time.Sleep(time.Duration((target-b.Tick())*16) * time.Millisecond)
	}
}

// Tick is the wall-clock frame counter (Unix milliseconds / 16).
func (b *ConsoleBackend) Tick() int64 { return time.Now().UnixMilli() / 16 }

// TextWidth counts runes (CUI has no pixel metrics).
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

// ---------- registry ----------

// builtinFn implements one builtin. args are already evaluated.
type builtinFn func(in *Interp, args []Value, at Pos) (Value, error)

type builtinDecl struct {
	minArgs int
	maxArgs int // -1 means variadic
	fn      builtinFn
}

// builtins maps builtin names to their declarations.
var builtins = map[string]builtinDecl{}

// register adds a builtin. Called from init() in builtin_*.go files.
func register(name string, minArgs, maxArgs int, fn builtinFn) {
	builtins[name] = builtinDecl{minArgs: minArgs, maxArgs: maxArgs, fn: fn}
}

// isBuiltin reports whether name is reserved by a builtin.
func isBuiltin(name string) bool {
	_, ok := builtins[name]
	return ok
}

// builtinNames returns all registered builtin names, sorted.
func builtinNames() []string {
	names := make([]string, 0, len(builtins))
	for name := range builtins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// callBuiltin checks arity, then invokes the builtin.
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

// ---------- argument helpers (1-based positions in errors) ----------

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

// splitStyleArgs consumes trailing mes()/print() style keywords
// ("bold", "italic", "bolditalic", "underline"; exact match) and
// returns the remaining args plus the combined style. Ordinary words
// (including non-strings) always stay printable.
func splitStyleArgs(args []Value) ([]Value, gui.TextStyle) {
	var st gui.TextStyle
	n := len(args)
	for n > 0 {
		a := args[n-1]
		if a.K != KString {
			break
		}
		s, ok := gui.ParseTextStyle(a.S)
		if !ok {
			break
		}
		st |= s
		n--
	}
	return args[:n], st
}

// mes is the basic output function (variadic, space-joined + newline).
// Trailing style keywords ("bold", "italic", "bolditalic", "underline")
// are consumed as decoration instead of printed. Void arguments are
// skipped silently (they contribute nothing, not even a separator).
func init() {
	register("mes", 0, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		rest, st := splitStyleArgs(args)
		parts := make([]string, 0, len(rest))
		for _, a := range rest {
			if a.K == KNull {
				continue
			}
			parts = append(parts, Stringify(a))
		}
		in.be.Println(strings.Join(parts, " "), st)
		return Null(), nil
	})
}
