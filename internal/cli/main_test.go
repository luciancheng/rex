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
	os.Unsetenv("REX_EVENTS")        // a feed set in the shell is never written by a test
	os.Unsetenv("CLAUDE_CONFIG_DIR") // nor a Claude config
	os.Unsetenv("CODEX_HOME")        // nor a Codex home
	os.Unsetenv("XDG_CONFIG_HOME")   // nor OpenCode's config folder
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
