package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func libraryApp(t *testing.T, plans ...string) (*app, string) {
	t.Helper()
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	root := seedPlans(t, repo, plans...)
	return newApp("yellow", false, "idle", NewPlanBrowser(repo), 120, 32), root
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
	if !strings.Contains(a.frame(), "[ + Create 'fresh-idea' ]") {
		t.Fatal("create row shown")
	}
	press(a, "\r")
	if _, err := os.Stat(filepath.Join(root, "fresh-idea.rex.json")); err != nil {
		t.Fatal("⏎ on + Create makes the plan")
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
	a, root := libraryApp(t, "zeta.rex.json", "team/alpha.rex.json")
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644)
	for i, row := range a.plans.Rows {
		if row.Kind == PlanRowOpen {
			a.plans.Cursor = i
		}
	}
	press(a, "\r")
	b := a.plans
	if b.Rows[b.Cursor].Kind != PlanRowDir {
		t.Fatalf("cursor should start on the first folder, got %#v", b.Rows[b.Cursor])
	}
	if last := b.Rows[len(b.Rows)-1]; last.Kind != PlanRowNew {
		t.Fatal("+ rows sit below the listing")
	}
	typeText(a, "zet")
	if b.Rows[b.Cursor].Name != "zeta.rex.json" {
		t.Fatalf("filter selects its first match: %#v", b.Rows[b.Cursor])
	}
	press(a, "\r")
	if !strings.HasSuffix(a.plans.Current, "zeta.rex.json") {
		t.Fatal("⏎ after a filter opens the match, not New folder")
	}
}

func TestBrowseFolderChangeClearsFilterAndFileExplains(t *testing.T) {
	a, root := libraryApp(t, "team/alpha.rex.json")
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644)
	for i, row := range a.plans.Rows {
		if row.Kind == PlanRowOpen {
			a.plans.Cursor = i
		}
	}
	press(a, "\r")
	typeText(a, "tea")
	press(a, "\r")
	if !strings.HasSuffix(a.plans.Dir, "team") || a.draft != "" || a.plans.Filter != "" {
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
	if !strings.Contains(viewText(BuildPlanView(a.plans, 60)), "no match for 'zzz'") {
		t.Fatal("no-match line")
	}
}

func TestCreateInASubfolder(t *testing.T) {
	a, root := libraryApp(t, "seed.rex.json")
	a.plans.Cursor = 0
	press(a, "\r")
	typeText(a, "onboarding/auth")
	if !strings.Contains(a.frame(), "Creates rex/plans/onboarding/auth.rex.json") {
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

func TestSelectionFollowsThePlanNotTheRow(t *testing.T) {
	a, root := libraryApp(t, "older.rex.json", "newer.rex.json")
	b := a.plans
	for i, row := range b.Rows {
		if row.Kind == PlanRowRecent && strings.HasSuffix(row.Path, "older.rex.json") {
			b.Cursor = i
		}
	}
	os.WriteFile(filepath.Join(root, "older.rex.json"), []byte("{} "), 0o644) // now the newest
	b.walk = planWalk{}
	b.Rebuild()
	if !strings.HasSuffix(b.Rows[b.Cursor].Path, "older.rex.json") {
		t.Fatalf("selection jumped to %#v", b.Rows[b.Cursor])
	}
}

func TestLongListScrollsWithTheSelection(t *testing.T) {
	var names []string
	for i := 0; i < 40; i++ {
		names = append(names, "group/p"+string(rune('a'+i%26))+string(rune('a'+i/26))+".rex.json")
	}
	a, _ := libraryApp(t, names...)
	a.settle(120, 20)
	for i, row := range a.plans.Rows {
		if row.Kind == PlanRowOpen {
			a.plans.Cursor = i
		}
	}
	press(a, "\r", "\r")
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
