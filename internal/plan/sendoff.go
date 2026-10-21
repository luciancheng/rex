package plan

import (
	"errors"
	"fmt"
	"strings"

	"github.com/luciancheng/rex/internal/watch"
)

// ErrEmpty means a plan has no tasks to send off.
var ErrEmpty = errors.New("nothing to send · this plan has no tasks · b⏎ copies a planning brief")

const (
	sendoffCap = 4096 // a brief never exceeds this; a typical plan's is ~2 KB
	fieldCap   = 240  // one field, before "(see plan file)"
)

// Fingerprint is the last line of a send-off. When the brief comes back
// as a prompt, the tape recognizes it by this line.
func Fingerprint(rel, task string) string { return "rex-sendoff: " + rel + "#" + task }

// Sendoff is the handoff for the next chat or person, generated only from
// the plan (issue #9's template, plus a short Done section, the reply
// trailer request and the fingerprint). rel is the plan's repo-relative
// path. Every field is cleaned of terminal control characters.
func Sendoff(p *Plan, rel string) (string, error) {
	if p == nil || len(p.Tasks) == 0 {
		return "", ErrEmpty
	}
	// Fullest first. Finished tasks leave the task list before their Done
	// entries go: what the last agent actually did matters more.
	var s string
	for _, limit := range []int{-1, 6, 3, 0} {
		for _, short := range []bool{false, true} {
			if s = sendoff(p, rel, limit, short); len(s) <= sendoffCap {
				return s, nil
			}
		}
	}
	return keepTail(s, sendoffCap), nil
}

// keepTail cuts s to n bytes but keeps the closing instructions and the
// fingerprint, which the tape and the next chat rely on.
func keepTail(s string, n int) string {
	i := strings.LastIndex(s, "\nFor the next chat\n")
	if i < 0 || len(s)-i > n/2 {
		return capBytes(s, n)
	}
	tail := s[i:]
	return capBytes(s[:i], n-len(tail)-len("\n… (see plan file)")) + "\n… (see plan file)" + tail
}

// sendoff writes the brief. done limits the Done section (-1 = all), and
// short lists only the tasks near the pin.
func sendoff(p *Plan, rel string, done int, short bool) string {
	g := p.Graph()
	pin := g.Pin()
	pinID := "none"
	if pin != nil {
		pinID = pin.ID
	}
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	title := clean(p.Title)
	if title == "" {
		title = clean(p.ID)
	}
	line("Send-off: %s", title)
	line("File: %s", clean(rel))
	line("Status: %s · pinned task: %s", p.Status, clean(pinID))
	if goal := clean(p.Goal); goal != "" {
		line("")
		line("Goal")
		line("%s", clip(goal, 2*fieldCap))
	}
	if len(p.Scope.In)+len(p.Scope.Out) > 0 {
		line("")
		line("Scope")
		if len(p.Scope.In) > 0 {
			line("- In: %s", list(p.Scope.In, ", "))
		}
		if len(p.Scope.Out) > 0 {
			line("- Out: %s", list(p.Scope.Out, ", "))
		}
	}
	line("")
	line("Tasks")
	shown := 0
	for i := range p.Tasks {
		t := &p.Tasks[i]
		if short && !near(g, t, pin) {
			continue
		}
		shown++
		deps := ""
		if len(t.DependsOn) > 0 {
			deps = " (depends on: " + list(t.DependsOn, ", ") + ")"
		}
		indent := strings.Repeat("  ", min(g.Depth(t), 2))
		name := clean(taskName(t))
		line("%s- [%s] %s%s", indent, t.Status, name, deps)
	}
	if hidden := len(p.Tasks) - shown; hidden > 0 {
		line("- … %d more in the plan file", hidden)
	}
	line("")
	line("Next")
	switch {
	case pin != nil:
		line("### %s", clean(pin.ID))
		if t := clean(pin.Title); t != "" && t != clean(pin.ID) {
			line("%s", clip(t, fieldCap))
		}
		field := func(label string, items []string, sep string) {
			if len(items) > 0 {
				line("%s: %s", label, list(items, sep))
			}
		}
		field("Does", pin.Does, "; ")
		field("Needs", pin.Needs, "; ")
		field("Verify", pin.Verify, "; ")
		field("Files", pin.Files, ", ")
		field("In", pin.Scope.In, ", ")
		field("Out", pin.Scope.Out, ", ")
		if d := clean(pin.Description); d != "" {
			line("Notes: %s", clip(d, fieldCap))
		}
	default:
		if finished, total := g.Progress(); finished == total {
			line("All tasks are done.")
		} else {
			line("Nothing is unblocked: every open task waits on another or is blocked.")
		}
	}
	var finished []*Task
	for i := range p.Tasks {
		if t := &p.Tasks[i]; t.Status == Done && len(t.Accomplished)+len(t.Modified)+len(t.VerifiedHow) > 0 {
			finished = append(finished, t)
		}
	}
	if done >= 0 && len(finished) > done {
		finished = finished[len(finished)-done:]
	}
	if len(finished) > 0 {
		line("")
		line("Done")
		for _, t := range finished {
			line("- %s", clean(t.ID))
			if len(t.Does) > 0 {
				line("  intended: %s", list(t.Does, "; "))
			}
			if len(t.Accomplished) > 0 {
				line("  actual: %s", list(t.Accomplished, "; "))
			}
			if len(t.Modified) > 0 {
				line("  modified: %s", list(t.Modified, ", "))
			}
			if len(t.VerifiedHow) > 0 {
				line("  verified: %s", list(t.VerifiedHow, "; "))
			}
		}
	}
	line("")
	line("For the next chat")
	line("Work only on the pinned task. Stay in scope. Open the plan file in Rex.")
	line("Do not inject tools or expand into scope.out. If the plan is wrong, edit the JSON.")
	line("When you finish, end your final reply with two lines:")
	line("accomplished: <what you actually did>")
	line("verified: <how you checked it>")
	line("")
	b.WriteString(Fingerprint(clean(rel), clean(pinID)))
	b.WriteString("\n")
	return b.String()
}

// near is a task worth listing in a short brief: the pin, what it waits
// on, what waits on it, and every unfinished leaf.
func near(g *Graph, t, pin *Task) bool {
	if pin == nil || t == pin || !Finished(t.Status) {
		return true
	}
	for _, d := range pin.DependsOn {
		if d == t.ID {
			return true
		}
	}
	return false
}

func taskName(t *Task) string {
	if t.ID != "" {
		return t.ID
	}
	return fmt.Sprintf("tasks[%d] %s", t.Index, t.Title)
}

// Brief asks an agent to fill or refine the plan at rel (b⏎). It carries
// the v1 shape and the rules, never anything from the tape.
func Brief(p *Plan, rel string) string {
	rel = clean(rel)
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("Planning brief: %s", rel)
	line("")
	line("Fill or refine the Rex plan at %s. It is JSON, v1:", rel)
	line(`{"version": 1, "id": "slug", "title": "", "goal": "one paragraph", "status": "pending",`)
	line(`  "scope": {"in": ["..."], "out": ["..."]},`)
	line(`  "tasks": [{"id": "short-slug", "title": "", "parent": "", "status": "pending",`)
	line(`    "needs": ["..."], "does": ["..."], "depends_on": ["..."], "verify": ["..."], "files": ["..."]}]}`)
	line("")
	line("Rules")
	line("- ids are short, stable slugs; never renumber or reuse them")
	line("- file order is do order; keep the tasks array flat")
	line("- parent groups tasks under a heading; depends_on lists hard prerequisites only")
	line("- one task should fit one PR a human can review in one sitting")
	line("- needs = what must already be true; does = what will be true when it is done")
	line("- verify = how a stranger would check it; files = likely paths, not a lock")
	line("- scope.out is the hard edge of the job: list what this work must not touch")
	line("- leave accomplished, modified and verified_how empty; they are written when a task is done")
	line("- never put prompts, tool permissions, diffs or secrets in the plan")
	line("")
	if p != nil {
		goal := clean(p.Goal)
		if goal == "" {
			goal = "(none yet)"
		}
		line("Goal now: %s", clip(goal, 2*fieldCap))
		ids := make([]string, 0, len(p.Tasks))
		for i := range p.Tasks {
			ids = append(ids, taskName(&p.Tasks[i]))
		}
		if len(ids) == 0 {
			line("Tasks now: none")
		} else {
			line("Tasks now: %d (%s)", len(ids), clip(strings.Join(ids, ", "), 2*fieldCap))
		}
		line("")
	}
	line("fill or refine %s", rel)
	return capBytes(b.String(), sendoffCap)
}

func clean(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(watch.CleanText(s), "\n", " "))
}

func list(items []string, sep string) string {
	var out []string
	for _, it := range items {
		if c := clean(it); c != "" {
			out = append(out, c)
		}
	}
	return clip(strings.Join(out, sep), fieldCap)
}

// clip cuts s to about n bytes on a rune boundary and says where the
// rest is.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xc0) == 0x80 {
		cut--
	}
	return strings.TrimSpace(s[:cut]) + "… (see plan file)"
}

func capBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xc0) == 0x80 {
		cut--
	}
	return s[:cut]
}
