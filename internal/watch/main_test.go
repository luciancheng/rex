package watch

import (
	"os"
	"testing"
)

// TestMain points HOME at a throwaway folder so no test can touch the
// real ~/.cursor, ~/.claude, ~/.codex or ~/.rex.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "rex-watch-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
