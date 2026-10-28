package tui

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// statusValue is a status member's value in a plan file.
var statusValue = regexp.MustCompile(`("status"\s*:\s*)"(pending|active|done|blocked|skipped)"`)

// onlyStatuses reports that after differs from before in status values
// alone, byte for byte everywhere else.
func onlyStatuses(before, after string) bool {
	blank := func(s string) string { return statusValue.ReplaceAllString(s, `$1""`) }
	return blank(before) == blank(after)
}

// sameButStatus reports that two plan files hold the same JSON but for
// status values: the plan's own (only where both have one: Rex never adds
// a member at the root) and its tasks' (a task may gain one), each one of
// the five. Anything else that differs, a root member added or removed or
// any other value, fails it.
func sameButStatus(before, after []byte) bool {
	decode := func(b []byte) map[string]any {
		var v map[string]any
		if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &v) != nil {
			return nil
		}
		return v
	}
	statuses := map[any]bool{"pending": true, "active": true, "done": true, "blocked": true, "skipped": true}
	// rest is obj without its status, which must be a status if after has
	// one; had says before had a status there.
	rest := func(obj map[string]any, had, mayAdd bool) (map[string]any, bool) {
		out := map[string]any{}
		for k, v := range obj {
			out[k] = v
		}
		st, has := out["status"]
		delete(out, "status")
		if !has {
			return out, !had // a status is never taken out
		}
		return out, statuses[st] && (had || mayAdd)
	}
	a, b := decode(before), decode(after)
	if a == nil || b == nil {
		return false
	}
	_, rootHad := a["status"]
	if _, has := b["status"]; has != rootHad {
		return false
	}
	ta, _ := a["tasks"].([]any)
	tb, _ := b["tasks"].([]any)
	if len(ta) != len(tb) {
		return false
	}
	for i := range ta {
		ma, okA := ta[i].(map[string]any)
		mb, okB := tb[i].(map[string]any)
		if okA != okB {
			return false
		}
		if !okA {
			if !reflect.DeepEqual(ta[i], tb[i]) {
				return false
			}
			continue
		}
		_, had := ma["status"]
		ra, _ := rest(ma, true, true)
		rb, ok := rest(mb, had, true)
		if !ok || !reflect.DeepEqual(ra, rb) {
			return false
		}
	}
	ra, _ := rest(a, true, false)
	rb, ok := rest(b, rootHad, false)
	delete(ra, "tasks")
	delete(rb, "tasks")
	return ok && reflect.DeepEqual(ra, rb)
}

// screenText is a's frame as plain text, a row a line, trailing blanks off.
func screenText(a *app) string {
	var rows []string
	for _, r := range a.frameRows().Rows {
		rows = append(rows, strings.TrimRight(textOf(r, a.columns), " "))
	}
	return strings.Join(rows, "\n")
}

// docs/ux.md §7, §7b: p on a task pins it at once. The file gets it
// active and the old pin pending, and nothing else; row 1, the tags and
// the footer follow; the notice says what moved and what it waits on; the
// cursor stays; AGENT records it. p again unpins it, and nothing is sent.
func TestPinKey(t *testing.T) {
	a, _, path := openDemo(t, specPlan, 80, 24)
	wire(a, wired("claude"))
	press(a, "down")
	press(a, "p")
	want := strings.NewReplacer(
		`"status": "active", "depends_on": ["tab-bar"]`, `"status": "pending", "depends_on": ["tab-bar"]`,
		`"branch": "feature/agent_stream"}`, `"branch": "feature/agent_stream", "status": "active"}`,
	).Replace(specPlan)
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Fatalf("p writes the two statuses and nothing else:\n%s", got)
	}
	frame := `Rex visual redesign                                     2/8 done   plan · agent
pinned  agent-stream · Fold runs of thoughts             sends with next prompt

  ✔ tab-bar       Tab bar on row 0
    page-titles   Page titles and footer keys
❯   agent-stream  Fold runs of thoughts                                  pinned
    plan-list     Plan list groups
    pin-card      Pinned card on the outline
  ✔ width-table   Glyph widths in width_test
  ! help-page     Help lists every key
  – readme        README screenshots











Pinned agent-stream · sends with your next prompt · waits on page-titles
↑↓ select · enter details · p unpin · d done · esc plans · tab agent · ? help`
	if got := screenText(a); got != frame {
		t.Fatalf("after p:\n%s\nwant\n%s", got, frame)
	}
	if a.toast.Text != "Pinned agent-stream · sends with your next prompt · page-titles unpinned · waits on page-titles" {
		t.Fatalf("the whole notice, before it is fitted: %q", a.toast.Text)
	}
	if last := a.log[len(a.log)-1]; last.Source != "rex" || last.Kind != "plan" || last.Text != "you pinned agent-stream" {
		t.Fatalf("AGENT records it: %+v", last)
	}
	if a.follow || a.plans.Current != path {
		t.Fatal("p never latches, follows or opens anything")
	}
	press(a, "p")
	if got := a.notice().Text; got != "Unpinned agent-stream · nothing is sent now" {
		t.Fatalf("p on the pin unpins it: %q", got)
	}
	if got := a.context().Text; got != "nothing pinned · next up page-titles · Page titles and footer keys" || a.sendState() != notSent {
		t.Fatalf("row 1: %q, %q", got, a.sendState())
	}
	if got, _ := os.ReadFile(path); string(got) != strings.Replace(want, `"branch": "feature/agent_stream", "status": "active"}`, `"branch": "feature/agent_stream", "status": "pending"}`, 1) {
		t.Fatalf("unpin writes one status:\n%s", got)
	}
	// A pin on a done task reopens it; on a blocked one, it says so.
	press(a, "home", "p")
	if got := a.toast.Text; got != "Pinned tab-bar · sends with your next prompt · reopened" {
		t.Fatalf("p on a done task: %q", got)
	}
	press(a, "end", "up", "p")
	if got := a.toast.Text; got != "Pinned help-page · sends with your next prompt · tab-bar unpinned · it was blocked" {
		t.Fatalf("p on a blocked task: %q", got)
	}
	// Narrow, the notice keeps what matters: what it waits on outlasts
	// what it moved off.
	n := partsLine("Pinned x", " · sends with your next prompt", []notePart{{" · y unpinned", 0}, {" · waits on y", 3}})
	for w, want := range map[int]string{80: "Pinned x · sends with your next prompt · y unpinned · waits on y", 60: "Pinned x · sends with your next prompt · waits on y", 40: "Pinned x · sends with your next prompt", 20: "Pinned x", 5: "Pinn…"} {
		if got := n.Fit(w); got != want {
			t.Errorf("%d cells: %q, want %q", w, got, want)
		}
	}
}

// docs/ux.md §8, §7b: d marks the selected task done at once. Nothing
// is pinned in its place: row 1 names what is next up, the notice says so,
// and the footer says d reopens it. d again reopens it.
func TestDoneKey(t *testing.T) {
	a, _, path := openDemo(t, specPlan, 80, 24)
	press(a, "d")
	if got, _ := os.ReadFile(path); string(got) != strings.Replace(specPlan, `"status": "active", "depends_on": ["tab-bar"]`, `"status": "done", "depends_on": ["tab-bar"]`, 1) {
		t.Fatalf("d writes one status:\n%s", got)
	}
	frame := `Rex visual redesign                                     3/8 done   plan · agent
nothing pinned · next up agent-stream · Fold runs of thoughts          not sent

  ✔ tab-bar       Tab bar on row 0
❯ ✔ page-titles   Page titles and footer keys
    agent-stream  Fold runs of thoughts                                 next up
    plan-list     Plan list groups
    pin-card      Pinned card on the outline
  ✔ width-table   Glyph widths in width_test
  ! help-page     Help lists every key
  – readme        README screenshots











✔ page-titles done · nothing pinned · next up agent-stream
↑↓ select · enter details · p pin · d reopen · esc plans · tab agent · ? help`
	if got := screenText(a); got != frame {
		t.Fatalf("after d:\n%s\nwant\n%s", got, frame)
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "you marked page-titles done" {
		t.Fatalf("AGENT records it: %+v", last)
	}
	press(a, "d")
	if got := a.notice().Text; got != "page-titles reopened" {
		t.Fatalf("d on a done task reopens it: %q", got)
	}
	if got := a.context().Text; got != "nothing pinned · next up page-titles · Page titles and footer keys" {
		t.Fatalf("row 1: %q", got)
	}
	// Finish the pin, then pin the next: d ↓ p (docs/ux.md §14).
	b, _, path := cujApp(t)
	k := &keys{a: b}
	k.press("d", "down", "p")
	k.spent(t, 3, "finish the pin, then pin the next (d ↓ p)")
	if p, _ := plan.Load(path); p.Task("card-highlight").Status != plan.Done || p.Task("load-outline").Status != plan.Active {
		t.Fatal("d ↓ p")
	}
	// Nothing is wired in the test's HOME: the notice never claims it goes.
	if got := b.notice().Text; got != "Pinned load-outline · not sent · ? help" {
		t.Fatalf("%q", got)
	}
	// The last leaf finishes the plan, and the file says so.
	b.outline.sel = "load-outline"
	press(b, "d")
	if got := b.notice().Text; got != "✔ load-outline done · the plan is done" {
		t.Fatalf("%q", got)
	}
	if p, _ := plan.Load(path); p.Status != plan.Done {
		t.Fatal("the plan's own status is done")
	}
}

// docs/ux.md §7b: the same key undoes, byte for byte: p p on the pin and
// d d on a pending task leave the file as it was.
func TestSameKeyUndoesInRex(t *testing.T) {
	a, _, path := cujApp(t)
	before, _ := os.ReadFile(path)
	press(a, "p")
	if got, _ := os.ReadFile(path); bytes.Equal(got, before) || !onlyStatuses(string(before), string(got)) {
		t.Fatalf("p unpins:\n%s", got)
	}
	press(a, "p")
	if got, _ := os.ReadFile(path); !bytes.Equal(got, before) {
		t.Fatalf("p p:\n%s", got)
	}
	press(a, "down", "d")
	if got, _ := os.ReadFile(path); bytes.Equal(got, before) || !onlyStatuses(string(before), string(got)) {
		t.Fatalf("d marks done:\n%s", got)
	}
	press(a, "d")
	if got, _ := os.ReadFile(path); !bytes.Equal(got, before) {
		t.Fatalf("d d:\n%s", got)
	}
	// From a task's details too, on that task.
	press(a, "\r", "d")
	if p, _ := plan.Load(path); a.at != atTaskDetail || p.Task("load-outline").Status != plan.Done {
		t.Fatal("d on a task's details")
	}
	if got := textOf(a.frameRows().Rows[0], a.columns); !strings.Contains(got, "✔ done") {
		t.Fatalf("its row 0 follows: %q", got)
	}
	press(a, "d", "esc")
	if got, _ := os.ReadFile(path); !bytes.Equal(got, before) || a.outline.sel != "load-outline" {
		t.Fatalf("d d from details:\n%s", got)
	}
}

// docs/ux.md §7b: the key carries what you saw. A task the file moved on
// since (your agent saved meanwhile, and no poll has shown it yet) is
// refused, nothing written, and the list shows the file as it is.
func TestStaleStatusRefused(t *testing.T) {
	a, _, path := openDemo(t, specPlan, 80, 24)
	agent := strings.Replace(specPlan, `"status": "active", "depends_on": ["tab-bar"]`, `"status": "done", "depends_on": ["tab-bar"]`, 1)
	os.WriteFile(path, []byte(agent), 0o644)
	press(a, "p") // the frame still shows page-titles pinned
	if got, _ := os.ReadFile(path); string(got) != agent {
		t.Fatalf("nothing written:\n%s", got)
	}
	frame := `Rex visual redesign                                     3/8 done   plan · agent
nothing pinned · next up agent-stream · Fold runs of thoughts          not sent

  ✔ tab-bar       Tab bar on row 0
❯ ✔ page-titles   Page titles and footer keys
    agent-stream  Fold runs of thoughts                                 next up
    plan-list     Plan list groups
    pin-card      Pinned card on the outline
  ✔ width-table   Glyph widths in width_test
  ! help-page     Help lists every key
  – readme        README screenshots











✗ page-titles just changed in the file · look again
↑↓ select · enter details · p pin · d reopen · esc plans · tab agent · ? help`
	if got := screenText(a); got != frame {
		t.Fatalf("the refusal:\n%s\nwant\n%s", got, frame)
	}
	if _, k := a.noticeOf(); k != noticeError {
		t.Fatalf("a refusal is an error notice: %v", k)
	}
	// Another task became active meanwhile: a pin would move off it.
	b, c, path := openDemo(t, specPlan, 80, 24)
	press(b, "down")
	other := strings.Replace(specPlan, `"title": "Plan list groups"`, `"title": "Plan list groups", "status": "active"`, 1)
	os.WriteFile(path, []byte(other), 0o644)
	press(b, "p")
	if got := b.notice().Text; got != "✗ plan-list just changed in the file · look again" {
		t.Fatalf("%q", got)
	}
	if got, _ := os.ReadFile(path); string(got) != other {
		t.Fatal("nothing written")
	}
	// The refusal holds its 4 s while the change it found is credited.
	settled(b, c)
	if _, k := b.noticeOf(); k != noticeError {
		t.Fatalf("the refusal holds: %q", b.notice().Text)
	}
	c.t = c.t.Add(toastFor)
	b.tick()
	if _, k := b.noticeOf(); k == noticeError {
		t.Fatalf("for 4 s: %q", b.notice().Text)
	}
}

// docs/ux.md §7b: a write is refused, with a notice and nothing written,
// on a read-only plan, one Rex can't read, a task whose id appears twice,
// a task with no id and a task with tasks under it. The footer offers p
// and d only where they can write.
func TestWritesRefused(t *testing.T) {
	for _, c := range []struct {
		name, src string
		setup     func(a *app, path string)
		want      string
		segs      bool
	}{
		{"read-only", strings.Replace(demoPlan, `"version": 1`, `"version": 2`, 1), nil, "✗ Read-only: version 2 · newer than this Rex reads · p and d are off", false},
		// The file's own error holds the notice row; the refusal is under it.
		{"unreadable", demoPlan, func(a *app, path string) {
			os.WriteFile(path, []byte("{"), 0o644)
			a.reloadDoc()
			a.reloadDoc()
		}, "✗ plan JSON error 1:2 · showing last good", false},
		{"a duplicate id", `{"version": 1, "tasks": [{"id": "a", "status": "active"}, {"id": "a", "title": "again"}]}`, nil, "✗ two tasks are called a · fix the plan first", true},
		// p on b would move the pin off a, whose id is used twice: the
		// notice names a.
		{"a duplicate id it moves off", `{"version": 1, "tasks": [{"id": "a", "status": "active"}, {"id": "a"}, {"id": "b"}]}`, func(a *app, _ string) { press(a, "end") }, "✗ two tasks are called a · fix the plan first", true},
		{"no id", `{"version": 1, "tasks": [{"title": "no id yet"}, {"id": "b"}]}`, func(a *app, _ string) { press(a, "home") }, "✗ This task has no id · give it one in the file first", true},
		{"a parent loop", `{"version": 1, "tasks": [{"id": "a", "parent": "b"}, {"id": "b", "parent": "a"}]}`, nil, "✗ a has tasks under it · pick one of those", true},
		{"a depends_on loop", `{"version": 1, "tasks": [{"id": "x", "depends_on": ["y"]}, {"id": "y", "depends_on": ["x"]}]}`, nil, "✗ x is in a loop · fix the plan first", true},
	} {
		a, _, path := openDemo(t, c.src, 80, 24)
		if c.setup != nil {
			c.setup(a, path)
		}
		before, _ := os.ReadFile(path)
		press(a, "p")
		if got := a.notice().Text; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		if c.name == "unreadable" && a.toast.Text != "Can't read the plan · p and d are off" {
			t.Errorf("unreadable: the refusal %q", a.toast.Text)
		}
		if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
			t.Errorf("%s: written", c.name)
		}
		footer := textOf(a.frameRows().Rows[23], 80)
		if got := strings.Contains(footer, "p pin") || strings.Contains(footer, "p unpin"); got != c.segs {
			t.Errorf("%s: the footer offers p: %q", c.name, footer)
		}
	}
	// Nothing to act on: no tasks, no list, About, Help, AGENT.
	for _, setup := range []func(a *app){
		func(a *app) { press(a, "i") },
		func(a *app) { press(a, "?") },
		func(a *app) { press(a, "\t") },
		func(a *app) { press(a, "esc") },
	} {
		a, _, path := openDemo(t, demoPlan, 80, 24)
		setup(a)
		before := a.log
		press(a, "p", "d")
		if got, _ := os.ReadFile(path); string(got) != demoPlan || len(a.log) != len(before) {
			t.Errorf("p and d act at=%d pane=%d overlay=%d", a.at, a.pane, a.overlay[a.pane])
		}
	}
}

// docs/ux.md §7b: every reload that changes the plan gets one notice,
// credited: your agent when its write of the file came with it, the plan
// when nothing claimed it (an editor, a checkout), and nobody's news when
// the file holds Rex's own last write again.
func TestWhoChangedIt(t *testing.T) {
	agentSaves := func(a *app, c *clock, path, body string) {
		os.WriteFile(path, []byte(body), 0o644)
		a.ingest([]watch.Event{at(c, 0, wrote(a.plans.Repo, path))})
	}
	a, c, path := openDemo(t, demoPlan, 80, 24)
	moved := strings.Replace(strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"]`, `"status": "pending",
      "depends_on": ["plans-ui"]`, 1), `"status": "pending",
      "depends_on": ["card-highlight"]`, `"status": "active",
      "depends_on": ["card-highlight"]`, 1)
	agentSaves(a, c, path, moved)
	if got := a.notice().Text; got != "Your agent pinned load-outline" {
		t.Fatalf("%q", got)
	}
	both := strings.Replace(demoPlan, `"status": "pending",
      "depends_on": ["card-highlight"]`, `"status": "active",
      "depends_on": ["card-highlight"]`, 1)
	both = strings.Replace(both, `"status": "active",
      "depends_on": ["plans-ui"]`, `"status": "done",
      "depends_on": ["plans-ui"]`, 1)
	agentSaves(a, c, path, demoPlan)
	agentSaves(a, c, path, both)
	if got := a.notice().Text; got != "Your agent finished card-highlight ✔ and pinned load-outline" {
		t.Fatalf("%q", got)
	}
	added := strings.Replace(both, "  ]\n}", `    ,{"id": "x"}, {"id": "y"}
  ]
}`, 1)
	agentSaves(a, c, path, added)
	if got := a.notice().Text; got != "Your agent added 2 tasks" {
		t.Fatalf("%q", got)
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "+2 tasks" {
		t.Fatalf("AGENT's line: %+v", last)
	}
	// The poll reads the save before its hook reports it: the notice waits
	// for the hook, and is your agent's.
	c.t = c.t.Add(toastFor)
	renamed := strings.Replace(added, `"title": "Outline view"`, `"title": "Outline"`, 1)
	os.WriteFile(path, []byte(renamed), 0o644)
	later(a, c)
	if got := a.notice().Text; got != "" || !strings.Contains(planBody(a), "load-outline    Outline ") {
		t.Fatalf("the rows follow at once, the notice waits: %q\n%s", got, planBody(a))
	}
	c.t = c.t.Add(300 * time.Millisecond)
	a.ingest([]watch.Event{at(c, 0, wrote(a.plans.Repo, path))})
	if got := a.notice().Text; got != "Your agent changed the plan · edited" {
		t.Fatalf("%q", got)
	}
	// No hook claims it: the plan changed, once creditWait is up.
	c.t = c.t.Add(toastFor)
	os.WriteFile(path, []byte(strings.Replace(renamed, `{"id": "x"}`, `{"id": "x", "status": "skipped"}`, 1)), 0o644)
	later(a, c)
	if got := a.notice().Text; got != "" {
		t.Fatalf("not yet: %q", got)
	}
	c.t = c.t.Add(creditWait)
	a.tick()
	if got := a.notice().Text; got != "The plan changed · x → skipped" {
		t.Fatalf("%q", got)
	}
	// Rex's own bytes again: AGENT records it, the notice row stays.
	c.t = c.t.Add(toastFor)
	a.tick()
	press(a, "home", "d") // reopen plans-ui
	mine, _ := os.ReadFile(path)
	c.t = c.t.Add(toastFor)
	a.tick()
	os.WriteFile(path, []byte(renamed), 0o644)
	settled(a, c)
	c.t = c.t.Add(toastFor)
	a.tick()
	os.WriteFile(path, mine, 0o644)
	logged := len(a.log)
	settled(a, c)
	if got := a.notice().Text; got != "" || len(a.log) != logged+1 {
		t.Fatalf("your own bytes: %q, %d lines", got, len(a.log)-logged)
	}
}

// docs/ux.md §7b: your agent saves the whole file from a copy
// older than your pin. Rex says so once, never writes it back, and p pins
// it again.
func TestYourAgentSavedOverYourPin(t *testing.T) {
	a, c, path := openDemo(t, specPlan, 80, 24)
	press(a, "down", "p")
	c.t = c.t.Add(toastFor)
	os.WriteFile(path, []byte(specPlan), 0o644) // the agent's older copy
	a.ingest([]watch.Event{at(c, 0, wrote(a.plans.Repo, path))})
	frame := `Rex visual redesign                                     2/8 done   plan · agent
pinned  page-titles · Page titles and footer keys             not sent · ? help

  ✔ tab-bar       Tab bar on row 0
    page-titles   Page titles and footer keys                            pinned
❯   agent-stream  Fold runs of thoughts
    plan-list     Plan list groups
    pin-card      Pinned card on the outline
  ✔ width-table   Glyph widths in width_test
  ! help-page     Help lists every key
  – readme        README screenshots











Your agent saved over your pin of agent-stream · p pins it again
↑↓ select · enter details · p pin · d done · esc plans · tab agent · ? help`
	if got := screenText(a); got != frame {
		t.Fatalf("saved over:\n%s\nwant\n%s", got, frame)
	}
	if got, _ := os.ReadFile(path); string(got) != specPlan {
		t.Fatal("Rex never fights back")
	}
	// Once: the agent's next save is news of its own.
	c.t = c.t.Add(toastFor)
	os.WriteFile(path, []byte(strings.Replace(specPlan, `"title": "Plan list groups"`, `"title": "Plan list"`, 1)), 0o644)
	a.ingest([]watch.Event{at(c, 0, wrote(a.plans.Repo, path))})
	if got := a.notice().Text; got != "Your agent changed the plan · edited" {
		t.Fatalf("%q", got)
	}
	// A save that keeps your pin says what it did, not that it saved over.
	press(a, "p")
	c.t = c.t.Add(toastFor)
	kept, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(kept), `"title": "Plan list"`, `"title": "Plan list groups"`, 1)), 0o644)
	a.ingest([]watch.Event{at(c, 0, wrote(a.plans.Repo, path))})
	if got := a.notice().Text; got != "Your agent changed the plan · edited" {
		t.Fatalf("%q", got)
	}
	// d and its undo say so too.
	for _, k := range []struct{ key, want string }{{"d", "Your agent saved over marking agent-stream done · d marks it again"}, {"d", "Your agent saved over reopening agent-stream · d reopens it again"}} {
		c.t = c.t.Add(toastFor)
		before, _ := os.ReadFile(path)
		press(a, k.key)
		c.t = c.t.Add(toastFor)
		os.WriteFile(path, before, 0o644)
		a.ingest([]watch.Event{at(c, 0, wrote(a.plans.Repo, path))})
		if got := a.notice().Text; got != k.want {
			t.Fatalf("%q, want %q", got, k.want)
		}
		os.WriteFile(path, before, 0o644)
		press(a, k.key) // write it again, for the next round
	}
}

// docs/ux.md §7b: a key fuzz on every screen. Only p and d write a plan,
// and only the open plan's status values, and only enter creates a plan
// (New plan, + Create) in rex/plans; no key writes, creates or removes
// anything else in the repo, nor anything outside it, where a link in
// rex/plans leads (a file and a folder).
func TestKeyFuzzWritesOnlyPDAndCreate(t *testing.T) {
	pool := []string{"p", "d", "p", "d", "i", "o", "?", "/", "x", "a", "e", "v", " ", "1", "\r", "\r", "esc", "\t", "up", "down", "left", "right", "home", "end", "pageup", "pagedown", "\x7f", "\x15", "\x17", "\x0c", PastePrefix + "pin a/b", PastePrefix + "evil", "é"}
	snapshot := func(roots ...string) map[string]string {
		files := map[string]string{}
		for _, root := range roots {
			filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					data, _ := os.ReadFile(p)
					files[p] = string(data)
				}
				return nil
			})
		}
		return files
	}
	writes, links := 0, 0
	for i, s := range matrixScreens(t) {
		a := matrixApp(t, s, 80, 24)
		// A plan and a folder outside the repo, linked into rex/plans.
		outside := t.TempDir()
		os.WriteFile(filepath.Join(outside, "evil.rex.json"), []byte(`{"version":1,"tasks":[{"id":"a","status":"pending"}]}`), 0o644)
		os.MkdirAll(filepath.Join(outside, "dir"), 0o755)
		os.WriteFile(filepath.Join(outside, "dir", "deep.rex.json"), []byte(`{"version":1,"tasks":[{"id":"a","status":"done"}]}`), 0o644)
		if os.MkdirAll(a.plans.Root, 0o755) == nil &&
			os.Symlink(filepath.Join(outside, "evil.rex.json"), filepath.Join(a.plans.Root, "evil.rex.json")) == nil &&
			os.Symlink(filepath.Join(outside, "dir"), filepath.Join(a.plans.Root, "evildir")) == nil {
			links++
		}
		a.plans.walk = planWalk{}
		rng := rand.New(rand.NewSource(int64(i) + 1))
		for n := 0; n < 50 && !a.quit; n++ {
			key := pool[rng.Intn(len(pool))]
			before := snapshot(a.plans.Repo, outside)
			open := a.plans.Current
			press(a, key)
			after := snapshot(a.plans.Repo, outside)
			for p, was := range before {
				now, ok := after[p]
				switch {
				case !ok:
					t.Fatalf("%s: %q removed %s", s.name, key, p)
				case now == was:
				case key != "p" && key != "d":
					t.Fatalf("%s: %q wrote %s", s.name, key, p)
				case !strings.HasPrefix(p, a.plans.Repo) || !samePlan(p, open) || !sameButStatus([]byte(was), []byte(now)):
					t.Fatalf("%s: %q wrote more than a status of the open plan in %s:\n%s", s.name, key, p, now)
				default:
					writes++
				}
			}
			for p := range after {
				if _, ok := before[p]; !ok && (key != "\r" || !strings.HasPrefix(p, a.plans.Root+string(filepath.Separator))) {
					t.Fatalf("%s: %q created %s", s.name, key, p)
				}
			}
		}
	}
	if writes == 0 {
		t.Fatal("the fuzz never pressed p or d where they write")
	}
	if links == 0 {
		t.Log("no symlinks here: the fuzz ran without links out")
	}
}

// docs/ux.md §4: the footer's p and d follow the selection, fitted by
// their longer verbs, on the task list and a task's details only.
func TestFooterVerbsFollowTheSelection(t *testing.T) {
	a, _, _ := cujApp(t)
	a.settle(80, 24)
	footer := func() string { r := a.frameRows().Rows; return textOf(r[len(r)-1], a.columns) }
	for _, c := range []struct{ key, want string }{
		{"", "↑↓ select · enter details · p unpin · d done · esc plans · tab agent · ? help"},
		{"up", "↑↓ select · enter details · p pin · d reopen · esc plans · tab agent · ? help"},
		{"end", "↑↓ select · enter details · p pin · d done · esc plans · tab agent · ? help"},
		{"\r", "↑↓ scroll · p pin · d done · esc tasks · tab agent · ? help"},
	} {
		if c.key != "" {
			press(a, c.key)
		}
		if got := footer(); got != c.want {
			t.Errorf("after %q: %q, want %q", c.key, got, c.want)
		}
	}
	press(a, "esc", "i")
	if got := footer(); strings.Contains(got, "p ") || strings.Contains(got, "d ") {
		t.Errorf("About has no p or d: %q", got)
	}
	// At 200 columns there is room for i about too.
	press(a, "esc")
	a.settle(200, 24)
	if got := footer(); got != "↑↓ select · enter details · p pin · d done · i about · esc plans · tab agent · ? help" {
		t.Errorf("wide: %q", got)
	}
}

// docs/ux.md §7b, §11: p and d write only a plan file under rex/plans. A
// link there that leads out of it (here, out of the repo) is never listed
// on Plans, in a find or in Open a plan, and a plan opened through one
// anyway is never written, and its footer offers neither key.
func TestALinkOutOfPlansIsNeverWritten(t *testing.T) {
	repo := attachRepo(t)
	writePlan(t, repo, "real.rex.json", demoPlan)
	outside := filepath.Join(t.TempDir(), "elsewhere.rex.json")
	const src = `{"version":1,"tasks":[{"id":"a","status":"pending"}]}`
	os.WriteFile(outside, []byte(src), 0o644)
	link := filepath.Join(PlansDir(repo), "evil.rex.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	a := startIn(repo)
	a.settle(80, 24)
	if f := screenText(a); strings.Contains(f, "evil") || a.plans.total != 1 {
		t.Fatalf("Plans lists the link out:\n%s", f)
	}
	typeText(a, "evil")
	for _, row := range a.plans.Rows {
		if row.Kind == PlanRowPlan {
			t.Fatalf("a find matches the link out: %+v", row)
		}
	}
	press(a, "esc")
	a.plans.pick(PlanRowOpen)
	press(a, "\r")
	for _, row := range a.plans.Rows {
		if strings.Contains(row.Name, "evil") {
			t.Fatalf("Open a plan lists the link out: %+v", row)
		}
	}
	// Opened anyway (a link swapped in after it was listed, say).
	press(a, "esc")
	a.plans.open(link)
	a.openedPlan()
	if a.at != atOutline || a.doc.plan == nil {
		t.Fatal("setup: the plan opens")
	}
	if got := textOf(a.frameRows().Rows[23], 80); strings.Contains(got, "p pin") || strings.Contains(got, "d done") {
		t.Fatalf("the footer offers p and d: %q", got)
	}
	press(a, "p", "d")
	if got := a.notice().Text; got != "✗ "+outsideNotice {
		t.Fatalf("%q", got)
	}
	if got, _ := os.ReadFile(outside); string(got) != src {
		t.Fatalf("the file outside was written:\n%s", got)
	}
	// And at the key, whatever the open said: a link swapped in later.
	a.doc.outside = false
	press(a, "p")
	if got, _ := os.ReadFile(outside); string(got) != src || a.notice().Text != "✗ "+outsideNotice {
		t.Fatalf("the key jails the file itself: %q\n%s", a.notice().Text, got)
	}
}

// docs/ux.md §3: p unpins only the pin. On a second active task (tagged
// also active) it pins that task, which moves the pin off the first.
func TestPOnASecondActiveTaskPinsIt(t *testing.T) {
	src := `{"version": 1, "status": "active", "tasks": [{"id": "a", "title": "A", "status": "active"}, {"id": "b", "title": "B", "status": "active"}]}`
	a, _, path := openDemo(t, src, 80, 24)
	wire(a, wired("codex"))
	press(a, "down")
	if a.outline.sel != "b" || !strings.HasSuffix(rowWith(a.frameRows(), "❯   b"), "also active") {
		t.Fatalf("setup: the cursor on the second active task\n%s", screenText(a))
	}
	if got := textOf(a.frameRows().Rows[23], 80); !strings.Contains(got, "p pin · d done") {
		t.Fatalf("the footer says p pins it: %q", got)
	}
	press(a, "p")
	if got, _ := os.ReadFile(path); string(got) != strings.Replace(src, `"id": "a", "title": "A", "status": "active"`, `"id": "a", "title": "A", "status": "pending"`, 1) {
		t.Fatalf("b stays active, a goes pending:\n%s", got)
	}
	if got := a.notice().Text; got != "Pinned b · sends with your next prompt · a unpinned" {
		t.Fatalf("%q", got)
	}
	if got := textOf(a.frameRows().Rows[23], 80); !strings.Contains(got, "p unpin") {
		t.Fatalf("now it is the pin: %q", got)
	}
}

// docs/ux.md §7b: your agent saved over Rex's write only when everything
// the write changed is back as it was. Your agent pinning another task
// after your pin, or finishing a task you reopened, is its own move.
func TestSavedOverNeedsEverythingBack(t *testing.T) {
	agentSaves := func(a *app, c *clock, path, body string) {
		c.t = c.t.Add(toastFor)
		os.WriteFile(path, []byte(body), 0o644)
		a.ingest([]watch.Event{at(c, 0, wrote(a.plans.Repo, path))})
	}
	// p on agent-stream, then your agent pins plan-list from the file as
	// Rex left it: agent-stream goes pending, but page-titles stays so.
	a, c, path := openDemo(t, specPlan, 80, 24)
	press(a, "down", "p")
	mine, _ := os.ReadFile(path)
	next := strings.Replace(string(mine), `"branch": "feature/agent_stream", "status": "active"}`, `"branch": "feature/agent_stream", "status": "pending"}`, 1)
	next = strings.Replace(next, `{"id": "plan-list", "title": "Plan list groups"}`, `{"id": "plan-list", "title": "Plan list groups", "status": "active"}`, 1)
	agentSaves(a, c, path, next)
	if got := a.notice().Text; got != "Your agent pinned plan-list" {
		t.Fatalf("%q", got)
	}
	// d reopens tab-bar, then your agent finishes it again, with what it
	// did this time.
	b, c, path := openDemo(t, specPlan, 80, 24)
	press(b, "home", "d")
	mine, _ = os.ReadFile(path)
	redone := strings.Replace(string(mine), `"title": "Tab bar on row 0", "status": "pending"`, `"title": "Tab bar on row 0", "status": "done"`, 1)
	redone = strings.Replace(redone, `"accomplished": ["Two tabs on row 0, the active one filled", "Cue cell is fixed width"]`, `"accomplished": ["Two tabs on row 0, redone"]`, 1)
	agentSaves(b, c, path, redone)
	if got := b.notice().Text; strings.Contains(got, "saved over") || !strings.HasPrefix(got, "Your agent ") {
		t.Fatalf("%q", got)
	}
	// The true case: your agent's whole file from before your pin.
	d, c, path := openDemo(t, specPlan, 80, 24)
	press(d, "down", "p")
	agentSaves(d, c, path, specPlan)
	if got := d.notice().Text; got != "Your agent saved over your pin of agent-stream · p pins it again" {
		t.Fatalf("%q", got)
	}
}

// docs/ux.md §7b: a hook that reports your agent's save after creditWait
// (Codex runs hooks through a login shell) still claims it: the notice
// that called it the plan's becomes your agent's, and says it saved over
// your pin. Long after, a write of the same bytes claims nothing.
func TestALateHookStillClaims(t *testing.T) {
	a, c, path := openDemo(t, specPlan, 80, 24)
	press(a, "down", "p")
	c.t = c.t.Add(toastFor)
	os.WriteFile(path, []byte(specPlan), 0o644) // your agent's older copy
	later(a, c)
	c.t = c.t.Add(creditWait)
	a.tick()
	if got := a.notice().Text; !strings.HasPrefix(got, "The plan changed · ") {
		t.Fatalf("unclaimed after a second: %q", got)
	}
	logged := len(a.log)
	c.t = c.t.Add(100 * time.Millisecond) // the hook, 1.1 s after the read
	a.ingest([]watch.Event{at(c, 0, wrote(a.plans.Repo, path))})
	if got := a.notice().Text; got != "Your agent saved over your pin of agent-stream · p pins it again" {
		t.Fatalf("the late hook claims it: %q", got)
	}
	for _, ev := range a.log[logged:] {
		if ev.Kind == "plan" {
			t.Fatalf("AGENT records the change once: %+v", ev)
		}
	}
	// Once only, and never long after.
	b, c, path := openDemo(t, specPlan, 80, 24)
	os.WriteFile(path, []byte(strings.Replace(specPlan, `"title": "Plan list groups"`, `"title": "Plan list"`, 1)), 0o644)
	settled(b, c)
	c.t = c.t.Add(lateWait + time.Second)
	b.tick()
	b.ingest([]watch.Event{at(c, 0, wrote(b.plans.Repo, path))})
	if got := b.notice().Text; got != "" {
		t.Fatalf("a hook long after claims nothing: %q", got)
	}
}

// The fuzz's check itself: status values pass, anything else fails.
func TestSameButStatus(t *testing.T) {
	const base = `{"version": 1, "status": "pending", "tasks": [{"id": "a", "status": "active"}, {"id": "b"}]}`
	for _, c := range []struct {
		after string
		ok    bool
	}{
		{`{"version": 1, "status": "active", "tasks": [{"id": "a", "status": "pending"}, {"id": "b", "status": "done"}]}`, true},
		{base, true},
		{`{"version": 1, "status": "active", "tasks": [{"id": "a", "status": "pending"}, {"id": "b"}], "x": 1}`, false},
		{`{"version": 1, "tasks": [{"id": "a", "status": "active"}, {"id": "b"}]}`, false},
		{`{"version": 1, "status": "wip", "tasks": [{"id": "a", "status": "active"}, {"id": "b"}]}`, false},
		{`{"version": 1, "status": "pending", "tasks": [{"id": "a"}, {"id": "b"}]}`, false},
		{`{"version": 1, "status": "pending", "tasks": [{"id": "a", "status": "active", "title": "x"}, {"id": "b"}]}`, false},
		{`{"version": 2, "status": "pending", "tasks": [{"id": "a", "status": "active"}, {"id": "b"}]}`, false},
	} {
		if got := sameButStatus([]byte(base), []byte(c.after)); got != c.ok {
			t.Errorf("%s: %v", c.after, got)
		}
	}
	if sameButStatus([]byte(`{"tasks": []}`), []byte(`{"tasks": [], "status": "active"}`)) {
		t.Error("a status added at the root")
	}
}
