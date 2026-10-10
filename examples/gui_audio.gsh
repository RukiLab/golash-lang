screen(640, 480)
title("Goulash audio demo")

mes("audio demo: mmload/mmplay/mmvol/mmstop")
mes("")
if exist("se.wav") {
    se = mmload("se.wav")
    mmvol(se, 80)
    mes("press Z for SE, ESC to quit")
    repeat 100000 {
        cls()
        mes("press Z for SE, ESC to quit")
        if getkey(90) {
            mmplay(se)
            sleep(200)
        }
        if getkey(27) {
            break
        }
        await()
    }
    mmstop(se)
} else {
    mes("put se.wav next to this file, then rerun")
    sleep(2500)
}
end(0)
