//go:build unix

package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestAppendDoesNotHangOnHeldLock(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	holder, err := os.OpenFile(feed+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := unix.Flock(int(holder.Fd()), unix.LOCK_EX); err != nil {
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
	if got := LastEvents(feed, 1); len(got) != 1 || got[0].Text != "still lands" {
		t.Fatalf("%#v", got)
	}
}
