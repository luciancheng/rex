package plan

import (
	"os"
	"testing"
)

// TestMain points HOME at a throwaway folder so no test can touch the
// real ~/.rex/state.json.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "rex-plan-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
