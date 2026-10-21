//go:build windows

package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestAppendDoesNotHangOnHeldLock(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	holder, err := os.OpenFile(feed+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(holder.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	ok, err := AppendEvent(feed, NewEvent("cursor", "user", "still lands", nil))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("append waited %v behind a stuck lock", elapsed)
	}
}
