// 非同期プロセスセッション（コンソールペイン用）。
//
// exec() はデタッチ起動、pipeexec() は終了待ちの一括取得なので、
// 対話的な入出力には使えません。ここでは子プロセスの stdin/stdout/stderr
// をパイプでつなぎ、フレームごとに少しずつ読み書きできるセッションを
// 提供します。
//
//	pid, err = procopen("gsh", "run", path)
//	out = procread(pid)          // 今ある分だけ取り出す（非ブロッキング）
//	procwrite(pid, line + "\n")  // 子の stdin へ
//	st = procpoll(pid)           // [実行中, 終了コード]
//	code = procclose(pid)        // 実行中なら kill して回収
package main

import (
	"io"
	"os/exec"
	"sync"
)

const procBufMax = 1 << 20 // 端末出力の保持上限（超えたら古い方を捨てる）

type procSession struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	out     []byte
	running bool
	code    int
	done    chan struct{}
}

var procRegistry = struct {
	sync.Mutex
	next int64
	m    map[int64]*procSession
}{next: 1, m: map[int64]*procSession{}}

func init() {
	register("procopen", 1, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		name, err := needString("procopen", args, 0, at)
		if err != nil {
			return Null(), err
		}
		argv := make([]string, 0, len(args)-1)
		for i := 1; i < len(args); i++ {
			s, err := needString("procopen", args, i, at)
			if err != nil {
				return Null(), err
			}
			argv = append(argv, s)
		}
		cmd := exec.Command(name, argv...)
		cmd.SysProcAttr = procSysProcAttr()
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return procOpenErr(err), nil
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return procOpenErr(err), nil
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return procOpenErr(err), nil
		}
		if err := cmd.Start(); err != nil {
			return procOpenErr(err), nil
		}
		s := &procSession{cmd: cmd, stdin: stdin, running: true, code: -1, done: make(chan struct{})}
		procRegistry.Lock()
		id := procRegistry.next
		procRegistry.next++
		procRegistry.m[id] = s
		procRegistry.Unlock()
		go s.readPipe(stdout)
		go s.readPipe(stderr)
		go s.wait()
		return ArrayOf([]Value{Int(id), Str("")}), nil
	})

	register("procread", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		id, ok := needIntArg("procread", args, 0)
		if !ok {
			return Null(), rtErrf(at, "procread：プロセス ID は整数である必要があります")
		}
		s := procFind(id)
		if s == nil {
			return Str(""), nil
		}
		return Str(s.drain()), nil
	})

	register("procwrite", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		id, ok := needIntArg("procwrite", args, 0)
		if !ok {
			return Null(), rtErrf(at, "procwrite：プロセス ID は整数である必要があります")
		}
		s, err := needString("procwrite", args, 1, at)
		if err != nil {
			return Null(), err
		}
		sess := procFind(id)
		if sess == nil {
			return IntOfBool(false), nil
		}
		sess.mu.Lock()
		defer sess.mu.Unlock()
		if !sess.running || sess.stdin == nil {
			return IntOfBool(false), nil
		}
		if _, err := io.WriteString(sess.stdin, s); err != nil {
			return IntOfBool(false), nil
		}
		return IntOfBool(true), nil
	})

	register("proceof", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		id, ok := needIntArg("proceof", args, 0)
		if !ok {
			return Null(), rtErrf(at, "proceof：プロセス ID は整数である必要があります")
		}
		s := procFind(id)
		if s == nil {
			return IntOfBool(false), nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stdin == nil {
			return IntOfBool(false), nil
		}
		_ = s.stdin.Close()
		s.stdin = nil
		return IntOfBool(true), nil
	})

	register("procpoll", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		id, ok := needIntArg("procpoll", args, 0)
		if !ok {
			return Null(), rtErrf(at, "procpoll：プロセス ID は整数である必要があります")
		}
		s := procFind(id)
		if s == nil {
			return ArrayOf([]Value{IntOfBool(false), Int(-1)}), nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return ArrayOf([]Value{IntOfBool(s.running), Int(int64(s.code))}), nil
	})

	register("procclose", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		id, ok := needIntArg("procclose", args, 0)
		if !ok {
			return Null(), rtErrf(at, "procclose：プロセス ID は整数である必要があります")
		}
		procRegistry.Lock()
		s := procRegistry.m[id]
		delete(procRegistry.m, id)
		procRegistry.Unlock()
		if s == nil {
			return Int(-1), nil
		}
		s.mu.Lock()
		running := s.running
		stdin := s.stdin
		s.mu.Unlock()
		if running && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		if stdin != nil {
			_ = stdin.Close()
		}
		<-s.done
		s.mu.Lock()
		code := s.code
		s.mu.Unlock()
		return Int(int64(code)), nil
	})
}

func procOpenErr(err error) Value {
	return ArrayOf([]Value{Int(-1), Str(err.Error())})
}

func procFind(id int64) *procSession {
	procRegistry.Lock()
	defer procRegistry.Unlock()
	return procRegistry.m[id]
}

func (s *procSession) readPipe(r io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			s.mu.Lock()
			if len(s.out)+n > procBufMax {
				drop := len(s.out) + n - procBufMax/2
				if drop > len(s.out) {
					drop = len(s.out)
				}
				s.out = append([]byte(nil), s.out[drop:]...)
			}
			s.out = append(s.out, buf[:n]...)
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *procSession) drain() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.out) == 0 {
		return ""
	}
	out := string(s.out)
	s.out = s.out[:0]
	return out
}

func (s *procSession) wait() {
	err := s.cmd.Wait()
	code := 0
	if err != nil {
		if s.cmd.ProcessState != nil {
			code = s.cmd.ProcessState.ExitCode()
		} else {
			code = -1
		}
	}
	s.mu.Lock()
	s.running = false
	s.code = code
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	s.mu.Unlock()
	close(s.done)
}

func needIntArg(name string, args []Value, i int) (int64, bool) {
	if i < 0 || i >= len(args) || args[i].K != KInt {
		return 0, false
	}
	return args[i].I, true
}
