package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/boot"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/watch"
)

// wire has feed health read the given wiring, now and on every re-read:
// the send state reads it (docs/ux.md §7a), and the test's HOME has
// nothing wired.
func wire(a *app, statuses ...watch.HarnessStatus) {
	a.health.statusesFor = func(string) []watch.HarnessStatus { return statuses }
	a.health.statuses, a.health.read = statuses, a.now()
}

// wired is a harness Rex is wired into, which hands its agent the pin.
func wired(name string) watch.HarnessStatus {
	return watch.HarnessStatus{Name: name, Present: true, Wired: true, Prompts: true}
}

// prompted is a prompt of the agent in cwd as rex ingest records it, with
// the stamps of what it carried from the repo its note came from (pin,
// then pin_plan; "" leaves one out, and with neither, the repo too).
func prompted(cwd, text, pin, rel string) watch.Event {
	meta := map[string]any{}
	if cwd != "" {
		meta["cwd"] = cwd
		if pin != "" || rel != "" {
			repo, _ := plan.FindRepo(cwd)
			meta["pin_repo"] = repo
		}
	}
	if pin != "" {
		meta["pin"] = pin
	}
	if rel != "" {
		meta["pin_plan"] = rel
	}
	return watch.Event{Source: "claude", Kind: "user", Text: text, TS: float64(time.Now().UnixNano()) / 1e9, Meta: meta}
}

const cujRel = "rex/plans/cuj/sendoff-demo.rex.json"

// docs/ux.md §4, §7a: the send state comes from the feed. Only a prompt
// of this repo's agent that rex ingest stamped with this pin and this
// plan says sent: another repo's prompt, even one naming a plan at the
// same path with the same pin, or one from nowhere known, never does; a
// newer prompt without the stamp, or one that carried another task or
// plan, takes it back; and once the pin moves, the last prompt carried
// the old one.
func TestSendStateFromTheFeed(t *testing.T) {
	a, c, path := cujApp(t)
	repo := a.plans.Repo
	if got := a.sendState(); got != noHarness {
		t.Fatalf("nothing wired: %q", got)
	}
	wire(a, wired("claude"))
	if got := a.sendState(); got != sendsNext {
		t.Fatalf("wired, no prompt yet: %q", got)
	}
	other := t.TempDir()
	os.Mkdir(filepath.Join(other, ".git"), 0o755)
	writePlan(t, other, "cuj/sendoff-demo.rex.json", string(cujFixture(t)))
	for _, step := range []struct {
		name string
		ev   watch.Event
		want string
	}{
		{"another repo's, the same pin and path", prompted(other, "elsewhere", "card-highlight", cujRel), sendsNext},
		{"from nowhere known", prompted("", "no origin", "card-highlight", cujRel), sendsNext},
		{"this repo's, stamped", prompted(repo, "carry on", "card-highlight", cujRel), sentLast},
		{"a newer one, not stamped (a missed deadline)", prompted(repo, "and then?", "", ""), sendsNext},
		{"stamped again", prompted(repo, "go on", "card-highlight", cujRel), sentLast},
		{"another task", prompted(repo, "other task", "load-outline", cujRel), sendsNext},
		{"another plan of this repo's", prompted(repo, "other plan", "card-highlight", "rex/plans/cuj/other.rex.json"), sendsNext},
		{"a new plan's line, no pin", prompted(repo, "no pin", "", cujRel), sendsNext},
		{"a path out of the repo", prompted(repo, "climbs", "card-highlight", "../"+filepath.Base(other)+"/"+cujRel), sendsNext},
		{"this pin, from a folder in this repo", prompted(filepath.Join(repo, "internal"), "from below", "card-highlight", cujRel), sentLast},
	} {
		a.ingest([]watch.Event{step.ev})
		if got := a.sendState(); got != step.want {
			t.Fatalf("%s: %q, want %q", step.name, got, step.want)
		}
	}
	if a.hidden != 1 {
		t.Fatalf("another repo's prompt is hidden, and counted: %d", a.hidden)
	}
	// Your agent moves the pin: the last prompt carried the old one.
	os.WriteFile(path, []byte(mustReplace(t, mustReplace(t, string(cujFixture(t)), `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],`), `"status": "pending",
      "depends_on": ["card-highlight"],`, `"status": "active",
      "depends_on": ["card-highlight"],`)), 0o644)
	later(a, c)
	if pin := a.doc.plan.Graph().Pin(); pin == nil || pin.ID != "load-outline" || a.sendState() != sendsNext {
		t.Fatalf("the pin moved: %v %q", pin, a.sendState())
	}
	a.ingest([]watch.Event{prompted(repo, "next", "load-outline", cujRel)})
	if got := a.sendState(); got != sentLast {
		t.Fatalf("then the next prompt carries it: %q", got)
	}
}

// docs/ux.md §7a: where the pin can't go, the send state says so and never
// more. No wired harness, or only an old OpenCode plugin: not sent · ?
// help. A Cursor install without sessionStart, or Claude Code wired for
// this repo only, still sends it. A plan Rex can't read now, one that
// isn't state.json's last plan here, or a read-only one: not sent. A
// prompt that carried it stays sent.
func TestSendStateWhereItCantGo(t *testing.T) {
	a, c, path := cujApp(t)
	for _, tc := range []struct {
		name     string
		statuses []watch.HarnessStatus
		want     string
	}{
		{"none found", nil, noHarness},
		{"found, not wired", []watch.HarnessStatus{{Name: "codex", Present: true}}, noHarness},
		{"an old plugin only", []watch.HarnessStatus{oldPlugin()}, noHarness},
		{"an old plugin and Cursor without sessionStart", []watch.HarnessStatus{oldPlugin(), {Name: "cursor", Present: true, Wired: true, Prompts: true, Partial: true}}, sendsNext},
		{"Claude Code for this repo", []watch.HarnessStatus{{Name: "claude", Present: true, Here: true, Prompts: true}}, sendsNext},
		{"Claude Code wired, but not on its prompts", []watch.HarnessStatus{{Name: "claude", Present: true, Wired: true}}, noHarness},
	} {
		wire(a, tc.statuses...)
		if got := a.sendState(); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	wire(a, wired("claude"))
	a.tick() // the re-read cujApp's clock has made due: read on the test's clock
	// Another Rex opened another plan here: only that one sends (A5). The
	// re-read waits for healthTTL, and says the state changed.
	other := writePlan(t, a.plans.Repo, "cuj/other.rex.json", string(cujFixture(t)))
	if err := plan.UpdateState(a.statePath, c.t, func(s *plan.State) {
		s.Repos[a.plans.Repo] = plan.RepoState{Plan: other, Opened: float64(c.t.Unix() + 1)}
	}); err != nil {
		t.Fatal(err)
	}
	if a.tick(); a.sendState() != sendsNext {
		t.Fatal("state.json is re-read every few seconds, not every frame")
	}
	c.t = c.t.Add(healthTTL)
	if !a.tick() || a.sendState() != notSent {
		t.Fatalf("not the plan Rex last opened here: %q", a.sendState())
	}
	press(a, "down", "p")
	if got := a.toast.Text; !strings.HasPrefix(got, "Pinned load-outline · not sent") {
		t.Fatalf("p never says it goes: %q", got)
	}
	press(a, "up", "p", "esc") // the pin back on card-highlight
	a.plans.pick(PlanRowContinue)
	press(a, "\r") // opening it makes it the last plan again
	if got := a.sendState(); got != sendsNext {
		t.Fatalf("opened again: %q", got)
	}
	// A prompt carried it, then the file stops parsing: it was sent, and
	// nothing more goes until it reads again.
	good, _ := os.ReadFile(path)
	a.ingest([]watch.Event{carried(a, "carry on")})
	os.WriteFile(path, []byte("{"), 0o644)
	later(a, c)
	later(a, c)
	if _, bad := docStatus(a.doc); !bad || a.sendState() != sentLast {
		t.Fatalf("unreadable, after it went: %v %q", bad, a.sendState())
	}
	a.ingest([]watch.Event{prompted(a.plans.Repo, "and now?", "", "")})
	if got := a.sendState(); got != notSent {
		t.Fatalf("unreadable: the hook sends nothing: %q", got)
	}
	os.WriteFile(path, good, 0o644)
	later(a, c)
	if got := a.sendState(); got != sendsNext {
		t.Fatalf("readable again: %q", got)
	}
	os.WriteFile(path, []byte(strings.Replace(string(good), `"version": 1`, `"version": 2`, 1)), 0o644)
	later(a, c)
	if a.doc.plan.ReadOnly == "" || a.sendState() != notSent {
		t.Fatalf("read-only sends nothing: %q", a.sendState())
	}
}

// docs/ux.md §7a (a new plan): with no tasks yet, the plan's name is what
// a prompt carries, and the send state says so too; a prompt that
// carried a pin of it (it had tasks then) isn't that line.
func TestSendStateOfANewPlan(t *testing.T) {
	a, _, _ := cujApp(t)
	wire(a, wired("codex"))
	press(a, "esc")
	typeText(a, "scratch")
	press(a, "\r")
	if a.at != atOutline || len(a.doc.plan.Tasks) != 0 {
		t.Fatal("setup: a new empty plan")
	}
	row := func() string { return textOf(a.frameRows().Rows[1], a.columns) }
	if got := row(); got != sides("nothing pinned · no tasks yet", "sends with next prompt", 78) {
		t.Fatalf("row 1: %q", got)
	}
	rel := "rex/plans/scratch.rex.json"
	a.ingest([]watch.Event{prompted(a.plans.Repo, "plan scratch", "a", rel)})
	if got := a.sendState(); got != sendsNext {
		t.Fatalf("a pin isn't the new plan's line: %q", got)
	}
	a.ingest([]watch.Event{prompted(a.plans.Repo, "plan this", "", rel)})
	if got := row(); got != sides("nothing pinned · no tasks yet", "sent with last prompt", 78) {
		t.Fatalf("row 1: %q", got)
	}
}

// docs/ux.md §10, §7a: the prompt before Rex started counts: warm start
// sees what it carried.
func TestSendStateFromBeforeRexStarted(t *testing.T) {
	a, _, _ := cujApp(t)
	b := newApp("yellow", false, "idle", NewPlanBrowser(a.plans.Repo), 120, 32)
	wire(b, wired("claude"))
	// As RunLoop does: the feed's last events, then the person opens the
	// plan (the first one opened only sets what changes count from).
	b.warmStart([]watch.Event{carried(a, "carry on"), {Source: "claude", Kind: "text", Text: "On it.", Meta: map[string]any{"cwd": a.plans.Repo}}}, time.Now())
	b.plans.open(a.plans.Current)
	b.openedPlan()
	if got := b.sendState(); got != sentLast {
		t.Fatalf("%q", got)
	}
}

// docs/ux.md §4: row 1 of the task list and of the pin's details at 24,
// 38, 60 and 79 cells, in each state. The send state sits at the right
// end, whole; the pin's title clips first to keep it, down to 8 cells,
// then it goes, before the pin line loses its id.
func TestSendStateFitsRowOne(t *testing.T) {
	type want struct{ list, details [4]string }
	cases := map[string]want{
		sendsNext: {
			[4]string{"pinned  card-highlight", "pinned  card-highlight · Selected pla…", "pinned  card-highlight · Selected p…  sends with next prompt", "pinned  card-highlight · Selected plan card is one blo…  sends with next prompt"},
			[4]string{"task 2 of 3", sides("task 2 of 3", "sends with next prompt", 39), sides("task 2 of 3", "sends with next prompt", 61), sides("task 2 of 3", "sends with next prompt", 80)},
		},
		sentLast: {
			[4]string{"pinned  card-highlight", "pinned  card-highlight · Selected pla…", "pinned  card-highlight · Selected pl…  sent with last prompt", sides("pinned  card-highlight · Selected plan card is one block", "sent with last prompt", 80)},
			[4]string{"task 2 of 3", sides("task 2 of 3", "sent with last prompt", 39), sides("task 2 of 3", "sent with last prompt", 61), sides("task 2 of 3", "sent with last prompt", 80)},
		},
		noHarness: {
			[4]string{"pinned  card-highlight", "pinned  card-highlight · Selected pla…", "pinned  card-highlight · Selected plan c…  not sent · ? help", sides("pinned  card-highlight · Selected plan card is one block", "not sent · ? help", 80)},
			[4]string{"task 2 of 3", sides("task 2 of 3", "not sent · ? help", 39), sides("task 2 of 3", "not sent · ? help", 61), sides("task 2 of 3", "not sent · ? help", 80)},
		},
		notSent: {
			list: [4]string{"nothing pinned · next u…", "nothing pinned · next up load-outline", "nothing pinned · next up load-outline · Outline v…  not sent", sides("nothing pinned · next up load-outline · Outline view", "not sent", 80)},
		},
	}
	widths := [4]int{24, 38, 60, 79}
	for state, w := range cases {
		a, _, _ := cujApp(t)
		switch state {
		case sendsNext:
			wire(a, wired("claude"))
		case sentLast:
			wire(a, wired("claude"))
			a.ingest([]watch.Event{carried(a, "go")})
		case notSent:
			wire(a, wired("claude"))
			press(a, "d")
		}
		if got := a.sendState(); got != state {
			t.Fatalf("setup: %q, want %q", got, state)
		}
		check := func(where string, wants [4]string) {
			for i, cells := range widths {
				row := statusRow(a.context(), cells+1, false)
				if cellLen(row) != cells+1 || !strings.HasSuffix(row, " ") {
					t.Errorf("%s, %s, %d cells: %d cells wide, or not a cell short of the edge: %q", state, where, cells, cellLen(row), row)
				}
				if got := strings.TrimRight(row, " "); got != wants[i] {
					t.Errorf("%s, %s, %d cells:\n got %q\nwant %q", state, where, cells, got, wants[i])
				}
			}
		}
		check("the task list", w.list)
		if state == notSent {
			continue
		}
		press(a, "\r")
		check("the pin's details", w.details)
		// A task that isn't the pin has no send state on its details.
		press(a, "esc", "down", "\r")
		if got := strings.TrimRight(statusRow(a.context(), 80, false), " "); got != "task 3 of 3" {
			t.Errorf("%s: another task's details: %q", state, got)
		}
	}
	// Painted, the state is muted and the pin's id keeps its yellow.
	a, _, _ := cujApp(t)
	wire(a, wired("claude"))
	painted := statusRow(a.context(), 80, true)
	if muted := mascot.FG(mascot.UI["muted"]); !strings.Contains(painted, mascot.FG(mascot.UI["accent"])+"card-highlight") || !strings.Contains(painted, muted+"  sends with next prompt") && !strings.Contains(painted, muted+"sends with next prompt") {
		t.Fatalf("%q", painted)
	}
}

// docs/ux.md §10: under a prompt that carried the pin, AGENT says so, in
// the prompt box's column: "with pin <id>", muted, the id in the pin's
// yellow; a new plan's line, "with plan <name>". It comes from the
// stamps, never the prompt's words, and a prompt with no text of its own
// draws as one.
func TestAgentSaysWhatAPromptCarried(t *testing.T) {
	log := []watch.Event{
		prompted("", "carry on", "card-highlight", cujRel),
		prompted("", "a prompt that says with pin fake-id in its words", "", ""),
		prompted("", "plan this", "", "rex/plans/team/auth.rex.json"),
		prompted("", watch.NoText, "card-highlight", cujRel),
	}
	text := viewText(BuildLogView(log, 60, viewOpt{tall: true}))
	want := `                                     carry on
                                     with pin card-highlight

                                 a prompt that says with pin
                                 fake-id in its words

                                         plan this
                                         with plan team/auth

                                     (no text)
                                     with pin card-highlight`
	if got := trimmed(text); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if strings.Count(text, "with pin") != 3 {
		t.Fatal("only the stamps say what a prompt carried")
	}
	// Painted: "with pin" muted, the id the pin's yellow.
	lines := BuildLogView(log[:1], 60, viewOpt{})
	painted := paintLine(lines[len(lines)-1], 60, true)
	if !strings.Contains(painted, mascot.FG(mascot.UI["muted"])+"with pin ") || !strings.Contains(painted, mascot.FG(mascot.UI["accent"])+"card-highlight") {
		t.Fatalf("%q", painted)
	}
	// Narrow, it clips inside the box's column, at most half the row.
	for _, w := range []int{16, 24, 38} {
		for _, l := range BuildLogView(log[:1], w, viewOpt{}) {
			if row := plainRow(l, w); cellLen(row) > w {
				t.Errorf("%d cells: %q", w, row)
			}
		}
		if last := BuildLogView(log[:1], w, viewOpt{}); !strings.Contains(plainRow(last[len(last)-1], w), "with pi") {
			t.Errorf("%d cells: %q", w, plainRow(last[len(last)-1], w))
		}
	}
}

// docs/ux.md §10: feed health has a row per harness saying whether it
// hands your agent the pin, or the one command that makes it.
func TestFeedHealthSaysWhoGetsThePin(t *testing.T) {
	cursor := watch.HarnessStatus{Name: "cursor", Present: true, Wired: true, Prompts: true, Caveat: "on 3.20+ (older: once per chat)"}
	partial := cursor
	partial.Partial = true
	noPrompt := wired("claude")
	noPrompt.Prompts = false
	codex := wired("codex")
	codex.Caveat = "once trusted in Codex's /hooks"
	for _, tc := range []struct {
		s    watch.HarnessStatus
		want string
	}{
		{wired("claude"), "claude wired · sends the pin with each prompt"},
		{watch.HarnessStatus{Name: "claude", Present: true, Here: true, Prompts: true}, "claude wired for this repo · sends the pin with each prompt"},
		{watch.HarnessStatus{Name: "claude", Present: true, Here: true}, "claude wired for this repo · rerun rex ingest --install claude --project"},
		{noPrompt, "claude wired · rerun rex ingest --install claude"},
		{codex, "codex wired · sends the pin with each prompt once trusted in Codex's /hooks"},
		{cursor, "cursor wired · sends the pin with each prompt on 3.20+ (older: once per chat)"},
		{partial, "cursor wired · rerun rex ingest --install cursor"},
		{oldPlugin(), "opencode wired · old plugin, no pin: rex ingest --install opencode"},
		{watch.HarnessStatus{Name: "codex", Present: true}, "codex not wired · rex ingest --install codex"},
		{watch.HarnessStatus{Name: "codex"}, "codex not found"},
	} {
		h := feedHealth{path: "/x/session.jsonl", statuses: []watch.HarnessStatus{tc.s}, read: time.Now()}
		for _, w := range []int{24, 38, 78} {
			lines := h.lines(w, 0, skill.Current, time.Now())
			text := viewText(lines)
			if got := strings.Join(strings.Fields(text), " "); !strings.Contains(got, tc.want) {
				t.Errorf("%d cells: want %q\n%s", w, tc.want, text)
			}
			for _, l := range strings.Split(text, "\n") {
				if cellLen(l) > w {
					t.Errorf("%d cells: %q", w, l)
				}
			}
		}
	}
	// Help says what the states mean, and FEED which command fixes one.
	a, _, _ := cujApp(t)
	wire(a, oldPlugin())
	press(a, "?")
	help := strings.Join(strings.Fields(viewText(a.helpBody(78))), " ")
	for _, want := range []string{"YOUR AGENT AND THE PIN", "sent your last prompt carried the pin", "sends not sent yet: it goes with your next prompt", "not sent nothing is pinned", "with pin under a prompt: the pin it carried", "opencode wired · old plugin, no pin: rex ingest --install opencode"} {
		if !strings.Contains(help, want) {
			t.Errorf("Help has %q\n%s", want, help)
		}
	}
	if got := sides("pinned  card-highlight · Selected plan card is one block", "not sent · ? help", 79); textOf(a.frameRows().Rows[1], a.columns) == got {
		t.Fatal("Help has its own row 1")
	}
	press(a, "esc")
	if got := textOf(a.frameRows().Rows[1], a.columns); got != sides("pinned  card-highlight · Selected plan card is one block", "not sent · ? help", 78) {
		t.Fatalf("an old plugin hands over nothing: %q", got)
	}
}

// setupRex is a git repo at dir whose plan p.rex.json pins setup, open in
// a Rex started there, with Cursor wired.
func setupRex(t *testing.T, dir string) *app {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	writePlan(t, dir, "p.rex.json", `{"version": 1, "title": "P", "tasks": [{"id": "setup", "title": "Set up", "status": "active"}]}`)
	a := newApp("yellow", false, "idle", NewPlanBrowser(dir), 80, 24)
	a.now = (&clock{t: time.Unix(1_800_000_000, 0)}).now
	if press(a, "\r"); a.at != atOutline || a.sendState() == "" {
		t.Fatalf("setup: the plan opens: at=%d", a.at)
	}
	wire(a, wired("cursor"))
	return a
}

// setupNote is the prompt rex ingest records when the note it printed was
// setup's from the repo at repo: the prompt itself came from origin.
func setupNote(repo string, origin map[string]any) watch.Event {
	meta := map[string]any{"pin": "setup", "pin_plan": "rex/plans/p.rex.json", "pin_repo": repo}
	for k, v := range origin {
		meta[k] = v
	}
	return watch.Event{Source: "cursor", Kind: "user", Text: "carry on", TS: float64(time.Now().UnixNano()) / 1e9, Meta: meta}
}

// sentHere is a's send state and whether AGENT draws the pin under the
// prompt, after ev.
func sentHere(a *app, ev watch.Event) (string, bool) {
	a.ingest([]watch.Event{ev})
	return a.sendState(), strings.Contains(viewText(a.logView(60)), "with pin setup")
}

// docs/ux.md §7a: only a prompt stamped with this repo (pin_repo) counts
// as sent here, and only it gets "with pin" on AGENT: a prompt from a
// workspace with this repo and another, from a repo inside this one or
// around it, or from a folder above it, may be this repo's by where it
// came from and carry another repo's pin of the same id at the same path.
// A prompt that carried this repo's pin counts wherever it came from.
func TestOnlyThisReposStampsCount(t *testing.T) {
	// Two roots, both pinning setup: the note was the first's.
	a, b := setupRex(t, t.TempDir()), setupRex(t, t.TempDir())
	roots := map[string]any{"roots": []any{a.plans.Repo, b.plans.Repo}}
	if state, tag := sentHere(a, setupNote(a.plans.Repo, roots)); state != sentLast || !tag {
		t.Errorf("multi-root, the root whose note it was: %q %v", state, tag)
	}
	if state, tag := sentHere(b, setupNote(a.plans.Repo, roots)); state != sendsNext || tag {
		t.Errorf("multi-root, the other root: %q %v", state, tag)
	}
	if !strings.Contains(viewText(b.logView(60)), "carry on") {
		t.Error("the prompt still shows there, as one of its agent's")
	}

	// A repo inside another: each prompt's note is the repo it came from.
	root := t.TempDir()
	outer := setupRex(t, root)
	inner := setupRex(t, filepath.Join(root, "sub"))
	below := setupNote(inner.plans.Repo, map[string]any{"cwd": filepath.Join(inner.plans.Repo, "internal")})
	if state, tag := sentHere(outer, below); state != sendsNext || tag {
		t.Errorf("nested, a prompt from inside, in the outer repo: %q %v", state, tag)
	}
	if state, tag := sentHere(inner, below); state != sentLast || !tag {
		t.Errorf("nested, a prompt from inside, in its repo: %q %v", state, tag)
	}
	above := setupNote(outer.plans.Repo, map[string]any{"cwd": outer.plans.Repo})
	if state, tag := sentHere(inner, above); state != sendsNext {
		t.Errorf("nested, a prompt from outside, in the inner repo: %q %v", state, tag)
	}
	if state, tag := sentHere(outer, above); state != sentLast || !tag {
		t.Errorf("nested, a prompt from outside, in its repo: %q %v", state, tag)
	}

	// An agent in a folder above the repo, outside git: its note was that
	// folder's.
	parent := t.TempDir()
	in := setupRex(t, filepath.Join(parent, "a"))
	if state, tag := sentHere(in, setupNote(parent, map[string]any{"cwd": parent})); state != sendsNext || tag {
		t.Errorf("from the folder above: %q %v", state, tag)
	}

	// This repo's note, from a workspace root elsewhere ($CURSOR_PROJECT_DIR
	// found the pin): hidden from AGENT as another folder's, and sent.
	home := setupRex(t, t.TempDir())
	if state, _ := sentHere(home, setupNote(home.plans.Repo, map[string]any{"roots": []any{t.TempDir()}})); state != sentLast || home.hidden != 1 {
		t.Errorf("this repo's note from elsewhere: %q, %d hidden", state, home.hidden)
	}
	// An older Rex's stamp names no repo: never counted, never drawn.
	old := setupNote(home.plans.Repo, map[string]any{"cwd": home.plans.Repo})
	delete(old.Meta, "pin_repo")
	if state, tag := sentHere(home, old); state != sendsNext || tag {
		t.Errorf("no pin_repo: %q %v", state, tag)
	}
}

// docs/ux.md §6, §7a: the line under the pin's title says what row 1 says,
// and another task's what pinning it would do, never more than happened;
// the page follows the state as it changes.
func TestDetailsSayWhatRowOneSays(t *testing.T) {
	a, c, path := cujApp(t)
	note := func(key string) string { return a.detailNote(key) }
	page := func() string { press(a, "\r"); defer press(a, "esc"); return planBody(a) }
	check := func(name, pin, other string) {
		t.Helper()
		if got := note("card-highlight"); got != pin {
			t.Errorf("%s: the pin: %q, want %q", name, got, pin)
		}
		if got := note("load-outline"); got != other {
			t.Errorf("%s: another task: %q, want %q", name, got, other)
		}
		if body := page(); !strings.Contains(body, "  "+pin+"\n") {
			t.Errorf("%s: the page says it\n%s", name, body)
		}
	}
	check("nothing wired", "Not sent: no wired agent takes the pin · ? help", "Pin it (p) to send these fields; no wired agent takes them now · ? help")
	wire(a, wired("claude"))
	check("wired", "These fields go to your agent with your next prompt.", "Pin it (p) and these fields go to your agent with your next prompt.")
	a.ingest([]watch.Event{carried(a, "carry on")})
	check("sent", "Your agent got these fields with your last prompt.", "Pin it (p) and these fields go to your agent with your next prompt.")
	a.ingest([]watch.Event{prompted(a.plans.Repo, "and now?", "", "")})
	good, _ := os.ReadFile(path)
	os.WriteFile(path, []byte("{"), 0o644)
	later(a, c)
	later(a, c)
	check("unreadable", "This plan sends your agent nothing: Rex can't read the file now.", "This plan sends your agent nothing: Rex can't read the file now.")
	os.WriteFile(path, good, 0o644)
	later(a, c)
	other := writePlan(t, a.plans.Repo, "cuj/other.rex.json", string(good))
	plan.UpdateState(a.statePath, c.t, func(s *plan.State) {
		s.Repos[a.plans.Repo] = plan.RepoState{Plan: other, Opened: float64(c.t.Unix() + 1)}
	})
	c.t = c.t.Add(healthTTL)
	a.tick()
	check("not the last plan", "This plan sends your agent nothing: it isn't the plan Rex last opened here.", "This plan sends your agent nothing: it isn't the plan Rex last opened here.")
	// Help says p and the pinned mark the same way.
	help := strings.Join(strings.Fields(viewText(helpLines(80, "tasks", false))), " ")
	for _, want := range []string{"p pin this task, or unpin the pin; a wired agent gets the pin with each prompt, and row 1 says whether it went", "pinned the task you (p) or your agent set active: a wired agent gets it with each prompt, and row 1 says whether it went"} {
		if !strings.Contains(help, want) {
			t.Errorf("Help: %q\n%s", want, help)
		}
	}
	if strings.Contains(help, "with every prompt") {
		t.Error("Help never says your agent gets it with every prompt")
	}
}

// docs/ux.md §7a, through rex ingest itself: a Cursor build before 3.20
// gets the note but doesn't read it, and its prompt says so, so row 1 says
// not sent · ? help even with Cursor wired, until a build that reads it
// carries it; a pin moved meanwhile doesn't change what the build is.
func TestAnOldCursorBuildSaysNotSent(t *testing.T) {
	if IngestRun == nil {
		t.Fatal("ingest_test.go links in rex ingest")
	}
	a, _, _ := cujApp(t)
	wire(a, wired("cursor"))
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	was := boot.At
	defer func() { boot.At = was }()
	run := func(version, text string) {
		t.Helper()
		data, _ := json.Marshal(map[string]any{"hook_event_name": "beforeSubmitPrompt", "prompt": text, "workspace_roots": []any{a.plans.Repo}, "conversation_id": "c1", "cursor_version": version})
		boot.At = time.Now()
		var out bytes.Buffer
		if code := IngestRun([]string{"ingest", "--source", "cursor", "--events", feed}, bytes.NewReader(data), &out, &bytes.Buffer{}); code != 0 || !strings.Contains(out.String(), "card-highlight") {
			t.Fatalf("%s: %d %q", version, code, out.String())
		}
		a.ingest(watch.LastEvents(feed, 1))
	}
	if got := a.sendState(); got != sendsNext {
		t.Fatalf("setup: %q", got)
	}
	run("3.2.11", "carry on")
	if got := a.sendState(); got != noHarness || a.detailNote("card-highlight") != "Not sent: your agent's build didn't read it with your last prompt · ? help" {
		t.Fatalf("an old build: %q / %q", got, a.detailNote("card-highlight"))
	}
	if strings.Contains(viewText(a.logView(60)), "with pin") {
		t.Fatal("and AGENT draws no pin under it")
	}
	press(a, "p", "p") // unpin, pin again
	if got := a.sendState(); got != noHarness || !strings.Contains(a.toast.Text, "not sent · ? help") {
		t.Fatalf("the pin moved, the build didn't: %q / %q", got, a.toast.Text)
	}
	run("3.20.17", "carry on, again")
	if got := a.sendState(); got != sentLast {
		t.Fatalf("a build that reads it: %q", got)
	}
}

// docs/ux.md §7a: only a prompt after the last change of what the plan
// sends counts as sent: p p on the pin, your agent moving the pin away and
// back, another plan opened and this one again. Row 1 and p's notice
// then agree, and the next prompt is sent again.
func TestOnlyAPromptSinceThePinChangedCounts(t *testing.T) {
	a, c, path := cujApp(t)
	wire(a, wired("claude"))
	send := func() {
		t.Helper()
		a.ingest([]watch.Event{carried(a, fmt.Sprintf("carry on %d", c.t.UnixNano()))})
		if got := a.sendState(); got != sentLast {
			t.Fatalf("a prompt carries it: %q", got)
		}
	}
	send()
	press(a, "p")
	if got := a.sendState(); got != notSent {
		t.Fatalf("unpinned: %q", got)
	}
	press(a, "p")
	if got := a.sendState(); got != sendsNext || a.toast.Text != "Pinned card-highlight · sends with your next prompt" {
		t.Fatalf("pinned again: row 1 %q, notice %q", got, a.toast.Text)
	}
	send()
	// Your agent moves the pin to load-outline, then back.
	good, _ := os.ReadFile(path)
	moved := strings.NewReplacer(`"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "pending",
      "depends_on": ["plans-ui"],`, `"status": "pending",
      "depends_on": ["card-highlight"],`, `"status": "active",
      "depends_on": ["card-highlight"],`).Replace(string(good))
	for _, src := range []string{moved, string(good)} {
		os.WriteFile(path, []byte(src), 0o644)
		later(a, c)
	}
	if got := a.sendState(); got != sendsNext {
		t.Fatalf("your agent moved it away and back: %q", got)
	}
	send()
	// A save that moves no pin changes nothing.
	os.WriteFile(path, []byte(strings.Replace(string(good), "Outline view", "Outline view, again", 1)), 0o644)
	later(a, c)
	if got := a.sendState(); got != sentLast {
		t.Fatalf("a save that keeps the pin: %q", got)
	}
	// Another plan, then this one again.
	other := writePlan(t, a.plans.Repo, "cuj/other.rex.json", string(good))
	for _, p := range []string{other, a.plans.Current} {
		a.plans.open(p)
		a.openedPlan()
	}
	if got := a.sendState(); got != sendsNext {
		t.Fatalf("another plan opened, then this one: %q", got)
	}
	send()
}

// The send state's re-reads hold whatever the clock does (finding: a test
// clock behind real time froze them): a read stamped later than now is due
// again, for state.json and the harnesses' wiring alike.
func TestReReadsSurviveAClockThatGoesBack(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, tc := range []struct {
		read time.Time
		due  bool
	}{{time.Time{}, true}, {now, false}, {now.Add(-healthTTL + time.Millisecond), false}, {now.Add(-healthTTL), true}, {now.Add(time.Hour), true}} {
		if got := stale(tc.read, now); got != tc.due {
			t.Errorf("read %v: due %v, want %v", tc.read.Sub(now), got, tc.due)
		}
	}
	a, c, _ := cujApp(t)
	wire(a, wired("claude"))
	reads := 0
	a.health.statusesFor = func(string) []watch.HarnessStatus { reads++; return []watch.HarnessStatus{wired("claude")} }
	a.sent.read, a.health.read = c.t.Add(time.Hour), c.t.Add(time.Hour) // stamped by a clock ahead of this one
	other := writePlan(t, a.plans.Repo, "cuj/other.rex.json", string(cujFixture(t)))
	plan.UpdateState(a.statePath, c.t, func(s *plan.State) {
		s.Repos[a.plans.Repo] = plan.RepoState{Plan: other, Opened: float64(c.t.Unix() + 1)}
	})
	if !a.tick() || a.sendState() != notSent || reads != 1 {
		t.Fatalf("re-read at once: %q, %d reads", a.sendState(), reads)
	}
}
