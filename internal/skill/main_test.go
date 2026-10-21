package skill

import (
	"os"
	"testing"
)

// TestMain points HOME at a throwaway folder so no test can touch the
// real ~/.claude/skills.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "rex-skill-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
