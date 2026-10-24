package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/skill"
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
  rex ingest --install cursor
`

const startHelp = `usage: rex start [-h] [--no-color] [--static] [--events EVENTS]

open the companion UI

options:
  -h, --help       show this help message and exit
  --no-color       disable ANSI color
  --static         print one frame and exit
  --events EVENTS  JSONL event feed (default: ~/.rex/session.jsonl)
`

const ingestHelp = `usage: rex ingest [-h] [--source {auto,cursor,claude,codex,opencode}] [--events EVENTS]
       rex ingest --install HARNESS | --uninstall HARNESS [--project]
       rex ingest --install-skill [--project] | --uninstall-skill [--project]

append an agent event to the watch feed

When run from a hook, ingest always exits 0 and writes nothing to stdout,
so it cannot block or steer the agent; problems go to stderr. Only
-h/--help, --version, --install, --uninstall, --install-cursor-hook and
the skill flags print to stdout.

options:
  -h, --help              show this help message and exit
  --source {auto,cursor,claude,codex,opencode}
                          payload dialect (default: auto)
  --install HARNESS       wire an agent so it writes the feed: cursor, claude,
                          codex, opencode, or auto for every one found here
  --project               with claude: wire only this git repo
                          (.claude/settings.local.json, kept out of git);
                          with the skill: install it in this repo
                          (.claude/skills and .agents/skills)
  --install-skill         install the rex-plan skill, which teaches every
                          agent the plan format and how to work a plan:
                          ~/.claude/skills/rex-plan for Claude Code and
                          ~/.agents/skills/rex-plan for Codex (Cursor and
                          OpenCode read both, and may list it twice); with
                          $CLAUDE_CONFIG_DIR, Claude Code's copy goes there,
                          and Rex's copy in ~/.claude/skills is updated too
  --uninstall-skill       remove the rex-plan skill copies Rex installed
  --uninstall HARNESS     take Rex's hook back out of that agent's config
  --install-cursor-hook   same as --install cursor
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
	if *noColor || os.Getenv("TERM") == "dumb" {
		enabled = false // a dumb terminal can't take color either
	}
	mascot.ColorDepth = mascot.DetectDepth(os.Getenv)
	if staticStart(*static, interactive(stdin, stdout), os.Getenv("TERM")) {
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
	installCursor := fs.Bool("install-cursor-hook", false, "install ~/.cursor/hooks.json")
	install := fs.String("install", "", "wire an agent's hooks")
	uninstall := fs.String("uninstall", "", "remove Rex's hooks from an agent")
	project := fs.Bool("project", false, "claude or the skill: only this repo")
	installSkill := fs.Bool("install-skill", false, "install the rex-plan agent skill")
	uninstallSkill := fs.Bool("uninstall-skill", false, "remove the rex-plan agent skill")
	events := fs.String("events", "", "JSONL event feed")
	if err := fs.Parse(args); err != nil {
		for _, a := range args {
			if strings.HasPrefix(strings.TrimLeft(a, "-"), "install") || strings.HasPrefix(strings.TrimLeft(a, "-"), "uninstall") {
				return 1 // a person installing, not a hook: say it failed
			}
		}
		return 0
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if (set["install"] || *installCursor) && set["uninstall"] {
		fmt.Fprintln(stderr, "rex: use --install or --uninstall, not both")
		return 1
	}
	for _, name := range []string{"install", "uninstall"} {
		if set[name] && fs.Lookup(name).Value.String() == "" {
			fmt.Fprintf(stderr, "rex: --%s needs a harness name\n", name)
			return 1
		}
	}
	if *installCursor && *install == "" {
		*install = "cursor"
	}
	// Installing is interactive, so it may fail loudly; ingest never does.
	if *installSkill || *uninstallSkill {
		// One thing at a time, so nothing asked for is silently skipped.
		if (*installSkill && *uninstallSkill) || set["install"] || set["uninstall"] || *installCursor {
			fmt.Fprintln(stderr, "rex: --install-skill and --uninstall-skill go on their own; nothing changed")
			return 1
		}
		repo, inRepo := tui.FindRepo("")
		if *project && !inRepo {
			fmt.Fprintf(stderr, "rex: --project needs a git repo, and %s isn't one; run it from inside the repo\n", repo)
			return 1
		}
		home, herr := os.UserHomeDir()
		failed, claude := false, false
		for i, dir := range skill.Dirs(home, repo, *project) {
			// One copy failing never stops the other: report it, go on,
			// and exit 1 at the end.
			var err error
			switch {
			case dir == "":
				err = fmt.Errorf("no home folder for ~/%s/skills: %v", []string{".claude", ".agents"}[i], herr)
			case *installSkill:
				err = skill.Install(dir, stdout)
			default:
				err = skill.Uninstall(dir, stdout)
			}
			if err != nil {
				fmt.Fprintf(stderr, "rex: %v\n", err)
				failed = true
			} else if i == 0 {
				claude = true
			}
		}
		// Rex's copy in ~/.claude/skills, left from before
		// $CLAUDE_CONFIG_DIR, is still read by Cursor and OpenCode: it is
		// updated or removed with the others. Anyone else's stays.
		if dir := skill.Legacy(home); dir != "" && !*project && skill.Ours(dir) {
			var err error
			if *installSkill {
				err = skill.Install(dir, stdout)
			} else {
				err = skill.Uninstall(dir, stdout)
			}
			if err != nil {
				fmt.Fprintf(stderr, "rex: %v\n", err)
				failed = true
			}
		}
		// The note points at Claude Code's copy, so only when it is there.
		if claude && *installSkill && !*project && os.Getenv("CLAUDE_CONFIG_DIR") != "" {
			fmt.Fprintln(stdout, "Claude Code's copy is in $CLAUDE_CONFIG_DIR, which only Claude Code reads; Codex, Cursor and OpenCode read ~/.agents/skills")
		}
		if failed {
			return 1
		}
		return 0
	}
	if *project && *install != "claude" && *uninstall != "claude" {
		// Only Claude Code has a per-repo hook file; never fall through to
		// the machine-wide config the user just asked to avoid.
		fmt.Fprintln(stderr, "rex: --project works with claude or the skill only; nothing changed")
		return 1
	}
	if *project {
		repo := tui.FindRepoRoot("")
		var err error
		if *install != "" {
			err = watch.InstallClaudeProject(repo, stdout)
		} else {
			err = watch.UninstallClaudeProject(repo, stdout)
		}
		if err != nil {
			fmt.Fprintf(stderr, "rex: %v\n", err)
			return 1
		}
		return 0
	}
	if *install != "" {
		if err := watch.InstallHarness(*install, stdout); err != nil {
			fmt.Fprintf(stderr, "rex: %v\n", err)
			return 1
		}
		return 0
	}
	if *uninstall != "" {
		if err := watch.UninstallHarness(*uninstall, stdout); err != nil {
			fmt.Fprintf(stderr, "rex: %v\n", err)
			return 1
		}
		return 0
	}
	switch *source {
	case "auto", "cursor", "claude", "codex", "opencode":
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

// staticStart reports whether start prints one card instead of the live
// screen: asked for, not a terminal, or a terminal that can't move the
// cursor.
func staticStart(static, interactive bool, term string) bool {
	return static || !interactive || term == "dumb"
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
