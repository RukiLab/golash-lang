package main

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestProcSession(t *testing.T) {
	var cmd, arg1, arg2, want string
	if runtime.GOOS == "windows" {
		cmd, arg1, arg2 = "cmd", "/c", "echo hello"
		want = "hello"
	} else {
		cmd, arg1, arg2 = "sh", "-c", "printf hello"
		want = "hello"
	}
	src := fmt.Sprintf(`
p = procopen(%s, %s, %s)
if p[0] < 0
    msg("ERR " + p[1])
else
    out = ""
    i = 0
    while i < 300
        out = out + procread(p[0])
        st = procpoll(p[0])
        if st[0] == false
            break
        end
        sleep(10)
        i = i + 1
    end
    out = out + procread(p[0])
    code = procclose(p[0])
    msg("OUT=" + strtrim(out))
    msg("CODE=" + str(code))
end
`, strconv.Quote(cmd), strconv.Quote(arg1), strconv.Quote(arg2))
	out, errout, _, err := runIO(t, src, "")
	if err != nil {
		t.Fatalf("run: %v (%s)", err, errout)
	}
	if strings.Contains(out, "ERR ") {
		t.Fatalf("procopen failed: %s", out)
	}
	if !strings.Contains(out, "OUT="+want+"\n") {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(out, "CODE=0\n") {
		t.Fatalf("exit code missing: %q", out)
	}
}

func TestProcStdin(t *testing.T) {
	var cmd, arg1, arg2 string
	if runtime.GOOS == "windows" {
		cmd, arg1, arg2 = "cmd", "/c", "findstr hello"
	} else {
		cmd, arg1, arg2 = "sh", "-c", "grep hello"
	}
	src := fmt.Sprintf(`
p = procopen(%s, %s, %s)
if p[0] < 0
    msg("ERR " + p[1])
else
    msg(procwrite(p[0], "hello" + chr(10)))
    msg(proceof(p[0]))
    out = ""
    i = 0
    while i < 300
        out = out + procread(p[0])
        st = procpoll(p[0])
        if st[0] == false
        break
        end
        sleep(10)
        i = i + 1
    end
    out = out + procread(p[0])
    msg("OUT=" + strtrim(out))
    msg("CODE=" + str(procclose(p[0])))
end
`, strconv.Quote(cmd), strconv.Quote(arg1), strconv.Quote(arg2))
	out, _, _, err := runIO(t, src, "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "1\n1\n") {
		t.Fatalf("write/eof = %q", out)
	}
	if !strings.Contains(out, "OUT=hello\n") {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(out, "CODE=0\n") {
		t.Fatalf("exit = %q", out)
	}
}

func TestProcExitCode(t *testing.T) {
	var cmd, arg1, arg2 string
	if runtime.GOOS == "windows" {
		cmd, arg1, arg2 = "cmd", "/c", "exit 3"
	} else {
		cmd, arg1, arg2 = "sh", "-c", "exit 3"
	}
	src := fmt.Sprintf(`
p = procopen(%s, %s, %s)
if p[0] < 0
    msg("ERR")
else
    i = 0
    while i < 300
        st = procpoll(p[0])
        if st[0] == false
            break
        end
        sleep(10)
        i = i + 1
    end
    msg("CODE=" + str(procclose(p[0])))
end
`, strconv.Quote(cmd), strconv.Quote(arg1), strconv.Quote(arg2))
	out, _, _, err := runIO(t, src, "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "CODE=3\n") {
		t.Fatalf("exit code = %q", out)
	}
}

func TestProcOpenError(t *testing.T) {
	out, _, _, err := runIO(t, `
p = procopen("__golash_no_such_command_12345__")
msg(p[0] < 0)
msg(p[1] != "")
`, "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "1\n1\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestProcPollUnknown(t *testing.T) {
	out, _, _, err := runIO(t, `
st = procpoll(999999)
msg(st[0])
msg(st[1])
msg(procread(999999) == "")
msg(procwrite(999999, "x") == false)
msg(procclose(999999))
`, "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "0\n-1\n1\n1\n-1\n" {
		t.Fatalf("out = %q", out)
	}
}
