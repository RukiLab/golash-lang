//go:build !windows

package main

import "syscall"

func procSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
