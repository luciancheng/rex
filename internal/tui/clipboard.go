package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"runtime"
	"sync"
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

	// turn passes from one copy to the next, so copies run one at a time
	// in the order they were asked for: the last brief asked for is the
	// one left on the clipboard.
	turn chan struct{}
	once sync.Once
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
// to OSC 52. All the tools it tries share one copyTimeout (plus the pipe
// wait of the last one), so callers run it off the UI goroutine.
func (c *clipboard) Copy(text string) copyResult {
	ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
	defer cancel()
	for _, t := range c.tools() {
		if ctx.Err() != nil {
			break
		}
		path, err := c.lookPath(t.name)
		if err != nil {
			continue
		}
		data := []byte(text)
		if t.utf16 {
			data = utf16LE(text)
		}
		if err = c.run(ctx, path, t.args, data); err == nil {
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
// Copies wait their turn, in the order they were asked for.
func (c *clipboard) copyAsync(text string) <-chan copyResult {
	c.once.Do(func() {
		c.turn = make(chan struct{}, 1)
		c.turn <- struct{}{}
	})
	done := make(chan copyResult, 1)
	ready := c.turn
	next := make(chan struct{}, 1)
	c.turn = next
	go func() {
		<-ready
		done <- c.Copy(text)
		next <- struct{}{}
	}()
	return done
}
