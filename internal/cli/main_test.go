package cli

import (
	"io"
	"os"
	"strings"
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
	os.Unsetenv("REX_EVENTS")         // a feed set in the shell is never written by a test
	os.Unsetenv("CLAUDE_CONFIG_DIR")  // nor a Claude config
	os.Unsetenv("CODEX_HOME")         // nor a Codex home
	os.Unsetenv("XDG_CONFIG_HOME")    // nor OpenCode's config folder
	os.Unsetenv("CURSOR_PROJECT_DIR") // nor a Cursor workspace a pin lookup would read
	var code int
	if os.Getenv("REX_TEST_CLOSED_STDOUT") != "" {
		// TestIngestClosedStdoutExitsZero's child: a hook printing a note
		// to a stdout whose reader is gone.
		ingestFn = func(io.Reader, string, string) ([]byte, error) { return []byte("note\n"), nil }
		code = Main([]string{"ingest", "--source", "claude"}, strings.NewReader("{}"), os.Stdout, os.Stderr)
		os.Stderr.WriteString("survived the write\n")
	} else {
		code = m.Run()
	}
	os.RemoveAll(home)
	os.Exit(code)
}
