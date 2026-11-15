# Golash for VS Code

Golash（`.gsh`）用の言語サポートです。TextMate による構文強調表示、
コメント・括弧の編集支援、F5 でのスクリプト実行を提供します。

## 実行（F5）

`.gsh` ファイルを開いた状態で `F5` を押すと、ファイルを保存してから
統合ターミナルで `gsh run "<file>"` が実行されます（ターミナルが
なければ自動作成）。
コマンドパレットから `Golash: 実行` でも同じ操作ができます。

前提：

- `gsh` に PATH が通っていること（`gsh.exe` のあるフォルダをユーザー
  PATH へ追加。追加後に VS Code を再起動しないと反映されません）
- GUI 実行にも特別なビルドは不要です（単一バイナリが CUI / GUI 両対応。
  `go build -o gsh.exe .` で作ったもので F5 が動作します）
- 拡張機能の更新後は VS Code の再読み込み（`開発者: ウィンドウの再読み込み`）

## 導入方法（ローカル）

1. このフォルダを拡張機能ディレクトリにコピー（またはシンボリックリンク）します。
   - Windows: `%USERPROFILE%\.vscode\extensions\golash-0.9.0\`
   - macOS / Linux: `~/.vscode/extensions/golash-0.9.0/`
2. VS Code を再読み込みし、`.gsh` ファイルを開きます。

## 公開手順（管理者向け）

拡張機能の版は `package.json` の `version` です。VS Code の manifest は
3桁 SemVer を要求するため `0.9.0` と書きますが、意味は処理系の `0.9` と同じ
（`TestVersionPinnedEverywhere` が `MAJOR.MINOR.0` を検査します）。版上げは
処理系と同じ `go run ./tools/bumpver.go` で一括更新されます。

```sh
npm install -g @vscode/vsce
vsce package
vsce publish
```

## 内容

- `syntaxes/golash.tmLanguage.json` — キーワード・組み込み関数・ローダ宣言
  （`include` / `dll` / `dlldef`）、文字列、
  コメント、`0x` / `0b` / `0o` 数値
- `language-configuration.json` — `//` コメント、括弧、
  自動補完ペア、`end` によるブロックのインデント
- `package.json` — `.gsh` に対する言語・文法・実行コマンド・F5 キーバインドの登録
- `extension.js` — `Golash: 実行` コマンド（保存→ターミナル確保→`gsh run`）

組み込み関数の一覧は処理系の登録内容（`builtin_*.go`）を反映しています。
組み込み関数を変更した場合は、`register("name", ...)` の出現箇所から
`support.function` の選択肢を再生成してください。
