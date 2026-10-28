package tui

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/skill"
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

// stampAfter gives path a modification time a second after ref's, so
// which is newer never rests on a filesystem's clock tick (NTFS's is
// about 15 ms) when both were written just now.
func stampAfter(t *testing.T, path, ref string) {
	t.Helper()
	info, err := os.Stat(ref)
	if err != nil {
		t.Fatal(err)
	}
	at := info.ModTime().Add(time.Second)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
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
	if err := watch.Record(watch.PayloadsFromRaw(string(data)), source, feed, nil); err != nil {
		t.Fatal(err)
	}
	return watch.LastEvents(feed, 10)
}

// docs/ux.md §11, §14: start lands on Plans with Continue, the plan you
// had last time, selected: opening it is one key, and it follows.
func TestStartLandsOnContinue(t *testing.T) {
	repo := attachRepo(t)
	last := writePlan(t, repo, "cuj/last.rex.json", demoPlan)
	newer := writePlan(t, repo, "newer.rex.json", demoPlan)
	now := time.Now()
	if err := plan.UpdateState(plan.StatePath(), now, func(s *plan.State) {
		s.Repos[repo] = plan.RepoState{Plan: last, Opened: float64(now.Unix())}
	}); err != nil {
		t.Fatal(err)
	}
	a := startIn(repo)
	if a.pane != PanePlans || a.at != atLibrary || a.plans.Current != "" || a.doc != nil {
		t.Fatalf("start opens nothing: at=%d current=%q", a.at, a.plans.Current)
	}
	row := a.plans.Rows[a.plans.Cursor]
	if row.Kind != PlanRowContinue || row.Path != last {
		t.Fatalf("the cursor starts on Continue: %+v", row)
	}
	if i := a.plans.rowOf(PlanRowLatest); i < 0 || a.plans.Rows[i].Path != newer {
		t.Fatalf("Open latest is the newer plan: %+v", a.plans.Rows)
	}
	k := &keys{a: a}
	k.press("\r")
	k.spent(t, 1, "open last time's plan (enter on Continue)")
	if a.at != atOutline || a.plans.Current != last || !a.follow || a.outline.sel != "card-highlight" {
		t.Fatalf("at=%d current=%q follow=%v sel=%q", a.at, a.plans.Current, a.follow, a.outline.sel)
	}
	if !strings.HasSuffix(textOf(a.frameRows().Rows[0], a.columns), "1/3 done · following   plan · agent") {
		t.Fatalf("row 0 says Rex follows the plan (docs/ux.md §4)\n%s", a.frame())
	}
}

// A state.json entry that is gone, or not one of this repo's plans, is no
// Continue card: the cursor starts on Open latest, else Create a plan.
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
		a := startIn(repo)
		if a.at != atLibrary || a.plans.Continue != "" || a.plans.Rows[a.plans.Cursor].Kind != PlanRowNew {
			t.Fatalf("%s: at=%d continue=%q rows=%+v", last, a.at, a.plans.Continue, a.plans.Rows)
		}
	}
	newest := writePlan(t, repo, "newest.rex.json", demoPlan)
	state, _ := json.Marshal(map[string]any{"version": 1, "repos": map[string]any{repo: map[string]any{"plan": outside, "opened": time.Now().Unix()}}})
	os.WriteFile(plan.StatePath(), state, 0o600)
	a := startIn(repo)
	if row := a.plans.Rows[a.plans.Cursor]; a.at != atLibrary || a.plans.rowOf(PlanRowContinue) >= 0 || row.Kind != PlanRowLatest || row.Path != newest {
		t.Fatalf("at=%d on %+v", a.at, row)
	}
}

// docs/ux.md §11: starting on Plans never opens a plan by itself. The
// newest plan is Open latest, and no tick opens it, however long you wait:
// only your agent's write does (attach), or a key.
func TestStartNeverOpensAPlan(t *testing.T) {
	repo := attachRepo(t)
	writePlan(t, repo, "old.rex.json", demoPlan)
	newest := writePlan(t, repo, "team/new.rex.json", demoPlan)
	writePlan(t, repo, "notes.md", "not a plan")
	a := startIn(repo)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	for i := 0; i < 50; i++ { // 2.5 s of polls, and a plan that shows up meanwhile
		if i == 10 {
			writePlan(t, repo, "by-hand.rex.json", demoPlan)
		}
		c.t = c.t.Add(docPollEvery / 10)
		a.tick()
		a.settle(a.columns, a.lines)
	}
	if a.at != atLibrary || a.plans.Current != "" || a.doc != nil {
		t.Fatalf("a plan opened by itself: at=%d current=%q", a.at, a.plans.Current)
	}
	if i := a.plans.rowOf(PlanRowLatest); i < 0 || filepath.Base(a.plans.Rows[i].Path) != "by-hand.rex.json" || a.plans.Rows[a.plans.Cursor].Path != newest {
		t.Fatalf("the scan only refreshed the cards, and the cursor stays on its plan: %+v", a.plans.Rows)
	}
	if _, err := os.Stat(plan.StatePath()); !os.IsNotExist(err) {
		t.Fatalf("start records no plan: %v", err)
	}
	a.ingest([]watch.Event{wrote(repo, newest)})
	if a.at != atOutline || a.plans.Current != newest || !a.follow {
		t.Fatalf("your agent's write opens it, following: at=%d current=%q", a.at, a.plans.Current)
	}
}

// Each harness reports a write its own way; every one of them opens the
// plan, and the screen changes only in PLAN, where the toast saying why
// waits for you (docs/ux.md §4).
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
		path := writePlan(t, repo, tc.name, demoPlan)
		evs := hook(t, tc.source, tc.payload(repo, path))
		if len(evs) == 0 {
			t.Fatalf("%s: nothing reached the feed", tc.source)
		}
		a.ingest(evs)
		if a.plans.Current != path || a.at != atOutline || !a.follow {
			t.Fatalf("%s %s: current=%q at=%d", tc.source, tc.name, a.plans.Current, a.at)
		}
		if a.pane != PaneChat {
			t.Fatalf("%s: an attach moved you: pane=%d", tc.source, a.pane)
		}
		says := "Your agent wrote Send-off demo · opened rex/plans/" + tc.name
		if s := a.toast; !a.toastLive() || s.Text != says || s.Kind != "text" || a.notice().Text != "" {
			t.Fatalf("%s: the toast that says why the plan changed waits for PLAN, in text, not the pin's yellow: %q %q", tc.source, s.Text, s.Kind)
		}
		if press(a, "\t"); a.notice().Text != says {
			t.Fatalf("%s: back on PLAN the notice row says why: %q", tc.source, a.notice().Text)
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
		if a.plans.Current != "" || a.at != atLibrary || a.pending != "" {
			t.Errorf("%s: current=%q at=%d pending=%q", c.name, a.plans.Current, a.at, a.pending)
		}
	}
	// A ".." is judged by where it leads: back into rex/plans is that plan.
	a := apps[0] // still on Plans
	sep := string(filepath.Separator)
	if a.ingest([]watch.Event{wrote(repo, PlansDir(repo)+sep+"sub"+sep+".."+sep+"real.rex.json")}); a.plans.Current != plan {
		t.Fatalf("current=%q", a.plans.Current)
	}
}

// The last plan written in a batch wins; a write of the open plan only
// reloads it, wherever you are in it. Another plan's write waits while you
// read a task's details, and opens once you are back on the list
// (docs/ux.md §11).
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
	// Following: the agent moves on to another plan, and Rex with it, once
	// you are back on the list.
	a.ingest([]watch.Event{wrote(repo, first)})
	if a.plans.Current != second || a.at != atTaskDetail || a.pending != first {
		t.Fatalf("details hold it: current=%q at=%d pending=%q", a.plans.Current, a.at, a.pending)
	}
	endToast(a) // second's Opened notice
	if got := a.statusLine().Text; got != "Your agent wrote Send-off demo (first) · opens when you're back on the list" {
		t.Fatalf("the notice says why it waits: %q", got)
	}
	press(a, "esc")
	if a.plans.Current != first || a.at != atOutline || !a.follow {
		t.Fatalf("current=%q at=%d", a.plans.Current, a.at)
	}
	press(a, "i") // About holds it too
	a.ingest([]watch.Event{wrote(repo, second)})
	if a.plans.Current != first || a.pending != second {
		t.Fatalf("About holds it: current=%q pending=%q", a.plans.Current, a.pending)
	}
	if press(a, "left"); a.plans.Current != second || a.at != atOutline {
		t.Fatalf("current=%q at=%d", a.plans.Current, a.at)
	}
}

// Latched: another plan's write is offered on the notice row until taken
// (docs/ux.md §4); o takes it and follows again, and enter never does.
func TestLatchedOffersTheNewPlan(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	repo := a.plans.Repo
	if a.follow || strings.Contains(a.frameRows().Rows[0], "following") {
		t.Fatalf("a plan picked in the list is latched, which shows no word\n%s", a.frame())
	}
	other := writePlan(t, repo, "cuj/next.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, other)})
	if a.plans.Current != path || a.at != atOutline {
		t.Fatalf("a latched plan is never yanked: current=%q", a.plans.Current)
	}
	c.t = c.t.Add(10 * toastFor)
	a.tick()
	const offer = "Your agent wrote Send-off demo (cuj/next) · o opens it"
	if got := a.statusLine().Text; got != offer {
		t.Fatalf("the notice offers the plan until it is taken: %q", got)
	}
	if footer := a.frameRows().Rows[a.lines-1]; strings.Contains(footer, "o ") || strings.Contains(footer, "switch") {
		t.Fatalf("o never joins the footer: %q", footer)
	}
	press(a, "esc")
	if got := a.statusLine().Text; got != offer {
		t.Fatalf("on Plans o opens it too: %q", got)
	}
	// Plans: Open latest is the plan your agent wrote, the newest, and says
	// so.
	now := time.Now().Add(time.Minute)
	os.Chtimes(other, now, now)
	a.plans.walk = planWalk{}
	a.plans.Rebuild()
	if body := strings.Join(a.frameRows().Rows, "\n"); !strings.Contains(body, "  Open latest     Send-off demo · new from your agent · 1/3 done") {
		t.Fatalf("Open latest names the agent's plan\n%s", body)
	}
	press(a, "\r") // Continue: the open plan, following again
	if a.offer != "" || a.plans.Current != path || !a.follow {
		t.Fatalf("Continue follows and settles the offer: offer=%q current=%q follow=%v", a.offer, a.plans.Current, a.follow)
	}
	press(a, "esc")
	typeText(a, "cuj/demo")
	press(a, "\r") // picked in a find: latched again
	if a.follow || a.plans.Current != path {
		t.Fatalf("a find's pick latches: follow=%v current=%q", a.follow, a.plans.Current)
	}
	a.ingest([]watch.Event{wrote(repo, other)})
	press(a, "s", "\r") // a letter does nothing on the outline; enter opens the task
	if a.plans.Current != path || a.at != atTaskDetail || a.offer == "" {
		t.Fatalf("enter never takes the offer: current=%q at=%d", a.plans.Current, a.at)
	}
	c.t = c.t.Add(toastFor) // reopening it said so on the notice row, over the offer
	press(a, "esc", "o")
	if a.plans.Current != other || !a.follow || a.offer != "" || a.at != atOutline {
		t.Fatalf("o opens it: current=%q follow=%v", a.plans.Current, a.follow)
	}
	if !strings.Contains(a.frameRows().Rows[0], " · following") {
		t.Fatalf("%s", a.frame())
	}
}

// Taking the offer opens the plan, so it never runs while that would drop
// what you typed in the plan list: a find, or a Name prompt. The notice
// stops naming o, and o does what it did before.
func TestTheOfferKeepsWhatYouTyped(t *testing.T) {
	offered := func(t *testing.T) (*app, string, string) {
		a, _, path := openDemo(t, demoPlan, 120, 32)
		return a, path, writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
	}
	noSwitch := func(t *testing.T, a *app) {
		t.Helper()
		if got := a.statusLine().Text; got != "Your agent wrote Send-off demo (cuj/next)" || strings.Contains(a.frame(), "o opens") {
			t.Fatalf("row 1 %q\n%s", got, a.frame())
		}
	}

	// A find half typed in the plan list, o on AGENT.
	a, path, next := offered(t)
	press(a, "esc")
	typeText(a, "half")
	press(a, "\t")
	a.ingest([]watch.Event{wrote(a.plans.Repo, next)})
	noSwitch(t, a)
	press(a, "o", "\r")
	if a.plans.Current != path || a.draft != "half" {
		t.Fatalf("the find was dropped: current=%q draft=%q", a.plans.Current, a.draft)
	}
	press(a, "\t")
	if typeText(a, "o"); a.draft != "halfo" || a.plans.Current != path {
		t.Fatalf("in a find o is a letter: draft=%q current=%q", a.draft, a.plans.Current)
	}
	press(a, "\x15", "\t") // clear it, back to AGENT
	if a.statusLine().Text != "Your agent wrote Send-off demo (cuj/next) · o opens it" {
		t.Fatalf("with nothing to lose the offer is back: %q", a.statusLine().Text)
	}
	if press(a, "o"); a.plans.Current != next || a.pane != PaneChat {
		t.Fatalf("current=%q pane=%d", a.plans.Current, a.pane)
	}

	// A Name prompt in the plan list, o on AGENT.
	a, path, next = offered(t)
	press(a, "esc")
	pickRow(a, PlanRowNew, "")
	press(a, "\r")
	if !a.plans.Prompting() {
		t.Fatal("setup: the Name prompt")
	}
	press(a, "\t")
	a.ingest([]watch.Event{wrote(a.plans.Repo, next)})
	noSwitch(t, a)
	if press(a, "o", "\r"); a.plans.Current != path || !a.plans.Prompting() {
		t.Fatalf("the Name prompt was dropped: current=%q", a.plans.Current)
	}
}

// From AGENT, o takes the offer for PLAN and leaves you on AGENT; enter
// never does.
func TestTakingTheOfferFromTheTape(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	other := writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
	press(a, "\t")
	a.ingest([]watch.Event{wrote(a.plans.Repo, other)})
	if press(a, "\r"); a.plans.Current == other || a.offer == "" {
		t.Fatalf("enter took the offer: current=%q", a.plans.Current)
	}
	press(a, "o")
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
	press(a, "o")
	if a.follow || a.plans.Current != demo || a.offer != "" || !strings.Contains(a.statusLine().Text, "cuj/gone is gone") {
		t.Fatalf("current=%q row 1 %q", a.plans.Current, a.statusLine().Text)
	}
}

// Text in Plans' field holds the attach until it clears. The task list,
// the tape and Help have no field, so they hold nothing: letters typed
// there build no hidden draft (docs/ux.md §2, §11).
func TestAttachWaitsForTheDraft(t *testing.T) {
	repo := attachRepo(t)
	a := startIn(repo)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, one)})
	if a.plans.Current != one {
		t.Fatal("setup: following one")
	}

	two := writePlan(t, repo, "two.rex.json", demoPlan)
	typeText(a, "xz")
	a.ingest([]watch.Event{wrote(repo, two)})
	if a.plans.Current != two || a.pending != "" || a.draft != "" || a.at != atOutline {
		t.Fatalf("letters on the outline hold nothing: current=%q pending=%q draft=%q", a.plans.Current, a.pending, a.draft)
	}

	press(a, "\t", "?") // Help on the tape
	typeText(a, "d")
	three := writePlan(t, repo, "three.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, three)})
	if a.plans.Current != three || a.overlay[PaneChat] != overlayHelp || a.pane != PaneChat {
		t.Fatal("Help and the tape hold nothing, and you stay on the tape")
	}

	press(a, "esc", "\t") // close Help, back to PLAN
	press(a, "esc")       // Plans, still following
	typeText(a, "q")      // a find
	a.ingest([]watch.Event{wrote(repo, two)})
	if a.plans.Current != three || a.at != atLibrary || a.pending != two {
		t.Fatalf("a find on Plans holds it: current=%q at=%d", a.plans.Current, a.at)
	}
	four := writePlan(t, repo, "four.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, four)})
	if a.pending != four {
		t.Fatal("a newer write replaces the held one")
	}
	press(a, "\t")
	a.ingest([]watch.Event{wrote(repo, two)})
	if a.plans.Current != three || a.pending != two {
		t.Fatal("Plans' find holds it from the tape too")
	}
	press(a, "\t", "\x15") // Ctrl+U
	if a.plans.Current != two || a.at != atOutline {
		t.Fatalf("the held write applies once the find is cleared: %q", a.plans.Current)
	}
}

// New plan is you making a plan: a write waits until you cancel it, and
// is offered on the notice row if you create one (docs/ux.md §4, §11). The
// notice says the prompt holds it, even an empty one.
func TestANamePromptHoldsTheAttach(t *testing.T) {
	repo := attachRepo(t)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	a := startIn(repo)
	press(a, "\r") // Open latest: following one
	endToast(a)
	press(a, "esc")
	pickRow(a, PlanRowNew, "")
	if a.at != atLibrary || a.plans.Rows[a.plans.Cursor].Kind != PlanRowNew {
		t.Fatal("setup: Create a plan")
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
	if got := a.statusLine().Text; got != "Your agent wrote Send-off demo (two) · opens when the prompt closes" {
		t.Fatalf("the line is clear, the prompt isn't closed: %q", got)
	}
	typeText(a, "x")
	if got := a.statusLine().Text; got != "Your agent wrote Send-off demo (two) · opens when the prompt closes" {
		t.Fatalf("a named prompt: %q", got)
	}
	a.settle(24, 24) // narrow, the notice keeps why it waits
	if got := strings.TrimRight(statusRow(a.statusLine(), a.layout().textW, false), " "); got != "new plan two · waiting" {
		t.Fatalf("narrow: %q", got)
	}
	a.settle(120, 32)
	press(a, "esc")
	if a.plans.Current != two || a.at != atOutline || a.plans.Prompting() {
		t.Fatalf("cancelling lets it through: current=%q at=%d prompting=%v", a.plans.Current, a.at, a.plans.Prompting())
	}

	press(a, "esc")
	pickRow(a, PlanRowNew, "")
	press(a, "\r")
	three := writePlan(t, repo, "three.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, three)})
	typeText(a, "mine")
	press(a, "\r")
	if a.plans.Current != filepath.Join(PlansDir(repo), "mine.rex.json") || a.follow || a.pending != "" || a.offer != three {
		t.Fatalf("creating latches and offers it: current=%q follow=%v pending=%q offer=%q", a.plans.Current, a.follow, a.pending, a.offer)
	}
	if got := a.statusLine().Text; got != "✔ Created rex/plans/mine.rex.json" {
		t.Fatalf("creating says so first: %q", got)
	}
	endToast(a)
	if got := a.statusLine().Text; got != "Your agent wrote Send-off demo (three) · o opens it" {
		t.Fatalf("notice %q", got)
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
	if a.plans.Current != "" || a.at != atLibrary || a.pending != "" {
		t.Fatalf("current=%q at=%d pending=%q", a.plans.Current, a.at, a.pending)
	}
}

// Picking a plan while a write is held makes it the notice row's offer,
// neither opened nor lost (docs/ux.md §4, §11); a pick in a find latches.
func TestLatchingOffersAHeldAttach(t *testing.T) {
	repo := attachRepo(t)
	alt := writePlan(t, repo, "alt.rex.json", demoPlan)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	two := filepath.Join(PlansDir(repo), "two.rex.json")
	a := startIn(repo)
	for _, pick := range []struct{ find, want string }{{"on", one}, {"al", alt}} {
		if press(a, "esc"); !a.follow {
			press(a, "\r", "esc") // Continue: following again
		}
		typeText(a, pick.find)
		writePlan(t, repo, "two.rex.json", demoPlan)
		a.ingest([]watch.Event{wrote(repo, two)})
		if a.pending != two {
			t.Fatal("setup: held")
		}
		press(a, "\r") // the first match
		if a.plans.Current != pick.want || a.follow || a.pending != "" || a.offer != two {
			t.Fatalf("%s: current=%q follow=%v pending=%q offer=%q", pick.find, a.plans.Current, a.follow, a.pending, a.offer)
		}
		if got := a.statusLine().Text; !strings.HasPrefix(got, "Opened Send-off demo · rex/plans/") {
			t.Fatalf("%s: the pick says what opened first: %q", pick.find, got)
		}
		endToast(a)
		a.settle(a.columns, a.lines)
		if a.plans.Current != pick.want || a.statusLine().Text != "Your agent wrote Send-off demo (two) · o opens it" {
			t.Fatalf("offered, not opened: current=%q notice %q", a.plans.Current, a.statusLine().Text)
		}
	}
	if press(a, "o"); a.plans.Current != two || !a.follow {
		t.Fatalf("o takes it and follows again: current=%q follow=%v", a.plans.Current, a.follow)
	}

	// The held write is the plan you pick: nothing left to offer.
	press(a, "esc")
	typeText(a, "on")
	writePlan(t, repo, "one.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, one)})
	if a.pending != one {
		t.Fatal("setup: held")
	}
	if press(a, "\r"); a.plans.Current != one || a.offer != "" || a.pending != "" {
		t.Fatalf("current=%q offer=%q pending=%q", a.plans.Current, a.offer, a.pending)
	}
}

// The QA scenario: find a plan, the agent writes another meanwhile, enter
// opens the one you found. Yours is latched and the agent's is offered on
// the notice row (docs/ux.md §4).
func TestPickingWhileHeldKeepsTheAgentsPlan(t *testing.T) {
	a, c, _ := cujApp(t)
	scratch := filepath.Join(a.plans.Root, "scratch.rex.json")
	os.WriteFile(scratch, plan.Stub("scratch"), 0o644)
	press(a, "esc")
	typeText(a, "scratch")
	other := filepath.Join(a.plans.Root, "agent-new.rex.json")
	os.WriteFile(other, plan.Stub("agent-new"), 0o644)
	a.ingest([]watch.Event{at(c, 0, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + other, Meta: map[string]any{"tool": "Write", "path": other, "ok": true, "cwd": a.plans.Repo}})})
	if a.pending != other {
		t.Fatal("setup: held")
	}
	press(a, "\r")
	if a.plans.Current != scratch || a.follow || a.offer != other {
		t.Fatalf("picked: current=%q follow=%v offer=%q", a.plans.Current, a.follow, a.offer)
	}
	c.t = c.t.Add(toastFor) // the pick's Opened notice first
	if got := a.statusLine().Text; got != "Your agent wrote agent-new · o opens it" {
		t.Fatalf("notice %q", got)
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

// docs/ux.md §4: row 0's meta says following in words while Rex follows
// the agent's writes; latched shows no word. Narrow, the meta's short form
// (the progress) stands in, before the places' label shortens.
func TestTitleSaysFollowing(t *testing.T) {
	a, _, _ := cujApp(t) // start follows the plan it lands on
	a.settle(120, 32)
	if got := textOf(a.frameRows().Rows[0], 120); got != "Send-off demo"+strings.Repeat(" ", 77-13-35)+"1/3 done · following   plan · agent" {
		t.Fatalf("row 0 following: %q", got)
	}
	a.settle(38, 24)
	if got := textOf(a.frameRows().Rows[0], 38); got != "Send-off demo      1/3   plan · agent" {
		t.Fatalf("row 0 at 38x24: %q", got)
	}
	a.settle(120, 32)
	a.latch()
	if got := textOf(a.frameRows().Rows[0], 120); got != "Send-off demo"+strings.Repeat(" ", 77-13-23)+"1/3 done   plan · agent" {
		t.Fatalf("row 0 latched: %q", got)
	}
	b := testApp(t)
	if strings.Contains(b.frameRows().Rows[0], "following") {
		t.Fatal("no plan, no word")
	}
}

// The old verbs are gone: on the tape, p, d, s, b and enter change
// nothing and write nothing (on Plans, letters find).
func TestOldVerbsDoNothingWithoutAPlan(t *testing.T) {
	for _, tab := range []bool{true} {
		repo := attachRepo(t)
		a := startIn(repo)
		if tab {
			press(a, "\t")
		}
		before, row1 := a.frame(), a.statusLine().Text
		for _, word := range []string{"d", "s", "p", "b", "o", "done", "sendoff", "pin load", "brief", "x"} {
			typeText(a, word)
			press(a, "\r", PastePrefix+word)
		}
		if a.frame() != before || a.draft != "" || a.statusLine().Text != row1 || a.notice().Text != "" || len(a.log) != 0 {
			t.Fatalf("tape %v: something changed\n%s", tab, a.frame())
		}
		if _, err := os.Stat(a.statePath); !os.IsNotExist(err) {
			t.Fatalf("tape %v: state.json was written", tab)
		}
	}
}

// docs/ux.md §4: the plan file's error and a toast outrank a new-plan
// offer on the notice row, hard drift sits under it, and o opens it only
// while the notice shows it; under anything else o does nothing, and
// enter never takes it.
func TestNoticeRanksTheOffer(t *testing.T) {
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
	const offer = "Your agent wrote Send-off demo (cuj/next) · o opens it"
	hidden := func(t *testing.T, a *app, path, want string) {
		t.Helper()
		if got := a.notice().Text; !strings.HasPrefix(got, want) || strings.Contains(a.frame(), "o opens it") {
			t.Fatalf("notice %q, want %q\n%s", got, want, a.frame())
		}
		press(a, "o", "\r") // o does nothing here; enter is the outline's own
		if a.plans.Current != path || a.at != atTaskDetail || a.offer == "" {
			t.Fatalf("an offer the notice didn't show was taken: current=%q at=%d", a.plans.Current, a.at)
		}
		press(a, "esc")
	}

	// A plan reload's toast, then the offer once it expires.
	a, c, path, next := offered(t)
	os.WriteFile(path, []byte(strings.Replace(demoPlan, `"status": "pending"`, `"status": "skipped"`, 1)), 0o644)
	settled(a, c)
	hidden(t, a, path, "The plan changed · load-outline → skipped")
	c.t = c.t.Add(toastFor)
	if got := a.notice().Text; got != offer {
		t.Fatalf("the offer is back once the toast is gone: %q", got)
	}
	if press(a, "o"); a.plans.Current != next || !a.follow {
		t.Fatalf("current=%q follow=%v", a.plans.Current, a.follow)
	}

	// Hard drift sits under the offer, and shows once it is settled.
	a, _, _, _ = offered(t, "README.md")
	if s, ok := a.driftStatus(); !ok || a.notice().Text != offer {
		t.Fatalf("hard drift %q (%v) is under the offer: %q", s.Text, ok, a.notice().Text)
	}
	press(a, "esc", "\r") // Continue, the open plan, settles the offer
	endToast(a)           // and says it opened
	if got := a.notice().Text; a.offer != "" || got != "off pin · README.md · outside card-highlight files" {
		t.Fatalf("notice %q", got)
	}

	// Soft drift is AGENT's only.
	a, _, _, _ = offered(t, "internal/tui/live.go")
	press(a, "esc", "\r")
	endToast(a)
	if got := a.notice().Text; a.offer != "" || got != "" || a.drift.soft == nil {
		t.Fatalf("soft drift on the notice row: %q", got)
	}

	// The plan file can't be read.
	a, c, path, _ = offered(t)
	os.WriteFile(path, []byte("{"), 0o644)
	later(a, c)
	later(a, c)
	hidden(t, a, path, "✗ plan JSON error")
}

// docs/ux.md §11: nothing on the task list or the tape latches a plan,
// not even p and d, which write a task's status (§7b); only a pick on Plans
// other than Continue and Open latest does. A followed plan keeps
// following whatever you press there, and its file changes only in the
// status values p and d write.
func TestOnlyThePlanListLatches(t *testing.T) {
	repo := attachRepo(t)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	a := startIn(repo)
	press(a, "\r") // Open latest: following
	before, _ := os.ReadFile(one)
	press(a, "end")
	for _, k := range []string{"p", "\r", "esc", "d", "\r", "esc", "s", "\r", "esc", "b", "\r", "esc", "\t", "p", "d", "\r", "\t"} {
		press(a, k)
		if k == "esc" && a.at == atLibrary {
			press(a, "\r") // Continue: the open plan, following
		}
	}
	after, _ := os.ReadFile(one)
	if !a.follow || !onlyStatuses(string(before), string(after)) || string(after) == string(before) {
		t.Fatalf("follow=%v; the plan file:\n%s", a.follow, after)
	}
	if p, _ := plan.Load(one); p.Task("load-outline").Status != plan.Done || p.Task("card-highlight").Status != plan.Pending {
		t.Fatalf("p pinned load-outline and d finished it:\n%s", after)
	}
	two := writePlan(t, repo, "two.rex.json", demoPlan)
	if a.ingest([]watch.Event{wrote(repo, two)}); a.plans.Current != two {
		t.Fatalf("still following: current=%q", a.plans.Current)
	}
	press(a, "esc")
	typeText(a, "one")
	press(a, "\r")
	if a.follow || a.plans.Current != one {
		t.Fatalf("picking one in a find latches: follow=%v current=%q", a.follow, a.plans.Current)
	}
}

// docs/ux.md §11: enter on Continue or Open latest opens its plan
// following, wherever it was latched before; a Recent plan, a find's
// match or one in Open a plan latches.
func TestContinueAndOpenLatestFollow(t *testing.T) {
	repo := attachRepo(t)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	two := writePlan(t, repo, "team/two.rex.json", demoPlan)
	a := startIn(repo)
	press(a, "\r") // Open latest
	if a.plans.Current != two || a.at != atOutline || !a.follow || !strings.Contains(textOf(a.frameRows().Rows[0], a.columns), " · following") {
		t.Fatalf("Open latest follows: current=%q follow=%v\n%s", a.plans.Current, a.follow, a.frame())
	}
	press(a, "esc")
	if a.plans.Rows[a.plans.Cursor].Kind != PlanRowContinue {
		t.Fatal("esc lands on Continue, the plan you left")
	}
	pickRow(a, PlanRowPlan, "one.rex.json") // under Recent
	press(a, "\r")
	if a.plans.Current != one || a.follow {
		t.Fatalf("a Recent plan latches: current=%q follow=%v", a.plans.Current, a.follow)
	}
	press(a, "esc", "\r") // Continue
	if a.plans.Current != one || !a.follow {
		t.Fatalf("Continue follows: current=%q follow=%v", a.plans.Current, a.follow)
	}
	three := writePlan(t, repo, "three.rex.json", demoPlan)
	if a.ingest([]watch.Event{wrote(repo, three)}); a.plans.Current != three {
		t.Fatalf("still following: %q", a.plans.Current)
	}
	press(a, "esc")
	pickRow(a, PlanRowOpen, "")
	press(a, "\r")
	pickRow(a, PlanRowDir, "team")
	press(a, "\r", "\r")
	if a.plans.Current != two || a.follow {
		t.Fatalf("a plan in Open a plan latches: current=%q follow=%v", a.plans.Current, a.follow)
	}
}

// RunLoop applies a read's keys before the events polled with it: a key
// acts on the plan you saw, and a draft typed in that read holds the
// attach those events bring.
func TestKeysActBeforeTheEventsReadWithThem(t *testing.T) {
	repo := attachRepo(t)
	one := writePlan(t, repo, "one.rex.json", demoPlan)
	a := startIn(repo)
	press(a, "\r", "esc") // following one, back on Plans
	two := writePlan(t, repo, "two.rex.json", demoPlan)
	if a.step([]string{"p", "i"}, []watch.Event{wrote(repo, two)}); a.plans.Current != one || a.pending != two || a.draft != "pi" {
		t.Fatalf("the find holds it: current=%q pending=%q draft=%q", a.plans.Current, a.pending, a.draft)
	}
	press(a, "esc")
	if a.plans.Current != two || a.at != atOutline {
		t.Fatalf("clearing the find lets it through: current=%q at=%d", a.plans.Current, a.at)
	}
	if a.step([]string{"\r"}, []watch.Event{wrote(repo, one)}); a.plans.Current != two || a.at != atTaskDetail || a.pending != one {
		t.Fatalf("enter opened two's task detail, which holds one: current=%q at=%d", a.plans.Current, a.at)
	}
	if press(a, "esc"); a.plans.Current != one || a.at != atOutline {
		t.Fatalf("back on the list, one opens: current=%q at=%d", a.plans.Current, a.at)
	}

	// Latched: enter read with the write that makes the offer opens the task
	// you were on; the offer waits on the notice row.
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

// A held attach says so on the notice row, and in a 38-cell text column
// keeps a clipped name (docs/ux.md §4). Letters on the tape hold nothing:
// there is no text field there (docs/ux.md §2).
func TestAHeldAttachShowsOnRowOne(t *testing.T) {
	repo := attachRepo(t)
	writePlan(t, repo, "one.rex.json", demoPlan)
	a := startIn(repo)
	endToast(a)
	typeText(a, "pi")
	two := writePlan(t, repo, "team/a-much-longer-plan-name.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, two)})
	if got := a.notice().Text; got != "Your agent wrote Send-off demo (team/a-much-longer-plan-name) · opens when the field is clear" {
		t.Fatalf("notice %q", got)
	}
	a.settle(38, 24)
	if row := textOf(a.frameRows().Rows[22], 38); !strings.HasPrefix(row, "Your agent wrote …") || !strings.HasSuffix(row, "-name · waiting") {
		t.Fatalf("the notice row at 38 columns: %q", row)
	}
	press(a, "\x15")
	if a.plans.Current != two || strings.Contains(a.notice().Text, "waiting") || strings.Contains(a.notice().Text, "opens when") {
		t.Fatalf("current=%q notice %q", a.plans.Current, a.notice().Text)
	}

	w := startIn(attachRepo(t))
	press(w, "\t")
	typeText(w, "pin load-outline")
	p := writePlan(t, w.plans.Repo, "p.rex.json", demoPlan)
	w.ingest([]watch.Event{wrote(w.plans.Repo, p)})
	if w.plans.Current != p || w.pending != "" || w.at != atOutline || w.draft != "" {
		t.Fatalf("the tape: current=%q pending=%q draft=%q", w.plans.Current, w.pending, w.draft)
	}
}

// docs/ux.md §3, §11: Plans is the top of the Esc ladder, with a plan
// open or not: Esc clears a find, then goes nowhere, and neither does ←.
func TestEscOnPlansGoesNowhere(t *testing.T) {
	a := testApp(t)
	if a.at != atLibrary || !strings.Contains(a.frame(), "Or ask your agent to plan the work.") {
		t.Fatalf("Plans with no plans says the agent can write one\n%s", a.frame())
	}
	typeText(a, "x")
	press(a, "esc")
	if a.at != atLibrary || a.draft != "" || a.plans.Find != "" {
		t.Fatal("Esc clears the find first")
	}
	before := a.frame()
	if press(a, "esc", "left"); a.at != atLibrary || a.frame() != before {
		t.Fatalf("Esc and ← go nowhere from Plans\n%s", a.frame())
	}

	b, _, _ := openDemo(t, demoPlan, 120, 32)
	press(b, "esc", "esc", "left")
	if b.at != atLibrary || b.plans.Screen != ScreenHome || b.plans.Rows[b.plans.Cursor].Kind != PlanRowContinue {
		t.Fatalf("with a plan open, Plans is the top too: at=%d", b.at)
	}
	if strings.Contains(b.frame(), "Or ask your agent") {
		t.Fatal("the hint is for a first run")
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
	press(a, "\r") // Open latest: following one
	two := writePlan(t, repo, "two.rex.json", demoPlan)
	if a.ingest([]watch.Event{patch(repo, two, one)}); a.plans.Current != two {
		t.Fatalf("following: %q", a.plans.Current)
	}
	press(a, "esc")
	typeText(a, "pi") // a find on Plans holds the next write
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
	writePlan(t, repo, "other.rex.json", demoPlan)
	a := startIn(repo)
	typeText(a, "alias")
	press(a, "\r")
	if a.plans.Current != alias || a.follow {
		t.Fatalf("setup: latched on the link: %q", a.plans.Current)
	}
	if a.ingest([]watch.Event{wrote(repo, alias), wrote(repo, real)}); a.offer != "" {
		t.Fatalf("offered the plan on screen: %q", a.offer)
	}

	// On real, a write through the link keeps the selection, and picking
	// the link in a find keeps it too: it is the plan already open.
	writePlan(t, repo, "real.rex.json", demoPlan) // newest again
	b := startIn(repo)
	press(b, "\r", "end") // Open latest: real
	sel := b.outline.sel
	press(b, "esc")
	typeText(b, "alias")
	press(b, "\r")
	if !samePlan(b.plans.Current, real) || b.outline.sel != sel {
		t.Fatalf("setup: on real with the selection kept: %q sel %q -> %q", b.plans.Current, sel, b.outline.sel)
	}
	if b.ingest([]watch.Event{wrote(repo, alias)}); !samePlan(b.plans.Current, real) || b.outline.sel != sel || strings.Contains(b.statusLine().Text, "following") {
		t.Fatalf("opened again: current=%q sel %q -> %q", b.plans.Current, sel, b.outline.sel)
	}

	// An offer that has become the open plan: o only follows again.
	c, _, path := openDemo(t, demoPlan, 120, 32)
	next := writePlan(t, c.plans.Repo, "cuj/next.rex.json", demoPlan)
	c.ingest([]watch.Event{wrote(c.plans.Repo, next)})
	os.Remove(next)
	if err := os.Symlink(path, next); err != nil {
		t.Fatal(err)
	}
	press(c, "down")
	sel = c.outline.sel
	if press(c, "o"); c.plans.Current != path || !c.follow || c.offer != "" || c.outline.sel != sel || c.at != atOutline {
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
		press(d, "\r", "esc") // following one, back on Plans
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
	press(a, "\r", "down") // Open latest, following
	sel := a.outline.sel
	if a.ingest([]watch.Event{wrote(repo, upper)}); a.plans.Current != path || a.outline.sel != sel || strings.Contains(a.statusLine().Text, "following") {
		t.Fatalf("following: opened again: current=%q sel %q -> %q", a.plans.Current, sel, a.outline.sel)
	}
	b, _, demo := openDemo(t, demoPlan, 120, 32)
	if b.ingest([]watch.Event{wrote(b.plans.Repo, filepath.Join(filepath.Dir(demo), "Demo.rex.json"))}); b.offer != "" {
		t.Fatalf("latched: offered the plan on screen: %q", b.offer)
	}
}

// docs/ux.md §11: Plans goes by the file, not the spelling. The open
// plan, written in another case or reached through a link, is Continue,
// once, by the name the folder gives it, and Esc lands on it.
func TestOpenPlanIsListedOnceByFile(t *testing.T) {
	once := func(t *testing.T, a *app, path, name string) {
		t.Helper()
		row := a.plans.Rows[a.plans.Cursor]
		if a.at != atLibrary || row.Kind != PlanRowContinue || row.Name != name {
			t.Fatalf("Esc lands on Continue %s: at=%d on %+v", name, a.at, row)
		}
		n := 0
		for _, row := range a.plans.Rows {
			if row.Path != "" && samePlan(row.Path, path) {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("listed %d times\n%s", n, a.frame())
		}
	}

	a, _, _ := cujApp(t)
	next := writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
	upper := filepath.Join(a.plans.Root, "CUJ", "Next.rex.json")
	if _, err := os.Stat(upper); err == nil {
		// Windows resolves the write to the file's own case (EvalSymlinks),
		// macOS keeps the agent's: either way it is one plan, listed once.
		if a.ingest([]watch.Event{wrote(a.plans.Repo, upper)}); a.plans.Current != upper && a.plans.Current != next {
			t.Fatalf("setup: the agent's spelling, or the file's own, is open: %q", a.plans.Current)
		}
		press(a, "esc")
		once(t, a, next, "cuj/next.rex.json")
	}

	// A hard link is one file under two names too: listed once, under the
	// open plan's name, and counted once.
	h, _, hdemo := cujApp(t)
	if err := os.Link(hdemo, filepath.Join(h.plans.Root, "hard.rex.json")); err == nil {
		h.plans.walk = planWalk{}
		press(h, "esc")
		once(t, h, hdemo, "cuj/sendoff-demo.rex.json")
		if meta := spanText(libraryPage(h.plans).Short); meta != "1 plan" {
			t.Fatalf("a hard link counted twice: %q", meta)
		}
	}

	b, _, demo := cujApp(t)
	alias := filepath.Join(b.plans.Root, "alias.rex.json")
	if err := os.Symlink(demo, alias); err != nil {
		t.Skip("no symlinks here (Windows without the privilege)")
	}
	b.plans.walk = planWalk{}
	press(b, "esc")
	once(t, b, demo, "cuj/sendoff-demo.rex.json")
	if title := textOf(b.frameRows().Rows[0], b.columns); !strings.HasSuffix(title, " · 1 plan   plan · agent") {
		t.Fatalf("one plan, counted once: %q", title)
	}
	// Latched on the link, the file keeps the link's name, and a find
	// still matches it by that name.
	typeText(b, "alias")
	press(b, "\r", "esc")
	once(t, b, demo, "alias.rex.json")
}

// With plans off, Plans' first run says so and how to turn them on, and
// offers nothing else; outside git it says where plans belong, and the
// Create a plan card names the folder. The notice is row 1 (docs/ux.md
// §4), the rest the body.
func TestFirstRunFollowsWherePlansGo(t *testing.T) {
	a := testApp(t)
	os.WriteFile(filepath.Join(a.plans.Repo, "rex"), []byte("x"), 0o644)
	a.plans.Rebuild()
	off := viewText(BuildPlanView(a.plans, 300))
	if a.context().Text != rexFileNotice || strings.Contains(off, rexFileNotice) || !strings.Contains(off, "Move it to turn plans on") {
		t.Fatalf("plans off: row 1 %q\n%s", a.context().Text, off)
	}
	for _, never := range []string{"Create a plan", "ask your agent", "install-skill"} {
		if strings.Contains(off, never) {
			t.Fatalf("plans off still says %q:\n%s", never, off)
		}
	}

	out := t.TempDir() // not a git repo
	b := newApp("yellow", false, "idle", NewPlanBrowser(out), 120, 32)
	body := viewText(BuildPlanView(b.plans, 300))
	if !strings.Contains(body, "❯ Create a plan   Rex makes "+homePath(PlansDir(out))+"/ for it\n\n  Start Rex from your project's folder") || b.context().Text != "Not in a git repo · plans go in "+homePath(PlansDir(out))+"/" {
		t.Fatalf("outside git: row 1 %q\n%s", b.context().Text, body)
	}
	if strings.Contains(body, "install-skill") {
		t.Fatalf("outside git it says where plans belong instead\n%s", body)
	}
}

// docs/ux.md §11: the first run's skill hint is never clipped: the
// command wraps whole under its lead, on a row of its own.
func TestFirstRunHintIsNeverClipped(t *testing.T) {
	a := testApp(t)
	for _, c := range []skill.Copy{skill.Missing, skill.Stale, skill.StaleProject} {
		a.plans.skill = c
		_, cmd, _ := strings.Cut(skillHint(c), " · ")
		for width := MinTextCols; width <= 120; width++ {
			body := a.plans.firstRun(width, 40, false)
			for _, line := range body {
				if cellLen(line.Text) > width-1 {
					t.Errorf("copy %d, %d cells: %q is clipped", c, width, line.Text)
				}
			}
			if width >= cellLen(cmd)+3 && !strings.Contains(viewText(body), ":\n  "+cmd+"\n") {
				t.Errorf("copy %d, %d cells: the command isn't whole under its lead\n%s", c, width, viewText(body))
			}
		}
		// As drawn, at 62 and 70 text cells (with the mascot and without):
		// the whole command, never a "…".
		for _, cols := range []int{62, 62 + 42, 70 + 42} {
			a.settle(cols, 24)
			a.skill.got, a.skill.at = c, a.now()
			for _, row := range a.frameRows().Rows {
				if strings.Contains(row, "rex ingest") && (!strings.Contains(row, cmd) || strings.Contains(row, "…")) {
					t.Errorf("copy %d at %d columns: %q", c, cols, strings.TrimSpace(row))
				}
			}
		}
	}
	if got := wrapPath("see /a/very-long-folder-name/x", 12); strings.Join(got, "|") != "see|/a/|…older-name/|x" {
		t.Errorf("wrapPath: %q", got)
	}
}

// docs/ux.md §11: on a first run Plans says how to install the rex-plan
// skill only while Rex's copy is nowhere an agent reads it, and an install
// from another terminal shows within a few seconds.
func TestFirstRunHintGoesOnceTheSkillIsInstalled(t *testing.T) {
	repo := attachRepo(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := os.Getenv("HOME")
	a := startIn(repo)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	const hint = "Agents learn the plan format from the rex-plan skill:"
	rows := func() []string {
		c.t = c.t.Add(3*time.Second + time.Millisecond) // past the lookup's cache
		var out []string
		for _, r := range a.frameRows().Rows {
			out = append(out, strings.TrimSpace(textOf(r, a.columns)))
		}
		return out
	}
	shows := func() bool { return slices.Contains(rows(), hint) }
	if a.at != atLibrary || !shows() || !slices.Contains(rows(), "rex ingest --install-skill") {
		t.Fatalf("with the skill nowhere, the hint shows: at=%d\n%s", a.at, a.frame())
	}
	for _, dir := range append(skill.Dirs(home, a.plans.Repo, false), skill.Dirs(home, a.plans.Repo, true)...) {
		if err := skill.Install(dir, io.Discard); err != nil {
			t.Fatal(err)
		}
		if shows() {
			t.Fatalf("installed at %s, the hint is gone\n%s", dir, a.frame())
		}
		if err := skill.Uninstall(dir, io.Discard); err != nil {
			t.Fatal(err)
		}
		if !shows() {
			t.Fatalf("uninstalled from %s, the hint is back", dir)
		}
	}
	own := filepath.Join(skill.Dirs(home, a.plans.Repo, false)[0], "SKILL.md")
	os.MkdirAll(filepath.Dir(own), 0o755)
	os.WriteFile(own, []byte("my own skill"), 0o644)
	if !shows() {
		t.Fatal("a SKILL.md Rex didn't write teaches no plans: the hint stays")
	}
	os.Remove(own)

	// An older Rex's copy still teaches its own workflow: the hint says to
	// replace it, with --project when the old copy is the repo's.
	old := []byte("<!-- generated by rex -->\nThe person marks tasks done in Rex.\n")
	says := func(cmd string) {
		t.Helper()
		got := rows()
		if !slices.Contains(got, "The rex-plan skill your agents read is out of date. Update it:") || !slices.Contains(got, cmd) || slices.Contains(got, hint) {
			t.Fatalf("want %q\n%s", cmd, a.frame())
		}
	}
	project := filepath.Join(skill.Dirs(home, a.plans.Repo, true)[1], "SKILL.md")
	for _, path := range []string{own, project} {
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, old, 0o644)
	}
	says("rex ingest --install-skill")
	for _, dir := range skill.Dirs(home, a.plans.Repo, false) {
		skill.Install(dir, io.Discard)
	}
	says("rex ingest --install-skill --project")
	skill.Install(filepath.Dir(project), io.Discard)
	if c.t = c.t.Add(3*time.Second + time.Millisecond); strings.Contains(a.frame(), "rex-plan skill") {
		t.Fatalf("every copy is this Rex's: no hint\n%s", a.frame())
	}
}

// docs/ux.md §11: with plans, Plans has no first run's hint, so an
// upgrader with an older Rex's skill hears of it on the notice row once,
// as a toast at start (Plans' own notice is row 1, docs/ux.md §4), and
// Help keeps a skill row; a current skill, or none, says nothing.
func TestStartSaysTheSkillIsOutOfDate(t *testing.T) {
	repo := attachRepo(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := os.Getenv("HOME")
	writePlan(t, repo, "cuj/demo.rex.json", demoPlan)
	start := func() *app {
		a := startIn(repo)
		if a.at != atLibrary {
			t.Fatalf("start lands on Plans: at=%d", a.at)
		}
		return a
	}
	if got := start().statusLine().Text; strings.Contains(got, "skill") {
		t.Fatalf("no skill at all is no news on row 1: %q", got)
	}
	old := []byte("<!-- generated by rex -->\nThe person marks tasks done in Rex (d⏎).\n")
	for _, dir := range skill.Dirs(home, repo, false) {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "SKILL.md"), old, 0o644)
	}
	a := start()
	if s := a.statusLine(); s.Text != "rex-plan skill is out of date · rex ingest --install-skill" || s.Kind != "text" {
		t.Fatalf("the notice on the first frame, in text: %q %q", s.Text, s.Kind)
	}
	if row := textOf(a.frameRows().Rows[30], a.columns); row != "rex-plan skill is out of date · rex ingest --install-skill" {
		t.Fatalf("drawn on the notice row: %q", row)
	}
	a.settle(38, 24)
	if row := textOf(a.frameRows().Rows[22], 38); row != "rex-plan skill out of date · ? help" {
		t.Fatalf("38 cells: %q", row)
	}
	press(a, "?")
	if help := strings.Join(strings.Fields(viewText(a.helpBody(78))), " "); !strings.Contains(help, "rex-plan out of date · rex ingest --install-skill") {
		t.Fatalf("Help keeps the skill row\n%s", help)
	}

	// An old rex-plans/ folder's notice too: the skill toast wins the
	// notice row, and Plans' row 1 still says the notice.
	os.MkdirAll(filepath.Join(repo, "rex-plans"), 0o755)
	os.WriteFile(filepath.Join(repo, "rex-plans", "old.rex.json"), []byte(demoPlan), 0o644)
	a = start()
	if got := a.statusLine().Text; got != "rex-plan skill is out of date · rex ingest --install-skill" {
		t.Fatalf("the notice with a Library notice too: %q", got)
	}
	if a.at != atLibrary || !strings.Contains(a.context().Text, "rex-plans/ has 1 old plan(s)") {
		t.Fatalf("Plans keeps the notice: at=%d row 1 %q", a.at, a.context().Text)
	}
	os.RemoveAll(filepath.Join(repo, "rex-plans"))

	// The repo's copy is the old one: --project.
	for _, dir := range skill.Dirs(home, repo, false) {
		skill.Install(dir, io.Discard)
	}
	project := skill.Dirs(home, repo, true)[1]
	os.MkdirAll(project, 0o755)
	os.WriteFile(filepath.Join(project, "SKILL.md"), old, 0o644)
	if got := start().statusLine().Text; got != "rex-plan skill is out of date · rex ingest --install-skill --project" {
		t.Fatalf("row 1: %q", got)
	}
	skill.Install(project, io.Discard)
	a = start()
	if got := a.statusLine().Text; strings.Contains(got, "skill") {
		t.Fatalf("every copy current: %q", got)
	}
	if help := strings.Join(strings.Fields(viewText(a.helpBody(78))), " "); !strings.Contains(help, "rex-plan current") {
		t.Fatalf("Help says it is current\n%s", help)
	}
}

// docs/ux.md §11: on a short screen the first run sheds its blank rows,
// then how your agent's plan gets here, then the long skill hint for the
// short one, which never goes; the Create a plan card stays on top.
func TestFirstRunSheds(t *testing.T) {
	a := testApp(t)
	a.plans.skill = skill.Missing
	body := func(rows int, short bool) string { return viewText(a.plans.firstRun(38, rows, short)) }
	full := len(a.plans.firstRun(38, 40, false))
	steps := []struct {
		rows       int
		short      bool
		want, gone []string
	}{
		{40, false, []string{"\n  Or ask your agent", "Agents learn the plan format", "\n  rex ingest --install-skill\n"}, nil},
		{full - 1, false, []string{"Or ask your agent", "Agents learn the plan format"}, []string{"\n\n"}},
		{40, true, []string{"Or ask your agent", "Agents learn the plan format"}, []string{"\n\n"}},
		{3, true, []string{"Agents learn the plan format"}, []string{"Or ask your agent"}},
		{1, true, []string{"  teach it:\n  rex ingest --install-skill\n"}, []string{"Agents learn"}},
	}
	for _, s := range steps {
		got := body(s.rows, s.short)
		if strings.HasPrefix(got, "\n") && s.short {
			t.Errorf("%d rows, short: a blank row\n%s", s.rows, got)
		}
		for _, w := range s.want {
			if !strings.Contains("\n"+got, w) {
				t.Errorf("%d rows: %q missing\n%s", s.rows, w, got)
			}
		}
		for _, w := range s.gone {
			if strings.Contains(got, w) {
				t.Errorf("%d rows: %q still there\n%s", s.rows, w, got)
			}
		}
	}
	for _, size := range [][2]int{{24, 7}, {38, 7}, {38, 12}, {80, 10}, {80, 24}} {
		a.settle(size[0], size[1])
		a.skill.got, a.skill.at = skill.Missing, a.now()
		frame := a.frameRows().Rows
		if !strings.HasPrefix(textOf(frame[2+a.layout().blank], size[0]), "❯ Create a plan") || !strings.Contains(strings.Join(frame, "\n"), "--install-skill") {
			t.Errorf("%d×%d: the card on top, and the command\n%s", size[0], size[1], strings.Join(frame, "\n"))
		}
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

// docs/ux.md §4: the offer's notice drops the plan's title first, then
// clips its path from the left while 5 cells are free for it, then drops
// its tail's long form, and only then its lead.
func TestTheOfferKeepsAClippedName(t *testing.T) {
	named := wroteAs{title: "Send-off demo", path: "cuj/sendoff-demo"}
	bare := wroteAs{path: "cuj/sendoff-demo"}
	const o = " · o opens it"
	cases := []struct {
		width       int
		name        wroteAs
		tail, short string
		want        string
	}{
		{78, named, o, o, "Your agent wrote Send-off demo (cuj/sendoff-demo) · o opens it"},
		{50, named, o, o, "Your agent wrote cuj/sendoff-demo · o opens it"},
		{38, named, o, o, "Your agent wrote …ff-demo · o opens it"},
		{34, named, o, o, "new plan …endoff-demo · o opens it"},
		{24, bare, o, o, "new plan · o opens it"},
		{38, bare, " · opens when the field is clear", " · waiting", "Your agent wrote …ndoff-demo · waiting"},
		{20, bare, " · opens when the prompt closes", " · waiting", "new plan · waiting"},
		{30, wroteAs{path: "x"}, "", "", "Your agent wrote x"},
	}
	for _, tc := range cases {
		if got := fitWrote(tc.name, tc.tail, tc.short, tc.width); got != tc.want || cellLen(got) > tc.width {
			t.Errorf("%d %+v: got %q, want %q", tc.width, tc.name, got, tc.want)
		}
	}
	a, _, _ := openDemo(t, demoPlan, 38, 24)
	next := writePlan(t, a.plans.Repo, "cuj/a-much-longer-plan-name-from-the-agent.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(a.plans.Repo, next)})
	a.toast = StatusLine{}
	if row := strings.TrimSpace(textOf(a.frameRows().Rows[22], 38)); !strings.HasPrefix(row, "Your agent wrote …") || !strings.HasSuffix(row, "-agent · o opens it") {
		t.Fatalf("the notice at 38 columns: %q", row)
	}
}

// Latched on Plans, a plan the agent writes shows up there at once, as
// Open latest marked new from your agent, so the notice row never names a
// plan Plans can't open (docs/ux.md §11); the cursor stays on Continue.
func TestTheListShowsAPlanTheAgentWrote(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	press(a, "esc")
	next := writePlan(t, a.plans.Repo, "cuj/zzz-brand-new.rex.json", demoPlan)
	now := time.Now().Add(time.Minute) // written just now: the newest
	os.Chtimes(next, now, now)
	a.ingest([]watch.Event{wrote(a.plans.Repo, next)})
	if row := rowWith(a.frameRows(), "Open latest"); a.plans.Rows[a.plans.Cursor].Kind != PlanRowContinue || !strings.HasPrefix(row, "  Open latest     Send-off demo · new from your agent · 1/3 done") {
		t.Fatalf("cursor on %v, Open latest %q\n%s", a.plans.Rows[a.plans.Cursor].Kind, row, a.frame())
	}
	if press(a, "down", "down", "down", "\r"); a.plans.Current != next || !a.follow {
		t.Fatalf("Open latest opens it, following: %q follow=%v", a.plans.Current, a.follow)
	}
}

// docs/ux.md §3: on Plans, with the field empty, o opens the plan the
// notice offers; after other text, and in Open a plan, it is a letter.
func TestOOpensTheOfferFromThePlanList(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 120, 32)
	next := writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(a.plans.Repo, next)})
	press(a, "esc")
	typeText(a, "n")
	if a.draft != "n" || a.notice().Text != "Your agent wrote Send-off demo (cuj/next)" {
		t.Fatalf("with text, o would be a letter, and the notice doesn't name it: %q %q", a.draft, a.notice().Text)
	}
	if typeText(a, "o"); a.draft != "no" || a.plans.Current != path {
		t.Fatalf("o after text is text: %q %q", a.draft, a.plans.Current)
	}
	press(a, "\x15")
	a.plans.browse(a.plans.Root)
	if typeText(a, "o"); a.draft != "o" || a.plans.Current != path {
		t.Fatalf("in Open a plan o filters: %q %q", a.draft, a.plans.Current)
	}
	press(a, "\x15", "esc")
	if a.plans.Screen != ScreenHome || a.notice().Text != "Your agent wrote Send-off demo (cuj/next) · o opens it" {
		t.Fatalf("back on Plans with the field empty: %d %q", a.plans.Screen, a.notice().Text)
	}
	press(a, "o")
	if a.plans.Current != next || !a.follow || a.at != atOutline || a.draft != "" {
		t.Fatalf("o opens it, following: %q follow=%v at=%d draft=%q", a.plans.Current, a.follow, a.at, a.draft)
	}
}
