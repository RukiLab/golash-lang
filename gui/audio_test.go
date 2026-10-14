package gui

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestMmLoadErrors(t *testing.T) {
	b, err := New(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.MmLoad(filepath.Join(t.TempDir(), "missing.wav")); err == nil {
		t.Fatal("expected error for missing file")
	}
	bad := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(bad, []byte("hello"), 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := b.MmLoad(bad); err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

// writeSineWAV は小さな 8kHz モノラル 16ビット WAV を書き込む。
func writeSineWAV(t *testing.T, path string) {
	t.Helper()
	const rate, n = 8000, 80
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+2*n))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(rate*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(2*n))
	for i := 0; i < n; i++ {
		v := int16(10000 * math.Sin(2*math.Pi*440*float64(i)/rate))
		_ = binary.Write(&buf, binary.LittleEndian, v)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestMmWavRoundtrip(t *testing.T) {
	b, err := New(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "tone.wav")
	writeSineWAV(t, p)
	id, err := b.MmLoad(p)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if id <= 0 {
		t.Fatalf("bad sound id %d", id)
	}
	if err := b.MmPlay(id, false); err != nil {
		t.Skipf("no audio device in this environment: %v", err)
	}
	b.MmStop(&id)
	b.MmStop(nil) // all: 空の場合は何もしない
	if err := b.MmPlay(9999, false); err == nil {
		t.Fatal("expected error for unknown sound")
	}
}

func TestMmVolume(t *testing.T) {
	b, err := New(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "tone.wav")
	writeSineWAV(t, p)
	id, err := b.MmLoad(p)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := b.MmVolume(9999, 50); err == nil {
		t.Fatal("expected error for unknown sound")
	}
	for _, v := range []int{-1, 101} {
		if err := b.MmVolume(id, v); err == nil {
			t.Fatalf("volume %d should error", v)
		}
	}
	if err := b.MmVolume(id, 50); err != nil {
		t.Fatalf("store: %v", err)
	}
	st := b.audioState()
	st.mu.Lock()
	vol := st.sounds[id].vol
	st.mu.Unlock()
	if vol != 0.5 {
		t.Fatalf("stored vol = %v, want 0.5", vol)
	}
	// ライブ調整にはデバイスが必要。デバイスが無い場合は正常にスキップする。
	if err := b.MmPlay(id, false); err != nil {
		t.Skipf("no audio device in this environment: %v", err)
	}
	if err := b.MmVolume(id, 25); err != nil {
		t.Fatalf("live adjust: %v", err)
	}
	b.MmStop(&id)
}

func TestKeyDownUnknown(t *testing.T) {
	b, err := New(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.KeyDown(999); ok {
		t.Fatal("code 999 must be unknown")
	}
	if _, ok := b.KeyDown(-1); ok {
		t.Fatal("code -1 must be unknown")
	}
	// 既知のコードはゲームループ内でのみ問い合わせられる（renderGame を参照）。
}
