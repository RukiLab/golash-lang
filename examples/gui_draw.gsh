screen(640, 480)
title("Golash draw demo")

color(40, 40, 120)
boxf(0, 0, 640, 480)

color(255, 255, 255)
boxf(10, 10, 100, 60)

color(255, 80, 80)
line(0, 0, 640, 480)
line(640, 0, 0, 480)

color(80, 255, 120)
circle(320, 240, 90, 1)

color(255, 220, 80)
circle(320, 240, 120)

color(255, 60, 60)
circle(520, 380, 40)
paint(520, 380)

gsel(1)
color(0, 200, 200)
boxf(0, 0, 60, 40)
color(255, 255, 255)
circle(30, 20, 15, 1)
gsel(0)
pos(0, 360)
gcopy(1, 0, 0, 60, 40, 2, 2)

pos(224, 280)
gcopy(1, 0, 0, 60, 40, 1, 1, 30)
galpha(128)
pos(224, 400)
gcopy(1, 0, 0, 60, 40)
galpha(255)

gquad(1,
    [300, 420, 390, 270], [300, 300, 380, 380],
    [0, 60, 60, 0], [0, 0, 40, 40],
    [16777215, 16777215, 16777215, 16777215])
gquad(1,
    [460, 580, 460, 300, 460, 580, 300, 460],
    [300, 300, 380, 380, 390, 390, 470, 470],
    [0, 60, 0, 0, 0, 60, 0, 0],
    [0, 0, 40, 40, 0, 0, 40, 40],
    [16777215, 16777215, 16777215, 16777215, 16711680, 65280, 255, 16777215])

color(255, 0, 0)
pset(5, 470)
assert(pget(5, 470) == 255 * 65536, "pget roundtrip")
mes("pget OK")

font(28)
color(255, 255, 255)
pos(16, 40)
mes("draw demo")
font(16)
mes("pset/line/boxf/circle OK")
mes("paint/gcopy/galpha/font/pngsave OK")
mes("gquad OK")

pngsave("draw_demo.png")
mes("saved draw_demo.png")

sleep(2500)
end(0)
