//go:build unix

package tui

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func enableWindowsVT(_ *os.File) {}

func enterRaw(fd int) (func(), error) {
	orig, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return func() {}, err
	}
	mode := *orig
	mode.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG
	mode.Cc[unix.VMIN] = 1
	mode.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &mode); err != nil {
		return func() {}, err
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, ioctlWriteTermios, orig)
	}, nil
}

func waitRead(fd int, timeout time.Duration) bool {
	tv := unix.NsecToTimeval(timeout.Nanoseconds())
	fds := &unix.FdSet{}
	fds.Set(fd)
	n, err := unix.Select(fd+1, fds, nil, nil, &tv)
	return err == nil && n > 0
}

func ReadKey(stream *os.File, timeout time.Duration) (string, bool) {
	fd := int(stream.Fd())
	if !waitRead(fd, timeout) {
		return "", false
	}
	buf := make([]byte, 1)
	n, err := unix.Read(fd, buf)
	if err != nil || n == 0 {
		return "", false
	}
	ch := string(buf[:n])
	if ch == "\x01" {
		return "home", true
	}
	if ch == "\x05" {
		return "end", true
	}
	if ch != "\x1b" {
		return ch, true
	}
	extra := ""
	if waitRead(fd, 20*time.Millisecond) {
		rest := make([]byte, 8)
		n, err := unix.Read(fd, rest)
		if err == nil && n > 0 {
			extra = string(rest[:n])
		}
	}
	seq := ch + extra
	names := map[string]string{
		"\x1b[A":  "up",
		"\x1b[B":  "down",
		"\x1b[C":  "right",
		"\x1b[D":  "left",
		"\x1b[5~": "pageup",
		"\x1b[6~": "pagedown",
		"\x1b[H":  "home",
		"\x1b[1~": "home",
		"\x1b[7~": "home",
		"\x1bOH":  "home",
		"\x1b[F":  "end",
		"\x1b[4~": "end",
		"\x1b[8~": "end",
		"\x1bOF":  "end",
		"\x1b":    "esc",
	}
	if name, ok := names[seq]; ok {
		return name, true
	}
	return seq, true
}
