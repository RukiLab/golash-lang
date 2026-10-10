// P3 builtins: time, waiting, program control.
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

// endSignal unwinds to Run, which then reports the requested exit code.
type endSignal struct{}

// syncWriter serializes writes into one in-process writer. exec()
// streams the child's output through the interpreter's own writer and
// os/exec copies it from a separate goroutine, so an unsynchronized
// target (the test/capture bytes.Buffer) loses or garbles writes under
// that race (TestExec flaked about 10% before this guard).
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// childOut wraps an interpreter-side writer for a child process. A
// real *os.File passes through untouched: the child inherits the
// handle directly and no goroutine in this process writes through it.
// Everything else gets the mutex, because os/exec then copies the
// child's output in-process while the interpreter keeps writing.
func childOut(w io.Writer) io.Writer {
	if _, ok := w.(*os.File); ok {
		return w
	}
	return &syncWriter{w: w}
}

// procStart anchors nanotime() to the monotonic clock.
var procStart = time.Now()

func init() {
	// gettime(t): 0 year, 1 month (1-12), 2 weekday (0=Sunday..6=Saturday),
	// 3 day, 4 hour, 5 minute, 6 second, 7 millisecond.
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

	// nanotime(): monotonic nanoseconds since process start (for
	// benchmarking; wall-clock milliseconds come from gettime(7)).
	// time.Since uses the monotonic clock, so this never goes backward.
	register("nanotime", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		return Int(time.Since(procStart).Nanoseconds()), nil
	})

	// sleep(ms): pause for milliseconds (replaces HSP's centisecond wait).
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

	// await([n]): wait for n frames (default 1). GUI waits for Update
	// ticks (vsync); CUI waits ~16ms per frame.
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
			// tick+n must stay representable: a wrapped target reads as
			// already passed and used to return immediately.
			if n > math.MaxInt64-in.be.Tick() {
				return Null(), argErr("await", 0, at, "待機tick数が大きすぎます。%d が指定されました", n)
			}
		}
		in.be.Await(n)
		return Null(), nil
	})

	// tick(): frame counter (GUI: Update count; CUI: wall clock / 16ms).
	register("tick", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		return Int(in.be.Tick()), nil
	})

	// end([code]): stop the program with an exit code (default 0).
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
		// A GUI backend closes its window on end(); console just unwinds.
		if closer, ok := in.be.(interface{ RequestClose() }); ok {
			closer.RequestClose()
		}
		panic(endSignal{})
	})

	// assert(cond [, msg]): abort with an error when cond is false.
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

	// logmes(args...): mes-style output to the error stream (debugging).
	// Void arguments are skipped silently, like mes()/print().
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

	// args(): command-line arguments after the script file (and `--`)
	// as an array of strings. Empty when none were given.
	register("args", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		elems := make([]Value, len(in.cliArgs))
		for i, s := range in.cliArgs {
			elems[i] = Str(s)
		}
		return ArrayOf(elems), nil
	})

	// exec(name, args...): launch an external command asynchronously,
	// return 0 once started (it keeps running after exec returns).
	// Stdout/stderr pass through. Failure to start is an error.
	// pipeexec() is the synchronous twin: it waits for completion
	// and captures stdout.
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
		// Reap in the background; output already streams through.
		go func() { _ = cmd.Wait() }()
		return Int(0), nil
	})

	// pipeexec(name, args...): run an external command, return its
	// stdout as a string. Stdin is detached (null device) so the
	// child can never steal script input; stderr passes through.
	// Failure to start, or a non-zero exit, is an error (the plain
	// exec() twin returns codes instead of capturing output).
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
