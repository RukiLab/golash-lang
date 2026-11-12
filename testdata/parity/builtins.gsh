// Builtin function showcase (HSP-derived, all functions).
randomize(1234)

mes("1d6 x5:")
repeat 5 as i
    mes("  " + str(rnd(6) + 1))
end

s = "  Hello, HSP World!  "
mes("len=" + str(strlen(s)))
mes("mid=" + strmid(s, 2, 5))
mes("pos=" + str(instr(s, "HSP")))
mes("trim=[" + strtrim(s) + "]")
words = split("a,b,c", ",")
mes("split: " + str(words) + " n=" + str(length(words)))

mes(strf("%04d", 42))
mes("pi=" + str(3.14159))
mes("abs=" + str(abs(-7)) + " sqrt=" + str(sqrt(2)))
mes("clamp=" + str(limit(99, 0, 10)))

mes("path base=" + getpath("C:\\work\\app.gsh", 9))
mes("path ext=" + getpath("C:\\work\\app.gsh", 2))
mes("year=" + str(gettime(0)) + " month=" + str(gettime(1)))

t = "one\ntwo\nthree"
mes("lines=" + str(notemax(t)) + " line2=" + noteget(t, 1))
mes(noteadd(t, 3, "four"))

buf = [0, 0, 0, 0]
buf[0] = 65
buf[1] = 66
buf[2] = 67
mes("b0=" + str(buf[0]) + " b12=" + str(buf[1] + buf[2] * 256))
mes("types: " + vartype(1) + " " + vartype("s") + " " + vartype(buf))

stack = []
push(stack, 1, 2, 3)
mes("stack: " + join(stack, "-") + " pop=" + str(pop(stack)))
u = ["Alice", 98]
mes("user: " + u[0] + " " + str(u[1]))
total = 0
repeat [1, 2, 3] as x
    total += x
end
mes("total=" + str(total))

sleep(10)
mes("done")
