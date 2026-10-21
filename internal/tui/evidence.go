package tui

import (
	"regexp"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// evidence is what Rex saw while a task was the pin, for the completion
// form (docs/ux.md §8): completed edits in this repo, test commands and
// whether they passed, and the agent's reply trailer. It is only ever a
// hint; the human decides what is written.
type evidence struct {
	files    []string // repo-relative, first seen first, never rex/plans/**
	test     string   // a command worth calling "verified", with its result
	did      string   // the agent's "accomplished:" trailer
	verified string   // the agent's "verified:" trailer
}

const evidenceKeep = 2000 // raw events kept for evidence

// remember keeps a raw event from this repo for evidence.
func (a *app) remember(ev watch.Event) {
	a.seen = append(a.seen, ev)
	if len(a.seen) > evidenceKeep {
		a.seen = a.seen[len(a.seen)-evidenceKeep:]
	}
}

// pinStart is when the task became the pin: from ~/.rex/state.json when
// it was pinned with p⏎ (and no later pin of the plan took over), else
// when this session first saw it as the pin. If the pin moved to the task
// while Rex watched, after that start, the move is the start.
func (a *app) pinStart(key, planPath string) (time.Time, string) {
	var start time.Time
	label := ""
	st := plan.LoadState(a.statePath)
	if ts := st.Pins[key]; ts > 0 {
		stale := false
		for k, other := range st.Pins {
			if k != key && strings.HasPrefix(k, plan.PinKey(planPath, "")) && other > ts {
				stale = true // another task of this plan was pinned since
			}
		}
		if !stale {
			start, label = time.Unix(int64(ts), 0), "since "
		}
	}
	if t, ok := a.pinMoved[key]; ok && t.Sub(start) > time.Second {
		start, label = t, "seen since "
	}
	if start.IsZero() {
		t, ok := a.pinSeen[key]
		if !ok {
			return time.Time{}, "not seen on this machine"
		}
		start, label = t, "seen since "
	}
	when := start.Format("15:04")
	if y, m, d := start.Date(); y != a.now().Year() || m != a.now().Month() || d != a.now().Day() {
		when = start.Format("Jan 2 15:04")
	}
	if label == "seen since " {
		return start, label + when + " (this session)"
	}
	return start, label + when
}

var (
	// Labels may be bold or italic (**accomplished:**, _verified_:).
	trailerDid      = regexp.MustCompile(`(?im)^[\s>*_-]*accomplished[*_]*\s*[:：][*_\s]*(.+?)[*_\s]*$`)
	trailerVerified = regexp.MustCompile(`(?im)^[\s>*_-]*verified(?:_how)?[*_]*\s*[:：][*_\s]*(.+?)[*_\s]*$`)
	// The path may hold spaces; the id is a slug, so trailing punctuation
	// (a backtick, a full stop) isn't part of it.
	fingerprint = regexp.MustCompile(`rex-sendoff:\s*(.+?)#([A-Za-z0-9_.-]*[A-Za-z0-9_-])`)
	// A pasted brief carries the fingerprint as a line of its own; a
	// prompt that merely mentions one is not a send-off.
	fingerprintLine = regexp.MustCompile(`(?m)^rex-sendoff:[ \t]*(.+)#([A-Za-z0-9_.-]*[A-Za-z0-9_-])[ \t]*$`)
	testCommand     = regexp.MustCompile(`(?i)\b(go test|go vet|npm (run )?test|pnpm test|yarn test|pytest|cargo test|make (test|check)|bun test|vitest|jest|rspec|mvn test|gradle test)\b`)
)

// lastFingerprint is the reply's own send-off line: its last one, on a
// line of its own if any. A reply may quote the line of the task its PR
// sits on before ending with its own.
func lastFingerprint(text string) []string {
	for _, re := range []*regexp.Regexp{fingerprintLine, fingerprint} {
		if all := re.FindAllStringSubmatch(text, -1); len(all) > 0 {
			return all[len(all)-1]
		}
	}
	return nil
}

// gather reads the evidence for task id out of events, which are raw
// events from this repo, oldest first. verify is the task's own verify
// list: a command matching it is preferred as the "verified" suggestion.
func gather(events []watch.Event, scope watch.Scope, since time.Time, rel, id string, verify []string) evidence {
	var ev evidence
	if since.IsZero() {
		return ev
	}
	cutoff := float64(since.UnixNano()) / 1e9
	seenFile := map[string]bool{}
	type run struct {
		cmd    string
		result string // "passed", "failed", or "" when no result was seen
	}
	var runs []run
	find := func(cmd string) int {
		for i := len(runs) - 1; i >= 0; i-- {
			if runs[i].cmd == cmd {
				return i
			}
		}
		return -1
	}
	for _, e := range events {
		if e.TS < cutoff {
			continue
		}
		tool := watch.MetaTool(e)
		ok, hasOK := e.Meta["ok"].(bool)
		switch {
		case e.Kind == "result" && watch.IsFileEditEvent(e) && (!hasOK || ok):
			for _, p := range watch.EventPaths(e) {
				rel, in := scope.Rel(p)
				if !in || rel == "" || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "rex/plans/") || seenFile[rel] {
					continue
				}
				seenFile[rel] = true
				ev.files = append(ev.files, rel)
			}
		case e.Kind == "command" && watch.IsShellTool(tool):
			if cmd := strings.TrimSpace(e.Text); cmd != "" {
				runs = append(runs, run{cmd: cmd})
			}
		case (e.Kind == "result" || e.Kind == "error") && hasOK:
			cmd, _ := e.Meta["command"].(string)
			if cmd = strings.TrimSpace(cmd); cmd == "" {
				continue
			}
			result := "passed"
			if !ok {
				result = "failed"
			}
			if i := find(cmd); i >= 0 {
				runs[i].result = result
			} else {
				runs = append(runs, run{cmd: cmd, result: result})
			}
		case e.Kind == "text":
			if m := lastFingerprint(e.Text); m != nil && (m[2] != id || (rel != "" && strings.TrimSpace(m[1]) != rel)) {
				continue // a reply about another task's (or plan's) send-off
			}
			if m := trailerDid.FindStringSubmatch(e.Text); m != nil {
				ev.did = strings.TrimSpace(m[1])
			}
			if m := trailerVerified.FindStringSubmatch(e.Text); m != nil {
				ev.verified = strings.TrimSpace(m[1])
			}
		}
	}
	pick := -1
	for i := len(runs) - 1; i >= 0 && pick < 0; i-- {
		cmd := strings.ToLower(runs[i].cmd)
		for _, v := range verify {
			// The run contains the verify line, or a verify sentence names a
			// real test command; "ls" inside "details" is no match.
			if v = strings.ToLower(strings.TrimSpace(v)); v != "" && (strings.Contains(cmd, v) || (testCommand.MatchString(cmd) && strings.Contains(v, cmd))) {
				pick = i
				break
			}
		}
	}
	for i := len(runs) - 1; i >= 0 && pick < 0; i-- {
		if testCommand.MatchString(runs[i].cmd) {
			pick = i
		}
	}
	if pick >= 0 {
		r := runs[pick]
		cmd := scope.Relativize(r.cmd) // no machine paths in a committed plan
		switch {
		case r.result == "" || strings.ContainsAny(r.cmd, "|;"):
			// A pipe or a ; means the exit status was another command's.
			ev.test = cmd + " (result not seen)"
		default:
			ev.test = cmd + " (" + r.result + ")"
		}
	}
	return ev
}
