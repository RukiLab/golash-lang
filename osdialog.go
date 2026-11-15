// OSネイティブのメッセージボックス。
//
// dialog() の 0〜3 は、ゲームウィンドウ内ではなく OS の標準／警告
// メッセージボックスを表示する。実装は OS ごとの osdialog_*.go に置く。
// テストでは showOSMessageBox を差し替えて、実際のダイアログを開かずに
// 引数の対応を検査する。
package main

// showOSMessageBox はテストで差し替えるためのフック。
var showOSMessageBox = osMessageBox
