//go:build windows

package watch

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// withFeedLock serializes appends across concurrent hook processes. It never
// waits long: a hook that blocks here blocks the agent, so after lockWait it
// appends without the lock.
func withFeedLock(path string, fn func() error) error {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fn()
	}
	defer f.Close()
	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	deadline := time.Now().Add(lockWait)
	for {
		err := windows.LockFileEx(h, flags, 0, 1, 0, ol)
		if err == nil {
			break
		}
		// Only a held lock is worth waiting for.
		if err != windows.ERROR_LOCK_VIOLATION || time.Now().After(deadline) {
			return fn()
		}
		time.Sleep(lockPoll)
	}
	defer windows.UnlockFileEx(h, 0, 1, 0, ol)
	return fn()
}
