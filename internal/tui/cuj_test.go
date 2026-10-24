package tui

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/watch"
)

// cujFixture is the CUJ plan, a copy of rex/plans/cuj/sendoff-demo.rex.json,
// so trying Rex on the repo's own demo can't change what the tests see.
func cujFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "sendoff-demo.rex.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// mustReplace is s with old replaced once. A fixture edit that doesn't
// apply fails the test, so a fixture that changed (or a CRLF checkout of
// it) never runs a test on the wrong bytes.
func mustReplace(t *testing.T, s, old, new string) string {
	t.Helper()
	if !strings.Contains(s, old) {
		t.Fatalf("fixture edit didn't apply: %q", old)
	}
	return strings.Replace(s, old, new, 1)
}

// cujApp is Rex started in a repo that holds the CUJ plan: start attaches
// it, so it opens on the outline (docs/ux.md §11).
func cujApp(t *testing.T) (*app, *clock, string) {
	t.Helper()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(PlansDir(repo), "cuj", "sendoff-demo.rex.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, cujFixture(t), 0o644)
	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 120, 32)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	return a, c, path
}

// keys counts keystrokes so the loop can be held to the budget in
// docs/ux.md §14.
type keys struct {
	a *app
	n int
}

func (k *keys) press(ks ...string) { k.n += len(ks); press(k.a, ks...) }
func (k *keys) typ(s string)       { k.n += len([]rune(s)); typeText(k.a, s) }
func (k *keys) spent(t *testing.T, budget int, what string) {
	t.Helper()
	if k.n > budget {
		t.Errorf("%s took %d keys, budget %d", what, k.n, budget)
	}
	k.n = 0
}

// The journey (docs/ux.md §14): Rex waits → the agent writes a plan and
// Rex opens it → the agent pins a task → an off-pin edit shows drift →
// the agent marks the task done → Tab to the tape and back → Esc to the
// plan list, where picking a plan latches → quit. The agent drives the
// plan; Rex only shows it and never writes it.
func TestCUJWholeLoop(t *testing.T) {
	a := testApp(t)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	k := &keys{a: a}
	repo := a.plans.Repo
	path := filepath.Join(a.plans.Root, "cuj", "sendoff-demo.rex.json")
	agent := func(body []byte) { // the agent edits the plan with its file tool
		os.WriteFile(path, body, 0o644)
		a.ingest([]watch.Event{at(c, 0, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + path, Meta: map[string]any{"tool": "Write", "path": path, "ok": true, "cwd": repo}})})
		later(a, c)
	}

	if a.pane != PanePlans || a.at != atWait || !strings.Contains(a.frame(), "no plan yet · tell your agent to make one") || strings.Contains(a.frame(), "╭") {
		t.Fatalf("A. with no plan yet, start waits for one, with no composer\n%s", a.frame())
	}
	if !strings.Contains(a.frame(), "agents learn plans from the rex-plan skill · rex ingest --install-skill") {
		t.Fatalf("A. with the skill nowhere, the wait screen says how to teach the agent\n%s", a.frame())
	}

	// B. The agent writes the plan: a stub first, then its tasks, none
	// active yet.
	os.MkdirAll(filepath.Dir(path), 0o755)
	agent(plan.Stub("sendoff-demo"))
	k.spent(t, 0, "create (the agent writes the plan)")
	if a.at != atOutline || a.plans.Current != path || !a.follow || !strings.Contains(a.frame(), "No tasks yet · your agent fills this file") || !strings.Contains(a.frame(), "following rex/plans/cuj/sendoff-demo.rex.json") {
		t.Fatalf("B. the agent's write opens the plan, followed\n%s", a.frame())
	}
	active := cujFixture(t)
	planned := []byte(mustReplace(t, mustReplace(t, string(active), `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "pending",
      "depends_on": ["plans-ui"],`), `"status": "active",
  "scope"`, `"status": "pending",
  "scope"`))
	agent(planned)
	k.spent(t, 0, "see the plan (Rex follows the write)")
	if a.outline.sel != "card-highlight" || !strings.Contains(a.frame(), "▌ ▸ card-highlight") || !strings.Contains(a.frame(), "plan updated · +3 tasks") {
		t.Fatalf("B. the filled plan lands on its pin: sel=%q\n%s", a.outline.sel, a.frame())
	}
	c.t = c.t.Add(toastFor)
	if got := a.statusLine().Text; got != "▸ card-highlight  Selected plan card is one block (next up)" {
		t.Fatalf("B. row 1 names the next up task, with nothing to type: %q", got)
	}

	// C. The agent starts the task: it writes active, and Rex shows it.
	agent(active)
	k.spent(t, 0, "see the pin")
	if got := a.statusLine().Text; got != "plan updated · card-highlight → active · plan → active" {
		t.Fatalf("C. the agent's pin arrives as a reload: %q", got)
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "card-highlight → active · plan → active" {
		t.Fatalf("C. one plan line on the tape: %+v", last)
	}
	c.t = c.t.Add(toastFor)
	if got := a.statusLine().Text; got != "▸ card-highlight  Selected plan card is one block" || !strings.Contains(a.frame(), "▌ ▸ card-highlight") {
		t.Fatalf("C. row 1 and the outline follow the file: %q\n%s", got, a.frame())
	}

	// The agent works the task; the old verbs do nothing on the outline.
	k.press("p", "d", "s", "b", PastePrefix+"pin load-outline")
	if got, _ := os.ReadFile(path); string(got) != string(active) || a.draft != "" || !strings.HasPrefix(a.statusLine().Text, "▸ card-highlight") {
		t.Fatal("C. letters and a paste on the outline change nothing")
	}
	k.n = 0
	write := filepath.Join(repo, "internal/tui/plans.go")
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "work the plan: continue rex/plans/cuj/sendoff-demo.rex.json"})})
	if a.turn != TurnThinking {
		t.Fatalf("D. a prompt starts the turn: %v", a.turn)
	}
	a.ingest([]watch.Event{
		at(c, 2*time.Second, watch.Event{Source: "claude", Kind: "thought", Text: "The card rows are painted separately."}),
		at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "command", Text: "Edit " + write, Meta: map[string]any{"tool": "Edit", "path": write}}),
		at(c, 4*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + write, Meta: map[string]any{"tool": "Edit", "path": write, "ok": true}}),
	})
	if a.turn != TurnRunning || strings.HasPrefix(a.statusLine().Text, "drift") {
		t.Fatalf("D. a listed file runs the turn and adds no drift: %v %q", a.turn, a.statusLine().Text)
	}
	// D. An edit off the pin shows drift.
	a.ingest([]watch.Event{at(c, 5*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote README.md", Meta: map[string]any{"tool": "Write", "path": filepath.Join(repo, "README.md"), "ok": true}})})
	if !strings.HasPrefix(a.statusLine().Text, "drift · hard: README.md") {
		t.Fatalf("D. drift shows: %q", a.statusLine().Text)
	}

	// E. The agent marks the task done, with its retrospective.
	done := []byte(mustReplace(t, string(active), `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],
      "accomplished": ["the selection bar, border and fill cover all three card rows"],
      "modified": ["internal/tui/plans.go"],
      "verified_how": ["go test ./internal/tui -run PlanCardSelection (passed)"],
      "completed_notes": "the pick colors were already in mascot.UI",`))
	agent(done)
	a.ingest([]watch.Event{at(c, 6*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "Done: one block now. card-highlight is marked done in the plan."})})
	k.spent(t, 0, "mark done (the agent does)")
	if got := a.statusLine().Text; got != "✓ card-highlight done · next up load-outline" {
		t.Fatalf("E. the file finished the pin: %q", got)
	}
	if a.turn != TurnIdle || !strings.Contains(a.frame(), "last ✓ card-highlight: the selection bar") {
		t.Fatalf("E. the reply ends the turn; the outline says where we left off\n%s", a.frame())
	}
	k.press("\r") // the selection stayed on the task the agent finished
	if a.at != atTaskDetail || a.outline.sel != "card-highlight" {
		t.Fatalf("E. enter on the task opens its detail: at=%d sel=%q", a.at, a.outline.sel)
	}
	detail := viewText(taskDetail(a.doc, a.outline.sel, 78)) // it scrolls; all of it
	for _, want := range []string{"── retrospective", "the selection bar, border and fill cover all three", "internal/tui/plans.go", "go test ./internal/tui -run PlanCardSelection (passed)", "the pick colors were already in mascot.UI"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("E. task detail shows %q\n%s", want, detail)
		}
	}
	k.press("esc")
	k.n = 0

	// F. Tab to the tape and back.
	k.press("\t")
	k.spent(t, 1, "watch the tape")
	tape := viewText(BuildLogView(a.log, 78))
	for _, want := range []string{"── thinking", "── run", "── reply", "Done: one block now.", "card-highlight → active", "card-highlight → done", "── drift · card-highlight"} {
		if !strings.Contains(tape, want) {
			t.Errorf("F. tape missing %q\n%s", want, tape)
		}
	}
	if strings.Contains(tape, "wrote ") || strings.Contains(tape, "── done") || strings.Contains(tape, "── note") {
		t.Errorf("F. no wrote/done spam, no notes\n%s", tape)
	}
	logged := len(a.log)
	k.press("p", "d", "s", "b", "x", "\r")
	if len(a.log) != logged || a.draft != "" {
		t.Fatal("F. typing on the tape adds nothing")
	}
	k.n = 0
	k.press("\t")
	k.spent(t, 1, "back to the plan")
	if a.pane != PanePlans || a.at != atOutline {
		t.Fatal("F. Tab returns to the outline")
	}

	// G. The plan list: create a plan there, then open one by name. Picking
	// a plan there latches it.
	k.press("esc")
	if a.at != atLibrary || a.plans.Rows[a.plans.Cursor].Kind != PlanRowContinue || !strings.Contains(a.frame(), "╭") {
		t.Fatal("G. Esc climbs to the plan list, on Continue, where the composer is")
	}
	k.press("\r")
	if !a.follow || a.at != atOutline {
		t.Fatal("G. enter on Continue keeps following")
	}
	k.n = 0
	k.press("esc")
	k.typ("scratch")
	k.press("\r")
	k.spent(t, 1+len("scratch")+1, "create in Rex (Esc, name, enter)")
	stub, err := plan.Load(filepath.Join(a.plans.Root, "scratch.rex.json"))
	if err != nil || len(stub.Problems) != 0 || a.at != atOutline || a.follow || !strings.Contains(a.frame(), "latched") {
		t.Fatalf("G. create writes a v1 stub, opens it and latches: %v\n%s", err, a.frame())
	}
	k.press("esc")
	k.typ("sendoff-demo")
	k.press("\r")
	k.spent(t, 1+len("sendoff-demo")+1, "open by name (Esc, name, enter)")
	if a.plans.Current != path || a.at != atOutline || a.follow || a.outline.sel != "load-outline" {
		t.Fatalf("G. the plan opens on its pin, latched: sel=%q\n%s", a.outline.sel, a.frame())
	}

	// H. Quit.
	k.press(CtrlC)
	if !strings.Contains(a.frame(), "ctrl+c again to quit") {
		t.Fatal("H. the first Ctrl+C asks again")
	}
	k.press(CtrlC)
	k.spent(t, 2, "quit")
	if !a.quit || a.exitLine() != "rex: rex/plans/cuj/sendoff-demo.rex.json · next up load-outline" {
		t.Fatalf("H. quit with the plan named: %q", a.exitLine())
	}
	if got, _ := os.ReadFile(path); string(got) != string(done) {
		t.Fatalf("Rex never wrote the plan: the file is what the agent wrote\n%s", got)
	}

	b := newApp("yellow", false, "idle", NewPlanBrowser(repo), 120, 32)
	(&keys{a: b}).spent(t, 0, "open last time's plan")
	if b.pane != PanePlans || b.at != atOutline || b.plans.Current != path || !b.follow || b.outline.sel != "load-outline" {
		t.Fatalf("I. the next start opens it again, following: at=%d current=%q sel=%q", b.at, b.plans.Current, b.outline.sel)
	}
}

// matrixScreen is one screen of the frame matrix: how to reach it from a
// fresh start, and how to tell it was reached.
type matrixScreen struct {
	name    string
	empty   bool // starts in a repo with no plans; the rest on the CUJ plan
	setup   func(a *app)
	reached func(a *app) bool
}

// matrixScreens is every screen and overlay Rex draws (AGENTS.md, Commands
// and tests): a new one joins the list.
func matrixScreens(t *testing.T) []matrixScreen {
	list := func(a *app) { press(a, "esc") } // the outline at start, Esc to the plan list
	lib := func(sc PlanScreen) func(a *app) bool {
		return func(a *app) bool { return a.pane == PanePlans && a.at == atLibrary && a.plans.Screen == sc }
	}
	at := func(p planAt) func(a *app) bool {
		return func(a *app) bool { return a.at == p && a.overlay[a.pane] == overlayNone }
	}
	over := func(o overlayKind) func(a *app) bool { return func(a *app) bool { return a.overlay[a.pane] == o } }
	browse := func(a *app) {
		list(a)
		a.plans.Screen, a.plans.Dir, a.plans.fresh = ScreenBrowse, a.plans.Root, true
		a.plans.Rebuild()
	}
	pick := func(a *app, kind PlanRowKind) {
		for i, row := range a.plans.Rows {
			if row.Kind == kind {
				a.plans.Cursor = i
			}
		}
	}
	write := func(a *app) {
		other := filepath.Join(a.plans.Root, "cuj", "a-much-longer-plan-name-from-the-agent.rex.json")
		os.WriteFile(other, []byte(`{"version": 1, "tasks": []}`), 0o644)
		a.ingest([]watch.Event{{Source: "claude", Kind: "result", Text: "wrote " + other, Meta: map[string]any{"tool": "Write", "path": other, "ok": true}}})
	}
	return []matrixScreen{
		{"wait", true, func(a *app) {}, func(a *app) bool { return a.at == atWait && a.plans.Current == "" }},
		{"wait, skill installed", true, func(a *app) {
			home, _ := os.UserHomeDir()
			for _, dir := range skill.Dirs(home, a.plans.Repo, true) {
				if err := skill.Install(dir, io.Discard); err != nil {
					t.Fatal(err)
				}
			}
		}, func(a *app) bool { return a.at == atWait && skillHint(a.skill.look(a.plans.Repo, a.now())) == "" }},
		{"wait plans off", true, func(a *app) {
			os.WriteFile(filepath.Join(a.plans.Repo, "rex"), []byte("x"), 0o644)
			a.plans.Rebuild()
		}, func(a *app) bool { return a.at == atWait && a.plans.Notice == rexFileNotice }},
		{"home no plans", true, list, func(a *app) bool { return lib(ScreenHome)(a) && len(a.plans.Rows) == 1 }},
		{"home", false, list, lib(ScreenHome)},
		{"find", false, func(a *app) { list(a); typeText(a, "send") }, func(a *app) bool {
			return lib(ScreenHome)(a) && a.plans.Rows[a.plans.Cursor].Kind == PlanRowPlan
		}},
		{"browse", false, browse, lib(ScreenBrowse)},
		{"create", false, func(a *app) { list(a); pick(a, PlanRowNew); press(a, "\r"); typeText(a, "x/y") }, lib(ScreenCreate)},
		{"new folder", false, func(a *app) { browse(a); pick(a, PlanRowMkdir); press(a, "\r"); typeText(a, "notes") }, lib(ScreenFolder)},
		{"outline", false, func(a *app) {}, at(atOutline)},
		{"outline, title row", false, func(a *app) { press(a, "home") }, func(a *app) bool { return at(atOutline)(a) && a.outline.sel == "" }},
		{"outline, a reply waiting", false, func(a *app) {
			a.ingest([]watch.Event{{Source: "claude", Kind: "text", Text: "done for now"}})
		}, func(a *app) bool { return at(atOutline)(a) && a.unseen[PaneChat] == "•" }},
		{"held attach", false, func(a *app) {
			list(a)
			typeText(a, "zz")
			press(a, "\t") // the list's find holds it, seen from AGENT
			write(a)
		}, func(a *app) bool { return a.pane == PaneChat && a.pending != "" && a.unseen[PanePlans] == "•" }},
		{"new plan offer", false, func(a *app) {
			list(a)
			typeText(a, "scratch")
			press(a, "\r") // made in the list: latched
			write(a)
		}, func(a *app) bool { return at(atOutline)(a) && a.offer != "" && !a.follow }},
		{"task detail", false, func(a *app) { press(a, "\r") }, at(atTaskDetail)},
		{"plan detail", false, func(a *app) { press(a, "home", "\r") }, at(atPlanDetail)},
		{"agent", false, func(a *app) {
			press(a, "\t")
			a.ingest([]watch.Event{{Source: "claude", Kind: "text", Text: "a reply with 中文 and ⚠️ in it \x1b[2J"}})
		}, func(a *app) bool { return a.pane == PaneChat && a.overlay[a.pane] == overlayNone }},
		{"agent, empty", true, func(a *app) { press(a, "\t") }, func(a *app) bool { return a.pane == PaneChat && len(a.log) == 0 }},
		{"empty outline", false, func(a *app) { list(a); typeText(a, "scratch"); press(a, "\r") }, func(a *app) bool {
			return at(atOutline)(a) && len(a.doc.plan.Tasks) == 0
		}},
		{"help", false, func(a *app) { press(a, "\t", "?") }, over(overlayHelp)},
		{"help over the list", false, func(a *app) { list(a); press(a, "?") }, func(a *app) bool { return over(overlayHelp)(a) && a.at == atLibrary }},
		{"help over a task", false, func(a *app) { press(a, "\r", "?") }, func(a *app) bool { return over(overlayHelp)(a) && a.at == atTaskDetail }},
	}
}

// matrixApp is a fresh app on screen s at cols×lines.
func matrixApp(t *testing.T, s matrixScreen, cols, lines int) *app {
	var a *app
	if s.empty {
		a = testApp(t) // the wait screen
	} else {
		a, _, _ = cujApp(t)
	}
	s.setup(a)
	a.settle(cols, lines)
	return a
}

// Every screen fits every size: no row wider than the terminal, no control
// bytes from content, the tab bar and its turn word on top, the page title
// on row 2 (AGENT's from 12 rows), and where to go next at the bottom, in
// words (docs/ux.md §4).
func TestFrameMatrix(t *testing.T) {
	for _, size := range [][2]int{{24, 7}, {24, 12}, {24, 24}, {80, 7}, {80, 12}, {80, 16}, {80, 20}, {80, 24}, {120, 7}, {120, 12}, {120, 24}} {
		for _, s := range matrixScreens(t) {
			a := matrixApp(t, s, size[0], size[1])
			f := a.frameRows()
			name := s.name + " " + strings.Join([]string{strconv.Itoa(size[0]), strconv.Itoa(size[1])}, "×")
			if !s.reached(a) {
				t.Errorf("%s: setup didn't reach the screen", name)
				continue
			}
			if len(f.Rows) != size[1] {
				t.Errorf("%s: %d rows", name, len(f.Rows))
				continue
			}
			for i, row := range f.Rows {
				if cellLen(row) > size[0] {
					t.Errorf("%s: row %d is %d cells", name, i, cellLen(row))
				}
				// Rex has no verbs, and keys are words: no footer,
				// placeholder or copy offers a verb, an icon for a key, or
				// the old name for AGENT (docs/ux.md §4, §6).
				for _, never := range []string{"⏎", "↔", "tape", "brief", "send-off", "clipboard"} {
					if strings.Contains(row, never) {
						t.Errorf("%s: row %d has %q: %q", name, i, never, row)
					}
				}
				for _, r := range row {
					if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
						t.Errorf("%s: control %U in row %d", name, r, i)
					}
				}
			}
			head := textOf(f.Rows[0], size[0])
			if !strings.Contains(head, "PLAN") || !strings.Contains(head, "AGENT") || !strings.Contains(head, statusForTurn(a.turn, a.lastKind)) {
				t.Errorf("%s: header %q", name, head)
			}
			if size[1] > 3 {
				title := textOf(f.Rows[2], size[0])
				switch {
				case a.overlay[a.pane] != overlayNone:
					if !strings.Contains(title, "› HELP") {
						t.Errorf("%s: Help's page title %q", name, title)
					}
				case a.pane == PanePlans:
					if !strings.HasPrefix(title, "  PLANS") && !strings.HasPrefix(title, "▌ PLANS") && !strings.HasPrefix(title, "  …") && !strings.HasPrefix(title, "▌ …") {
						t.Errorf("%s: page title %q", name, title)
					}
				case strings.HasPrefix(title, "  AGENT") != (size[1] >= agentTitleRows):
					t.Errorf("%s: AGENT's page title from %d rows: %q", name, agentTitleRows, title)
				}
			}
			footer := textOf(f.Rows[len(f.Rows)-1], size[0])
			if strings.TrimSpace(footer) == "" || strings.Contains(footer, "·") {
				t.Errorf("%s: footer %q", name, footer)
			}
			if textW := PaneTextWidth(size[0]); textW >= 38 && a.overlay[a.pane] == overlayNone && !strings.Contains(footer, "tab") && !strings.Contains(footer, "esc") {
				t.Errorf("%s: footer doesn't say where to go: %q", name, footer)
			}
		}
	}
}

// docs/ux.md §5, rule 4: color never changes the characters. Every
// screen's text column in color, stripped of its SGR, is its NO_COLOR
// frame; only the composer's placeholder differs, in parentheses.
func TestColorFrameSameText(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 24}} {
		for _, s := range matrixScreens(t) {
			a := matrixApp(t, s, size[0], size[1])
			plain := a.frameRows().Rows
			a.enabled = true
			color := a.frameRows().Rows
			l := a.layout()
			for i := range plain {
				if i >= len(plain)-FooterRows-l.composer && i < len(plain)-FooterRows {
					continue // the composer
				}
				if got, want := tailCells(stripSGR(color[i]), l.textW), tailCells(plain[i], l.textW); got != want {
					t.Errorf("%s %d×%d row %d:\ncolor    %q\nno color %q", s.name, size[0], size[1], i, got, want)
				}
			}
			if !strings.Contains(strings.Join(color, ""), "\033[1m") {
				t.Errorf("%s: no bold anywhere", s.name)
			}
		}
	}
}
