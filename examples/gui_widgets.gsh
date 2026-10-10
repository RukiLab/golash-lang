screen(640, 480)
title("Goulash widgets demo")

button(1, "Count", 20, 400, 120, 40)
button(2, "終了", 500, 400, 120, 40)
inputbox(3, 20, 350, 300, 36, "にほんご")
listbox(4, 340, 60, 280, 200, ["apple", "banana", "cherry"])

n = 0

repeat 100000 {
    cls()
    mes("press Count, pick a fruit, then Exit")
    mes("")
    mes("count=" + str(n))
    mes("input=" + gettext(3))
    sel = selected(4)
    if sel >= 0 {
        mes("fruit #" + str(sel))
    }
    if pressed(1) {
        n = n + 1
    }
    if pressed(2) {
        break
    }
    if getkey(27) {
        break
    }
    await()
}

r = dialog("bye?", "yesno")
mes("dialog=" + str(r))

end(0)
