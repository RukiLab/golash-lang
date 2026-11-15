//go:build windows

package main

import "syscall"

// CREATE_NO_WINDOW: 子プロセスにコンソールウィンドウを作らせない。
const procCreateNoWindow = 0x08000000

func procSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: procCreateNoWindow,
	}
}
