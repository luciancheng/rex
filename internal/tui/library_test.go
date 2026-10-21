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

func TestFindComparesTheWholeName(t *testing.T) {
	// A root plan b doesn't count as a/b: + Create 'a/b' is offered, and
	// ⏎ makes it in folder a.
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
		t.Fatalf("name⏎ opened %q", b.Current)
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
	if a.plans.Find != "" || a.plans.Rows[a.plans.Cursor].Kind != PlanRowLatest {
		t.Fatalf("spaces alone keep the cards: find=%q row=%#v", a.plans.Find, a.plans.Rows[a.plans.Cursor])
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

func TestLibraryHeaderNeverScrollsAway(t *testing.T) {
	var names []string
	for i := 0; i < 30; i++ {
		names = append(names, "many/p"+string(rune('a'+i%26))+string(rune('a'+i/26))+".rex.json")
	}
	a, root := libraryApp(t, names...)
	os.WriteFile(filepath.Join(root, "many", "zz-notes.txt"), []byte("x"), 0o644)
	a.settle(110, 20)
	if frame := a.frame(); !strings.Contains(frame, "── plans") || !strings.Contains(frame, "rex/plans") {
		t.Fatalf("first frame keeps the title and folder\n%s", frame)
	}
	for i, row := range a.plans.Rows {
		if row.Kind == PlanRowOpen {
			a.plans.Cursor = i
		}
	}
	press(a, "\r", "\r") // Browse, then into many/
	for i := 0; i < 40; i++ {
		press(a, "down")
		a.frame()
	}
	for i := 0; i < 40; i++ {
		press(a, "up")
		a.frame()
	}
	if frame := a.frame(); !strings.Contains(frame, "── open") || !strings.Contains(frame, "rex/plans/many") {
		t.Fatalf("back at the top, the title and folder show\n%s", frame)
	}
	// ⏎ on a file far down the list says why, on screen.
	for a.plans.Rows[a.plans.Cursor].Kind != PlanRowFile {
		press(a, "down")
		a.frame()
	}
	press(a, "\r")
	if frame := a.frame(); !strings.Contains(frame, "not a .rex.json") {
		t.Fatalf("the error is drawn above the scrolled list\n%s", frame)
	}
}

func TestOutsideGitNoticeShowsOnASmallScreen(t *testing.T) {
	repo := t.TempDir()
	seedPlans(t, repo, "one.rex.json")
	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 60, 20)
	a.settle(60, 20)
	if frame := a.frame(); !strings.Contains(frame, "not in") {
		t.Fatalf("the outside-git notice is on the first frame\n%s", frame)
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
	a, root := libraryApp(t, "alpha.rex.json")
	for i, row := range a.plans.Rows {
		if row.Kind == PlanRowOpen {
			a.plans.Cursor = i
		}
	}
	press(a, "\r")
	typeText(a, "roadmap")
	press(a, "\r")
	if a.plans.Screen != ScreenCreate || a.draft != "roadmap" {
		t.Fatalf("screen=%v draft=%q", a.plans.Screen, a.draft)
	}
	if !strings.Contains(a.frame(), "Creates rex/plans/roadmap.rex.json") {
		t.Fatal("the target is shown")
	}
	press(a, "\r")
	if _, err := os.Stat(filepath.Join(root, "roadmap.rex.json")); err != nil {
		t.Fatal("⏎ creates the filtered name")
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
	// In the Library a verb is just a name (docs/ux.md §2): done⏎ finds or
	// creates "done", and only /done would run the verb.
	a, root := libraryApp(t, "alpha.rex.json")
	typeText(a, "done")
	if !strings.Contains(a.frame(), "⏎ create") {
		t.Fatalf("the footer says ⏎ creates\n%s", a.frame())
	}
	press(a, "\r")
	if _, err := os.Stat(filepath.Join(root, "done.rex.json")); err != nil {
		t.Fatal("done⏎ on Home creates done.rex.json")
	}
}

func TestHomeFolderLineIsThePlansRoot(t *testing.T) {
	a, _ := libraryApp(t, "team/alpha.rex.json")
	b := a.plans
	b.Screen, b.From, b.Dir = ScreenCreate, ScreenBrowse, filepath.Join(b.Root, "team")
	b.CommitCreate("beta")
	b.Screen = ScreenHome
	if got := b.RelDir(); got != "rex/plans" {
		t.Fatalf("Home says %q", got)
	}
}
