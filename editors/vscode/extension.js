// Golash (.gsh) VS Code extension host: F5 run commands.
// Grammar-only would rely on sendSequence, which silently does
// nothing without an active terminal; real commands create one.

const vscode = require('vscode');

function runScript() {
  const editor = vscode.window.activeTextEditor;
  if (!editor || editor.document.languageId !== 'goulash') {
    vscode.window.showWarningMessage('Golash: .gsh ファイルを開いて実行してください');
    return;
  }
  editor.document.save().then((ok) => {
    if (!ok) {
      return;
    }
    const file = editor.document.fileName;
    let term = vscode.window.activeTerminal;
    if (!term) {
      term = vscode.window.createTerminal('gsh');
    }
    term.show(true);
    term.sendText(`gsh run "${file}"`);
  });
}

function activate(context) {
  context.subscriptions.push(
    vscode.commands.registerCommand('goulash.run', () => runScript())
  );
}

function deactivate() {}

module.exports = { activate, deactivate };
