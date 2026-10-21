//go:build unix

package tui

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var quitSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

func notifyResume(ch chan os.Signal) { signal.Notify(ch, syscall.SIGCONT) }

// reenterRaw restores cbreak after a stop/continue, when the shell may
// have put the TTY back into cooked mode.
func reenterRaw(fd int) {
	if mode, err := unix.IoctlGetTermios(fd, ioctlReadTermios); err == nil {
		next := cbreak(*mode)
		_ = unix.IoctlSetTermios(fd, ioctlWriteTermios, &next)
	}
}

func enableWindowsVT(_ *os.File) func() { return func() {} }

// enterRaw puts the TTY in cbreak: no echo, no line buffering, no signal
// keys (Ctrl+C reaches Rex as a byte), no XON/XOFF (Ctrl+S would freeze
// output) and no IEXTEN (^V/^O). OPOST stays on so "\n" still returns the
// carriage; Rex never uses full raw mode.
func enterRaw(fd int) (func(), error) {
	orig, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return func() {}, err
	}
	mode := cbreak(*orig)
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &mode); err != nil {
		return func() {}, err
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, ioctlWriteTermios, orig)
	}, nil
}

func cbreak(mode unix.Termios) unix.Termios {
	mode.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	mode.Iflag &^= unix.IXON
	mode.Cc[unix.VMIN] = 1
	mode.Cc[unix.VTIME] = 0
	return mode
}

func waitRead(fd int, timeout time.Duration) bool {
	tv := unix.NsecToTimeval(timeout.Nanoseconds())
	fds := &unix.FdSet{}
	fds.Set(fd)
	n, err := unix.Select(fd+1, fds, nil, nil, &tv)
	return err == nil && n > 0
}

// keyReader reads every byte the terminal has ready and decodes it into
// key tokens, so queued arrows, a scroll burst or a paste arrive whole.
type keyReader struct {
	fd  int
	dec keyDecoder
	buf []byte
}

func newKeyReader(stream *os.File) *keyReader {
	return &keyReader{fd: int(stream.Fd()), buf: make([]byte, 4096)}
}

// Read waits up to timeout for input and returns the keys it decoded.
// When the input is gone (EOF, EIO after a hangup) it resolves whatever
// is pending and waits out the timeout, so the caller's loop still reaches
// its signal checks instead of spinning on a dead terminal.
func (k *keyReader) Read(timeout time.Duration) []string {
	var out []string
	if waitRead(k.fd, timeout) {
		toks, ok := k.read()
		out = append(out, toks...)
		if !ok {
			return k.gone(out, timeout)
		}
	}
	for {
		pending, wait := k.dec.waiting()
		if !pending {
			return out
		}
		if waitRead(k.fd, wait) {
			toks, ok := k.read()
			out = append(out, toks...)
			if !ok {
				return k.gone(out, timeout)
			}
			continue
		}
		return append(out, k.dec.flush(time.Now().Add(wait))...)
	}
}

func (k *keyReader) gone(out []string, timeout time.Duration) []string {
	far := time.Now().Add(time.Hour)
	out = append(out, k.dec.flush(far)...)
	out = append(out, k.dec.flush(far)...) // a stalled paste needs a second flush to end
	time.Sleep(timeout)
	return out
}

// read reports false when the terminal can't be read any more.
func (k *keyReader) read() ([]string, bool) {
	n, err := unix.Read(k.fd, k.buf)
	if err == unix.EINTR || err == unix.EAGAIN {
		return nil, true
	}
	if err != nil || n <= 0 {
		return nil, false
	}
	return k.dec.feed(k.buf[:n], time.Now()), true
}

// Bracketed paste is decoded by keyDecoder, so Unix turns it on.
const (
	pasteModeOn  = PasteOn
	pasteModeOff = PasteOff
)
