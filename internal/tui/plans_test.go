package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// docs/ux.md §4: outside git the plan list's notice is its row 1, on the
// Wait screen and on the list, and never a line of the list's body.
func TestOutsideGitShowsWherePlansGo(t *testing.T) {
	dir := t.TempDir()
	b := NewPlanBrowser(dir)
	if !strings.Contains(b.Notice, "not in a git repo") || !strings.Contains(b.Notice, "rex/plans") {
		t.Fatalf("notice %q", b.Notice)
	}
	if strings.Contains(viewText(BuildPlanView(b, 80)), "not in a git repo") {
		t.Fatal("the notice is row 1's, not the body's")
	}
	a := newApp("yellow", false, "idle", b, 120, 32)
	for _, step := range []string{"wait", "list"} {
		if step == "list" {
			press(a, "esc")
		}
		if row := textOf(a.frameRows().Rows[1], 120); !strings.HasPrefix(row, "not in a git repo · plans go to ") {
			t.Fatalf("%s: row 1 %q", step, row)
		}
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

func TestNewFolderCreatesPlansDirAndRespectsARexFile(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	b := NewPlanBrowser(repo)
	b.Screen = ScreenBrowse
	b.Rebuild()
	if b.CommitFolder("grp") != PlanStay || b.Err != "" {
		t.Fatalf("err %q", b.Err)
	}
	for _, p := range []string{"rex/plans/.gitkeep", "rex/plans/grp"} {
		if _, err := os.Stat(filepath.Join(repo, p)); err != nil {
			t.Fatalf("missing %s", p)
		}
	}
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "rex"), []byte("bin"), 0o755)
	c := NewPlanBrowser(other)
	c.Screen = ScreenBrowse
	if c.CommitFolder("grp") != PlanStay || !strings.Contains(c.Err, "move the rex file") {
		t.Fatalf("err %q", c.Err)
	}
	// On the list the notice is row 1 and the error the notice row
	// (docs/ux.md §4): they don't say the same thing twice.
	a := newApp("yellow", false, "idle", NewPlanBrowser(other), 120, 32)
	press(a, "esc")
	a.plans.CommitFolder("grp")
	if f := a.frame(); strings.Count(f, "rex/ is a file here") != 1 || !strings.Contains(f, "✗ move the rex file to create plans") {
		t.Fatalf("the notice and the error must not say the same thing twice\n%s", f)
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
		if !strings.Contains(row, "git") || !strings.HasSuffix(row, "/rex/plans") || cellLen(row) > width-1 {
			t.Fatalf("width %d: %q", width, row)
		}
	}
}

// Every notice keeps its point at the narrowest text column (24 cells),
// at 38 cells and at 80 columns, which give the text all 80 (docs/ux.md
// §12).
func TestNoticesFitNarrowPanes(t *testing.T) {
	cases := []struct{ notice, want string }{
		{outsideGitPrefix + "~/work/clients/acme/very/deep/folder/rex/plans", "/rex/plans"},
		{outsideGitPrefix + "~/作業/顧客/とても長いフォルダの名前/rex/plans", "/rex/plans"},
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

func seedPlans(t *testing.T, repo string, names ...string) string {
	t.Helper()
	root := filepath.Join(repo, "rex", "plans")
	for _, name := range names {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	return root
}

func TestPlanHomeShowsTheGroups(t *testing.T) {
	repo := t.TempDir()
	seedPlans(t, repo, "alpha.rex.json")
	b := NewPlanBrowser(repo)
	if b.Screen != ScreenHome {
		t.Fatal("home first")
	}
	view := viewText(BuildPlanView(b, 48))
	for _, needle := range []string{"RECENT ─", "FOLDERS ─", "rex/plans/", "alpha", "+ New plan", "+ New folder"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("missing %q in\n%s", needle, view)
		}
	}
	if b.Rows[b.Cursor].Group != groupRecent {
		t.Fatal("with plans and none open, the cursor starts on the newest")
	}
}

func TestFirstRunShowsTheActions(t *testing.T) {
	b := NewPlanBrowser(t.TempDir())
	if len(b.Rows) != 2 || b.Rows[0].Kind != PlanRowNew || b.Rows[1].Kind != PlanRowMkdir || b.Cursor != 0 {
		t.Fatalf("rows %#v", b.Rows)
	}
	if _, err := os.Stat(b.Root); !os.IsNotExist(err) {
		t.Fatalf("start must not create the plans dir: %v", err)
	}
	if !strings.Contains(viewText(BuildPlanView(b, 60)), "No plans in rex/plans/ yet") {
		t.Fatal("first-run sentence")
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
	again.From = ScreenHome
	name := "alpha"
	if act := again.CommitCreate(name); act != PlanStay {
		t.Fatal("duplicate must not open")
	}
	if !strings.Contains(again.Err, "already exists") {
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

func TestPlanBrowseFoldersAndCreateFolder(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "rex", "plans")
	if err := os.MkdirAll(filepath.Join(root, "archive", "old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "archive", "sprint.rex.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "archive", "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewPlanBrowser(repo)
	for i, row := range b.Rows {
		if row.Kind == PlanRowDir {
			b.Cursor = i
		}
	}
	if b.Activate() != PlanStay || b.Screen != ScreenBrowse || b.Dir != filepath.Join(root, "archive") {
		t.Fatal("a folder under FOLDERS opens in Browse")
	}
	view := viewText(BuildPlanView(b, 48))
	for _, want := range []string{"  ../", "❯ old/ ", "  sprint ", "  notes.txt", "  + New plan", "  + New folder"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "[ + ") {
		t.Fatalf("actions have no brackets\n%s", view)
	}
	b.Screen, b.From = ScreenFolder, ScreenBrowse // as + New folder in Browse does
	if b.CommitFolder("team") != PlanStay {
		t.Fatal("mkdir")
	}
	if b.Dir != filepath.Join(root, "archive", "team") {
		t.Fatalf("dir %q", b.Dir)
	}
	if _, err := os.Stat(b.Dir); err != nil {
		t.Fatal(err)
	}
}

func TestPlanRecentIsNewestFirst(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "rex", "plans")
	nested := filepath.Join(root, "archive")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "old.rex.json")
	fresh := filepath.Join(nested, "fresh.rex.json")
	if err := os.WriteFile(old, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := os.WriteFile(fresh, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewPlanBrowser(repo)
	if plans := b.plans(); len(plans) == 0 || plans[0].path != fresh {
		t.Fatalf("newest first: %+v", plans)
	}
	view := viewText(BuildPlanView(b, 48))
	if !strings.Contains(view, "RECENT ─") || !strings.Contains(view, "archive/fresh") {
		t.Fatalf("%s", view)
	}
	if b.Rows[0].Path != fresh || b.Rows[1].Path != old {
		t.Fatalf("rows %+v", b.Rows)
	}
	b.Cursor = 0
	if act := b.Activate(); act != PlanOpenChat || b.Current != fresh {
		t.Fatalf("act=%d current=%q", act, b.Current)
	}
	if b.CurrentRel() != "rex/plans/archive/fresh.rex.json" {
		t.Fatalf("rel %q", b.CurrentRel())
	}
}

func TestPlanBrowseDoesNotLeaveRoot(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "rex", "plans")
	nested := filepath.Join(root, "archive", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	b := NewPlanBrowser(repo)
	b.Screen = ScreenBrowse
	b.Dir = nested
	b.Rebuild()
	if !b.Up() || b.Dir != filepath.Join(root, "archive") || !b.Browsing() {
		t.Fatal("up a folder")
	}
	if !b.Up() || b.Screen != ScreenHome || b.Dir != root {
		t.Fatal("up from a top folder is the plan list")
	}
	b.Screen, b.Dir = ScreenBrowse, root
	b.Rebuild()
	if b.Screen != ScreenHome {
		t.Fatal("there is no Browse root: FOLDERS is it")
	}
	b.Screen, b.Dir = ScreenBrowse, filepath.Dir(root)
	b.Rebuild()
	if b.Dir != root || b.Screen != ScreenHome {
		t.Fatalf("Browse never leaves rex/plans: %q", b.Dir)
	}
}

func TestComposeFramePlansPane(t *testing.T) {
	repo := t.TempDir()
	b := NewPlanBrowser(repo)
	page := libraryPage(b)
	frame := ComposeFrame("idle", "yellow", ComposeOptions{
		Enabled: false, Columns: 38, Lines: 24,
		Pane: PanePlans, Plans: b, Composer: true, Page: &page,
	})
	// Row 0 at 38 cells: the title, the meta's short form, the label
	// (docs/ux.md §4).
	if !strings.Contains(frame, "Plans     no plans yet   plan · agent") || !strings.Contains(frame, "❯ + New plan") {
		t.Fatalf("%s", frame)
	}
}
