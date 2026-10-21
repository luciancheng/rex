//go:build windows

package tui

import (
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var quitSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

func notifyResume(chan os.Signal) {}

func reenterRaw(int) {}

const keyEvent = 0x0001

type inputRecord struct {
	EventType uint16
	_         uint16
	Event     [16]byte
}

type keyEventRecord struct {
	KeyDown         int32
	RepeatCount     uint16
	VirtualKeyCode  uint16
	VirtualScanCode uint16
	UnicodeChar     uint16
	ControlKeyState uint32
}

var (
	modkernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procReadConsoleInputW   = modkernel32.NewProc("ReadConsoleInputW")
	procWaitForSingleObject = modkernel32.NewProc("WaitForSingleObject")
)

// enableWindowsVT turns on VT output and returns a func that restores the
// original console output mode.
func enableWindowsVT(stdout *os.File) func() {
	if stdout == nil {
		return func() {}
	}
	handle := windows.Handle(stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return func() {}
	}
	_ = windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	return func() { _ = windows.SetConsoleMode(handle, mode) }
}

// enterRaw clears processed input on the console so Ctrl+C arrives as a
// key. If the console mode can't be read, input stays as it was.
func enterRaw(fd int) (func(), error) {
	handle := windows.Handle(fd)
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return func() {}, nil
	}
	if err := windows.SetConsoleMode(handle, rawInputMode(mode)); err != nil {
		return func() {}, nil
	}
	return func() { _ = windows.SetConsoleMode(handle, mode) }, nil
}

var procGetNumberOfConsoleInputEvents = modkernel32.NewProc("GetNumberOfConsoleInputEvents")

type keyReader struct {
	handle windows.Handle
	keys   consoleKeys
}

func newKeyReader(stream *os.File) *keyReader {
	return &keyReader{handle: windows.Handle(stream.Fd())}
}

// Read waits up to timeout for console input, then drains every record
// that is ready so a paste arrives as one burst.
func (k *keyReader) Read(timeout time.Duration) []string {
	ms := uint32(timeout / time.Millisecond)
	if ms == 0 {
		ms = 1
	}
	if r1, _, _ := procWaitForSingleObject.Call(uintptr(k.handle), uintptr(ms)); r1 != 0 { // WAIT_OBJECT_0 == 0
		return nil
	}
	var out []string
	for {
		var rec inputRecord
		var n uint32
		ok, _, _ := procReadConsoleInputW.Call(uintptr(k.handle), uintptr(unsafe.Pointer(&rec)), 1, uintptr(unsafe.Pointer(&n)))
		if ok == 0 || n == 0 {
			break
		}
		if rec.EventType == keyEvent {
			key := (*keyEventRecord)(unsafe.Pointer(&rec.Event[0]))
			if tok := k.keys.token(consoleKey{Down: key.KeyDown != 0, VK: key.VirtualKeyCode, Char: key.UnicodeChar}); tok != "" {
				for i := uint16(0); i < max(1, key.RepeatCount); i++ {
					out = append(out, tok)
				}
			}
		}
		var more uint32
		if r, _, _ := procGetNumberOfConsoleInputEvents.Call(uintptr(k.handle), uintptr(unsafe.Pointer(&more))); r == 0 || more == 0 {
			break
		}
	}
	return coalescePaste(out)
}
