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
	idCap      = 80   // a task id or name in a list
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
	// entries go (what the last agent actually did matters more), then the
	// list of open tasks is shortened; the pin's Next section stays when it fits,
	// and the closing instructions and fingerprint always do.
	var s string
	for _, limit := range []int{-1, 6, 3, 0} {
		for _, open := range []int{-1, -2, 12, 4} {
			if s = sendoff(p, rel, limit, open); len(s) <= sendoffCap {
				return s, nil
			}
		}
	}
	return keepTail(s, sendoffCap), nil
}

// keepTail cuts s to n bytes. The closing instructions and the
// fingerprint always stay, since the tape and the completion form rely on
// them; the pin's Next section stays when it fits in most of what is left,
// and the task list is what gives way.
func keepTail(s string, n int) string {
	const cut = "\n… (see plan file)"
	var foot string
	if j := strings.LastIndex(s, "\nFor the next chat\n"); j >= 0 {
		s, foot = s[:j], s[j:]
	}
	if len(foot)+len(cut)+64 > n { // a huge pin id or path: no room to keep it whole
		return capBytes(s+foot, n)
	}
	n -= len(foot)
	if len(s) <= n {
		return s + foot
	}
	if i := strings.LastIndex(s, "\nNext\n"); i >= 0 && len(s)-i <= n*3/4 {
		return capBytes(s[:i], n-(len(s)-i)-len(cut)) + cut + s[i:] + foot
	}
	return capBytes(s, n-len(cut)) + cut + foot
}

// sendoff writes the brief. done limits the Done section (-1 = all).
// open picks the task list: -1 every task, -2 the tasks near the pin
// (it, what it waits on, every unfinished task), n ≥ 0 the pin and what
// it waits on, plus the first n other open tasks.
func sendoff(p *Plan, rel string, done, open int) string {
	g := p.Graph()
	pin := g.Pin()
	pinID := "none"
	if pin != nil {
		pinID = pin.ID
	}
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	title := clip(clean(p.Title), fieldCap)
	if title == "" {
		title = clip(clean(p.ID), fieldCap)
	}
	line("Send-off: %s", title)
	line("File: %s", clean(rel))
	line("Status: %s · pinned task: %s", p.Status, clip(clean(pinID), idCap))
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
	shown, others := 0, 0
	for i := range p.Tasks {
		t := &p.Tasks[i]
		switch {
		case open == -2 && !near(t, pin):
			continue
		case open >= 0 && !core(t, pin):
			if Finished(t.Status) || others >= open {
				continue
			}
			others++
		}
		shown++
		deps := ""
		if len(t.DependsOn) > 0 {
			deps = " (depends on: " + list(t.DependsOn, ", ") + ")"
		}
		indent := strings.Repeat("  ", min(g.Depth(t), 2))
		name := clip(clean(taskName(t)), idCap)
		line("%s- [%s] %s%s", indent, t.Status, name, deps)
	}
	if hidden := len(p.Tasks) - shown; hidden > 0 {
		line("- … %d more in the plan file", hidden)
	}
	line("")
	line("Next")
	switch {
	case pin != nil:
		line("### %s", clip(clean(pin.ID), idCap))
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
		if len(pin.Accomplished)+len(pin.Modified)+len(pin.VerifiedHow) > 0 || clean(pin.CompletedNotes) != "" {
			// A reopened task: what the last attempt did.
			line("Last time:")
			retro(line, pin)
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
		if t := &p.Tasks[i]; t.Status == Done && (len(t.Accomplished)+len(t.Modified)+len(t.VerifiedHow) > 0 || clean(t.CompletedNotes) != "") {
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
			line("- %s", clip(clean(t.ID), idCap))
			if len(t.Does) > 0 {
				line("  intended: %s", list(t.Does, "; "))
			}
			retro(line, t)
		}
	}
	line("")
	line("For the next chat")
	if pin == nil {
		line("No task is pinned. Pin one in Rex before starting work. Open the plan file in Rex.")
	} else {
		line("Work only on the pinned task. Stay in scope. Open the plan file in Rex.")
	}
	line("Do not inject tools or expand into scope.out. If the plan is wrong, edit the JSON.")
	line("When you finish, end your final reply with these lines:")
	line("accomplished: <what you actually did>")
	line("verified: <how you checked it>")
	line("and, last, the rex-sendoff line below as it is.")
	line("")
	b.WriteString(Fingerprint(clean(rel), clean(pinID)))
	b.WriteString("\n")
	return b.String()
}

// near is a task worth listing in a short brief: the pin, what it waits
// on, and every unfinished task.
func near(t, pin *Task) bool {
	return pin == nil || !Finished(t.Status) || core(t, pin)
}

// core is the pin itself or what it waits on: always listed.
func core(t, pin *Task) bool {
	if pin == nil || t == pin {
		return true
	}
	for _, d := range pin.DependsOn {
		if d == t.ID {
			return true
		}
	}
	return false
}

// retro is a task's retrospective: what was done, changed and checked,
// and the notes left for the next send-off.
func retro(line func(string, ...any), t *Task) {
	if len(t.Accomplished) > 0 {
		line("  actual: %s", list(t.Accomplished, "; "))
	}
	if len(t.Modified) > 0 {
		line("  modified: %s", list(t.Modified, ", "))
	}
	if len(t.VerifiedHow) > 0 {
		line("  verified: %s", list(t.VerifiedHow, "; "))
	}
	if n := clean(t.CompletedNotes); n != "" {
		line("  notes: %s", clip(n, fieldCap))
	}
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
			ids = append(ids, clip(clean(taskName(&p.Tasks[i])), idCap))
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
