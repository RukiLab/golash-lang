// System/OS-window builtins: environment, launching, networking.
//
// These wrap OS facilities a script cannot reach on its own. Each stays
// minimal on purpose; anything scriptable (parsing, formatting, math)
// belongs in script code, not here.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sqweek/dialog"
	"golang.design/x/clipboard"
)

// httpMaxBytes caps httpget bodies (16MB).
const httpMaxBytes = 16 << 20

var httpClient = &http.Client{Timeout: 10 * time.Second}

// httpFetch gets url with shared limits: non-2xx and oversize are errors.
func httpFetch(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return drainResp(resp)
}

// httpPost sends body to url with shared limits.
func httpPost(url, body, contentType string) ([]byte, error) {
	resp, err := httpClient.Post(url, contentType, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return drainResp(resp)
}

// drainResp checks the status and reads a capped body.
func drainResp(resp *http.Response) ([]byte, error) {
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, httpMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > httpMaxBytes {
		return nil, fmt.Errorf("body exceeds %d bytes", httpMaxBytes)
	}
	return body, nil
}

// parseDlgFilter splits "名前|*.png;*.jpg" into description and
// extensions (without "*."). Empty input means no filter.
func parseDlgFilter(f string) (string, []string) {
	parts := strings.SplitN(f, "|", 2)
	if len(parts) != 2 {
		return "", nil
	}
	var exts []string
	for _, p := range strings.Split(parts[1], ";") {
		p = strings.TrimSpace(p)
		p = strings.TrimPrefix(p, "*.")
		p = strings.TrimPrefix(p, "*")
		p = strings.TrimPrefix(p, ".")
		if p != "" {
			exts = append(exts, p)
		}
	}
	if len(exts) == 0 {
		return "", nil
	}
	return parts[0], exts
}

// Clipboard state: initialized once, watched in the background.
var (
	clipOnce sync.Once
	clipErr  error
	clipMu   sync.Mutex
	clipText string
)

// clipEnsure initializes the clipboard and starts the watcher.
func clipEnsure() error {
	clipOnce.Do(func() {
		if err := clipboard.Init(); err != nil {
			clipErr = err
			return
		}
		ch := clipboard.Watch(context.Background(), clipboard.FmtText)
		go func() {
			for d := range ch {
				if d.Format != clipboard.FmtText {
					continue
				}
				clipMu.Lock()
				clipText = string(d.Bytes)
				clipMu.Unlock()
			}
		}()
	})
	return clipErr
}

func init() {
	// getenv(name): environment variable value, or "" when unset
	// (unset and empty-but-set both read as ""; aborting on a
	// missing variable would make optional configuration unusable).
	register("getenv", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		name, err := needString("getenv", args, 0, at)
		if err != nil {
			return Null(), err
		}
		v, ok := os.LookupEnv(name)
		if !ok {
			return Str(""), nil
		}
		return Str(v), nil
	})

	// open(target): open a file, folder or URL with the associated
	// application (detached; returns immediately).
	register("open", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		target, err := needString("open", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if target == "" {
			return Null(), argErr("open", 0, at, "対象を空にすることはできません")
		}
		// URLs pass through untouched; file paths resolve against the
		// script directory like every other file builtin.
		if !strings.Contains(target, "://") {
			target = in.resolvePath(target)
		}
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
		case "darwin":
			cmd = exec.Command("open", target)
		default:
			cmd = exec.Command("xdg-open", target)
		}
		if err := cmd.Start(); err != nil {
			return Null(), rtErrf(at, "open：%s", err.Error())
		}
		// Reap in the background (same as exec); the call stays async.
		go func() { _ = cmd.Wait() }()
		return Null(), nil
	})

	// Clipboard (text only): clipboard_get polls the cached text,
	// clipboard_set writes it. External copies surface through a
	// background watcher; our own writes update the cache at once.
	register("clipboard_get", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		if err := clipEnsure(); err != nil {
			return Null(), rtErrf(at, "clipboard：%s", err.Error())
		}
		clipMu.Lock()
		defer clipMu.Unlock()
		return Str(clipText), nil
	})
	register("clipboard_set", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("clipboard_set", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := clipEnsure(); err != nil {
			return Null(), rtErrf(at, "clipboard：%s", err.Error())
		}
		if _, err := clipboard.Write(context.Background(), clipboard.FmtText, []byte(s)); err != nil {
			return Null(), rtErrf(at, "clipboard：%s", err.Error())
		}
		clipMu.Lock()
		clipText = s
		clipMu.Unlock()
		return Null(), nil
	})

	// dlgopen([filter]): file-open dialog, or "" on cancel (an empty
	// path is never a real selection, so it doubles as the sentinel).
	// filter is "名前|*.png;*.jpg" (one group).
	register("dlgopen", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		b := dialog.File().Title("開く")
		if len(args) == 1 {
			f, err := needString("dlgopen", args, 0, at)
			if err != nil {
				return Null(), err
			}
			desc, exts := parseDlgFilter(f)
			if len(exts) > 0 {
				b = b.Filter(desc, exts...)
			}
		}
		path, err := b.Load()
		if err != nil {
			if errors.Is(err, dialog.ErrCancelled) {
				return Str(""), nil
			}
			return Null(), rtErrf(at, "dlgopen：%s", err.Error())
		}
		return Str(path), nil
	})

	// dlgsave(): file-save dialog, or "" on cancel.
	register("dlgsave", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		path, err := dialog.File().Title("保存").Save()
		if err != nil {
			if errors.Is(err, dialog.ErrCancelled) {
				return Str(""), nil
			}
			return Null(), rtErrf(at, "dlgsave：%s", err.Error())
		}
		return Str(path), nil
	})

	// httpget(url [, path]): fetch over HTTP(S) with a 10s timeout and
	// 16MB cap. One argument returns the body as a string; with path
	// the body is saved and the byte count is returned.
	register("httpget", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		url, err := needString("httpget", args, 0, at)
		if err != nil {
			return Null(), err
		}
		// ネットワーク I/O より先に全引数を検証する（型エラー時に
		// 無駄な fetch・サーバ側副作用を起こさないため）。
		var path string
		if len(args) == 2 {
			path, err = needString("httpget", args, 1, at)
			if err != nil {
				return Null(), err
			}
		}
		body, err := httpFetch(url)
		if err != nil {
			return Null(), rtErrf(at, "httpget：%s", err.Error())
		}
		if len(args) == 1 {
			return Str(string(body)), nil
		}
		if err := os.WriteFile(in.resolvePath(path), body, 0o666); err != nil {
			return Null(), rtErrf(at, "httpget：%s", err.Error())
		}
		return Int(int64(len(body))), nil
	})

	// httppost(url, body [, contentType [, path]]): POST with the same
	// 10s timeout and 16MB cap as httpget. contentType defaults to
	// application/json. Returns the response body as a string; with
	// path the body is saved and the byte count is returned.
	register("httppost", 2, 4, func(in *Interp, args []Value, at Pos) (Value, error) {
		url, err := needString("httppost", args, 0, at)
		if err != nil {
			return Null(), err
		}
		reqBody, err := needString("httppost", args, 1, at)
		if err != nil {
			return Null(), err
		}
		contentType := "application/json"
		if len(args) >= 3 {
			contentType, err = needString("httppost", args, 2, at)
			if err != nil {
				return Null(), err
			}
		}
		// POST 前に path の型も検証する（httpget と同様、副作用前の失敗を優先）。
		var path string
		hasPath := len(args) == 4
		if hasPath {
			path, err = needString("httppost", args, 3, at)
			if err != nil {
				return Null(), err
			}
		}
		body, err := httpPost(url, reqBody, contentType)
		if err != nil {
			return Null(), rtErrf(at, "httppost：%s", err.Error())
		}
		if hasPath {
			if err := os.WriteFile(in.resolvePath(path), body, 0o666); err != nil {
				return Null(), rtErrf(at, "httppost：%s", err.Error())
			}
			return Int(int64(len(body))), nil
		}
		return Str(string(body)), nil
	})

	// setenv(name, value): set for this process (and children via exec).
	register("setenv", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		name, err := needString("setenv", args, 0, at)
		if err != nil {
			return Null(), err
		}
		value, err := needString("setenv", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := os.Setenv(name, value); err != nil {
			return Null(), rtErrf(at, "setenv：%s", err.Error())
		}
		return Null(), nil
	})
}
