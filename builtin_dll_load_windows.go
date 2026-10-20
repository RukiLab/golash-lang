//go:build windows

package main

import "golang.org/x/sys/windows"

// dllHandle は Windows の DLL 実体 (x/sys の遅延ロード)。
type dllHandle struct {
	dll *windows.LazyDLL
}

// dllOpen は DLL を開く (見つからなければエラー)。
func dllOpen(path string) (*dllHandle, error) {
	d := windows.NewLazyDLL(path)
	if err := d.Load(); err != nil {
		return nil, err
	}
	return &dllHandle{dll: d}, nil
}

// dllSym は関数のアドレスを返す (無ければエラー)。
func dllSym(h *dllHandle, name string) (uintptr, error) {
	p := h.dll.NewProc(name)
	if err := p.Find(); err != nil {
		return 0, err
	}
	return p.Addr(), nil
}

// dllClose は登録を外す (Windows にアンロード API は無いため
// プロセス終了まで常駐する。HSP の DLL と同じ扱い)。
func dllClose(h *dllHandle) error {
	return nil
}
