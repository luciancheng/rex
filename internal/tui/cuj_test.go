package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/plan"
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

// The journey: the agent creates the plan and Rex opens it → the plan
// list as an escape hatch → watch a turn → done → pin next → send off →
// drift → back to the Library → quit.
func TestCUJWholeLoop(t *testing.T) {
	a := testApp(t)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	k := &keys{a: a}
	var copied []string
	a.copy = fakeCopy(&copied, copyResult{status: "copied (fake)"})

	if a.pane != PanePlans || a.at != atWait || !strings.Contains(a.frame(), "no plan yet · tell your agent to make one") {
		t.Fatalf("A. with no plan yet, start waits for one\n%s", a.frame())
	}
	repo := a.plans.Repo
	path := filepath.Join(a.plans.Root, "cuj", "sendoff-demo.rex.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, plan.Stub("sendoff-demo"), 0o644)
	a.ingest([]watch.Event{at(c, 0, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + path, Meta: map[string]any{"tool": "Write", "path": path, "ok": true, "cwd": repo}})})
	k.spent(t, 0, "create (the agent writes the plan)") // docs/ux.md §14
	if a.at != atOutline || a.plans.Current != path || !a.follow || !strings.Contains(a.frame(), "No tasks yet") || !strings.Contains(a.frame(), "following · rex/plans/cuj/sendoff-demo.rex.json") {
		t.Fatalf("B. the agent's write opens the plan, followed\n%s", a.frame())
	}
	os.WriteFile(path, cujFixture(t), 0o644)
	later(a, c)
	k.spent(t, 0, "open (Rex follows the write)")
	if a.outline.sel != "card-highlight" || !strings.Contains(a.frame(), "▌ ▸ card-highlight") || !strings.Contains(a.frame(), "plan updated · +3 tasks") {
		t.Fatalf("B. the filled plan lands on its pin: sel=%q\n%s", a.outline.sel, a.frame())
	}

	// The plan list stays an escape hatch: create, refuse a duplicate,
	// open by name. Picking a plan there latches it.
	k.press("esc")
	k.typ("scratch")
	k.press("\r")
	k.spent(t, 1+len("scratch")+1, "create in Rex (Esc, find, ⏎)")
	stub, err := plan.Load(filepath.Join(a.plans.Root, "scratch.rex.json"))
	if err != nil || len(stub.Problems) != 0 || a.at != atOutline || a.follow || !strings.Contains(a.frame(), "No tasks yet") || !strings.Contains(a.frame(), "latched") {
		t.Fatalf("C. create writes a v1 stub, opens it and latches: %v %+v\n%s", err, stub, a.frame())
	}
	k.press("esc", "home", "down")
	if a.at != atLibrary || a.plans.Rows[a.plans.Cursor].Kind != PlanRowNew {
		t.Fatal("C. Create sits under Continue")
	}
	k.press("\r")
	k.typ("scratch")
	k.press("\r")
	if !strings.Contains(a.frame(), "already exists") {
		t.Fatalf("C. a duplicate name is refused\n%s", a.frame())
	}
	k.press("\x15", "esc", "home", "\r") // Ctrl+U, back to Home, ⏎ on Continue
	if a.plans.Current != filepath.Join(a.plans.Root, "scratch.rex.json") || a.at != atOutline || a.follow {
		t.Fatal("C. Continue reopens scratch and keeps it latched")
	}
	k.n = 0
	k.press("esc")
	k.typ("sendoff-demo")
	k.press("\r")
	k.spent(t, 1+len("sendoff-demo")+1, "open by name (Esc, name, ⏎)")
	if a.at != atOutline || a.outline.sel != "card-highlight" || !strings.Contains(a.frame(), "▸ card-highlight  Selected plan card is one block (active)") {
		t.Fatalf("D. the outline opens on the pin\n%s", a.frame())
	}

	k.press("\t")
	k.spent(t, 1, "to the tape")
	write := filepath.Join(repo, "internal/tui/plans.go")
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "make the selected card one block"})})
	if a.turn != TurnThinking {
		t.Fatalf("E. a prompt starts the turn: %v", a.turn)
	}
	a.ingest([]watch.Event{
		at(c, 2*time.Second, watch.Event{Source: "claude", Kind: "thought", Text: "The card rows are painted separately."}),
		at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "command", Text: "Edit " + write, Meta: map[string]any{"tool": "Edit", "path": write}}),
	})
	if a.turn != TurnRunning {
		t.Fatalf("E. a tool runs the turn: %v", a.turn)
	}
	a.ingest([]watch.Event{
		at(c, 4*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + write, Meta: map[string]any{"tool": "Edit", "path": write, "ok": true}}),
		at(c, 5*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "ran go test ./internal/tui -run PlanCardSelection", Meta: map[string]any{"tool": "Bash", "command": "go test ./internal/tui -run PlanCardSelection", "ok": true}}),
		at(c, 6*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "Done: one block now.\n\naccomplished: the selection bar, border and fill cover all three card rows\nverified: go test ./internal/tui -run PlanCardSelection passed"}),
	})
	tape := viewText(BuildLogView(a.log, 78))
	for _, want := range []string{"── thinking", "── run", "── reply", "Done: one block now."} {
		if !strings.Contains(tape, want) {
			t.Errorf("E. tape missing %q\n%s", want, tape)
		}
	}
	if strings.Contains(tape, "wrote ") || strings.Contains(tape, "── done") {
		t.Errorf("E. no wrote/done spam\n%s", tape)
	}
	if a.turn != TurnIdle {
		t.Fatalf("E. the reply ends the turn: %v", a.turn)
	}

	k.typ("d")
	k.press("\r", "\r")
	k.spent(t, 3, "done with the agent's words")
	p, _ := plan.Load(path)
	ch := p.Task("card-highlight")
	if ch.Status != plan.Done || ch.Accomplished[0] != "the selection bar, border and fill cover all three card rows" || !strings.Contains(strings.Join(ch.Modified, ","), "internal/tui/plans.go") || ch.VerifiedHow[0] != "go test ./internal/tui -run PlanCardSelection passed" {
		t.Fatalf("F. the retrospective is on disk: %+v", ch)
	}
	if p.Task("load-outline").Status != plan.Pending || a.statusLine().Text != "✓ card-highlight done · next up load-outline · p⏎ pins it" {
		t.Fatalf("F. nothing is auto-pinned: %q", a.statusLine().Text)
	}

	k.typ("p")
	k.press("\r")
	k.spent(t, 2, "pin next up")
	if p, _ := plan.Load(path); p.Task("load-outline").Status != plan.Active {
		t.Fatal("D. p⏎ pins next up")
	}

	k.typ("s")
	k.press("\r")
	k.spent(t, 2, "sendoff")
	a.tick()
	if len(copied) != 1 {
		t.Fatal("G. the brief is copied")
	}
	brief := copied[0]
	for _, want := range []string{"File: rex/plans/cuj/sendoff-demo.rex.json", "Goal\n", "- Out: extra palettes, web UI", "- [done] card-highlight (depends on: plans-ui)", "- [active] load-outline (depends on: card-highlight)", "### load-outline", "Done\n- plans-ui", "  actual: the selection bar, border and fill cover all three card rows", "Work only on the pinned task.", "rex-sendoff: rex/plans/cuj/sendoff-demo.rex.json#load-outline"} {
		if !strings.Contains(brief, want) {
			t.Errorf("G. brief missing %q\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "make the selected card one block") || strings.Contains(brief, "The card rows are painted") {
		t.Fatal("G. nothing from the tape is in the brief")
	}
	k.press("esc")
	k.spent(t, 1, "close the brief")

	c.t = c.t.Add(toastFor) // a toast outranks drift on row 1 (docs/ux.md §4)
	a.ingest([]watch.Event{at(c, 10*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote README.md", Meta: map[string]any{"tool": "Write", "path": filepath.Join(repo, "README.md"), "ok": true}})})
	if !strings.HasPrefix(a.statusLine().Text, "drift · hard: README.md") {
		t.Fatalf("H. drift shows: %q", a.statusLine().Text)
	}
	a.ingest([]watch.Event{at(c, 11*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote outline.go", Meta: map[string]any{"tool": "Write", "path": filepath.Join(repo, "internal/tui/outline.go"), "ok": true}})})
	if line := a.statusLine().Text; !strings.HasPrefix(line, "drift · hard: README.md") || strings.Contains(line, "outline.go") {
		t.Fatalf("H. a listed file adds no drift: %q", line)
	}

	k.press("\t")
	if a.pane != PanePlans || a.at != atOutline {
		t.Fatal("I. Tab returns to the outline")
	}
	k.press("esc")
	if a.at != atLibrary || a.plans.Rows[a.plans.Cursor].Kind != PlanRowContinue {
		t.Fatal("I. Esc climbs to the Library, on Continue")
	}
	k.press(CtrlC)
	if !strings.Contains(a.frame(), "ctrl+c again to quit") {
		t.Fatal("J. the first Ctrl+C asks again")
	}
	k.press(CtrlC)
	k.spent(t, 4, "back home and quit")
	if !a.quit || a.exitLine() != "rex: rex/plans/cuj/sendoff-demo.rex.json · pinned load-outline" {
		t.Fatalf("J. quit with the plan named: %q", a.exitLine())
	}

	b := newApp("yellow", false, "idle", NewPlanBrowser(repo), 120, 32)
	(&keys{a: b}).spent(t, 0, "open last time's plan") // docs/ux.md §14
	if b.pane != PanePlans || b.at != atOutline || b.plans.Current != path || !b.follow || b.outline.sel != "load-outline" {
		t.Fatalf("K. the next start opens it again, following: at=%d current=%q sel=%q", b.at, b.plans.Current, b.outline.sel)
	}
}

// Every screen fits every size: no row wider than the terminal, no control
// bytes from content, the header and its state word on top, and where to
// go next at the bottom.
func TestFrameMatrix(t *testing.T) {
	type screen struct {
		name    string
		setup   func(a *app)
		reached func(a *app) bool
	}
	// These start in a repo with no plans; the rest on the CUJ plan.
	empty := map[string]bool{"wait": true, "wait plans off": true, "home no plans": true}
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
	screens := []screen{
		{"wait", func(a *app) {}, func(a *app) bool { return a.at == atWait && a.plans.Current == "" }},
		{"wait plans off", func(a *app) {
			os.WriteFile(filepath.Join(a.plans.Repo, "rex"), []byte("x"), 0o644)
			a.plans.Rebuild()
		}, func(a *app) bool { return a.at == atWait && a.plans.Notice == rexFileNotice }},
		{"home no plans", list, func(a *app) bool { return lib(ScreenHome)(a) && len(a.plans.Rows) == 1 }},
		{"home", list, lib(ScreenHome)},
		{"find", func(a *app) { list(a); typeText(a, "send") }, func(a *app) bool {
			return lib(ScreenHome)(a) && a.plans.Rows[a.plans.Cursor].Kind == PlanRowPlan
		}},
		{"browse", browse, lib(ScreenBrowse)},
		{"create", func(a *app) { list(a); pick(a, PlanRowNew); press(a, "\r"); typeText(a, "x/y") }, lib(ScreenCreate)},
		{"new folder", func(a *app) { browse(a); pick(a, PlanRowMkdir); press(a, "\r"); typeText(a, "notes") }, lib(ScreenFolder)},
		{"outline", func(a *app) {}, at(atOutline)},
		{"held attach", func(a *app) {
			press(a, "\t")
			typeText(a, "d")
			press(a, "\r", "\t") // a done form on the tape holds it
			other := filepath.Join(a.plans.Root, "cuj", "a-much-longer-plan-name-from-the-agent.rex.json")
			os.WriteFile(other, []byte(`{"version": 1, "tasks": []}`), 0o644)
			a.ingest([]watch.Event{{Source: "claude", Kind: "result", Text: "wrote " + other, Meta: map[string]any{"tool": "Write", "path": other, "ok": true}}})
		}, func(a *app) bool { return at(atOutline)(a) && a.pending != "" }},
		{"new plan offer", func(a *app) {
			list(a)
			typeText(a, "scratch")
			press(a, "\r") // made in the list: latched
			other := filepath.Join(a.plans.Root, "cuj", "a-much-longer-plan-name-from-the-agent.rex.json")
			os.WriteFile(other, []byte(`{"version": 1, "tasks": []}`), 0o644)
			a.ingest([]watch.Event{{Source: "claude", Kind: "result", Text: "wrote " + other, Meta: map[string]any{"tool": "Write", "path": other, "ok": true}}})
		}, func(a *app) bool { return at(atOutline)(a) && a.offer != "" && !a.follow }},
		{"task detail", func(a *app) { press(a, "\r") }, at(atTaskDetail)},
		{"plan detail", func(a *app) { press(a, "home", "\r") }, at(atPlanDetail)},
		{"tape", func(a *app) {
			press(a, "\t")
			a.ingest([]watch.Event{{Source: "claude", Kind: "text", Text: "a reply with 中文 and ⚠️ in it \x1b[2J"}})
		}, func(a *app) bool { return a.pane == PaneChat && a.overlay[a.pane] == overlayNone }},
		{"help", func(a *app) { press(a, "\t"); typeText(a, "?"); press(a, "\r") }, over(overlayHelp)},
		{"sendoff", func(a *app) { typeText(a, "s"); press(a, "\r") }, func(a *app) bool {
			return over(overlaySendoff)(a) && !a.briefs[a.pane].planning
		}},
		{"planning brief", func(a *app) { typeText(a, "b"); press(a, "\r") }, func(a *app) bool {
			return over(overlaySendoff)(a) && a.briefs[a.pane].planning
		}},
		{"done form", func(a *app) { typeText(a, "d"); press(a, "\r") }, over(overlayDone)},
		{"pin confirm", func(a *app) { press(a, "end"); typeText(a, "p"); press(a, "\r") }, over(overlayPin)},
	}
	for _, size := range [][2]int{{24, 7}, {24, 12}, {24, 24}, {80, 7}, {80, 12}, {80, 24}, {120, 7}, {120, 12}, {120, 24}} {
		for _, s := range screens {
			var a *app
			if empty[s.name] {
				a = testApp(t) // the wait screen
			} else {
				a, _, _ = cujApp(t)
			}
			s.setup(a)
			a.settle(size[0], size[1])
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
				for _, r := range row {
					if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
						t.Errorf("%s: control %U in row %d", name, r, i)
					}
				}
			}
			if !strings.Contains(f.Rows[0], "REX ") || !strings.Contains(f.Rows[0], "idle") {
				t.Errorf("%s: header %q", name, f.Rows[0])
			}
			footer := f.Rows[len(f.Rows)-1]
			if strings.TrimSpace(footer) == "" {
				t.Errorf("%s: empty footer", name)
			}
			if textW := PaneTextWidth(size[0]); textW >= 38 && a.overlay[a.pane] == overlayNone && !strings.Contains(footer, "tab") && !strings.Contains(footer, "esc") {
				t.Errorf("%s: footer doesn't say where to go: %q", name, footer)
			}
		}
	}
}
