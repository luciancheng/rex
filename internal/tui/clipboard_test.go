package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
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
