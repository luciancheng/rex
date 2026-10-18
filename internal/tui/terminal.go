package tui

import (
	"io"
	"os"

	"golang.org/x/term"
)

const (
	HideCursor = "\033[?25l"
	ShowCursor = "\033[?25h"
	AltEnter   = "\033[?1049h"
	AltLeave   = "\033[?1049l"
	WrapOff    = "\033[?7l"
	WrapOn     = "\033[?7h"
	Home       = "\033[H"
	Clear      = "\033[2J"
	ClearDown  = "\033[J"
	Reset      = "\033[0m"
	CtrlC      = "\x03"
)

func Size() (int, int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}

func LiveScreen(stdout *os.File, stdin *os.File, fn func() error) error {
	enableWindowsVT(stdout)
	_, _ = stdout.WriteString(AltEnter + WrapOff + HideCursor + Clear + Home)
	_ = stdout.Sync()
	restore, err := enterRaw(int(stdin.Fd()))
	defer func() {
		_, _ = stdout.WriteString(WrapOn + ShowCursor + AltLeave + Reset)
		_ = stdout.Sync()
	}()
	if err != nil {
		return err
	}
	defer restore()
	return fn()
}

func RestoreOnInterrupt(w io.Writer) {
	if f, ok := w.(*os.File); ok {
		if !isFileTTY(f) {
			return
		}
	}
	_, _ = io.WriteString(w, ShowCursor+Reset)
	if f, ok := w.(*os.File); ok {
		_ = f.Sync()
	}
}

func isFileTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
