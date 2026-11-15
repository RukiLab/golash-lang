//go:build !windows

package main

import "github.com/ebitengine/purego"

// dllHandle は Unix 系の DLL 実体 (purego の dlopen)。
type dllHandle struct {
	handle uintptr
}

// dllOpen は DLL を開く (見つからなければエラー)。
func dllOpen(path string) (*dllHandle, error) {
	h, err := purego.Dlopen(path, purego.RTLD_NOW)
	if err != nil {
		return nil, err
	}
	return &dllHandle{handle: h}, nil
}

// dllSym は関数のアドレスを返す (無ければエラー)。
func dllSym(h *dllHandle, name string) (uintptr, error) {
	return purego.Dlsym(h.handle, name)
}
