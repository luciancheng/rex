//go:build unix

package watch

import (
	"os"

	"golang.org/x/sys/unix"
)

func withFeedLock(path string, fn func() error) error {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fn()
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fn()
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return fn()
}
