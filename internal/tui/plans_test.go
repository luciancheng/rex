package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/plan"
)

func TestPlanFileName(t *testing.T) {
	if PlanFileName("sprint") != "sprint.rex.json" {
		t.Fatalf("%q", PlanFileName("sprint"))
	}
	if PlanFileName("sprint.rex.json") != "sprint.rex.json" {
		t.Fatal("keep suffix")
	}
	if PlanFileName("../x") != "x.rex.json" {
		t.Fatal("basename only")
	}
	if PlanFileName(" .rex.json ") != "" {
		t.Fatal("empty stem")
	}
}

func TestEnsurePlansDirCreatesFolder(t *testing.T) {
	repo := t.TempDir()
	root, err := EnsurePlansDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	if root != filepath.Join(repo, "rex", "plans") {
		t.Fatalf("root %q", root)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("missing dir %v", err)
	}
}

func TestEnsurePlansDirKeepsAFileNamedRex(t *testing.T) {
	repo := t.TempDir()
	clash := filepath.Join(repo, "rex")
	if err := os.WriteFile(clash, []byte("#!/bin/sh\necho mine\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsurePlansDir(repo); err == nil {
		t.Fatal("expected an error instead of replacing the file")
	}
	b := NewPlanBrowser(repo)
	if !strings.Contains(b.Notice, "rex/ is a file here") {
		t.Fatalf("notice %q", b.Notice)
	}
	if act := b.CommitCreate("alpha"); act != PlanStay || !strings.Contains(b.Err, "move the rex file") {
		t.Fatalf("create should refuse: act=%d err=%q", act, b.Err)
	}
	data, err := os.ReadFile(clash)
	if err != nil || string(data) != "#!/bin/sh\necho mine\n" {
		t.Fatalf("the user's rex file was touched: %v %q", err, data)
	}
}

func TestStartCreatesNothing(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := NewPlanBrowser(repo)
	if b.Notice != "" {
		t.Fatalf("notice in a repo: %q", b.Notice)
	}
	if _, err := os.Stat(filepath.Join(repo, "rex")); !os.IsNotExist(err) {
		t.Fatalf("start created rex/: %v", err)
	}
	if act := b.CommitCreate("first"); act != PlanOpenChat {
		t.Fatalf("create act=%d err=%q", act, b.Err)
	}
	for _, p := range []string{"rex/plans/.gitkeep", "rex/plans/first.rex.json"} {
		if _, err := os.Stat(filepath.Join(repo, p)); err != nil {
			t.Fatalf("create should make %s: %v", p, err)
		}
	}
}

// docs/ux.md §4, §11: outside git, Plans says so on row 0 and row 1, the
// Create a plan card says where Rex makes the folder, and start creates
// nothing there.
func TestOutsideGitShowsWherePlansGo(t *testing.T) {
	dir := t.TempDir()
	b := NewPlanBrowser(dir)
	if !strings.HasPrefix(b.Notice, "Not in a git repo · plans go in ") || !strings.HasSuffix(b.Notice, "/rex/plans/") {
		t.Fatalf("notice %q", b.Notice)
	}
	if strings.Contains(viewText(BuildPlanView(b, 80)), "Not in a git repo") {
		t.Fatal("the notice is row 1's, not the body's")
	}
	a := newApp("yellow", false, "idle", b, 120, 32)
	rows := a.frameRows().Rows
	if head := textOf(rows[0], 120); !strings.HasSuffix(head, " no git repo   plan · agent") {
		t.Fatalf("row 0 %q", head)
	}
	if row := textOf(rows[1], 120); !strings.HasPrefix(row, "Not in a git repo · plans go in ") {
		t.Fatalf("row 1 %q", row)
	}
	if body := viewText(BuildPlanView(b, 300)); !strings.Contains(body, "❯ Create a plan   Rex makes "+homePath(b.Root)+"/ for it\n") || !strings.Contains(body, "Start Rex from your project's folder") {
		t.Fatalf("the card says where plans go\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(dir, "rex")); !os.IsNotExist(err) {
		t.Fatalf("start outside git created rex/: %v", err)
	}
}

func TestStartLeavesLegacyPlansAlone(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	legacy := filepath.Join(repo, "rex-plans")
	if err := os.MkdirAll(filepath.Join(legacy, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(legacy, "old.rex.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(legacy, "src", "main.go"), []byte("package main"), 0o644)
	b := NewPlanBrowser(repo)
	if _, err := os.Stat(filepath.Join(repo, "rex")); !os.IsNotExist(err) {
		t.Fatal("start must not create rex/ because rex-plans/ exists")
	}
	for _, p := range []string{"rex-plans/old.rex.json", "rex-plans/src/main.go"} {
		if _, err := os.Stat(filepath.Join(repo, p)); err != nil {
			t.Fatalf("start moved %s: %v", p, err)
		}
	}
	if !strings.Contains(b.Notice, "rex-plans/ has 1 old plan") {
		t.Fatalf("notice %q", b.Notice)
	}
}

// docs/ux.md §11: with plans off Plans has no card to pick, says how to
// turn them on, and a find's + Create is refused with the cause on the
// notice row, not twice.
func TestPlansOffRefusesToCreate(t *testing.T) {
	other := t.TempDir()
	os.Mkdir(filepath.Join(other, ".git"), 0o755)
	os.WriteFile(filepath.Join(other, "rex"), []byte("bin"), 0o755)
	a := newApp("yellow", false, "idle", NewPlanBrowser(other), 120, 32)
	if len(a.plans.Rows) != 0 || !strings.Contains(a.frame(), "A file named rex sits where the rex/plans/ folder goes.") || strings.Contains(a.frame(), "❯") {
		t.Fatalf("no cards, and how to turn plans on\n%s", a.frame())
	}
	if f := a.frame(); !strings.Contains(f, "tab agent · ? help") || strings.Contains(f, "enter") {
		t.Fatalf("nothing for enter to do\n%s", f)
	}
	typeText(a, "grp")
	press(a, "\r")
	if f := a.frame(); strings.Count(f, "rex/ is a file here") > 1 || !strings.Contains(f, "✗ move the rex file to create plans") {
		t.Fatalf("the notice and the error must not say the same thing twice\n%s", f)
	}
	if data, _ := os.ReadFile(filepath.Join(other, "rex")); string(data) != "bin" {
		t.Fatal("the rex file was touched")
	}
}

func TestRexFileNoticeClearsWhenMovedAway(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	clash := filepath.Join(repo, "rex")
	os.WriteFile(clash, []byte("bin"), 0o755)
	b := NewPlanBrowser(repo)
	if b.Notice != rexFileNotice {
		t.Fatalf("%q", b.Notice)
	}
	os.Rename(clash, clash+".bin")
	b.Rebuild()
	if b.Notice != "" {
		t.Fatalf("stale notice %q", b.Notice)
	}
}

func TestDanglingRexSymlinkTurnsPlansOff(t *testing.T) {
	repo := t.TempDir()
	if err := os.Symlink(filepath.Join(repo, "nowhere"), filepath.Join(repo, "rex")); err != nil {
		t.Skip("symlinks unavailable")
	}
	b := NewPlanBrowser(repo)
	if b.Notice != rexFileNotice {
		t.Fatalf("%q", b.Notice)
	}
}

// docs/ux.md §4: row 1's outside-git notice keeps where plans go however
// narrow the row.
func TestOutsideGitNoticeKeepsWherePlansGo(t *testing.T) {
	deep := filepath.Join(t.TempDir(), "a", "very", "deep", "folder", "that", "does", "not", "fit")
	os.MkdirAll(deep, 0o755)
	b := NewPlanBrowser(deep)
	for _, width := range []int{40, 60, 80} {
		row := strings.TrimRight(statusRow(noticeLine(b.Notice), width, false), " ")
		if !strings.Contains(row, "git") || !strings.HasSuffix(row, "/rex/plans/") || cellLen(row) > width-1 {
			t.Fatalf("width %d: %q", width, row)
		}
	}
}

// Every notice keeps its point at the narrowest text column (24 cells),
// at 38 cells and at 80 columns, which give the text all 80 (docs/ux.md
// §12).
func TestNoticesFitNarrowPanes(t *testing.T) {
	cases := []struct{ notice, want string }{
		{outsideGitPrefix + "~/work/clients/acme/very/deep/folder/rex/plans/", "/rex/plans/"},
		{outsideGitPrefix + "~/作業/顧客/とても長いフォルダの名前/rex/plans/", "/rex/plans/"},
		{rexFileNotice, "plans off"},
		{"rex-plans/ has 3 old plan(s) · move them into rex/plans", "rex-plans/"},
	}
	for _, width := range []int{PaneTextWidth(24, 24) - 2, PaneTextWidth(38, 24) - 2, PaneTextWidth(80, 24) - 2} {
		for _, c := range cases {
			got := fitNotice(c.notice, width)
			if cellLen(got) > width || !strings.Contains(got, c.want) {
				t.Errorf("width %d: %q -> %q", width, c.notice, got)
			}
		}
	}
}

// seedPlans writes each name as an empty plan under repo's rex/plans,
// each newer than the last by writePlan's clock.
func seedPlans(t *testing.T, repo string, names ...string) string {
	t.Helper()
	for _, name := range names {
		writePlan(t, repo, name, "{}\n")
	}
	return PlansDir(repo)
}

// docs/ux.md §11: Plans is the cards, in #70's order, each only when it
// does something, then Recent; with no plan to continue the cursor starts
// on Open latest.
func TestPlansShowsTheCards(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	seedPlans(t, repo, "alpha.rex.json", "beta.rex.json")
	b := NewPlanBrowser(repo)
	if b.Screen != ScreenHome {
		t.Fatal("Plans first")
	}
	var kinds []PlanRowKind
	for _, row := range b.Rows {
		kinds = append(kinds, row.Kind)
	}
	if want := []PlanRowKind{PlanRowNew, PlanRowOpen, PlanRowLatest, PlanRowPlan}; !slices.Equal(kinds, want) {
		t.Fatalf("rows %v, want %v", kinds, want)
	}
	view := viewText(BuildPlanView(b, 80))
	for _, needle := range []string{"  Create a plan   name it; you or your agent fill in the tasks", "  Open a plan     browse rex/plans/ · 2 plans", "❯ Open latest     beta · no tasks · changed ", "\n\n  Recent\n  alpha "} {
		if !strings.Contains(view, needle) {
			t.Fatalf("missing %q in\n%s", needle, view)
		}
	}
	if b.Rows[b.Cursor].Kind != PlanRowLatest {
		t.Fatal("with plans and none to continue, the cursor starts on Open latest")
	}
}

// docs/ux.md §11: a first run is the Create a plan card alone, and start
// makes no folder for it.
func TestFirstRunShowsOnlyCreate(t *testing.T) {
	b := NewPlanBrowser(t.TempDir())
	if len(b.Rows) != 1 || b.Rows[0].Kind != PlanRowNew || b.Cursor != 0 {
		t.Fatalf("rows %#v", b.Rows)
	}
	if _, err := os.Stat(b.Root); !os.IsNotExist(err) {
		t.Fatalf("start must not create the plans dir: %v", err)
	}
}

func TestPlanCreateWarnsOnDuplicate(t *testing.T) {
	repo := t.TempDir()
	b := NewPlanBrowser(repo)
	b.Screen = ScreenCreate
	if b.CommitCreate("alpha") != PlanOpenChat {
		t.Fatal("first create should open")
	}
	again := NewPlanBrowser(repo)
	again.Screen = ScreenCreate
	if act := again.CommitCreate("alpha"); act != PlanStay {
		t.Fatal("duplicate must not open")
	}
	if again.Err != "alpha already exists · pick another name" {
		t.Fatalf("err %q", again.Err)
	}
}

func TestPlanCreateWritesTheV1Stub(t *testing.T) {
	repo := t.TempDir()
	b := NewPlanBrowser(repo)
	b.Screen = ScreenCreate
	if b.CommitCreate("Auth Flow") != PlanOpenChat {
		t.Fatal(b.Err)
	}
	p, err := plan.Load(b.Current)
	if err != nil || p.Version != plan.Version || p.ID != "auth-flow" || len(p.Problems) != 0 {
		t.Fatalf("%v %+v", err, p)
	}
	before, _ := os.ReadFile(b.Current)
	again := NewPlanBrowser(repo)
	again.Screen = ScreenCreate
	again.CommitCreate("Auth Flow")
	if after, _ := os.ReadFile(b.Current); string(after) != string(before) {
		t.Fatal("a duplicate create must not touch the file")
	}
}

// docs/ux.md §11: Open a plan lists a folder's folders, with their plans,
// then its plans; no ../, no files that aren't plans, no hidden folders,
// no + rows.
func TestOpenAPlanListsFoldersThenPlans(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "rex", "plans")
	for _, dir := range []string{"archive/old", "archive/.hidden", "zeta"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"archive/sprint.rex.json", "archive/old/a.rex.json", "archive/.hidden/b.rex.json", "zeta/z.rex.json"} {
		os.WriteFile(filepath.Join(root, name), []byte("{}\n"), 0o644)
	}
	os.WriteFile(filepath.Join(root, "archive", "notes.txt"), []byte("x"), 0o644)
	b := NewPlanBrowser(repo)
	b.pick(PlanRowOpen)
	if b.Activate() != PlanStay || b.Screen != ScreenBrowse || b.Dir != root {
		t.Fatal("Open a plan browses rex/plans")
	}
	if got := viewText(BuildPlanView(b, 48)); got != "❯ archive/                              2 plans\n  zeta/                                  1 plan\n" {
		t.Fatalf("rex/plans:\n%s", got)
	}
	if b.Activate() != PlanStay || b.Dir != filepath.Join(root, "archive") {
		t.Fatalf("enter goes into the folder: %q", b.Dir)
	}
	view := viewText(BuildPlanView(b, 48))
	if view != "❯ old/                                   1 plan\n  sprint                               no tasks\n" {
		t.Fatalf("archive/:\n%s", view)
	}
}

func TestPlanBrowseDoesNotLeaveRoot(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "rex", "plans")
	nested := filepath.Join(root, "archive", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	seedPlans(t, repo, "x.rex.json")
	b := NewPlanBrowser(repo)
	b.browse(nested)
	if !b.Up() || b.Dir != filepath.Join(root, "archive") || !b.Browsing() {
		t.Fatal("up a folder")
	}
	if !b.Up() || b.Dir != root || !b.Browsing() || b.Rows[b.Cursor].Name != "archive" {
		t.Fatal("up to rex/plans itself, on the folder")
	}
	if !b.Up() || b.Screen != ScreenHome || b.Rows[b.Cursor].Kind != PlanRowOpen {
		t.Fatal("up from rex/plans is Plans, on the Open a plan card")
	}
	b.Screen, b.Dir = ScreenBrowse, filepath.Dir(root)
	b.Rebuild()
	if b.Dir != root {
		t.Fatalf("Open a plan never leaves rex/plans: %q", b.Dir)
	}
}

func TestComposeFramePlansPane(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	b := NewPlanBrowser(repo)
	page := libraryPage(b)
	frame := ComposeFrame("idle", "yellow", ComposeOptions{
		Enabled: false, Columns: 38, Lines: 24,
		Pane: PanePlans, Plans: b, Page: &page,
	})
	// Row 0 at 38 cells: the title, the meta's short form, the label
	// (docs/ux.md §4).
	if !strings.Contains(frame, "Plans     no plans yet   plan · agent") || !strings.Contains(frame, "❯ Create a plan   name it; you or yo…") {
		t.Fatalf("%s", frame)
	}
}
