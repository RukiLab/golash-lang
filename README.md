# Golash（`gsh`）

> **テスト段階に関する注意:**  
> Golash はテスト段階です。そのため、予告なく破壊的変更が行われる可能性があります。  
> 仕様・文法・組み込み関数・CLI・GUI API などは変更される場合があります。  
> 最新の変更内容は `CHANGELOG.md` を確認してください。

Golash は、HSPに影響を受けた、ツリーウォーク型のスクリプト言語処理系です。
同一ランタイムで CUI と GUI（Ebiten ウィンドウ）が動作します。v0.3（テスト段階）。

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

## ドキュメント

- `docs/MANUAL.md` — 言語仕様マニュアル（文法・組み込み関数・GUI・エラー）
- `CHANGELOG.md` — 変更履歴（破壊的変更を含む）
- `editors/vscode/README.md` — VS Code 拡張機能の導入方法
