package cli

import (
	"os"
	"testing"
)

// TestMain points HOME at a throwaway folder so no test can touch the
// real ~/.cursor, ~/.claude, ~/.codex or ~/.rex.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "rex-cli-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	os.Unsetenv("REX_EVENTS") // a feed set in the shell is never written by a test
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
