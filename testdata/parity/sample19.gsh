// HSP後継言語 v0.2 サンプル (§19)
def greet(p)
    mes("Hello, " + p[0])
end

people = [
    ["Alice", 20],
    ["Bob", 25],
]

repeat 2 as i
    greet(people[i])
end

x = 15

if x % 2 == 0
    mes("even")
else
    mes("odd")
end
