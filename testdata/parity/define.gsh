// 定数（変数への代入で代用）の作例
Color_Red = 0
Color_Green = 1
Color_Blue = 2
msg(Color_Red)
msg(Color_Green)
msg(Color_Blue)

Low = 3
High = 10
Max = 11
msg(Low)
msg(High)
msg(Max)

Dir_North = 12
Dir_South = 13
Dir_East = 14
Dir_West = 15
msg(Dir_West)

d = Dir_South
switch d
    case Dir_North
        msg("north")
    case Dir_South
        msg("south")
    default
        msg("other")
end
