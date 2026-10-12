//go:build windows

package gui

import (
	"os"
	"syscall"
	"unsafe"
)

// Windows IMM ground truth for the in-conversion (uncommitted) string.
//
// ebiten's Windows backend (exp/textinput) never reports an emptied
// composition: update() drops a zero-length GCS_COMPSTR silently and
// WM_IME_ENDCOMPOSITION (cancel) sends no state at all, so Field keeps
// the stale text and UncommittedTextLengthInBytes stays > 0 forever.
// pumpIME reconciles ebiten's mirror against this OS-side length: when
// the OS composition is empty, the unconfirmed string became empty and
// the bar must blank.
//
// NOTE: pumpIME runs on ebiten's game thread, which is NOT the main
// (window) thread, so thread-affine queries like GetActiveWindow always
// fail here (ebiten itself calls it via RunOnMainThread). The window is
// therefore discovered with EnumWindows filtered by our own PID, and
// only thread-safe query APIs (no SendMessage) are used.

var (
	modImm32  = syscall.NewLazyDLL("imm32.dll")
	modUser32 = syscall.NewLazyDLL("user32.dll")

	procImmGetContext            = modImm32.NewProc("ImmGetContext")
	procImmReleaseContext        = modImm32.NewProc("ImmReleaseContext")
	procImmGetCompositionStringW = modImm32.NewProc("ImmGetCompositionStringW")

	procEnumWindows              = modUser32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = modUser32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = modUser32.NewProc("IsWindowVisible")
)

const (
	imeGCSCompStr  = 0x0008
	imeErrorNoData = -1
)

// enumWindowsOut collects EnumWindows handles. Package scope keeps the
// callback alive and avoids uintptr<->pointer round trips (vet-clean).
// pumpIME is game-thread only, so no concurrent access happens.
var enumWindowsOut []uintptr

var enumWindowsCb = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
	enumWindowsOut = append(enumWindowsOut, hwnd)
	return 1
})

// ourWindowHwnds returns the visible top-level windows owned by this
// process (normally just the ebiten window). Game thread only.
func ourWindowHwnds() []uintptr {
	enumWindowsOut = nil
	procEnumWindows.Call(enumWindowsCb, 0)
	me := uint32(os.Getpid())
	var res []uintptr
	for _, h := range enumWindowsOut {
		var pid uint32
		procGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
		if pid != me {
			continue
		}
		if r, _, _ := procIsWindowVisible.Call(h); r == 0 {
			continue
		}
		res = append(res, h)
	}
	return res
}

// imcCompositionLen queries one window's input context for the
// unconfirmed string length in bytes (UTF-16). definitive=false means
// it could not be determined (keep the ebiten-side value).
func imcCompositionLen(hwnd uintptr) (n int, definitive bool) {
	himc, _, _ := procImmGetContext.Call(hwnd)
	if himc == 0 {
		// IME is disabled for the window: no composition can exist.
		return 0, true
	}
	defer procImmReleaseContext.Call(hwnd, himc)
	// Size query: NULL buffer returns the byte count, IMM_ERROR_NODATA
	// (-1, no composition data) or IMM_ERROR_GENERAL (-2, failure).
	r, _, _ := procImmGetCompositionStringW.Call(himc, imeGCSCompStr, 0, 0)
	v := int32(uint32(r))
	if v == imeErrorNoData {
		// No composition data: the unconfirmed string is empty.
		return 0, true
	}
	if v < 0 {
		// IMM_ERROR_GENERAL or unexpected: cannot determine.
		return 0, false
	}
	return int(v), true
}

// osIMECompositionLen reports the OS-side unconfirmed string length in
// bytes. ok=false means it could not be determined (keep the
// ebiten-side value). Game thread only (called from pumpIME).
func osIMECompositionLen() (n int, ok bool) {
	hwnds := ourWindowHwnds()
	if len(hwnds) == 0 {
		return 0, false
	}
	// A live composition in any of our windows keeps the mirror; only
	// an all-empty result clears it. A foreign window's composition can
	// never appear here (PID filter), so this cannot mask our own end.
	// Callers only test zero vs non-zero, so the first live one wins.
	seen := false
	for _, h := range hwnds {
		l, def := imcCompositionLen(h)
		if !def {
			continue
		}
		seen = true
		if l > 0 {
			return l, true
		}
	}
	if !seen {
		return 0, false
	}
	return 0, true
}
