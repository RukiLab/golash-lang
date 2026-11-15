package gui

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/mp3"
	"github.com/hajimehoshi/ebiten/v2/audio/vorbis"
	"github.com/hajimehoshi/ebiten/v2/audio/wav"
)

// サウンドモデル（G2）：読み込み時にファイルをPCMへ完全にデコードし、プレイヤーはsndplayごとに生成する
// ため、サウンドは再開や重ね掛けが自由にできる。ループにはInfiniteLoopを使う。

const audioRate = 44100

type sound struct {
	pcm   []byte
	vol   float64 // 0..1、再生時にプレイヤーへ適用する
	pitch float64 // 再生速度兼ピッチ倍率（1.0 = 原速）。sndpitch で 0.25..4.0 に設定する
}

type audioState struct {
	mu      sync.Mutex
	sounds  map[int]*sound
	players map[int]*audio.Player
	loops   map[int]bool
	nextID  int
}

// sharedCtxはプロセス全体で共有する単一のオーディオコンテキスト：ebitenでは1つだけしか許可されず
// （単一のゲームループと同様）、テストでは複数のバックエンドが作られる。
var sharedCtx struct {
	sync.Mutex
	ctx *audio.Context
}

func sharedContext() *audio.Context {
	sharedCtx.Lock()
	defer sharedCtx.Unlock()
	if sharedCtx.ctx == nil {
		sharedCtx.ctx = audio.NewContext(audioRate)
	}
	return sharedCtx.ctx
}

func (b *WindowBackend) audioState() *audioState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.audio == nil {
		b.audio = &audioState{sounds: map[int]*sound{}, players: map[int]*audio.Player{}, loops: map[int]bool{}}
	}
	return b.audio
}

// SndLoadはwav/mp3/oggをメモリにデコードし、サウンドIDを返す。
func (b *WindowBackend) SndLoad(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("sndload：%s", err.Error())
	}
	var stream io.Reader
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		stream, err = wav.DecodeWithSampleRate(audioRate, bytes.NewReader(data))
	case ".mp3":
		stream, err = mp3.DecodeWithSampleRate(audioRate, bytes.NewReader(data))
	case ".ogg":
		stream, err = vorbis.DecodeWithSampleRate(audioRate, bytes.NewReader(data))
	default:
		return 0, fmt.Errorf("sndload：サポートされていない形式 %q です（.wav/.mp3/.ogg が必要です）", filepath.Ext(path))
	}
	if err != nil {
		return 0, fmt.Errorf("sndload：%s", err.Error())
	}
	pcm, err := io.ReadAll(stream)
	if err != nil {
		return 0, fmt.Errorf("sndload：%s", err.Error())
	}
	st := b.audioState()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.nextID++
	st.sounds[st.nextID] = &sound{pcm: pcm, vol: 1, pitch: 1}
	return st.nextID, nil
}

// pitchedPCM は原速 PCM を rate 倍速（高ピッチ）にリサンプルする。
// ebiten のストリーム形式は 16ビットLE・2ch（1フレーム4バイト）のため、
// フレーム単位で線形補間する。rate == 1 は複写なしで原 PCM を返す。
func pitchedPCM(pcm []byte, rate float64) []byte {
	if rate == 1 {
		return pcm
	}
	const frameSize = 4
	nFrames := len(pcm) / frameSize
	if nFrames == 0 {
		return pcm
	}
	outFrames := int(float64(nFrames) / rate)
	if outFrames < 1 {
		outFrames = 1
	}
	out := make([]byte, outFrames*frameSize)
	for i := 0; i < outFrames; i++ {
		pos := float64(i) * rate
		i0 := int(pos)
		if i0 >= nFrames-1 {
			copy(out[i*frameSize:(i+1)*frameSize], pcm[(nFrames-1)*frameSize:nFrames*frameSize])
			continue
		}
		frac := pos - float64(i0)
		i1 := i0 + 1
		for ch := 0; ch < 2; ch++ {
			s0 := int16(binary.LittleEndian.Uint16(pcm[i0*frameSize+ch*2:]))
			s1 := int16(binary.LittleEndian.Uint16(pcm[i1*frameSize+ch*2:]))
			v := float64(s0)*(1-frac) + float64(s1)*frac
			if v > 32767 {
				v = 32767
			} else if v < -32768 {
				v = -32768
			}
			binary.LittleEndian.PutUint16(out[i*frameSize+ch*2:], uint16(int16(v)))
		}
	}
	return out
}

// SndPlayはサウンドを再生する（loop指定で無限に繰り返す）。再生中のものは再開する。
// sndpitch で設定されたピッチ（再生速度）を適用する。
func (b *WindowBackend) SndPlay(id int, loop bool) error {
	st := b.audioState()
	st.mu.Lock()
	s, ok := st.sounds[id]
	if !ok {
		st.mu.Unlock()
		return fmt.Errorf("sndplay：不明なサウンド %d です", id)
	}
	ctx := sharedContext()
	if p, ok := st.players[id]; ok {
		st.mu.Unlock()
		p.Close()
		st.mu.Lock()
	}
	pcm := pitchedPCM(s.pcm, s.pitch)
	var src io.Reader = bytes.NewReader(pcm)
	if loop {
		src = audio.NewInfiniteLoop(bytes.NewReader(pcm), int64(len(pcm)))
	}
	p, err := audio.NewPlayer(ctx, src)
	if err != nil {
		st.mu.Unlock()
		return fmt.Errorf("sndplay：%s", err.Error())
	}
	p.SetVolume(s.vol)
	st.players[id] = p
	if st.loops == nil {
		st.loops = map[int]bool{}
	}
	st.loops[id] = loop
	st.mu.Unlock()
	p.Play()
	return nil
}

// SndVolumeはサウンドの音量を設定する（0..100パーセント）。再生中のプレイヤーがあれば適用し、
// 次回以降の再生用に保存する。Player.SetVolumeは並行利用しても安全なため、スクリプトを止めることはない。
func (b *WindowBackend) SndVolume(id, vol int) error {
	if vol < 0 || vol > 100 {
		return fmt.Errorf("sndvol：音量は 0 から 100 の範囲で指定してください。%d が指定されました", vol)
	}
	st := b.audioState()
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.sounds[id]
	if !ok {
		return fmt.Errorf("sndvol：不明なサウンド %d です", id)
	}
	s.vol = float64(vol) / 100
	if p, ok := st.players[id]; ok {
		p.SetVolume(s.vol)
	}
	return nil
}

// SndPitchはサウンドのピッチ（再生速度）を設定する（25..400パーセント、既定 100 = 原速）。
// 50 で1オクターブ低く半分の速さ、200 で1オクターブ高く倍速になる。
// 次回以降の再生用に保存し、再生中のプレイヤーがあれば新しいピッチで最初から再生し直す。
func (b *WindowBackend) SndPitch(id, pitch int) error {
	if pitch < 25 || pitch > 400 {
		return fmt.Errorf("sndpitch：ピッチは 25 から 400 の範囲で指定してください。%d が指定されました", pitch)
	}
	st := b.audioState()
	st.mu.Lock()
	s, ok := st.sounds[id]
	if !ok {
		st.mu.Unlock()
		return fmt.Errorf("sndpitch：不明なサウンド %d です", id)
	}
	s.pitch = float64(pitch) / 100
	_, playing := st.players[id]
	loop := false
	if st.loops != nil {
		loop = st.loops[id]
	}
	if !playing {
		st.mu.Unlock()
		return nil
	}
	ctx := sharedContext()
	if p, ok := st.players[id]; ok {
		st.mu.Unlock()
		p.Close()
		st.mu.Lock()
		// s は st.mu 配下のポインタのため再取得する。
		s = st.sounds[id]
		if s == nil {
			st.mu.Unlock()
			return fmt.Errorf("sndpitch：不明なサウンド %d です", id)
		}
		loop = st.loops[id]
	}
	pcm := pitchedPCM(s.pcm, s.pitch)
	var src io.Reader = bytes.NewReader(pcm)
	if loop {
		src = audio.NewInfiniteLoop(bytes.NewReader(pcm), int64(len(pcm)))
	}
	p, err := audio.NewPlayer(ctx, src)
	if err != nil {
		st.mu.Unlock()
		return fmt.Errorf("sndpitch：%s", err.Error())
	}
	p.SetVolume(s.vol)
	st.players[id] = p
	st.mu.Unlock()
	p.Play()
	return nil
}

// SndStopは1つのサウンドを停止する（nilは全停止）。不明なIDは黙って何もしない。
func (b *WindowBackend) SndStop(id *int) {
	st := b.audioState()
	st.mu.Lock()
	defer st.mu.Unlock()
	stop := func(p *audio.Player) { p.Close() }
	if id == nil {
		for sid, p := range st.players {
			stop(p)
			delete(st.players, sid)
		}
		for sid := range st.loops {
			delete(st.loops, sid)
		}
		return
	}
	if p, ok := st.players[*id]; ok {
		stop(p)
		delete(st.players, *id)
	}
	delete(st.loops, *id)
}
