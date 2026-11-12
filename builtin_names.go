package main

// guiBuiltinNames は GUI 関連の組込（常に登録される）を列挙する。
// TestBuiltinRegistryComplete から参照される。
var guiBuiltinNames = []string{
	"screen", "gsel",
	"pset", "pget", "line", "rect", "ellipse",
	"gcopy", "gblit", "gmode", "picload",
	"pngsave", "galpha", "font",
	"gquad", "tri",
	"getkey", "mouse", "mousewheel", "fullscreen", "closing", "cursor",
	"padcount", "padbtn", "padaxis", "padname", "resizable",
	"touch", "dropfiles", "dropload",
	"mmload", "mmplay", "mmstop", "mmvol", "mmpitch",
	"button", "inputbox", "ime",
	"listbox", "clrobj", "dialog",
	"chkbox", "combox", "mesbox", "objset", "objget", "objenable",
	"wbind", "wbind_items", "wunbind", "wsync",
}
