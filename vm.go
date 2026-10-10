// Goulash v0.2 レジスタ型VMの実行ループ。
//
// runtime.go との観測一致が最優先：純粋な値演算は runtime.go の関数
// （add/arith/bitwise/shift/compare/valuesEqual/applyBinary/setIndex
// requireValue/requireBool）を直接呼び出すため、文言・条件がずれない。
// 評価順序・検査位置は compile.go 側の発行順序で再現する。
package main

import (
	"fmt"
	"os"
)

// vmGlobals は VM のグローバル束縛。名前はロード時に intern され、
// 命令は名前表 index で直接スロットを引く（繰返しハッシュ検索なし）。
type vmGlobals struct {
	index map[string]int
	names []string // gidx→名前（逆引き。エラーメッセージ用）
	vals  []Value
	has   []bool
	ro    []bool
}

func (g *vmGlobals) intern(name string) int {
	if i, ok := g.index[name]; ok {
		return i
	}
	i := len(g.vals)
	if g.index == nil {
		g.index = map[string]int{}
	}
	g.index[name] = i
	g.names = append(g.names, name)
	g.vals = append(g.vals, Null())
	g.has = append(g.has, false)
	g.ro = append(g.ro, false)
	return i
}

// vmSaved はループ変数の退避記録（POPLOOP・巻戻しで復元）。
type vmSaved struct {
	isGlobal bool
	slot     int
	gidx     int
	had      bool
	val      Value
	ro       bool
}

// vmLoop は実行時ループ制御（FORIPREP/FORAPREP が push、POPLOOP が pop）。
type vmLoop struct {
	isArray bool
	limit   int64
	idx     int64
	snap    []Value
	saves   []vmSaved
}

// vmFrame は呼出フレーム。レジスタは呼出ごとに確保する
// （再帰ベンチマークの 3 倍速には十分。while/repeat のホットループは
// フレーム内で完結し、1周あたりの割当てはゼロ）。
type vmFrame struct {
	proto  *VMProto
	regs   []Value
	ro     []bool
	loops  []vmLoop
	cur    *vmLoop // loops 末尾の別名（ホットループ用キャッシュ）
	retPC  int
	retReg uint8
}

// vmachine は VM の実行状態。Interp（Backend・終了コード・引数・
// スクリプトディレクトリ）は共有し、変数・関数表だけ VM 側に持つ。
// builtin は globals/funcs に触れないため、この分離は観測に影響しない。
type vmachine struct {
	in      *Interp
	prog    *VMProgram // 実行中プログラム（FUNCDEF の解決用。REPL では入力毎に差替え）
	globals vmGlobals
	funcs   map[string]*VMProto
	frames  []*vmFrame
	depth   int
	argsBuf []Value
	trace   bool
	// フレームプール：呼出毎の make を避ける（再帰の高速化）。
	// サイズ別に保持し、返却時は参照解放のためクリアする。
	// 添字はサイズそのもの（map 検索を避けるためスライス化）。
	regPool   [][][]Value
	roPool    [][][]bool
	framePool []*vmFrame
}

// takeFrame はプールからフレームを取る。
func (m *vmachine) takeFrame() *vmFrame {
	if n := len(m.framePool); n > 0 {
		fr := m.framePool[n-1]
		m.framePool = m.framePool[:n-1]
		return fr
	}
	return &vmFrame{}
}

// giveFrame はフレームをプールへ返す。
func (m *vmachine) giveFrame(fr *vmFrame) {
	fr.proto = nil
	fr.regs = nil
	fr.ro = nil
	fr.loops = nil
	fr.cur = nil
	fr.retPC = 0
	fr.retReg = 0
	m.framePool = append(m.framePool, fr)
}

// pushLoop はループ制御を積み、cur を更新する。
func (m *vmachine) pushLoop(fr *vmFrame, l vmLoop) {
	fr.loops = append(fr.loops, l)
	fr.cur = &fr.loops[len(fr.loops)-1]
}

// takeRegs はプールからレジスタ列を取る（なければ確保）。
func (m *vmachine) takeRegs(n int) []Value {
	if n <= 0 {
		n = 1
	}
	for len(m.regPool) <= n {
		m.regPool = append(m.regPool, nil)
	}
	if p := m.regPool[n]; len(p) > 0 {
		regs := p[len(p)-1]
		m.regPool[n] = p[:len(p)-1]
		return regs
	}
	return make([]Value, n)
}

// giveRegs はレジスタ列をプールへ返す（参照をクリアして GC リーク防止）。
func (m *vmachine) giveRegs(regs []Value) {
	for i := range regs {
		regs[i] = Value{}
	}
	n := len(regs)
	for len(m.regPool) <= n {
		m.regPool = append(m.regPool, nil)
	}
	m.regPool[n] = append(m.regPool[n], regs)
}

func (m *vmachine) takeRO(n int) []bool {
	if n <= 0 {
		return nil
	}
	for len(m.roPool) <= n {
		m.roPool = append(m.roPool, nil)
	}
	if p := m.roPool[n]; len(p) > 0 {
		ro := p[len(p)-1]
		m.roPool[n] = p[:len(p)-1]
		return ro
	}
	return make([]bool, n)
}

func (m *vmachine) giveRO(ro []bool) {
	for i := range ro {
		ro[i] = false
	}
	if len(ro) == 0 {
		return
	}
	n := len(ro)
	for len(m.roPool) <= n {
		m.roPool = append(m.roPool, nil)
	}
	m.roPool[n] = append(m.roPool[n], ro)
}

func newVmachine(in *Interp) *vmachine {
	m := &vmachine{in: in, funcs: map[string]*VMProto{}}
	if os.Getenv("GOULASH_TRACE") == "1" {
		m.trace = true
	}
	return m
}

// addProg はプログラムの名前を intern し、引数バッファを拡張する。
// globals/funcs は機械に蓄積される（REPL の継続性）。
// ついでに G系命令（LOADG/STOREG/PUTG/SAVEVAR-global）の D operand を
// 名前表 index から直接の gidx へ書換え、実行時のハッシュ検索をなくす。
func (m *vmachine) addProg(prog *VMProgram) {
	need := 0
	seen := func(p *VMProto) {
		if p.MaxArgs > need {
			need = p.MaxArgs
		}
		for _, n := range p.Names {
			m.globals.intern(n)
		}
		// D 書換え：G系のみ。N系・CALL系は Names 引きのまま。
		// OpIncChk / OpRepInc は2ワード命令のため word1 を読み飛ばす。
		for i := 0; i < len(p.Code); i++ {
			ins := p.Code[i]
			op, a, b, c, d := ins.Decode()
			switch op {
			case OpLoadG, OpStoreG, OpPutG:
				// 不正バンドル対策：範囲外の名前参照は書換えず、
				// 実行時に未定義エラーとして報告させる（panic回避）。
				if int(d) < len(p.Names) {
					p.Code[i] = EncodeInstr(op, a, b, c, uint32(m.globals.index[p.Names[d]]))
				}
			case OpSaveVar:
				if b&SaveIsGlobal != 0 && int(d) < len(p.Names) {
					p.Code[i] = EncodeInstr(op, a, b, c, uint32(m.globals.index[p.Names[d]]))
				}
			case OpIncChk:
				// word1（名前表 index）はグローバル形のみ gidx へ。
				// 切り詰め・範囲外は実行時/検証時に報告させる。
				if i+1 >= len(p.Code) {
					break
				}
				if b == NoReg {
					ni := uint32(p.Code[i+1])
					if int(ni) < len(p.Names) {
						p.Code[i+1] = EncodeInstr(0, 0, 0, 0, uint32(m.globals.index[p.Names[ni]]))
					}
				}
				i++ // word1 を飛ばす
			case OpRepInc:
				// word1 下位（名前表 index）を gidx へ。上位の低速 sBx は保持。
				if i+1 >= len(p.Code) {
					break
				}
				w1 := uint64(p.Code[i+1])
				ni := uint32(w1)
				if int(ni) < len(p.Names) {
					gi := uint32(m.globals.index[p.Names[ni]])
					p.Code[i+1] = Instr(w1&0xFFFFFFFF00000000 | uint64(gi))
				}
				i++ // word1 を飛ばす
			}
		}
	}
	seen(prog.Main)
	for _, p := range prog.Protos {
		seen(p)
	}
	if need > len(m.argsBuf) {
		m.argsBuf = make([]Value, need)
	}
}

// lookupVar は「スロット（作成済み）→グローバル」の順で探す。
// 見つかれば値・真、なければ偽（呼出解決・LOADN 用）。
func (m *vmachine) lookupVar(fr *vmFrame, name string, slot int) (Value, bool) {
	if slot >= 0 && slot < len(fr.regs) && fr.regs[slot].K != KNull {
		return fr.regs[slot], true
	}
	if gi, ok := m.globals.index[name]; ok && m.globals.has[gi] {
		return m.globals.vals[gi], true
	}
	return Null(), false
}

func (m *vmachine) restoreSave(fr *vmFrame, s vmSaved) {
	if s.isGlobal {
		if s.had {
			m.globals.vals[s.gidx] = s.val
		} else {
			m.globals.has[s.gidx] = false
			m.globals.vals[s.gidx] = Null()
		}
		m.globals.ro[s.gidx] = s.ro
		return
	}
	fr.regs[s.slot] = s.val
	fr.ro[s.slot] = s.ro
}

func (m *vmachine) popLoop(fr *vmFrame) {
	top := fr.loops[len(fr.loops)-1]
	fr.loops = fr.loops[:len(fr.loops)-1]
	if n := len(fr.loops); n > 0 {
		fr.cur = &fr.loops[n-1]
	} else {
		fr.cur = nil
	}
	for i := len(top.saves) - 1; i >= 0; i-- {
		m.restoreSave(fr, top.saves[i])
	}
}

// unwindAll はエラー・end 時の全フレームの束縛復元。
// runtime.go の repeat における defer 復元（エラー時も実行）と対応する。
func (m *vmachine) unwindAll() {
	for _, fr := range m.frames {
		for len(fr.loops) > 0 {
			m.popLoop(fr)
		}
	}
}

// runMain はプログラムの Main プロトタイプを実行する。
// 終了時（エラー時も）はフレーム・深度を入口状態に戻す
// （REPL 継続での汚染を防ぐ。tree の defer 深度復元と対応）。
func (m *vmachine) runMain(prog *VMProgram) (err error) {
	m.addProg(prog)
	m.prog = prog
	baseFrames := len(m.frames)
	baseDepth := m.depth
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(endSignal); ok {
				m.unwindAll()
				m.dropFrames(baseFrames)
				m.depth = baseDepth
				err = nil
				return
			}
			panic(r)
		}
	}()
	main := m.takeFrame()
	main.proto = prog.Main
	main.regs = m.takeRegs(prog.Main.NumRegs)
	m.frames = append(m.frames, main)
	pc := 0
	err = m.loop(pc)
	if err != nil {
		m.unwindAll()
	}
	m.dropFrames(baseFrames)
	m.depth = baseDepth
	return err
}

// dropFrames は baseFrames までフレームを破棄し、資源をプールへ返す。
func (m *vmachine) dropFrames(baseFrames int) {
	for len(m.frames) > baseFrames {
		fr := m.frames[len(m.frames)-1]
		m.frames = m.frames[:len(m.frames)-1]
		m.giveRegs(fr.regs)
		m.giveRO(fr.ro)
		m.giveFrame(fr)
	}
}

// evalOne は単一式を評価する（REPL の bare-expression echo 用）。
// 共有の globals/funcs を使い、結果レジスタの値を返す。
func (m *vmachine) evalOne(x Expr, at Pos) (out Value, err error) {
	g := &vmcGlobal{
		constMap: map[string]int{},
		nameMap:  map[string]int{},
		strMap:   map[string]int{},
	}
	fc := &fcomp{g: g, isMain: true}
	rv, err := fc.expr(x)
	if err != nil {
		return Null(), err
	}
	fc.emit(OpHalt, 0, 0, 0, 0, at)
	proto, err := fc.finish("eval", nil)
	if err != nil {
		return Null(), err
	}
	prog := &VMProgram{Main: proto}
	m.addProg(prog)
	m.prog = prog
	baseFrames := len(m.frames)
	baseDepth := m.depth
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(endSignal); ok {
				m.unwindAll()
				m.dropFrames(baseFrames)
				m.depth = baseDepth
				out = Null()
				err = nil
				return
			}
			panic(r)
		}
	}()
	fr := m.takeFrame()
	fr.proto = proto
	fr.regs = m.takeRegs(proto.NumRegs)
	m.frames = append(m.frames, fr)
	err = m.loop(0)
	if err != nil {
		m.unwindAll()
		m.dropFrames(baseFrames)
		m.depth = baseDepth
		return Null(), err
	}
	out = fr.regs[rv]
	m.dropFrames(baseFrames)
	m.depth = baseDepth
	return out, nil
}

func (m *vmachine) loop(pc int) error {
	fr := m.frames[len(m.frames)-1]
	proto := fr.proto
	regs := fr.regs
	for {
		ins := proto.Code[pc]
		op, a, b, c, d := ins.Decode()
		pos := proto.Positions[pc]
		if m.trace {
			fmt.Fprintf(os.Stderr, "[trace] %s:%d %s\n", proto.Name, pc, m.fmtInstr(proto, pc))
		}
		next := pc + 1
		switch op {
		case OpMove:
			regs[a] = regs[b]
		case OpLoadK:
			regs[a] = proto.Consts[d]
		case OpLoadG:
			// D は addProg で gidx へ書換え済み。
			gi := int(d)
			if gi >= len(m.globals.has) || !m.globals.has[gi] {
				return rtErrf(pos, "未定義の変数 %q です", m.globals.names[gi])
			}
			regs[a] = m.globals.vals[gi]
		case OpStoreG:
			gi := int(d)
			name := m.globals.names[gi]
			v := regs[a]
			if v.K == KNull {
				return rtErrf(pos, "void値を使用できません")
			}
			if isBuiltin(name) {
				return rtErrf(pos, "%q に代入できません：組み込み関数です", name)
			}
			if m.globals.has[gi] {
				if m.globals.ro[gi] {
					return rtErrf(pos, "%q に代入できません：repeat カウンタは読み取り専用です", name)
				}
				m.globals.vals[gi] = v
			} else {
				// `let` 廃止後：未定義への代入は宣言になる
				// （見える定義の更新、なければ現在のスコープに作成）。
				m.globals.vals[gi] = v
				m.globals.has[gi] = true
				m.globals.ro[gi] = false
			}
		case OpLoadN:
			name := proto.Names[d]
			slot := int(c)
			if regs[slot].K != KNull {
				regs[a] = regs[slot]
			} else {
				gi, ok := m.globals.index[name]
				if !ok || !m.globals.has[gi] {
					return rtErrf(pos, "未定義の変数 %q です", name)
				}
				regs[a] = m.globals.vals[gi]
			}
		case OpStoreN:
			name := proto.Names[d]
			slot := int(c)
			v := regs[a]
			if err := requireValue(v, pos); err != nil {
				return err
			}
			if regs[slot].K != KNull {
				if fr.ro[slot] {
					return rtErrf(pos, "%q に代入できません：repeat カウンタは読み取り専用です", name)
				}
				regs[slot] = v
			} else {
				if isBuiltin(name) {
					return rtErrf(pos, "%q に代入できません：組み込み関数です", name)
				}
				if gi, ok := m.globals.index[name]; ok && m.globals.has[gi] {
					if m.globals.ro[gi] {
						return rtErrf(pos, "%q に代入できません：repeat カウンタは読み取り専用です", name)
					}
					m.globals.vals[gi] = v
				} else {
					// `let` 廃止後：可視定義がなければ関数ローカルに作る。
					regs[slot] = v
					fr.ro[slot] = false
				}
			}

		case OpCkVal:
			// requireValue のインライン化（文言同一）。
			if regs[a].K == KNull {
				return rtErrf(pos, "void値を使用できません")
			}
		case OpTest:
			// requireBool のインライン化（文言同一、値は保持）。
			if regs[a].K != KBool {
				return rtErrf(pos, "条件式は bool 型である必要があります。%s が指定されました", typeNameOf(regs[a]))
			}
		case OpJmp:
			next = pc + 1 + int(int32(d))
		case OpJmpT:
			if regs[a].B {
				next = pc + 1 + int(int32(d))
			}
		case OpJmpF:
			if !regs[a].B {
				next = pc + 1 + int(int32(d))
			}
		case OpCkInt:
			if regs[a].K != KInt {
				return rtErrf(pos, "配列のインデックスは整数である必要があります。%s が指定されました", typeNameOf(regs[a]))
			}
		case OpCkNeg:
			if regs[a].I < 0 {
				return rtErrf(pos, "配列のインデックスは 0 以上である必要があります。%d が指定されました", regs[a].I)
			}
		case OpCkArr:
			if regs[a].K != KArray {
				return rtErrf(pos, "%s にインデックスでアクセスできません（[...] は配列に対応しています）", typeNameOf(regs[a]))
			}
		case OpCkBnd:
			idx := regs[a].I
			arr := regs[b].Arr
			if idx < 0 || int(idx) >= len(arr.Elems) {
				return rtErrf(pos, "インデックス %d は範囲外です（長さ %d）", idx, len(arr.Elems))
			}
		case OpGetI:
			base := regs[b]
			idxv := regs[c]
			if idxv.K != KInt {
				return rtErrf(pos, "配列のインデックスは整数である必要があります。%s が指定されました", typeNameOf(idxv))
			}
			if base.K != KArray {
				return rtErrf(pos, "%s にインデックスでアクセスできません（[...] は配列に対応しています）", typeNameOf(base))
			}
			if idxv.I < 0 {
				return rtErrf(pos, "配列のインデックスは 0 以上である必要があります。%d が指定されました", idxv.I)
			}
			if int(idxv.I) >= len(base.Arr.Elems) {
				return rtErrf(pos, "インデックス %d は範囲外です（長さ %d）", idxv.I, len(base.Arr.Elems))
			}
			regs[a] = base.Arr.Elems[int(idxv.I)]
		case OpSetI:
			base := regs[a]
			idxv := regs[b]
			if idxv.K != KInt {
				return rtErrf(pos, "配列のインデックスは整数である必要があります。%s が指定されました", typeNameOf(idxv))
			}
			if base.K != KArray {
				return rtErrf(pos, "%s にインデックスでアクセスできません（[...] は配列に対応しています）", typeNameOf(base))
			}
			if idxv.I < 0 {
				return rtErrf(pos, "配列のインデックスは 0 以上である必要があります。%d が指定されました", idxv.I)
			}
			if err := setIndex(base.Arr, int(idxv.I), regs[c], pos); err != nil {
				return err
			}
		case OpNewArr:
			n := int(d)
			bb := int(b)
			elems := make([]Value, n)
			copy(elems, regs[bb:bb+n])
			regs[a] = ArrayOf(elems)
		case OpAppend:
			arr := regs[a]
			if arr.K != KArray {
				return rtErrf(pos, "内部エラー：不正な配列構築です")
			}
			arr.Arr.Elems = append(arr.Arr.Elems, regs[b])
		case OpAdd:
			// 整数同士は add() の int 経路と同一（void は KNull のため混入不可）。
			if l, r := regs[b], regs[c]; l.K == KInt && r.K == KInt {
				regs[a] = Int(l.I + r.I)
				break
			} else {
				v, err := add(l, r, pos)
				if err != nil {
					return err
				}
				regs[a] = v
			}
		case OpSub:
			if l, r := regs[b], regs[c]; l.K == KInt && r.K == KInt {
				regs[a] = Int(l.I - r.I)
				break
			} else {
				v, err := arith(TokMinus, l, r, pos)
				if err != nil {
					return err
				}
				regs[a] = v
			}
		case OpMul:
			if l, r := regs[b], regs[c]; l.K == KInt && r.K == KInt {
				regs[a] = Int(l.I * r.I)
				break
			} else {
				v, err := arith(TokStar, l, r, pos)
				if err != nil {
					return err
				}
				regs[a] = v
			}
		case OpDiv:
			if l, r := regs[b], regs[c]; l.K == KInt && r.K == KInt {
				if r.I == 0 {
					return rtErrf(pos, "0 による除算です")
				}
				regs[a] = Int(l.I / r.I)
				break
			} else {
				v, err := arith(TokSlash, l, r, pos)
				if err != nil {
					return err
				}
				regs[a] = v
			}
		case OpMod:
			if l, r := regs[b], regs[c]; l.K == KInt && r.K == KInt {
				if r.I == 0 {
					return rtErrf(pos, "0 による剰余演算です")
				}
				regs[a] = Int(l.I % r.I)
				break
			} else {
				v, err := arith(TokMod, l, r, pos)
				if err != nil {
					return err
				}
				regs[a] = v
			}
		case OpBitAnd:
			if l, r := regs[b], regs[c]; l.K == KInt && r.K == KInt {
				regs[a] = Int(l.I & r.I)
				break
			} else {
				v, err := bitwise(TokBitAnd, l, r, pos)
				if err != nil {
					return err
				}
				regs[a] = v
			}
		case OpBitOr:
			if l, r := regs[b], regs[c]; l.K == KInt && r.K == KInt {
				regs[a] = Int(l.I | r.I)
				break
			} else {
				v, err := bitwise(TokBitOr, l, r, pos)
				if err != nil {
					return err
				}
				regs[a] = v
			}
		case OpBitXor:
			if l, r := regs[b], regs[c]; l.K == KInt && r.K == KInt {
				regs[a] = Int(l.I ^ r.I)
				break
			} else {
				v, err := bitwise(TokBitXor, l, r, pos)
				if err != nil {
					return err
				}
				regs[a] = v
			}
		case OpShl:
			if l, r := regs[b], regs[c]; l.K == KInt && r.K == KInt {
				if r.I < 0 || r.I > 63 {
					return rtErrf(pos, "シフト数は 0 から 63 の範囲で指定してください。%d が指定されました", r.I)
				}
				regs[a] = Int(l.I << uint(r.I))
				break
			} else {
				v, err := shift(TokShl, l, r, pos)
				if err != nil {
					return err
				}
				regs[a] = v
			}
		case OpShr:
			if l, r := regs[b], regs[c]; l.K == KInt && r.K == KInt {
				if r.I < 0 || r.I > 63 {
					return rtErrf(pos, "シフト数は 0 から 63 の範囲で指定してください。%d が指定されました", r.I)
				}
				regs[a] = Int(l.I >> uint(r.I))
				break
			} else {
				v, err := shift(TokShr, l, r, pos)
				if err != nil {
					return err
				}
				regs[a] = v
			}
		case OpEq, OpNe:
			l, r := regs[b], regs[c]
			var eq bool
			if l.K == KInt && r.K == KInt {
				eq = l.I == r.I
			} else {
				if l.K == KNull {
					return rtErrf(pos, "void値を使用できません")
				}
				if r.K == KNull {
					return rtErrf(pos, "void値を使用できません")
				}
				eq = valuesEqual(l, r)
			}
			if op == OpNe {
				eq = !eq
			}
			regs[a] = Bool(eq)
		case OpLt, OpLe, OpGt, OpGe:
			if l, r := regs[b], regs[c]; l.K == KInt && r.K == KInt {
				// compare() は数値間を float64 で比較するため同一式で再現する。
				lf, rf := float64(l.I), float64(r.I)
				var c int
				switch {
				case lf < rf:
					c = -1
				case lf > rf:
					c = 1
				}
				var out bool
				switch op {
				case OpLt:
					out = c < 0
				case OpLe:
					out = c <= 0
				case OpGt:
					out = c > 0
				default:
					out = c >= 0
				}
				regs[a] = Bool(out)
				break
			} else {
				var v Value
				var err error
				switch op {
				case OpLt:
					v, err = compare(TokLt, l, r, pos)
				case OpLe:
					v, err = compare(TokLtEq, l, r, pos)
				case OpGt:
					v, err = compare(TokGt, l, r, pos)
				default:
					v, err = compare(TokGtEq, l, r, pos)
				}
				if err != nil {
					return err
				}
				regs[a] = v
			}
		case OpNot:
			if regs[b].K != KBool {
				return rtErrf(pos, "条件式は bool 型である必要があります。%s が指定されました", typeNameOf(regs[b]))
			}
			regs[a] = Bool(!regs[b].B)
		case OpNeg:
			if v := regs[b]; v.K == KInt {
				regs[a] = Int(-v.I)
			} else if v.K == KFloat {
				regs[a] = Float(-v.F)
			} else {
				return rtErrf(pos, "%s を負数化できません", typeNameOf(v))
			}
		case OpBitNot:
			v := regs[b]
			if v.K != KInt {
				return rtErrf(pos, "%s に ~ を適用できません（整数のみ対応しています）", typeNameOf(v))
			}
			regs[a] = Int(^v.I)
		case OpRepDisp:
			v := regs[a]
			switch v.K {
			case KArray:
				next = pc + 1 + int(int32(d))
			case KInt:
			default:
				return rtErrf(pos, "repeat は整数または配列が必要です。%s が指定されました", typeNameOf(v))
			}
		case OpRepIntErr:
			return rtErrf(pos, "整数の repeat では変数は 1 つまでです（as i, item は配列専用です）")
		case OpForIPrep:
			v := regs[a]
			if v.K != KInt {
				return rtErrf(pos, "repeat は整数または配列が必要です。%s が指定されました", typeNameOf(v))
			}
			if v.I < 0 {
				return rtErrf(pos, "repeat の回数は 0 以上である必要があります。%d が指定されました", v.I)
			}
			m.pushLoop(fr, vmLoop{limit: v.I, idx: 0})
			if v.I <= 0 {
				next = pc + 1 + int(int32(d))
			} else if b != NoReg {
				regs[b] = Int(0)
			}
		case OpForAPrep:
			v := regs[a]
			if v.K != KArray {
				return rtErrf(pos, "repeat は整数または配列が必要です。%s が指定されました", typeNameOf(v))
			}
			snap := append([]Value(nil), v.Arr.Elems...)
			m.pushLoop(fr, vmLoop{isArray: true, idx: -1, snap: snap})
		case OpSaveVar:
			flags := b
			isGlobal := flags&SaveIsGlobal != 0
			if isGlobal {
				gi := int(d)
				sv := vmSaved{isGlobal: true, gidx: gi, had: m.globals.has[gi], val: m.globals.vals[gi], ro: m.globals.ro[gi]}
				top := fr.cur
				top.saves = append(top.saves, sv)
				if flags&SaveInitZero != 0 {
					m.globals.vals[gi] = Int(0)
					m.globals.has[gi] = true
				}
				if flags&SaveReadonly != 0 {
					m.globals.ro[gi] = true
				}
			} else {
				slot := int(a)
				sv := vmSaved{slot: slot, val: regs[slot], ro: fr.ro[slot]}
				if regs[slot].K != KNull {
					sv.had = true
				}
				top := fr.cur
				top.saves = append(top.saves, sv)
				if flags&SaveInitZero != 0 {
					regs[slot] = Int(0)
				}
				if flags&SaveReadonly != 0 {
					fr.ro[slot] = true
				}
			}
		case OpForILoop:
			// ボトムテスト形：idx を進め、継続なら top へ戻り、
			// 終了ならフォールスルーする。
			top := fr.cur
			top.idx++
			if top.idx < top.limit {
				if a != NoReg {
					regs[a] = Int(top.idx)
				}
				next = pc + 1 + int(int32(d))
			}
		case OpForALoop:
			top := fr.cur
			top.idx++
			if top.idx >= int64(len(top.snap)) {
				next = pc + 1 + int(int32(d))
			} else {
				if a != NoReg {
					regs[a] = Int(top.idx)
				}
				if b != NoReg {
					regs[b] = top.snap[top.idx]
				}
			}
		case OpPutN:
			regs[a] = regs[b]
		case OpPutG:
			gi := int(d)
			m.globals.vals[gi] = regs[a]
			m.globals.has[gi] = true
		case OpIncChk:
			// 複合代入(+/-)融合の高速経路。word1 が名前表 index
			// （グローバル形は addProg で gidx へ書換え済み）。
			// 低速（非整数）は sBx 先のスタブへ。
			// 不正バンドル対策：切り詰めは内部エラーで返す（panic回避）。
			if pc+1 >= len(proto.Code) {
				return rtErrf(pos, "内部エラー：壊れたバイトコードです")
			}
			nameIdx := uint32(proto.Code[pc+1])
			next = pc + 2
			var rhs Value
			rhsIsInt := c&IncConstInt != 0
			if c&IncRhsReg != 0 {
				rhs = regs[a]
			} else {
				rhs = proto.Consts[a]
			}
			sub := c&IncSub != 0
			if b == NoReg {
				// グローバル形。
				gi := int(nameIdx)
				if gi >= len(m.globals.has) || !m.globals.has[gi] {
					name := "?"
					if gi >= 0 && gi < len(m.globals.names) {
						name = m.globals.names[gi]
					}
					return rtErrf(pos, "未定義の変数 %q です", name)
				}
				cur := m.globals.vals[gi]
				if cur.K == KInt && (rhsIsInt || rhs.K == KInt) {
					var nv int64
					if sub {
						nv = cur.I - rhs.I
					} else {
						nv = cur.I + rhs.I
					}
					if m.globals.ro[gi] {
						return rtErrf(pos, "%q に代入できません：repeat カウンタは読み取り専用です", m.globals.names[gi])
					}
					m.globals.vals[gi] = Int(nv)
				} else {
					next = pc + 1 + int(int32(d))
				}
			} else {
				// スロット形。
				name := proto.Names[nameIdx]
				slot := int(b)
				if regs[slot].K != KNull {
					cur := regs[slot]
					if cur.K == KInt && (rhsIsInt || rhs.K == KInt) {
						var nv int64
						if sub {
							nv = cur.I - rhs.I
						} else {
							nv = cur.I + rhs.I
						}
						if fr.ro[slot] {
							return rtErrf(pos, "%q に代入できません：repeat カウンタは読み取り専用です", name)
						}
						regs[slot] = Int(nv)
					} else {
						next = pc + 1 + int(int32(d))
					}
				} else if gi, ok := m.globals.index[name]; ok && m.globals.has[gi] {
					cur := m.globals.vals[gi]
					if cur.K == KInt && (rhsIsInt || rhs.K == KInt) {
						var nv int64
						if sub {
							nv = cur.I - rhs.I
						} else {
							nv = cur.I + rhs.I
						}
						if m.globals.ro[gi] {
							return rtErrf(pos, "%q に代入できません：repeat カウンタは読み取り専用です", name)
						}
						m.globals.vals[gi] = Int(nv)
					} else {
						next = pc + 1 + int(int32(d))
					}
				} else {
					return rtErrf(pos, "未定義の変数 %q です", name)
				}
			}
		case OpRepInc:
			// 計数ループ融合：本体（単一のグローバル加算）を取込んだループ。
			// word1 上位＝低速 sBx、下位＝gidx（addProg 書換え済み）。
			// 高速閉形：整数同士の反復加算は中間状態が観測不能
			// （カウンタなし・本体単一・再入なし）のため、残り回数分を
			// 一括適用する（mod 2^64 で反復と等価）。非整数は低速へ。
			if pc+1 >= len(proto.Code) {
				return rtErrf(pos, "内部エラー：壊れたバイトコードです")
			}
			w1 := uint64(proto.Code[pc+1])
			next = pc + 2
			top := fr.cur
			top.idx++
			if top.idx <= top.limit {
				gi := int(uint32(w1))
				delta := proto.Consts[a]
				if gi >= len(m.globals.has) || !m.globals.has[gi] {
					name := "?"
					if gi >= 0 && gi < len(m.globals.names) {
						name = m.globals.names[gi]
					}
					return rtErrf(pos, "未定義の変数 %q です", name)
				}
				cur := m.globals.vals[gi]
				if cur.K == KInt && delta.K == KInt {
					if m.globals.ro[gi] {
						return rtErrf(pos, "%q に代入できません：repeat カウンタは読み取り専用です", m.globals.names[gi])
					}
					rest := top.limit - top.idx + 1
					if c&IncSub != 0 {
						m.globals.vals[gi] = Int(cur.I - delta.I*rest)
					} else {
						m.globals.vals[gi] = Int(cur.I + delta.I*rest)
					}
					// 閉形で完結したため end（POPLOOP）へフォールスルー。
				} else {
					// 低速スタブへ（低速 sBx は word1 上位）。
					next = pc + 1 + int(int32(uint32(w1>>32)))
				}
			}
		case OpPopLoop:
			m.popLoop(fr)
		case OpFuncDef:
			proto2 := m.prog.Protos[d]
			seen := map[string]bool{}
			for i, p := range proto2.Params {
				if seen[p] {
					var ppos Pos
					if i < len(proto2.ParamPos) {
						ppos = proto2.ParamPos[i]
					} else {
						ppos = pos
					}
					return rtErrf(ppos, "関数 %q にパラメータ %q が重複しています", proto2.Name, p)
				}
				seen[p] = true
			}
			if isBuiltin(proto2.Name) {
				return rtErrf(pos, "%q を再定義できません：組み込み関数です", proto2.Name)
			}
			if _, ok := m.funcs[proto2.Name]; ok {
				return rtErrf(pos, "関数 %q は既に定義されています", proto2.Name)
			}
			m.funcs[proto2.Name] = proto2
		case OpCkCall:
			name := proto.Names[d]
			argc := int(c)
			if callee, ok := m.funcs[name]; ok {
				want := len(callee.Params)
				if argc != want {
					return rtErrf(pos, "関数 %s は引数を %d 個必要としますが、%d 個が渡されました", name, want, argc)
				}
			} else if v, ok := m.lookupVar(fr, name, m.slotOf(fr, name, proto, d)); ok {
				if v.K == KArray {
					return rtErrf(pos, "値は配列です。配列へのアクセスは [...] を使用してください（例：a[0]、a(0) ではありません）")
				}
				return rtErrf(pos, "%q は %s であり、関数ではありません", name, typeNameOf(v))
			} else if isBuiltin(name) {
				// 引数評価へ進む（arity は CALLB で検査）。
			} else {
				return rtErrf(pos, "未定義の関数 %q です", name)
			}
		case OpCallF:
			name := proto.Names[d]
			argc := int(c)
			callee, ok := m.funcs[name]
			if !ok {
				// CKCALL を通過しているため到達不能のはずだが、
				// 変数・未定義の診断を再現する（防御）。
				if v, ok2 := m.lookupVar(fr, name, m.slotOf(fr, name, proto, d)); ok2 {
					if v.K == KArray {
						return rtErrf(pos, "値は配列です。配列へのアクセスは [...] を使用してください（例：a[0]、a(0) ではありません）")
					}
					return rtErrf(pos, "%q は %s であり、関数ではありません", name, typeNameOf(v))
				}
				return rtErrf(pos, "未定義の関数 %q です", name)
			}
			if argc != len(callee.Params) {
				return rtErrf(pos, "関数 %s は引数を %d 個必要としますが、%d 個が渡されました", name, len(callee.Params), argc)
			}
			m.depth++
			if m.depth > maxCallDepth {
				m.depth--
				return rtErrf(pos, "関数の呼び出しが深すぎます（上限 %d）", maxCallDepth)
			}
			nfr := m.takeFrame()
			nfr.proto = callee
			nfr.regs = m.takeRegs(callee.NumRegs)
			nfr.retPC = next
			nfr.retReg = a
			if callee.NumSlots > 0 {
				nfr.ro = m.takeRO(callee.NumSlots)
			} else {
				nfr.ro = nil
			}
			copy(nfr.regs, regs[int(b):int(b)+argc])
			m.frames = append(m.frames, nfr)
			fr = nfr
			proto = callee
			regs = nfr.regs
			next = 0
		case OpCallB:
			argc := int(c)
			copy(m.argsBuf, regs[int(b):int(b)+argc])
			v, err := callBuiltinByID(int(d), m.in, m.argsBuf[:argc], pos)
			if err != nil {
				return err
			}
			regs[a] = v
		case OpCallV:
			cv := regs[a]
			if cv.K == KArray {
				return rtErrf(pos, "値は配列です。配列へのアクセスは [...] を使用してください（例：a[0]、a(0) ではありません）")
			}
			return rtErrf(pos, "%s を呼び出せません（%s は関数ではありません）", proto.Strs[d], typeNameOf(cv))
		case OpRet:
			if len(m.frames) == 1 {
				return &RuntimeError{Msg: "関数外の return です"}
			}
			var rv Value
			if c == 1 {
				rv = regs[a]
			} else {
				rv = Null()
			}
			for len(fr.loops) > 0 {
				m.popLoop(fr)
			}
			retPC := fr.retPC
			retReg := fr.retReg
			m.giveRegs(fr.regs)
			m.giveRO(fr.ro)
			m.frames = m.frames[:len(m.frames)-1]
			m.giveFrame(fr)
			m.depth--
			parent := m.frames[len(m.frames)-1]
			parent.regs[retReg] = rv
			fr = parent
			proto = fr.proto
			regs = fr.regs
			next = retPC
		case OpBrkTop:
			return &RuntimeError{Msg: "ループ外の break です"}
		case OpContTop:
			return &RuntimeError{Msg: "ループ外の continue です"}
		case OpRetTop:
			return &RuntimeError{Msg: "関数外の return です"}
		case OpHalt:
			return nil
		default:
			return rtErrf(pos, "内部エラー：不明な命令です")
		}
		pc = next
	}
}

// slotOf は CkCall/CallF の防御経路用に名前→スロットを引く。
// main では -1（グローバルのみ）。
func (m *vmachine) slotOf(fr *vmFrame, name string, proto *VMProto, d uint32) int {
	if fr.proto.Slots == nil {
		return -1
	}
	if s, ok := fr.proto.Slots[name]; ok {
		return s
	}
	return -1
}
