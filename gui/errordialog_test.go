package gui

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

// errDlgGame は実際のループと同じようにバックエンドを駆動し、未応答の
// エラーダイアログが出ている間に Draw から描画ピクセルを測る
// （ReadPixels が許されるのはゲーム実行中だけ）。
type errDlgGame struct {
	b                    *WindowBackend
	frames               int
	measured             bool
	minX, minY, maxX, maxY int
	dark                 int
}

func (g *errDlgGame) Update() error {
	if g.measured {
		return ebiten.Termination
	}
	if err := g.b.Update(); err != nil {
		return err
	}
	return nil
}

func (g *errDlgGame) Draw(screen *ebiten.Image) {
	g.frames++
	g.b.Draw(screen)
	if g.frames >= 20 && !g.measured {
		g.measured = true
		g.minX, g.minY, g.maxX, g.maxY = 640, 480, -1, -1
		for y := 0; y < 480; y++ {
			for x := 0; x < 640; x++ {
				r, gg, bl, _ := screen.At(x, y).RGBA()
				if r < 0xF000 || gg < 0xF000 || bl < 0xF000 {
					g.dark++
					if x < g.minX {
						g.minX = x
					}
					if x > g.maxX {
						g.maxX = x
					}
					if y < g.minY {
						g.minY = y
					}
					if y > g.maxY {
						g.maxY = y
					}
				}
			}
		}
	}
}

func (g *errDlgGame) Layout(outsideWidth, outsideHeight int) (int, int) {
	return 640, 480
}

// TestErrorDialogStaysInWindow は最初のフレームで失敗するスクリプトの本番順序を再現する（Update がまだ1回も走っていない）。
// エラーダイアログはスクリプト側の dialog() と同様に 640x480 ウィンドウ内に完全に収まって描画されなければならない。
func TestErrorDialogStaysInWindow(t *testing.T) {
	if loopRan {
		t.Skip("ebiten allows a single game loop per process")
	}
	loopRan = true
	b := mustNew(t)
	msg := `C:\Users\kocha\Desktop\Dev\GolashProjects\golash-editor\無題.gsh:1:6: 0 による除算です`
	go func() {
		// ヘッドレスでは応答されない。計測後にゲームが終了する。
		_, _ = b.Dialog(msg, "ok")
	}()
	ebiten.SetWindowSize(640, 480)
	ebiten.SetWindowTitle("Golash test")
	g := &errDlgGame{b: b}
	_ = ebiten.RunGame(g)
	t.Logf("dialog bbox x[%d..%d] y[%d..%d] dark=%d frames=%d",
		g.minX, g.maxX, g.minY, g.maxY, g.dark, g.frames)
	if !g.measured {
		t.Fatal("measurement stage never ran")
	}
	if g.dark < 2000 {
		t.Fatalf("dialog did not render: only %d dark pixels", g.dark)
	}
	// ウィンドウ端で切れたらダイアログがはみ出していることになる。
	if g.minX <= 1 || g.minY <= 1 || g.maxX >= 638 || g.maxY >= 478 {
		t.Fatalf("dialog touches window edge (overflow): bbox x[%d..%d] y[%d..%d] dark=%d",
			g.minX, g.maxX, g.minY, g.maxY, g.dark)
	}
	// 折り返しメッセージ向けの中央寄せダイアログでは、テキストは十分内側から始まる。
	if g.minX < 16 || g.maxX > 623 {
		t.Fatalf("dialog not inside margins: bbox x[%d..%d] y[%d..%d] dark=%d",
			g.minX, g.maxX, g.minY, g.maxY, g.dark)
	}
}
