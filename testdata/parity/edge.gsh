a = [[[[[1, 2]]]]]
msg(a[0][0][0][0][1])
a[0][0][0][0][0] = 99
msg(a[0][0][0][0][0])

data = [["Zed", 77]]
msg(data[0][0])
msg(data[0][1])

total = 0
repeat 5
    total = total + 1
end
msg(total)

def mymax(a, b)
    if a > b
        return a
    else
        return b
    end
end
msg(mymax(3, 7))
msg(mymax(9, 2))

f = 2.5
msg(f * 4)
msg(10 == 10.0)
msg(true && !false)
