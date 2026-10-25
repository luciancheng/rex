package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/luciancheng/rex/internal/boot"
	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/plan"
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

When run from a hook, ingest always exits 0, so it cannot block the
agent, and problems go to stderr. On Claude Code's, Codex's and Cursor's
prompt hooks, and when a session starts (Claude Code's and Codex's after
a compaction, Cursor's when a chat starts), it prints one thing: a short
note naming the task pinned in this repo's Rex plan (with nothing
pinned, a new plan that has no tasks yet), which the agent adds to its
context; else nothing. Otherwise only -h/--help, --version, --install,
--uninstall, --install-cursor-hook and the skill flags print to stdout.

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
// so a bad flag or source in a hook must fail open and stay silent; only
// the pin note is ever printed from a hook (docs/ux.md §7a).
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
		repo, inRepo := plan.FindRepo("")
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
		repo, _ := plan.FindRepo("")
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
	ingestSafely(stdin, *source, *events, stdout, stderr)
	return 0
}

// pinBudget is how long after the process starts a prompt hook may still
// hand the agent the pin (docs/ux.md §7a): Codex runs hooks through
// $SHELL -lc, and that startup is already spent from its 5 s.
const pinBudget = 750 * time.Millisecond

var (
	// pinBy is when the pin lookup gives up; tests move it.
	pinBy = func() time.Time { return boot.At.Add(pinBudget) }
	// pinFn finds the pin; tests swap it.
	pinFn = plan.PinFor
	// ingestFn is ingest; tests swap it.
	ingestFn = ingest
)

// ingestSafely runs one hook payload and prints what it returns in one
// write, so nothing can leave half a reply. A hook's exit code is read by
// the agent (2 means "block"), and a Go panic exits 2, so even a crash
// here is reported on stderr and swallowed, and then nothing is printed.
// A harness that has closed its end of stdout would kill the process with
// SIGPIPE (exit 141) on the note's write, so that signal is ignored: the
// write fails, the note is lost, and the exit is still 0.
func ingestSafely(stdin io.Reader, source, events string, stdout, stderr io.Writer) {
	signal.Ignore(syscall.SIGPIPE)
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "rex: ingest crashed: %v\n", r)
		}
	}()
	reply, err := ingestFn(stdin, source, events)
	if err != nil {
		fmt.Fprintf(stderr, "rex: %v\n", err)
	}
	if len(reply) > 0 {
		_, _ = stdout.Write(reply)
	}
}

// ingest records a hook payload in the feed and returns what the hook
// prints: the pin note, on a prompt hook that hands it to the agent
// (watch.ContextEvent), found by the deadline; else nothing. The prompt's
// event says what it carried (meta pin and pin_plan) only when the note
// is printed.
func ingest(stdin io.Reader, source, events string) ([]byte, error) {
	payloads := watch.PayloadsFromRaw(string(watch.ReadPayload(stdin)))
	var reply []byte
	var meta map[string]any
	var crash error
	at := -1
	for i, p := range payloads {
		event := watch.ContextEvent(p, source)
		if event == "" {
			continue
		}
		pin, note, err := lookPin(watch.ContextFolders(p, source), watch.IsSessionStart(event), pinBy())
		crash = err
		if reply = watch.ContextReply(source, event, note); reply != nil {
			at, meta = i, map[string]any{"pin_plan": pin.Rel}
			if pin.Task != nil {
				meta["pin"] = pin.Task.ID
			}
		}
		break // one note a run
	}
	err := watch.Record(payloads, source, events, func(i int) map[string]any {
		if i == at {
			return meta
		}
		return nil
	})
	return reply, errors.Join(crash, err)
}

// lookPin finds the pin for an agent working in dirs (best first) and
// words its note, giving up at by: a slow disk costs the agent the note,
// never its prompt. A crash in the lookup is an error and no note.
func lookPin(dirs []string, session bool, by time.Time) (*plan.Pinned, string, error) {
	wait := time.Until(by)
	if wait <= 0 {
		return nil, "", nil
	}
	type found struct {
		pin  *plan.Pinned
		note string
		err  error
	}
	got, find := make(chan found, 1), pinFn
	go func() {
		defer func() {
			if r := recover(); r != nil {
				got <- found{err: fmt.Errorf("pin lookup crashed: %v", r)}
			}
		}()
		pin := find(plan.PinFolder(dirs), plan.StatePath())
		if pin == nil {
			got <- found{}
			return
		}
		got <- found{pin: pin, note: plan.PinNote(pin, session)}
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case f := <-got:
		return f.pin, f.note, f.err
	case <-timer.C:
		return nil, "", nil
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
