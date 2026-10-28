package cli

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain points HOME at a throwaway folder so no test can touch the
// real ~/.cursor, ~/.claude, ~/.codex or ~/.rex.
func TestMain(m *testing.M) {
	if os.Getenv("REX_TEST_AS_REX") != "" {
		// TestOpenCodePluginGetsThePin's rex: the plugin, under node, runs
		// this binary as rex ingest, with that test's HOME and feed, and
		// all the time in the world to look the pin up.
		pinBy = func() time.Time { return time.Now().Add(time.Hour) }
		os.Exit(Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
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
		ingestFn = func(_ io.Reader, _, _ string, out io.Writer) error { _, err := out.Write([]byte("note\n")); return err }
		code = Main([]string{"ingest", "--source", "claude"}, strings.NewReader("{}"), os.Stdout, os.Stderr)
		os.Stderr.WriteString("survived the write\n")
	} else {
		code = m.Run()
	}
	os.RemoveAll(home)
	os.Exit(code)
}
