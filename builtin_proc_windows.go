//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachConsole はコンソールウィンドウなしで cmd を起動する
// （DETACHED_PROCESS）。コンソールサブシステムの子プロセスは
// それ以外だと親のコンソールを共有（ダブルクリック起動時）または
// 継承（端末起動時）してしまう。そのコンソールウィンドウを閉じると
// それに接続された全プロセスが終了するため、スクリプトを実行する
// GUI エディタは子プロセスと一緒に死んでしまう。パイプを使う
// capture 系の組込（piperun/pipeexec）はコンソールを必要とせず、
// 対話的な exec() はそれを保つ（切り離された双子については
// execgui() を参照）。
func detachConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// DETACHED_PROCESS: コンソールは生成も継承もされない。
		CreationFlags: 0x08000000,
	}
}
