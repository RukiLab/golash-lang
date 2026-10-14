//go:build !windows

package main

import (
	"os/exec"
)

// detachConsole は Windows 外では何もしない（そこにコンソール
// ウィンドウは存在せず、子プロセスが親と閉じられるコンソールを
// 共有することはないため）。
func detachConsole(cmd *exec.Cmd) {
}
