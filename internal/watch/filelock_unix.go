//go:build unix

package watch

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
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
	deadline := time.Now().Add(lockWait)
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		// Only a held lock is worth waiting for; a filesystem without
		// flock fails the same way every time.
		if (err != unix.EWOULDBLOCK && err != unix.EINTR) || time.Now().After(deadline) {
			return fn()
		}
		time.Sleep(lockPoll)
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return fn()
}
