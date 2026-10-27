package tui

import (
	"fmt"
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

// at stamps an event at the test clock plus d.
func at(c *clock, d time.Duration, ev watch.Event) watch.Event {
	ev.TS = float64(c.t.Add(d).UnixNano()) / 1e9
	return ev
}

func edit(a *app, c *clock, d time.Duration, rel string) watch.Event {
	p := filepath.Join(a.plans.Repo, rel)
	return at(c, d, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + p, Meta: map[string]any{"tool": "Write", "path": p, "ok": true}})
}

// docs/ux.md §4, §9a: hard drift is a notice on PLAN, "off pin" in the
// pin's yellow; every drift is in AGENT's block. None of it is ever
// written anywhere.
func TestDriftIsShownNeverSent(t *testing.T) {
	a, c, path := openDemo(t, driftDemo, 120, 32)
	feed := watch.DefaultEventPath() // under the test's HOME
	planBefore, _ := os.ReadFile(path)
	a.ingest([]watch.Event{edit(a, c, time.Second, "README.md")})
	if got := a.statusLine().Text; got != "off pin · README.md · outside card-highlight files" {
		t.Fatalf("notice %q", got)
	}
	if s := a.statusLine(); s.Kind == "error" || s.Spans(s.Text)[0].Text != "off pin" || s.Spans(s.Text)[0].Role != "accent" {
		t.Fatalf("hard drift is off the pin, so the pin's yellow, never an error's red: %q %+v", s.Kind, s.Spans(s.Text))
	}
	a.ingest([]watch.Event{edit(a, c, 2*time.Second, "internal/tui/live_view.go"), edit(a, c, 3*time.Second, "README.md")})
	view := viewText(BuildLogView(a.log, 78, viewOpt{tall: true}))
	if strings.Count(view, "rex › drift · card-highlight") != 1 || strings.Count(view, "README.md") != 1 ||
		!strings.Contains(view, "hard README.md · outside card-highlight files") || !strings.Contains(view, "soft internal/tui/live_view.go · next to plans.go") {
		t.Fatalf("one block per pin, each path once:\n%s", view)
	}
	a.ingest([]watch.Event{at(c, 3500*time.Millisecond, watch.Event{Source: "claude", Kind: "user", Text: "ok continue"})})
	if a.drift.soft == nil || !strings.HasPrefix(a.statusLine().Text, "off pin · README.md") {
		t.Fatal("a short prompt on the pin clears nothing, the soft drift included")
	}
	a.ingest([]watch.Event{edit(a, c, 4*time.Second, "internal/tui/plans.go")})
	if a.drift.soft != nil || !strings.HasPrefix(a.statusLine().Text, "off pin · README.md") {
		t.Fatal("an on-pin edit clears soft drift, not hard")
	}
	a.ingest([]watch.Event{edit(a, c, 5*time.Second, "internal/tui/live_extra.go")})
	if a.drift.soft == nil || a.statusLine().Text != "off pin · README.md · outside card-highlight files" {
		t.Fatalf("soft drift is AGENT's only: the hard notice stays: %q", a.statusLine().Text)
	}
	a.ingest([]watch.Event{edit(a, c, 6*time.Second, "internal/tui/live.go")})
	if got := a.statusLine().Text; a.drift.soft != nil || got != "off pin · README.md · outside card-highlight files" {
		t.Fatalf("back on the pin clears the soft drift; the unresolved hard notice stays: %q", got)
	}
	a.ingest([]watch.Event{at(c, 7*time.Second, watch.Event{Source: "claude", Kind: "user", Text: "now add three extra palettes for dark mode"})})
	if got := a.statusLine().Text; got != "off pin · now add three extra palettes for dark mode · asks for extra palettes, which is out of scope" {
		t.Fatalf("%q", got)
	}
	a.ingest([]watch.Event{edit(a, c, 8*time.Second, "rex/plans/cuj/demo.rex.json")})
	if strings.Contains(viewText(BuildLogView(a.log, 78, viewOpt{tall: true})), "demo.rex.json ·") {
		t.Fatal("edits to plans never drift")
	}
	if after, _ := os.ReadFile(path); string(after) != string(planBefore) {
		t.Fatal("drift never writes the plan")
	}
	if _, err := os.Stat(feed); !os.IsNotExist(err) {
		t.Fatal("drift never writes the feed")
	}
	// The agent pins another task: the notice clears and a new block starts.
	moved := strings.Replace(strings.Replace(driftDemo, `"status": "active"`, `"status": "done"`, 1), `"title": "Outline",`, `"title": "Outline", "status": "active",`, 1)
	os.WriteFile(path, []byte(moved), 0o644)
	later(a, c)
	if s, ok := a.driftStatus(); ok {
		t.Fatal(s.Text)
	}
	a.ingest([]watch.Event{edit(a, c, 9*time.Second, "README.md")})
	if v := viewText(BuildLogView(a.log, 78, viewOpt{tall: true})); !strings.Contains(v, "rex › drift · load-outline") || strings.Count(v, "rex › drift ·") != 2 {
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
	if strings.HasPrefix(a.statusLine().Text, offPin) {
		t.Fatalf("another plan doesn't inherit drift: %q", a.statusLine().Text)
	}
	if !a.ingest([]watch.Event{edit(a, c, 2*time.Second, "go.mod")}) {
		t.Fatal("a drifting edit repaints at once")
	}
	if n := strings.Count(viewText(BuildLogView(a.log, 78, viewOpt{tall: true})), "rex › drift ·"); n != 2 {
		t.Fatalf("the second plan's drift gets its own block, %d blocks", n)
	}
}

// docs/ux.md §9a, §11: while a latched plan offers the one the agent
// wrote, the agent is working on that plan. Its edits and prompts are not
// drift from the latched pin, so they never take the notice row from the
// offer (§4).
func TestTheOfferedPlansWorkIsNotDrift(t *testing.T) {
	a, c, path := openDemo(t, driftDemo, 120, 32)
	repo := a.plans.Repo
	other := filepath.Join(a.plans.Root, "a.rex.json")
	if err := os.WriteFile(other, []byte(`{"tasks": [{"id": "render", "title": "Render", "status": "active", "files": ["src/a.go"]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a.ingest([]watch.Event{at(c, time.Second, wrote(repo, other))})
	c.t = c.t.Add(toastFor)
	a.ingest([]watch.Event{
		edit(a, c, 2*time.Second, "src/a.go"),
		at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "user", Text: "now add three extra palettes for dark mode"}),
	})
	if got := a.statusLine().Text; got != "Your agent wrote a · o opens it" {
		t.Fatalf("the notice keeps the offer: %q", got)
	}
	if len(a.drift.items) != 0 {
		t.Fatalf("the offered plan's work is not drift: %v", a.drift.items)
	}
	for _, ev := range a.log {
		if ev.Kind == "drift" {
			t.Fatalf("no drift block on the tape: %q", ev.Text)
		}
	}
	if press(a, "o"); a.plans.Current != other || !a.follow || a.offer != "" {
		t.Fatalf("o switches: current=%q (was %q) follow=%v", a.plans.Current, path, a.follow)
	}
	c.t = c.t.Add(toastFor) // it said what opened
	a.ingest([]watch.Event{edit(a, c, 4*time.Second, "README.md")})
	if got := a.statusLine().Text; got != "off pin · README.md · outside render files" {
		t.Fatalf("following again, edits are judged against the new pin: %q", got)
	}
}

func TestDriftClearsWhenThePlanIsWidened(t *testing.T) {
	a, c, path := openDemo(t, driftDemo, 120, 32)
	a.ingest([]watch.Event{edit(a, c, time.Second, "README.md")})
	os.WriteFile(path, []byte(strings.Replace(driftDemo, `"files": ["internal/tui/plans.go"`, `"files": ["README.md", "internal/tui/plans.go"`, 1)), 0o644)
	later(a, c)
	if s, ok := a.driftStatus(); ok || a.drift.hard != nil || a.drift.soft != nil {
		t.Fatalf("README.md is listed now: %q", s.Text)
	}
}

func TestDriftBlockSitsBelowWhatItJudges(t *testing.T) {
	a, c, _ := openDemo(t, driftDemo, 120, 32)
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "now add three extra palettes for dark mode", Meta: map[string]any{"cwd": a.plans.Repo}})})
	view := viewText(BuildLogView(a.log, 78, viewOpt{tall: true}))
	if p, d := strings.Index(view, "extra palettes for dark"), strings.Index(view, "rex › drift"); p < 0 || d < p {
		t.Fatalf("the drift block comes after the prompt it is about\n%s", view)
	}
	a.settle(70, 24)
	if !strings.Contains(a.statusLine().Short, "asks for extra palettes") {
		t.Fatalf("the short row 1 keeps the reason for a prompt: %q", a.statusLine().Short)
	}
}

func TestDriftKeepsAScrolledUpViewStill(t *testing.T) {
	for _, where := range []string{"above", "below"} {
		a, c, _ := openDemo(t, driftDemo, 120, 20)
		press(a, "\t")
		filler := func(from, n int) {
			for i := 0; i < n; i++ {
				a.ingest([]watch.Event{at(c, time.Duration(from+i)*time.Second, watch.Event{Source: "claude", Kind: "text", Text: fmt.Sprintf("reply %d", from+i)})})
			}
		}
		filler(1, 60)
		a.ingest([]watch.Event{edit(a, c, 100*time.Second, "README.md")}) // the block, then more below it
		filler(200, 30)
		if where == "above" {
			a.scroll = 5 // the block is far above the view
		} else {
			for i, e := range a.log {
				if e.Kind == "drift" {
					a.scroll = len(BuildLogView(a.log[i+1:], PaneTextWidth(a.columns, a.lines), a.viewOpt())) + 10 // the view ends just above the block
				}
			}
		}
		top := a.frameRows().Rows[3]
		a.ingest([]watch.Event{edit(a, c, 300*time.Second, "go.mod")}) // the block gains a row
		if got := a.frameRows().Rows[3]; got != top {
			t.Errorf("%s: the view moved: %q → %q", where, top, got)
		}
	}
}

// The drift block's last row is the bottom row of a scrolled-up view (or
// one row either side of it), and the blocks under it pack: the block
// gains a row in place and the view keeps its lines when that row lands
// under it (docs/ux.md §10).
func TestDriftBlockAtTheViewBottom(t *testing.T) {
	for _, off := range []int{0, 1} {
		a, c, _ := openDemo(t, driftDemo, 120, 20)
		press(a, "\t")
		for i := 0; i < 40; i++ {
			a.ingest([]watch.Event{at(c, time.Duration(1+i)*time.Second, watch.Event{Source: "claude", Kind: "text", Text: fmt.Sprintf("reply %d", i)})})
		}
		a.ingest([]watch.Event{edit(a, c, 100*time.Second, "README.md")})
		for i := 0; i < 20; i++ {
			cmd := fmt.Sprintf("go test %d", i)
			a.ingest([]watch.Event{
				at(c, time.Duration(200+2*i)*time.Second, watch.Event{Source: "claude", Kind: "command", Text: cmd, Meta: map[string]any{"tool": "Bash", "command": cmd}}),
				at(c, time.Duration(201+2*i)*time.Second, watch.Event{Source: "claude", Kind: "error", Text: "Bash failed · " + cmd + ": exit status 1", Meta: map[string]any{"tool": "Bash", "command": cmd}}),
			})
		}
		for i, e := range a.log {
			if e.Kind == "drift" {
				if v := BuildLogView(a.log[i:i+2], PaneTextWidth(a.columns, a.lines), a.viewOpt()); v[len(v)-1].Kind == "gap" {
					t.Fatal("setup: the block under the drift packs")
				}
				a.scroll = len(BuildLogView(a.log[i+1:], PaneTextWidth(a.columns, a.lines), a.viewOpt())) + off
			}
		}
		a.settle(a.columns, a.lines)
		before := agentBody(a)
		a.ingest([]watch.Event{edit(a, c, 300*time.Second, "go.mod")})
		if got := agentBody(a); strings.Join(got, "\n") != strings.Join(before, "\n") {
			t.Errorf("the drift block %d row(s) under the view bottom gained a row and moved it:\n%s\n→\n%s", off, strings.Join(before, "\n"), strings.Join(got, "\n"))
		}
	}
}

func TestOnePatchManyFiles(t *testing.T) {
	a, c, _ := openDemo(t, driftDemo, 120, 32)
	repo := a.plans.Repo
	// the on-pin file last, so it mustn't clear what the others raised
	paths := []any{filepath.Join(repo, "README.md"), filepath.Join(repo, "internal/tui/live_view.go"), filepath.Join(repo, "internal/tui/plans.go")}
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "codex", Kind: "result", Text: "wrote " + paths[0].(string), Meta: map[string]any{"tool": "apply_patch", "path": paths[0], "paths": paths, "ok": true}})})
	if got := a.statusLine().Text; !strings.HasPrefix(got, "off pin · README.md") || a.drift.soft == nil {
		t.Fatalf("one patch's files are judged worst last, wherever they sit in it: %q", got)
	}
	if view := viewText(BuildLogView(a.log, 78, viewOpt{tall: true})); !strings.Contains(view, "hard README.md") || !strings.Contains(view, "soft internal/tui/live_view.go") {
		t.Fatalf("every off-pin file of the patch is in the block:\n%s", view)
	}
}

// One batch both saves the plan (the pin moves on) and edits a file of
// the new pin: a parallel tool call, or a Codex patch touching both. The
// edit is judged against the plan it was written with, so it isn't drift
// (docs/ux.md §9a).
func TestABatchThatMovesThePinIsJudgedAgainstTheNewPin(t *testing.T) {
	a, c, path := cujApp(t)
	repo := a.plans.Repo
	data, _ := os.ReadFile(path)
	moved := mustReplace(t, string(data), `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],`)
	moved = mustReplace(t, moved, `"status": "pending",
      "depends_on": ["card-highlight"],`, `"status": "active",
      "depends_on": ["card-highlight"],`)
	os.WriteFile(path, []byte(moved), 0o644)
	code := filepath.Join(repo, "internal", "tui", "outline.go") // listed for load-outline
	a.ingest([]watch.Event{
		at(c, 0, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + path, Meta: map[string]any{"tool": "Write", "path": path, "ok": true, "cwd": repo}}),
		at(c, 0, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + code, Meta: map[string]any{"tool": "Edit", "path": code, "ok": true, "cwd": repo}}),
	})
	if got := a.statusLine().Text; got != "✔ card-highlight done · now load-outline" {
		t.Fatalf("row 1 after the batch: %q", got)
	}
	later(a, c)
	c.t = c.t.Add(toastFor)
	if got := a.statusLine().Text; got != "pinned  load-outline · Outline view" {
		t.Fatalf("row 1: %q", got)
	}
	if view := viewText(BuildLogView(a.log, 78, viewOpt{tall: true})); strings.Contains(view, "rex › drift") {
		t.Fatalf("no drift block\n%s", view)
	}
}

// The converse: the finished task's last edit and the save that marks it
// done share a batch, in that order or as one patch. The edit was made
// under the old pin, so it isn't drift either (docs/ux.md §9a).
func TestTheFinishedTasksLastEditIsNotDrift(t *testing.T) {
	for _, patch := range []bool{false, true} {
		a, c, path := cujApp(t)
		repo := a.plans.Repo
		data, _ := os.ReadFile(path)
		done := mustReplace(t, string(data), `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],`)
		os.WriteFile(path, []byte(done), 0o644)
		code := filepath.Join(repo, "internal", "tui", "live.go") // listed for card-highlight only
		evs := []watch.Event{
			at(c, 0, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + code, Meta: map[string]any{"tool": "Edit", "path": code, "ok": true, "cwd": repo}}),
			at(c, 0, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + path, Meta: map[string]any{"tool": "Write", "path": path, "ok": true, "cwd": repo}}),
		}
		if patch {
			evs = []watch.Event{at(c, 0, watch.Event{Source: "codex", Kind: "result", Text: "wrote " + code, Meta: map[string]any{"tool": "apply_patch", "path": code, "paths": []any{code, path}, "ok": true, "cwd": repo}})}
		}
		a.ingest(evs)
		if got := a.statusLine().Text; !strings.HasPrefix(got, "✔ card-highlight done") {
			t.Fatalf("patch=%v: row 1 after the batch: %q", patch, got)
		}
		later(a, c)
		c.t = c.t.Add(toastFor)
		if got := a.statusLine().Text; strings.HasPrefix(got, offPin) {
			t.Fatalf("patch=%v: row 1: %q", patch, got)
		}
		if view := viewText(BuildLogView(a.log, 78, viewOpt{tall: true})); strings.Contains(view, "rex › drift") {
			t.Fatalf("patch=%v: no drift block\n%s", patch, view)
		}
	}
}

const switchDemo = `{"version":1,"id":"auth","title":"Login with passkeys","goal":"Users sign in with a passkey.","status":"active","tasks":[
 {"id":"passkey-ui","title":"Passkey button","status":"active","files":["web/signin/"]},
 {"id":"recovery","title":"Recovery codes","status":"pending"},
 {"id":"audit-log","title":"Audit log","status":"pending"}]}`

// switchTo is the agent starting id as the skill teaches: it active, the
// old pin pending, saved with its file tool.
func switchTo(t *testing.T, a *app, c *clock, path, from, id string, d time.Duration) {
	t.Helper()
	data, _ := os.ReadFile(path)
	s := setStatus(setStatus(string(data), from, "pending"), id, "active")
	os.WriteFile(path, []byte(s), 0o644)
	a.ingest([]watch.Event{at(c, d, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + path, Meta: map[string]any{"tool": "Edit", "path": path, "ok": true}})})
}

// setStatus sets task id's status in a one-line task object of src.
func setStatus(src, id, status string) string {
	s := src
	i := strings.Index(s, `"id":"`+id+`"`)
	j := i + strings.Index(s[i:], `"status":"`) + len(`"status":"`)
	k := j + strings.Index(s[j:], `"`)
	return s[:j] + status + s[k:]
}

// Asking the agent to start another task is how the pin moves now: the
// prompt names that task, and once the agent makes it the pin, it was a
// switch, not drift (docs/ux.md §9a).
func TestAskingTheAgentToSwitchTasksIsNotDrift(t *testing.T) {
	a, c, path := openDemo(t, switchDemo, 120, 32)
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "start recovery"})})
	if got := a.statusLine().Text; strings.HasPrefix(got, offPin) || len(a.drift.items) != 0 {
		t.Fatalf("held while the turn runs: %q", got)
	}
	switchTo(t, a, c, path, "passkey-ui", "recovery", 2*time.Second)
	a.ingest([]watch.Event{at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "Started recovery."})})
	later(a, c)
	c.t = c.t.Add(toastFor)
	if got := a.statusLine().Text; got != "pinned  recovery · Recovery codes" {
		t.Fatalf("row 1 %q", got)
	}
	if tape := viewText(BuildLogView(a.log, 100, viewOpt{tall: true})); strings.Contains(tape, "rex › drift") {
		t.Fatalf("the switch left a drift block\n%s", tape)
	}
}

// A prompt that names another task is still drift when its turn ends with
// the pin where it was, or moved somewhere else. It is soft, so AGENT's
// block shows it and no row does (docs/ux.md §4, §9a).
func TestNamingAnotherTaskDriftsWhenThePinStays(t *testing.T) {
	a, c, path := openDemo(t, switchDemo, 120, 32)
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "start recovery"})})
	a.ingest([]watch.Event{at(c, 2*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "Let's finish the passkey button first."})})
	if s := a.drift.soft; s == nil || s.prompt != "start recovery" || a.notice().Text != "" {
		t.Fatalf("the turn ended on the old pin: soft drift, on no row: %+v %q", s, a.notice().Text)
	}
	if tape := viewText(BuildLogView(a.log, 100, viewOpt{tall: true})); !strings.Contains(tape, "rex › drift · passkey-ui") || !strings.Contains(tape, "soft start recovery · names recovery, not the pin") {
		t.Fatalf("recorded as before\n%s", tape)
	}

	// The agent starts a third task instead: judged against that pin at the
	// turn's end.
	a.ingest([]watch.Event{at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "user", Text: "now do recovery"})})
	switchTo(t, a, c, path, "passkey-ui", "audit-log", 4*time.Second)
	if got := a.statusLine().Text; strings.HasPrefix(got, offPin) || len(a.drift.held) == 0 {
		t.Fatalf("still held under the new pin: %q", got)
	}
	a.ingest([]watch.Event{at(c, 5*time.Second, watch.Event{Source: "claude", Kind: "idle"})})
	if tape := viewText(BuildLogView(a.log, 100, viewOpt{tall: true})); !strings.Contains(tape, "rex › drift · audit-log") || !strings.Contains(tape, "soft now do recovery · names recovery, not the pin") {
		t.Fatalf("the new pin's block\n%s", tape)
	}
}

// The agent asked to start another task may edit that task's files before
// it saves the plan, in its own batch or in the save's. Those edits wait
// with the prompt and go when the save makes that task the pin; if the
// turn ends with the pin where it was, both are drift (docs/ux.md §9a).
func TestAnEditBeforeTheSwitchIsNotDrift(t *testing.T) {
	demo := mustReplace(t, switchDemo, `"title":"Recovery codes","status":"pending"`, `"title":"Recovery codes","status":"pending","files":["internal/recovery/"]`)
	for _, how := range []string{"apart", "one batch", "no switch"} {
		a, c, path := openDemo(t, demo, 120, 32)
		a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "start recovery"})})
		code := edit(a, c, 2*time.Second, "internal/recovery/codes.go")
		switch how {
		case "apart":
			a.ingest([]watch.Event{code})
			if got := a.statusLine().Text; strings.HasPrefix(got, offPin) || len(a.drift.items) != 0 {
				t.Fatalf("%s: the edit waits with the prompt: %q", how, got)
			}
			switchTo(t, a, c, path, "passkey-ui", "recovery", 3*time.Second)
		case "one batch":
			data, _ := os.ReadFile(path)
			os.WriteFile(path, []byte(setStatus(setStatus(string(data), "passkey-ui", "pending"), "recovery", "active")), 0o644)
			a.ingest([]watch.Event{code, at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + path, Meta: map[string]any{"tool": "Edit", "path": path, "ok": true}})})
		default:
			a.ingest([]watch.Event{code})
		}
		a.ingest([]watch.Event{at(c, 4*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "Done."})})
		later(a, c)
		c.t = c.t.Add(toastFor)
		tape := viewText(BuildLogView(a.log, 100, viewOpt{tall: true}))
		if how == "no switch" {
			if !strings.Contains(tape, "rex › drift · passkey-ui") || !strings.Contains(tape, "soft start recovery · names recovery, not the pin") || !strings.Contains(tape, "soft internal/recovery/codes.go · listed for recovery") {
				t.Fatalf("%s: the turn ended on the old pin: both are drift\n%s", how, tape)
			}
			continue
		}
		if got := a.statusLine().Text; got != "pinned  recovery · Recovery codes" {
			t.Fatalf("%s: row 1 %q", how, got)
		}
		if strings.Contains(tape, "rex › drift") {
			t.Fatalf("%s: the switch left a drift block\n%s", how, tape)
		}
	}

	// An edit of another task's file that no prompt asked for is drift at
	// once, as before: soft, so on AGENT only.
	a, c, _ := openDemo(t, demo, 120, 32)
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "start audit-log"})})
	a.ingest([]watch.Event{edit(a, c, 2*time.Second, "internal/recovery/codes.go")})
	tape := viewText(BuildLogView(a.log, 100, viewOpt{tall: true}))
	if s := a.drift.soft; s == nil || s.rel != "internal/recovery/codes.go" || a.notice().Text != "" || !strings.Contains(tape, "soft internal/recovery/codes.go · listed for recovery") {
		t.Fatalf("soft drift at once, on AGENT only: notice %q\n%s", a.notice().Text, tape)
	}
}
