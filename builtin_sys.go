// システム／OS連携の組み込み関数：環境変数・外部起動・ネットワーク。
//
// スクリプト単体では届かない OS 機能を包む。各関数は意図的に
// 最小限にとどめる；スクリプトで書ける処理（解析・整形・計算）は
// こちらではなくスクリプト側に置く。
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

// httpMaxBytes は httpget の本文上限（16MB）。
const httpMaxBytes = 16 << 20

var httpClient = &http.Client{Timeout: 10 * time.Second}

// httpFetch は共通の制限付きで url を取得する：非2xx とサイズ超過はエラー。
func httpFetch(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return drainResp(resp)
}

// httpPost は body を url に共通の制限付きで送信する。
func httpPost(url, body, contentType string) ([]byte, error) {
	resp, err := httpClient.Post(url, contentType, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return drainResp(resp)
}

// drainResp はステータスを確認し、上限付きで本文を読み込む。
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

// parseDlgFilter は「名前|*.png;*.jpg」を説明文と
// extensions（"*." なし）。空入力はフィルタなしを意味する。
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

// クリップボードの状態：一度だけ初期化し、裏で監視する。
var (
	clipOnce sync.Once
	clipErr  error
	clipMu   sync.Mutex
	clipText string
)

// clipEnsure はクリップボードを初期化し、監視を開始する。
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
	// getenv(name)：環境変数の値。未設定時は「」
	//（未設定と空文字設定のどちらも「」として読む；存在しない変数で
	// 中断すると任意設定が使い物にならなくなるため）。
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

	// open(target)：関連付けアプリでファイル・フォルダ・URL を開く
	//（切り離し起動で即時復帰する）。
	register("open", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		target, err := needString("open", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if target == "" {
			return Null(), argErr("open", 0, at, "対象を空にすることはできません")
		}
		// URL はそのまま通し、ファイルパスは他のファイル系組み込み関数と同様に
		// スクリプトディレクトリ基準で解決する。
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
		// 裏で回収する（exec と同様）；呼び出しは非同期のまま。
		go func() { _ = cmd.Wait() }()
		return Null(), nil
	})

	// クリップボード（テキストのみ）：clipget は溜めたテキストを読み、
	// clipset は書き込む。外部からのコピーは裏の監視経由で反映され、
	// 自身の書き込みはキャッシュに即時反映される。
	register("clipget", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		if err := clipEnsure(); err != nil {
			return Null(), rtErrf(at, "clipboard：%s", err.Error())
		}
		clipMu.Lock()
		defer clipMu.Unlock()
		return Str(clipText), nil
	})
	register("clipset", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("clipset", args, 0, at)
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

	// dlgopen([filter])：ファイルを開くダイアログ。キャンセル時は「」（空の
	// パスは実際の選択になり得ないため、番兵として兼ねる）。
	// filter は「名前|*.png;*.jpg」（1グループ）。
	register("dlgopen", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		filter := ""
		if len(args) == 1 {
			var err error
			filter, err = needString("dlgopen", args, 0, at)
			if err != nil {
				return Null(), err
			}
		}
		path, err := dlgFileBuilder(in, "開く", filter, "").Load()
		if err != nil {
			if errors.Is(err, dialog.ErrCancelled) {
				return Str(""), nil
			}
			return Null(), rtErrf(at, "dlgopen：%s", err.Error())
		}
		return Str(path), nil
	})

	// dlgsave([filter] [, name])：ファイル保存ダイアログ。キャンセル時は「」。
	// filter は「名前|*.png;*.jpg」（1グループ；不要なら「」）で、name は
	// ファイル名欄の初期値。上書き確認はダイアログ自体が行う（OS の問い合わせ）
	// ため、返ってきたパスは常に利用者の確認済みになる。
	register("dlgsave", 0, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		filter, name := "", ""
		var err error
		if len(args) >= 1 {
			filter, err = needString("dlgsave", args, 0, at)
			if err != nil {
				return Null(), err
			}
		}
		if len(args) == 2 {
			name, err = needString("dlgsave", args, 1, at)
			if err != nil {
				return Null(), err
			}
		}
		path, err := dlgFileBuilder(in, "保存", filter, name).Save()
		if err != nil {
			if errors.Is(err, dialog.ErrCancelled) {
				return Str(""), nil
			}
			return Null(), rtErrf(at, "dlgsave：%s", err.Error())
		}
		return Str(path), nil
	})

	// httpget(url [, path])：HTTP(S) で取得する（10秒タイムアウト・16MB上限）。
	// 引数1個では本文を文字列で返し、path 付きでは保存してバイト数を返す。
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

	// httppost(url, body [, contentType [, path]])：httpget と同じ
	// 10秒タイムアウト・16MB上限で POST する。contentType の既定は
	// application/json。path なしでは応答本文を文字列で返し、
	// path 付きでは保存してバイト数を返す。
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

	// setenv(name, value)：このプロセス用に設定する（exec 経由で子にも継承）。
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

// dlgStartDir はファイルダイアログの初期フォルダ：存在すればスクリプト自身の
// ディレクトリ、なければ作業ディレクトリ。font() の探索順を写しているため、
// スクリプトは既定で手元のファイルを拾う（どちらも使えない場合は「」で OS 任せ）。
func dlgStartDir(in *Interp) string {
	if d := in.ScriptDir(); d != "" {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// dlgFileBuilder は共通のファイルダイアログを整える：開始フォルダ
//（dlgStartDir）、任意フィルタ（「名前|*.png;*.jpg」、1グループ）、保存時は
// 初期ファイル名。Load()／Save() までは OS 画面を開かないため、
// セッションなしでも設定部分を試験できる。
func dlgFileBuilder(in *Interp, title, filter, name string) *dialog.FileBuilder {
	b := dialog.File().Title(title)
	if dir := dlgStartDir(in); dir != "" {
		b = b.SetStartDir(dir)
	}
	if desc, exts := parseDlgFilter(filter); len(exts) > 0 {
		b = b.Filter(desc, exts...)
	}
	if name != "" {
		b = b.SetStartFile(name)
	}
	return b
}
