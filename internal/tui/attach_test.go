package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// attachRepo is an empty git repo with its own ~/.rex/state.json, so what
// start reads and writes there is this test's alone.
func attachRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

// written stamps each writePlan file a second after the last, so "newest"
// never rests on a filesystem's clock tick (NTFS's is about 15 ms).
var written = time.Now().Add(-time.Hour).Truncate(time.Second)

// writePlan writes a plan file under repo's rex/plans; each one is newer
// than the last.
func writePlan(t *testing.T, repo, name, body string) string {
	t.Helper()
	path := filepath.Join(PlansDir(repo), filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	written = written.Add(time.Second)
	if err := os.Chtimes(path, written, written); err != nil {
		t.Fatal(err)
	}
	return path
}

func startIn(repo string) *app {
	return newApp("yellow", false, "idle", NewPlanBrowser(repo), 120, 32)
}

// wrote is a completed Write of path, as Claude Code reports it.
func wrote(repo, path string) watch.Event {
	return watch.Event{Source: "claude", Kind: "result", Text: "wrote " + path, Meta: map[string]any{"tool": "Write", "path": path, "ok": true, "cwd": repo}}
}

// hook runs one raw hook payload through rex ingest and reads it back from
// the feed, as the live tail would.
func hook(t *testing.T, source string, payload map[string]any) []watch.Event {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	if err := watch.RunIngest(strings.NewReader(string(data)), source, feed); err != nil {
		t.Fatal(err)
	}
	return watch.LastEvents(feed, 10)
}

// docs/ux.md §11, §14: opening the plan you had last time takes no keys.
func TestStartOpensTheLastPlan(t *testing.T) {
	repo := attachRepo(t)
	last := writePlan(t, repo, "cuj/last.rex.json", demoPlan)
	writePlan(t, repo, "newer.rex.json", demoPlan)
	now := time.Now()
	if err := plan.UpdateState(plan.StatePath(), now, func(s *plan.State) {
		s.Repos[repo] = plan.RepoState{Plan: last, Opened: float64(now.Unix())}
	}); err != nil {
		t.Fatal(err)
	}
	a := startIn(repo)
	if a.pane != PanePlans || a.at != atOutline || a.plans.Current != last || !a.follow || a.outline.sel != "card-highlight" {
		t.Fatalf("at=%d current=%q follow=%v sel=%q", a.at, a.plans.Current, a.follow, a.outline.sel)
	}
	if !strings.Contains(a.frame(), "idle · following · rex/plans/cuj/last.rex.json") {
		t.Fatalf("%s", a.frame())
	}
}

// A state.json entry that is gone, or not one of this repo's plans, falls
// through to the newest plan, then to the wait screen.
func TestStartIgnoresAStateEntryItCantUse(t *testing.T) {
	repo := attachRepo(t)
	outside := filepath.Join(repo, "docs", "elsewhere.rex.json")
	os.MkdirAll(filepath.Dir(outside), 0o755)
	os.WriteFile(outside, []byte(demoPlan), 0o644)
	sep := string(filepath.Separator)
	climbs := PlansDir(repo) + sep + ".." + sep + ".." + sep + "docs" + sep + "elsewhere.rex.json" // a real "..", not cleaned away
	for _, last := range []string{filepath.Join(PlansDir(repo), "gone.rex.json"), outside, climbs, "rex/plans/relative.rex.json"} {
		state, _ := json.Marshal(map[string]any{"version": 1, "repos": map[string]any{repo: map[string]any{"plan": last, "opened": time.Now().Unix()}}})
		os.MkdirAll(filepath.Dir(plan.StatePath()), 0o700)
		os.WriteFile(plan.StatePath(), state, 0o600)
		if a := startIn(repo); a.at != atWait || a.plans.Current != "" {
			t.Fatalf("%s: at=%d current=%q", last, a.at, a.plans.Current)
		}
	}
	newest := writePlan(t, repo, "newest.rex.json", demoPlan)
	state, _ := json.Marshal(map[string]any{"version": 1, "repos": map[string]any{repo: map[string]any{"plan": outside, "opened": time.Now().Unix()}}})
	os.WriteFile(plan.StatePath(), state, 0o600)
	if a := startIn(repo); a.at != atOutline || a.plans.Current != newest {
		t.Fatalf("at=%d current=%q", a.at, a.plans.Current)
	}
}

func TestStartAttachesTheNewestPlan(t *testing.T) {
	repo := attachRepo(t)
	writePlan(t, repo, "old.rex.json", demoPlan)
	newest := writePlan(t, repo, "team/new.rex.json", demoPlan)
	writePlan(t, repo, "notes.md", "not a plan")
	a := startIn(repo)
	if a.at != atOutline || a.plans.Current != newest || !a.follow {
		t.Fatalf("at=%d current=%q follow=%v", a.at, a.plans.Current, a.follow)
	}
	if st := plan.LoadState(plan.StatePath()); st.Repos[repo].Plan != newest {
		t.Fatalf("the attached plan is the repo's last plan: %+v", st.Repos)
	}
}

// Each harness reports a write its own way; every one of them opens the
// plan, and the screen changes only in PLAN.
func TestEveryHarnessWriteOpensThePlan(t *testing.T) {
	cases := []struct {
		source, name string
		payload      func(repo, path string) map[string]any
	}{
		{"claude", "claude.rex.json", func(repo, path string) map[string]any {
			return map[string]any{"hook_event_name": "PostToolUse", "session_id": "s1", "cwd": repo, "tool_name": "Write", "tool_input": map[string]any{"file_path": path, "content": "{}"}, "tool_response": map[string]any{"type": "create"}}
		}},
		{"claude", "edited.rex.json", func(repo, path string) map[string]any {
			return map[string]any{"hook_event_name": "PostToolUse", "session_id": "s1", "cwd": repo, "tool_name": "Edit", "tool_input": map[string]any{"file_path": path, "old_string": "a", "new_string": "b"}}
		}},
		{"cursor", "cursor.rex.json", func(repo, path string) map[string]any {
			return map[string]any{"hook_event_name": "afterFileEdit", "conversation_id": "c1", "workspace_roots": []any{repo}, "file_path": path, "edits": []any{}}
		}},
		{"codex", "team/codex.rex.json", func(repo, path string) map[string]any {
			return map[string]any{"hook_event_name": "PostToolUse", "turn_id": "t1", "session_id": "s1", "cwd": repo, "tool_name": "apply_patch",
				"tool_input":    map[string]any{"command": "*** Begin Patch\n*** Update File: internal/x.go\n*** Add File: rex/plans/team/codex.rex.json\n+{}\n*** End Patch"},
				"tool_response": "Exit code: 0\nWall time: 0.1 seconds\nOutput:\nSuccess."}
		}},
		{"opencode", "opencode.rex.json", func(repo, path string) map[string]any {
			return map[string]any{"type": "message.part.updated", "cwd": repo, "part": map[string]any{"type": "tool", "tool": "write", "callID": "c1", "state": map[string]any{"status": "completed", "input": map[string]any{"filePath": "rex/plans/opencode.rex.json"}}}}
		}},
	}
	for _, tc := range cases {
		repo := attachRepo(t)
		a := startIn(repo)
		press(a, "\t")
		typeText(a, "a note in progress")
		path := writePlan(t, repo, tc.name, demoPlan)
		evs := hook(t, tc.source, tc.payload(repo, path))
		if len(evs) == 0 {
			t.Fatalf("%s: nothing reached the feed", tc.source)
		}
		a.ingest(evs)
		if a.plans.Current != path || a.at != atOutline || !a.follow {
			t.Fatalf("%s %s: current=%q at=%d", tc.source, tc.name, a.plans.Current, a.at)
		}
		if a.pane != PaneChat || a.draft != "a note in progress" {
			t.Fatalf("%s: an attach moved you or ate the tape's draft: pane=%d draft=%q", tc.source, a.pane, a.draft)
		}
		if !strings.Contains(a.statusLine().Text, "following "+planStem(tc.name)) {
			t.Fatalf("%s: row 1 says why the plan changed: %q", tc.source, a.statusLine().Text)
		}
		if last := a.log[len(a.log)-1]; last.Source != "rex" || last.Kind != "plan" || last.Text != "following "+planStem(tc.name) {
			t.Fatalf("%s: the tape keeps a record of the move: %+v", tc.source, last)
		}
	}
}

// Reads, failed writes, other files and other repos never open a plan.
func TestOnlyACompletedPlanWriteInThisRepoAttaches(t *testing.T) {
	repo := attachRepo(t)
	other := t.TempDir()
	os.Mkdir(filepath.Join(other, ".git"), 0o755)
	plan := filepath.Join(PlansDir(repo), "real.rex.json")
	theirs := filepath.Join(PlansDir(other), "theirs.rex.json")
	notes := filepath.Join(PlansDir(repo), "notes.md")
	docs := filepath.Join(repo, "docs", "x.rex.json")
	link := filepath.Join(PlansDir(repo), "link.rex.json")
	type check struct {
		name string
		evs  func() []watch.Event
	}
	claude := func(m map[string]any) func() []watch.Event {
		return func() []watch.Event { return hook(t, "claude", m) }
	}
	checks := []check{
		{"read", claude(map[string]any{"hook_event_name": "PostToolUse", "cwd": repo, "tool_name": "Read", "tool_input": map[string]any{"file_path": plan}})},
		{"cursor read", func() []watch.Event {
			return hook(t, "cursor", map[string]any{"hook_event_name": "postToolUse", "workspace_roots": []any{repo}, "tool_name": "Read", "tool_input": map[string]any{"path": plan}})
		}},
		{"about to write", claude(map[string]any{"hook_event_name": "PreToolUse", "cwd": repo, "tool_name": "Write", "tool_input": map[string]any{"file_path": plan}})},
		{"failed write", claude(map[string]any{"hook_event_name": "PostToolUseFailure", "cwd": repo, "tool_name": "Write", "tool_input": map[string]any{"file_path": plan}, "error": "denied"})},
		{"failed patch", func() []watch.Event {
			return hook(t, "codex", map[string]any{"hook_event_name": "PostToolUse", "turn_id": "t", "cwd": repo, "tool_name": "apply_patch", "tool_input": map[string]any{"command": "*** Begin Patch\n*** Update File: rex/plans/real.rex.json\n*** End Patch"}, "tool_response": "Failed to read file to update"})
		}},
		{"ok false", func() []watch.Event {
			ev := wrote(repo, plan)
			ev.Meta["ok"] = false
			return []watch.Event{ev}
		}},
		{"not a plan", func() []watch.Event { return []watch.Event{wrote(repo, notes)} }},
		{"outside rex/plans", func() []watch.Event { return []watch.Event{wrote(repo, docs)} }},
		{"climbs out", func() []watch.Event {
			sep := string(filepath.Separator)
			return []watch.Event{wrote(repo, PlansDir(repo)+sep+".."+sep+".."+sep+"docs"+sep+"x.rex.json")}
		}},
		{"climbs out, relative", func() []watch.Event {
			return []watch.Event{wrote(repo, "rex/plans/../../docs/x.rex.json")}
		}},
		{"gone", func() []watch.Event {
			return []watch.Event{wrote(repo, filepath.Join(PlansDir(repo), "gone.rex.json"))}
		}},
		{"another repo's plan", func() []watch.Event { return []watch.Event{wrote(other, theirs)} }},
		{"another repo's agent", func() []watch.Event { return []watch.Event{wrote(other, plan)} }},
		{"a shell command", func() []watch.Event {
			return []watch.Event{{Source: "claude", Kind: "result", Text: "ran cat > " + plan, Meta: map[string]any{"tool": "Bash", "command": "cat > " + plan, "ok": true, "cwd": repo}}}
		}},
		{"a link out of rex/plans", func() []watch.Event { return []watch.Event{wrote(repo, link)} }},
	}
	// Each app starts before the files exist, so each waits.
	apps := make([]*app, len(checks))
	for i := range apps {
		apps[i] = startIn(repo)
	}
	writePlan(t, repo, "real.rex.json", demoPlan)
	writePlan(t, other, "theirs.rex.json", demoPlan)
	writePlan(t, repo, "notes.md", "# notes")
	os.MkdirAll(filepath.Dir(docs), 0o755)
	os.WriteFile(docs, []byte(demoPlan), 0o644)
	linked := os.Symlink(theirs, link) == nil
	for i, c := range checks {
		if c.name == "a link out of rex/plans" && !linked {
			continue // no symlinks here (Windows without the privilege)
		}
		a := apps[i]
		a.ingest(c.evs())
		if a.plans.Current != "" || a.at != atWait || a.pending != "" {
			t.Errorf("%s: current=%q at=%d pending=%q", c.name, a.plans.Current, a.at, a.pending)
		}
	}
	// A ".." is judged by where it leads: back into rex/plans is that plan.
	a := apps[0] // still waiting
	sep := string(filepath.Separator)
	if a.ingest([]watch.Event{wrote(repo, PlansDir(repo)+sep+"sub"+sep+".."+sep+"real.rex.json")}); a.plans.Current != plan {
		t.Fatalf("current=%q", a.plans.Current)
	}
}

// The last plan written in a batch wins; a write of the open plan only
// reloads it, wherever you are in it.
func TestLastWriteWinsAndTheOpenPlanStays(t *testing.T) {
	repo := attachRepo(t)
	a := startIn(repo)
	first := writePlan(t, repo, "first.rex.json", demoPlan)
	second := writePlan(t, repo, "second.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, first), wrote(repo, second)})
	if a.plans.Current != second {
		t.Fatalf("current=%q", a.plans.Current)
	}
	press(a, "\r") // task detail
	if a.at != atTaskDetail {
		t.Fatal("setup: task detail")
	}
	if a.ingest([]watch.Event{wrote(repo, second)}); a.at != atTaskDetail || a.plans.Current != second {
		t.Fatalf("a write of the open plan moved you: at=%d", a.at)
	}
	// Following: the agent moves on to another plan, and Rex with it.
	a.ingest([]watch.Event{wrote(repo, first)})
	if a.plans.Current != first || a.at != atOutline || !a.follow {
		t.Fatalf("current=%q at=%d", a.plans.Current, a.at)
	}
}

// Latched: another plan's write is offered on row 1 until taken; Enter on
// an empty draft takes it and follows again.
func TestLatchedOffersTheNewPlan(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	repo := a.plans.Repo
	if a.follow || !strings.Contains(a.frame(), "idle · latched · rex/plans/cuj/demo.rex.json") {
		t.Fatalf("a plan picked in the list is latched\n%s", a.frame())
	}
	other := writePlan(t, repo, "cuj/next.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, other)})
	if a.plans.Current != path || a.at != atOutline {
		t.Fatalf("a latched plan is never yanked: current=%q", a.plans.Current)
	}
	c.t = c.t.Add(10 * toastFor)
	a.tick()
	if got := a.statusLine().Text; got != "new plan cuj/next · ⏎ switch" {
		t.Fatalf("row 1 offers the plan until it is taken: %q", got)
	}
	if !strings.Contains(a.frame(), "⏎ switch") {
		t.Fatalf("%s", a.frame())
	}
	press(a, "esc")
	if got := a.statusLine().Text; got != "new plan cuj/next" {
		t.Fatalf("in the list ⏎ opens the selected row, so the offer doesn't claim it: %q", got)
	}
	press(a, "\r") // Continue
	if a.offer != "" || a.plans.Current != path || a.follow {
		t.Fatalf("opening a plan settles the offer: offer=%q current=%q", a.offer, a.plans.Current)
	}
	a.ingest([]watch.Event{wrote(repo, other)})
	typeText(a, "s")
	press(a, "\x7f", "\r")
	if a.plans.Current != other || !a.follow || a.offer != "" || a.at != atOutline {
		t.Fatalf("⏎ on an empty draft switches: current=%q follow=%v", a.plans.Current, a.follow)
	}
	if !strings.Contains(a.frame(), "following · rex/plans/cuj/next.rex.json") {
		t.Fatalf("%s", a.frame())
	}
}

// Taking the offer opens the plan, so it never runs while that would drop
// what you typed: PLAN's draft, or a done, send-off or pin overlay in
// either place. Row 1 and the footer stop saying ⏎ switch, and ⏎ does
// what it did before.
func TestTheOfferKeepsWhatYouTyped(t *testing.T) {
	var c *clock
	offered := func(t *testing.T) (*app, string, string) {
		a, clk, path := openDemo(t, demoPlan, 120, 32)
		c = clk
		return a, path, writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
	}
	noSwitch := func(t *testing.T, a *app) {
		t.Helper()
		if got := a.statusLine().Text; got != "new plan cuj/next" || strings.Contains(a.frame(), "⏎ switch") {
			t.Fatalf("row 1 %q\n%s", got, a.frame())
		}
	}

	// A half-written Completion in PLAN, ⏎ on the tape.
	a, path, next := offered(t)
	typeText(a, "d")
	press(a, "\r")
	typeText(a, "half written")
	press(a, "\t")
	a.ingest([]watch.Event{wrote(a.plans.Repo, next)})
	noSwitch(t, a)
	press(a, "\r")
	if a.plans.Current != path || a.overlay[PanePlans] != overlayDone || a.form[PanePlans] == nil || a.parked != "half written" {
		t.Fatalf("the form was dropped: current=%q overlay=%d parked=%q", a.plans.Current, a.overlay[PanePlans], a.parked)
	}
	press(a, "\t", "\x15", "esc", "\t") // clear it, close it, back to the tape
	c.t = c.t.Add(toastFor)             // "not marked done" outranks the offer until it expires
	if a.statusLine().Text != "new plan cuj/next · ⏎ switch" {
		t.Fatalf("with nothing to lose the offer is back: %q", a.statusLine().Text)
	}
	if press(a, "\r"); a.plans.Current != next || a.pane != PaneChat {
		t.Fatalf("current=%q pane=%d", a.plans.Current, a.pane)
	}

	// A verb half typed on the outline, ⏎ on the tape.
	a, path, next = offered(t)
	typeText(a, "pin load")
	press(a, "\t")
	a.ingest([]watch.Event{wrote(a.plans.Repo, next)})
	noSwitch(t, a)
	press(a, "\r")
	if a.plans.Current != path || a.parked != "pin load" {
		t.Fatalf("PLAN's draft was dropped: current=%q parked=%q", a.plans.Current, a.parked)
	}

	// A Completion on the tape, ⏎ on the outline opens the task as before.
	a, path, next = offered(t)
	press(a, "\t")
	typeText(a, "d")
	press(a, "\r")
	typeText(a, "half")
	press(a, "\t")
	a.ingest([]watch.Event{wrote(a.plans.Repo, next)})
	noSwitch(t, a)
	press(a, "\r")
	if a.plans.Current != path || a.overlay[PaneChat] != overlayDone || a.form[PaneChat] == nil || a.parked != "half" || a.at != atTaskDetail {
		t.Fatalf("the tape's form was dropped: current=%q overlay=%d parked=%q at=%d", a.plans.Current, a.overlay[PaneChat], a.parked, a.at)
	}
}

// From the tape, ⏎ takes the offer for PLAN and leaves you on the tape.
func TestTakingTheOfferFromTheTape(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	other := writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
	press(a, "\t")
	a.ingest([]watch.Event{wrote(a.plans.Repo, other)})
	press(a, "\r")
	if a.pane != PaneChat || a.plans.Current != other || !a.follow {
		t.Fatalf("pane=%d current=%q", a.pane, a.plans.Current)
	}
	// An offer for a plan deleted since says so and changes nothing.
	press(a, "\t", "esc")
	typeText(a, "cuj/demo")
	press(a, "\r", "\t") // latch again: pick demo
	demo := a.plans.Current
	gone := writePlan(t, a.plans.Repo, "cuj/gone.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(a.plans.Repo, gone)})
	os.Remove(gone)
	press(a, "\r")
	if a.follow || a.plans.Current != demo || a.offer != "" || !strings.Contains(a.statusLine().Text, "cuj/gone is gone") {
		t.Fatalf("current=%q row 1 %q", a.plans.Current, a.statusLine().Text)
	}
}

// Typing in PLAN, or a done, send-off or pin overlay in either place,
// holds the attach until it clears; Help and the tape's draft don't.
func TestAttachWaitsForTheDraftAndOverlays(t *testing.T) {
	repo := attachRepo(t)
	a := startIn(repo)
	a.copy = fakeCopy(new([]string), copyResult{status: "copied (fake)"})
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, one)})
	if a.plans.Current != one {
		t.Fatal("setup: following one")
	}

	two := writePlan(t, repo, "two.rex.json", demoPlan)
	typeText(a, "pi")
	a.ingest([]watch.Event{wrote(repo, two)})
	if a.plans.Current != one || a.pending != two || a.draft != "pi" {
		t.Fatalf("a PLAN draft holds it: current=%q pending=%q", a.plans.Current, a.pending)
	}
	three := writePlan(t, repo, "three.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, three)})
	if a.pending != three {
		t.Fatal("a newer write replaces the held one")
	}
	press(a, "esc")
	if a.plans.Current != three || a.pending != "" || a.at != atOutline {
		t.Fatalf("an empty draft lets it through: current=%q", a.plans.Current)
	}

	typeText(a, "s")
	press(a, "\r") // the send-off overlay
	a.ingest([]watch.Event{wrote(repo, two)})
	if a.plans.Current != three || a.overlay[PanePlans] != overlaySendoff {
		t.Fatal("an open send-off holds it")
	}
	press(a, "esc")
	if a.plans.Current != two {
		t.Fatalf("closing it lets it through: %q", a.plans.Current)
	}

	press(a, "end") // load-outline waits on card-highlight
	typeText(a, "p")
	press(a, "\r") // Pin confirm
	if a.overlay[PanePlans] != overlayPin {
		t.Fatal("setup: Pin confirm")
	}
	a.ingest([]watch.Event{wrote(repo, one)})
	if a.plans.Current != two || a.pending != one || a.overlay[PanePlans] != overlayPin {
		t.Fatalf("an open Pin confirm holds it: current=%q pending=%q", a.plans.Current, a.pending)
	}
	press(a, "esc")
	if a.plans.Current != one || a.pending != "" {
		t.Fatalf("cancelling it lets it through: %q", a.plans.Current)
	}

	press(a, "\t")
	typeText(a, "d")
	press(a, "\r") // the done form, on the tape
	a.ingest([]watch.Event{wrote(repo, two)})
	if a.plans.Current != one || a.form[PaneChat] == nil {
		t.Fatal("a done form in the other place holds it")
	}
	press(a, "esc")
	if a.plans.Current != two || a.pane != PaneChat {
		t.Fatalf("closing it lets it through, and you stay on the tape: current=%q pane=%d", a.plans.Current, a.pane)
	}

	typeText(a, "?")
	press(a, "\r") // Help on the tape, and a draft on the tape
	a.ingest([]watch.Event{wrote(repo, three)})
	if a.plans.Current != three || a.overlay[PaneChat] != overlayHelp {
		t.Fatal("Help holds nothing")
	}

	press(a, "esc", "\t") // close Help, back to PLAN
	press(a, "esc")       // the plan list, still following
	typeText(a, "q")      // a find
	a.ingest([]watch.Event{wrote(repo, two)})
	if a.plans.Current != three || a.at != atLibrary {
		t.Fatalf("a find in the list holds it: current=%q at=%d", a.plans.Current, a.at)
	}
	press(a, "\x15") // Ctrl+U
	if a.plans.Current != two || a.at != atOutline {
		t.Fatalf("the held write applies once the find is cleared: %q", a.plans.Current)
	}
}

// A Name prompt in the plan list is you making a plan: a write waits until
// you cancel it, and is dropped if you create one (docs/ux.md §11).
func TestANamePromptHoldsTheAttach(t *testing.T) {
	repo := attachRepo(t)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	a := startIn(repo)
	press(a, "esc", "home", "down")
	if a.at != atLibrary || a.plans.Rows[a.plans.Cursor].Kind != PlanRowNew {
		t.Fatal("setup: the Create card")
	}
	press(a, "\r")
	if !a.plans.Prompting() {
		t.Fatal("setup: the Name prompt")
	}
	two := writePlan(t, repo, "two.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, two)})
	if a.plans.Current != one || a.at != atLibrary || !a.plans.Prompting() || a.pending != two {
		t.Fatalf("an empty Name prompt holds it: current=%q at=%d pending=%q", a.plans.Current, a.at, a.pending)
	}
	press(a, "esc")
	if a.plans.Current != two || a.at != atOutline || a.plans.Prompting() {
		t.Fatalf("cancelling lets it through: current=%q at=%d prompting=%v", a.plans.Current, a.at, a.plans.Prompting())
	}

	press(a, "esc", "home", "down", "\r")
	three := writePlan(t, repo, "three.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, three)})
	typeText(a, "mine")
	press(a, "\r")
	if a.plans.Current != filepath.Join(PlansDir(repo), "mine.rex.json") || a.follow || a.pending != "" {
		t.Fatalf("creating latches and drops it: current=%q follow=%v pending=%q", a.plans.Current, a.follow, a.pending)
	}
}

// A rex/plans folder that links out of the repo leads nowhere: not at
// start, not from state.json, not on a write through it, not on a scan.
func TestALinkedFolderOutOfRexPlansNeverAttaches(t *testing.T) {
	repo := attachRepo(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "o.rex.json"), []byte(demoPlan), 0o644); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(PlansDir(repo), 0o755)
	if err := os.Symlink(outside, filepath.Join(PlansDir(repo), "ext")); err != nil {
		t.Skip("no symlinks here (Windows without the privilege)")
	}
	through := filepath.Join(PlansDir(repo), "ext", "o.rex.json")
	now := time.Now()
	if err := plan.UpdateState(plan.StatePath(), now, func(s *plan.State) {
		s.Repos[repo] = plan.RepoState{Plan: through, Opened: float64(now.Unix())}
	}); err != nil {
		t.Fatal(err)
	}
	a := startIn(repo)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	a.ingest([]watch.Event{wrote(repo, through), wrote(repo, "rex/plans/ext/o.rex.json")})
	later(a, c)
	later(a, c)
	if a.plans.Current != "" || a.at != atWait || a.pending != "" {
		t.Fatalf("current=%q at=%d pending=%q", a.plans.Current, a.at, a.pending)
	}
}

// Picking a plan while a write is held drops it; picking another plan
// latches, and picking the open one again keeps following.
func TestLatchingDropsAHeldAttach(t *testing.T) {
	repo := attachRepo(t)
	alt := writePlan(t, repo, "alt.rex.json", demoPlan)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	a := startIn(repo)
	for _, pick := range []struct{ find, want string }{{"on", one}, {"al", alt}} {
		press(a, "esc")
		typeText(a, pick.find)
		two := writePlan(t, repo, "two.rex.json", demoPlan)
		a.ingest([]watch.Event{wrote(repo, two)})
		if a.pending != two {
			t.Fatal("setup: held")
		}
		press(a, "\r") // the first match
		if a.plans.Current != pick.want || a.follow != (pick.want == one) || a.pending != "" {
			t.Fatalf("%s: current=%q follow=%v pending=%q", pick.find, a.plans.Current, a.follow, a.pending)
		}
		a.settle(a.columns, a.lines)
		if a.plans.Current != pick.want {
			t.Fatal("a dropped write never comes back")
		}
	}
}

// The agent writes a stub, then fills it: the outline opens empty and
// lands on the pin once tasks appear.
func TestStubThenFillLandsOnThePin(t *testing.T) {
	repo := attachRepo(t)
	a := startIn(repo)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	path := writePlan(t, repo, "stub.rex.json", string(plan.Stub("stub")))
	a.ingest([]watch.Event{wrote(repo, path)})
	if a.at != atOutline || a.outline.sel != "" || !strings.Contains(a.frame(), "No tasks yet") {
		t.Fatalf("at=%d sel=%q\n%s", a.at, a.outline.sel, a.frame())
	}
	os.WriteFile(path, []byte(demoPlan), 0o644)
	a.ingest([]watch.Event{wrote(repo, path)})
	later(a, c)
	if a.outline.sel != "card-highlight" {
		t.Fatalf("sel=%q", a.outline.sel)
	}
}

// With no plan open, a plan file that appears without a hook is found on
// the next poll. Once one is open, a new file never takes the screen.
func TestColdStartFindsAPlanWithoutAHook(t *testing.T) {
	repo := attachRepo(t)
	a := startIn(repo)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	later(a, c)
	if a.at != atWait {
		t.Fatal("nothing to find yet")
	}
	path := writePlan(t, repo, "saved.rex.json", demoPlan)
	later(a, c)
	if a.plans.Current != path || a.at != atOutline || !a.follow {
		t.Fatalf("current=%q at=%d", a.plans.Current, a.at)
	}
	writePlan(t, repo, "newer.rex.json", demoPlan)
	later(a, c)
	later(a, c)
	if a.plans.Current != path {
		t.Fatalf("a plan file with no write seen never takes the screen once one is open: %q", a.plans.Current)
	}
}

// docs/ux.md §4: the header says following or latched in words. The path
// drops rex/plans/ and .rex.json, then clips down to a few cells, before
// the word goes; a dropped word never gives ".rex.json" its room back.
// Both words come and go together, at 38 cells.
func TestHeaderSaysFollowingOrLatched(t *testing.T) {
	cases := []struct {
		width                     int
		state, follow, path, meta string
	}{
		{78, "running", "following", "rex/plans/cuj/sendoff-demo.rex.json", "running · following · rex/plans/cuj/sendoff-demo.rex.json"},
		{38, "running", "latched", "rex/plans/scratch.rex.json", "running · latched · scratch"},
		{38, "running", "latched", "rex/plans/cuj/sendoff-demo.rex.json", "running · latched · …ff-demo"},
		{38, "thinking", "following", "rex/plans/cuj/sendoff-demo.rex.json", "thinking · following · …demo"},
		{38, "idle", "following", "rex/plans/plan_attach.rex.json", "idle · following · …n_attach"},
		{30, "running", "following", "rex/plans/cuj/sendoff-demo.rex.json", "running · …doff-demo"},
		{32, "idle", "following", "rex/plans/x.rex.json", "idle · x"},
		{36, "idle", "latched", "rex/plans/scan.rex.json", "idle · scan"},
		{37, "idle", "latched", "rex/plans/cuj/sendoff-demo.rex.json", "idle · cuj/sendoff-demo"},
		{32, "idle", "", "rex/plans/x.rex.json", "idle · x.rex.json"},
		{38, "running", "", "", "running"},
	}
	for _, tc := range cases {
		got := headerRow(tc.width, "plan", tc.state, tc.follow, tc.path, false)
		if !strings.HasPrefix(got, "REX plan ") || !strings.HasSuffix(got, " "+tc.meta) || cellLen(got) != tc.width {
			t.Errorf("%d %s %s:\n got %q\nwant %q on the right", tc.width, tc.follow, tc.path, got, tc.meta)
		}
	}
	// From 38 cells the word always shows; a name under 5 cells lets it in
	// a little sooner.
	for w := 1; w <= 130; w++ {
		for path, from := range map[string]int{"rex/plans/x.rex.json": 34, "rex/plans/scan.rex.json": 37, "rex/plans/cuj/sendoff-demo.rex.json": 38} {
			f := strings.Contains(headerRow(w, "plan", "idle", "following", path, false), " · following")
			l := strings.Contains(headerRow(w, "plan", "idle", "latched", path, false), " · latched")
			if f != l || f != (w >= from) {
				t.Errorf("%d cells, %s: following %v, latched %v", w, path, f, l)
			}
		}
	}
	// At 80 columns the word shows whatever the turn state and the name.
	w := layoutFor(80, 24).textW
	for _, state := range []string{"idle", "thinking", "running", "ask", "error"} {
		for _, follow := range []string{"following", "latched"} {
			for _, path := range []string{"rex/plans/cuj/sendoff-demo.rex.json", "rex/plans/a-rather-long/plan_attach_and_more.rex.json"} {
				if got := headerRow(w, "tape", state, follow, path, false); !strings.Contains(got, state+" · "+follow+" · ") || cellLen(got) != w {
					t.Errorf("80 columns, %s %s %s: %q", state, follow, path, got)
				}
			}
		}
	}
	a := testApp(t)
	if a.followWord() != "" || strings.Contains(a.frameRows().Rows[0], "following") {
		t.Fatal("no plan, no word")
	}
	b, _, _ := openDemo(t, demoPlan, 80, 24)
	if row := b.frameRows().Rows[0]; !strings.Contains(row, "idle · latched · ") {
		t.Fatalf("row 0 at 80x24: %q", row)
	}
}

// A verb that needs a plan, on the Wait screen or the tape, says the agent
// makes one; at 80 columns row 1 keeps the short form.
func TestVerbsWithNoPlanPointAtTheAgent(t *testing.T) {
	for _, verb := range []string{"d", "s", "p", "b", "done", "sendoff", "pin load", "brief"} {
		for _, tab := range []bool{false, true} {
			a := testApp(t)
			if tab {
				press(a, "\t")
			}
			typeText(a, verb)
			press(a, "\r")
			if got := a.statusLine(); got.Text != noPlanText || got.Short != "no plan yet" {
				t.Fatalf("%s (tape %v): %+v", verb, tab, got)
			}
			if row := statusRow(a.statusLine(), layoutFor(80, 24).textW, false); !strings.HasPrefix(row, "no plan yet ") || strings.Contains(row, "tab to plan") {
				t.Fatalf("%s at 80 columns: %q", verb, row)
			}
		}
	}
}

// docs/ux.md §4: the plan file's error, a toast and hard drift outrank a
// new-plan offer on row 1, soft drift sits under it, and ⏎ switches only
// while row 1 shows the offer; under anything else it does what it did.
func TestRowOneOutranksTheOffer(t *testing.T) {
	// Any drift comes in the batch that brings the offer, which is judged
	// before the offer is up (§11); after it, the agent's edits are the
	// offered plan's and are not judged (§9a).
	offered := func(t *testing.T, drift ...string) (*app, *clock, string, string) {
		a, c, path := openDemo(t, demoPlan, 120, 32)
		next := writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
		batch := []watch.Event{wrote(a.plans.Repo, next)}
		for _, rel := range drift {
			p := filepath.Join(a.plans.Repo, filepath.FromSlash(rel))
			batch = append(batch, at(c, time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + p, Meta: map[string]any{"tool": "Write", "path": p, "ok": true, "cwd": a.plans.Repo}}))
		}
		a.ingest(batch)
		c.t = c.t.Add(toastFor)
		return a, c, path, next
	}
	hidden := func(t *testing.T, a *app, path, want string) {
		t.Helper()
		if got := a.statusLine().Text; !strings.HasPrefix(got, want) || strings.Contains(a.frame(), "⏎ switch") {
			t.Fatalf("row 1 %q, want %q\n%s", got, want, a.frame())
		}
		press(a, "\r") // the outline's own ⏎
		if a.plans.Current != path || a.at != atTaskDetail || a.offer == "" {
			t.Fatalf("⏎ switched to an offer row 1 didn't show: current=%q at=%d", a.plans.Current, a.at)
		}
		press(a, "esc")
	}

	// A verb's error toast, then the offer once it expires.
	a, c, path, next := offered(t)
	typeText(a, "pin nosuch")
	press(a, "\r")
	hidden(t, a, path, "no task nosuch in this plan")
	c.t = c.t.Add(toastFor)
	if got := a.statusLine().Text; got != "new plan cuj/next · ⏎ switch" {
		t.Fatalf("the offer is back once the toast is gone: %q", got)
	}
	if press(a, "\r"); a.plans.Current != next || !a.follow {
		t.Fatalf("current=%q follow=%v", a.plans.Current, a.follow)
	}

	// Hard drift: it stays until resolved, and so does ⏎'s usual meaning.
	a, _, path, _ = offered(t, "README.md")
	hidden(t, a, path, "drift · hard: README.md")

	// Soft drift sits under the offer, and shows once it is settled.
	a, _, _, _ = offered(t, "internal/tui/live.go")
	if s, ok := a.driftStatus(plan.Soft); !ok || a.statusLine().Text != "new plan cuj/next · ⏎ switch" {
		t.Fatalf("soft drift %q (%v) is under the offer: %q", s.Text, ok, a.statusLine().Text)
	}
	press(a, "esc", "\r") // Continue settles the offer
	if got := a.statusLine().Text; a.offer != "" || !strings.HasPrefix(got, "drift · soft: internal/tui/live.go") {
		t.Fatalf("row 1 %q", got)
	}

	// The plan file can't be read.
	a, c, path, _ = offered(t)
	os.WriteFile(path, []byte("{"), 0o644)
	later(a, c)
	later(a, c)
	hidden(t, a, path, "plan JSON error")
}

// docs/ux.md §11: p⏎ or d⏎ writing to the followed plan latches it, so a
// pin stint isn't yanked by another plan's write; a write held meanwhile
// becomes an offer.
func TestYourOwnWritesLatch(t *testing.T) {
	repo := attachRepo(t)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	a := startIn(repo)
	press(a, "end") // load-outline, after card-highlight
	typeText(a, "p")
	press(a, "\r")
	if !a.follow || a.overlay[PanePlans] != overlayPin {
		t.Fatal("setup: Pin confirm, nothing written yet, still following")
	}
	typeText(a, "p")
	press(a, "\r")
	if p, _ := plan.Load(one); a.follow || p.Task("load-outline").Status != plan.Active || !strings.Contains(a.frame(), "latched · rex/plans/one.rex.json") {
		t.Fatalf("a pin you wrote latches: follow=%v\n%s", a.follow, a.frame())
	}
	two := writePlan(t, repo, "two.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, two)})
	if a.plans.Current != one || a.offer != two {
		t.Fatalf("offered, not attached: current=%q offer=%q", a.plans.Current, a.offer)
	}

	// d⏎ with a write held: the write turns into an offer, and the
	// form's own result keeps row 1 until it expires.
	repo = attachRepo(t)
	one = writePlan(t, repo, "one.rex.json", demoPlan)
	b := startIn(repo)
	c := &clock{t: time.Now()}
	b.now = c.now
	typeText(b, "d")
	press(b, "\r")
	typeText(b, "made the card one block")
	two = writePlan(t, repo, "two.rex.json", demoPlan)
	b.ingest([]watch.Event{wrote(repo, two)})
	if b.pending != two {
		t.Fatal("setup: held by the form")
	}
	press(b, "\r")
	if b.plans.Current != one || b.follow || b.pending != "" || b.offer != two || !strings.HasPrefix(b.statusLine().Text, "✓ card-highlight done") {
		t.Fatalf("current=%q follow=%v pending=%q offer=%q row 1 %q", b.plans.Current, b.follow, b.pending, b.offer, b.statusLine().Text)
	}
	c.t = c.t.Add(toastFor)
	if got := b.statusLine().Text; got != "new plan two · ⏎ switch" {
		t.Fatalf("row 1 %q", got)
	}
}

// ⏎ on Continue reopens the plan already open: it picks nothing new, so
// a followed plan stays followed (docs/ux.md §11).
func TestContinueKeepsFollowing(t *testing.T) {
	repo := attachRepo(t)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	a := startIn(repo)
	press(a, "esc")
	if a.plans.Rows[a.plans.Cursor].Kind != PlanRowContinue {
		t.Fatal("setup: on Continue")
	}
	press(a, "\r")
	if a.plans.Current != one || a.at != atOutline || !a.follow || !strings.Contains(a.frame(), "following · rex/plans/one.rex.json") {
		t.Fatalf("follow=%v\n%s", a.follow, a.frame())
	}
	two := writePlan(t, repo, "two.rex.json", demoPlan)
	if a.ingest([]watch.Event{wrote(repo, two)}); a.plans.Current != two {
		t.Fatalf("still following: %q", a.plans.Current)
	}
}

// RunLoop applies a read's keys before the events polled with it: a key
// acts on the plan you saw, and a draft typed in that read holds the
// attach those events bring.
func TestKeysActBeforeTheEventsReadWithThem(t *testing.T) {
	repo := attachRepo(t)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	a := startIn(repo)
	two := writePlan(t, repo, "two.rex.json", demoPlan)
	if a.step([]string{"p", "i"}, []watch.Event{wrote(repo, two)}); a.plans.Current != one || a.pending != two || a.draft != "pi" {
		t.Fatalf("the draft holds it: current=%q pending=%q draft=%q", a.plans.Current, a.pending, a.draft)
	}
	press(a, "esc")
	if a.step([]string{"\r"}, []watch.Event{wrote(repo, one)}); a.plans.Current != one || a.at != atOutline {
		t.Fatalf("⏎ opened two's task detail, then one opened: current=%q at=%d", a.plans.Current, a.at)
	}

	// Latched: ⏎ read with the write that makes the offer opens the task
	// you were on; the offer waits on row 1.
	b, _, path := openDemo(t, demoPlan, 120, 32)
	next := writePlan(t, b.plans.Repo, "cuj/next.rex.json", demoPlan)
	if b.step([]string{"\r"}, []watch.Event{wrote(b.plans.Repo, next)}); b.plans.Current != path || b.at != atTaskDetail || b.offer != next {
		t.Fatalf("current=%q at=%d offer=%q", b.plans.Current, b.at, b.offer)
	}
	third := writePlan(t, b.plans.Repo, "cuj/third.rex.json", demoPlan)
	b.step([]string{CtrlC, CtrlC}, []watch.Event{wrote(b.plans.Repo, third)})
	if !b.quit || b.offer != next {
		t.Fatalf("a quit ends the read before its events: quit=%v offer=%q", b.quit, b.offer)
	}
}

// A held attach says so on row 1, and on an 80-column terminal keeps a
// clipped name. A Wait screen draft holds nothing: the plan opens and the
// draft, a verb there as on the outline, carries over.
func TestAHeldAttachShowsOnRowOne(t *testing.T) {
	repo := attachRepo(t)
	writePlan(t, repo, "one.rex.json", demoPlan)
	a := startIn(repo)
	typeText(a, "pi")
	two := writePlan(t, repo, "team/a-much-longer-plan-name.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, two)})
	if got := a.statusLine().Text; got != "new plan team/a-much-longer-plan-name · opens when the line is clear" {
		t.Fatalf("row 1 %q", got)
	}
	a.settle(80, 24)
	if row := strings.TrimSpace(a.frameRows().Rows[1]); !strings.HasPrefix(row, "new plan …") || !strings.HasSuffix(row, "-name · waiting") {
		t.Fatalf("row 1 at 80 columns: %q", row)
	}
	press(a, "\x15")
	if a.plans.Current != two || strings.HasPrefix(a.statusLine().Text, "new plan") {
		t.Fatalf("current=%q row 1 %q", a.plans.Current, a.statusLine().Text)
	}

	for _, tape := range []bool{false, true} {
		repo := attachRepo(t)
		w := startIn(repo)
		typeText(w, "pin load-outline")
		if tape {
			press(w, "\t")
		}
		p := writePlan(t, repo, "p.rex.json", demoPlan)
		w.ingest([]watch.Event{wrote(repo, p)})
		draft := w.draft
		if tape {
			draft = w.parked
		}
		if w.plans.Current != p || w.pending != "" || w.at != atOutline || draft != "pin load-outline" {
			t.Fatalf("tape %v: current=%q pending=%q draft=%q", tape, w.plans.Current, w.pending, draft)
		}
	}
}

// docs/ux.md §3, §11: with no plan open, Esc and ← climb from Library Home
// back to the Wait screen; with one open, Home is the top.
func TestEscFromTheListGoesBackToWait(t *testing.T) {
	a := testApp(t)
	press(a, "esc")
	if a.at != atLibrary || !strings.Contains(a.frame(), "or ask your agent to write rex/plans/<name>.rex.json · Rex opens it") {
		t.Fatalf("the plan list with no plans says the agent can write one\n%s", a.frame())
	}
	typeText(a, "x")
	press(a, "esc")
	if a.at != atLibrary || a.draft != "" {
		t.Fatal("Esc clears the find first")
	}
	press(a, "esc")
	if a.at != atWait {
		t.Fatalf("Esc goes back to Wait: at=%d", a.at)
	}
	press(a, "esc", "left")
	if a.at != atWait || !strings.Contains(a.frame(), noPlanText) {
		t.Fatalf("← goes back too: at=%d", a.at)
	}

	b, _, _ := openDemo(t, demoPlan, 120, 32)
	press(b, "esc", "esc", "left")
	if b.at != atLibrary {
		t.Fatalf("with a plan open, Home is the top: at=%d", b.at)
	}
	if strings.Contains(b.frame(), "or ask your agent") {
		t.Fatal("the hint is for the list with no plans")
	}
}

// A batch (or one patch) that writes a new plan and the open one still
// offers or opens the new one; a write of the open plan alone drops a
// held attach.
func TestAMixedBatchStillOffersTheNewPlan(t *testing.T) {
	patch := func(repo string, paths ...string) watch.Event {
		var list []any
		for _, p := range paths {
			list = append(list, p)
		}
		return watch.Event{Source: "codex", Kind: "result", Text: "apply_patch", Meta: map[string]any{"tool": "apply_patch", "paths": list, "cwd": repo}}
	}
	for _, batch := range []func(repo, next, open string) []watch.Event{
		func(repo, next, open string) []watch.Event {
			return []watch.Event{wrote(repo, next), wrote(repo, open)}
		},
		func(repo, next, open string) []watch.Event {
			return []watch.Event{wrote(repo, open), wrote(repo, next)}
		},
		func(repo, next, open string) []watch.Event { return []watch.Event{patch(repo, next, open)} },
	} {
		a, _, path := openDemo(t, demoPlan, 120, 32)
		next := writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
		a.ingest(batch(a.plans.Repo, next, path))
		if a.plans.Current != path || a.offer != next {
			t.Fatalf("latched: current=%q offer=%q", a.plans.Current, a.offer)
		}
	}

	repo := attachRepo(t)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	a := startIn(repo)
	two := writePlan(t, repo, "two.rex.json", demoPlan)
	if a.ingest([]watch.Event{patch(repo, two, one)}); a.plans.Current != two {
		t.Fatalf("following: %q", a.plans.Current)
	}
	typeText(a, "pi")
	a.ingest([]watch.Event{wrote(repo, one)})
	if a.pending != one {
		t.Fatal("setup: held")
	}
	a.ingest([]watch.Event{wrote(repo, two)})
	if press(a, "esc"); a.pending != "" || a.plans.Current != two {
		t.Fatalf("the agent is back on the open plan: pending=%q current=%q", a.pending, a.plans.Current)
	}
}

// The open plan under another spelling (a link inside rex/plans, another
// case on macOS or Windows) is the same plan: not offered, not opened
// again, and an offer or a held write that turns out to be it just ends.
func TestAnotherSpellingIsTheSamePlan(t *testing.T) {
	repo := attachRepo(t)
	real := writePlan(t, repo, "real.rex.json", demoPlan)
	alias := filepath.Join(PlansDir(repo), "alias.rex.json")
	if err := os.Symlink(real, alias); err != nil {
		t.Skip("no symlinks here (Windows without the privilege)")
	}
	writePlan(t, repo, "other.rex.json", demoPlan) // start follows this one
	a := startIn(repo)
	press(a, "esc")
	typeText(a, "alias")
	press(a, "\r")
	if a.plans.Current != alias || a.follow {
		t.Fatalf("setup: latched on the link: %q", a.plans.Current)
	}
	if a.ingest([]watch.Event{wrote(repo, alias), wrote(repo, real)}); a.offer != "" {
		t.Fatalf("offered the plan on screen: %q", a.offer)
	}

	// Following real, a write through the link keeps the selection, and
	// picking the link in the list picks nothing new.
	writePlan(t, repo, "real.rex.json", demoPlan) // newest again
	b := startIn(repo)
	press(b, "end")
	sel := b.outline.sel
	press(b, "esc")
	typeText(b, "alias")
	press(b, "\r")
	if !samePlan(b.plans.Current, real) || !b.follow || b.outline.sel != sel {
		t.Fatalf("setup: following real with the selection kept: %q sel %q -> %q", b.plans.Current, sel, b.outline.sel)
	}
	if b.ingest([]watch.Event{wrote(repo, alias)}); !samePlan(b.plans.Current, real) || b.outline.sel != sel || strings.Contains(b.statusLine().Text, "following") {
		t.Fatalf("opened again: current=%q sel %q -> %q", b.plans.Current, sel, b.outline.sel)
	}

	// An offer that has become the open plan: ⏎ only follows again.
	c, _, path := openDemo(t, demoPlan, 120, 32)
	next := writePlan(t, c.plans.Repo, "cuj/next.rex.json", demoPlan)
	c.ingest([]watch.Event{wrote(c.plans.Repo, next)})
	os.Remove(next)
	if err := os.Symlink(path, next); err != nil {
		t.Fatal(err)
	}
	press(c, "down")
	sel = c.outline.sel
	if press(c, "\r"); c.plans.Current != path || !c.follow || c.offer != "" || c.outline.sel != sel || c.at != atOutline {
		t.Fatalf("current=%q follow=%v sel %q -> %q at=%d", c.plans.Current, c.follow, sel, c.outline.sel, c.at)
	}

	// A held write that has become the open plan, or is gone, just ends.
	for _, change := range []func(held, open string){
		func(held, open string) { os.Remove(held); os.Symlink(open, held) },
		func(held, open string) { os.Remove(held) },
	} {
		repo := attachRepo(t)
		one := writePlan(t, repo, "one.rex.json", demoPlan)
		d := startIn(repo)
		typeText(d, "pi")
		two := writePlan(t, repo, "two.rex.json", demoPlan)
		d.ingest([]watch.Event{wrote(repo, two)})
		change(two, one)
		if press(d, "esc"); d.plans.Current != one || d.pending != "" || strings.HasPrefix(d.statusLine().Text, "new plan") {
			t.Fatalf("current=%q pending=%q row 1 %q", d.plans.Current, d.pending, d.statusLine().Text)
		}
	}
}

// On a case-insensitive filesystem, DEMO.rex.json is demo.rex.json.
func TestAnotherCaseIsTheSamePlan(t *testing.T) {
	repo := attachRepo(t)
	path := writePlan(t, repo, "demo.rex.json", demoPlan)
	upper := filepath.Join(PlansDir(repo), "DEMO.rex.json")
	if _, err := os.Stat(upper); err != nil {
		t.Skip("this filesystem tells case apart")
	}
	a := startIn(repo)
	press(a, "down")
	sel := a.outline.sel
	if a.ingest([]watch.Event{wrote(repo, upper)}); a.plans.Current != path || a.outline.sel != sel || strings.Contains(a.statusLine().Text, "following") {
		t.Fatalf("following: opened again: current=%q sel %q -> %q", a.plans.Current, sel, a.outline.sel)
	}
	b, _, demo := openDemo(t, demoPlan, 120, 32)
	if b.ingest([]watch.Event{wrote(b.plans.Repo, filepath.Join(filepath.Dir(demo), "Demo.rex.json"))}); b.offer != "" {
		t.Fatalf("latched: offered the plan on screen: %q", b.offer)
	}
}

// With plans off, the Wait screen says so and how to turn them on, and
// sends you nowhere else; outside git it names the folder plans go to.
func TestWaitCopyFollowsWherePlansGo(t *testing.T) {
	text := func(lines []LogLine) string { return viewText(lines) }
	a := testApp(t)
	os.WriteFile(filepath.Join(a.plans.Repo, "rex"), []byte("x"), 0o644)
	a.plans.Rebuild()
	off := text(a.waitBody(300))
	if !strings.Contains(off, rexFileNotice) || !strings.Contains(off, "Move it to turn plans on") {
		t.Fatalf("plans off:\n%s", off)
	}
	for _, never := range []string{"tell your agent", "Rex opens", "install-skill"} {
		if strings.Contains(off, never) {
			t.Fatalf("plans off still says %q:\n%s", never, off)
		}
	}

	out := t.TempDir() // not a git repo
	b := newApp("yellow", false, "idle", NewPlanBrowser(out), 120, 32)
	body := text(b.waitBody(300))
	if want := "Rex opens " + homePath(PlansDir(out)) + "/<name>.rex.json here"; !strings.Contains(body, want) || !strings.Contains(body, "not in a git repo") {
		t.Fatalf("outside git, want %q:\n%s", want, body)
	}
}

// docs/ux.md §11: every line of the Wait screen can be reached on a short
// terminal: ↑↓, pages, Home and End scroll it.
func TestTheWaitScreenScrolls(t *testing.T) {
	a := testApp(t)
	a.settle(60, 7) // text only: rows are the text column
	body := a.waitBody(60)
	seen := map[string]bool{}
	look := func() {
		for _, row := range a.frameRows().Rows[2:5] {
			seen[strings.TrimSpace(row)] = true
		}
	}
	look()
	if footer := a.frameRows().Rows[6]; !strings.Contains(footer, "↑↓ scroll") {
		t.Fatalf("footer %q", footer)
	}
	for i := 0; i < len(body); i++ {
		press(a, "down")
		look()
	}
	for _, line := range body {
		if text := strings.TrimSpace(line.Text); text != "" && !seen[text] {
			t.Errorf("never on screen: %q", text)
		}
	}
	press(a, "home")
	if a.outline.scroll != 0 || !strings.Contains(a.frameRows().Rows[2], "plan") {
		t.Fatal("Home is the top")
	}
	press(a, "end")
	a.frameRows()
	if a.outline.scroll != len(body)-layoutFor(60, 7).bodyH {
		t.Fatalf("End is the bottom: %d", a.outline.scroll)
	}
	for range body {
		press(a, "pageup")
	}
	if a.frameRows(); a.outline.scroll != 0 {
		t.Fatalf("scroll=%d", a.outline.scroll)
	}
	a.settle(60, 32)
	if footer := a.frameRows().Rows[31]; strings.Contains(footer, "scroll") {
		t.Fatalf("nothing to scroll at 32 rows: %q", footer)
	}
	if a.settle(20, 10); a.waitBody(0) != nil || len(a.frameRows().Rows) != 10 {
		t.Fatal("too narrow: no copy, still a frame")
	}
}

// The Wait screen's composer takes verbs: Esc clears a draft first, ←
// leaves it be, and a word that isn't a verb says so.
func TestWaitKeys(t *testing.T) {
	a := testApp(t)
	typeText(a, "zzz")
	if press(a, "left", "right"); a.draft != "zzz" || a.at != atWait {
		t.Fatalf("draft=%q at=%d", a.draft, a.at)
	}
	press(a, "\r")
	if got := a.statusLine().Text; got != "'zzz' is not a verb · ?⏎ help" || a.draft != "zzz" {
		t.Fatalf("row 1 %q draft=%q", got, a.draft)
	}
	if press(a, "esc"); a.draft != "" || a.at != atWait {
		t.Fatalf("Esc clears the draft and stays: draft=%q at=%d", a.draft, a.at)
	}
	if a.waitKey("\r") {
		t.Fatal("⏎ on an empty draft does nothing")
	}
}

func TestSamePlanGoesByTheFile(t *testing.T) {
	repo := attachRepo(t)
	p := writePlan(t, repo, "p.rex.json", demoPlan)
	gone := filepath.Join(PlansDir(repo), "gone.rex.json")
	for _, tc := range []struct {
		a, b string
		want bool
	}{{p, p, true}, {p, PlansDir(repo) + string(filepath.Separator) + "." + string(filepath.Separator) + "p.rex.json", true}, {gone, p, false}, {p, gone, false}, {p, "", false}, {"", "", false}} {
		if got := samePlan(tc.a, tc.b); got != tc.want {
			t.Errorf("samePlan(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}

// docs/ux.md §4: row 1's offer keeps a clipped name while 5 cells are
// free for it, then drops it whole.
func TestTheOfferKeepsAClippedName(t *testing.T) {
	cases := []struct {
		width                   int
		name, tail, short, want string
	}{
		{40, "cuj/sendoff-demo", " · ⏎ switch", " · ⏎ switch", "new plan cuj/sendoff-demo · ⏎ switch"},
		{30, "cuj/sendoff-demo", " · ⏎ switch", " · ⏎ switch", "new plan …doff-demo · ⏎ switch"},
		{25, "cuj/sendoff-demo", " · ⏎ switch", " · ⏎ switch", "new plan …demo · ⏎ switch"},
		{24, "cuj/sendoff-demo", " · ⏎ switch", " · ⏎ switch", "new plan · ⏎ switch"},
		{38, "a-much-longer-plan-name", " · opens when the line is clear", " · waiting", "new plan …h-longer-plan-name · waiting"},
		{14, "x", "", "", "new plan x"},
	}
	for _, tc := range cases {
		if got := fitNewPlan(tc.name, tc.tail, tc.short, tc.width); got != tc.want || cellLen(got) > max(tc.width, cellLen("new plan · ⏎ switch")) {
			t.Errorf("%d %q: got %q, want %q", tc.width, tc.name, got, tc.want)
		}
	}
	a, _, _ := openDemo(t, demoPlan, 80, 24)
	next := writePlan(t, a.plans.Repo, "cuj/a-much-longer-plan-name-from-the-agent.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(a.plans.Repo, next)})
	a.toast = StatusLine{}
	if row := strings.TrimSpace(a.frameRows().Rows[1]); !strings.HasPrefix(row, "new plan …") || !strings.HasSuffix(row, "-the-agent · ⏎ switch") {
		t.Fatalf("row 1 at 80 columns: %q", row)
	}
}

// Latched in the plan list, a plan the agent writes shows up in it at
// once, so row 1 never names a plan the list can't open.
func TestTheListShowsAPlanTheAgentWrote(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	press(a, "esc")
	next := writePlan(t, a.plans.Repo, "cuj/zzz-brand-new.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(a.plans.Repo, next)})
	if a.plans.Rows[a.plans.Cursor].Kind != PlanRowContinue || !strings.Contains(viewText(BuildPlanView(a.plans, 78)), "cuj/zzz-brand-new.rex.json") {
		t.Fatalf("cursor on %v\n%s", a.plans.Rows[a.plans.Cursor].Kind, viewText(BuildPlanView(a.plans, 78)))
	}
}
