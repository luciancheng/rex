package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

type fakeTools struct {
	installed map[string]bool
	fail      map[string]bool
	ran       []string
	stdin     map[string][]byte
}

func (f *fakeTools) clipboard(goos string, env map[string]string) *clipboard {
	f.stdin = map[string][]byte{}
	return &clipboard{
		goos:   goos,
		getenv: func(k string) string { return env[k] },
		lookPath: func(name string) (string, error) {
			if f.installed[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		run: func(ctx context.Context, path string, args []string, stdin []byte) error {
			if _, ok := ctx.Deadline(); !ok {
				return errors.New("tools always run with a deadline")
			}
			name := path[strings.LastIndex(path, "/")+1:]
			f.ran = append(f.ran, strings.TrimSpace(name+" "+strings.Join(args, " ")))
			f.stdin[name] = stdin
			if f.fail[name] {
				return errors.New("exit status 1")
			}
			return nil
		},
	}
}

func TestClipboardBranches(t *testing.T) {
	cases := []struct {
		name      string
		goos      string
		env       map[string]string
		installed []string
		fail      []string
		status    string
		ran       string
		osc       bool
	}{
		{"mac", "darwin", nil, []string{"pbcopy"}, nil, "copied (pbcopy)", "pbcopy", false},
		{"mac tool fails", "darwin", nil, []string{"pbcopy"}, []string{"pbcopy"}, "sent (OSC 52) · may be blocked", "pbcopy", true},
		{"ssh", "darwin", map[string]string{"SSH_CONNECTION": "1 2 3 4"}, []string{"pbcopy"}, nil, "sent (OSC 52) · may be blocked", "", true},
		{"ssh tty", "linux", map[string]string{"SSH_TTY": "/dev/pts/1", "DISPLAY": ":0"}, []string{"xclip"}, nil, "sent (OSC 52) · may be blocked", "", true},
		{"wayland", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, []string{"wl-copy", "xclip"}, nil, "copied (wl-copy)", "wl-copy", false},
		{"x11", "linux", map[string]string{"DISPLAY": ":0"}, []string{"xclip"}, nil, "copied (xclip)", "xclip -selection clipboard", false},
		{"x11 xsel", "linux", map[string]string{"DISPLAY": ":0"}, []string{"xsel"}, nil, "copied (xsel)", "xsel --clipboard --input", false},
		{"no display", "linux", nil, []string{"xclip"}, nil, "sent (OSC 52) · may be blocked", "", true},
		{"windows", "windows", nil, []string{"clip.exe"}, nil, "copied (clip.exe)", "clip.exe", false},
		{"nothing installed", "darwin", nil, nil, nil, "sent (OSC 52) · may be blocked", "", true},
	}
	for _, c := range cases {
		f := &fakeTools{installed: map[string]bool{}, fail: map[string]bool{}}
		for _, n := range c.installed {
			f.installed[n] = true
		}
		for _, n := range c.fail {
			f.fail[n] = true
		}
		r := f.clipboard(c.goos, c.env).Copy("héllo 😀")
		if r.status != c.status || strings.Join(f.ran, ";") != c.ran || (r.osc != "") != c.osc {
			t.Errorf("%s: %+v ran %q", c.name, r, f.ran)
		}
	}
}

func TestClipExeGetsUTF16WithABOM(t *testing.T) {
	f := &fakeTools{installed: map[string]bool{"clip.exe": true}}
	f.clipboard("windows", nil).Copy("é😀")
	got := f.stdin["clip.exe"]
	if len(got) < 2 || got[0] != 0xff || got[1] != 0xfe {
		t.Fatalf("% x", got)
	}
	var units []uint16
	for i := 2; i+1 < len(got); i += 2 {
		units = append(units, uint16(got[i])|uint16(got[i+1])<<8)
	}
	if string(utf16.Decode(units)) != "é😀" {
		t.Fatalf("% x", got)
	}
}

func TestOSC52(t *testing.T) {
	if osc52("hi") != "\x1b]52;c;aGk=\a" {
		t.Fatalf("%q", osc52("hi"))
	}
}

func TestCopiesFinishInTheOrderAsked(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	var order []string
	c := &clipboard{goos: "darwin", getenv: func(string) string { return "" }, lookPath: func(n string) (string, error) { return n, nil },
		run: func(ctx context.Context, path string, args []string, stdin []byte) error {
			if string(stdin) == "first" {
				<-release // the first copy is slow
			}
			mu.Lock()
			order = append(order, string(stdin))
			mu.Unlock()
			return nil
		}}
	first, second := c.copyAsync("first"), c.copyAsync("second")
	close(release)
	<-first
	<-second
	if strings.Join(order, ",") != "first,second" {
		t.Fatalf("the last brief asked for is the one left on the clipboard: %v", order)
	}
}

func TestToolsShareOneDeadline(t *testing.T) {
	var deadlines []time.Time
	display := func(k string) string {
		if k == "WAYLAND_DISPLAY" || k == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	c := &clipboard{goos: "linux", getenv: display, lookPath: func(n string) (string, error) { return n, nil },
		run: func(ctx context.Context, path string, args []string, stdin []byte) error {
			d, _ := ctx.Deadline()
			deadlines = append(deadlines, d)
			return errors.New("no display")
		}}
	if r := c.Copy("x"); r.osc == "" {
		t.Fatal("every tool failed: OSC 52")
	}
	if len(deadlines) != 3 || !deadlines[0].Equal(deadlines[2]) {
		t.Fatalf("wl-copy, xclip and xsel share one %v deadline: %v", copyTimeout, deadlines)
	}
}

func TestAReplacedBriefStillGetsItsRecord(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	slow := make(chan copyResult, 1)
	a.copy = func(string) <-chan copyResult { return slow }
	typeText(a, "s")
	press(a, "\r") // copying…
	press(a, "esc")
	var copied []string
	a.copy = fakeCopy(&copied, copyResult{status: "copied (fake)"})
	typeText(a, "b")
	press(a, "\r")
	slow <- copyResult{status: "copied (slow)"}
	a.pollCopies()
	var records []string
	for _, ev := range a.log {
		if ev.Kind == "sendoff" || ev.Kind == "brief" {
			records = append(records, ev.Kind+": "+ev.Text)
		}
	}
	if len(records) != 2 || !strings.Contains(strings.Join(records, "|"), "copied (slow)") {
		t.Fatalf("both briefs leave their line on the tape: %v", records)
	}
}
