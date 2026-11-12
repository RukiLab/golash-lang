// 定数（変数への代入で代用）の作例
Color_Red = 0
Color_Green = 1
Color_Blue = 2
mes(Color_Red)
mes(Color_Green)
mes(Color_Blue)

Low = 3
High = 10
Max = 11
mes(Low)
mes(High)
mes(Max)

Dir_North = 12
Dir_South = 13
Dir_East = 14
Dir_West = 15
mes(Dir_West)

d = Dir_South
switch d
    case Dir_North
        mes("north")
    case Dir_South
        mes("south")
    default
        mes("other")
end
