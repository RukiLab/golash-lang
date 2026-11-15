package main

// guiBuiltinNames は GUI 関連の組込（常に登録される）を列挙する。
// TestBuiltinRegistryComplete から参照される。
var guiBuiltinNames = []string{
	"screen", "gfxsel",
	"pset", "pget", "line", "rect", "ellipse",
	"gfxcopy", "gfxblit", "gfxmode", "gfxfile",
	"font",
	"gfxquad", "tri",
	"getkey", "mouse", "mousewheel", "fullscreen", "closing", "cursor",
	"pad", "padbtn", "padaxis", "resizable",
	"touch", "dropload",
	"sndload", "sndplay", "sndstop", "sndvol", "sndpitch",
	"button", "inputbox", "ime",
	"listbox", "objdel",
	"chkbox", "slider", "combox", "msgbox", "objset", "objget", "objenable",
}
