package gui

import (
	"fmt"
	"image"
	"io/fs"

	"github.com/hajimehoshi/ebiten/v2"
)

// ドロップされたファイル (A7)。ebiten は Update ごとの仮想 FS スナップショットで報告する（アイドル時は nil）ため、pumpDrops が到着時に名前を集め、
// dropload() がそれを取り出す。スナップショットは dropload(name) 用に保持される。

// pumpDrops は新しくドロップされたファイル名のスナップショットを取る。Update から呼ばれる。
// モーダルダイアログ表示中は破棄する (閉じた後に背後へ流れ込ませない)。
func (b *WindowBackend) pumpDrops() {
	fsys := ebiten.DroppedFiles()
	if b.isDialogActive() {
		return
	}
	if fsys == nil {
		return
	}
	var names []string
	_ = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil // 最善努力で処理し、読めない項目は飛ばす
		}
		if path != "." {
			names = append(names, path)
		}
		return nil
	})
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dropFS = fsys
	b.dropNames = append(b.dropNames, names...)
}

// DropNames は保留中のドロップされたファイル名を返す（取り出し後に消費される）。
func (b *WindowBackend) DropNames() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	names := b.dropNames
	b.dropNames = nil
	return names
}

// DropLoad はドロップされたファイル（dropload() が返した名前で指定）をデコードして新しい
// バッファに入れ、その id を返す。
func (b *WindowBackend) DropLoad(name string) (int, error) {
	b.mu.Lock()
	fsys := b.dropFS
	b.mu.Unlock()
	if fsys == nil {
		return 0, fmt.Errorf("dropload：ドロップされたファイルがありません")
	}
	f, err := fsys.Open(name)
	if err != nil {
		return 0, fmt.Errorf("dropload：%s", err.Error())
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return 0, fmt.Errorf("dropload：%s", err.Error())
	}
	return b.allocUpload(src)
}
