package main

import (
	"errors"
	"testing"

	"github.com/sqweek/dialog"
)

type dialogMsgCall struct {
	title   string
	text    string
	warning bool
	yesNo   bool
}

type dialogFileCall struct {
	title  string
	filter string
	save   bool
}

func TestDialogNativeModes(t *testing.T) {
	oldMsg, oldFile := showOSMessageBox, showOSFileDialog
	defer func() {
		showOSMessageBox = oldMsg
		showOSFileDialog = oldFile
	}()

	var msgs []dialogMsgCall
	showOSMessageBox = func(title, text string, warning, yesNo bool) (bool, error) {
		msgs = append(msgs, dialogMsgCall{title: title, text: text, warning: warning, yesNo: yesNo})
		return !yesNo, nil
	}
	var files []dialogFileCall
	showOSFileDialog = func(_ *Interp, title, filter string, save bool) (string, error) {
		files = append(files, dialogFileCall{title: title, filter: filter, save: save})
		return "C:/tmp/out.txt", nil
	}

	mustOutIO(t,
		"msg(dialog(\"m\"))\nmsg(dialog(\"m\", 1))\nmsg(dialog(\"m\", 2))\nmsg(dialog(\"m\", 3))\nmsg(dialog(\"filt\", 4))\nmsg(dialog(\"filt\", 5))\n",
		"", "1\n1\n0\n0\nC:/tmp/out.txt\nC:/tmp/out.txt\n")

	wantMsgs := []dialogMsgCall{
		{title: "Golash", text: "m", warning: false, yesNo: false},
		{title: "Golash", text: "m", warning: true, yesNo: false},
		{title: "Golash", text: "m", warning: false, yesNo: true},
		{title: "Golash", text: "m", warning: true, yesNo: true},
	}
	if len(msgs) != len(wantMsgs) {
		t.Fatalf("message box calls = %d, want %d", len(msgs), len(wantMsgs))
	}
	for i, want := range wantMsgs {
		if msgs[i] != want {
			t.Errorf("message box %d = %+v, want %+v", i, msgs[i], want)
		}
	}

	wantFiles := []dialogFileCall{
		{title: "開く", filter: "filt", save: false},
		{title: "保存", filter: "filt", save: true},
	}
	if len(files) != len(wantFiles) {
		t.Fatalf("file dialog calls = %d, want %d", len(files), len(wantFiles))
	}
	for i, want := range wantFiles {
		if files[i] != want {
			t.Errorf("file dialog %d = %+v, want %+v", i, files[i], want)
		}
	}
}

func TestDialogNativeErrors(t *testing.T) {
	oldMsg, oldFile := showOSMessageBox, showOSFileDialog
	defer func() {
		showOSMessageBox = oldMsg
		showOSFileDialog = oldFile
	}()

	showOSMessageBox = func(title, text string, warning, yesNo bool) (bool, error) {
		return false, errors.New("message failed")
	}
	mustErrIO(t, "dialog(\"x\")\n", "dialog：message failed")

	showOSFileDialog = func(_ *Interp, title, filter string, save bool) (string, error) {
		return "", errors.New("file failed")
	}
	mustErrIO(t, "dialog(\"x\", 4)\n", "dialog：file failed")

	showOSFileDialog = func(_ *Interp, title, filter string, save bool) (string, error) {
		return "", dialog.ErrCancelled
	}
	mustOutIO(t, "msg(dialog(\"x\", 5))\n", "", "\n")
}
