screen(640, 480)
title("Goulash forms demo")

button(1, "OK", 20, 400, 120, 40)
button(2, "終了", 500, 400, 120, 40)
chkbox(3, "同意する", 20, 350, 0)
combox(4, 240, 350, 200, 36, ["red", "green", "blue"], 0)
mesbox(5, 20, 60, 300, 200, "one\ntwo")

n = 0

repeat 600 {
    cls()
    mes("forms demo: check, drop down, edit")
    mes("")
    mes("agree=" + str(checked(3)))
    mes("color #" + str(selected(4)))
    mes("memo=" + getstr(5))
    if checked(3) {
        mes("agreed!")
    }
    if pressed(1) {
        n = n + 1
    }
    if n >= 3 {
        objprm(1, "enable", 0)
    }
    if pressed(2) {
        break
    }
    if getkey(27) {
        break
    }
    await()
}

mes("bye!")
sleep(800)
end(0)
