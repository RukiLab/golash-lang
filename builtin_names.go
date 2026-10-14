package main

// guiBuiltinNames は GUI 関連の組込（常に登録される）を列挙する。
// TestBuiltinRegistryComplete から参照される。
var guiBuiltinNames = []string{
	"screen", "gsel", "buffer",
	"pset", "pget", "line", "boxf", "circle",
	"gcopy", "gquad", "gmode", "picload",
	"pngsave", "galpha", "paint", "font",
	"getkey", "mousex", "mousey", "clicked", "mousedown", "mousewheel", "fullscreen", "closing", "screensize", "cursor",
	"padcount", "padbtn", "padaxis", "padname", "resizable",
	"touchcount", "touchx", "touchy", "winmove", "dropfiles", "dropload",
	"mmload", "mmplay", "mmstop", "mmvol",
	"button", "pressed", "inputbox", "gettext", "ime",
	"listbox", "setlist", "sellist", "selected", "clrobj", "dialog",
	"chkbox", "checked", "combox", "mesbox", "getstr", "setstr", "objprm",
}
