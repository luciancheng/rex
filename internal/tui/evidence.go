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
// it was pinned with p⏎, else when this session first saw it as the pin.
func (a *app) pinStart(key string) (time.Time, string) {
	if ts := plan.LoadState(a.statePath).Pins[key]; ts > 0 {
		t := time.Unix(int64(ts), 0)
		return t, "since " + t.Format("15:04")
	}
	if t, ok := a.pinSeen[key]; ok {
		return t, "seen since " + t.Format("15:04") + " (this session)"
	}
	return time.Time{}, "not seen on this machine"
}

var (
	trailerDid      = regexp.MustCompile(`(?im)^[\s>*_-]*accomplished\s*[:：]\s*(.+?)\s*$`)
	trailerVerified = regexp.MustCompile(`(?im)^[\s>*_-]*verified(?:_how)?\s*[:：]\s*(.+?)\s*$`)
	fingerprint     = regexp.MustCompile(`rex-sendoff:\s*(\S+)#(\S+)`)
	testCommand     = regexp.MustCompile(`(?i)\b(go test|go vet|npm (run )?test|pnpm test|yarn test|pytest|cargo test|make (test|check)|bun test|vitest|jest|rspec|mvn test|gradle test)\b`)
)

// gather reads the evidence for task id out of events, which are raw
// events from this repo, oldest first. verify is the task's own verify
// list: a command matching it is preferred as the "verified" suggestion.
func gather(events []watch.Event, scope watch.Scope, since time.Time, id string, verify []string) evidence {
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
			p := watch.EventPath(e)
			rel, in := scope.Rel(p)
			if !in {
				continue
			}
			if rel == "" || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "rex/plans/") || seenFile[rel] {
				continue
			}
			seenFile[rel] = true
			ev.files = append(ev.files, rel)
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
			if m := fingerprint.FindStringSubmatch(e.Text); m != nil && m[2] != id {
				continue // a reply about another task's send-off
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
		for _, v := range verify {
			if v = strings.ToLower(strings.TrimSpace(v)); v != "" && (strings.Contains(strings.ToLower(runs[i].cmd), v) || strings.Contains(v, strings.ToLower(runs[i].cmd))) {
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
		switch r.result {
		case "":
			ev.test = r.cmd + " (result not seen)"
		default:
			ev.test = r.cmd + " (" + r.result + ")"
		}
	}
	return ev
}
