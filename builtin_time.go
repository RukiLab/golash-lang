// P3 組み込み関数：時刻・待機・プログラム制御。
package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// endSignal は Run まで巻き戻し、要求された終了コードを報告させる。
type endSignal struct{}

// syncWriter はプロセス内の一つの書き込み先への書き込みを直列化する。exec()
// は子の出力をインタプリタ自身の writer 経由で流し、os/exec は別 goroutine
// からコピーするため、同期しない受け側（テスト／取り込み用の bytes.Buffer）は
// その競合で書き込みを取りこぼし・化けさせる（この guard 以前は TestExec が約10%で不安定化）。
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// childOut は子プロセス用にインタプリタ側の writer を包む。本物の
// *os.File はそのまま通す：子がハンドルを直接継承し、このプロセス内の
// goroutine から書き込まれることはない。それ以外は mutex で守る。os/exec が
// 子の出力をプロセス内でコピーする一方、インタプリタも書き続けるため。
func childOut(w io.Writer) io.Writer {
	if _, ok := w.(*os.File); ok {
		return w
	}
	return &syncWriter{w: w}
}

// procStart は nanotime() を単調時計に固定する起点。
var procStart = time.Now()

func init() {
	// gettime(t)：0 年、1 月（1-12）、2 曜日（0=日曜..6=土曜）、
	// 3 日、4 時、5 分、6 秒、7 ミリ秒。
	register("gettime", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		t, err := needInt("gettime", args, 0, at)
		if err != nil {
			return Null(), err
		}
		now := time.Now()
		switch t {
		case 0:
			return Int(int64(now.Year())), nil
		case 1:
			return Int(int64(now.Month())), nil
		case 2:
			return Int(int64(now.Weekday())), nil
		case 3:
			return Int(int64(now.Day())), nil
		case 4:
			return Int(int64(now.Hour())), nil
		case 5:
			return Int(int64(now.Minute())), nil
		case 6:
			return Int(int64(now.Second())), nil
		case 7:
			return Int(int64(now.Nanosecond() / 1e6)), nil
		}
		return Null(), argErr("gettime", 0, at, "type は 0 から 7 の範囲で指定してください。%d が指定されました", t)
	})

	// nanotime()：プロセス開始からの単調な経過ナノ秒（計測用。
	// 壁時計のミリ秒は gettime(7) を使う）。
	// time.Since は単調時計を使うため、逆戻りしない。
	register("nanotime", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		return Int(time.Since(procStart).Nanoseconds()), nil
	})

	// sleep(ms)：ミリ秒だけ休止する（HSP のセンチ秒 wait の置き換え）。
	register("sleep", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		ms, err := needInt("sleep", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if ms < 0 {
			return Null(), argErr("sleep", 0, at, "0 以上である必要があります。%d が指定されました", ms)
		}
		// time.Duration(ms)*time.Millisecond が int64 ナノ秒に収まる上限。
		// 超えると負数にラップして即時復帰してしまうためエラーにする。
		if ms > math.MaxInt64/int64(time.Millisecond) {
			return Null(), argErr("sleep", 0, at, "待機ミリ秒数が大きすぎます。%d が指定されました", ms)
		}
		in.be.Sleep(ms)
		return Null(), nil
	})

	// await([n])：n フレーム待つ（既定は 1）。GUI は Update の
	// 刻み（vsync）を待ち、CUI は1フレーム約16ms 待つ。
	register("await", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		n := int64(1)
		if len(args) == 1 {
			var err error
			n, err = needInt("await", args, 0, at)
			if err != nil {
				return Null(), err
			}
			if n < 0 {
				return Null(), argErr("await", 0, at, "0 以上である必要があります。%d が指定されました", n)
			}
			// tick+n は表現可能な範囲に収める：桁あふれした目標値は
			// 通過済みに見えて即時復帰してしまっていた。
			if n > math.MaxInt64-in.be.Tick() {
				return Null(), argErr("await", 0, at, "待機tick数が大きすぎます。%d が指定されました", n)
			}
		}
		in.be.Await(n)
		return Null(), nil
	})

	// tick()：フレーム計数（GUI は Update 回数、CUI は壁時計／16ms）。
	register("tick", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		return Int(in.be.Tick()), nil
	})

	// end([code])：終了コード付きでプログラムを止める（既定は 0）。
	register("end", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		code := int64(0)
		if len(args) == 1 {
			var err error
			code, err = needInt("end", args, 0, at)
			if err != nil {
				return Null(), err
			}
		}
		c := int(code)
		in.exitCode.Store(&c)
		// GUI バックエンドは end() でウィンドウを閉じる。コンソールは
		// 単に巻き戻るだけ。
		if closer, ok := in.be.(interface{ RequestClose() }); ok {
			closer.RequestClose()
		}
		panic(endSignal{})
	})

	// assert(cond [, msg]): cond が偽のときエラーで中断する。
	register("assert", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		if args[0].K != KBool {
			return Null(), argErr("assert", 0, at, "条件式は bool 型である必要があります。%s が指定されました", typeNameOf(args[0]))
		}
		if !args[0].B {
			msg := "assertion failed"
			if len(args) == 2 {
				m, err := needString("assert", args, 1, at)
				if err != nil {
					return Null(), err
				}
				msg = m
			}
			return Null(), rtErrf(at, "%s", msg)
		}
		return Null(), nil
	})

	// logmes(args...): mes 形式の出力をエラーストリームに出す（デバッグ用）。
	// void 引数は mes()/print() と同じく無音でスキップされる。
	register("logmes", 0, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		parts := make([]string, 0, len(args))
		for _, a := range args {
			if a.K == KNull {
				continue
			}
			parts = append(parts, Stringify(a))
		}
		fmt.Fprintln(in.errOut, strings.Join(parts, " "))
		return Null(), nil
	})

	// args(): スクリプトファイル（と `--`）より後のコマンドライン引数を
	// 文字列配列で返す。指定がなければ空。
	register("args", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		elems := make([]Value, len(in.cliArgs))
		for i, s := range in.cliArgs {
			elems[i] = Str(s)
		}
		return ArrayOf(elems), nil
	})

	// exec(name, args...): 外部コマンドを非同期に起動し、起動すると 0 を
	// 返す（exec が返った後も動き続ける）。標準出力・標準エラーは
	// そのまま通過する。起動失敗はエラー。pipeexec() は同期待ちの双子で、
	// 完了を待って標準出力を取り込む。
	// 子プロセスは親のコンソールを共有する（Windows）: そのコンソール
	// ウィンドウを閉じると接続された全プロセスが終了する。単独で生き残る
	// 必要のある GUI 子プロセスは代わりに execgui() を使う。
	register("exec", 1, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		name, err := needString("exec", args, 0, at)
		if err != nil {
			return Null(), err
		}
		argv := make([]string, len(args)-1)
		for i := range args[1:] {
			s, err := needString("exec", args, i+1, at)
			if err != nil {
				return Null(), err
			}
			argv[i] = s
		}
		cmd := exec.Command(name, argv...)
		cmd.Stdin = in.in
		cmd.Stdout = childOut(in.be.Out())
		cmd.Stderr = childOut(in.errOut)
		if err := cmd.Start(); err != nil {
			return Null(), rtErrf(at, "exec：%s", err.Error())
		}
		// バックグラウンドで刈り取る。出力は既に流れている。
		go func() { _ = cmd.Wait() }()
		return Int(0), nil
	})

	// execgui(name, args...): exec() と同じ（非同期・起動すると 0 を返す・
	// 起動失敗はエラー）だが、子プロセスにコンソールウィンドウを与えない
	// （Windows の DETACHED_PROCESS。他プラットフォームでは何もしない）。
	// GUI プログラムから起動される GUI 子プロセス用: これがないと子は親の
	// コンソールを共有し、そのコンソールウィンドウを閉じると両プロセスが
	// 一緒に終了してしまう。標準出力・標準エラーは依然継承される（親に
	// コンソールがなければ破棄される）が、何も取り込まれない。出力が必要
	// なら piperun() を使う。
	register("execgui", 1, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		name, err := needString("execgui", args, 0, at)
		if err != nil {
			return Null(), err
		}
		argv := make([]string, len(args)-1)
		for i := range args[1:] {
			s, err := needString("execgui", args, i+1, at)
			if err != nil {
				return Null(), err
			}
			argv[i] = s
		}
		cmd := exec.Command(name, argv...)
		detachConsole(cmd)
		cmd.Stdin = in.in
		cmd.Stdout = childOut(in.be.Out())
		cmd.Stderr = childOut(in.errOut)
		if err := cmd.Start(); err != nil {
			return Null(), rtErrf(at, "execgui：%s", err.Error())
		}
		// バックグラウンドで刈り取る。出力は既に流れている。
		go func() { _ = cmd.Wait() }()
		return Int(0), nil
	})

	// piperun(name, args...): 外部コマンドを実行して取り込み、
	// [終了コード, 標準出力, 標準エラー] を返す。pipeexec() と違い、
	// 非ゼロ終了は値（エラーではない）で、標準エラーは通過させず
	// 取り込むため、エディタは失敗を（try/catch で包めない
	// pipeexec のエラーに死ぬことなく）ウィンドウ内で示せる。
	// コマンドの起動失敗だけが値に [-1, "", message] として畳まれる。
	// 標準入力は pipeexec と同じく切り離される（null）。完了は待たれる
	// （ブロッキング、タイムアウトなし）。子プロセスにはコンソール
	// ウィンドウが与えられない（Windows。取り込みはパイプを使う）。
	register("piperun", 1, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		name, err := needString("piperun", args, 0, at)
		if err != nil {
			return Null(), err
		}
		argv := make([]string, len(args)-1)
		for i := range args[1:] {
			s, err := needString("piperun", args, i+1, at)
			if err != nil {
				return Null(), err
			}
			argv[i] = s
		}
		cmd := exec.Command(name, argv...)
		detachConsole(cmd)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				return ArrayOf([]Value{Int(int64(exit.ExitCode())), Str(stdout.String()), Str(stderr.String())}), nil
			}
			return ArrayOf([]Value{Int(-1), Str(""), Str(fmt.Sprintf("piperun：%s", err.Error()))}), nil
		}
		return ArrayOf([]Value{Int(0), Str(stdout.String()), Str(stderr.String())}), nil
	})

	// pipeexec(name, args...): 外部コマンドを実行し、その標準出力を
	// 文字列で返す。標準入力は（null デバイスで）切り離されるため、
	// 子プロセスがスクリプトの入力を横取りすることはない。標準エラーは
	// 通過する。起動失敗、または非ゼロ終了はエラー（単純な exec() の
	// 双子は出力を取り込まずコードを返す）。エラー出力を値として得るには
	// 代わりに piperun() を使う。子プロセスにはコンソールウィンドウが
	// 与えられない（Windows。取り込みはパイプを使う）。
	register("pipeexec", 1, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		name, err := needString("pipeexec", args, 0, at)
		if err != nil {
			return Null(), err
		}
		argv := make([]string, len(args)-1)
		for i := range args[1:] {
			s, err := needString("pipeexec", args, i+1, at)
			if err != nil {
				return Null(), err
			}
			argv[i] = s
		}
		cmd := exec.Command(name, argv...)
		detachConsole(cmd)
		var out strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = childOut(in.errOut)
		if err := cmd.Run(); err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				return Null(), rtErrf(at, "pipeexec：%s は終了コード %d で終了しました", name, exit.ExitCode())
			}
			return Null(), rtErrf(at, "pipeexec：%s", err.Error())
		}
		return Str(out.String()), nil
	})
}
