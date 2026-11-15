//go:build linux

package main

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
#include <stdlib.h>

static gint golash_message_box(const char *title, const char *msg, int warning, int yesno) {
	GtkMessageType type = warning ? GTK_MESSAGE_WARNING : GTK_MESSAGE_OTHER;
	GtkButtonsType buttons = yesno ? GTK_BUTTONS_YES_NO : GTK_BUTTONS_OK;
	GtkWidget *dlg = gtk_message_dialog_new(NULL, GTK_DIALOG_MODAL, type, buttons, "%s", msg);
	gtk_window_set_title(GTK_WINDOW(dlg), title);
	gtk_window_set_position(GTK_WINDOW(dlg), GTK_WIN_POS_CENTER);
	gtk_window_set_keep_above(GTK_WINDOW(dlg), TRUE);
	gint r = gtk_dialog_run(GTK_DIALOG(dlg));
	gtk_widget_destroy(dlg);
	while (gtk_events_pending()) {
		gtk_main_iteration();
	}
	return r;
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"
)

var gtkMessageOnce sync.Once
var gtkMessageReady bool

// osMessageBox は GTK のメッセージダイアログを表示する。
// 標準は GTK_MESSAGE_OTHER（アイコンなし）、警告は GTK_MESSAGE_WARNING。
func osMessageBox(title, text string, warning, yesNo bool) (bool, error) {
	gtkMessageOnce.Do(func() {
		gtkMessageReady = C.gtk_init_check(nil, nil) != 0
	})
	if !gtkMessageReady {
		return false, fmt.Errorf("GTK を初期化できません（X サーバがありません）")
	}
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	cm := C.CString(text)
	defer C.free(unsafe.Pointer(cm))
	r := C.golash_message_box(ct, cm, cBoolInt(warning), cBoolInt(yesNo))
	if yesNo {
		return r == C.GTK_RESPONSE_YES, nil
	}
	return true, nil
}

func cBoolInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
