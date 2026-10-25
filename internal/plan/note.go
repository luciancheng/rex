package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/luciancheng/rex/internal/watch"
)

// Pin context (docs/ux.md §7a): before each prompt, Rex's prompt hooks
// hand the agent the task the person's plan pins, read from the file just
// then. PinFor finds it and PinNote words it; internal/watch shapes the
// reply for each harness, and nothing here writes anything.

// Pinned is what a prompt hook hands the agent: the explicitly pinned task
// of a repo's plan, or, with nothing pinned anywhere, the repo's current
// plan while it has no tasks yet (Task is nil).
type Pinned struct {
	Rel  string // the plan file from the repo root, slash-separated
	Plan *Plan
	Task *Task
}

// The lookup's walk of rex/plans: hidden folders skipped, and bounded, so
// a huge tree costs a hook only its deadline.
var pinWalk = Limit{Files: 256, Depth: 4, Hidden: true}

// PinFor is what the prompt hooks hand an agent working in cwd (the
// payload's, never the hook's own folder), or nil to send nothing. The
// repo is the folder holding .git above cwd, else cwd. Its plan is the one
// state.json last opened for it (through the path jail) if that has an
// active leaf, else the one plan under rex/plans with an active leaf;
// none, or several, send nothing. A computed next up is never sent, a
// read-only plan sends nothing, and neither does a plan that can't be read,
// the current one or any in the walk (it may be mid-save or mid-merge with
// a pin inside, and another plan's pin is no guess to make), nor does one
// whose path the note can't name as it is. With nothing pinned in a walk
// read to its end, a current plan with no tasks yet is named instead.
func PinFor(cwd, statePath string) *Pinned {
	if !filepath.IsAbs(cwd) {
		return nil
	}
	repo, _ := FindRepo(cwd)
	scope := watch.NewScope(repo)
	found := func(path string, p *Plan, t *Task) *Pinned {
		if p.ReadOnly != "" {
			return nil
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if noteText(rel, noteItem) != rel {
			return nil // the note would fold or cut it and name no file
		}
		return &Pinned{Rel: rel, Plan: p, Task: t}
	}
	var cur *Plan
	var seen []os.FileInfo // each file once: a link or a hard link to a plan is that plan
	last := Jail(scope, repo, LoadState(statePath).Last(repo), "")
	if last != "" {
		p, err := Load(last)
		if err != nil {
			return nil
		}
		if t := activePin(p); t != nil {
			return found(last, p, t)
		}
		cur = p
		if info, err := os.Stat(last); err == nil {
			seen = append(seen, info)
		}
	}
	files, more := Files(Dir(repo), pinWalk)
	var one *Pinned
	n := 0
	for _, f := range files {
		if slices.ContainsFunc(seen, func(info os.FileInfo) bool { return os.SameFile(info, f.Info) }) {
			continue
		}
		seen = append(seen, f.Info)
		path := Jail(scope, repo, f.Path, "")
		if path == "" {
			continue // a link that leads out of rex/plans
		}
		p, err := Load(path)
		if err != nil {
			return nil
		}
		if t := activePin(p); t != nil {
			n++
			one = found(path, p, t)
		}
	}
	switch {
	case n == 1 && !more:
		return one
	case n > 0 || more:
		return nil // several, or some past the walk's bound: no guessing
	case cur != nil && len(cur.Tasks) == 0:
		return found(last, cur, nil)
	}
	return nil
}

// activePin is p's explicit pin: its first active leaf, the one Rex shows
// as pinned, or nil.
func activePin(p *Plan) *Task {
	if t := p.Graph().Pin(); t != nil && t.Status == Active {
		return t
	}
	return nil
}

// The note's caps (docs/ux.md §7a), in characters but for the whole note,
// which is in bytes.
const (
	noteItem  = 160
	noteGoal  = 200
	noteDesc  = 300
	noteItems = 4
	noteMax   = 1500
)

// noteLine is one "Label: value" line of the note. A list shows its first
// items, then how many more the file has.
type noteLine struct {
	label string
	items []string
	sep   string // between items; "" for a single value
	more  int
	tail  string
}

func (l noteLine) String() string {
	s := l.label + ": " + strings.Join(l.items, l.sep)
	if l.more > 0 {
		s += fmt.Sprintf(" (+%d more in the file)", l.more)
	}
	return s + l.tail
}

// list is a list line: each item cleaned and capped, at most noteItems of
// them.
func list(label, sep string, values []string) noteLine {
	l := noteLine{label: label, sep: sep}
	for _, v := range values {
		if v = noteText(v, noteItem); v == "" {
			continue
		}
		if len(l.items) == noteItems {
			l.more++
			continue
		}
		l.items = append(l.items, v)
	}
	return l
}

// PinNote is the note a prompt hook prints (docs/ux.md §7a): facts read
// from the plan, never orders, always starting "Rex note" (so Codex never
// reads it as JSON), at most 1,500 bytes. session words it for the start
// of a session (after a compaction) rather than for one prompt. A plan
// with no tasks gets one line.
func PinNote(pin *Pinned, session bool) string {
	path := noteText(pin.Rel, noteItem)
	if pin.Task == nil {
		return "Rex note: the person has " + path + " open in Rex; it has no tasks yet."
	}
	p, t := pin.Plan, pin.Task
	g := p.Graph()
	head := "Rex note, as of this prompt: the task pinned in the person's plan, read just now from " + path + ". The plan file is the source of truth; earlier Rex notes in this conversation are out of date."
	if session {
		head = "Rex note, as of the start of this session: the task pinned in the person's plan, read from " + path + ". The pin can move during the session; the plan file has the current one."
	}
	task := "Pinned task: " + noteText(t.ID, noteItem)
	if title := noteText(t.Title, noteItem); title != "" {
		task += " · " + title
	}
	if title := noteText(p.Title, noteItem); title != "" {
		task += ` (status active, in plan "` + title + `")`
	} else {
		task += " (status active)"
	}
	var also []string
	for i := range p.Tasks {
		if e := &p.Tasks[i]; e != t && e.Status == Active && e.ID != "" && g.Leaf(e) && !g.Cyclic(e) {
			also = append(also, e.ID)
		}
	}
	var waits []string
	for _, id := range g.Unmet(t) {
		if !strings.HasPrefix(id, "?") {
			waits = append(waits, id)
		}
	}
	waiting := list("Waits on", ", ", waits)
	if len(waiting.items) > 0 {
		waiting.tail = " (not done)"
	}
	var builds noteLine
	if b := g.BuildsOn(t); b != nil {
		what := noteText(b.DoneWhat(), noteItem)
		if id := noteText(b.ID, noteItem); id != "" {
			what = noteText(id+" (done): "+what, noteItem)
		} else {
			what = noteText(what, noteItem-len(" (done)")) + " (done)"
		}
		builds = noteLine{label: "Builds on", items: []string{what}}
	}
	var branch noteLine
	if base := g.StackBase(t); plainRef(t.Branch) && base != "" {
		branch = noteLine{label: "Branch", items: []string{noteText(t.Branch+", stacked on "+base, noteItem)}}
	} else if plainRef(t.Branch) {
		branch = noteLine{label: "Branch", items: []string{t.Branch}}
	} else if base != "" {
		branch = noteLine{label: "Branch", items: []string{noteText("not named yet, stacked on "+base, noteItem)}}
	}
	single := func(label, v string, limit int) noteLine {
		if v = noteText(v, limit); v == "" {
			return noteLine{label: label}
		}
		return noteLine{label: label, items: []string{v}}
	}
	out := append(append([]string(nil), p.Scope.Out...), t.Scope.Out...)
	files := list("Files", ", ", t.Files)
	body := []noteLine{
		list("Also active", ", ", also),
		list("Does", "; ", t.Does),
		single("Description", t.Description, noteDesc),
		list("Needs", "; ", t.Needs),
		waiting,
		list("Verify", "; ", t.Verify),
		files,
		builds,
		list("Out of scope", "; ", distinctText(out)),
		branch,
		single("Plan goal", p.Goal, noteGoal),
	}
	return fitNote(head, task, body)
}

// The note's last line. It names the files only while a Files line is
// there to say which they are.
const (
	footFiles = "The person wants this turn to serve this task and stay inside its files and scope. If the prompt asks for something outside it, they expect you to say so before editing. Edits outside these files show in Rex as drift."
	footScope = "The person wants this turn to serve this task and stay inside its scope. If the prompt asks for something outside it, they expect you to say so before editing."
)

// fitNote joins the note's lines, leaving out those with no value, and
// keeps it to noteMax bytes, cut at line boundaries: the plan's goal goes
// first, then the branch, what it builds on and what is out of scope, then
// list items from the end, a round at a time from the last list up, each
// list's first item only once no list has a second, and the description
// only once no list has an item. The first two lines and the last always
// stay; only the task line is cut, last of all.
func fitNote(head, task string, body []noteLine) string {
	join := func() string {
		lines, foot := []string{head, task}, footScope
		for _, l := range body {
			if len(l.items) > 0 {
				lines = append(lines, l.String())
				if l.label == "Files" {
					foot = footFiles
				}
			}
		}
		return strings.Join(append(lines, foot), "\n")
	}
	drop := func(label string) {
		for i := range body {
			if body[i].label == label {
				body[i].items = nil
			}
		}
	}
	for _, label := range []string{"Plan goal", "Branch", "Builds on", "Out of scope"} {
		if len(join()) <= noteMax {
			return join()
		}
		drop(label)
	}
	for _, keep := range []int{1, 0} {
		for cut := true; cut && len(join()) > noteMax; {
			cut = false
			for i := len(body) - 1; i >= 0 && len(join()) > noteMax; i-- {
				if l := &body[i]; l.sep != "" && len(l.items) > keep {
					l.items = l.items[:len(l.items)-1]
					l.more++
					cut = true
				}
			}
		}
	}
	if len(join()) > noteMax {
		drop("Description")
	}
	if over := len(join()) - noteMax; over > 0 {
		task = cutBytes(task, len(task)-over)
	}
	return join()
}

// noteText is s as one line of the note: cleaned of escape sequences and
// control characters, its whitespace folded, a system-reminder tag
// defused, and at most limit characters.
func noteText(s string, limit int) string {
	s = defuse(watch.NormalizeText(watch.CleanText(s)))
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit-1]) + "…"
}

// defuse turns "<system-reminder" and "</system-reminder", in any case,
// into "‹system-reminder" and "‹/system-reminder", so plan text can't open
// or close the tags an agent's harness wraps its own notes in.
func defuse(s string) string {
	const tag = "system-reminder"
	if !strings.Contains(strings.ToLower(s), tag) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			j := i + 1
			if j < len(s) && s[j] == '/' {
				j++
			}
			if j+len(tag) <= len(s) && asciiFold(s[j:j+len(tag)]) == tag {
				b.WriteString("‹")
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// asciiFold lowercases ASCII letters only, so a match keeps its length.
func asciiFold(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// cutBytes is s in at most n bytes, cut on a rune boundary with "…".
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	n -= len("…")
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:max(0, n)] + "…"
}

// distinctText is values without repeats, in order.
func distinctText(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if k := strings.TrimSpace(v); !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	return out
}
