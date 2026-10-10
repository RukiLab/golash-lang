// human_check.gsh — 人手検証テスト（GUI ビルド専用）。
//
// 実行: gsh run examples/human_check.gsh --gui
// 目視・操作が必要な項目を順に提示し、dialog() の Yes/No または
// 制限時間付きの自動判定で合否を記録します。最後に集計を表示します。

screen(640, 480)
title("human check")

passed = []
failed = []

def ok(name) {
    push(passed, name)
    mes("PASS: " + name)
}

def ng(name) {
    push(failed, name)
    mes("FAIL: " + name)
}

def ask(name, msg) {
    if dialog(msg, "yesno") == 1 {
        ok(name)
    } else {
        ng(name)
    }
}

// 次へボタンで待機する（時間経過ではなく操作で進む）。
// 指示 mes は呼び出し側で表示しておくこと。
def wait_next() {
    button(90, "次へ", 500, 420, 120, 40)
    while true {
        if pressed(90) {
            break
        }
        await()
    }
    clrobj(90)
}

// ---- T1: 描画・文字 ----
cls()
color(32, 32, 48)
boxf()
color(255, 255, 255)
boxf(10, 10, 100, 60)
color(255, 80, 80)
line(0, 0, 640, 480)
color(80, 255, 120)
circle(320, 240, 90, 1)
pset(320, 240)
color(255, 255, 255)
pos(16, 40)
mes("draw あいう 012")
mes("確認したら次へ")
wait_next()
ask("T1 draw+text", "図形（四角・対角線・円・点）と文字が正しく見えたか")

// ---- T2: フォント切替 ----
font("meiryo.ttc", 24)
cls()
pos(16, 40)
mes("フォント切替 あいう ABC")
mes("BOLD ABC 012", "bold")
mes("ITALIC ABC 012", "italic")
mes("UNDERLINE ABC 012", "underline")
mes("確認したら次へ")
wait_next()
ask("T2 font switch", "書体が游ゴシックから変わって見えたか")
font("YuGothR.ttc", 16)
cls()
pos(16, 40)
mes("16px に戻った")
wait_next()

// ---- T3: ボタン画像4状態 ----
gsel(1)
color(200, 60, 60)
boxf()
gsel(2)
color(60, 200, 60)
boxf()
gsel(3)
color(60, 60, 200)
boxf()
gsel(0)
color(255, 255, 255)
cls()
button(10, "", 260, 200, 120, 40, 1, 2, 3)
pos(16, 40)
mes("赤=通常。ホバー=緑、押下=青を確認して次へ")
wait_next()
ask("T3 button states", "通常赤・ホバー緑・押下青に変わったか")
objprm(10, "enable", 0)
sleep(500)
ask("T3 button disabled", "ボタンが減光（無効表示）されたか")
objprm(10, "enable", 1)
clrobj(10)

// ---- T4: ウィジェット表示・読取 ----
cls()
inputbox(11, 20, 60, 300, 36, "abc")
chkbox(12, "同意する", 20, 120, 1)
combox(13, 20, 170, 200, 36, ["red", "green", "blue"], 1)
mesbox(14, 340, 60, 280, 200, "one\ntwo")
if gettext(11) == "abc" {
    ok("T4 inputbox prefill")
} else {
    ng("T4 inputbox prefill")
}
if checked(12) {
    ok("T4 chkbox initial")
} else {
    ng("T4 chkbox initial")
}
if selected(13) == 1 {
    ok("T4 combox initial")
} else {
    ng("T4 combox initial")
}
if getstr(14) == "one\ntwo" {
    ok("T4 mesbox prefill")
} else {
    ng("T4 mesbox prefill")
}
// mesbox は表示専用。setstr で書き換えて getstr で読む。
setstr(14, "three\nfour")
if getstr(14) == "three\nfour" {
    ok("T4 mesbox setstr")
} else {
    ng("T4 mesbox setstr")
}
pos(16, 420)
mes("入力・チェック外し・選択変更を操作して次へ")
wait_next()
cls()
mes("agree=" + str(checked(12)))
mes("color=" + str(selected(13)))
mes("text=" + gettext(11))
ask("T4 widget interactive", "操作が読取に反映されたか")
clrobj()

// ---- T5: ダイアログ ----
if dialog("Yes を押してください", "yesno") == 1 {
    ok("T5 dialog yes")
} else {
    ng("T5 dialog yes")
}
if dialog("No を押してください", "yesno") == 0 {
    ok("T5 dialog no")
} else {
    ng("T5 dialog no")
}

// ---- T6: IME（即時inputポーリング） ----
cls()
pos(16, 40)
mes("10秒: 日本語を変換確定させ、最後にEnter")
mes("(確定文字列が input() で読める)")
ime(1)
s = ""
t0 = tick()
while tick() - t0 < 600 {
    k = input()
    if k != "" {
        if instr(k, "\r") >= 0 {
            break
        }
        s = s + k
    }
    await()
}
ime(0)
cls()
mes("入力=" + s)
ask("T6 ime input", "入力した通りに確定されたか")

// ---- T7: 音声 ----
snd = mmload("examples/assets/sfx_move.mp3")
mmplay(snd)
ask("T7 audio", "音が鳴ったか")
mmstop()

// ---- T8: キー自動判定（各5秒） ----
cls()
pos(16, 40)
mes("5秒: 左矢印キーを押す")
t0 = tick()
hit = false
while tick() - t0 < 300 {
    if getkey(37) {
        hit = true
    }
    await()
}
if hit {
    ok("T8 arrow key")
} else {
    ng("T8 arrow key")
}

cls()
pos(16, 40)
mes("5秒: Shift キーを押す")
t0 = tick()
hit = false
while tick() - t0 < 300 {
    if getkey(16) {
        hit = true
    }
    await()
}
if hit {
    ok("T8 shift key")
} else {
    ng("T8 shift key")
}

cls()
pos(16, 40)
mes("離して→5秒: A を3秒ほど長押し（リピートで複数回 a が出る）")
sleep(1000)
t0 = tick()
n = 0
while tick() - t0 < 300 {
    if input() == "a" {
        n = n + 1
    }
    await()
}
mes("fires=" + str(n))
if n >= 2 {
    ok("T8 input repeat")
} else {
    ng("T8 input repeat")
}

cls()
pos(16, 40)
mes("離して→5秒: A を押す（input で a が出る）")
sleep(1000)
t0 = tick()
hit = false
while tick() - t0 < 300 {
    if input() == "a" {
        hit = true
    }
    await()
}
if hit {
    ok("T8 input lower")
} else {
    ng("T8 input lower")
}

cls()
pos(16, 40)
mes("離して→5秒: Shift+A を押す（input で A が出る）")
sleep(1000)
t0 = tick()
hit = false
while tick() - t0 < 300 {
    if input() == "A" {
        hit = true
    }
    await()
}
if hit {
    ok("T8 input upper")
} else {
    ng("T8 input upper")
}

cls()
pos(16, 40)
mes("離して→5秒: Backspace を押す（asc で 8 が出る）")
sleep(1000)
t0 = tick()
hit = false
while tick() - t0 < 300 {
    k = input()
    if k != "" {
        if asc(k) == 8 {
            hit = true
        }
    }
    await()
}
if hit {
    ok("T8 input backspace")
} else {
    ng("T8 input backspace")
}

// ---- T9: マウス ----
cls()
pos(16, 40)
mes("5秒: 左クリックする")
t0 = tick()
hit = false
mx = 0
my = 0
while tick() - t0 < 300 {
    if clicked() {
        hit = true
        mx = mousex()
        my = mousey()
    }
    await()
}
mes("click=" + str(mx) + "," + str(my))
if hit {
    ok("T9 mouse click")
} else {
    ng("T9 mouse click")
}

// ---- 集計 ----
cls()
pos(16, 40)
mes("PASSED: " + str(length(passed)))
mes("FAILED: " + str(length(failed)))
repeat failed as f {
    mes("  miss: " + f)
}
dialog("終了", "ok")
