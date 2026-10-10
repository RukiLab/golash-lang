// Goulash v0.2 バイトコードのバイナリ直列化と単一exe連結。
//
// `gsh build` はコンパイル済み VMProgram をバイト列化し、ランタイム exe の
// 末尾に連結する（Windows のローダは exe 末尾の余分なデータを無視するため、
// 連結したまま実行可能）。起動時は自 exe 末尾の trailer を見て
// バンドル有無を判定し、有れば CLI の代わりに内蔵プログラムを実行する。
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"os"
)

const (
	// codecMagic はペイロード先頭の識別子。
	codecMagic = "GSHBC01\x00"
	// codecVersion はペイロード形式の版（`let` 廃止による
	// DEFG/DEFN 撤去で 1→2。旧バンドルは版エラーで拒否）。
	codecVersion = 2
	// bundleMagic は exe 末尾 trailer の識別子。
	bundleMagic = "GSHBNDL\x00"
)

// --- エンコーダ ---

type codecWriter struct {
	buf bytes.Buffer
	err error
}

func (w *codecWriter) u64(v uint64) {
	if w.err != nil {
		return
	}
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], v)
	_, w.err = w.buf.Write(tmp[:n])
}

func (w *codecWriter) i64(v int64) {
	if w.err != nil {
		return
	}
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutVarint(tmp[:], v)
	_, w.err = w.buf.Write(tmp[:n])
}

func (w *codecWriter) raw(b []byte) {
	if w.err != nil {
		return
	}
	_, w.err = w.buf.Write(b)
}

func (w *codecWriter) str(s string) {
	w.u64(uint64(len(s)))
	w.raw([]byte(s))
}

func (w *codecWriter) pos(p Pos) {
	w.str(p.File)
	w.i64(int64(p.Line))
	w.i64(int64(p.Column))
}

func (w *codecWriter) value(v Value) {
	switch v.K {
	case KNull:
		w.raw([]byte{0})
	case KInt:
		w.raw([]byte{1})
		w.u64(uint64(v.I))
	case KFloat:
		w.raw([]byte{2})
		w.u64(math.Float64bits(v.F))
	case KString:
		w.raw([]byte{3})
		w.str(v.S)
	case KBool:
		w.raw([]byte{4})
		if v.B {
			w.raw([]byte{1})
		} else {
			w.raw([]byte{0})
		}
	default:
		if w.err == nil {
			w.err = fmt.Errorf("内部エラー：定数化できない値です")
		}
	}
}

func (w *codecWriter) proto(p *VMProto) {
	w.str(p.Name)
	w.u64(uint64(len(p.Params)))
	for _, s := range p.Params {
		w.str(s)
	}
	// ParamPos は Params と同数（不足時はゼロ値で埋める）。
	for i := range p.Params {
		var pp Pos
		if i < len(p.ParamPos) {
			pp = p.ParamPos[i]
		}
		w.pos(pp)
	}
	w.u64(uint64(p.NumParams))
	w.u64(uint64(p.NumSlots))
	w.u64(uint64(p.NumRegs))
	w.u64(uint64(p.MaxArgs))
	w.u64(uint64(len(p.Code)))
	for _, ins := range p.Code {
		w.u64(uint64(ins))
	}
	w.u64(uint64(len(p.Consts)))
	for _, c := range p.Consts {
		w.value(c)
	}
	w.u64(uint64(len(p.Names)))
	for _, n := range p.Names {
		w.str(n)
	}
	w.u64(uint64(len(p.Strs)))
	for _, s := range p.Strs {
		w.str(s)
	}
	// 位置表は命令と同数。
	for i := range p.Code {
		var pp Pos
		if i < len(p.Positions) {
			pp = p.Positions[i]
		}
		w.pos(pp)
	}
	if p.Slots != nil {
		w.u64(uint64(len(p.Slots)))
		for k, v := range p.Slots {
			w.str(k)
			w.u64(uint64(v))
		}
	} else {
		w.u64(0)
	}
}

// MarshalVMProgram はプログラムと実行モードをバイト列化する。
func MarshalVMProgram(prog *VMProgram, mode string) ([]byte, error) {
	w := &codecWriter{}
	w.raw([]byte(codecMagic))
	w.u64(codecVersion)
	w.str(mode)
	w.proto(prog.Main)
	w.u64(uint64(len(prog.Protos)))
	for _, p := range prog.Protos {
		w.proto(p)
	}
	if w.err != nil {
		return nil, w.err
	}
	body := w.buf.Bytes()
	out := make([]byte, 0, len(body)+4)
	out = append(out, body...)
	var crc [4]byte
	binary.LittleEndian.PutUint32(crc[:], crc32.ChecksumIEEE(body))
	out = append(out, crc[:]...)
	return out, nil
}

// --- デコーダ ---

type codecReader struct {
	data []byte
	off  int
	err  error
}

func (r *codecReader) eof() bool { return r.off >= len(r.data) }

func (r *codecReader) u64() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.data[r.off:])
	if n <= 0 {
		r.err = fmt.Errorf("バンドルの読み込みに失敗しました（壊れています）")
		return 0
	}
	r.off += n
	return v
}

func (r *codecReader) i64() int64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Varint(r.data[r.off:])
	if n <= 0 {
		r.err = fmt.Errorf("バンドルの読み込みに失敗しました（壊れています）")
		return 0
	}
	r.off += n
	return v
}

func (r *codecReader) raw(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.off+n > len(r.data) {
		r.err = fmt.Errorf("バンドルの読み込みに失敗しました（壊れています）")
		return nil
	}
	b := r.data[r.off : r.off+n]
	r.off += n
	return b
}

// count は可変長配列の要素数を読み、巨大値による make での
// panic/OOM を防ぐため残りバイト数と上限で検証する。
// 各要素は最低1バイトを消費するため、要素数が残りバイトを
// 超えることは正規バンドルではあり得ない。
func (r *codecReader) count() int {
	n := r.u64()
	if r.err != nil {
		return 0
	}
	const maxCount = 1 << 28 // 256M: 正規バンドルを十分上回る上限
	if n > uint64(len(r.data)-r.off) || n > maxCount {
		r.err = fmt.Errorf("バンドルの読み込みに失敗しました（壊れています）")
		return 0
	}
	return int(n)
}

func (r *codecReader) str() string {
	n := r.u64()
	if r.err != nil {
		return ""
	}
	if n > uint64(len(r.data)-r.off) {
		r.err = fmt.Errorf("バンドルの読み込みに失敗しました（壊れています）")
		return ""
	}
	return string(r.raw(int(n)))
}

func (r *codecReader) pos() Pos {
	f := r.str()
	l := r.i64()
	c := r.i64()
	return Pos{File: f, Line: int(l), Column: int(c)}
}

func (r *codecReader) value() Value {
	t := r.raw(1)
	if r.err != nil {
		return Null()
	}
	switch t[0] {
	case 0:
		return Null()
	case 1:
		return Int(int64(r.u64()))
	case 2:
		return Float(math.Float64frombits(r.u64()))
	case 3:
		return Str(r.str())
	case 4:
		b := r.raw(1)
		if r.err != nil {
			return Null()
		}
		return Bool(b[0] == 1)
	}
	r.err = fmt.Errorf("バンドルの読み込みに失敗しました（壊れています）")
	return Null()
}

func (r *codecReader) proto() *VMProto {
	p := &VMProto{}
	p.Name = r.str()
	np := r.count()
	p.Params = make([]string, np)
	for i := range p.Params {
		p.Params[i] = r.str()
	}
	p.ParamPos = make([]Pos, np)
	for i := range p.ParamPos {
		p.ParamPos[i] = r.pos()
	}
	p.NumParams = int(r.u64())
	p.NumSlots = int(r.u64())
	p.NumRegs = int(r.u64())
	p.MaxArgs = int(r.u64())
	// レジスタ数・引数上限は8bit operandの範囲に収まるはず。
	if p.NumRegs < 0 || p.NumRegs > 0xFF || p.MaxArgs < 0 || p.MaxArgs > 0xFF {
		r.err = fmt.Errorf("バンドルの読み込みに失敗しました（壊れています）")
		return nil
	}
	nc := r.count()
	p.Code = make([]Instr, nc)
	for i := range p.Code {
		p.Code[i] = Instr(r.u64())
	}
	nk := r.count()
	p.Consts = make([]Value, nk)
	for i := range p.Consts {
		p.Consts[i] = r.value()
	}
	nn := r.count()
	p.Names = make([]string, nn)
	for i := range p.Names {
		p.Names[i] = r.str()
	}
	ns := r.count()
	p.Strs = make([]string, ns)
	for i := range p.Strs {
		p.Strs[i] = r.str()
	}
	p.Positions = make([]Pos, nc)
	for i := range p.Positions {
		p.Positions[i] = r.pos()
	}
	nslots := r.count()
	if nslots > 0 {
		p.Slots = make(map[string]int, nslots)
		for i := 0; i < nslots; i++ {
			k := r.str()
			v := int(r.u64())
			p.Slots[k] = v
		}
	}
	if r.err != nil {
		return nil
	}
	// 2ワード命令の切り詰めを検出する（末尾の INCCHK/REPINC に
	// word1 が無い不正バンドルは実行時の範囲外参照panicになる）。
	for i := 0; i < len(p.Code); i++ {
		if op, _, _, _, _ := p.Code[i].Decode(); op == OpIncChk || op == OpRepInc {
			if i+1 >= len(p.Code) {
				r.err = fmt.Errorf("バンドルの読み込みに失敗しました（壊れています）")
				return nil
			}
			i++
		}
	}
	return p
}

// UnmarshalVMProgram はバイト列からプログラムと実行モードを復元する。
func UnmarshalVMProgram(data []byte) (*VMProgram, string, error) {
	if len(data) < len(codecMagic)+4 {
		return nil, "", fmt.Errorf("バンドルの読み込みに失敗しました（短すぎます）")
	}
	body := data[:len(data)-4]
	want := binary.LittleEndian.Uint32(data[len(data)-4:])
	if crc32.ChecksumIEEE(body) != want {
		return nil, "", fmt.Errorf("バンドルの読み込みに失敗しました（検査和が一致しません）")
	}
	r := &codecReader{data: body}
	if string(r.raw(len(codecMagic))) != codecMagic {
		return nil, "", fmt.Errorf("バンドルの読み込みに失敗しました（形式が違います）")
	}
	if v := r.u64(); v != codecVersion {
		return nil, "", fmt.Errorf("バンドルの読み込みに失敗しました（版 %d に対応していません）", v)
	}
	mode := r.str()
	prog := &VMProgram{}
	prog.Main = r.proto()
	np := r.count()
	prog.Protos = make([]*VMProto, np)
	for i := range prog.Protos {
		prog.Protos[i] = r.proto()
	}
	if r.err != nil {
		return nil, "", r.err
	}
	if !r.eof() {
		return nil, "", fmt.Errorf("バンドルの読み込みに失敗しました（余分なデータがあります）")
	}
	return prog, mode, nil
}

// --- exe 連結 ---

// bundleTrailerLen は末尾 trailer（payload長8＋識別子8）の大きさ。
const bundleTrailerLen = 16

// stripBundle は exe イメージから既存バンドルを除去する
// （入替えビルドの多重連結を防ぐ）。
func stripBundle(exe []byte) []byte {
	if len(exe) < bundleTrailerLen {
		return exe
	}
	tail := exe[len(exe)-bundleTrailerLen:]
	if string(tail[8:]) != bundleMagic {
		return exe
	}
	n := binary.LittleEndian.Uint64(tail[:8])
	if n > uint64(len(exe)-bundleTrailerLen) {
		return exe
	}
	return exe[:len(exe)-bundleTrailerLen-int(n)]
}

// AppendBundle は runtimeExe の複製に payload を連結して outPath に書く。
func AppendBundle(runtimeExe, outPath string, payload []byte) error {
	exe, err := os.ReadFile(runtimeExe)
	if err != nil {
		return fmt.Errorf("ランタイムの読込に失敗しました: %w", err)
	}
	exe = stripBundle(exe)
	out := make([]byte, 0, len(exe)+len(payload)+bundleTrailerLen)
	out = append(out, exe...)
	out = append(out, payload...)
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], uint64(len(payload)))
	out = append(out, tmp[:]...)
	out = append(out, []byte(bundleMagic)...)
	if err := os.WriteFile(outPath, out, 0755); err != nil {
		return fmt.Errorf("出力に失敗しました: %w", err)
	}
	return nil
}

// ExtractBundle は自 exe 末尾のバンドルを取り出す。無ければ ok=false。
func ExtractBundle(exePath string) (payload []byte, ok bool, err error) {
	f, err := os.Open(exePath)
	if err != nil {
		return nil, false, nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, false, nil
	}
	if st.Size() < bundleTrailerLen {
		return nil, false, nil
	}
	tail := make([]byte, bundleTrailerLen)
	if _, err := f.ReadAt(tail, st.Size()-bundleTrailerLen); err != nil {
		return nil, false, nil
	}
	if string(tail[8:]) != bundleMagic {
		return nil, false, nil
	}
	n := binary.LittleEndian.Uint64(tail[:8])
	if n == 0 || n > uint64(st.Size()-bundleTrailerLen) || n > 1<<31 {
		return nil, false, fmt.Errorf("バンドルの読み込みに失敗しました（大きさが不正です）")
	}
	payload = make([]byte, n)
	if _, err := f.ReadAt(payload, st.Size()-bundleTrailerLen-int64(n)); err != nil {
		return nil, false, fmt.Errorf("バンドルの読み込みに失敗しました: %w", err)
	}
	return payload, true, nil
}
