# Golash（`gsh`）

> **テスト段階に関する注意:**  
> Golash はテスト段階です。そのため、予告なく破壊的変更が行われる可能性があります。  
> 仕様・文法・組み込み関数・CLI・GUI API などは変更される場合があります。  
> 最新の変更内容は `CHANGELOG.md` を確認してください。

Golash は、HSPに影響を受けた、レジスタ型VMで動作するスクリプト言語処理系です。
同一ランタイムで CUI と GUI（Ebiten ウィンドウ）が動作します。v0.9（テスト段階）。

## 必要環境

- Go 1.25 以上

## ビルド

```sh
go build -o gsh.exe .      # Windows（コマンド名 gsh）
go build -o gsh .          # Linux / macOS
```

単一バイナリで CUI / GUI の両方が動作します（特別なビルドは不要です）。

## 使い方

```sh
gsh run <file.gsh> [-- args...]   スクリプトを実行します
gsh repl                               対話環境（REPL）を起動します
gsh lex <file.gsh>                     字句トークン列を出力します（デバッグ用）
gsh parse <file.gsh>                   構文木を出力します（デバッグ用）
```

実行例：

```sh
gsh run examples/falling.gsh
```

## モード・定数・include

実行モードは完全自動判定です。GUI 専用の組込み（`screen` / `gsel` / `button` など）が
main から到達可能なコードにあれば GUI、なければ CUI になります。モードを固定する
宣言も判定クエリもありません。

```gsh
MAX = 100
Color_Red = 0
Color_Green = 1
Color_Blue = 2
LOW = 0
MID = 10
HIGH = 11

include "lib.gsh"
dll "user32.dll"
dlldef msgbox "MessageBoxA" "int(int,string,string,int)"
```

`#define` / `#ifdef` / `#mode` などの旧ディレクティブは廃止されました。
`#` で始まる行は移行エラーになります。

## サンプル

`examples/` に実行可能なサンプルがあります（言語の回帰用フィクスチャは `testdata/parity/` に分離しています）。

| ファイル | 内容 |
| --- | --- |
| `falling.gsh` | 落ちものパズル（素材は `assets/mino.png`） |

## テスト

```sh
go test ./...
```

## バージョン管理

版は `MAJOR.MINOR` のドット1つです（例 `0.9`）。
現在の版は `VERSION` ファイルが唯一の正本で、`v0.9`（0.x はテスト段階のため
破壊的変更がありえます）。0.8 以前は SemVer（`MAJOR.MINOR.PATCH`）でしたが、
0.9 からドット1つに統一しました。

```sh
gsh version          # 処理系の版（Golash v0.9）
gsh version -json    # {"name":"Golash","version":"0.9"}
```

### 版を上げる

`tools/bumpver.go` が関係するファイルを一括更新します（対象が1件も無ければ
エラーになるため、反映漏れを黙って通しません）。

```sh
go run ./tools/bumpver.go -show            # 現在の版を表示
go run ./tools/bumpver.go -bump minor      # 0.9 → 0.10
go run ./tools/bumpver.go -bump major      # 0.9 → 1.0
go run ./tools/bumpver.go -set 0.10        # 任意の版へ
```

更新されるのは `VERSION`・`main.go` の `golashVersion`・ルート `package.json`・
`editors/vscode/package.json`・`docs/MANUAL.md`・`docs/DESIGN_VM.md`・
`README.md`・`CHANGELOG.md` の `v<X.Y>` 表記です。実行後に
`CHANGELOG.md` の見出し（`## [0.9] - 日付`）と内容を整え、`go test ./...` を
回してください（`TestVersionPinnedEverywhere` が取りこぼしを指摘します）。

### どの桁を上げるか

- **MAJOR** — 後方互換のない変更（文法・組込み関数の削除や意味変更）
- **MINOR** — 後方互換を保つ追加・修正（新しい組込み関数・機能、不具合修正）

### リリース

版を上げてコミットし、`VERSION` と同名の `v*` タグを push すると、GitHub Actions
（`.github/workflows/release.yml`）が Windows / Linux / macOS 向けバイナリを
ビルドして GitHub Releases に公開します。版はビルド時に ldflags で埋め込みます。

```sh
go run ./tools/bumpver.go -bump minor
# CHANGELOG.md を編集
git commit -am "release: v0.9"
git tag -a v0.9 -m "v0.9"
git push origin main --tags
```

手元で同じ版入りバイナリを作るには：

```sh
go build -ldflags "-X main.golashVersion=$(cat VERSION)" -o gsh.exe .
```

## ドキュメント

- `docs/MANUAL.md` — 言語仕様マニュアル（文法・組み込み関数・GUI・エラー）
- `CHANGELOG.md` — 変更履歴（破壊的変更を含む）
- `editors/vscode/README.md` — VS Code 拡張機能の導入方法
