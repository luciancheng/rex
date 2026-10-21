package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// at stamps an event at the test clock plus d.
func at(c *clock, d time.Duration, ev watch.Event) watch.Event {
	ev.TS = float64(c.t.Add(d).UnixNano()) / 1e9
	return ev
}

func workEvents(a *app, c *clock) []watch.Event {
	repo := a.plans.Repo
	return []watch.Event{
		at(c, time.Second, watch.Event{Source: "claude", Kind: "command", Text: "go test ./internal/tui/", Meta: map[string]any{"tool": "Bash"}}),
		at(c, 2*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + filepath.Join(repo, "internal/tui/plans.go"), Meta: map[string]any{"tool": "Write", "path": filepath.Join(repo, "internal/tui/plans.go"), "ok": true}}),
		at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + filepath.Join(repo, "rex/plans/cuj/demo.rex.json"), Meta: map[string]any{"tool": "Write", "path": filepath.Join(repo, "rex/plans/cuj/demo.rex.json"), "ok": true}}),
		at(c, 4*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "ran go test ./internal/tui/", Meta: map[string]any{"tool": "Bash", "command": "go test ./internal/tui/", "ok": true}}),
		at(c, 5*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "Done.\n\naccomplished: the bar spans all three card rows\nverified: go test ./internal/tui/ passed"}),
	}
}

func TestGatherEvidence(t *testing.T) {
	a, c, _ := openDemo(t, demoPlan, 120, 32)
	evs := append([]watch.Event{at(c, -time.Hour, watch.Event{Source: "claude", Kind: "result", Text: "wrote old.go", Meta: map[string]any{"tool": "Write", "path": filepath.Join(a.plans.Repo, "old.go")}})}, workEvents(a, c)...)
	evs = append(evs, at(c, 6*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote /elsewhere/x.go", Meta: map[string]any{"tool": "Write", "path": "/elsewhere/x.go"}}))
	ev := gather(evs, a.scope, c.t, "", "card-highlight", nil)
	if strings.Join(ev.files, ",") != "internal/tui/plans.go" {
		t.Fatalf("only this repo's edits since the pin, never rex/plans: %q", ev.files)
	}
	if ev.test != "go test ./internal/tui/ (passed)" || ev.did != "the bar spans all three card rows" || ev.verified != "go test ./internal/tui/ passed" {
		t.Fatalf("%+v", ev)
	}
	other := at(c, 7*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "rex-sendoff: rex/plans/cuj/demo.rex.json#load-outline\naccomplished: something else"})
	if ev := gather(append(evs, other), a.scope, c.t, "", "card-highlight", nil); ev.did != "the bar spans all three card rows" {
		t.Fatalf("another task's reply never lands on this one: %q", ev.did)
	}
	if ev := gather(evs, a.scope, time.Time{}, "", "card-highlight", nil); len(ev.files) != 0 {
		t.Fatal("no pin start, no evidence")
	}
}

func TestDoneFromTheTrailer(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	a.ingest(workEvents(a, c))
	press(a, "\t")
	typeText(a, "d")
	press(a, "\r")
	if a.overlay[PaneChat] != overlayDone {
		t.Fatalf("d⏎ opens the completion form: %q", a.statusLine().Text)
	}
	frame := a.frame()
	for _, want := range []string{"── done · card-highlight", "intended", "Bar, border, and fill", "internal/tui/plans.go", "(this session) · may be partial", "agent says: the bar spans all three card rows", "done · ⏎ save · esc cancel · ↑↓ field", "did ›"} {
		if !strings.Contains(frame, want) {
			t.Errorf("missing %q\n%s", want, frame)
		}
	}
	press(a, "\r")
	p, err := plan.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ch := p.Task("card-highlight")
	if ch.Status != plan.Done || ch.Accomplished[0] != "the bar spans all three card rows" || ch.VerifiedHow[0] != "go test ./internal/tui/ passed" || strings.Join(ch.Modified, ",") != "internal/tui/plans.go" {
		t.Fatalf("%+v", ch)
	}
	if a.overlay[PaneChat] != overlayNone || a.statusLine().Text != "✓ card-highlight done · next up load-outline · p⏎ pins it" {
		t.Fatalf("%q", a.statusLine().Text)
	}
	if p.Task("load-outline").Status != plan.Pending {
		t.Fatal("nothing is auto-pinned")
	}
	logs := len(a.log)
	later(a, c)
	if len(a.log) != logs {
		t.Fatal("Rex's own write is quiet")
	}
}

func TestDoneTypedAndCancelled(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 80, 24)
	before, _ := os.ReadFile(path)
	typeText(a, "d")
	press(a, "\r")
	press(a, "\r")
	if a.statusLine().Text != "did › is required: what was actually done" {
		t.Fatalf("no trailer, nothing typed: %q", a.statusLine().Text)
	}
	typeText(a, "shipped it")
	press(a, "down")
	typeText(a, "looked at it")
	press(a, "up")
	if a.draft != "shipped it" {
		t.Fatal("each field keeps its value")
	}
	press(a, "esc")
	if a.overlay[PanePlans] != overlayDone || a.draft != "" {
		t.Fatal("the first Esc clears the field, like every draft")
	}
	press(a, "esc")
	if after, _ := os.ReadFile(path); string(after) != string(before) || a.overlay[PanePlans] != overlayNone {
		t.Fatal("Esc on an empty field cancels and writes nothing")
	}
	typeText(a, "d")
	press(a, "\r")
	typeText(a, "shipped it")
	// Someone edits the task while the form is open.
	os.WriteFile(path, []byte(strings.Replace(demoPlan, `"pick / pickbg colors exist"`, `"colors exist"`, 1)), 0o644)
	press(a, "\r")
	if !strings.Contains(a.statusLine().Text, "card-highlight changed on disk · ⏎ to save anyway") {
		t.Fatal(a.statusLine().Text)
	}
	press(a, "\r")
	if p, _ := plan.Load(path); p.Task("card-highlight").Status != plan.Done || p.Task("card-highlight").Needs[0] != "colors exist" {
		t.Fatal("the second ⏎ saves on top of the new file")
	}
}

func TestDoneWithoutAPin(t *testing.T) {
	a, _, _ := openDemo(t, `{"tasks": [{"id": "a", "status": "done"}]}`, 120, 32)
	typeText(a, "d")
	press(a, "\r")
	if a.statusLine().Text != "no pin · p⏎ a task first" || a.overlay[PanePlans] != overlayNone {
		t.Fatal(a.statusLine().Text)
	}
}

func TestDoneFormBelongsToItsPlan(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 120, 32)
	press(a, "\t")
	typeText(a, "d")
	press(a, "\r")
	typeText(a, "did the A thing")
	other := strings.Replace(path, "demo.rex.json", "other.rex.json", 1)
	os.WriteFile(other, []byte(demoPlan), 0o644)
	press(a, "\t")
	a.plans.Current = other
	a.openedPlan()
	if a.overlay[PaneChat] == overlayDone || a.form[PaneChat] != nil {
		t.Fatal("opening another plan closes a form opened for the old one")
	}
	if got, _ := os.ReadFile(other); string(got) != demoPlan {
		t.Fatal("the other plan is untouched")
	}
}

func TestEvidenceRestartsWhenThePinComesBack(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	a.notePin() // card-highlight is the pin
	c.t = c.t.Add(time.Minute)
	// The agent moves the pin to load-outline, works on it, then puts it back.
	moved := strings.Replace(strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"]`, `"status": "pending",
      "depends_on": ["plans-ui"]`, 1), `"status": "pending",
      "depends_on": ["card-highlight"]`, `"status": "active",
      "depends_on": ["card-highlight"]`, 1)
	os.WriteFile(path, []byte(moved), 0o644)
	later(a, c)
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "text", Text: "accomplished: the outline view", Meta: map[string]any{"cwd": a.plans.Repo}})})
	c.t = c.t.Add(time.Minute)
	os.WriteFile(path, []byte(demoPlan), 0o644)
	later(a, c)
	press(a, "\t")
	typeText(a, "d")
	press(a, "\r")
	if f := a.form[PaneChat]; f == nil || f.id != "card-highlight" || strings.Contains(f.hints[0], "outline view") {
		t.Fatalf("another task's trailer from before the pin came back is no hint: %+v", f)
	}
}

func TestEvidenceReadsMarkdownAndTrustsOnlyRealResults(t *testing.T) {
	a, c, _ := openDemo(t, demoPlan, 120, 32)
	repo := a.plans.Repo
	run := func(d time.Duration, cmd string, ok bool) []watch.Event {
		return []watch.Event{
			at(c, d, watch.Event{Source: "claude", Kind: "command", Text: cmd, Meta: map[string]any{"tool": "Bash"}}),
			at(c, d+time.Millisecond, watch.Event{Source: "claude", Kind: "result", Text: "ran " + cmd, Meta: map[string]any{"tool": "Bash", "command": cmd, "ok": ok}}),
		}
	}
	evs := run(time.Second, "cd "+repo+string(filepath.Separator)+"internal && go test ./... 2>&1 | tail -20", true)
	evs = append(evs, run(2*time.Second, "ls", true)...)
	evs = append(evs, at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "Done, see `rex-sendoff: rex/plans/cuj/demo.rex.json#card-highlight`.\n\n**Accomplished:** the bar spans all three rows\n- **verified**: go test passed"}))
	ev := gather(evs, a.scope, c.t, "rex/plans/cuj/demo.rex.json", "card-highlight", []string{"Task details render on narrow terminals"})
	if ev.did != "the bar spans all three rows" || ev.verified != "go test passed" {
		t.Fatalf("bold trailers, and the task's own fingerprint before punctuation: %+v", ev)
	}
	if strings.Contains(ev.test, "ls") || !strings.Contains(ev.test, "(result not seen)") || strings.Contains(ev.test, repo) {
		t.Fatalf("a piped test isn't 'passed', ls isn't the verify, and no machine path: %q", ev.test)
	}
	otherPlan := at(c, 4*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "rex-sendoff: rex/plans/other.rex.json#card-highlight\naccomplished: wrong plan"})
	if ev := gather(append(evs, otherPlan), a.scope, c.t, "rex/plans/cuj/demo.rex.json", "card-highlight", nil); ev.did == "wrong plan" {
		t.Fatal("a reply about another plan's task of the same id doesn't count")
	}
}

func TestDoneFormFitsNarrowAndShortScreens(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 80, 24)
	typeText(a, "d")
	press(a, "\r")
	if frame := a.frame(); !strings.Contains(frame, "may be partial") {
		t.Fatalf("the files are always labelled partial at 80×24\n%s", frame)
	}
	a.settle(80, 12)
	if frame := a.frame(); !strings.Contains(frame, "▌ did ›") {
		t.Fatalf("the focused field stays on screen at 80×12\n%s", frame)
	}
}

func TestDoneKeepsModifiedItCantRead(t *testing.T) {
	a, c, path := openDemo(t, strings.Replace(demoPlan, `"files": ["internal/tui/plans.go"]`, `"files": ["internal/tui/plans.go"],
      "modified": ["kept.go", {"path": "agent-wrote-this.go"}]`, 1), 120, 32)
	a.seen = []watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote x", Meta: map[string]any{"tool": "Write", "path": filepath.Join(a.plans.Repo, "new.go"), "ok": true}})}
	typeText(a, "d")
	press(a, "\r")
	if f := a.form[PanePlans]; f == nil || strings.Join(f.files, ",") != "new.go" {
		t.Fatalf("the edit is seen: %+v", f)
	}
	typeText(a, "shipped")
	press(a, "\r")
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `{"path": "agent-wrote-this.go"}`) || strings.Contains(string(got), `"new.go"`) {
		t.Fatalf("a list Rex can't read is left as it is:\n%s", got)
	}
}

func TestEvidenceSurvivesLookingAtAnotherPlan(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	a.ingest(workEvents(a, c))
	other := strings.Replace(path, "demo.rex.json", "other.rex.json", 1)
	os.WriteFile(other, []byte(demoPlan), 0o644)
	c.t = c.t.Add(time.Minute)
	a.plans.Current = other
	a.openedPlan()
	a.plans.Current = path
	a.openedPlan()
	typeText(a, "d")
	press(a, "\r")
	if f := a.form[PanePlans]; f == nil || strings.Join(f.files, ",") != "internal/tui/plans.go" || !strings.Contains(f.hints[0], "the bar spans all three card rows") {
		t.Fatalf("opening another plan and coming back isn't the pin moving: %+v", f)
	}
}

func TestEvidenceFromBeforeRexStarted(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	a.feed = filepath.Join(t.TempDir(), "session.jsonl")
	for _, ev := range workEvents(a, c) {
		meta := map[string]any{"cwd": a.plans.Repo}
		for k, v := range ev.Meta {
			meta[k] = v
		}
		ev.Meta = meta
		watch.AppendEvent(a.feed, ev)
	}
	plan.UpdateState(a.statePath, c.t, func(s *plan.State) { s.Pins[plan.PinKey(path, "card-highlight")] = float64(c.t.Unix()) })
	a.started = c.t.Add(time.Hour) // Rex started after the work
	c.t = c.t.Add(2 * time.Hour)
	a.pinSeen = map[string]time.Time{plan.PinKey(path, "card-highlight"): c.t} // this session saw it only now
	a.pinMoved = map[string]time.Time{}
	typeText(a, "d")
	press(a, "\r")
	if f := a.form[PanePlans]; f == nil || strings.Join(f.files, ",") != "internal/tui/plans.go" || !strings.Contains(f.hints[0], "the bar spans all three card rows") {
		t.Fatalf("a pin started before Rex reads back in the feed: %+v", f)
	}
}

func TestSavingDropsTheOtherFormsText(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	typeText(a, "d")
	press(a, "\r") // a form in PLAN
	typeText(a, "half typed")
	press(a, "\t")
	typeText(a, "d")
	press(a, "\r") // the same task's form on the tape
	typeText(a, "shipped")
	press(a, "\r")
	press(a, "\t")
	if a.draft != "" {
		t.Fatalf("the other place's form closed, and its text with it: %q", a.draft)
	}
}

func TestClosingAFormElsewhereDropsWhatWasTyped(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 120, 32)
	press(a, "\t")
	typeText(a, "d")
	press(a, "\r")
	typeText(a, "did the A thing")
	press(a, "\t")
	other := strings.Replace(path, "demo.rex.json", "other.rex.json", 1)
	os.WriteFile(other, []byte(demoPlan), 0o644)
	a.plans.Current = other
	a.openedPlan()
	press(a, "\t")
	if a.draft != "" {
		t.Fatalf("the closed form's text doesn't land in the tape's composer: %q", a.draft)
	}
}

func TestSavingClosesTheOtherPlacesForm(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	typeText(a, "d")
	press(a, "\r") // a form in PLAN
	press(a, "\t")
	typeText(a, "d")
	press(a, "\r") // and one on the tape, same task
	typeText(a, "shipped")
	press(a, "\r")
	if a.form[PanePlans] != nil || a.overlay[PanePlans] == overlayDone {
		t.Fatal("the other place's form for the same task closes once it's done")
	}
}
