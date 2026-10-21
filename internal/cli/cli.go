package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/tui"
	"github.com/luciancheng/rex/internal/version"
	"github.com/luciancheng/rex/internal/watch"
	"golang.org/x/term"
)

const rootHelp = `usage: rex [-h] [--version] {start,ingest} ...

REX — a terminal companion with a dinosaur mascot.

positional arguments:
  {start,ingest}

options:
  -h, --help     show this help message and exit
  --version      show program's version number and exit

examples:
  rex start
  rex start --static
  rex ingest --install-cursor-hook
`

const startHelp = `usage: rex start [-h] [--no-color] [--static] [--events EVENTS]

open the companion UI

options:
  -h, --help       show this help message and exit
  --no-color       disable ANSI color
  --static         print one frame and exit
  --events EVENTS  JSONL event feed (default: ~/.rex/session.jsonl)
`

const ingestHelp = `usage: rex ingest [-h] [--source {auto,cursor,claude,codex}] [--install-cursor-hook] [--events EVENTS]

append an agent event to the watch feed

When run from a hook, ingest always exits 0 and writes nothing to stdout,
so it cannot block or steer the agent; problems go to stderr. Only
-h/--help, --version and --install-cursor-hook print to stdout.

options:
  -h, --help              show this help message and exit
  --source {auto,cursor,claude,codex}
                          payload dialect (default: auto)
  --install-cursor-hook   install ~/.cursor/hooks.json so Cursor writes the feed
  --events EVENTS         JSONL event feed (default: ~/.rex/session.jsonl)
`

func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasFlag(args, "--version") {
		fmt.Fprintf(stdout, "rex %s\n", version.Version)
		return 0
	}
	if len(args) == 0 {
		_, _ = io.WriteString(stdout, rootHelp)
		return 0
	}
	cmd := args[0]
	rest := args[1:]
	if cmd == "-h" || cmd == "--help" {
		_, _ = io.WriteString(stdout, rootHelp)
		return 0
	}
	switch cmd {
	case "start":
		if hasFlag(rest, "-h") || hasFlag(rest, "--help") {
			_, _ = io.WriteString(stdout, startHelp)
			return 0
		}
		return runStart(rest, stdin, stdout, stderr)
	case "ingest":
		if hasFlag(rest, "-h") || hasFlag(rest, "--help") {
			_, _ = io.WriteString(stdout, ingestHelp)
			return 0
		}
		return runIngest(rest, stdin, stdout, stderr)
	default:
		// Not 2: a hook pointing at a subcommand this binary doesn't have
		// must not read as "block" to the agent.
		fmt.Fprintf(stderr, "rex: unknown command '%s'\n", cmd)
		return 1
	}
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}

func runStart(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noColor := fs.Bool("no-color", false, "disable ANSI color")
	static := fs.Bool("static", false, "print one frame and exit")
	events := fs.String("events", "", "JSONL event feed")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	enabled := mascot.ColorEnabledWriter(isTTY(stdout))
	if *noColor {
		enabled = false
	}
	if *static || !interactive(stdin, stdout) {
		if err := tui.PrintCard("idle", mascot.DefaultPalette, enabled, stdout); err != nil {
			fmt.Fprintf(stderr, "rex: %v\n", err)
			return 1
		}
		return 0
	}
	inFile, okIn := stdin.(*os.File)
	outFile, okOut := stdout.(*os.File)
	if !okIn || !okOut {
		if err := tui.PrintCard("idle", mascot.DefaultPalette, enabled, stdout); err != nil {
			fmt.Fprintf(stderr, "rex: %v\n", err)
			return 1
		}
		return 0
	}
	code, err := tui.RunLoop(mascot.DefaultPalette, enabled, "idle", inFile, outFile, *events)
	if err != nil {
		if isBrokenPipe(err) {
			return 0
		}
		tui.RestoreOnInterrupt(stdout)
		fmt.Fprintf(stderr, "rex: %v\n", err)
		return 1
	}
	return code
}

// runIngest exits 0 for anything that is not an install. Agent harnesses
// read exit code 2 as "block" and some add stdout to the agent's context,
// so a bad flag or source in a hook must fail open and stay silent.
func runIngest(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	source := fs.String("source", "auto", "payload dialect")
	install := fs.Bool("install-cursor-hook", false, "install ~/.cursor/hooks.json")
	events := fs.String("events", "", "JSONL event feed")
	if err := fs.Parse(args); err != nil {
		return 0
	}
	if *install {
		if err := watch.InstallCursorHook(stdout); err != nil {
			fmt.Fprintf(stderr, "rex: %v\n", err)
			return 1
		}
		return 0
	}
	switch *source {
	case "auto", "cursor", "claude", "codex":
	default:
		fmt.Fprintf(stderr, "rex: invalid source '%s', reading as auto\n", *source)
		*source = "auto"
	}
	ingestSafely(stdin, *source, *events, stderr)
	return 0
}

// ingestFn is watch.RunIngest; tests swap it.
var ingestFn = watch.RunIngest

// ingestSafely runs one hook payload. A hook's exit code is read by the
// agent (2 means "block"), and a Go panic exits 2, so even a crash here is
// reported on stderr and swallowed.
func ingestSafely(stdin io.Reader, source, events string, stderr io.Writer) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "rex: ingest crashed: %v\n", r)
		}
	}()
	if err := ingestFn(stdin, source, events); err != nil {
		fmt.Fprintf(stderr, "rex: %v\n", err)
	}
}

func interactive(stdin io.Reader, stdout io.Writer) bool {
	return isTTY(stdout) && isTTY(stdin)
}

func isTTY(w any) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "broken pipe")
}
