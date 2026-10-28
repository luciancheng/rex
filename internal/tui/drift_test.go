package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/plan"
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

// drifted is each row AGENT tags with drift, in order, as "<level>
// <what>": an edit's step (or the step Rex drew for it) by its path, a
// prompt by its text. It reads the tags as the stream does, from meta.
func drifted(log []watch.Event) []string {
	var out []string
	for _, ev := range log {
		level := driftOf(ev)
		if level == plan.OnPin {
			continue
		}
		what := ev.Text
		if rel, ok := ev.Meta["rel"].(string); ok {
			what = rel
		} else if _, path := stepText(ev); ev.Kind == "command" && path != "" {
			what = path
		}
		out = append(out, level.String()+" "+what)
	}
	return out
}

// docs/ux.md §4, §9a: hard drift is a notice on PLAN, "off pin" in the
// pin's yellow; every drift is a tag on its row on AGENT. None of it is
// ever written anywhere.
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
	// No step of these edits is on AGENT (the fixture reports only their
	// completion), so Rex draws one for each file, tagged: hard yellow, soft
	// muted. README.md's second edit in the turn tags the row its first
	// drew.
	if got := strings.Join(drifted(a.log), ", "); got != "hard README.md, soft internal/tui/live_view.go" {
		t.Fatalf("a tag per edit: %s", got)
	}
	view := BuildLogView(a.log, 78, viewOpt{tall: true})
	tagged := "outside card-highlight files · off pin"
	if v := viewText(view); !strings.Contains(v, "  edit README.md"+strings.Repeat(" ", 78-1-len("  edit README.md")-cellLen(tagged))+tagged+"\n") || !strings.Contains(v, "edit internal/tui/live_view.go") || !strings.Contains(v, "next to plans.go · off pin?\n") || strings.Contains(v, "rex ›") {
		t.Fatalf("each edit is a step row with why and its tag at the end:\n%s", v)
	}
	if hard, soft := view[0].Spans[len(view[0].Spans)-1], view[1].Spans[len(view[1].Spans)-1]; hard != (Span{Text: "off pin", Role: "accent"}) || soft != (Span{Text: "off pin?", Role: "muted"}) {
		t.Fatalf("hard is the pin's yellow, soft muted, never an error's red: %+v %+v", hard, soft)
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
	if got := strings.Join(drifted(a.log), ", "); strings.Contains(got, "demo.rex.json") {
		t.Fatalf("edits to plans never drift: %s", got)
	}
	if after, _ := os.ReadFile(path); string(after) != string(planBefore) {
		t.Fatal("drift never writes the plan")
	}
	if _, err := os.Stat(feed); !os.IsNotExist(err) {
		t.Fatal("drift never writes the feed")
	}
	// The agent pins another task: the notice clears, and the next edit is
	// judged against the new pin.
	moved := strings.Replace(strings.Replace(driftDemo, `"status": "active"`, `"status": "done"`, 1), `"title": "Outline",`, `"title": "Outline", "status": "active",`, 1)
	os.WriteFile(path, []byte(moved), 0o644)
	later(a, c)
	if s, ok := a.driftStatus(); ok {
		t.Fatal(s.Text)
	}
	tags := len(drifted(a.log))
	a.ingest([]watch.Event{edit(a, c, 9*time.Second, "README.md")})
	if got := a.statusLine().Text; got != "off pin · README.md · outside load-outline files" || len(drifted(a.log)) != tags+1 {
		t.Fatalf("%q %v", got, drifted(a.log))
	}
}

func TestDriftNeedsAPinAndLiveEvents(t *testing.T) {
	a, c, _ := openDemo(t, `{"tasks": [{"id": "a", "status": "done", "files": ["x.go"]}]}`, 120, 32)
	a.ingest([]watch.Event{edit(a, c, time.Second, "README.md")})
	if len(drifted(a.log)) != 0 {
		t.Fatal("no pin, no drift")
	}
	b, c2, _ := openDemo(t, driftDemo, 120, 32)
	b.warmStart([]watch.Event{edit(b, c2, -time.Minute, "README.md")}, c2.t)
	if len(drifted(b.log)) != 0 {
		t.Fatal("events from before Rex started never drift")
	}
	b.ingest([]watch.Event{at(c2, time.Second, watch.Event{Source: "claude", Kind: "result", Text: "Write failed", Meta: map[string]any{"tool": "Write", "path": filepath.Join(b.plans.Repo, "README.md"), "ok": false}})})
	if len(drifted(b.log)) != 0 {
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
	if got, _ := a.driftStatus(); got.Text != "off pin · go.mod · outside card-highlight files" {
		t.Fatalf("the second plan's pin judges its edits: %q", got.Text)
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
	if got := drifted(a.log); len(got) != 0 {
		t.Fatalf("the offered plan's work is not drift: %v", got)
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

// docs/ux.md §9a, §10: a prompt off the pin is tagged on its own first
// row, which always leaves the tag's room: the tag never moves a row.
func TestPromptDriftTagsItsRow(t *testing.T) {
	a, c, _ := openDemo(t, driftDemo, 120, 32)
	prompt := "now add three extra palettes for dark mode, and while you are at it make every one of them easy to pick"
	untagged := viewRows([]watch.Event{{Source: "claude", Kind: "user", Text: prompt}}, 78, viewOpt{tall: true})
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: prompt, Meta: map[string]any{"cwd": a.plans.Repo}})})
	if got := drifted(a.log); len(got) != 1 || !strings.HasPrefix(got[0], "hard now add three extra palettes") {
		t.Fatalf("the prompt's row is tagged: %v", got)
	}
	rows := viewRows(a.log, 78, viewOpt{tall: true})
	if len(rows) != len(untagged) || !strings.HasPrefix(rows[0], "› now add three extra palettes") || !strings.HasSuffix(rows[0], " off pin") || rows[1] != untagged[1] {
		t.Fatalf("tagged on its first row, the rest as it was:\n%s\n---\n%s", strings.Join(rows, "\n"), strings.Join(untagged, "\n"))
	}
	a.settle(70, 24)
	if !strings.Contains(a.statusLine().Short, "asks for extra palettes") {
		t.Fatalf("the short row 1 keeps the reason for a prompt: %q", a.statusLine().Short)
	}
}

// docs/ux.md §10: a drift tag lands on its row in place and never adds
// one, so a scrolled-up view keeps its lines wherever the tagged step is:
// far above it, its last row, or just under it. A step Rex draws for an
// edit AGENT shows none of lands under the view and keeps it too.
func TestDriftKeepsAScrolledUpViewStill(t *testing.T) {
	for _, where := range []string{"above", "its last row", "under it", "no step"} {
		a, c, _ := openDemo(t, driftDemo, 120, 20)
		press(a, "\t")
		filler := func(from, n int) {
			for i := 0; i < n; i++ {
				a.ingest([]watch.Event{at(c, time.Duration(from+i)*time.Second, watch.Event{Source: "claude", Kind: "text", Text: fmt.Sprintf("reply %d", from+i)})})
			}
		}
		filler(1, 60)
		a.ingest([]watch.Event{editStep(a, c, 100*time.Second, "e1", "README.md")})
		filler(200, 30)
		step := len(a.log) - 31
		under := a.logLines() - len(BuildLogView(a.log[:step+1], PaneTextWidth(a.columns, a.lines), a.viewOpt()))
		switch where {
		case "its last row":
			a.scroll = under
		case "under it":
			a.scroll = under + 1
		default:
			a.scroll = 5
		}
		a.settle(a.columns, a.lines)
		before := agentBody(a)
		done := editDone(a, c, 300*time.Second, "e1", "README.md")
		if where == "no step" {
			done = edit(a, c, 300*time.Second, "go.mod")
		}
		a.ingest([]watch.Event{done})
		// The tagged row gains its tag in place; every row stays where it was.
		untagged := func(rows []string) string {
			for i, r := range rows {
				if strings.HasSuffix(r, " off pin") {
					rows[i] = r[:2+strings.Index(r[2:], "  ")] // the step, without its reason and tag
				}
			}
			return strings.Join(rows, "\n")
		}
		if got := agentBody(a); untagged(got) != untagged(before) {
			t.Errorf("%s: the view moved:\n%s\n→\n%s", where, strings.Join(before, "\n"), strings.Join(got, "\n"))
		}
		want := "hard README.md"
		if where == "no step" {
			want = "hard go.mod"
		}
		if got := drifted(a.log); len(got) != 1 || got[0] != want {
			t.Errorf("%s: tagged %v, want %s", where, got, want)
		}
	}
}

// editStep and editDone are an edit as Claude Code reports it: the step
// (PreToolUse) and its completion (PostToolUse), one tool call.
func editStep(a *app, c *clock, d time.Duration, call, rel string) watch.Event {
	p := filepath.Join(a.plans.Repo, rel)
	return at(c, d, watch.Event{Source: "claude", Kind: "command", Text: "Edit " + p, Meta: map[string]any{"tool": "Edit", "path": p, "tool_use_id": call}})
}

func editDone(a *app, c *clock, d time.Duration, call, rel string) watch.Event {
	ev := edit(a, c, d, rel)
	ev.Meta["tool"], ev.Meta["tool_use_id"] = "Edit", call
	return ev
}

// docs/ux.md §9a, §10: drift is a tag on the edit's own step: the one
// with its tool call, else (no call named) the newest edit step of its
// file. A step of another call is never tagged for it, and only an edit
// with no step on AGENT gets a step drawn by Rex.
func TestDriftTagsTheEditsStep(t *testing.T) {
	a, c, _ := openDemo(t, driftDemo, 120, 32)
	press(a, "\t")
	a.ingest([]watch.Event{
		editStep(a, c, time.Second, "e1", "README.md"),
		editStep(a, c, 2*time.Second, "e2", "README.md"),
		at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "command", Text: "go test ./...", Meta: map[string]any{"tool": "Bash", "command": "go test ./..."}}),
	})
	a.ingest([]watch.Event{editDone(a, c, 4*time.Second, "e1", "README.md")})
	if got := drifted(a.log); len(got) != 1 || driftOf(a.log[0]) != plan.Hard || driftOf(a.log[1]) != plan.OnPin {
		t.Fatalf("the step of call e1, not e2's: %v", got)
	}
	rows := viewRows(a.log, 60, viewOpt{tall: true})
	tagged := "outside card-highlight files · off pin"
	if want := "  edit README.md" + strings.Repeat(" ", 60-1-len("  edit README.md")-cellLen(tagged)) + tagged; rows[0] != want || rows[1] != "  edit README.md" || rows[2] != "  $ go test ./..." {
		t.Fatalf("the tag at the step's end:\n%s", strings.Join(rows, "\n"))
	}
	// No call named: the newest edit step of the file, never a shell
	// command that names it.
	b, c2, _ := openDemo(t, driftDemo, 120, 32)
	p := filepath.Join(b.plans.Repo, "docs", "a.md")
	b.ingest([]watch.Event{
		at(c2, time.Second, watch.Event{Source: "cursor", Kind: "command", Text: "Write " + p, Meta: map[string]any{"tool": "Write", "path": p}}),
		at(c2, 2*time.Second, watch.Event{Source: "cursor", Kind: "command", Text: "cat " + p, Meta: map[string]any{"tool": "shell", "path": p}}),
	})
	b.ingest([]watch.Event{at(c2, 3*time.Second, watch.Event{Source: "cursor", Kind: "result", Text: "wrote " + p, Meta: map[string]any{"tool": "Write", "path": p}})})
	if got := drifted(b.log); len(got) != 1 || got[0] != "hard docs/a.md" || driftOf(b.log[0]) != plan.Hard {
		t.Fatalf("the Write step: %v", got)
	}
	// A Codex patch's step names all its files: it takes the worst tag.
	d, c3, _ := openDemo(t, driftDemo, 120, 32)
	repo := d.plans.Repo
	paths := []any{filepath.Join(repo, "internal/tui/live_view.go"), filepath.Join(repo, "README.md")}
	d.ingest([]watch.Event{at(c3, time.Second, watch.Event{Source: "codex", Kind: "command", Text: "Edit " + paths[0].(string) + " (+1 more)", Meta: map[string]any{"tool": "apply_patch", "path": paths[0], "paths": paths}})})
	d.ingest([]watch.Event{at(c3, 2*time.Second, watch.Event{Source: "codex", Kind: "result", Text: "wrote " + paths[0].(string), Meta: map[string]any{"tool": "apply_patch", "path": paths[0], "paths": paths, "ok": true}})})
	if got := drifted(d.log); len(got) != 1 || driftOf(d.log[0]) != plan.Hard {
		t.Fatalf("one step, its worst tag: %v", got)
	}
	if row := viewRows(d.log, 60, viewOpt{})[0]; !strings.HasPrefix(row, "  edit internal/tui/live_view.go (+1 more)") || !strings.HasSuffix(row, " off pin") {
		t.Fatalf("%q", row)
	}
}

// docs/ux.md §9a, §10: a completion's step is found in feed order, among
// the rows before it, though its whole batch is on AGENT when it is
// judged; with no tool-call ids (OpenCode's, Cursor's afterFileEdit) the
// newest step of its file that no earlier completion took. So two edits of
// one file in one batch each tag their own step, and an edit judged under
// the old pin never tags the step after the save that moved it.
func TestDriftTagsTheStepBeforeIt(t *testing.T) {
	step := func(a *app, c *clock, d time.Duration, rel string) watch.Event {
		p := filepath.Join(a.plans.Repo, rel)
		return at(c, d, watch.Event{Source: "opencode", Kind: "command", Text: "edit " + p, Meta: map[string]any{"tool": "edit", "path": p, "cwd": a.plans.Repo}})
	}
	done := func(a *app, c *clock, d time.Duration, rel string) watch.Event {
		p := filepath.Join(a.plans.Repo, rel)
		return at(c, d, watch.Event{Source: "opencode", Kind: "result", Text: "wrote " + p, Meta: map[string]any{"tool": "edit", "path": p, "ok": true, "cwd": a.plans.Repo}})
	}
	// Two edits of one file off the pin, in one batch: each its own step.
	a, c, _ := openDemo(t, driftDemo, 120, 32)
	// (5 s apart, or the feed's dedupe takes the second step for the first.)
	a.ingest([]watch.Event{step(a, c, time.Second, "README.md"), done(a, c, 2*time.Second, "README.md"), step(a, c, 7*time.Second, "README.md"), done(a, c, 8*time.Second, "README.md")})
	if len(a.log) != 2 || driftOf(a.log[0]) != plan.Hard || driftOf(a.log[1]) != plan.Hard {
		t.Fatalf("each step tagged: %v (%d rows)", drifted(a.log), len(a.log))
	}
	// An edit judged under the old pin, the save that moves the pin onto
	// its file, and an edit of it under the new pin, in one batch: the
	// first step is soft, the second on the pin.
	b, c2, path := cujApp(t)
	data, _ := os.ReadFile(path)
	moved := mustReplace(t, string(data), `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],`)
	moved = mustReplace(t, moved, `"status": "pending",
      "depends_on": ["card-highlight"],`, `"status": "active",
      "depends_on": ["card-highlight"],`)
	os.WriteFile(path, []byte(moved), 0o644)
	outline := "internal/tui/outline.go" // listed for load-outline
	b.ingest([]watch.Event{
		step(b, c2, time.Second, outline),
		done(b, c2, 2*time.Second, outline),
		at(c2, 3*time.Second, watch.Event{Source: "opencode", Kind: "result", Text: "wrote " + path, Meta: map[string]any{"tool": "write", "path": path, "ok": true, "cwd": b.plans.Repo}}),
		step(b, c2, 8*time.Second, outline),
		done(b, c2, 9*time.Second, outline),
	})
	var steps []watch.Event
	for _, e := range b.log {
		if e.Kind == "command" {
			steps = append(steps, e)
		}
	}
	if len(steps) != 2 || driftOf(steps[0]) != plan.Soft || driftOf(steps[1]) != plan.OnPin {
		t.Fatalf("the soft tag on the first step only: %v", drifted(b.log))
	}
}

// docs/ux.md §9a: a completion reported twice (Cursor's postToolUse, then
// its afterFileEdit with no call id, seconds apart) is one edit: it tags
// the step the first took, or the step Rex drew for it, and draws no
// second. A new turn's edit of the file is a new row.
func TestARepeatedCompletionIsOneEdit(t *testing.T) {
	a, c, _ := openDemo(t, driftDemo, 120, 32)
	p := filepath.Join(a.plans.Repo, "README.md")
	post := func(d time.Duration, call string) watch.Event {
		meta := map[string]any{"tool": "Write", "path": p, "cwd": a.plans.Repo}
		if call != "" {
			meta["tool_use_id"] = call
		}
		return at(c, d, watch.Event{Source: "cursor", Kind: "result", Text: "wrote " + p, Meta: meta})
	}
	a.ingest([]watch.Event{post(time.Second, "x1")})
	a.ingest([]watch.Event{post(6*time.Second, "")})
	if got := drifted(a.log); len(a.log) != 1 || len(got) != 1 || got[0] != "hard README.md" {
		t.Fatalf("one row Rex drew: %v (%d rows)", got, len(a.log))
	}
	a.ingest([]watch.Event{at(c, 7*time.Second, watch.Event{Source: "cursor", Kind: "user", Text: "and again", Meta: map[string]any{"cwd": a.plans.Repo}})})
	a.ingest([]watch.Event{post(8*time.Second, "")})
	if got := drifted(a.log); len(got) != 2 || got[1] != "hard README.md" {
		t.Fatalf("a new turn's edit is a new row: %v", got)
	}
	// With its step on AGENT, the repeat tags that step and draws nothing.
	b, c2, _ := openDemo(t, driftDemo, 120, 32)
	b.ingest([]watch.Event{at(c2, time.Second, watch.Event{Source: "cursor", Kind: "command", Text: "Write " + filepath.Join(b.plans.Repo, "README.md"), Meta: map[string]any{"tool": "Write", "path": filepath.Join(b.plans.Repo, "README.md"), "tool_use_id": "x1", "cwd": b.plans.Repo}})})
	b.ingest([]watch.Event{editDone(b, c2, 2*time.Second, "x1", "README.md")})
	b.ingest([]watch.Event{edit(b, c2, 7*time.Second, "README.md")})
	if got := drifted(b.log); len(b.log) != 1 || len(got) != 1 || driftOf(b.log[0]) != plan.Hard {
		t.Fatalf("the step, tagged once: %v (%d rows)", got, len(b.log))
	}
}

// docs/ux.md §9a, §10: AGENT draws a tag, and its reason, only from the
// meta Rex itself puts on a row, never from text: a feed line that says
// "off pin" or reads like a reason, or one whose meta claims a tag, a
// reason or a row of Rex's, draws none, and a feed line that claims to be
// Rex's own drawn step is dropped.
func TestDriftTagsNeverComeFromText(t *testing.T) {
	a, c, _ := openDemo(t, driftDemo, 120, 32)
	press(a, "\t")
	var feed []watch.Event
	for _, raw := range []string{
		`{"source":"claude","kind":"command","text":"echo off pin","meta":{"tool":"Bash","rex.drift":2}}`,
		`{"source":"claude","kind":"command","text":"Edit docs/a.md outside card-highlight files · off pin","meta":{"tool":"Edit","path":"docs/a.md","rex.drift":{"level":2,"why":"evil"}}}`,
		`{"source":"claude","kind":"command","text":"Edit README.md off pin","meta":{"tool":"Edit","rex.drift":"hard","rex.row":1}}`,
		`{"source":"claude","kind":"user","text":"hard README.md · off pin?","meta":{"rex.drift":2,"rex.row":1}}`,
		`{"source":"rex","kind":"drift","text":"hard README.md","meta":{"rex.drift":2,"rel":"README.md"}}`,
		`{"source":"claude","kind":"drift","text":"off pin","meta":{"rel":"README.md"}}`,
	} {
		var ev watch.Event
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			t.Fatal(err)
		}
		ev = at(c, time.Duration(len(feed)+1)*time.Second, ev)
		feed = append(feed, ev)
	}
	a.ingest(feed)
	if len(a.log) != 4 {
		t.Fatalf("Rex's own kinds never come from the feed: %d events", len(a.log))
	}
	if got := drifted(a.log); len(got) != 0 {
		t.Fatalf("tags from the feed: %v", got)
	}
	a.enabled = true
	for _, line := range a.logView(78) {
		for _, sp := range line.Spans {
			if sp.Role == "accent" || strings.Contains(sp.Text, "evil") || sp.Role == "muted" && strings.HasSuffix(sp.Text, whySep) {
				t.Fatalf("a tag or a reason from the feed: %+v", line)
			}
		}
	}
	// A step Rex tagged reads its reason from the tag: whatever its own
	// text says stays its text.
	ev := watch.Event{Kind: "command", Text: "Edit README.md names evil, not the pin", Meta: map[string]any{"tool": "Edit", "path": "/r/README.md", driftKey: driftTag{plan.Soft, "next to plans.go"}}}
	row := BuildLogView([]watch.Event{ev}, 80, viewOpt{})[0]
	if n := len(row.Spans); n < 3 || row.Spans[n-2] != (Span{Text: "next to plans.go" + whySep, Role: "muted"}) || strings.Count(row.Text, "evil") != 1 {
		t.Fatalf("the judge's reason: %+v", row.Spans)
	}
}

// docs/ux.md §9a: a patch's step names its first file. It takes its
// worst file's tag and reason, and a reason that isn't the shown file's
// says whose it is: never a reason that is false for the file on the row.
func TestAPatchsStepSaysWhoseReason(t *testing.T) {
	for _, tc := range []struct {
		files []string
		want  string
	}{
		// The shown file is on the pin; README.md is off it.
		{[]string{"internal/tui/plans.go", "README.md"}, "README.md: outside card-highlight files · off pin"},
		// Both off it, the shown one first: its own reason, no name.
		{[]string{"internal/palettes/x.go", "README.md"}, ""},
	} {
		a, c, _ := openDemo(t, driftDemo, 120, 32)
		var paths []any
		for _, f := range tc.files {
			paths = append(paths, filepath.Join(a.plans.Repo, f))
		}
		first := paths[0].(string)
		a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "codex", Kind: "command", Text: "Edit " + first + " (+1 more)", Meta: map[string]any{"tool": "apply_patch", "path": first, "paths": paths}})})
		a.ingest([]watch.Event{at(c, 2*time.Second, watch.Event{Source: "codex", Kind: "result", Text: "wrote " + first, Meta: map[string]any{"tool": "apply_patch", "path": first, "paths": paths, "ok": true}})})
		want := tc.want
		if want == "" {
			own := plan.CheckEdit(a.doc.plan, a.doc.plan.Graph().Pin(), tc.files[0], foldPaths)
			if own.Level != plan.Hard || strings.Contains(own.Reason, "README") {
				t.Fatalf("setup: %+v", own)
			}
			want = own.Reason + " · off pin"
		}
		if len(a.log) != 1 || tagOf(a.log[0]).level != plan.Hard {
			t.Fatalf("%v: one step, hard: %v", tc.files, drifted(a.log))
		}
		row := viewRows(a.log, 100, viewOpt{})[0]
		if !strings.HasPrefix(row, "  edit "+tc.files[0]+" (+1 more)  ") || !strings.HasSuffix(row, " "+want) {
			t.Errorf("%v: %q, want it to end %q", tc.files, row, want)
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
	// With no step on AGENT, each off-pin file gets a step of Rex's own.
	if got := strings.Join(drifted(a.log), ", "); got != "soft internal/tui/live_view.go, hard README.md" {
		t.Fatalf("every off-pin file of the patch is tagged: %s", got)
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
	if got := a.statusLine().Text; got != "Your agent finished card-highlight ✔ and pinned load-outline" {
		t.Fatalf("row 1 after the batch: %q", got)
	}
	later(a, c)
	c.t = c.t.Add(toastFor)
	if got := a.statusLine().Text; got != "pinned  load-outline · Outline view" {
		t.Fatalf("row 1: %q", got)
	}
	if got := drifted(a.log); len(got) != 0 {
		t.Fatalf("no drift: %v", got)
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
		if got := a.statusLine().Text; !strings.HasPrefix(got, "Your agent finished card-highlight ✔") {
			t.Fatalf("patch=%v: row 1 after the batch: %q", patch, got)
		}
		later(a, c)
		c.t = c.t.Add(toastFor)
		if got := a.statusLine().Text; strings.HasPrefix(got, offPin) {
			t.Fatalf("patch=%v: row 1: %q", patch, got)
		}
		if got := drifted(a.log); len(got) != 0 {
			t.Fatalf("patch=%v: no drift: %v", patch, got)
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
	if got := a.statusLine().Text; strings.HasPrefix(got, offPin) || len(drifted(a.log)) != 0 {
		t.Fatalf("held while the turn runs: %q", got)
	}
	switchTo(t, a, c, path, "passkey-ui", "recovery", 2*time.Second)
	a.ingest([]watch.Event{at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "Started recovery."})})
	later(a, c)
	c.t = c.t.Add(toastFor)
	if got := a.statusLine().Text; got != "pinned  recovery · Recovery codes" {
		t.Fatalf("row 1 %q", got)
	}
	if got := drifted(a.log); len(got) != 0 {
		t.Fatalf("the switch left drift: %v", got)
	}
}

// A prompt that names another task is still drift when its turn ends with
// the pin where it was, or moved somewhere else. It is soft, so only its
// tag on AGENT shows it, and no notice (docs/ux.md §4, §9a).
func TestNamingAnotherTaskDriftsWhenThePinStays(t *testing.T) {
	a, c, path := openDemo(t, switchDemo, 120, 32)
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "start recovery"})})
	a.ingest([]watch.Event{at(c, 2*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "Let's finish the passkey button first."})})
	if s := a.drift.soft; s == nil || s.prompt != "start recovery" || a.notice().Text != "" {
		t.Fatalf("the turn ended on the old pin: soft drift, on no row: %+v %q", s, a.notice().Text)
	}
	if got := drifted(a.log); len(got) != 1 || got[0] != "soft start recovery" {
		t.Fatalf("recorded on its prompt's row: %v", got)
	}
	if row := viewRows(a.log, 100, viewOpt{tall: true})[0]; !strings.HasPrefix(row, "› start recovery ") || !strings.HasSuffix(row, " off pin?") {
		t.Fatalf("%q", row)
	}

	// The agent starts a third task instead: judged against that pin at the
	// turn's end.
	a.ingest([]watch.Event{at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "user", Text: "now do recovery"})})
	switchTo(t, a, c, path, "passkey-ui", "audit-log", 4*time.Second)
	if got := a.statusLine().Text; strings.HasPrefix(got, offPin) || len(a.drift.held) == 0 {
		t.Fatalf("still held under the new pin: %q", got)
	}
	a.ingest([]watch.Event{at(c, 5*time.Second, watch.Event{Source: "claude", Kind: "idle"})})
	if got := drifted(a.log); len(got) != 2 || got[1] != "soft now do recovery" {
		t.Fatalf("judged against the new pin at the turn's end: %v", got)
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
			if got := a.statusLine().Text; strings.HasPrefix(got, offPin) || len(drifted(a.log)) != 0 {
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
		got := strings.Join(drifted(a.log), ", ")
		if how == "no switch" {
			if got != "soft start recovery, soft internal/recovery/codes.go" {
				t.Fatalf("%s: the turn ended on the old pin: both are drift: %s", how, got)
			}
			continue
		}
		if row1 := a.statusLine().Text; row1 != "pinned  recovery · Recovery codes" {
			t.Fatalf("%s: row 1 %q", how, row1)
		}
		if got != "" {
			t.Fatalf("%s: the switch left drift: %s", how, got)
		}
	}

	// An edit of another task's file that no prompt asked for is drift at
	// once, as before: soft, so on AGENT only.
	a, c, _ := openDemo(t, demo, 120, 32)
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "start audit-log"})})
	a.ingest([]watch.Event{edit(a, c, 2*time.Second, "internal/recovery/codes.go")})
	if s, got := a.drift.soft, drifted(a.log); s == nil || s.rel != "internal/recovery/codes.go" || a.notice().Text != "" || len(got) != 1 || got[0] != "soft internal/recovery/codes.go" {
		t.Fatalf("soft drift at once, on AGENT only: notice %q, %v", a.notice().Text, got)
	}
}
