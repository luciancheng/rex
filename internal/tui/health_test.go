package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/watch"
)

func TestFeedHealthOnTheEmptyAgentTab(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	h := feedHealth{
		path: "/home/me/.rex/session.jsonl",
		last: &watch.Event{Source: "cursor", TS: float64(now.Add(-3*time.Hour).UnixNano()) / 1e9},
		statuses: []watch.HarnessStatus{
			{Name: "claude", Present: true, Wired: false},
			{Name: "cursor", Present: true, Wired: true},
		},
		read: time.Now(), // fresh: the test's statuses stand
	}
	// Help's Feed: one section, the events file, a row per harness, the
	// skill's and what Rex hid.
	text := viewText(h.lines(78, 4, skill.Missing, now, false, true))
	want := strings.Join([]string{
		"  Feed",
		"    events      /home/me/.rex/session.jsonl · last event 3h ago (cursor)",
		"    claude      not wired · rex ingest --install claude",
		"    cursor      wired · rerun rex ingest --install cursor",
		"    rex-plan    missing · rex ingest --install-skill",
		"    4 events from other folders hidden",
	}, "\n") + "\n"
	if text != want {
		t.Fatalf("got\n%s\nwant\n%s", text, want)
	}
	// The empty AGENT tab: the same rows in three sections (docs/ux.md §10).
	text = viewText(h.lines(78, 4, skill.Missing, now, true, true))
	want = strings.Join([]string{
		"  Hooks",
		"    claude      not wired · rex ingest --install claude",
		"    cursor      wired · rerun rex ingest --install cursor",
		"",
		"  Skill",
		"    rex-plan    missing · rex ingest --install-skill",
		"",
		"  Feed",
		"    events      /home/me/.rex/session.jsonl · last event 3h ago (cursor)",
		"    4 events from other folders hidden",
	}, "\n") + "\n"
	if text != want {
		t.Fatalf("got\n%s\nwant\n%s", text, want)
	}
	for _, l := range h.lines(78, 4, skill.Missing, now, true, false) {
		if l.Kind == "gap" {
			t.Fatal("no blank rows under 16 rows")
		}
		if l.Kind == "h2" && (l.Spans[1].Role != "text" || !l.Spans[1].Bold || strings.Contains(l.Text, "─")) {
			t.Fatalf("a section label is bold text with no rule: %+v", l)
		}
	}
	a := testApp(t)
	a.health = h
	press(a, "\t")
	rows := a.frameRows().Rows
	if f := a.frame(); !strings.HasPrefix(textOf(rows[0], a.columns), "Agent ") || !strings.Contains(textOf(rows[0], a.columns), "no events yet") || !strings.Contains(f, "  Hooks") || strings.Contains(f, "Ready. Watching") {
		t.Fatalf("the empty AGENT tab shows feed health, and says it waits in its title (docs/ux.md §4)\n%s", f)
	}
	// Its lead sentence is row 1 (docs/ux.md §4, §10); the body is the
	// feed's health, from Hooks.
	if got := textOf(rows[1], a.columns); got != feedLead {
		t.Fatalf("row 1 is the lead: %q", got)
	}
	for _, w := range []int{38, 78} {
		if body := viewText(a.emptyAgent(w)); !strings.HasPrefix(body, "  Hooks\n") || strings.Contains(body, feedShort) {
			t.Fatalf("%d cells: the body starts at Hooks, with no lead\n%s", w, body)
		}
	}
	if a.settle(120, 11); !strings.Contains(a.frame(), "  Hooks") || textOf(a.frameRows().Rows[1], a.columns) != feedLead || !strings.Contains(textOf(a.frameRows().Rows[0], a.columns), "no events yet") {
		t.Fatalf("under 12 rows the title still says it waits, and row 1 is the lead\n%s", a.frame())
	}
	if strings.Contains(viewText(a.helpBody(78)), feedShort) {
		t.Fatal("Help's feed block has no lead")
	}
	a.settle(120, 32)
	typeText(a, "?")
	press(a, "\r")
	if a.overlay[PaneChat] != overlayHelp || !strings.Contains(viewText(a.helpBody(PaneTextWidth(a.columns, a.lines))), "rex ingest --install claude") {
		t.Fatal("help shows feed health too")
	}
}

// The empty AGENT tab is a page (docs/ux.md §10): it starts at its top,
// Hooks, and its lead is row 1 (§4), so the page never starts mid-sentence.
// With every harness found but not wired and the skill missing, the first
// body row is Hooks at every size, and the lead is never in the body.
func TestEmptyAgentNeverStartsMidSentence(t *testing.T) {
	for _, size := range [][2]int{{80, 12}, {80, 16}, {80, 20}, {50, 12}, {80, 24}, {30, 8}, {24, 7}} {
		a := testApp(t)
		a.health = notWired()
		a.hidden = 4
		press(a, "\t")
		a.settle(size[0], size[1])
		body := agentBody(a)
		name := fmt.Sprintf("%d×%d", size[0], size[1])
		if body[0] != "  Hooks" || strings.Contains(strings.Join(body, "\n"), feedShort) {
			t.Errorf("%s: the page starts mid-block, or the lead is in it: %q\n%s", name, body[0], strings.Join(body, "\n"))
		}
		if row, want := textOf(a.frameRows().Rows[1], a.columns), feedLead; row != want && (cellLen(feedLead) < PaneTextWidth(a.columns, a.lines) || row != feedShort && !strings.HasSuffix(row, "…")) {
			t.Errorf("%s: row 1 is the lead, or its short form where it doesn't fit: %q", name, row)
		}
	}
}

// The empty AGENT tab scrolls like Wait (docs/ux.md §10): from its top,
// ↑↓, pages and Home / End move it, ↑ more / ↓ more say it runs past, and
// only then does its footer offer ↑↓. A first run's whole block is
// reachable, the skill row and the hidden count with it.
func TestEmptyAgentScrollsLikeWait(t *testing.T) {
	a := testApp(t)
	a.health, a.hidden = notWired(), 4
	press(a, "\t")
	a.settle(38, 16)
	l := a.layout()
	all := a.emptyAgent(l.textW)
	footer := func() string {
		rows := a.frameRows().Rows
		return textOf(rows[len(rows)-1], a.columns)
	}
	if len(all) <= l.bodyH {
		t.Fatalf("the block fits %d rows: %d", l.bodyH, len(all))
	}
	body := agentBody(a)
	if body[len(body)-1] != "  ↓ more" || body[0] != "  Hooks" || footer() != "↑↓ scroll · tab plan · ? help" {
		t.Fatalf("the top, with ↓ more\n%s\n%s", strings.Join(body, "\n"), footer())
	}
	seen := map[string]bool{}
	for range all {
		for _, row := range agentBody(a) {
			seen[strings.TrimSpace(row)] = true
		}
		press(a, "down")
	}
	for _, line := range all {
		if text := strings.TrimSpace(line.Text); text != "" && !seen[text] {
			t.Errorf("never on screen: %q", text)
		}
	}
	if body := agentBody(a); body[0] != "  ↑ more" || !strings.Contains(body[len(body)-1], "4 events from other folders hidden") {
		t.Fatalf("the bottom, with ↑ more\n%s", strings.Join(body, "\n"))
	}
	press(a, "home")
	if a.frameRows(); a.emptyScroll != 0 || a.scroll != 0 {
		t.Fatalf("Home is the top: %d", a.emptyScroll)
	}
	press(a, "end")
	if a.frameRows(); a.emptyScroll != len(all)-l.bodyH {
		t.Fatalf("End is the bottom: %d", a.emptyScroll)
	}
	press(a, "pageup")
	if a.frameRows(); a.emptyScroll != max(0, len(all)-l.bodyH-(l.bodyH-2)) {
		t.Fatalf("a page is the body less its two marker rows: %d", a.emptyScroll)
	}
	// A page that fits has no markers, and nothing to scroll.
	a.settle(38, 32)
	before := a.frame()
	press(a, "down", "pagedown", "end")
	if f := a.frame(); f != before || strings.Contains(f, "more") || footer() != "tab plan · ? help" {
		t.Fatalf("nothing to scroll at 32 rows\n%s", f)
	}
	// The first event ends the page: AGENT is live, at the bottom.
	a.settle(38, 16)
	press(a, "down", "down")
	a.ingest([]watch.Event{watch.NewEvent("claude", "text", "done", nil)})
	if a.settle(38, 16); a.scroll != 0 || !strings.Contains(a.frame(), "done") {
		t.Fatalf("scroll=%d\n%s", a.scroll, a.frame())
	}
}

// notWired is the feed's health on a first run: every harness found, none
// wired, an old event, and (in the test's HOME) no skill.
func notWired() feedHealth {
	now := time.Now()
	h := feedHealth{
		path: "/home/me/.rex/session.jsonl",
		// Half an hour off the hour, so the float stamp never rounds to 2h.
		last: &watch.Event{Source: "cursor", TS: float64(now.Add(-3*time.Hour-30*time.Minute).UnixNano()) / 1e9},
		read: now,
	}
	for _, name := range []string{"claude", "codex", "cursor", "opencode"} {
		h.statuses = append(h.statuses, watch.HarnessStatus{Name: name, Present: true})
	}
	// Never read from the temp HOME mid-test.
	h.statusesFor = func(string) []watch.HarnessStatus { return h.statuses }
	return h
}

func TestFeedHealthWrapsAndSaysWhatIsWiredHere(t *testing.T) {
	now := time.Now()
	h := feedHealth{
		path: "/home/me/.rex/session.jsonl",
		statuses: []watch.HarnessStatus{
			{Name: "claude", Present: true, Here: true, Prompts: true},
			{Name: "codex", Present: true},
		},
		read: now,
	}
	text := viewText(h.lines(38, 0, skill.Current, now, false, false))
	if !strings.Contains(strings.Join(strings.Fields(text), " "), "claude wired for this repo · sends the pin with each prompt") {
		t.Fatalf("a --project install is wired here\n%s", text)
	}
	if strings.Contains(text, "waiting") {
		t.Fatalf("Help doesn't claim to be waiting\n%s", text)
	}
	if !strings.Contains(strings.Join(strings.Fields(text), " "), "rex ingest --install codex") {
		t.Fatalf("the install command wraps instead of being cut\n%s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if cellLen(line) > 38 {
			t.Fatalf("row wider than the pane: %q", line)
		}
	}
}

func TestFeedHealthFollowsTheFeed(t *testing.T) {
	a := testApp(t)
	a.health = feedHealth{path: "/x/session.jsonl", read: time.Now()}
	press(a, "\t")
	if !a.ingest([]watch.Event{{Source: "claude", Kind: "user", Text: "elsewhere", TS: float64(time.Now().UnixNano()) / 1e9, Meta: map[string]any{"cwd": t.TempDir()}}}) {
		t.Fatal("a hidden event repaints the empty tape's count")
	}
	if frame := a.frame(); !strings.Contains(frame, "just now (claude)") || !strings.Contains(frame, "1 event from other folders hidden") {
		t.Fatalf("the empty tape shows the newest event and the count\n%s", frame)
	}
}

func TestAskClearsOnceTheToolRuns(t *testing.T) {
	a := testApp(t)
	repo := map[string]any{"cwd": a.plans.Repo}
	a.ingest([]watch.Event{
		{Source: "claude", Kind: "user", Text: "go", Meta: repo},
		{Source: "claude", Kind: "question", Text: "Claude needs your permission to use Bash", Meta: repo},
	})
	if got := statusForTurn(a.turn, a.lastKind); got != "ask" {
		t.Fatalf("status %q", got)
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "result", Text: "ran go test", Meta: map[string]any{"cwd": a.plans.Repo, "tool": "Bash", "ok": true}}})
	if got := statusForTurn(a.turn, a.lastKind); got != "running" {
		t.Fatalf("after the tool ran the header says %q", got)
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "idle", Text: "turn completed", Meta: repo}})
	if got := statusForTurn(a.turn, a.lastKind); got == "ask" {
		t.Fatal("idle never says ask")
	}
}

// The skill row says how the rex-plan skill stands, so an upgrader who
// lands on a plan (never the Wait screen) can see it (docs/ux.md §10, §13).
func TestFeedHealthSaysHowTheSkillStands(t *testing.T) {
	h := feedHealth{path: "/x/session.jsonl", read: time.Now()}
	for c, want := range map[skill.Copy]string{
		skill.Current:      "rex-plan current",
		skill.Missing:      "rex-plan missing · rex ingest --install-skill",
		skill.Stale:        "rex-plan out of date · rex ingest --install-skill",
		skill.StaleProject: "rex-plan out of date · rex ingest --install-skill --project",
	} {
		if text := strings.Join(strings.Fields(viewText(h.lines(78, 0, c, time.Now(), false, false))), " "); !strings.Contains(text, strings.Join(strings.Fields(want), " ")) {
			t.Errorf("%v: want %q\n%s", c, want, text)
		}
	}
}

// A word wider than the value column, such as --install-skill at 24-25
// text cells (24-25 columns), gets a row of its own, whole: a command
// split mid-word can't be copied (docs/ux.md §10).
func TestFeedHealthNeverSplitsAWord(t *testing.T) {
	h := feedHealth{path: "/x/session.jsonl", statuses: []watch.HarnessStatus{{Name: "codex", Present: true}}, read: time.Now()}
	for _, width := range []int{24, 25, 38} {
		for _, c := range []skill.Copy{skill.Missing, skill.StaleProject} {
			lines := strings.Split(viewText(h.lines(width, 0, c, time.Now(), false, false)), "\n")
			words := map[string]bool{}
			for _, line := range lines {
				if cellLen(line) > width {
					t.Fatalf("%d cells: row wider than the pane: %q", width, line)
				}
				for _, w := range strings.Fields(line) {
					words[w] = true
				}
			}
			for _, w := range []string{"--install-skill", "--install", "codex", "/x/session.jsonl"} {
				if !words[w] {
					t.Errorf("%d cells, %v: %q is split\n%s", width, c, w, strings.Join(lines, "\n"))
				}
			}
			if c == skill.StaleProject && !words["--project"] {
				t.Errorf("%d cells: --project is split\n%s", width, strings.Join(lines, "\n"))
			}
		}
	}
	// Under 36 cells a row's name sits above its words, at column 6.
	text := viewText(h.lines(24, 0, skill.Missing, time.Now(), false, false))
	if want := "    rex-plan\n      missing · rex\n      ingest\n      --install-skill"; !strings.Contains(text, want) {
		t.Fatalf("want\n%s\n%s", want, text)
	}
	for _, cols := range []int{24, 25} {
		a := testApp(t)
		a.settle(cols, 44)
		press(a, "\t")
		if f := a.frame(); !strings.Contains(f, "--install-skill") {
			t.Fatalf("%d columns: the empty tape keeps the command whole\n%s", cols, f)
		}
	}
}
