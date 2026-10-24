package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// libraryApp is the plan list as Esc from the wait screen shows it, with
// plans that appeared after start (no tick ran, so none was attached).
func libraryApp(t *testing.T, plans ...string) (*app, string) {
	t.Helper()
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 120, 32)
	root := seedPlans(t, repo, plans...)
	a.plans.walk = planWalk{}
	press(a, "esc")
	return a, root
}

// pickRow puts the cursor on the first row of kind, named name when name
// isn't empty.
func pickRow(a *app, kind PlanRowKind, name string) {
	for i, row := range a.plans.Rows {
		if row.Kind == kind && (name == "" || row.Name == name) {
			a.plans.Cursor = i
			return
		}
	}
}

// browseTo enters the folder named name from the list or a folder.
func browseTo(t *testing.T, a *app, name string) {
	t.Helper()
	pickRow(a, PlanRowDir, name)
	press(a, "\r")
	if !a.plans.Browsing() || filepath.Base(a.plans.Dir) != name {
		t.Fatalf("setup: browse %s: dir=%q", name, a.plans.Dir)
	}
}

// listRepo is the CUJ plan, open and followed, and the other plans of the
// mockups in docs/ux.md §11: RECENT newest first, a finished plan, a folder
// for each.
func listRepo(t *testing.T) *app {
	t.Helper()
	a, _, _ := cujApp(t)
	repo := a.plans.Repo
	var done []string
	for i := 0; i < 13; i++ {
		done = append(done, fmt.Sprintf(`{"id": "t%d", "status": "done"}`, i))
	}
	writePlan(t, repo, "rex/v1-sendoff-loop.rex.json", `{"version": 1, "title": "Rex v1: the send-off loop", "status": "done", "tasks": [`+strings.Join(done, ", ")+`]}`)
	writePlan(t, repo, "auth-flow.rex.json", `{"version": 1, "title": "Sign-in and first run", "tasks": [{"id": "a"}, {"id": "b"}, {"id": "c"}, {"id": "d"}]}`)
	writePlan(t, repo, "ux/redesign.rex.json", `{"version": 1, "title": "Rex visual redesign", "status": "active", "tasks": [{"id": "a", "status": "done"}, {"id": "b"}, {"id": "c"}, {"id": "d"}, {"id": "e"}, {"id": "f"}]}`)
	a.plans.walk = planWalk{}
	return a
}

// body is the text column's rows from the page title down, trimmed.
func body(a *app) string {
	var out []string
	for _, r := range a.frameRows().Rows[2:] {
		out = append(out, strings.TrimRight(textOf(r, a.columns), " "))
	}
	return strings.Join(out, "\n")
}

// docs/ux.md §11: the plan list is OPEN, RECENT and FOLDERS, in that
// order, and every plan is on it exactly once: the open one under OPEN,
// the newest others under RECENT, and the root plans RECENT leaves out
// under FOLDERS.
func TestPlanListGroups(t *testing.T) {
	a := listRepo(t)
	for i := 0; i < 6; i++ {
		old := writePlan(t, a.plans.Repo, fmt.Sprintf("old-%d.rex.json", i), demoPlan)
		at := time.Now().Add(time.Duration(i)*time.Second - 2*time.Hour) // older than the rest
		os.Chtimes(old, at, at)
	}
	a.plans.walk = planWalk{}
	press(a, "esc")
	var heads []string
	for _, line := range BuildPlanView(a.plans, 78) {
		if label, _, ok := strings.Cut(line.Text, " ─"); ok && label == strings.ToUpper(label) && !strings.HasPrefix(label, " ") {
			heads = append(heads, line.Text)
		}
	}
	if len(heads) != 3 || !strings.HasPrefix(heads[0], "OPEN ") || !strings.HasSuffix(heads[0], "─") || !strings.HasPrefix(heads[1], "RECENT ") || !strings.HasPrefix(heads[2], "FOLDERS ") || !strings.HasSuffix(heads[2], " rex/plans/") {
		t.Fatalf("H2s %q", heads)
	}
	// The OPEN H2's meta is the follow word (docs/ux.md §11).
	if f := a.frame(); !strings.Contains(f, "─ following") {
		t.Fatalf("OPEN says following\n%s", f)
	}
	seen := map[string]int{}
	groups := map[string]string{}
	recent := 0
	for _, row := range a.plans.Rows {
		if row.Kind == PlanRowPlan {
			seen[row.Path]++
			groups[row.Name] = row.Group
			if row.Group == groupRecent {
				recent++
			}
		}
	}
	// A plan in a folder is listed once or reached through its folder; a
	// root plan is always listed, once.
	all := a.plans.plans()
	if len(all) != 10 {
		t.Fatalf("%d plans", len(all))
	}
	for _, p := range all {
		top, _, nested := strings.Cut(p.rel, "/")
		switch {
		case seen[p.path] > 1:
			t.Errorf("%s listed %d times", p.rel, seen[p.path])
		case seen[p.path] == 0 && (!nested || groups[top] != ""):
			t.Errorf("%s isn't on the list", p.rel)
		}
	}
	if groups["cuj/sendoff-demo.rex.json"] != groupOpen || recent != maxRecent {
		t.Fatalf("the open plan under OPEN, %d under RECENT: %v", recent, groups)
	}
	for _, name := range []string{"ux/redesign.rex.json", "auth-flow.rex.json", "rex/v1-sendoff-loop.rex.json", "old-5.rex.json", "old-4.rex.json"} {
		if groups[name] != groupRecent {
			t.Errorf("%s is one of the newest, under RECENT: %q", name, groups[name])
		}
	}
	for _, name := range []string{"old-0.rex.json", "old-3.rex.json"} {
		if groups[name] != groupFolders {
			t.Errorf("root plan %s RECENT leaves out goes under FOLDERS: %q", name, groups[name])
		}
	}
	var folders []string
	for _, row := range a.plans.Rows {
		if row.Group == groupFolders && row.Kind != PlanRowPlan {
			folders = append(folders, fmt.Sprintf("%d %s %d", row.Kind, row.Name, row.Plans))
		}
	}
	want := []string{fmt.Sprintf("%d cuj 1", PlanRowDir), fmt.Sprintf("%d rex 1", PlanRowDir), fmt.Sprintf("%d ux 1", PlanRowDir), fmt.Sprintf("%d New plan 0", PlanRowNew), fmt.Sprintf("%d New folder 0", PlanRowMkdir)}
	if strings.Join(folders, ",") != strings.Join(want, ",") {
		t.Fatalf("FOLDERS: the top folders, then + New plan and + New folder: %v", folders)
	}
}

// The plan list as docs/ux.md §11 draws it: groups with a blank row above
// each H2 at 24 rows, the accordion on the selected plan, no blank rows
// at 12 rows and the accordion's title and pin, nothing but rows at 7.
func TestPlanListMockups(t *testing.T) {
	cases := []struct {
		cols, lines int
		keys        []string
		want        string
	}{
		{80, 24, nil, `
  PLANS                       4 plans

OPEN ─────────────────────── following
▌ cuj/sendoff-demo                 1/3
▌   Send-off demo · active
▌   goal    A teammate opens this pla…
▌   pinned  ▸ card-highlight  Selecte…
▌   then    · load-outline
▌   done    ✓ plans-ui  Home cards, f…

RECENT ───────────────────────────────
  ux/redesign                      1/6
  auth-flow                        0/4
  rex/v1-sendoff-loop          ✓ 13/13

FOLDERS ─────────────────── rex/plans/
  cuj/                          1 plan
  ↓ more`},
		// At 16 rows the accordion keeps 4 lines, over the blank rows. One
		// ↓ scrolls the open plan away: ↑ more stands on its row, not on
		// a heading.
		{80, 16, []string{"down"}, `
  PLANS                       4 plans
  ↑ more
RECENT ───────────────────────────────
▌ ux/redesign                      1/6
▌   Rex visual redesign · active
▌   next up ▸ b
▌   then    ○ c  ○ d
▌   done    ✓ a
  auth-flow                        0/4
  ↓ more`},
		{80, 12, nil, `
  PLANS                       4 plans
OPEN ─────────────────────── following
▌ cuj/sendoff-demo                 1/3
▌   Send-off demo · active
▌   pinned  ▸ card-highlight  Selecte…
RECENT ───────────────────────────────
  ux/redesign                      1/6
  ↓ more`},
		{80, 7, nil, `
  PLANS                       4 plans
▌ cuj/sendoff-demo          open · 1/3
  ux/redesign                      1/6`},
		{24, 12, nil, `
  PLANS         4 plans
OPEN ───────── following
▌ cuj/sendoff-demo   1/3
▌   Send-off demo
▌   ▸ card-highlight
RECENT ─────────────────
  ux/redesign        1/6
  ↓ more`},
	}
	for _, tc := range cases {
		a := listRepo(t)
		press(a, "esc")
		a.settle(tc.cols, tc.lines)
		press(a, tc.keys...)
		l := a.layout()
		got := strings.Join(strings.Split(body(a), "\n")[:1+l.bodyH], "\n")
		if want := strings.TrimPrefix(tc.want, "\n"); got != want {
			t.Errorf("%d×%d %v:\n%s\nwant\n%s", tc.cols, tc.lines, tc.keys, got, want)
		}
	}

	// From 60 cells a row has the plan's title in a column, and the
	// accordion's first line says the status and when the file changed.
	a := listRepo(t)
	press(a, "esc")
	a.settle(120, 24)
	got := body(a)
	for _, want := range []string{
		"▌ cuj/sendoff-demo      Send-off demo                                      1/3",
		"▌   active · changed ",
		"▌   then    · load-outline  Outline view",
		"  rex/v1-sendoff-loop   Rex v1: the send-off loop                      ✓ 13/13",
		"  cuj/                                                                  1 plan",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("120×24 lacks %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "Send-off demo · active") {
		t.Errorf("from 60 cells the title is on the row, not in the accordion\n%s", got)
	}
}

// docs/ux.md §11: the cursor starts on the open plan, else the newest
// plan, else + New plan; Esc from the outline lands on OPEN, enter there
// keeps following, and enter on another plan latches.
func TestPlanListCursor(t *testing.T) {
	a := listRepo(t)
	press(a, "esc")
	if row := a.plans.Rows[a.plans.Cursor]; row.Group != groupOpen || row.Path != a.plans.Current {
		t.Fatalf("on OPEN: %+v", row)
	}
	press(a, "\r")
	if !a.follow || a.at != atOutline {
		t.Fatal("enter on OPEN keeps following")
	}
	press(a, "esc", "down")
	if row := a.plans.Rows[a.plans.Cursor]; row.Group != groupRecent || row.Name != "ux/redesign.rex.json" {
		t.Fatalf("↓ is the newest other plan: %+v", row)
	}
	press(a, "\r")
	if a.follow || filepath.Base(a.plans.Current) != "redesign.rex.json" || !strings.Contains(a.frame(), "latched") {
		t.Fatalf("enter on another plan latches\n%s", a.frame())
	}

	none, _ := libraryApp(t, "older.rex.json", "newer.rex.json")
	if row := none.plans.Rows[none.plans.Cursor]; row.Group != groupRecent || row.Name != "newer.rex.json" {
		t.Fatalf("with no plan open, on the newest: %+v", row)
	}
	empty := testApp(t)
	os.MkdirAll(filepath.Join(empty.plans.Root, "team"), 0o755)
	press(empty, "esc")
	if row := empty.plans.Rows[empty.plans.Cursor]; row.Kind != PlanRowNew {
		t.Fatalf("with no plans, on + New plan: %+v", row)
	}
}

// docs/ux.md §11: the selected plan opens in place. Its block has ▌ down
// every line, only the plan's own row is filled, and its lines give way
// as rows run short. Moving the cursor reads no files: every summary was
// read when the list was built.
func TestAccordion(t *testing.T) {
	a := listRepo(t)
	press(a, "esc")
	block := func() []LogLine {
		var out []LogLine
		for _, line := range planList(a.plans, a.layout().textW, listFit{body: a.layout().bodyH, tall: a.lines >= 16}) {
			if line.Selected {
				out = append(out, line)
			}
		}
		return out
	}
	labels := func() string {
		var out []string
		for _, line := range block()[1:] {
			out = append(out, strings.TrimSpace(cutCells(strings.TrimPrefix(line.Text, "▌ "), 10)))
		}
		return strings.Join(out, ",")
	}
	for _, tc := range []struct {
		cols, lines int
		want        string
	}{
		{80, 24, "Send-off,goal,pinned,then,done"},
		{80, 20, "Send-off,goal,pinned,then,done"},
		{80, 16, "Send-off,goal,pinned,done"},
		{80, 15, "Send-off,goal,pinned,done"},
		{80, 12, "Send-off,pinned"},
		{120, 16, "active ·,goal,pinned,done"},
		{120, 12, "pinned,done"},
		{120, 24, "active ·,goal,pinned,then,done"},
		{24, 24, "Send-off,▸ card-h"},
	} {
		a.settle(tc.cols, tc.lines)
		if got := labels(); got != tc.want {
			t.Errorf("%d×%d: %q, want %q", tc.cols, tc.lines, got, tc.want)
		}
	}
	a.settle(80, 7)
	if n := len(block()); n != 1 {
		t.Fatalf("at 7 rows only the row: %d lines", n)
	}

	a.settle(80, 24)
	for i, line := range block() {
		if !strings.HasPrefix(line.Text, "▌ ") {
			t.Errorf("line %d has no ▌: %q", i, line.Text)
		}
		if filled := line.Fill == "pickbg"; filled != (i == 0) {
			t.Errorf("line %d filled=%v: only the plan's row is", i, filled)
		}
	}
	// The pin keeps its yellow inside the block.
	pinned := false
	for _, line := range block() {
		for _, s := range line.Spans {
			if s.Text == "▸ card-highlight" && s.Role == "title" && s.Bold {
				pinned = true
			}
		}
	}
	if !pinned {
		t.Error("the pin's ▸ id is bold yellow in the accordion")
	}

	loads := 0
	defer func(was func(string) (*plan.Plan, error)) { loadPlan = was }(loadPlan)
	loadPlan = func(path string) (*plan.Plan, error) { loads++; return plan.Load(path) }
	for _, key := range []string{"down", "down", "down", "up", "end", "home", "pagedown"} {
		press(a, key)
		a.frameRows()
	}
	if loads != 0 {
		t.Fatalf("moving the cursor read %d plans", loads)
	}
	press(a, "home")
	a.frameRows()
	other := filepath.Join(a.plans.Root, "ux", "redesign.rex.json")
	data, _ := os.ReadFile(other)
	os.WriteFile(other, data, 0o644) // same bytes, a new mtime
	os.Chtimes(other, a.now().Add(time.Hour), a.now().Add(time.Hour))
	os.WriteFile(a.plans.Current, cujFixture(t), 0o644)
	os.Chtimes(a.plans.Current, a.now().Add(time.Hour), a.now().Add(time.Hour))
	a.plans.walk = planWalk{}
	a.plans.Rebuild()
	if loads != 1 {
		t.Fatalf("a rebuild reads the one plan that changed, the open one being the outline's: %d reads", loads)
	}
}

// The accordion's lines for a plan with nothing pinned, no tasks, or a
// file that doesn't parse (docs/ux.md §11).
func TestAccordionStates(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{"version": 1, "title": "Shipped", "tasks": [{"id": "a", "status": "done", "accomplished": ["the thing"]}]}`, "✓ all done"},
		{`{"version": 1, "title": "Stuck", "tasks": [{"id": "a", "status": "blocked"}]}`, "no pin · nothing unblocked"},
		{`{"version": 1, "title": "Fresh", "tasks": []}`, "no tasks yet · the agent adds them"},
		{`{"version": 1, "tasks": [`, "can't read: "},
	}
	for _, tc := range cases {
		a, root := libraryApp(t)
		os.MkdirAll(root, 0o755)
		os.WriteFile(filepath.Join(root, "p.rex.json"), []byte(tc.body), 0o644)
		a.plans.walk = planWalk{}
		a.plans.Rebuild()
		pickRow(a, PlanRowPlan, "p.rex.json")
		if v := viewText(BuildPlanView(a.plans, 38)); !strings.Contains(v, "▌ ") || !strings.Contains(v, tc.want) {
			t.Errorf("%s: want %q\n%s", tc.body, tc.want, v)
		}
	}
	a, root := libraryApp(t)
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, "p.rex.json"), []byte(`{"tasks": [`), 0o644)
	a.plans.walk = planWalk{}
	a.plans.Rebuild()
	pickRow(a, PlanRowPlan, "p.rex.json")
	v := viewText(BuildPlanView(a.plans, 38))
	if !strings.Contains(v, "▌ p                         can't read\n▌   can't read: ") {
		t.Fatalf("an unreadable plan's row says so, and its accordion why\n%s", v)
	}
	if line := lineWith(BuildPlanView(a.plans, 78), "▌ p "); strings.Join(strings.Fields(line), " ") != "▌ p can't read" {
		t.Fatalf("an unreadable plan has no title beside its name: %q", line)
	}
}

// lineWith is the first line of view that starts with prefix.
func lineWith(view []LogLine, prefix string) string {
	for _, line := range strings.Split(viewText(view), "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

// A plan with no title, as every new stub is, doesn't repeat its name: the
// title column is blank and the accordion's first line is its status. A
// finished plan's ✓ all done sits under the other values (docs/ux.md §11).
func TestAccordionUntitledAndFinished(t *testing.T) {
	a, root := libraryApp(t)
	os.MkdirAll(filepath.Join(root, "cuj"), 0o755)
	os.WriteFile(filepath.Join(root, "cuj", "fresh.rex.json"), plan.Stub("fresh"), 0o644)
	os.WriteFile(filepath.Join(root, "shipped.rex.json"), []byte(`{"version": 1, "title": "Shipped", "status": "done", "tasks": [{"id": "a", "status": "done"}, {"id": "b", "status": "done", "accomplished": ["Tagged v1.0"]}]}`), 0o644)
	a.plans.walk = planWalk{}
	a.plans.Rebuild()
	pickRow(a, PlanRowPlan, "cuj/fresh.rex.json")
	if v := viewText(BuildPlanView(a.plans, 38)); !strings.Contains(v, "▌   pending\n▌   no tasks yet · the agent adds them\n") {
		t.Fatalf("38 cells: the status alone, then what's next\n%s", v)
	}
	if line := lineWith(BuildPlanView(a.plans, 78), "▌ cuj/fresh"); strings.Join(strings.Fields(line), " ") != "▌ cuj/fresh no tasks" {
		t.Fatalf("78 cells: no title beside the name: %q", line)
	}
	pickRow(a, PlanRowPlan, "shipped.rex.json")
	if v := viewText(BuildPlanView(a.plans, 78)); !strings.Contains(v, "\n▌           ✓ all done\n▌   done    ✓ b  Tagged v1.0") {
		t.Fatalf("✓ all done in the value column\n%s", v)
	}
}

// docs/ux.md §11: OPEN's row and accordion are the plan the outline shows
// (§6). A save caught half-written by a rebuild keeps the last good copy
// there, as it does on the outline, and only an error row 1 shows reaches
// the list.
func TestOpenRowFollowsTheOutline(t *testing.T) {
	a, c, path := cujApp(t)
	press(a, "esc")
	write := func(data []byte, after time.Duration) {
		os.WriteFile(path, data, 0o644)
		at := c.t.Add(after)
		os.Chtimes(path, at, at)
	}
	polls := func(n int) {
		for i := 0; i < n; i++ {
			c.t = c.t.Add(docPollEvery)
			a.tick()
		}
	}
	torn := []byte(`{"tasks": [`)
	write(torn, time.Hour)
	typeText(a, "s") // a find key rebuilds the list while the save is torn
	if f := a.frame(); strings.Contains(f, "can't read") || !strings.Contains(f, "open · 1/3") {
		t.Fatalf("a torn save shows the last good copy\n%s", f)
	}
	write(cujFixture(t), 2*time.Hour) // the same bytes as before the tear
	polls(4)
	press(a, "down", "up")
	if f := a.frame(); strings.Contains(f, "can't read") || !strings.Contains(f, "Send-off demo") {
		t.Fatalf("the save done, the list shows the plan\n%s", f)
	}
	write(torn, 3*time.Hour)
	polls(3)
	if f := a.frame(); !strings.Contains(f, "plan JSON error") || !strings.Contains(f, "can't read: 1:") {
		t.Fatalf("an error the outline shows, the list shows\n%s", f)
	}
}

// docs/ux.md §4, §11: a plan Rex can't open says why on row 1 and in the
// accordion, the open plan's and another's alike: the cause, never the
// absolute path an open error leads with, which pushed the cause off.
func TestUnreadablePlansSayWhy(t *testing.T) {
	a, c, path := cujApp(t)
	other := writePlan(t, a.plans.Repo, "other.rex.json", demoPlan)
	for _, p := range []string{path, other} {
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(p, 0o644) })
	}
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("this user reads a file with no permissions (root, or Windows)")
	}
	c.t = c.t.Add(docPollEvery)
	a.tick()
	if got, want := a.statusLine().Text, "plan error: can't read the file: permission denied · showing last good"; got != want {
		t.Fatalf("row 1: %q, want %q", got, want)
	}
	a.plans.walk = planWalk{}
	press(a, "esc")
	for _, name := range []string{"cuj/sendoff-demo.rex.json", "other.rex.json"} {
		pickRow(a, PlanRowPlan, name)
		view := BuildPlanView(a.plans, 78)
		if line := strings.TrimRight(lineWith(view, "▌   can't read"), " "); line != "▌   can't read: permission denied" {
			t.Errorf("%s: %q\n%s", name, line, viewText(view))
		}
	}
	if f := a.frame(); strings.Contains(f, "open /") || !strings.Contains(f, "can't read: permission denied") {
		t.Fatalf("the cause, not the path\n%s", f)
	}
}

// docs/ux.md §11: a name the filesystem refuses says why in the Name
// prompt, not the absolute path the error leads with.
func TestNamePromptSaysWhyCreateFails(t *testing.T) {
	long := strings.Repeat("x", draftCap) // over any filesystem's 255
	for kind, made := range map[PlanRowKind]func(string) error{
		PlanRowNew:   func(dir string) error { return writeNew(filepath.Join(dir, long+".rex.json"), nil) },
		PlanRowMkdir: func(dir string) error { return os.Mkdir(filepath.Join(dir, long), 0o755) },
	} {
		err := made(t.TempDir())
		pe, ok := err.(*os.PathError)
		if !ok {
			t.Fatalf("setup: %v", err)
		}
		a, _ := libraryApp(t)
		pickRow(a, kind, "")
		press(a, "\r")
		typeText(a, long)
		press(a, "\r")
		if !a.plans.Prompting() || a.plans.Err != pe.Err.Error() || !strings.Contains(a.frame(), "  "+pe.Err.Error()) {
			t.Errorf("row %d: %q, want %q\n%s", kind, a.plans.Err, pe.Err, a.frame())
		}
	}
}

// docs/ux.md §11: RECENT starts with the plans opened earlier in this run,
// the latest first, then the newest: the plan you just left leads it,
// however old its file. Each plan is still listed once.
func TestRecentLeadsWithPlansOpenedThisRun(t *testing.T) {
	a := listRepo(t)
	for i := 0; i < maxRecent+1; i++ {
		writePlan(t, a.plans.Repo, fmt.Sprintf("new-%d.rex.json", i), demoPlan)
	}
	auth := filepath.Join(a.plans.Root, "auth-flow.rex.json")
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(auth, old, old) // the oldest plan there is
	a.plans.walk = planWalk{}
	press(a, "esc")
	pickRow(a, PlanRowPlan, "auth-flow.rex.json")
	press(a, "\r", "esc")
	typeText(a, "redesign")
	press(a, "\r", "esc")
	if !strings.HasSuffix(a.plans.Current, "redesign.rex.json") {
		t.Fatalf("setup: opened %q", a.plans.Current)
	}
	var recent []string
	seen := map[string]int{}
	for _, row := range a.plans.Rows {
		if row.Group == groupRecent {
			recent = append(recent, row.Name)
		}
		if row.Kind == PlanRowPlan {
			seen[row.Name]++
		}
	}
	want := []string{"auth-flow.rex.json", "cuj/sendoff-demo.rex.json", "new-5.rex.json", "new-4.rex.json", "new-3.rex.json"}
	if strings.Join(recent, ",") != strings.Join(want, ",") {
		t.Fatalf("RECENT %v, want %v", recent, want)
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("%s listed %d times", name, n)
		}
	}
}

// docs/ux.md §12: the accordion takes the rows the screen leaves, so a
// taller terminal never shows fewer of its lines: not at 16 rows, where
// the composer becomes its box, and not for the blank rows, which go in
// only where the accordion keeps its lines. The block always fits.
func TestAccordionNeverShrinksAsRowsGrow(t *testing.T) {
	for _, screen := range []string{"list", "find", "browse"} {
		for _, cols := range []int{80, 120} {
			a := listRepo(t)
			press(a, "esc")
			switch screen {
			case "find":
				typeText(a, "demo")
			case "browse":
				browseTo(t, a, "cuj")
			}
			was := 0
			for rows := 7; rows <= 30; rows++ {
				a.settle(cols, rows)
				n := -1
				for _, r := range a.frameRows().Rows {
					if strings.HasPrefix(textOf(r, cols), "▌ ") {
						n++
					}
				}
				if n < was {
					t.Errorf("%s at %d cells: %d lines at %d rows, %d at %d\n%s", screen, PaneTextWidth(cols), n, rows, was, rows-1, body(a))
				}
				if fold := len(accordion(a.plans.sums[a.plans.Rows[a.plans.Cursor].Path], PaneTextWidth(cols), 5, a.now())); rows >= 17 && n != fold {
					t.Errorf("%s at %d×%d: %d of the accordion's %d lines", screen, cols, rows, n, fold)
				}
				was = n
			}
		}
	}
}

// Outside git a notice heads Browse. The accordion is sized after it (and
// its blank row, which goes in only where the accordion keeps its lines),
// so no row is left empty under a cut accordion (docs/ux.md §12).
func TestBrowseAccordionFillsItsRows(t *testing.T) {
	repo := t.TempDir() // no .git: the notice
	writePlan(t, repo, "cuj/a.rex.json", demoPlan)
	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 80, 16)
	press(a, "esc")
	browseTo(t, a, "cuj")
	pickRow(a, PlanRowPlan, "a.rex.json")
	// Under the page title and the notice: all 9 rows used at 16 rows,
	// and from 20 the blank row and the whole accordion.
	for _, tc := range []struct {
		rows int
		want string
	}{
		{16, `  ../
▌ a                         open · 1/3
▌   Send-off demo · active
▌   pinned  ▸ card-highlight  Selecte…
▌   done    ✓ plans-ui  Home cards, b…
  ──────────────────────────────────
  + New plan
  + New folder`},
		{20, `
  ../
▌ a                         open · 1/3
▌   Send-off demo · active
▌   goal    Show a plan, pin a task, …
▌   pinned  ▸ card-highlight  Selecte…
▌   then    · load-outline
▌   done    ✓ plans-ui  Home cards, b…
  ──────────────────────────────────
  + New plan
  + New folder
`},
	} {
		a.settle(80, tc.rows)
		lines := strings.Split(body(a), "\n")
		if got := strings.Join(lines[2:1+a.layout().bodyH], "\n"); got != tc.want || !strings.HasPrefix(lines[1], "  no git") {
			t.Errorf("80×%d:\n%s\nwant\n%s", tc.rows, strings.Join(lines, "\n"), tc.want)
		}
	}
}

// docs/ux.md §11: the list's window never scrolls only to hide blank
// rows, ↑ more never stands where an H2 is, and ↓ more comes right under
// the last row, never under a blank row or a heading left alone.
func TestListWindowEdges(t *testing.T) {
	for _, cols := range []int{80, 120, 24} {
		for rows := 12; rows <= 26; rows++ {
			a := listRepo(t)
			for i := 0; i < 8; i++ {
				writePlan(t, a.plans.Repo, fmt.Sprintf("p%d.rex.json", i), demoPlan)
			}
			a.plans.walk = planWalk{}
			press(a, "esc")
			a.settle(cols, rows)
			for step := 0; step < 2*len(a.plans.Rows); step++ {
				key := "down"
				if step >= len(a.plans.Rows) {
					key = "up"
				}
				where := fmt.Sprintf("%d×%d %s %d", cols, rows, key, step)
				frame := a.frameRows().Rows
				l := a.layout()
				all := planList(a.plans, l.textW, listFit{body: l.bodyH, tall: rows >= 16, follow: a.followWord()})
				list, start := all[headOf(all):], a.plans.View
				if start > 0 && !slices.ContainsFunc(list[:start], notGap) {
					t.Errorf("%s: scrolled only to hide blank rows\n%s", where, body(a))
				}
				var text []string
				for _, r := range frame[3 : 3+l.bodyH] {
					text = append(text, textOf(r, cols))
				}
				if text[0] == "  ↑ more" && strings.Contains(list[start].Text, " ──") {
					t.Errorf("%s: ↑ more covers %q\n%s", where, list[start].Text, body(a))
				}
				if start > 0 && text[0] != "  ↑ more" && !list[start].Selected {
					t.Errorf("%s: rows hidden above, and no ↑ more\n%s", where, body(a))
				}
				for i, line := range text {
					if line == "  ↓ more" && i > 0 && (text[i-1] == "" || strings.Contains(text[i-1], " ──")) {
						t.Errorf("%s: ↓ more under %q\n%s", where, text[i-1], body(a))
					}
				}
				press(a, key)
			}
		}
	}
}

// docs/ux.md §11: plans under a hidden folder are left out, as Browse and
// FOLDERS leave the folder out, so every plan the page counts is on the
// list or in a folder on it; the open plan is listed wherever it lives.
// A folder counts its plans as the page does, each file once.
func TestOnePlanSetForEveryCount(t *testing.T) {
	a := listRepo(t)
	hidden := writePlan(t, a.plans.Repo, ".archive/old.rex.json", demoPlan)
	a.plans.walk = planWalk{}
	press(a, "esc")
	if meta := libraryPage(a.plans).Meta; meta != "4 plans" || strings.Contains(body(a), ".archive") {
		t.Fatalf("a hidden folder's plans are left out: %q\n%s", meta, body(a))
	}
	// Left off the list, not out of reach: a find matches it, and start
	// still lands on it as the newest plan.
	typeText(a, "old")
	if !strings.Contains(body(a), ".archive/old") {
		t.Fatalf("a find reaches a hidden folder's plan:\n%s", body(a))
	}
	press(a, "esc")
	later := time.Now()
	for _, p := range walkPlans(a.plans.Root) {
		if p.mod.After(later) {
			later = p.mod // writePlan's clock runs ahead of the wall's
		}
	}
	later = later.Add(time.Second)
	if err := os.Chtimes(hidden, later, later); err != nil {
		t.Fatal(err)
	}
	got, err1 := os.Stat(a.newestPlan())
	want, err2 := os.Stat(hidden)
	if err1 != nil || err2 != nil || !os.SameFile(got, want) {
		t.Fatalf("start's newest plan is %q, want %q", a.newestPlan(), hidden)
	}
	a.ingest([]watch.Event{wrote(a.plans.Repo, hidden)})
	press(a, "esc")
	if row := a.plans.Rows[a.plans.Cursor]; row.Group != groupOpen || row.Name != ".archive/old.rex.json" || libraryPage(a.plans).Meta != "5 plans" {
		t.Fatalf("the open plan is under OPEN wherever it lives: %+v %q", row, libraryPage(a.plans).Meta)
	}

	b := listRepo(t)
	if err := os.Symlink(b.plans.Current, filepath.Join(b.plans.Root, "cuj", "alias.rex.json")); err != nil {
		t.Skip("no symlinks here (Windows without the privilege)")
	}
	b.plans.walk = planWalk{}
	press(b, "esc")
	for _, row := range b.plans.Rows {
		if row.Kind == PlanRowDir && row.Name == "cuj" && row.Plans != 1 {
			t.Fatalf("cuj/ counts the plan and its link once: %d", row.Plans)
		}
	}
	if meta := libraryPage(b.plans).Meta; meta != "4 plans" {
		t.Fatalf("page %q", meta)
	}
	browseTo(t, b, "cuj")
	if meta := libraryPage(b.plans).Meta; meta != "1 plan" {
		t.Fatalf("Browse cuj/ %q", meta)
	}
}

// Opened by a link's name, the plan's own row in Browse and in a find
// still says it is open: the open plan is marked by file (docs/ux.md §11).
func TestOpenMarkGoesByFile(t *testing.T) {
	a, _, demo := cujApp(t)
	alias := filepath.Join(a.plans.Root, "alias.rex.json")
	if err := os.Symlink(demo, alias); err != nil {
		t.Skip("no symlinks here (Windows without the privilege)")
	}
	a.plans.walk = planWalk{}
	press(a, "esc")
	typeText(a, "alias")
	press(a, "\r", "esc")
	if a.plans.Current != alias {
		t.Fatalf("setup: open %q", a.plans.Current)
	}
	marked := func(where string) {
		t.Helper()
		for _, line := range strings.Split(body(a), "\n") {
			if strings.Contains(line, "sendoff-demo ") && !strings.HasSuffix(line, "open · 1/3") {
				t.Fatalf("%s: %q isn't marked open\n%s", where, line, body(a))
			}
		}
	}
	browseTo(t, a, "cuj")
	marked("browse")
	press(a, "esc")
	typeText(a, "sendoff")
	marked("find")
}

// docs/ux.md §11: a find is one flat list under PLANS › find, the open
// plan's meta says it is open, the selected match keeps its accordion,
// and + Create 'x' comes last.
func TestFindFlat(t *testing.T) {
	a := listRepo(t)
	press(a, "esc")
	a.settle(80, 12)
	typeText(a, "demo")
	got := body(a)
	// A find has no H2s: the accordion takes every row the rest leaves.
	want := `  PLANS › find                1 match
▌ cuj/sendoff-demo          open · 1/3
▌   Send-off demo · active
▌   goal    A teammate opens this pla…
▌   pinned  ▸ card-highlight  Selecte…
▌   then    · load-outline
▌   done    ✓ plans-ui  Home cards, f…
  + Create 'demo'`
	if !strings.HasPrefix(got, want) {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	press(a, "\x15")
	typeText(a, "r")
	rows := a.plans.Rows
	if last := rows[len(rows)-1]; last.Kind != PlanRowCreateNamed || last.Name != "r" {
		t.Fatalf("+ Create 'x' is last: %+v", last)
	}
	for _, line := range BuildPlanView(a.plans, 38) {
		if strings.Contains(line.Text, "─") {
			t.Fatalf("a find has no group heads: %q", line.Text)
		}
	}
}

// docs/ux.md §11: there is no Browse root. → or enter on a folder under
// FOLDERS browses it (§3). From a top folder ../, Esc and ← each go back
// to the plan list, on the folder; deeper they go up one.
func TestBrowseUpFromTopFolder(t *testing.T) {
	right := listRepo(t)
	press(right, "esc")
	pickRow(right, PlanRowDir, "cuj")
	if press(right, "right"); !right.plans.Browsing() || right.plans.Dir != filepath.Join(right.plans.Root, "cuj") {
		t.Fatalf("→ on cuj/ browses it: screen=%d dir=%q", right.plans.Screen, right.plans.Dir)
	}
	for _, key := range []string{"../", "esc", "left"} {
		a := listRepo(t)
		os.MkdirAll(filepath.Join(a.plans.Root, "cuj", "sub"), 0o755)
		press(a, "esc")
		browseTo(t, a, "cuj")
		browseTo(t, a, "sub")
		for i := 0; i < 2; i++ {
			if key == "../" {
				pickRow(a, PlanRowUp, "")
				press(a, "\r")
			} else {
				press(a, key)
			}
		}
		if a.at != atLibrary || a.plans.Screen != ScreenHome || a.plans.Rows[a.plans.Cursor].Name != "cuj" {
			t.Fatalf("%s twice: screen=%d on %+v", key, a.plans.Screen, a.plans.Rows[a.plans.Cursor])
		}
		if title := textOf(a.frameRows().Rows[2], a.columns); !strings.HasPrefix(title, "  PLANS ") {
			t.Fatalf("%s: %q", key, title)
		}
	}
	a := listRepo(t)
	press(a, "esc")
	browseTo(t, a, "cuj")
	got := body(a)
	for _, want := range []string{"  PLANS › cuj/", "\n  ../\n▌ sendoff-demo ", "open · 1/3", "\n  ────", "\n  + New plan\n  + New folder"} {
		if !strings.Contains(got, want) {
			t.Errorf("Browse lacks %q\n%s", want, got)
		}
	}
	if !strings.Contains(a.frame(), "esc plans") {
		t.Fatalf("from a top folder Esc is the plan list\n%s", a.frame())
	}
}

// docs/ux.md §11: the plan list's actions read as actions, without
// brackets, wherever they show.
func TestNoBracketedActions(t *testing.T) {
	a := listRepo(t)
	press(a, "esc")
	frames := []string{a.frame()}
	typeText(a, "brand-new")
	frames = append(frames, a.frame())
	press(a, "\x15")
	browseTo(t, a, "cuj")
	frames = append(frames, a.frame())
	empty := testApp(t)
	press(empty, "esc")
	frames = append(frames, empty.frame())
	for i, f := range frames {
		if strings.Contains(f, "[ + ") || strings.Contains(f, " ]") {
			t.Errorf("frame %d has brackets\n%s", i, f)
		}
		if !strings.Contains(f, "+ New plan") && !strings.Contains(f, "+ Create 'brand-new'") {
			t.Errorf("frame %d has no action\n%s", i, f)
		}
	}
}

// docs/ux.md §11: with no plans, one sentence and the two actions, + New
// plan selected; Esc goes back to Wait.
func TestNoPlansYet(t *testing.T) {
	a := testApp(t)
	press(a, "esc")
	a.settle(80, 12)
	got := body(a)
	want := `  PLANS                       0 plans
  No plans in rex/plans/ yet. Type a
  name below, or ask your agent to
  write one.

▌ + New plan
  + New folder`
	if !strings.HasPrefix(got, want) {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(a.frame(), "esc wait") {
		t.Fatalf("with no plan open the footer says esc wait\n%s", a.frame())
	}
	press(a, "esc")
	if a.at != atWait {
		t.Fatal("Esc goes back to Wait")
	}
}

// docs/ux.md §11: the Name prompt says what to type, where it will go as
// it is typed, and that it never overwrites.
func TestNamePrompt(t *testing.T) {
	a := listRepo(t)
	press(a, "esc")
	a.settle(80, 12)
	pickRow(a, PlanRowNew, "")
	press(a, "\r")
	typeText(a, "auth-flow2")
	want := `  PLANS › new plan
  Name the plan, then enter.
  a/b makes plan b in folder a.

  → rex/plans/auth-flow2.rex.json
  never overwrites a file`
	if got := body(a); !strings.HasPrefix(got, want) {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	press(a, "\x15")
	if got := body(a); !strings.Contains(got, "→ rex/plans/<name>.rex.json") {
		t.Fatalf("an empty name shows where it would go\n%s", got)
	}
	typeText(a, "../out")
	if got := body(a); !strings.Contains(got, "plan names stay inside rex/plans") {
		t.Fatalf("a name that climbs out says so before enter\n%s", got)
	}
	// On its way into a folder, the name says where it goes, not an error.
	for typed, want := range map[string]string{"team/": "→ rex/plans/team/<name>.rex.json", "team\\": "→ rex/plans/team/<name>.rex.json", "a/./": "→ rex/plans/a/<name>.rex.json", "  /": "→ rex/plans/<name>.rex.json", "../": "plan names stay inside rex/plans"} {
		press(a, "\x15")
		typeText(a, typed)
		if got := body(a); !strings.Contains(got, want) {
			t.Fatalf("%q: want %q\n%s", typed, want, got)
		}
	}
	press(a, "\x15")
	typeText(a, "auth-flow")
	press(a, "\r")
	if a.at != atLibrary || !strings.Contains(a.frame(), "auth-flow.rex.json already exists") {
		t.Fatalf("a taken name is refused\n%s", a.frame())
	}
}

func TestFindAcrossAllPlans(t *testing.T) {
	a, root := libraryApp(t, "onboarding/auth.rex.json", "billing.rex.json", "onboarding/signup.rex.json")
	typeText(a, "auth")
	b := a.plans
	if b.Find != "auth" || len(b.Rows) != 2 {
		t.Fatalf("find rows %#v", b.Rows)
	}
	if b.Rows[0].Kind != PlanRowPlan || b.Rows[0].Name != "onboarding/auth.rex.json" || b.Cursor != 0 {
		t.Fatalf("first match selected: %#v cursor=%d", b.Rows[0], b.Cursor)
	}
	if b.Rows[1].Kind != PlanRowCreateNamed {
		t.Fatal("find ends with + Create")
	}
	press(a, "\r")
	if a.plans.Current != filepath.Join(root, "onboarding", "auth.rex.json") {
		t.Fatalf("opened %q", a.plans.Current)
	}
}

func TestFindWithNoMatchCreatesInOneKey(t *testing.T) {
	a, root := libraryApp(t, "billing.rex.json")
	typeText(a, "fresh-idea")
	if len(a.plans.Rows) != 1 || a.plans.Rows[0].Kind != PlanRowCreateNamed {
		t.Fatalf("%#v", a.plans.Rows)
	}
	if !strings.Contains(a.frame(), "▌ + Create 'fresh-idea'") {
		t.Fatal("create row shown")
	}
	press(a, "\r")
	if _, err := os.Stat(filepath.Join(root, "fresh-idea.rex.json")); err != nil {
		t.Fatal("enter on + Create makes the plan")
	}
}

func TestFindExactNameOffersNoCreate(t *testing.T) {
	a, _ := libraryApp(t, "billing.rex.json")
	typeText(a, "billing")
	for _, row := range a.plans.Rows {
		if row.Kind == PlanRowCreateNamed {
			t.Fatal("a plan named exactly this exists")
		}
	}
}

func TestBrowseCursorFilterAndActionsBelow(t *testing.T) {
	a, root := libraryApp(t, "team/zeta.rex.json", "team/sub/alpha.rex.json")
	os.WriteFile(filepath.Join(root, "team", "notes.txt"), []byte("x"), 0o644)
	browseTo(t, a, "team")
	b := a.plans
	if b.Rows[b.Cursor].Kind != PlanRowDir {
		t.Fatalf("cursor should start on the first folder, got %#v", b.Rows[b.Cursor])
	}
	if n := len(b.Rows); b.Rows[n-2].Kind != PlanRowNew || b.Rows[n-1].Kind != PlanRowMkdir {
		t.Fatal("+ New plan and + New folder sit below the listing")
	}
	typeText(a, "zet")
	if b.Rows[b.Cursor].Name != "zeta.rex.json" {
		t.Fatalf("filter selects its first match: %#v", b.Rows[b.Cursor])
	}
	press(a, "\r")
	if !strings.HasSuffix(a.plans.Current, "zeta.rex.json") {
		t.Fatal("enter after a filter opens the match, not New folder")
	}
}

func TestBrowseFolderChangeClearsFilterAndFileExplains(t *testing.T) {
	a, root := libraryApp(t, "team/alpha.rex.json", "team/inner/beta.rex.json")
	os.WriteFile(filepath.Join(root, "team", "notes.txt"), []byte("x"), 0o644)
	browseTo(t, a, "team")
	typeText(a, "inn")
	press(a, "\r")
	if !strings.HasSuffix(a.plans.Dir, "inner") || a.draft != "" || a.plans.Filter != "" {
		t.Fatalf("dir=%q draft=%q filter=%q", a.plans.Dir, a.draft, a.plans.Filter)
	}
	press(a, "esc")
	typeText(a, "notes")
	press(a, "\r")
	if !strings.Contains(a.plans.Err, "not a .rex.json") {
		t.Fatalf("err %q", a.plans.Err)
	}
	press(a, "x")
	if a.plans.Err != "" {
		t.Fatal("errors clear on the next key")
	}
	press(a, "\x15")
	typeText(a, "zzz")
	if !strings.Contains(viewText(BuildPlanView(a.plans, 60)), "no match for 'zzz' in rex/plans/team") {
		t.Fatal("no-match line")
	}
}

func TestCreateInASubfolder(t *testing.T) {
	a, root := libraryApp(t, "seed.rex.json")
	pickRow(a, PlanRowNew, "")
	press(a, "\r")
	typeText(a, "onboarding/auth")
	if !strings.Contains(a.frame(), "→ rex/plans/onboarding/auth.rex.json") {
		t.Fatalf("target path shown\n%s", a.frame())
	}
	press(a, "\r")
	if _, err := os.Stat(filepath.Join(root, "onboarding", "auth.rex.json")); err != nil {
		t.Fatal("a/b creates b in folder a")
	}
	b := NewPlanBrowser(a.plans.Repo)
	b.From = ScreenHome
	if b.CommitCreate("../escape") != PlanStay || !strings.Contains(b.Err, "inside rex/plans") {
		t.Fatalf("err %q", b.Err)
	}
}

// The Name prompt's preview and its commit go by one folder: rex/plans
// from the list, whatever folder was browsed last.
func TestPromptPreviewIsWhereItGoes(t *testing.T) {
	a, root := libraryApp(t, "team/alpha.rex.json")
	b := a.plans
	b.Screen, b.From, b.Dir, b.Name = ScreenFolder, ScreenHome, filepath.Join(root, "team"), "q3"
	if v := viewText(BuildPlanView(b, 60)); !strings.Contains(v, "→ rex/plans/q3/") {
		t.Fatalf("%s", v)
	}
	if b.CommitFolder("q3"); b.Err != "" {
		t.Fatal(b.Err)
	}
	if _, err := os.Stat(filepath.Join(root, "q3")); err != nil {
		t.Fatal("made where the preview said")
	}
}

// + New folder under FOLDERS makes a folder in rex/plans and browses it.
func TestNewFolderFromTheList(t *testing.T) {
	a, root := libraryApp(t, "seed.rex.json")
	pickRow(a, PlanRowMkdir, "")
	press(a, "\r")
	typeText(a, "team")
	if !strings.Contains(a.frame(), "→ rex/plans/team/") {
		t.Fatalf("target shown\n%s", a.frame())
	}
	press(a, "\r")
	if info, err := os.Stat(filepath.Join(root, "team")); err != nil || !info.IsDir() || !a.plans.Browsing() || a.plans.Dir != filepath.Join(root, "team") {
		t.Fatalf("made and browsed: %v dir=%q", err, a.plans.Dir)
	}
	press(a, "esc")
	if a.plans.Screen != ScreenHome || a.plans.Rows[a.plans.Cursor].Name != "team" {
		t.Fatalf("Esc is the plan list, on the new folder: %+v", a.plans.Rows[a.plans.Cursor])
	}
}

func TestSelectionFollowsThePlanNotTheRow(t *testing.T) {
	a, root := libraryApp(t, "older.rex.json", "newer.rex.json")
	b := a.plans
	pickRow(a, PlanRowPlan, "older.rex.json")
	os.WriteFile(filepath.Join(root, "older.rex.json"), []byte("{} "), 0o644) // now the newest
	b.walk = planWalk{}
	b.Rebuild()
	if !strings.HasSuffix(b.Rows[b.Cursor].Path, "older.rex.json") || b.Cursor != 0 {
		t.Fatalf("selection jumped to %#v", b.Rows[b.Cursor])
	}

	// Across groups too: the oldest plan in RECENT, pushed out by a newer
	// one, is selected under FOLDERS (docs/ux.md §11).
	a, _ = libraryApp(t)
	b = a.plans
	for i := 1; i <= 6; i++ {
		writePlan(t, b.Repo, fmt.Sprintf("p%d.rex.json", i), demoPlan)
	}
	b.walk = planWalk{}
	b.Rebuild()
	pickRow(a, PlanRowPlan, "p2.rex.json")
	if b.Rows[b.Cursor].Group != groupRecent {
		t.Fatalf("setup: p2 is the oldest in RECENT: %+v", b.Rows[b.Cursor])
	}
	want := b.Rows[b.Cursor].Path
	writePlan(t, b.Repo, "p7.rex.json", demoPlan)
	b.walk = planWalk{}
	b.Rebuild()
	if row := b.Rows[b.Cursor]; row.Path != want || row.Group != groupFolders {
		t.Fatalf("selection jumped to %+v", row)
	}
}

func TestLongListScrollsWithTheSelection(t *testing.T) {
	var names []string
	for i := 0; i < 40; i++ {
		names = append(names, "group/p"+string(rune('a'+i%26))+string(rune('a'+i/26))+".rex.json")
	}
	a, _ := libraryApp(t, names...)
	a.settle(120, 20)
	browseTo(t, a, "group")
	for i := 0; i < 30; i++ {
		press(a, "down")
		frame := a.frame()
		if !strings.Contains(frame, "▌") {
			t.Fatalf("selection scrolled out of view after %d moves\n%s", i+1, frame)
		}
	}
	if !strings.Contains(a.frame(), "↑ more") {
		t.Fatal("a scrolled list says there is more above")
	}
}

func TestFindComparesTheWholeName(t *testing.T) {
	// A root plan b doesn't count as a/b: + Create 'a/b' is offered, and
	// enter makes it in folder a.
	a, root := libraryApp(t, "b.rex.json")
	typeText(a, "a/b")
	b := a.plans
	if len(b.Rows) == 0 || b.Rows[len(b.Rows)-1].Kind != PlanRowCreateNamed {
		t.Fatalf("rows %#v", b.Rows)
	}
	press(a, "end", "\r")
	if _, err := os.Stat(filepath.Join(root, "a", "b.rex.json")); err != nil {
		t.Fatal("+ Create 'a/b' creates b in folder a")
	}

	// An existing a/b is an exact match: no + Create, and it is selected.
	a, _ = libraryApp(t, "a/b.rex.json", "b.rex.json")
	typeText(a, "a/b")
	b = a.plans
	for _, row := range b.Rows {
		if row.Kind == PlanRowCreateNamed {
			t.Fatalf("a/b exists, so no + Create: %#v", b.Rows)
		}
	}
	if b.Rows[b.Cursor].Name != "a/b.rex.json" {
		t.Fatalf("selected %#v", b.Rows[b.Cursor])
	}
}

func TestFindRanksTheExactNameFirst(t *testing.T) {
	a, root := libraryApp(t, "auth.rex.json", "oauth.rex.json", "authz-later.rex.json")
	typeText(a, "auth")
	b := a.plans
	var got []string
	for _, row := range b.Rows {
		got = append(got, row.Name)
	}
	want := []string{"auth.rex.json", "authz-later.rex.json", "oauth.rex.json"}
	for i, name := range want {
		if i >= len(got) || got[i] != name {
			t.Fatalf("order %v, want %v first", got, want)
		}
	}
	press(a, "\r")
	if b.Current != filepath.Join(root, "auth.rex.json") {
		t.Fatalf("name enter opened %q", b.Current)
	}
}

func TestFindIgnoresTheSuffixAndBlankQueries(t *testing.T) {
	a, _ := libraryApp(t, "alpha.rex.json", "beta.rex.json")
	typeText(a, "json")
	if rows := a.plans.Rows; len(rows) != 1 || rows[0].Kind != PlanRowCreateNamed {
		t.Fatalf("the .rex.json suffix matched: %#v", rows)
	}
	press(a, "\x15")
	typeText(a, "   ")
	if a.plans.Find != "" || a.plans.Rows[a.plans.Cursor].Group != groupRecent {
		t.Fatalf("spaces alone keep the list: find=%q row=%#v", a.plans.Find, a.plans.Rows[a.plans.Cursor])
	}
}

func TestFindSaysWhenMatchesAreLeftOut(t *testing.T) {
	var names []string
	for i := 0; i < maxMatches+5; i++ {
		names = append(names, "p"+string(rune('a'+i%26))+string(rune('a'+i/26))+".rex.json")
	}
	a, _ := libraryApp(t, names...)
	typeText(a, "p")
	if !strings.Contains(viewText(BuildPlanView(a.plans, 60)), "+5 more · keep typing") {
		t.Fatal("left-out matches are counted")
	}
}

// The page title is chrome: it stays while a long folder scrolls, and an
// error far down the list is drawn above it.
func TestPageTitleNeverScrollsAway(t *testing.T) {
	var names []string
	for i := 0; i < 30; i++ {
		names = append(names, "many/p"+string(rune('a'+i%26))+string(rune('a'+i/26))+".rex.json")
	}
	a, root := libraryApp(t, names...)
	os.WriteFile(filepath.Join(root, "many", "zz-notes.txt"), []byte("x"), 0o644)
	a.settle(110, 20)
	browseTo(t, a, "many")
	for i := 0; i < 40; i++ {
		press(a, "down")
		if title := textOf(a.frameRows().Rows[2], a.columns); !strings.HasPrefix(title, "  PLANS › many/") {
			t.Fatalf("the page title scrolled: %q", title)
		}
	}
	for a.plans.Rows[a.plans.Cursor].Kind != PlanRowFile {
		press(a, "up")
		a.frame()
	}
	press(a, "\r")
	if frame := a.frame(); !strings.Contains(frame, "not a .rex.json") {
		t.Fatalf("the error is drawn above the scrolled list\n%s", frame)
	}
}

func TestOutsideGitNoticeShowsOnASmallScreen(t *testing.T) {
	repo := t.TempDir()
	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 60, 20)
	a.settle(60, 20)
	if frame := a.frame(); a.at != atWait || !strings.Contains(frame, "not in") {
		t.Fatalf("the outside-git notice is on the first frame, the wait screen\n%s", frame)
	}
	press(a, "esc")
	if frame := a.frame(); a.at != atLibrary || !strings.Contains(frame, "not in") {
		t.Fatalf("and on the plan list\n%s", frame)
	}

	// Start opens a plan rather than the list: the notice is row 1 on the
	// first frames, fitted, and so is an old rex-plans/ folder's.
	seeded := t.TempDir()
	seedPlans(t, seeded, "one.rex.json")
	b := newApp("yellow", false, "idle", NewPlanBrowser(seeded), 60, 20)
	b.settle(60, 20)
	if row := b.frameRows().Rows[1]; b.at != atOutline || !strings.HasPrefix(row, "not in a git repo · plans go to …") || !strings.HasSuffix(row, "/rex/plans") {
		t.Fatalf("at=%d row 1 %q\n%s", b.at, row, b.frame())
	}
	legacy := t.TempDir()
	os.Mkdir(filepath.Join(legacy, ".git"), 0o755)
	seedPlans(t, legacy, "one.rex.json")
	os.MkdirAll(filepath.Join(legacy, "rex-plans"), 0o755)
	os.WriteFile(filepath.Join(legacy, "rex-plans", "old.rex.json"), []byte("{}"), 0o644)
	c := newApp("yellow", false, "idle", NewPlanBrowser(legacy), 60, 20)
	if row := c.frameRows().Rows[1]; c.at != atOutline || !strings.Contains(row, "rex-plans/") {
		t.Fatalf("at=%d row 1 %q", c.at, row)
	}
}

func TestErrorsClearOnAnyKey(t *testing.T) {
	a, _ := libraryApp(t, "x.rex.json")
	for _, key := range []string{"\t", CtrlC, "\x0c"} {
		a.pane = PanePlans
		a.quitPending = false
		a.plans.Err = "boom"
		press(a, key)
		if a.plans.Err != "" {
			t.Fatalf("%q left the error", key)
		}
	}
}

func TestBrowseNewPlanKeepsTheFilter(t *testing.T) {
	a, root := libraryApp(t, "team/alpha.rex.json")
	browseTo(t, a, "team")
	typeText(a, "roadmap")
	press(a, "\r")
	if a.plans.Screen != ScreenCreate || a.draft != "roadmap" {
		t.Fatalf("screen=%v draft=%q", a.plans.Screen, a.draft)
	}
	if !strings.Contains(a.frame(), "→ rex/plans/team/roadmap.rex.json") {
		t.Fatal("the target is shown")
	}
	press(a, "\r")
	if _, err := os.Stat(filepath.Join(root, "team", "roadmap.rex.json")); err != nil {
		t.Fatal("enter creates the filtered name")
	}
}

func TestNewFolderTakesAPath(t *testing.T) {
	a, root := libraryApp(t, "alpha.rex.json")
	b := a.plans
	b.Screen, b.Dir = ScreenBrowse, root
	if b.CommitFolder("team/q3") != PlanStay || b.Err != "" {
		t.Fatalf("err %q", b.Err)
	}
	if info, err := os.Stat(filepath.Join(root, "team", "q3")); err != nil || !info.IsDir() {
		t.Fatal("a/b makes b inside a")
	}
	b.Dir = root
	if b.CommitFolder("../out"); !strings.Contains(b.Err, "inside rex/plans") {
		t.Fatalf("err %q", b.Err)
	}
	b.Dir = root
	if b.CommitFolder("team/q3"); b.Err != "team/q3 already exists" {
		t.Fatalf("err %q", b.Err)
	}
}

func TestDuplicateCreateNamesTheFolder(t *testing.T) {
	a, _ := libraryApp(t, "team/alpha.rex.json")
	b := a.plans
	b.From = ScreenHome
	if b.CommitCreate("team/alpha"); b.Err != "team/alpha.rex.json already exists" {
		t.Fatalf("err %q", b.Err)
	}
}

func TestHelpPagesOverlapUnderTheMarkers(t *testing.T) {
	a := testApp(t)
	a.settle(80, 12)
	typeText(a, "?")
	press(a, "\r")
	page := PaneBodyHeight(a.lines)
	press(a, "pagedown")
	if got := a.overlayScroll[a.pane]; got != page-2 {
		t.Fatalf("pagedown moved %d, want %d", got, page-2)
	}
}

func TestHomeTakesWordsLiterally(t *testing.T) {
	// In the plan list text is a name (docs/ux.md §2): done enter finds or creates
	// "done"; a /word is only ever /help or /breakdance.
	a, root := libraryApp(t, "alpha.rex.json")
	typeText(a, "done")
	if !strings.Contains(a.frame(), "enter create") {
		t.Fatalf("the footer says enter creates\n%s", a.frame())
	}
	press(a, "\r")
	if _, err := os.Stat(filepath.Join(root, "done.rex.json")); err != nil {
		t.Fatal("done enter on Home creates done.rex.json")
	}
}

func TestHomeFolderIsThePlansRoot(t *testing.T) {
	a, _ := libraryApp(t, "team/alpha.rex.json")
	b := a.plans
	b.Screen, b.From, b.Dir = ScreenCreate, ScreenBrowse, filepath.Join(b.Root, "team")
	b.CommitCreate("beta")
	b.Screen = ScreenHome
	if got := b.RelDir(); got != "rex/plans" {
		t.Fatalf("Home says %q", got)
	}
}

func TestFindCleansPathsLikeCreate(t *testing.T) {
	a, _ := libraryApp(t, "b.rex.json", "a/b.rex.json")
	for _, q := range []string{"./b", "a//b", "a / b"} {
		typeText(a, q)
		b := a.plans
		if len(b.Rows) == 0 || b.Rows[b.Cursor].Kind != PlanRowPlan {
			t.Fatalf("%q finds what it names: %#v", q, b.Rows)
		}
		for _, row := range b.Rows {
			if row.Kind == PlanRowCreateNamed {
				t.Fatalf("%q exists, so no + Create: %#v", q, b.Rows)
			}
		}
		press(a, "\x15")
	}
	typeText(a, ".rex.json")
	if a.plans.Find != "" || a.plans.Rows[a.plans.Cursor].Group != groupRecent {
		t.Fatalf("a bare suffix finds nothing and keeps the list: find=%q", a.plans.Find)
	}
}

func TestNewFolderShowsWhereItGoes(t *testing.T) {
	a, root := libraryApp(t, "alpha.rex.json")
	b := a.plans
	b.Screen, b.Dir, b.Name = ScreenFolder, root, "team/q3"
	if v := viewText(BuildPlanView(b, 60)); !strings.Contains(v, "→ rex/plans/team/q3/") || !strings.Contains(v, "a/b makes folder b in folder a.") {
		t.Fatalf("%s", v)
	}
}

// Find and Browse rows carry the plan's progress; the open plan's says it
// is open (docs/ux.md §11), and never leaves a dangling " · ".
func TestFindAndBrowseRowsCarryProgress(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "rex", "plans", "team")
	os.MkdirAll(root, 0o755)
	work := `{"tasks": [{"id": "x", "status": "done"}, {"id": "y"}]}`
	os.WriteFile(filepath.Join(root, "auth.rex.json"), []byte(work), 0o644)
	os.WriteFile(filepath.Join(root, "auth-v2.rex.json"), []byte(work), 0o644)
	os.WriteFile(filepath.Join(root, "auth-bad.rex.json"), []byte("not json"), 0o644)
	b := NewPlanBrowser(repo)
	b.Current = filepath.Join(root, "auth.rex.json")
	check := func(where, prefix string) {
		t.Helper()
		v := viewText(BuildPlanView(b, 38))
		for name, want := range map[string]string{"auth": "open · 1/2", "auth-v2": "1/2", "auth-bad": "can't read"} {
			found := false
			for _, line := range strings.Split(v, "\n") {
				if f := strings.Fields(line); len(f) > 1 && (f[1] == prefix+name || f[0] == prefix+name) {
					found = true
					if !strings.HasSuffix(line, " "+want) || strings.HasSuffix(line, " · ") {
						t.Errorf("%s: %q, want it to end %q", where, line, want)
					}
				}
			}
			if !found {
				t.Errorf("%s: no row for %s\n%s", where, name, v)
			}
		}
	}
	b.Screen, b.Dir, b.fresh = ScreenBrowse, root, true
	b.Rebuild()
	check("browse", "")
	b.Screen, b.Find, b.fresh = ScreenHome, "auth", true
	b.Rebuild()
	check("find", "team/")
}

// At 7 rows the plan list has two body rows: no group heads and no
// accordion, and the selected row is never the part cut (docs/ux.md §12).
// With no OPEN H2 the open plan's meta says it is open.
func TestSevenRowsListOnlyRows(t *testing.T) {
	for _, cols := range []int{24, 80} {
		a, _, _ := cujApp(t)
		press(a, "esc")
		a.settle(cols, 7)
		if body := strings.Join(a.frameRows().Rows[3:5], "\n"); !strings.Contains(body, "▌ cuj/sendo") || !strings.Contains(body, "open · 1/3") || strings.Contains(body, "─") {
			t.Fatalf("%d×7: the open plan's row, marked open, and no heads\n%s", cols, a.frame())
		}
		empty := testApp(t)
		press(empty, "esc")
		empty.settle(cols, 7)
		if body := strings.Join(empty.frameRows().Rows[3:5], "\n"); !strings.Contains(body, "▌ + New plan") || strings.Contains(body, "No plans") {
			t.Fatalf("%d×7, no plans: + New plan, without the sentence\n%s", cols, empty.frame())
		}
	}
}

// On a short screen the plan list's error outlasts its notice: enter on a
// row says what went wrong, whatever else gives way (docs/ux.md §11).
func TestShortListKeepsTheError(t *testing.T) {
	repo := t.TempDir() // outside git: the list has a notice
	seedPlans(t, repo, "team/a.rex.json", "team/b.rex.json", "team/c.rex.json")
	os.WriteFile(filepath.Join(PlansDir(repo), "team", "notes.txt"), []byte("x"), 0o644)
	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 80, 9)
	press(a, "esc")
	browseTo(t, a, "team")
	pickRow(a, PlanRowFile, "")
	press(a, "\r")
	got := body(a)
	if !strings.Contains(got, "notes.txt is not a .rex.json plan") || strings.Contains(got, "not in a git repo") || !strings.Contains(got, "▌ notes.txt") {
		t.Fatalf("the error and the selection stay, the notice goes\n%s", got)
	}
}
