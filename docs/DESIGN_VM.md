# Golash v0.8.1 レジスタ型VM 設計書

`runtime.go`（ツリーウォーク）を正解（Ground Truth）とし、観測可能動作を
1文字単位で維持したまま、実行系を「AST → バイトコード・コンパイラ +
レジスタ型VM」へ移行した記録である。

実装：`opcode.go`（命令）・`program.go`（Proto/Program）・`compile.go`
（コンパイラ）・`vm.go`（実行ループ）・`disasm.go`（逆アセンブラ）・
`program_codec.go`（直列化・exe連結）。
VM が既定（`GOULASH_BACKEND=tree` でツリーウォークに戻せる）。
検証：`parity_test.go`、`vm_bench_test.go`、`compile_test.go`、
`vm_test.go`、`program_codec_test.go`。

---

## 1. `runtime.go` セマンティクス確認結果（§4 チェックリスト回答）

### 1.1 算術・演算エッジケース

- int と float の混合演算は float に昇格する（`toFloat`）。`%` は int
  のみ（float 混じりは `演算子 % は整数が必要です`）。
- 0 除算：int・float とも `0 による除算です`（`arith`）。`%` の
  ゼロ剰余は `0 による剰余演算です`。
- `abs(MinInt64)` のオーバーフロー、`sqrt` の負数、`exp`/`pow` の溢れ、
  `float("nan"/"inf")`、範囲外の `int()` は `runtime.go` の管轄外
  （`builtin_math.go`・値生成側で拒否）。VM は組込をそのまま呼ぶため
  自動的に一致する。

### 1.2 関数登録・前方参照

- `def` は実行文として先頭から逐次登録される（hoisting なし）。
  定義より前の呼出は `未定義の関数 %q です`。
- VM も `FUNCDEF` 命令で実行時登録するため、登録タイミング・
  エラー文言・位置が一致する（重複検査の順序も同一：
  引数重複 → 組込名 → 再定義）。

### 1.3 未定義変数の参照

- `未定義の変数 %q です`。VM の `LOADN`/`LOADG` が同一文言・同一位置で送出する。

### 1.4 void の消費エラー

- 一律 `void値を使用できません`。ただし位置が用途別に異なる：
  代入 RHS はターゲット位置、複合代入 RHS は値位置、`==` は左右の各位置、
  呼出引数は各引数位置、配列要素は各要素位置、switch 主部・case 値は各位置、
  `+` は演算子位置。
- `mes`/`print`/`logmes` のみ引数の void を素通しし、組込側でスキップする。
- VM ではコンパイラが `CKVAL` を正しい位置付きで先行発行し、
  命令本体の防御検査は到達不能な二重化に留める。

### 1.5 ループ変数の保護

- `repeat ... as i` の `i` への代入は**実行時**エラー
 （`%q に代入できません：repeat カウンタは読み取り専用です`）。
  パーサは組込名・重複等の静的検査のみ担当する。
- VM ではフレーム／グローバルの readonly ビットで再現する。

### 1.6 配列操作

- 添字の float は `配列のインデックスは整数である必要があります`。
  検査順序が読出（base→idx→int→base種別→負数→範囲）と
  書込（idx→int→負数→base→…）で異なるため、VM も発行順序で再現する
  （`CKINT`/`CKNEG` の先行＋`GETI`/`SETI` 内蔵検査）。
- 複層書込の中間段は負数も `インデックス %d は範囲外です` に畳む
  （`CKBND` が負数分岐を持たないことで再現）。

### 1.7 switch 文

- 主部を1回評価（void 検査つき）。case 値はソース順に逐次評価し、
  最初の一致で確定（後続は評価しない＝短絡）。`break` は switch のみ抜け、
  `continue` は外側ループへ伝播する。
- VM では線形な比較・ジャンプ列＋終端ラベルで再現する。

### 1.8 再帰深度上限

- 10000 フレーム超過で `関数の呼び出しが深すぎます（上限 10000）`
  （呼出位置）。組込呼出は数えない。VM もユーザ関数のみ数える。

---

## 2. アーキテクチャ

### 2.1 命令形式（`opcode.go`）

64bit 固定長：`[op:8][A:8][B:8][C:8][D:32]`。D は Bx（定数・名前・
プロトタイプ・組込ID・個数）または sBx（`pc+1` 相対ジャンプ）。

各命令は高々1つのソース位置を持つ。`runtime.go` が複数位置を
使い分ける箇所は、コンパイラが検査命令を正しい位置で先行発行する
ことで再現し、VM 側に副位置テーブルは持たない。

### 2.2 プログラム表現（`program.go`）

- `VMProto`：名前・仮引数（名＋位置）・スロット表・レジスタ数・
  最大組込引数・命令列・定数プール（void 可・配列不可）・名前表・
  `CALLV` 表記表・1:1 位置表。
- `VMProgram`：`Main`＋`def` 出現順の `Protos`。
- 組込は名前順の安定 ID 表（`builtinTable`、遅延初期化）で参照する。

### 2.3 値表現（`value.go` の扱い）

`Value` は既にフラットな構造体（Kind＋payload、ボックス化なし）であり、
要求の Tagged Union と実質同一のため、識別子（`KNull` 等）を維持して
流用した。改名の churn に対して振舞い利得がゼロと判断した。
`Array`（`Elems []Value`）も同様。

### 2.4 フレームと名前解決（`vm.go`）

- main：スロットなし。全変数はグローバル（`LOADG`/`STOREG`）。
  REPL の継続性（追加入力で蓄積）は共有の機械＋グローバル表で実現する。
- 関数：仮引数＋静的収集名にスロットを割当て、未作成状態＝`void` で
  突入する。`LOADN` は「スロット作成済み→直接／未作成→グローバル／
  不在→未定義エラー」、読出は従来通り。`STOREN` は「スロット作成済み→
  直接／未作成→グローバル（あれば更新）／不在→スロットに作成」
 （`let` 廃止後の宣言兼用。通常代入は見える定義の更新、なければ作成）。
- main：全変数はグローバル（`LOADG`／作成兼用の `STOREG`）。
- グローバルはロード時に intern され、G系命令の D は `addProg` で
  直接の gidx へ書換える（実行時ハッシュ検索なし＝インラインキャッシュ相当）。
- 組込名への代入は実行時に拒否する（`STOREG`/`STOREN` 内検査）。
  静的に弾くと `mes("hi"); mes = 1` の出力順序が変わるためである。
- フレーム・レジスタ・readonly ビットはサイズ別プールで再利用する
  （返却時は参照クリア）。

### 2.5 repeat の lowering（`compile.go`）

- `REPDISP` で整数／配列に分岐（種別エラーは Count 位置）。
- 整数形の `as i, x` は無条件エラー命令（`n.At` 位置）。
- 準備命令がループ制御（上限・添字・配列スナップショット）を
  フレーム内ループスタックへ push し、`SAVEVAR` が旧束縛を退避する。
- 本体脱出（正常・`break`）では `POPLOOP` で復元する。
  `return`・実行時エラー時は全フレームを巻戻し復元する
  （`interp.go` の `defer` 復元と対応。REPL 継続で観測可能）。
- `break` は最も内側のループ／switch を対象とし、repeat のみ pop する
  （`while`・`switch` は pop なし）。`continue` は復元なしで継続点へ。
- カウンタ書込は無検査の `PUTN`/`PUTG` で行う（`interp.go` の直接代入と対応）。

### 2.6 呼出の lowering

- `CKCALL` が引数評価より先に解決順序（関数→変数→組込→未定義）と
  arity を確定させる（`evalCall` と同順。`f(1, 未定義)` の arity 優先も再現）。
- 引数は左→右に連続レジスタへ評価する（各引数位置で `CKVAL`。
  `mes`/`print`/`logmes` のみ素通し）。
- 非変数 callee は callee のみ評価し `CALLV` でエラーにする（引数不評価）。
- `def` 本体はコンパイル時に Proto 化するが登録は実行時（`FUNCDEF`）。
- `end()` の `endSignal` panic は最上位で回収する（tree の `Run` と対応）。

---

## 3. 命令セット

`MOVE LOADK LOADG STOREG LOADN STOREN DEFN DEFG CKVAL TEST JMP JMPT JMPF
CKINT CKNEG CKARR CKBND GETI SETI NEWARR APPEND
ADD SUB MUL DIV MOD BAND BOR BXOR SHL SHR EQ NE LT LE GT GE
NOT NEG BITNOT
REPDISP REPITMERR FORIPREP FORAPREP SAVEVAR FORILOOP FORALOOP
PUTN PUTG POPLOOP FUNCDEF CKCALL CALLF CALLB CALLV RET
BRKTOP CONTTOP RETTOP HALT`

純粋演算は `runtime.go` の関数（`add`/`arith`/`bitwise`/`shift`/`compare`/
`valuesEqual`/`applyBinary`/`setIndex`/`requireValue`/`requireBool`）を
直接呼ぶか、検証済みの等価式でインライン化する（整数高速パス）。
インライン化した分岐の文言・条件は `parity_test.go` で両系一致を検証する。

---

## 3.5 ホットループ最適化（計数ループ 0.35秒目標）

目標 1000万回 `repeat`＋`i += 1` の計数ループの wall 0.35秒以内。
方針：意味論を変えない・decoder/codec に手を入れない・外れは通常経路へ。

- 複合代入融合（`INCCHK`、2ワード）：変数ターゲットの `+=`/`-=` を
  resolve→整数加算→readonly検査→格納の1命令に融合する。
  非整数はプロトタイプ末尾の低速スタブ（一般命令列・位置も同一）へ。
  組込名ターゲットは静的に除外し、一般経路の実行時検査に任せる
  （`mes("hi"); mes = 1` の出力順序を保つため）。
- 整数 `repeat` のボトムテスト化：`FORIPREP`（0周スキップ付き）→
  本体→`FORILOOP`（継続は先頭へ）の形にし、周回あたりの `JMP` をなくす。
  `FORILOOP`/`FORALOOP` は不要レジスタに `NoReg` を取れる。
- 単一加算ループ融合（`REPINC`、2ワード）：本体が単一のグローバル定数
  `INCCHK` である計数ループを検出し、`REPINC` に置換する。
  整数同士の反復加算は中間状態が観測不能（カウンタなし・本体単一・再入なし）
  のため残り回数分を一括適用する（mod 2^64 で反復と等価）。
  未定義・readonly・非整数は従来どおり（非整数は低速スタブで1回ずつ）。
- リテラル RHS の `CKVAL` 省略（`NullLit` 除く非void確定分）。
- ループ制御の `cur` ポインタキャッシュ、エラー時 frame/depth 巻戻し
  （REPL継続汚染の修正を兼ねる）。

実測：ループ内計測 0.15秒→1ms未満、wall 0.9秒→0.12秒前後。

## 4. アロケーション0化の技法

- レジスタはフラットな `[]Value`（ボックス化なし）。整数演算の
  高速パスは呼出も割当てもない。
- フレーム・レジスタ・readonly ビットはプール再利用（再帰の `make` 排除）。
- `CALLB` の引数は機械共有バッファの切出しで渡す（組込は再入しない）。
- `LOADG`/`STOREG` の名前解決はロード時 intern＋gidx 書換え（都度 map なし）。
- 配列リテラルは空配列＋`APPEND` の逐次構築に lowering し、
  巨大リテラル（`examples/falling.gsh` の 400 要素超）でもレジスタを消費しない。
- ホットループ（`while` 整数加算 100万回）は反復回数を10倍にしても
  1実行の割当てが不変（＝1周あたり 0 allocs。残りは起動時の定数分）。

---

## 5. 検証結果

- `parity_test.go`：約120の inline スクリプト（正常・全エラー文言・位置・
  終了コード）＋`testdata/parity/` のCLI作例6ファイルの両系完全一致。
- `TestVMZeroAllocLoop`：反復 10万／100万で割当て不変。
- `TestVMSpeedup`（中央値、実測例）：`fib(24)` 約5.3倍、
  100万回 `while` 加算 約3.7～4.3倍。
- `gsh disasm`：定数・名前・レジスタ数・位置付き逆アセンブル。
  `GOULASH_TRACE=1`：命令トレース（stderr）。
- 既存全テスト（tree 系）は無修正で全緑（VM は追加経路）。

---

## 6. 単一exe化（`gsh build`）

- `MarshalVMProgram` で Proto 群＋`#mode` をバイト列化し（版・CRC付き）、
  ランタイム exe の末尾に連結する（`AppendBundle`）。
  識別子＋長さの 16B trailer で検出する（`ExtractBundle`）。
- 起動時は自 exe 末尾だけ見てバンドル判定し、有れば CLI の代わりに
  内蔵プログラムを実行する（`--` 以外はスクリプト引数、
  相対パスの基準は exe のあるディレクトリ）。
- 連結済みを入力にしても `stripBundle` で入替えになり多重化しない。
- バンドル実行は常に VM（組込表は遅延初期化のため `callBuiltinByID` 側でも
  ensure する）。エラー位置はビルド時のソース位置を保持する。

## 7. 既知の制限（内部限界）

- 1プロトタイプあたりレジスタ255（8bit operand）。超過時は内部エラー
  （現実の作例では `examples/falling.gsh` も13に収まる）。
- 1呼出あたり引数255。配列リテラルの要素数は無制限（`APPEND` 方式）。
- `VMProgram` は実行した機械に帰属する（`addProg` が G系の D を書換えるため）。
  プログラムの機械間共有はしない（毎回 `Compile` する）。
- GUI のイベントループ・Backend・組込は tree/VM 共通（振舞い差なし）。
