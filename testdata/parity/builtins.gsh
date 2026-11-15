// Builtin function showcase (HSP-derived, all functions).
randomize(1234)

msg("1d6 x5:")
repeat 5 as i
    msg("  " + str(rnd(6) + 1))
end

s = "  Hello, HSP World!  "
msg("len=" + str(len(s)))
msg("mid=" + slice(s, 2, 7))
msg("pos=" + str(find(s, "HSP")))
msg("trim=[" + strtrim(s) + "]")
words = split("a,b,c", ",")
msg("split: " + str(words) + " n=" + str(len(words)))

msg(strf("%04d", 42))
msg("pi=" + str(3.14159))
msg("abs=" + str(abs(-7)) + " sqrt=" + str(sqrt(2)))
msg("clamp=" + str(limit(99, 0, 10)))

msg("path base=" + getpath("C:\\work\\app.gsh", 9))
msg("path ext=" + getpath("C:\\work\\app.gsh", 2))
msg("year=" + str(gettime(0)) + " month=" + str(gettime(1)))

buf = [0, 0, 0, 0]
buf[0] = 65
buf[1] = 66
buf[2] = 67
msg("b0=" + str(buf[0]) + " b12=" + str(buf[1] + buf[2] * 256))
msg("types: " + vartype(1) + " " + vartype("s") + " " + vartype(buf))

stack = []
push(stack, 1, 2, 3)
msg("stack: " + join(stack, "-") + " pop=" + str(pop(stack)))
u = ["Alice", 98]
msg("user: " + u[0] + " " + str(u[1]))
total = 0
repeat [1, 2, 3] as x
    total += x
end
msg("total=" + str(total))

sleep(10)
msg("done")
