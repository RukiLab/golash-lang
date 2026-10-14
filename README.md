# Golash（`gsh`）

> **テスト段階に関する注意:**  
> Golash はテスト段階です。そのため、予告なく破壊的変更が行われる可能性があります。  
> 仕様・文法・組み込み関数・CLI・GUI API などは変更される場合があります。  
> 最新の変更内容は `CHANGELOG.md` を確認してください。

Golash は、HSPに影響を受けた、ツリーウォーク型のスクリプト言語処理系です。
同一ランタイムで CUI と GUI（Ebiten ウィンドウ）が動作します。v0.8.0（テスト段階）。

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
gsh run examples/hello.gsh
gsh run examples/gui_hello.gsh
```

## サンプル

`examples/` に実行可能なサンプルがあります。

| ファイル | 内容 |
| --- | --- |
| `hello.gsh` | 基本出力・変数 |
| `builtins.gsh` | 組み込み関数の一覧動作 |
| `edge.gsh` | 境界条件の作例 |
| `sample19.gsh` | 分岐・関数の作例 |
| `define.gsh` | `#define`・`switch`・`lextokens` の作例 |
| `falling.gsh` | 落ちものパズル（`#define` 使用例） |
| `include_demo.gsh` | `#include` による分割例 |
| `gui_hello.gsh` | 最小の GUI ウィンドウ |
| `gui_draw.gsh` | 図形描画・画像転送・PNG 保存 |
| `gui_forms.gsh` | ボタン・チェック・ドロップダウン・複数行入力 |
| `gui_input.gsh` | キー・マウス入力 |
| `gui_widgets.gsh` | ウィジェット集（ダイアログ含む） |
| `gui_audio.gsh` | 音声再生 |
| `human_check.gsh` | 人手検証テスト（GUI専用。描画・入力・IME・音声の確認） |

## テスト

```sh
go test ./...
```

## バージョン管理

版は [SemVer](https://semver.org/lang/ja/) の `MAJOR.MINOR.PATCH` です。
現在の版は `VERSION` ファイルが唯一の正本で、`v0.8.0`（0.x はテスト段階のため
破壊的変更がありえます）。

```sh
gsh version          # 処理系の版（Golash v0.8.0 (SemVer)）
gsh version -json    # {"name":"Golash","version":"0.8.0","semver":true}
```

### 版を上げる

`tools/bumpver.go` が関係するファイルを一括更新します（対象が1件も無ければ
エラーになるため、反映漏れを黙って通しません）。

```sh
go run ./tools/bumpver.go -show            # 現在の版を表示
go run ./tools/bumpver.go -bump minor      # 0.8.0 → 0.9.0
go run ./tools/bumpver.go -bump patch      # 0.8.0 → 0.8.1
go run ./tools/bumpver.go -bump major      # 0.8.0 → 1.0.0
go run ./tools/bumpver.go -set 0.9.0       # 任意の版へ
```

更新されるのは `VERSION`・`main.go` の `golashVersion`・ルート `package.json`・
`editors/vscode/package.json`・`docs/MANUAL.md`・`docs/DESIGN_VM.md`・
`README.md`・`CHANGELOG.md` の `v<X.Y.Z>` 表記です。実行後に
`CHANGELOG.md` の見出し（`## [0.9.0] - 日付`）と内容を整え、`go test ./...` を
回してください（`TestVersionPinnedEverywhere` が取りこぼしを指摘します）。

### どの桁を上げるか

- **MAJOR** — 後方互換のない変更（文法・組込み関数の削除や意味変更）
- **MINOR** — 後方互換を保つ追加（新しい組込み関数・機能）
- **PATCH** — 後方互換な不具合修正

### リリース

版を上げてコミットし、`VERSION` と同名の `v*` タグを push すると、GitHub Actions
（`.github/workflows/release.yml`）が Windows / Linux / macOS 向けバイナリを
ビルドして GitHub Releases に公開します。版はビルド時に ldflags で埋め込みます。

```sh
go run ./tools/bumpver.go -bump minor
# CHANGELOG.md を編集
git commit -am "release: v0.8.0"
git tag -a v0.8.0 -m "v0.8.0"
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
