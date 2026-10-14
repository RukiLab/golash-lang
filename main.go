// Golash 言語インタプリタのコマンドラインインターフェース。
//
// 使い方：
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

// golashVersion は処理系の版の唯一の正本です。版上げ手順はこの1行を
// 変えて go test すること (TestVersionPinnedEverywhere が追随すべき
// ドキュメントを指摘する)。usage と REPL バナーはここから組み立て
// られます。コメント・コード内に版リテラルは書かないこと (同テストが
// 検出する)。バンドル形式の codecVersion とは独立した番号です。
const golashVersion = "0.3"

// runProgram はレジスタVM上で prog を実行します。
func runProgram(in *Interp, prog *Program) error {
	vprog, err := Compile(prog)
	if err != nil {
		return err
	}
	return newVmachine(in).runMain(vprog)
}

var usage = `gsh: Golash v` + golashVersion + ` インタプリタ

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
	verbatim := false // `--` 以降：すべてスクリプト引数扱い
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
		// ファイルより後の余分な位置引数はスクリプト引数になるため、
		// `run game.gsh --hard` はそのまま動く（引数自体が `-` で始まる
		// 場合は `--` を依然推奨する）。
		scriptArgs = append(scriptArgs, a)
	}
	if file == "" {
		fmt.Fprintln(os.Stderr, "run にはスクリプトファイルが必要です")
		os.Exit(2)
	}
	prog, mode, err := ParseFileMode(file)
	if err != nil {
		// ParseFileMode はエラー時でも意図したモードを返すため、
		// GUI スクリプトはダイアログを得る（ウィンドウはまだ存在せず、
		// 切り離し起動では見える標準エラー出力がない）。
		if mode == "gui" {
			reportGUIError(err.Error())
		} else {
			fmt.Fprintln(os.Stderr, "エラー:", err)
		}
		os.Exit(1)
	}
	// #mode cli はコンソールを実行し、それ以外（gui または指定なし）は
	// ウィンドウを開く。
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

// scriptDirOf は file を含む絶対ディレクトリを返す（決定できない場合は
// ""）。ファイル系組込はここで相対パスを解決する。
func scriptDirOf(file string) string {
	abs, err := filepath.Abs(file)
	if err != nil {
		return ""
	}
	return filepath.Dir(abs)
}

// enginePanicMsg は回復されたエンジン（Ebiten）の panic を1行の簡潔な
// メッセージに描画する: メッセージの最初の行で、Go のスタックトレースは
// 含めない。スクリプトがエンジン内部をクラッシュダンプとして見ることは
// 決してあってはならない。
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

// runGUI は prog をウィンドウで実行する。
func runGUI(file string, prog *Program, scriptArgs []string) {
	runGUIWith(scriptDirOf(file), scriptArgs, func(in *Interp) error {
		return runProgram(in, prog)
	})
}

// runGUIBundled はバンドルされた VM プログラムをウィンドウで実行する。
func runGUIBundled(exePath string, prog *VMProgram, scriptArgs []string) {
	runGUIWith(scriptDirOf(exePath), scriptArgs, func(in *Interp) error {
		return newVmachine(in).runMain(prog)
	})
}

func runGUIWith(scriptDir string, scriptArgs []string, run func(in *Interp) error) {
	wb, err := gui.New(640, 480)
	if err != nil {
		reportGUIError(err.Error())
		os.Exit(1)
	}
	in := NewInterpWithBackend(wb, os.Stdin)
	in.SetArgs(scriptArgs)
	in.SetScriptDir(scriptDir)
	// スクリプトは自身のゴルーチン（RunLoop を参照）で動き、ゲーム
	// ループがこのゴルーチンを占有するため、結果はチャネルを伝わる。
	// バッファ付きにして、早期にウィンドウが閉じた後に終了する
	// スクリプトがブロックしないようにする。まだ動作中のスクリプトが
	// ある閉じたウィンドウはエラーなしと解釈される。
	runCh := make(chan error, 1)
	// Ebiten はエンジンの誤用（大きすぎる画像、破棄されたリソース等）で
	// panic する。ゲームスレッドからもスクリプトスレッド上の組込みからも
	// 表面化する。どちらも簡潔なエラーと終了コード 1 になり、Go の
	// スタックトレースには決してならない。
	loopCode := func() (code int) {
		defer func() {
			if r := recover(); r != nil {
				reportGUIError(enginePanicMsg(r))
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
				// 端末に報告し、それをウィンドウ自体にも表示する
				// （dialog() と同じ外観）: ネイティブのポップアップは
				// ゲームウィンドウの陰に隠れうる。そして終了する:
				// 正常終了（ウィンドウは開いたまま）と違い、エラーは
				// ウィンドウを閉じる。下記の終了コードは 1。
				fmt.Fprintln(os.Stderr, "エラー:", runErr)
				showErrorInWindow(wb, runErr.Error())
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
	if final, err := AppendBundle(runtime, out, payload); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	} else if final != out {
		// 別名出力 (実行中の exe への上書き不可など)。使うには
		// 終了後にリネームする。
		fmt.Fprintf(os.Stderr, "注意: %s に書けなかったため %s に出力しました（実行中の exe への上書きはできません）。使うには終了後にリネームしてください\n", out, final)
		out = final
	} else {
		out = final
	}
	// GUI モード (#mode gui・省略時) はコンソールなし (windowsgui)
	// で出す。コンソール付きのままだとダブルクリック起動で黒窓が
	// 開き、それを閉じると同コンソールの全プロセスが道連れに
	// 終了する (エディタ本体＋実行した子)。#mode cli は端末用に
	// コンソール付きのまま。
	if mode == "gui" {
		if err := SetPEGuiSubsystem(out); err != nil {
			fmt.Fprintln(os.Stderr, "エラー:", err)
			os.Exit(1)
		}
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

// runReplProg は1つの REPL 入力を VM で実行する。
// VM マシンは入力をまたいで永続する（グローバルと関数が蓄積される）。
func runReplProg(in *Interp, vm *vmachine, prog *Program) error {
	vprog, err := Compile(prog)
	if err != nil {
		return err
	}
	return vm.runMain(vprog)
}

func cmdRepl() {
	fmt.Println("Golash v" + golashVersion + " REPL (type \"exit\" to quit)")
	in := NewInterp(os.Stdout)
	replVM := newVmachine(in)
	// 注意: REPL はインタプリタの Backend を通じて読み取るため、
	// input() は同じ stdin リーダーを共有し、それと競合しない。
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
		// 裸の式はその値をエコーする。それ以外は単に実行するだけ。
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

// bracketDepth はトークン列中の閉じられていない (, [, { を数える。
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
