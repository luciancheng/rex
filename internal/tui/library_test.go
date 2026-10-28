package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// libraryApp is Rex started on Plans in a repo with plans (docs/ux.md
// §11), none opened yet.
func libraryApp(t *testing.T, plans ...string) (*app, string) {
	t.Helper()
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	root := seedPlans(t, repo, plans...)
	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 120, 32)
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

// browseTo goes into the folder named name: from Plans through the Open a
// plan card, or from a folder in it.
func browseTo(t *testing.T, a *app, name string) {
	t.Helper()
	if !a.plans.Browsing() {
		pickRow(a, PlanRowOpen, "")
		press(a, "\r")
	}
	pickRow(a, PlanRowDir, name)
	press(a, "\r")
	if !a.plans.Browsing() || filepath.Base(a.plans.Dir) != name {
		t.Fatalf("setup: browse %s: dir=%q", name, a.plans.Dir)
	}
}

// listRepo is the CUJ plan, open and followed, and the other plans of the
// mockups in docs/ux.md §11: a finished plan, one with no title, and the
// newest, each in a folder or at the root.
func listRepo(t *testing.T) *app {
	t.Helper()
	a, _, _ := cujApp(t)
	repo := a.plans.Repo
	var done []string
	for i := 0; i < 12; i++ {
		done = append(done, fmt.Sprintf(`{"id": "t%d", "status": "done"}`, i))
	}
	done = append(done, `{"id": "t12", "status": "skipped"}`) // finished, never done
	writePlan(t, repo, "rex/v1-sendoff-loop.rex.json", `{"version": 1, "title": "Rex v1: the send-off loop", "status": "done", "tasks": [`+strings.Join(done, ", ")+`]}`)
	writePlan(t, repo, "auth-flow.rex.json", `{"version": 1, "tasks": [{"id": "a"}, {"id": "b"}, {"id": "c"}, {"id": "d"}]}`)
	writePlan(t, repo, "ux/redesign.rex.json", `{"version": 1, "title": "Rex visual redesign", "status": "active", "tasks": [{"id": "a", "status": "done"}, {"id": "b", "status": "skipped"}, {"id": "c"}, {"id": "d"}, {"id": "e"}, {"id": "f"}]}`)
	a.plans.walk = planWalk{}
	return a
}

// body is the text column's rows from the body down, trimmed: under the
// title row and row 1, and under row 2's blank from blankFrom rows
// (docs/ux.md §4).
func body(a *app) string {
	var out []string
	for _, r := range a.frameRows().Rows[2+a.layout().blank:] {
		out = append(out, strings.TrimRight(textOf(r, a.columns), " "))
	}
	return strings.Join(out, "\n")
}

// docs/ux.md §11 (A2): Plans is its cards, one row each, the label bold
// in a 16-cell column and what it does muted, in #70's order and each only
// when it does something; then a blank row, Recent and its rows. Under 12
// rows the blank rows go, and the cards never drop below Recent; the list
// says how many rows it hides.
func TestPlansMockups(t *testing.T) {
	a := listRepo(t)
	latest := writePlan(t, a.plans.Repo, "auth/login.rex.json", `{"version": 1, "title": "Login flow", "tasks": [{"id": "a", "status": "done"}, {"id": "b"}, {"id": "c"}]}`)
	when := a.now().Add(-8 * time.Minute) // newer than the rest, on Rex's clock
	os.Chtimes(latest, when, when)
	a.plans.walk = planWalk{}
	press(a, "esc")
	a.settle(80, 24)
	want := `❯ Continue        Send-off demo · 1/3 done · pinned card-highlight
  Create a plan   name it; you or your agent fill in the tasks
  Open a plan     browse rex/plans/ · 4 folders · 5 plans
  Open latest     Login flow · 1/3 done · changed 8m ago

  Recent
  ux/redesign          Rex visual redesign                             1/6 done
  auth-flow            untitled                                        0/4 done
  rex/v1-sendoff-loop  Rex v1: the send-off loop                   ✔ 12/13 done`
	if got := body(a); !strings.HasPrefix(got, want+"\n\n") {
		t.Fatalf("80×24:\n%s\nwant\n%s", got, want)
	}
	rows := a.frameRows().Rows
	if head, ctx := textOf(rows[0], 80), textOf(rows[1], 80); !strings.HasPrefix(head, "Plans ") || !strings.HasSuffix(head, " · 5 plans   plan · agent") || ctx != "Pick up where you left off, or start a new plan." {
		t.Fatalf("row 0 %q, row 1 %q", head, ctx)
	}
	a.settle(80, 10)
	want = `❯ Continue        Send-off demo · 1/3 done · pinned card-highlight
  Create a plan   name it; you or your agent fill in the tasks
  Open a plan     browse rex/plans/ · 4 folders · 5 plans
  Open latest     Login flow · 1/3 done · changed 8m ago
  Recent
  ux/redesign          Rex visual redesign                             1/6 done
  ↓ 2 more`
	if got := strings.Join(strings.Split(body(a), "\n")[:7], "\n"); got != want {
		t.Fatalf("80×10:\n%s\nwant\n%s", got, want)
	}
	a.settle(38, 12)
	want = `❯ Continue        Send-off demo · 1/…
  Create a plan   name it; you or yo…
  Open a plan     browse rex/plans/ …
  Open latest     Login flow · 1/3 d…
  ↓ 3 more`
	if got := strings.Join(strings.Split(body(a), "\n")[:5], "\n"); got != want {
		t.Fatalf("38×12:\n%s\nwant\n%s", got, want)
	}
	// A skipped task is finished, never done: a plan counts as its task
	// list's row 0 does, once opened (docs/ux.md §4, §11).
	a.settle(80, 24)
	for name, meta := range map[string]string{"ux/redesign.rex.json": "1/6 done", "rex/v1-sendoff-loop.rex.json": "12/13 done"} {
		press(a, "esc")
		pickRow(a, PlanRowPlan, name)
		press(a, "\r")
		if head := textOf(a.frameRows().Rows[0], 80); !strings.Contains(head, " "+meta+"   plan · agent") {
			t.Errorf("%s: row 0 %q, Plans said %s", name, head, meta)
		}
	}
}

// docs/ux.md §11: Continue's plan is on no other card and nowhere under
// Recent; Open latest is hidden when it is Continue's plan, and is never
// under Recent either. Every plan Plans names is named once.
func TestContinueIsDedupedAgainstOpenLatestAndRecent(t *testing.T) {
	once := func(t *testing.T, a *app) {
		t.Helper()
		seen := map[string]int{}
		for _, row := range a.plans.Rows {
			if row.Path != "" {
				for p := range seen {
					if samePlan(p, row.Path) {
						t.Fatalf("%s named twice: %+v", row.Name, a.plans.Rows)
					}
				}
				seen[row.Path]++
			}
		}
	}
	// Continue is the newest: no Open latest.
	a := listRepo(t)
	press(a, "esc")
	once(t, a)
	if a.plans.rowOf(PlanRowLatest) >= 0 || a.plans.Rows[0].Kind != PlanRowContinue {
		t.Fatalf("Continue is the newest plan, so there is no Open latest: %+v", a.plans.Rows)
	}
	// Another plan is the newest: Open latest names it, and Recent the
	// rest, the plan you had open earlier first.
	pickRow(a, PlanRowPlan, "auth-flow.rex.json")
	press(a, "\r", "esc")
	once(t, a)
	var kinds []string
	for _, row := range a.plans.Rows {
		kinds = append(kinds, fmt.Sprintf("%d %s", row.Kind, planStem(row.Name)))
	}
	want := []string{
		fmt.Sprintf("%d auth-flow", PlanRowContinue), fmt.Sprintf("%d ", PlanRowNew), fmt.Sprintf("%d ", PlanRowOpen),
		fmt.Sprintf("%d cuj/sendoff-demo", PlanRowLatest), fmt.Sprintf("%d ux/redesign", PlanRowPlan), fmt.Sprintf("%d rex/v1-sendoff-loop", PlanRowPlan),
	}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("rows %v\nwant %v", kinds, want)
	}
	if i := a.plans.rowOf(PlanRowPlan); i < 0 || planStem(a.plans.Rows[i].Name) != "ux/redesign" {
		t.Fatal("Recent never repeats Open latest's plan, which you had open")
	}
}

// docs/ux.md §11: the cursor starts on Continue, else Open latest, else
// Create a plan; back from a plan it is on Continue, the plan you left.
func TestPlansCursorStart(t *testing.T) {
	a := listRepo(t)
	press(a, "esc")
	if row := a.plans.Rows[a.plans.Cursor]; row.Kind != PlanRowContinue || row.Path != a.plans.Current {
		t.Fatalf("on Continue: %+v", row)
	}
	none, _ := libraryApp(t, "older.rex.json", "newer.rex.json")
	if row := none.plans.Rows[none.plans.Cursor]; row.Kind != PlanRowLatest || row.Name != "newer.rex.json" {
		t.Fatalf("with nothing to continue, on Open latest: %+v", row)
	}
	empty := testApp(t)
	os.MkdirAll(filepath.Join(empty.plans.Root, "team"), 0o755)
	if row := empty.plans.Rows[empty.plans.Cursor]; row.Kind != PlanRowNew || len(empty.plans.Rows) != 1 {
		t.Fatalf("with no plans, on Create a plan alone: %+v", empty.plans.Rows)
	}
}

// docs/ux.md §5 (A2) on Plans: a card's label is bold in the bright title
// role and what it does muted; the selected card is purple through and
// through but for the marks that keep their meaning; Recent is a bold
// label, its names plain text, not bold. Nothing on Plans is yellow.
func TestPlansRoles(t *testing.T) {
	a := listRepo(t)
	press(a, "esc", "down")
	lines := planList(a.plans, 78, listFit{body: 19, tall: true})
	for _, line := range lines {
		for _, sp := range line.Spans {
			if sp.Role == "accent" {
				t.Errorf("yellow on Plans: %q", line.Text)
			}
		}
		switch {
		case strings.HasPrefix(line.Text, "  Continue "):
			if line.Spans[1].Role != "title" || !line.Spans[1].Bold || line.Spans[len(line.Spans)-1].Role != "muted" || line.Selected {
				t.Errorf("an unselected card: %+v", line.Spans)
			}
		case strings.HasPrefix(line.Text, "❯ Create a plan "):
			if line.Kind != "pick" || !line.Selected || !line.Spans[1].Bold {
				t.Errorf("the selected card: %+v", line)
			}
			for _, sp := range line.Spans[1:] {
				if sp.Role != "" {
					t.Errorf("the selected card keeps %q in %q, not the selection's purple", sp.Text, sp.Role)
				}
			}
		case line.Text == "  Recent":
			if line.Kind != "h2" || !line.Spans[1].Bold || line.Selected {
				t.Errorf("Recent: %+v", line)
			}
		case strings.HasPrefix(line.Text, "  ux/redesign "):
			if line.Kind != "text" || line.Spans[1].Bold || line.Spans[1].Role != "" {
				t.Errorf("a Recent row's name is plain text: %+v", line.Spans)
			}
		}
	}
}

// docs/ux.md §11: Recent starts with the plans opened earlier in this run,
// the latest first, then the newest: the plan you just left leads it,
// however old its file. Each plan is still named once.
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
	typeText(a, "auth-flow")
	press(a, "\r", "esc")
	typeText(a, "redesign")
	press(a, "\r", "esc")
	if !strings.HasSuffix(a.plans.Current, "redesign.rex.json") {
		t.Fatalf("setup: opened %q", a.plans.Current)
	}
	var recent []string
	for _, row := range a.plans.Rows {
		if row.Kind == PlanRowPlan {
			recent = append(recent, row.Name)
		}
	}
	// The CUJ plan's file is the newest: it is Open latest, not Recent.
	want := []string{"auth-flow.rex.json", "new-5.rex.json", "new-4.rex.json", "new-3.rex.json", "new-2.rex.json"}
	if strings.Join(recent, ",") != strings.Join(want, ",") || a.plans.Rows[a.plans.rowOf(PlanRowLatest)].Name != "cuj/sendoff-demo.rex.json" {
		t.Fatalf("Recent %v, want %v", recent, want)
	}
}

// docs/ux.md §11: plans under a hidden folder are left out, as Open a plan
// leaves the folder out, so every plan the page counts is one it can
// reach; Continue's plan is counted wherever it lives. A folder counts its
// plans as the page does, each file once.
func TestOnePlanSetForEveryCount(t *testing.T) {
	a := listRepo(t)
	hidden := writePlan(t, a.plans.Repo, ".archive/old.rex.json", demoPlan)
	a.plans.walk = planWalk{}
	press(a, "esc")
	if meta := spanText(libraryPage(a.plans).Short); meta != "4 plans" || strings.Contains(body(a), ".archive") {
		t.Fatalf("a hidden folder's plans are left out: %q\n%s", meta, body(a))
	}
	// Left off Plans, not out of reach: a find matches it, and your
	// agent's write of it opens it.
	typeText(a, "old")
	if !strings.Contains(body(a), ".archive/old") {
		t.Fatalf("a find reaches a hidden folder's plan:\n%s", body(a))
	}
	press(a, "esc")
	a.ingest([]watch.Event{wrote(a.plans.Repo, hidden)})
	press(a, "esc")
	if row := a.plans.Rows[a.plans.Cursor]; row.Kind != PlanRowContinue || row.Name != ".archive/old.rex.json" || spanText(libraryPage(a.plans).Short) != "5 plans" {
		t.Fatalf("Continue's plan is counted wherever it lives: %+v %q", row, spanText(libraryPage(a.plans).Short))
	}

	b := listRepo(t)
	if err := os.Symlink(b.plans.Current, filepath.Join(b.plans.Root, "cuj", "alias.rex.json")); err != nil {
		t.Skip("no symlinks here (Windows without the privilege)")
	}
	b.plans.walk = planWalk{}
	press(b, "esc")
	if meta := spanText(libraryPage(b.plans).Short); meta != "4 plans" {
		t.Fatalf("page %q", meta)
	}
	browseTo(t, b, "cuj")
	press(b, "esc")
	for _, row := range b.plans.Rows {
		if row.Kind == PlanRowDir && row.Name == "cuj" && row.Plans != 1 {
			t.Fatalf("cuj/ counts the plan and its link once: %d", row.Plans)
		}
	}
}

// docs/ux.md §4, §11: Find is one flat list under Plans › Find on row 0,
// which counts its matches; row 1 says what it finds; the field is a box
// titled find at the top of the body from 16 rows, one row under that;
// + Create <name> comes last, a blank row above it from 12 rows.
func TestFindMockups(t *testing.T) {
	a := listRepo(t)
	press(a, "esc")
	a.settle(80, 24)
	typeText(a, "re")
	rows := a.frameRows()
	if head, ctx := textOf(rows.Rows[0], 80), textOf(rows.Rows[1], 80); !strings.HasPrefix(head, "Plans › Find ") || !strings.Contains(head, " 2 matches ") || ctx != `Plans whose name or title has "re".` {
		t.Fatalf("row 0 %q, row 1 %q", head, ctx)
	}
	want := `  ╭ find ─────────────────────────────────────────────────────────────────────╮
  │ re                                                                        │
  ╰───────────────────────────────────────────────────────────────────────────╯

❯ ux/redesign          Rex visual redesign                             1/6 done
  rex/v1-sendoff-loop  Rex v1: the send-off loop                   ✔ 12/13 done

  + Create re`
	if got := body(a); !strings.HasPrefix(got, want+"\n\n") {
		t.Fatalf("80×24:\n%s\nwant\n%s", got, want)
	}
	if rows.CursorRow != 5 || rows.CursorCol != 7 {
		t.Fatalf("the cursor sits after the text in the field: %d,%d", rows.CursorRow, rows.CursorCol)
	}
	a.settle(80, 10)
	want = `  find  re
❯ ux/redesign          Rex visual redesign                             1/6 done
  rex/v1-sendoff-loop  Rex v1: the send-off loop                   ✔ 12/13 done
  + Create re`
	if got := body(a); !strings.HasPrefix(got, want+"\n") {
		t.Fatalf("80×10:\n%s\nwant\n%s", got, want)
	}
	if rows := a.frameRows(); rows.CursorRow != 3 || rows.CursorCol != 11 {
		t.Fatalf("the cursor in the one-row field: %d,%d", rows.CursorRow, rows.CursorCol)
	}
	press(a, "esc")
	if a.draft != "" || a.plans.Find != "" || strings.Contains(body(a), "  find  ") || strings.Contains(body(a), "╭") || a.frameRows().CursorRow != 0 {
		t.Fatalf("esc clears the field and the cards come back\n%s", body(a))
	}
}

// docs/ux.md §11: a find matches a plan's title as well as its name,
// after every name match.
func TestFindMatchesTitles(t *testing.T) {
	a, _ := libraryApp(t)
	writePlan(t, a.plans.Repo, "q3.rex.json", `{"version": 1, "title": "Billing revamp", "tasks": []}`)
	writePlan(t, a.plans.Repo, "billing-old.rex.json", `{"version": 1, "tasks": []}`)
	a.plans.walk = planWalk{}
	typeText(a, "billing")
	var got []string
	for _, row := range a.plans.Rows {
		got = append(got, row.Name)
	}
	if want := []string{"billing-old.rex.json", "q3.rex.json", "billing"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("matches %v, want %v", got, want)
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
	if a.plans.Current != filepath.Join(root, "onboarding", "auth.rex.json") || a.follow {
		t.Fatalf("opened %q, latched: follow=%v", a.plans.Current, a.follow)
	}
}

func TestFindWithNoMatchCreatesInOneKey(t *testing.T) {
	a, root := libraryApp(t, "billing.rex.json")
	typeText(a, "fresh-idea")
	if len(a.plans.Rows) != 1 || a.plans.Rows[0].Kind != PlanRowCreateNamed {
		t.Fatalf("%#v", a.plans.Rows)
	}
	if !strings.Contains(a.frame(), "❯ + Create fresh-idea") || strings.Contains(a.frame(), "[ + ") {
		t.Fatal("create row shown, no brackets")
	}
	press(a, "\r")
	if _, err := os.Stat(filepath.Join(root, "fresh-idea.rex.json")); err != nil || a.follow {
		t.Fatal("enter on + Create makes the plan and latches it")
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

func TestFindComparesTheWholeName(t *testing.T) {
	// A root plan b doesn't count as a/b: + Create a/b is offered, and
	// enter makes it in folder a.
	a, root := libraryApp(t, "b.rex.json")
	typeText(a, "a/b")
	b := a.plans
	if len(b.Rows) == 0 || b.Rows[len(b.Rows)-1].Kind != PlanRowCreateNamed {
		t.Fatalf("rows %#v", b.Rows)
	}
	press(a, "end", "\r")
	if _, err := os.Stat(filepath.Join(root, "a", "b.rex.json")); err != nil {
		t.Fatal("+ Create a/b creates b in folder a")
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
	if a.plans.Find != "" || a.plans.Rows[a.plans.Cursor].Kind != PlanRowLatest {
		t.Fatalf("spaces alone keep the cards: find=%q row=%#v", a.plans.Find, a.plans.Rows[a.plans.Cursor])
	}
}

// docs/ux.md §11: a find shows 30 matches and counts the rest, whether
// + Create ends it or not (the exact plan exists, or the text names no
// plan, as "team/" doesn't).
func TestFindSaysWhenMatchesAreLeftOut(t *testing.T) {
	var names []string
	for i := 0; i < maxMatches+5; i++ {
		names = append(names, "team/p"+string(rune('a'+i%26))+string(rune('a'+i/26))+".rex.json")
	}
	a, _ := libraryApp(t, append(names, "team.rex.json")...)
	for q, more := range map[string]string{"team/p": "+5 more", "team": "+6 more", "team/": "+5 more"} {
		press(a, "\x15")
		typeText(a, q)
		view := viewText(BuildPlanView(a.plans, 60))
		create := strings.Contains(view, "+ Create ")
		if !strings.Contains(view, "\n  "+more+" · keep typing") || create != (q == "team/p") {
			t.Errorf("%q: the left-out matches are counted, + Create %v\n%s", q, create, view)
		}
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
	if a.plans.Find != "" || a.plans.Rows[a.plans.Cursor].Kind != PlanRowLatest {
		t.Fatalf("a bare suffix finds nothing and keeps the cards: find=%q", a.plans.Find)
	}
}

// In Plans text is a name (docs/ux.md §2): done enter finds or creates
// "done"; a /word is only ever /help or /breakdance.
func TestPlansTakeWordsLiterally(t *testing.T) {
	a, root := libraryApp(t, "alpha.rex.json")
	typeText(a, "done")
	if !strings.Contains(a.frame(), "enter create") {
		t.Fatalf("the footer says enter creates\n%s", a.frame())
	}
	press(a, "\r")
	if _, err := os.Stat(filepath.Join(root, "done.rex.json")); err != nil {
		t.Fatal("done enter on Plans creates done.rex.json")
	}
}

// docs/ux.md §11 (#70): Open a plan starts on its first folder or plan,
// and a filter selects its first match, which enter opens.
func TestOpenAPlanCursorAndFilter(t *testing.T) {
	a, root := libraryApp(t, "zeta.rex.json", "team/alpha.rex.json")
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644)
	pickRow(a, PlanRowOpen, "")
	press(a, "\r")
	b := a.plans
	if b.Rows[b.Cursor].Kind != PlanRowDir || len(b.Rows) != 2 {
		t.Fatalf("the first folder, then the plan, and no other file: %#v", b.Rows)
	}
	typeText(a, "zet")
	if b.Rows[b.Cursor].Name != "zeta.rex.json" || !strings.Contains(a.frame(), "╭ filter ─") {
		t.Fatalf("filter selects its first match: %#v\n%s", b.Rows[b.Cursor], a.frame())
	}
	press(a, "\r")
	if !strings.HasSuffix(a.plans.Current, "zeta.rex.json") || a.follow {
		t.Fatal("enter after a filter opens the match, latched")
	}
}

// docs/ux.md §11 (#70): entering or leaving a folder clears the filter; a
// filter that matches nothing says so.
func TestOpenAPlanFolderChangeClearsTheFilter(t *testing.T) {
	a, _ := libraryApp(t, "team/alpha.rex.json", "team/inner/beta.rex.json")
	browseTo(t, a, "team")
	typeText(a, "inn")
	press(a, "\r")
	if !strings.HasSuffix(a.plans.Dir, "inner") || a.draft != "" || a.plans.Filter != "" {
		t.Fatalf("dir=%q draft=%q filter=%q", a.plans.Dir, a.draft, a.plans.Filter)
	}
	press(a, "esc")
	if !strings.HasSuffix(a.plans.Dir, "team") || a.plans.Rows[a.plans.Cursor].Name != "inner" {
		t.Fatalf("esc goes up, on the folder: dir=%q", a.plans.Dir)
	}
	typeText(a, "zzz")
	if !strings.Contains(body(a), "no match for 'zzz' in rex/plans/team/") {
		t.Fatalf("no-match line\n%s", body(a))
	}
	press(a, "esc")
	if a.draft != "" || !strings.HasSuffix(a.plans.Dir, "team") {
		t.Fatal("esc clears the filter first")
	}
}

// docs/ux.md §3, §11: Open a plan's Esc ladder: a folder, up to its
// parent, on it; rex/plans itself, to Plans, on the Open a plan card. ←
// climbs the same way, and → goes into a folder as enter does.
func TestOpenAPlanEscLadder(t *testing.T) {
	for _, key := range []string{"esc", "left"} {
		a := listRepo(t)
		os.MkdirAll(filepath.Join(a.plans.Root, "cuj", "sub"), 0o755)
		press(a, "esc")
		pickRow(a, PlanRowOpen, "")
		press(a, "right")
		if !a.plans.Browsing() || !a.plans.topFolder() {
			t.Fatal("→ on Open a plan browses rex/plans")
		}
		if title := textOf(a.frameRows().Rows[0], a.columns); !strings.HasPrefix(title, "Plans › Open a plan ") || !strings.Contains(title, " rex/plans/ ") {
			t.Fatalf("row 0 %q", title)
		}
		pickRow(a, PlanRowDir, "cuj")
		press(a, "right")
		pickRow(a, PlanRowDir, "sub")
		press(a, "\r")
		if title := textOf(a.frameRows().Rows[0], a.columns); !strings.HasPrefix(title, "Plans › Open a plan › cuj/ › sub/ ") || !strings.Contains(a.frame(), "esc up") {
			t.Fatalf("row 0 %q", title)
		}
		press(a, key)
		if a.plans.Rows[a.plans.Cursor].Name != "sub" {
			t.Fatalf("%s: up, on sub/: %+v", key, a.plans.Rows[a.plans.Cursor])
		}
		press(a, key)
		if !a.plans.topFolder() || a.plans.Rows[a.plans.Cursor].Name != "cuj" || !strings.Contains(a.frame(), "esc plans") {
			t.Fatalf("%s: up to rex/plans, on cuj/", key)
		}
		press(a, key)
		if a.at != atLibrary || a.plans.Screen != ScreenHome || a.plans.Rows[a.plans.Cursor].Kind != PlanRowOpen {
			t.Fatalf("%s: Plans, on the Open a plan card: screen=%d on %+v", key, a.plans.Screen, a.plans.Rows[a.plans.Cursor])
		}
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
		if title := textOf(a.frameRows().Rows[0], a.columns); !strings.HasPrefix(title, "Plans › Open a plan › group/ ") {
			t.Fatalf("the title scrolled: %q", title)
		}
		if frame := a.frame(); !strings.Contains(frame, "❯") {
			t.Fatalf("selection scrolled out of view after %d moves\n%s", i+1, frame)
		}
	}
	if !strings.Contains(a.frame(), "↑ 19 more") {
		t.Fatalf("a scrolled list says how much is above\n%s", a.frame())
	}
}

// docs/ux.md §4, §11: New plan, Plans › New plan on row 0 with where it
// creates, has its name field, where the plan goes as it is typed, and
// that Rex never overwrites a file.
func TestNewPlan(t *testing.T) {
	a := listRepo(t)
	press(a, "esc")
	a.settle(80, 24)
	pickRow(a, PlanRowNew, "")
	press(a, "\r")
	typeText(a, "team/auth")
	if head := textOf(a.frameRows().Rows[0], 80); !strings.HasPrefix(head, "Plans › New plan ") || !strings.Contains(head, " rex/plans/ ") || textOf(a.frameRows().Rows[1], 80) != "Name it. a/b puts plan b in folder a." {
		t.Fatalf("row 0 %q", head)
	}
	want := `  ╭ name ─────────────────────────────────────────────────────────────────────╮
  │ team/auth                                                                 │
  ╰───────────────────────────────────────────────────────────────────────────╯
  → rex/plans/team/auth.rex.json

  Rex writes an empty plan there and opens it. It never overwrites a file.`
	if got := body(a); !strings.HasPrefix(got, want+"\n") {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if f := a.frameRows(); f.CursorRow != 5 || f.CursorCol != 14 {
		t.Fatalf("the cursor after the name: %d,%d", f.CursorRow, f.CursorCol)
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
	a.settle(80, 10)
	press(a, "\x15")
	typeText(a, "team/auth")
	if got := body(a); !strings.HasPrefix(got, "  name  team/auth\n  → rex/plans/team/auth.rex.json\n  Rex writes an empty plan there and opens it. It never overwrites a file.") {
		t.Fatalf("80×10:\n%s", got)
	}
	press(a, "esc")
	if a.plans.Prompting() || a.plans.Rows[a.plans.Cursor].Kind != PlanRowNew || a.draft != "" {
		t.Fatal("esc cancels, back on the Create a plan card")
	}
	if _, err := os.Stat(filepath.Join(a.plans.Root, "team")); !os.IsNotExist(err) {
		t.Fatal("nothing was written")
	}
}

// docs/ux.md §11: a name that is taken is refused on the notice row;
// nothing is written, and the name stays to be fixed. The fixed name is
// created, opened and latched.
func TestNewPlanRefusesATakenName(t *testing.T) {
	a := listRepo(t)
	press(a, "esc")
	a.settle(80, 24)
	pickRow(a, PlanRowNew, "")
	press(a, "\r")
	typeText(a, "cuj/sendoff-demo")
	before, _ := os.ReadFile(a.plans.Current)
	press(a, "\r")
	rows := a.frameRows().Rows
	if !a.plans.Prompting() || a.draft != "cuj/sendoff-demo" || textOf(rows[22], 80) != "✗ cuj/sendoff-demo already exists · pick another name" {
		t.Fatalf("refused, the text kept\n%s", a.frame())
	}
	if after, _ := os.ReadFile(a.plans.Current); string(after) != string(before) {
		t.Fatal("a refused name wrote the file")
	}
	typeText(a, "-2")
	if a.plans.Err != "" {
		t.Fatal("the error clears on the next key")
	}
	press(a, "\r")
	if made := filepath.Join(a.plans.Root, "cuj", "sendoff-demo-2.rex.json"); a.plans.Current != made || a.at != atOutline || a.follow {
		t.Fatalf("created and latched: %q follow=%v", a.plans.Current, a.follow)
	}
}

// docs/ux.md §4, §11: a name the filesystem refuses says why on the notice
// row, not the absolute path the error leads with.
func TestNewPlanSaysWhyCreateFails(t *testing.T) {
	long := strings.Repeat("x", draftCap) // over any filesystem's 255
	err := writeNew(filepath.Join(t.TempDir(), long+".rex.json"), nil)
	pe, ok := err.(*os.PathError)
	if !ok {
		t.Fatalf("setup: %v", err)
	}
	a, _ := libraryApp(t)
	pickRow(a, PlanRowNew, "")
	press(a, "\r")
	typeText(a, long)
	press(a, "\r")
	if !a.plans.Prompting() || a.plans.Err != pe.Err.Error() || a.notice().Text != "✗ "+pe.Err.Error() || !strings.Contains(a.frame(), "✗ "+pe.Err.Error()) {
		t.Errorf("%q, want %q\n%s", a.plans.Err, pe.Err, a.frame())
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
	if b.CommitCreate("../escape") != PlanStay || !strings.Contains(b.Err, "inside rex/plans") {
		t.Fatalf("err %q", b.Err)
	}
}

func TestSelectionFollowsThePlanNotTheRow(t *testing.T) {
	a, root := libraryApp(t, "oldest.rex.json", "older.rex.json", "newer.rex.json")
	b := a.plans
	pickRow(a, PlanRowPlan, "older.rex.json")
	os.WriteFile(filepath.Join(root, "older.rex.json"), []byte("{} "), 0o644)
	stampAfter(t, filepath.Join(root, "older.rex.json"), filepath.Join(root, "newer.rex.json")) // now the newest
	b.walk = planWalk{}
	b.Rebuild()
	// older is the newest now: Open latest, and the selection with it.
	if row := b.Rows[b.Cursor]; row.Kind != PlanRowLatest || !strings.HasSuffix(row.Path, "older.rex.json") {
		t.Fatalf("selection jumped to %#v", row)
	}
}

// docs/ux.md §11: Continue is the plan the task list shows. A save caught
// half-written keeps the last good copy there, as the task list does, and
// only an error the notice row shows (§4) reaches Plans.
func TestContinueFollowsTheTaskList(t *testing.T) {
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
	card := func() string { return rowWith(a.frameRows(), "Continue") }
	torn := []byte(`{"tasks": [`)
	write(torn, time.Hour)
	polls(1) // the scan rebuilds Plans while the save is torn
	if got := card(); strings.Contains(got, "can't read") || !strings.Contains(got, "Send-off demo · 1/3 done") {
		t.Fatalf("a torn save shows the last good copy: %q", got)
	}
	write(cujFixture(t), 2*time.Hour)
	polls(4)
	write(torn, 3*time.Hour)
	polls(3)
	if got := card(); !strings.Contains(a.frame(), "plan JSON error") || !strings.Contains(got, " · can't read") {
		t.Fatalf("an error the task list shows, Plans shows: %q\n%s", got, a.frame())
	}
}

// docs/ux.md §4, §11: a plan Rex can't open says why on the notice row
// and can't read on its card or row, the open plan's and another's alike.
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
	if got, want := a.notice().Text, "✗ plan error: permission denied · showing last good"; got != want {
		t.Fatalf("the notice row: %q, want %q", got, want)
	}
	a.plans.walk = planWalk{}
	press(a, "esc")
	f := a.frameRows()
	if row := rowWith(f, "other"); !strings.HasSuffix(row, " can't read") {
		t.Errorf("other: %q\n%s", row, a.frame())
	}
	if strings.Contains(a.frame(), "open /") {
		t.Fatalf("the cause, not the path\n%s", a.frame())
	}
}

// docs/ux.md §11, §12: Plans' window never scrolls only to hide blank
// rows, ↑ N more never stands on Recent, and ↓ N more comes right under
// the last row, never under a blank row or Recent left alone.
func TestListWindowEdges(t *testing.T) {
	for _, cols := range []int{80, 120, 24} {
		for rows := 7; rows <= 16; rows++ {
			a := listRepo(t)
			press(a, "esc")
			a.settle(cols, rows)
			for step := 0; step < 2*len(a.plans.Rows); step++ {
				key := "down"
				if step >= len(a.plans.Rows) {
					key = "up"
				}
				where := fmt.Sprintf("%d×%d %s %d", cols, rows, key, step)
				l := a.layout()
				frame := a.frameRows().Rows
				all := planList(a.plans, l.textW, listFit{body: l.bodyH, tall: l.boxed, short: rows < blankFrom})
				list, start := all[headOf(all):], a.plans.View
				var text []string
				for _, r := range frame[2+l.blank : 2+l.blank+l.bodyH] {
					text = append(text, strings.TrimRight(textOf(r, cols), " "))
				}
				if start > 0 && !strings.HasPrefix(text[0], "  ↑ ") && !list[start].Selected {
					t.Errorf("%s: rows hidden above, and no ↑ more\n%s", where, body(a))
				}
				for i, line := range text {
					if strings.HasPrefix(line, "  ↓ ") && i > 0 && (text[i-1] == "" || text[i-1] == "  Recent") {
						t.Errorf("%s: ↓ more under %q\n%s", where, text[i-1], body(a))
					}
					if strings.HasPrefix(line, "  ↑ ") && strings.Contains(line, " 0 more") {
						t.Errorf("%s: %q", where, line)
					}
				}
				if !strings.Contains(strings.Join(text, "\n"), "❯") {
					t.Errorf("%s: the selection is off screen\n%s", where, body(a))
				}
				press(a, key)
			}
		}
	}
}

// docs/ux.md §11, F1: starting Rex, and moving around Plans, Open a plan
// and Find, creates no file and no rex/plans/, and records nothing in
// ~/.rex/state.json; only opening or creating a plan does.
func TestStartCreatesNothingOnPlans(t *testing.T) {
	repo := attachRepo(t)
	a := startIn(repo)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	for i := 0; i < 5; i++ {
		later(a, c)
	}
	press(a, "down", "up", "right", "esc")
	typeText(a, "anything")
	press(a, "esc", "?", "esc", "\t", "\t")
	if _, err := os.Stat(filepath.Join(repo, "rex")); !os.IsNotExist(err) {
		t.Fatalf("rex/ was created: %v", err)
	}
	if _, err := os.Stat(plan.StatePath()); !os.IsNotExist(err) {
		t.Fatalf("state.json was written: %v", err)
	}
	seedPlans(t, repo, "one.rex.json")
	b := startIn(repo)
	press(b, "home", "down", "\r", "esc") // Open a plan, and back
	if _, err := os.Stat(plan.StatePath()); !os.IsNotExist(err) {
		t.Fatalf("browsing wrote state.json: %v", err)
	}
	if entries, _ := os.ReadDir(PlansDir(repo)); len(entries) != 1 {
		t.Fatalf("rex/plans holds %d entries", len(entries))
	}
}

// docs/ux.md §11: the filter in Open a plan holds an attach as Find does,
// and the notice says it waits for the field.
func TestAFilterHoldsTheAttach(t *testing.T) {
	repo := attachRepo(t)
	writePlan(t, repo, "team/a.rex.json", demoPlan)
	a := startIn(repo)
	browseTo(t, a, "team")
	typeText(a, "a")
	two := writePlan(t, repo, "two.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, two)})
	if a.plans.Current != "" || a.pending != two || !strings.HasSuffix(a.notice().Text, " · opens when the field is clear") {
		t.Fatalf("the filter holds it: current=%q pending=%q notice %q", a.plans.Current, a.pending, a.notice().Text)
	}
	press(a, "esc")
	if a.plans.Current != two || a.at != atOutline || !a.follow {
		t.Fatalf("cleared, it opens, following: current=%q at=%d", a.plans.Current, a.at)
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

func TestHelpPagesOverlapUnderTheMarkers(t *testing.T) {
	a := testApp(t)
	a.settle(80, 12)
	press(a, "?")
	page := PaneBodyHeight(a.lines)
	press(a, "pagedown")
	if got := a.overlayScroll[a.pane]; got != page-2 {
		t.Fatalf("pagedown moved %d, want %d", got, page-2)
	}
}

// Find and Open a plan rows carry the plan's progress, and never a
// dangling " · " (docs/ux.md §11).
func TestFindAndBrowseRowsCarryProgress(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "rex", "plans", "team")
	os.MkdirAll(root, 0o755)
	work := `{"tasks": [{"id": "x", "status": "done"}, {"id": "y"}]}`
	os.WriteFile(filepath.Join(root, "auth.rex.json"), []byte(work), 0o644)
	os.WriteFile(filepath.Join(root, "auth-v2.rex.json"), []byte(work), 0o644)
	os.WriteFile(filepath.Join(root, "auth-bad.rex.json"), []byte("not json"), 0o644)
	b := NewPlanBrowser(repo)
	check := func(where, prefix string) {
		t.Helper()
		v := viewText(BuildPlanView(b, 38))
		for name, want := range map[string]string{"auth": "1/2 done", "auth-v2": "1/2 done", "auth-bad": "can't read"} {
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
	b.browse(root)
	check("open a plan", "")
	b.Screen, b.Find, b.fresh = ScreenHome, "auth", true
	b.Rebuild()
	check("find", "team/")
}

// At 7 rows Plans has four body rows under rows 0 and 1 (docs/ux.md §4):
// the cards, no blank row, and the selected row is never the part cut
// (§12).
func TestSevenRowsListOnlyRows(t *testing.T) {
	for _, cols := range []int{24, 80} {
		a, _, _ := cujApp(t)
		press(a, "esc")
		a.settle(cols, 7)
		if body := strings.Join(a.frameRows().Rows[2:6], "\n"); !strings.Contains(body, "❯ Continue ") || !strings.Contains(body, "  Open a plan ") {
			t.Fatalf("%d×7: the cards\n%s", cols, a.frame())
		}
		empty := testApp(t)
		empty.settle(cols, 7)
		if body := strings.Join(empty.frameRows().Rows[2:6], "\n"); !strings.Contains(body, "❯ Create a plan") {
			t.Fatalf("%d×7, no plans: the card on top\n%s", cols, empty.frame())
		}
	}
}

// Under 12 rows Plans' error takes row 1, as any notice does (docs/ux.md
// §4), and the field and the text stay.
func TestShortPlansKeepTheError(t *testing.T) {
	a, _ := libraryApp(t, "x.rex.json")
	a.settle(80, 9)
	pickRow(a, PlanRowNew, "")
	press(a, "\r")
	typeText(a, "x")
	press(a, "\r")
	if row := textOf(a.frameRows().Rows[1], 80); row != "✗ x already exists · pick another name" || !strings.Contains(body(a), "  name  x") {
		t.Fatalf("the error on row 1 (%q), and the field stays\n%s", row, a.frame())
	}
}

// Outside git Plans says so on row 1 on a small screen too, and start
// creates nothing (docs/ux.md §4, §11).
func TestOutsideGitNoticeShowsOnASmallScreen(t *testing.T) {
	repo := t.TempDir()
	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 60, 20)
	a.settle(60, 20)
	if row := textOf(a.frameRows().Rows[1], 60); a.at != atLibrary || !strings.HasPrefix(row, "Not in a git repo · plans go in …") || !strings.HasSuffix(row, "/rex/plans/") {
		t.Fatalf("row 1 %q\n%s", row, a.frame())
	}
	seeded := t.TempDir()
	seedPlans(t, seeded, "one.rex.json")
	b := newApp("yellow", false, "idle", NewPlanBrowser(seeded), 60, 20)
	if row := textOf(b.frameRows().Rows[1], 60); b.at != atLibrary || !strings.Contains(row, "/rex/plans/") || b.notice().Text != "" {
		t.Fatalf("with plans, row 1 says it, and the notice row is free: %q %q", row, b.notice().Text)
	}
}

// docs/ux.md §11: moving the cursor on Plans reads no files: every
// summary was read when the rows were built, and a rebuild reads only a
// plan whose file changed, never the open plan, which the task list shows.
func TestMovingReadsNoFiles(t *testing.T) {
	a := listRepo(t)
	press(a, "esc")
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
	other := filepath.Join(a.plans.Root, "ux", "redesign.rex.json")
	data, _ := os.ReadFile(other)
	os.WriteFile(other, data, 0o644) // same bytes, a new mtime
	os.Chtimes(other, a.now().Add(time.Hour), a.now().Add(time.Hour))
	os.WriteFile(a.plans.Current, cujFixture(t), 0o644)
	os.Chtimes(a.plans.Current, a.now().Add(time.Hour), a.now().Add(time.Hour))
	a.plans.walk = planWalk{}
	a.plans.Rebuild()
	if loads != 1 {
		t.Fatalf("a rebuild reads the one plan that changed, the open one being the task list's: %d reads", loads)
	}
}

// docs/ux.md §11: the Open a plan card shows only with a plan to browse.
// Continue's plan in a hidden folder is Continue, never something Open a
// plan reaches, and the card counts only the plans it reaches.
func TestOpenAPlanCardNeedsAPlanToBrowse(t *testing.T) {
	repo := attachRepo(t)
	hidden := writePlan(t, repo, ".archive/old.rex.json", demoPlan)
	now := time.Now()
	if err := plan.UpdateState(plan.StatePath(), now, func(s *plan.State) {
		s.Repos[repo] = plan.RepoState{Plan: hidden, Opened: float64(now.Unix())}
	}); err != nil {
		t.Fatal(err)
	}
	a := startIn(repo)
	var kinds []PlanRowKind
	for _, row := range a.plans.Rows {
		kinds = append(kinds, row.Kind)
	}
	if want := []PlanRowKind{PlanRowContinue, PlanRowNew}; fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("only Continue and Create a plan: %v\n%s", kinds, a.frame())
	}
	writePlan(t, repo, "team/visible.rex.json", demoPlan)
	a.plans.walk = planWalk{}
	a.plans.Rebuild()
	if row := rowWith(a.frameRows(), "Open a plan"); a.plans.rowOf(PlanRowOpen) < 0 || !strings.HasPrefix(row, "  Open a plan     browse rex/plans/ · 1 folder · 1 plan") {
		t.Fatalf("a plan to browse brings the card, counting it alone: %q\n%s", row, a.frame())
	}
}
