//go:build windows

package gui

import (
	"os"
	"syscall"
	"unsafe"
)

// 変換中 (未確定) 文字列に対する Windows IMM の正解値。
//
// ebiten の Windows バックエンド (exp/textinput) は空になった変換を
// 報告しない: update() は長さゼロの GCS_COMPSTR を黙って捨て、
// WM_IME_ENDCOMPOSITION (キャンセル) は状態を一切送らないため、Field が古い
// テキストを保持し続け UncommittedTextLengthInBytes が永遠に > 0 のままになる。
// pumpIME は ebiten 側のミラーをこの OS 側の長さと突き合わせる: OS 側の
// 変換が空なら未確定文字列は空になったのでバーを消す。
//
// NOTE: pumpIME は ebiten のゲームスレッド上で動き、メイン
// (ウィンドウ) スレッドではないため、GetActiveWindow のようなスレッド依存の
// 照会はここでは常に失敗する (ebiten 自身は RunOnMainThread 経由で呼ぶ)。そのため
// ウィンドウは自 PID で絞った EnumWindows で検出し、スレッドセーフな照会
// API のみを使う (SendMessage は使わない)。

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

// enumWindowsOut は EnumWindows のハンドルを集める。パッケージスコープに
// 置くことでコールバックを生かし続け、uintptr<->ポインタ往復を避ける (vet-clean)。
// pumpIME はゲームスレッド専用のため同時アクセスは起きない。
var enumWindowsOut []uintptr

var enumWindowsCb = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
	enumWindowsOut = append(enumWindowsOut, hwnd)
	return 1
})

// ourWindowHwnds はこのプロセスが所有する可視トップレベルウィンドウを
// 返す (通常は ebiten ウィンドウのみ)。ゲームスレッド専用。
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

// imcCompositionLen は1つのウィンドウの入力コンテキストに問い合わせて
// 未確定文字列長をバイト数 (UTF-16) で返す。definitive=false は
// 判定できなかったこと (ebiten 側の値を維持する)。
func imcCompositionLen(hwnd uintptr) (n int, definitive bool) {
	himc, _, _ := procImmGetContext.Call(hwnd)
	if himc == 0 {
		// IME がそのウィンドウで無効: 変換は存在し得ない。
		return 0, true
	}
	defer procImmReleaseContext.Call(hwnd, himc)
	// サイズ照会: NULL バッファでバイト数が返り、IMM_ERROR_NODATA
	// (-1、変換データなし) または IMM_ERROR_GENERAL (-2、失敗)。
	r, _, _ := procImmGetCompositionStringW.Call(himc, imeGCSCompStr, 0, 0)
	v := int32(uint32(r))
	if v == imeErrorNoData {
		// 変換データなし: 未確定文字列は空。
		return 0, true
	}
	if v < 0 {
		// IMM_ERROR_GENERAL または想定外: 判定不能。
		return 0, false
	}
	return int(v), true
}

// osIMECompositionLen は OS 側の未確定文字列長をバイト数で返す。
// ok=false は判定不能 (ebiten 側の値を維持)。ゲームスレッド専用 (pumpIME から呼ぶ)。
func osIMECompositionLen() (n int, ok bool) {
	hwnds := ourWindowHwnds()
	if len(hwnds) == 0 {
		return 0, false
	}
	// いずれかの自ウィンドウに生きた変換があればミラーを維持する; 全て
	// 空のときだけ消す。他プロセスのウィンドウの変換はここに出ない
	// (PID フィルタ) ため、自らの終了を隠すことはない。
	// 呼び出し側はゼロか非ゼロかだけ見るので、最初の生きたものを返す。
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
