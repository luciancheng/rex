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
	ev := gather(evs, a.scope, c.t, "card-highlight", nil)
	if strings.Join(ev.files, ",") != "internal/tui/plans.go" {
		t.Fatalf("only this repo's edits since the pin, never rex/plans: %q", ev.files)
	}
	if ev.test != "go test ./internal/tui/ (passed)" || ev.did != "the bar spans all three card rows" || ev.verified != "go test ./internal/tui/ passed" {
		t.Fatalf("%+v", ev)
	}
	other := at(c, 7*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "rex-sendoff: rex/plans/cuj/demo.rex.json#load-outline\naccomplished: something else"})
	if ev := gather(append(evs, other), a.scope, c.t, "card-highlight", nil); ev.did != "the bar spans all three card rows" {
		t.Fatalf("another task's reply never lands on this one: %q", ev.did)
	}
	if ev := gather(evs, a.scope, time.Time{}, "card-highlight", nil); len(ev.files) != 0 {
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
	if after, _ := os.ReadFile(path); string(after) != string(before) || a.overlay[PanePlans] != overlayNone {
		t.Fatal("Esc writes nothing")
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
