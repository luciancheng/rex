package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestFindRepoRootStopsWithoutGit(t *testing.T) {
	dir := t.TempDir()
	got := FindRepoRoot(dir)
	if got != dir && !strings.HasPrefix(got, dir) {
		t.Fatalf("walked away from %s -> %s", dir, got)
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

func TestEnsurePlansDirReplacesBinaryClash(t *testing.T) {
	repo := t.TempDir()
	clash := filepath.Join(repo, "rex")
	if err := os.WriteFile(clash, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := EnsurePlansDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected plans dir, got %v", err)
	}
}

func TestPlanHomeShowsActions(t *testing.T) {
	repo := t.TempDir()
	b := NewPlanBrowser(repo)
	if b.Screen != ScreenHome {
		t.Fatal("home first")
	}
	if _, err := os.Stat(b.Root); err != nil {
		t.Fatalf("plans dir: %v", err)
	}
	view := viewText(BuildPlanView(b, 48))
	for _, needle := range []string{"Create a plan", "Open a plan", "Open latest", "rex/plans"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("missing %q in\n%s", needle, view)
		}
	}
}

func TestPlanCardSelectionSpansBox(t *testing.T) {
	repo := t.TempDir()
	b := NewPlanBrowser(repo)
	b.Cursor = 2
	view := BuildPlanView(b, 48)
	var selected []LogLine
	for _, line := range view {
		if strings.HasPrefix(strings.TrimLeft(line.Text, " "), "▌") || strings.HasPrefix(line.Text, "▌") {
			selected = append(selected, line)
		}
	}
	if len(selected) != 3 {
		t.Fatalf("want 3 highlighted card rows, got %d\n%s", len(selected), viewText(view))
	}
	for _, line := range selected {
		if line.Kind != "pick" || line.Fill != "pickbg" {
			t.Fatalf("incomplete highlight %#v", line)
		}
		if !strings.HasPrefix(line.Text, "▌ ") {
			t.Fatalf("bar missing %q", line.Text)
		}
	}
}

func TestPlanCreateWarnsOnDuplicate(t *testing.T) {
	repo := t.TempDir()
	b := NewPlanBrowser(repo)
	draft := "alpha"
	b.Screen = ScreenCreate
	_, open := handlePlanKey(b, &draft, "\r", 4)
	if !open {
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

func TestPlanBrowseFoldersAndCreateFolder(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "rex", "plans")
	if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sprint.rex.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewPlanBrowser(repo)
	b.Cursor = 1 // Open a plan
	if b.Activate() != PlanStartBrowse || b.Screen != ScreenBrowse {
		t.Fatal("open should browse")
	}
	view := viewText(BuildPlanView(b, 48))
	if !strings.Contains(view, "[ + New folder ]") || !strings.Contains(view, "[ + New plan ]") {
		t.Fatalf("actions\n%s", view)
	}
	if !strings.Contains(view, "archive/") || !strings.Contains(view, "sprint.rex.json") {
		t.Fatalf("%s", view)
	}
	if !strings.Contains(view, "notes.txt") {
		t.Fatal("other files")
	}
	if strings.Contains(view, "folder  archive") || strings.Contains(view, "plan    sprint") {
		t.Fatalf("old glyphs\n%s", view)
	}
	draft := "team"
	b.Screen = ScreenFolder
	if b.CommitFolder(draft) != PlanStay {
		t.Fatal("mkdir")
	}
	if b.Dir != filepath.Join(root, "team") {
		t.Fatalf("dir %q", b.Dir)
	}
	if _, err := os.Stat(b.Dir); err != nil {
		t.Fatal(err)
	}
}

func TestPlanOpenLatestAndRecents(t *testing.T) {
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
	if b.Latest() != fresh {
		t.Fatalf("latest %q", b.Latest())
	}
	view := viewText(BuildPlanView(b, 48))
	if !strings.Contains(view, "recents") || !strings.Contains(view, "archive/fresh.rex.json") {
		t.Fatalf("%s", view)
	}
	b.Cursor = 2
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
	nested := filepath.Join(root, "archive")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	b := NewPlanBrowser(repo)
	b.Screen = ScreenBrowse
	b.Dir = nested
	b.Rebuild()
	if !b.Up() || b.Dir != b.Root {
		t.Fatal("up to root")
	}
	if !b.Up() || b.Screen != ScreenHome {
		t.Fatal("up from root goes home")
	}
}

func TestComposeFramePlansPane(t *testing.T) {
	repo := t.TempDir()
	b := NewPlanBrowser(repo)
	frame := ComposeFrame("idle", "yellow", ComposeOptions{
		Enabled: false, Columns: 80, Lines: 24, AnimationName: "idle",
		Pane: PanePlans, Plans: b,
	})
	if !strings.Contains(frame, "── plans") || !strings.Contains(frame, "Create a plan") {
		t.Fatalf("%s", frame)
	}
}
