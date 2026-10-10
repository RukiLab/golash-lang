#mode cli
mes("計測中...")
i = 0
start_time = gettime(5) * 60 + gettime(6) + float(gettime(7)) / 1000
repeat 10000000 {
    i += 1
}
end_time = gettime(5) * 60 + gettime(6) + float(gettime(7)) / 1000
time = end_time - start_time
mes(time)
