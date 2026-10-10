// Command-line interface for the Goulash language v0.2 interpreter.
//
// Usage:
//
//	gsh run <file.gsh>
//	gsh repl
//	gsh lex <file.gsh>
//	gsh parse <file.gsh>

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gsh/gui"
)

// goulashVersion は処理系の版です。版上げはこの1箇所だけ変えます。
// usage と REPL バナーはここから組み立てられます。
const goulashVersion = "0.3"

// runProgram executes prog on the register VM.
func runProgram(in *Interp, prog *Program) error {
	vprog, err := Compile(prog)
	if err != nil {
		return err
	}
	return newVmachine(in).runMain(vprog)
}

var usage = `gsh: Goulash v` + goulashVersion + ` インタプリタ

使い方:
  gsh run <file.gsh> [-- args...]   スクリプトを実行します
  gsh repl                               対話環境（REPL）を起動します
  gsh lex <file.gsh>                    字句トークン列を出力します（デバッグ用）
  gsh parse <file.gsh>                  構文木（AST）を出力します（デバッグ用）
  gsh disasm <file.gsh>                 バイトコードを逆アセンブルします（VM用）
  gsh build <file.gsh> [-o out.exe]     単一exeを生成します（バイトコード連結）

既定はレジスタVMバックエンドです（コンパイルして実行）。
GOULASH_TRACE=1 でVM命令トレースを標準エラー出力します。

実行モードはコード内の #mode cli/gui で指定します（省略時は gui で
ウィンドウを開きます）。`

func main() {
	// 単一exe（バンドル）実行：自 exe 末尾にバイトコードが連結されていれば、
	// CLI の代わりに内蔵プログラムを実行する。
	if exe, err := os.Executable(); err == nil {
		if payload, ok, berr := ExtractBundle(exe); berr != nil {
			fmt.Fprintln(os.Stderr, "エラー:", berr)
			os.Exit(1)
		} else if ok {
			runBundled(exe, payload, os.Args[1:])
			return
		}
	}
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		cmdRun(os.Args[2:])
	case "repl":
		cmdRepl()
	case "lex":
		cmdLex(os.Args[2:])
	case "parse":
		cmdParse(os.Args[2:])
	case "disasm":
		cmdDisasm(os.Args[2:])
	case "build":
		cmdBuild(os.Args[2:])
	case "help", "--help", "-h":
		fmt.Println(usage)
	default:
		fmt.Fprintf(os.Stderr, "不明なコマンド %q です\n\n%s\n", os.Args[1], usage)
		os.Exit(2)
	}
}

func cmdRun(args []string) {
	var file string
	var scriptArgs []string
	verbatim := false // after `--`: everything is a script argument
	for _, a := range args {
		if verbatim {
			scriptArgs = append(scriptArgs, a)
			continue
		}
		if a == "--" {
			verbatim = true
			continue
		}
		if file == "" && strings.HasPrefix(a, "-") {
			fmt.Fprintf(os.Stderr, "不明なフラグ %q です\n", a)
			os.Exit(2)
		}
		if file == "" {
			file = a
			continue
		}
		// Extra positionals after the file are script arguments, so
		// `run game.gsh --hard` just works (`--` still recommended
		// when an argument itself starts with `-`).
		scriptArgs = append(scriptArgs, a)
	}
	if file == "" {
		fmt.Fprintln(os.Stderr, "run にはスクリプトファイルが必要です")
		os.Exit(2)
	}
	prog, mode, err := ParseFileMode(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	// #mode cli runs console, otherwise (gui or omitted) a window opens.
	guiMode := mode == "gui"
	if guiMode {
		runGUI(file, prog, scriptArgs)
		return
	}
	in := NewInterp(os.Stdout)
	in.SetArgs(scriptArgs)
	in.SetScriptDir(scriptDirOf(file))
	if err := runProgram(in, prog); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	if code, ok := in.ExitCode(); ok {
		os.Exit(code)
	}
}

// scriptDirOf returns the absolute directory containing file (""
// when it cannot be determined); file builtins resolve relative
// paths there.
func scriptDirOf(file string) string {
	abs, err := filepath.Abs(file)
	if err != nil {
		return ""
	}
	return filepath.Dir(abs)
}

// enginePanicMsg renders a recovered engine (Ebiten) panic as one clean
// line: the first line of its message, without the Go stack trace.
// A script must never see engine internals as a crash dump.
func enginePanicMsg(r any) string {
	s := strings.TrimSpace(fmt.Sprint(r))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if s == "" {
		s = "不明なエンジンエラー"
	}
	return s
}

// runGUI executes prog in a window.
func runGUI(file string, prog *Program, scriptArgs []string) {
	runGUIWith(scriptDirOf(file), scriptArgs, func(in *Interp) error {
		return runProgram(in, prog)
	})
}

// runGUIBundled executes a bundled VM program in a window.
func runGUIBundled(exePath string, prog *VMProgram, scriptArgs []string) {
	runGUIWith(scriptDirOf(exePath), scriptArgs, func(in *Interp) error {
		return newVmachine(in).runMain(prog)
	})
}

func runGUIWith(scriptDir string, scriptArgs []string, run func(in *Interp) error) {
	wb, err := gui.New(640, 480)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	in := NewInterpWithBackend(wb, os.Stdin)
	in.SetArgs(scriptArgs)
	in.SetScriptDir(scriptDir)
	// The script runs on its own goroutine (see RunLoop) while the game
	// loop owns this one, so the result travels over a channel. Buffered
	// so a script finishing after an early window close never blocks.
	// A closed window with a still-running script reads as no error.
	runCh := make(chan error, 1)
	// Ebiten panics on engine misuse (oversized images, disposed
	// resources...), whether surfaced from the game thread or a
	// builtin on the script thread. Both become a clean error and
	// exit 1, never a Go stack trace.
	loopCode := func() (code int) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintln(os.Stderr, "エラー:", enginePanicMsg(r))
				code = 1
			}
		}()
		return gui.RunLoop(wb, func() {
			runErr := func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						err = errors.New(enginePanicMsg(r))
					}
				}()
				return run(in)
			}()
			if runErr != nil {
				// Report to the terminal and terminate the process:
				// unlike a clean finish (the window stays open), an
				// error closes the window; the exit code below is 1.
				fmt.Fprintln(os.Stderr, "エラー:", runErr)
				wb.RequestClose()
			} else {
				wb.SetDone()
			}
			runCh <- runErr
		})
	}()
	var runErr error
	select {
	case runErr = <-runCh:
	default:
	}
	if runErr != nil {
		os.Exit(1)
	}
	if code, ok := in.ExitCode(); ok {
		os.Exit(code)
	}
	os.Exit(loopCode)
}

// runBundled は連結バイトコードを実行する。引数は -- を除き
// すべてスクリプト引数になる。#mode はビルド時に記録したものを使う。
// 相対パスの基準は exe のあるディレクトリ。
func runBundled(exePath string, payload []byte, args []string) {
	prog, mode, err := UnmarshalVMProgram(payload)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	verbatim := false
	scriptArgs := []string{}
	for _, a := range args {
		if verbatim {
			scriptArgs = append(scriptArgs, a)
			continue
		}
		if a == "--" {
			verbatim = true
			continue
		}
		scriptArgs = append(scriptArgs, a)
	}
	guiMode := mode == "gui"
	if guiMode {
		runGUIBundled(exePath, prog, scriptArgs)
		return
	}
	in := NewInterp(os.Stdout)
	in.SetArgs(scriptArgs)
	in.SetScriptDir(scriptDirOf(exePath))
	if err := newVmachine(in).runMain(prog); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	if code, ok := in.ExitCode(); ok {
		os.Exit(code)
	}
}

// cmdBuild はスクリプトをコンパイルし、ランタイム exe にバイトコードを
// 連結した単一exeを生成する（`gsh build <file.gsh> [-o out.exe]`）。
func cmdBuild(args []string) {
	var file, out string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-o" || a == "--output" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "build: -o には出力先が必要です")
				os.Exit(2)
			}
			i++
			out = args[i]
			continue
		}
		if file == "" && strings.HasPrefix(a, "-") {
			fmt.Fprintf(os.Stderr, "不明なフラグ %q です\n", a)
			os.Exit(2)
		}
		if file == "" {
			file = a
			continue
		}
		fmt.Fprintf(os.Stderr, "build: 余分な引数 %q です\n", a)
		os.Exit(2)
	}
	if file == "" {
		fmt.Fprintln(os.Stderr, "build にはスクリプトファイルが必要です")
		os.Exit(2)
	}
	if out == "" {
		stem := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		if stem == "" {
			stem = "app"
		}
		out = filepath.Join(filepath.Dir(file), stem+".exe")
	}
	prog, mode, err := ParseFileMode(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	vprog, err := Compile(prog)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	payload, err := MarshalVMProgram(vprog, mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	runtime, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	if err := AppendBundle(runtime, out, payload); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	fmt.Println(out)
}

func cmdDisasm(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "disasm はファイルを 1 つだけ指定してください")
		os.Exit(2)
	}
	prog, err := ParseFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	vprog, err := Compile(prog)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	fmt.Print(Disassemble(vprog))
}

func cmdLex(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "lex はファイルを 1 つだけ指定してください")
		os.Exit(2)
	}
	toks, err := CombineFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	for _, t := range toks {
		fmt.Println(t.String())
	}
}

func cmdParse(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "parse はファイルを 1 つだけ指定してください")
		os.Exit(2)
	}
	prog, err := ParseFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	fmt.Print(prog.String())
}

// runReplProg executes one REPL input on the VM.
// The VM machine persists across inputs (globals/functions accumulate).
func runReplProg(in *Interp, vm *vmachine, prog *Program) error {
	vprog, err := Compile(prog)
	if err != nil {
		return err
	}
	return vm.runMain(vprog)
}

func cmdRepl() {
	fmt.Println("Goulash v" + goulashVersion + " REPL (type \"exit\" to quit)")
	in := NewInterp(os.Stdout)
	replVM := newVmachine(in)
	// NOTE: the REPL reads through the interpreter Backend so input()
	// shares the same stdin reader instead of competing with it.
	var buf strings.Builder
	depth := 0
	prompt := "> "
	for {
		line, ok := in.be.ReadLine(prompt)
		if !ok {
			fmt.Println()
			return
		}
		trimmed := strings.TrimSpace(line)
		if buf.Len() == 0 && (trimmed == "exit" || trimmed == ":quit" || trimmed == ":q") {
			return
		}
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(line)
		src := buf.String()
		toks, err := CombineSource("", src, "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "エラー:", err)
			buf.Reset()
			depth = 0
			prompt = "> "
			continue
		}
		depth = bracketDepth(toks)
		if depth > 0 {
			prompt = ">> "
			continue
		}
		if strings.TrimSpace(src) == "" {
			buf.Reset()
			prompt = "> "
			continue
		}
		prog, err := ParseTokens(toks)
		if err != nil {
			fmt.Fprintln(os.Stderr, "エラー:", err)
			buf.Reset()
			prompt = "> "
			continue
		}
		// Bare expressions echo their value; everything else just runs.
		if len(prog.Stmts) == 1 {
			if es, ok := prog.Stmts[0].(*ExprStmt); ok {
				v, err := replVM.evalOne(es.X, es.X.Pos())
				if err != nil {
					fmt.Fprintln(os.Stderr, "エラー:", err)
				} else if v.K != KNull {
					fmt.Println(Stringify(v))
				}
				if code, ok := in.ExitCode(); ok {
					os.Exit(code)
				}
				buf.Reset()
				prompt = "> "
				continue
			}
		}
		if err := runReplProg(in, replVM, prog); err != nil {
			fmt.Fprintln(os.Stderr, "エラー:", err)
		}
		if code, ok := in.ExitCode(); ok {
			os.Exit(code)
		}
		buf.Reset()
		prompt = "> "
	}
}

// bracketDepth counts unclosed (, [, { in a token stream.
func bracketDepth(toks []Token) int {
	d := 0
	for _, t := range toks {
		switch t.Type {
		case TokLParen, TokLBracket, TokLBrace:
			d++
		case TokRParen, TokRBracket, TokRBrace:
			d--
		}
	}
	return d
}
