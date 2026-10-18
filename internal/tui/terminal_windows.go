//go:build windows

package tui

import (
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

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

func enableWindowsVT(stdout *os.File) {
	if stdout == nil {
		return
	}
	handle := windows.Handle(stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return
	}
	_ = windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}

func enterRaw(_ int) (func(), error) {
	return func() {}, nil
}

func ReadKey(stream *os.File, timeout time.Duration) (string, bool) {
	handle := windows.Handle(stream.Fd())
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining < 0 {
			remaining = 0
		}
		ms := uint32(remaining / time.Millisecond)
		if ms == 0 {
			ms = 1
		}
		r1, _, _ := procWaitForSingleObject.Call(uintptr(handle), uintptr(ms))
		if r1 != 0 { // WAIT_OBJECT_0 == 0
			if time.Now().After(deadline) {
				return "", false
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		var rec inputRecord
		var n uint32
		ok, _, _ := procReadConsoleInputW.Call(
			uintptr(handle),
			uintptr(unsafe.Pointer(&rec)),
			1,
			uintptr(unsafe.Pointer(&n)),
		)
		if ok == 0 || n == 0 {
			continue
		}
		if rec.EventType != keyEvent {
			continue
		}
		key := *(*keyEventRecord)(unsafe.Pointer(&rec.Event[0]))
		if key.KeyDown == 0 {
			continue
		}
		if name := virtualKeyName(key.VirtualKeyCode); name != "" && key.UnicodeChar == 0 {
			return name, true
		}
		if key.UnicodeChar == 0 {
			continue
		}
		r := rune(key.UnicodeChar)
		if r == 1 {
			return "home", true
		}
		if r == 5 {
			return "end", true
		}
		return string(r), true
	}
	return "", false
}

func virtualKeyName(vk uint16) string {
	switch vk {
	case 0x26:
		return "up"
	case 0x28:
		return "down"
	case 0x25:
		return "left"
	case 0x27:
		return "right"
	case 0x21:
		return "pageup"
	case 0x22:
		return "pagedown"
	case 0x24:
		return "home"
	case 0x23:
		return "end"
	default:
		return ""
	}
}
