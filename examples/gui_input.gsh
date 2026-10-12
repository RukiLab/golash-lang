screen(640, 480)
title("Golash input demo")

repeat 100000 {
    cls()
    mes("move the mouse, press arrows...")
    mes("click or ESC to exit")
    mes("")
    mes("mouse=" + str(mousex()) + "," + str(mousey()))
    mes("arrows=" + str(getkey(37)) + str(getkey(38)) + str(getkey(39)) + str(getkey(40)))
    if clicked() {
        break
    }
    if getkey(27) {
        break
    }
    await()
}

cls()
mes("bye!")
sleep(800)
end(0)
