package tui

import (
	"io"
	"os"
	"os/signal"

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
	PasteOn    = "\033[?2004h"
	PasteOff   = "\033[?2004l"
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

// screenSignals tells the live loop to leave (quit) or to restore cbreak and
// repaint after the process was stopped and continued (resume).
type screenSignals struct {
	quit   <-chan os.Signal
	resume <-chan os.Signal
}

// LiveScreen runs fn on the alternate screen in cbreak mode and always puts
// the terminal back: on return, on panic, and on SIGTERM/SIGHUP/interrupt,
// which make fn return through the same path.
func LiveScreen(stdout *os.File, stdin *os.File, fn func(screenSignals) error) error {
	restoreOut := enableWindowsVT(stdout)
	_, _ = stdout.WriteString(AltEnter + WrapOff + HideCursor + PasteOn + Clear + Home)
	_ = stdout.Sync()
	restore, err := enterRaw(int(stdin.Fd()))
	defer func() {
		_, _ = stdout.WriteString(PasteOff + WrapOn + ShowCursor + AltLeave + Reset)
		_ = stdout.Sync()
		restoreOut()
	}()
	if err != nil {
		return err
	}
	defer restore()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, quitSignals...)
	defer signal.Stop(quit)
	resume := make(chan os.Signal, 1)
	notifyResume(resume)
	defer signal.Stop(resume)
	return fn(screenSignals{quit: quit, resume: resume})
}

func RestoreOnInterrupt(w io.Writer) {
	if f, ok := w.(*os.File); ok {
		if !isFileTTY(f) {
			return
		}
	}
	_, _ = io.WriteString(w, PasteOff+ShowCursor+Reset)
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
