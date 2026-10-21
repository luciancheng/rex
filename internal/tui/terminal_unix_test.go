//go:build unix

package tui

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestCbreakKeepsOPOST(t *testing.T) {
	var m unix.Termios
	m.Lflag = unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	m.Iflag = unix.IXON | unix.ICRNL
	m.Oflag = unix.OPOST
	got := cbreak(m)
	if got.Lflag&(unix.ECHO|unix.ICANON|unix.ISIG|unix.IEXTEN) != 0 {
		t.Fatalf("lflag %#x", got.Lflag)
	}
	if got.Iflag&unix.IXON != 0 || got.Iflag&unix.ICRNL == 0 {
		t.Fatalf("iflag %#x: IXON off, the rest kept", got.Iflag)
	}
	if got.Oflag&unix.OPOST == 0 {
		t.Fatal("OPOST must stay on")
	}
	if got.Cc[unix.VMIN] != 1 || got.Cc[unix.VTIME] != 0 {
		t.Fatal("vmin/vtime")
	}
}
