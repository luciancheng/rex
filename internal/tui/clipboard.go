package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"runtime"
	"time"
	"unicode/utf16"
)

// copyResult is what copying a brief did. osc, when set, is an OSC 52
// sequence the live loop writes to the terminal between frames.
type copyResult struct {
	status string
	osc    string
}

// clipboard copies text with the platform's tool, or OSC 52 when there is
// none (or over SSH, where a local tool would copy on the wrong machine).
// It says "copied (<tool>)" only when the tool exited 0; OSC 52 gets no
// answer from the terminal, so it says it may be blocked.
type clipboard struct {
	goos     string
	getenv   func(string) string
	lookPath func(string) (string, error)
	run      func(ctx context.Context, path string, args []string, stdin []byte) error
}

func newClipboard() *clipboard {
	return &clipboard{goos: runtime.GOOS, getenv: os.Getenv, lookPath: exec.LookPath, run: runTool}
}

const (
	copyTimeout  = 2 * time.Second
	copyWaitPipe = 500 * time.Millisecond // xclip keeps its pipes open after forking
)

func runTool(ctx context.Context, path string, args []string, stdin []byte) error {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = nil, nil
	cmd.WaitDelay = copyWaitPipe
	return cmd.Run()
}

type copyTool struct {
	name  string
	args  []string
	utf16 bool // clip.exe reads UTF-16LE with a BOM; UTF-8 comes out garbled
}

// tools are the clipboard tools worth trying here, in order.
func (c *clipboard) tools() []copyTool {
	if c.getenv("SSH_TTY") != "" || c.getenv("SSH_CONNECTION") != "" {
		return nil
	}
	switch c.goos {
	case "darwin":
		return []copyTool{{name: "pbcopy"}}
	case "windows":
		return []copyTool{{name: "clip.exe", utf16: true}}
	}
	var tools []copyTool
	if c.getenv("WAYLAND_DISPLAY") != "" {
		tools = append(tools, copyTool{name: "wl-copy"})
	}
	if c.getenv("DISPLAY") != "" {
		tools = append(tools, copyTool{name: "xclip", args: []string{"-selection", "clipboard"}},
			copyTool{name: "xsel", args: []string{"--clipboard", "--input"}})
	}
	return tools
}

// Copy runs the first tool that is installed and works, else falls back
// to OSC 52. It blocks for at most copyTimeout (plus the pipe wait), so
// callers run it off the UI goroutine.
func (c *clipboard) Copy(text string) copyResult {
	for _, t := range c.tools() {
		path, err := c.lookPath(t.name)
		if err != nil {
			continue
		}
		data := []byte(text)
		if t.utf16 {
			data = utf16LE(text)
		}
		ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
		err = c.run(ctx, path, t.args, data)
		cancel()
		if err == nil {
			return copyResult{status: "copied (" + t.name + ")"}
		}
	}
	return copyResult{status: "sent (OSC 52) · may be blocked", osc: osc52(text)}
}

// osc52 asks the terminal to put text on the clipboard.
func osc52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
}

func utf16LE(text string) []byte {
	units := utf16.Encode([]rune(text))
	out := make([]byte, 2, 2+2*len(units))
	out[0], out[1] = 0xff, 0xfe
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// copyAsync starts copying text and returns where the result arrives.
func (c *clipboard) copyAsync(text string) <-chan copyResult {
	done := make(chan copyResult, 1)
	go func() { done <- c.Copy(text) }()
	return done
}
