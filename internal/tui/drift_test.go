package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/watch"
)

const driftDemo = `{
  "title": "Drift demo",
  "scope": {"out": ["extra palettes"]},
  "tasks": [
    {"id": "card-highlight", "title": "Card", "status": "active", "files": ["internal/tui/plans.go", "internal/tui/live.go"]},
    {"id": "load-outline", "title": "Outline", "files": ["internal/tui/outline.go"]}
  ]
}`

func edit(a *app, c *clock, d time.Duration, rel string) watch.Event {
	p := filepath.Join(a.plans.Repo, rel)
	return at(c, d, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + p, Meta: map[string]any{"tool": "Write", "path": p, "ok": true}})
}

func TestDriftIsShownNeverSent(t *testing.T) {
	a, c, path := openDemo(t, driftDemo, 120, 32)
	a.feed = filepath.Join(t.TempDir(), "session.jsonl")
	planBefore, _ := os.ReadFile(path)
	a.ingest([]watch.Event{edit(a, c, time.Second, "README.md")})
	if got := a.statusLine().Text; got != "drift · hard: README.md · outside card-highlight files" {
		t.Fatalf("row 1 %q", got)
	}
	a.ingest([]watch.Event{edit(a, c, 2*time.Second, "internal/tui/live_view.go"), edit(a, c, 3*time.Second, "README.md")})
	view := viewText(BuildLogView(a.log, 78))
	if strings.Count(view, "── drift · card-highlight") != 1 || strings.Count(view, "README.md") != 1 ||
		!strings.Contains(view, "hard README.md (outside card-highlight files)") || !strings.Contains(view, "soft internal/tui/live_view.go (next to plans.go)") {
		t.Fatalf("one block per pin, each path once:\n%s", view)
	}
	a.ingest([]watch.Event{edit(a, c, 4*time.Second, "internal/tui/plans.go")})
	if !strings.HasPrefix(a.statusLine().Text, "drift · hard: README.md") {
		t.Fatal("an on-pin edit clears a soft notice, not a hard one")
	}
	a.ingest([]watch.Event{edit(a, c, 5*time.Second, "internal/tui/live_extra.go"), edit(a, c, 6*time.Second, "internal/tui/live.go")})
	if got := a.statusLine().Text; got != "drift · hard: README.md · outside card-highlight files" {
		t.Fatalf("back on the pin clears the soft notice; the unresolved hard one shows again: %q", got)
	}
	a.ingest([]watch.Event{at(c, 7*time.Second, watch.Event{Source: "claude", Kind: "user", Text: "now add three extra palettes for dark mode"})})
	if got := a.statusLine().Text; got != "drift · hard: now add three extra palettes for dark mode · asks for extra palettes, which is out of scope" {
		t.Fatalf("%q", got)
	}
	a.ingest([]watch.Event{edit(a, c, 8*time.Second, "rex/plans/cuj/demo.rex.json")})
	if strings.Contains(viewText(BuildLogView(a.log, 78)), "demo.rex.json (") {
		t.Fatal("edits to plans never drift")
	}
	if after, _ := os.ReadFile(path); string(after) != string(planBefore) {
		t.Fatal("drift never writes the plan")
	}
	if _, err := os.Stat(a.feed); !os.IsNotExist(err) {
		t.Fatal("drift never writes the feed")
	}
	// A new pin clears row 1 and starts a new block.
	typeText(a, "pin load-outline")
	press(a, "\r")
	typeText(a, "p")
	press(a, "\r")
	if strings.HasPrefix(a.statusLine().Text, "drift") {
		t.Fatal(a.statusLine().Text)
	}
	a.ingest([]watch.Event{edit(a, c, 9*time.Second, "README.md")})
	if v := viewText(BuildLogView(a.log, 78)); !strings.Contains(v, "── drift · load-outline") || strings.Count(v, "── drift ·") != 2 {
		t.Fatalf("%s", v)
	}
}

func TestDriftNeedsAPinAndLiveEvents(t *testing.T) {
	a, c, _ := openDemo(t, `{"tasks": [{"id": "a", "status": "done", "files": ["x.go"]}]}`, 120, 32)
	a.ingest([]watch.Event{edit(a, c, time.Second, "README.md")})
	if len(a.drift.items) != 0 {
		t.Fatal("no pin, no drift")
	}
	b, c2, _ := openDemo(t, driftDemo, 120, 32)
	b.warmStart([]watch.Event{edit(b, c2, -time.Minute, "README.md")}, c2.t)
	if len(b.drift.items) != 0 {
		t.Fatal("events from before Rex started never drift")
	}
	b.ingest([]watch.Event{at(c2, time.Second, watch.Event{Source: "claude", Kind: "result", Text: "Write failed", Meta: map[string]any{"tool": "Write", "path": filepath.Join(b.plans.Repo, "README.md"), "ok": false}})})
	if len(b.drift.items) != 0 {
		t.Fatal("a denied write didn't happen")
	}
}

func TestDriftBelongsToItsPlanAndStint(t *testing.T) {
	a, c, path := openDemo(t, driftDemo, 120, 32)
	a.ingest([]watch.Event{edit(a, c, time.Second, "README.md")})
	// Another plan whose pin has the same id: none of the first plan's drift.
	other := strings.Replace(path, "demo.rex.json", "other.rex.json", 1)
	os.WriteFile(other, []byte(driftDemo), 0o644)
	a.plans.Current = other
	a.openedPlan()
	if strings.HasPrefix(a.statusLine().Text, "drift") {
		t.Fatalf("another plan doesn't inherit drift: %q", a.statusLine().Text)
	}
	if !a.ingest([]watch.Event{edit(a, c, 2*time.Second, "go.mod")}) {
		t.Fatal("a drifting edit repaints at once")
	}
	if n := strings.Count(viewText(BuildLogView(a.log, 78)), "── drift ·"); n != 2 {
		t.Fatalf("the second plan's drift gets its own block, %d blocks", n)
	}
}

func TestDriftClearsWhenThePlanIsWidened(t *testing.T) {
	a, c, path := openDemo(t, driftDemo, 120, 32)
	a.ingest([]watch.Event{edit(a, c, time.Second, "README.md")})
	os.WriteFile(path, []byte(strings.Replace(driftDemo, `"files": ["internal/tui/plans.go"`, `"files": ["README.md", "internal/tui/plans.go"`, 1)), 0o644)
	later(a, c)
	if s, ok := a.driftStatus(); ok {
		t.Fatalf("README.md is listed now: %q", s.Text)
	}
}

func TestDriftBlockSitsBelowWhatItJudges(t *testing.T) {
	a, c, _ := openDemo(t, driftDemo, 120, 32)
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "now add three extra palettes for dark mode", Meta: map[string]any{"cwd": a.plans.Repo}})})
	view := viewText(BuildLogView(a.log, 78))
	if p, d := strings.Index(view, "extra palettes for dark"), strings.Index(view, "── drift"); p < 0 || d < p {
		t.Fatalf("the drift block comes after the prompt it is about\n%s", view)
	}
	a.settle(70, 24)
	if !strings.Contains(a.statusLine().Short, "asks for extra palettes") {
		t.Fatalf("the short row 1 keeps the reason for a prompt: %q", a.statusLine().Short)
	}
}
