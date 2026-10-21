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

// cujApp is a fresh app in a repo holding the CUJ fixture plan. The
// fixture is a copy of rex/plans/cuj/sendoff-demo.rex.json, so trying Rex
// on the repo's own demo can't change what the tests see.
func cujApp(t *testing.T) (*app, *clock, string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "sendoff-demo.rex.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := testApp(t)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	path := filepath.Join(a.plans.Root, "cuj", "sendoff-demo.rex.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, data, 0o644)
	a.plans.walk = planWalk{}
	a.plans.fresh = true
	a.plans.Rebuild()
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

// The journey: create → open → watch a turn → done → pin next → send off
// → drift → back to the Library → quit.
func TestCUJWholeLoop(t *testing.T) {
	a, c, path := cujApp(t)
	k := &keys{a: a}
	var copied []string
	a.copy = fakeCopy(&copied, copyResult{status: "copied (fake)"})

	if a.pane != PanePlans || a.plans.Screen != ScreenHome || !strings.Contains(a.frame(), "Open latest") {
		t.Fatal("A. starts on the plans home")
	}
	k.typ("scratch")
	k.press("\r")
	k.spent(t, len("scratch")+1, "create through find") // docs/ux.md §14
	stub, err := plan.Load(filepath.Join(a.plans.Root, "scratch.rex.json"))
	if err != nil || len(stub.Problems) != 0 || a.at != atOutline || !strings.Contains(a.frame(), "No tasks yet") {
		t.Fatalf("B. create writes a v1 stub and opens it: %v %+v\n%s", err, stub, a.frame())
	}
	k.press("esc", "home", "down")
	if a.at != atLibrary || a.plans.Rows[a.plans.Cursor].Kind != PlanRowNew {
		t.Fatal("B. Create sits under Continue")
	}
	k.press("\r")
	k.typ("scratch")
	k.press("\r")
	if !strings.Contains(a.frame(), "already exists") {
		t.Fatalf("B. a duplicate name is refused\n%s", a.frame())
	}
	k.press("\x15", "esc") // Ctrl+U, then back to Home
	k.n = 0
	k.typ("sendoff-demo")
	k.press("\r")
	k.spent(t, len("sendoff-demo")+1, "open by name")
	if a.at != atOutline || a.outline.sel != "card-highlight" || !strings.Contains(a.frame(), "▸ card-highlight  Selected plan card is one block (active)") {
		t.Fatalf("D. the outline opens on the pin\n%s", a.frame())
	}

	k.press("\t")
	k.spent(t, 1, "to the tape")
	repo := a.plans.Repo
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
	open := func(a *app) { typeText(a, "sendoff-demo"); press(a, "\r") }
	lib := func(sc PlanScreen) func(a *app) bool {
		return func(a *app) bool { return a.pane == PanePlans && a.at == atLibrary && a.plans.Screen == sc }
	}
	at := func(p planAt) func(a *app) bool {
		return func(a *app) bool { return a.at == p && a.overlay[a.pane] == overlayNone }
	}
	over := func(o overlayKind) func(a *app) bool { return func(a *app) bool { return a.overlay[a.pane] == o } }
	screens := []screen{
		{"home", func(a *app) {}, lib(ScreenHome)},
		{"find", func(a *app) { typeText(a, "send") }, func(a *app) bool {
			return lib(ScreenHome)(a) && a.plans.Rows[a.plans.Cursor].Kind == PlanRowPlan
		}},
		{"browse", func(a *app) {
			a.plans.Screen, a.plans.Dir, a.plans.fresh = ScreenBrowse, a.plans.Root, true
			a.plans.Rebuild()
		}, lib(ScreenBrowse)},
		{"create", func(a *app) { press(a, "home", "\r"); typeText(a, "x/y") }, lib(ScreenCreate)},
		{"new folder", func(a *app) {
			a.plans.Screen, a.plans.Dir, a.plans.fresh = ScreenBrowse, a.plans.Root, true
			a.plans.Rebuild()
			for i, row := range a.plans.Rows {
				if row.Kind == PlanRowMkdir {
					a.plans.Cursor = i
				}
			}
			press(a, "\r")
			typeText(a, "notes")
		}, lib(ScreenFolder)},
		{"outline", open, at(atOutline)},
		{"task detail", func(a *app) { open(a); press(a, "\r") }, at(atTaskDetail)},
		{"plan detail", func(a *app) { open(a); press(a, "home", "\r") }, at(atPlanDetail)},
		{"tape", func(a *app) {
			press(a, "\t")
			a.ingest([]watch.Event{{Source: "claude", Kind: "text", Text: "a reply with 中文 and ⚠️ in it \x1b[2J"}})
		}, func(a *app) bool { return a.pane == PaneChat && a.overlay[a.pane] == overlayNone }},
		{"help", func(a *app) { press(a, "\t"); typeText(a, "?"); press(a, "\r") }, over(overlayHelp)},
		{"sendoff", func(a *app) { open(a); typeText(a, "s"); press(a, "\r") }, func(a *app) bool {
			return over(overlaySendoff)(a) && !a.briefs[a.pane].planning
		}},
		{"planning brief", func(a *app) { open(a); typeText(a, "b"); press(a, "\r") }, func(a *app) bool {
			return over(overlaySendoff)(a) && a.briefs[a.pane].planning
		}},
		{"done form", func(a *app) { open(a); typeText(a, "d"); press(a, "\r") }, over(overlayDone)},
		{"pin confirm", func(a *app) { open(a); press(a, "end"); typeText(a, "p"); press(a, "\r") }, over(overlayPin)},
	}
	for _, size := range [][2]int{{24, 7}, {24, 12}, {24, 24}, {80, 7}, {80, 12}, {80, 24}, {120, 7}, {120, 12}, {120, 24}} {
		for _, s := range screens {
			a, _, _ := cujApp(t)
			s.setup(a)
			a.settle(size[0], size[1])
			f := a.frameRows()
			name := s.name + " " + strings.Join([]string{itoa(size[0]), itoa(size[1])}, "×")
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

func itoa(n int) string { return strconv.Itoa(n) }
