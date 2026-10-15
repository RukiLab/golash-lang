randomize()
answer = rnd(10) + 1
mes("1から10の数字を入力してください")
mes("よろしく")
pos(0, 16)
inputbox(0, 64, 24)
pos(64, 16)
button(1, "決定", 64, 24)
while true {
    if objget(1) {
        user_input = objget(0)
        if user_input == str(answer) {
            pos(0, 40)
            mes("正解")
            break
        } else {
            pos(0, 40)
            mes("不正解")
            break
        }
    }
}
