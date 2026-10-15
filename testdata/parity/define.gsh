#mode cli
// 連番定数の作例（#define 無値形）
#define Color_Red
#define Color_Green
#define Color_Blue
mes(Color_Red)
mes(Color_Green)
mes(Color_Blue)

#define Low
#define High 10
#define Max
mes(Low)
mes(High)
mes(Max)

#define Dir_North
#define Dir_South
#define Dir_East
#define Dir_West
mes(Dir_West)

d = Dir_South
switch d {
    case Dir_North {
        mes("north")
    }
    case Dir_South {
        mes("south")
    }
    default {
        mes("other")
    }
}

// 字句解析の作例（エディタ支援向け）
t = lextokens("x = 1")
mes(t[0][0])
mes(t[0][1])
