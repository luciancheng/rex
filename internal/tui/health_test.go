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
	text := viewText(h.lines(78, 4, skill.Missing, now, true))
	for _, want := range []string{"FEED ──", "last event 3h ago (cursor)", "cursor   wired", "claude   not wired · rex ingest --install claude", "skill    missing · rex ingest --install-skill", "4 events from other folders hidden"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q\n%s", want, text)
		}
	}
	a := testApp(t)
	a.health = h
	press(a, "\t")
	if f := a.frame(); !strings.Contains(f, "AGENT") || !strings.Contains(f, "waiting for events") || !strings.Contains(f, "FEED ──") || strings.Contains(f, "Ready. Watching") {
		t.Fatalf("the empty AGENT tab shows feed health, and says it waits in its page title\n%s", f)
	}
	// A lead sentence heads it (docs/ux.md §10), with a blank row above FEED
	// from 16 rows.
	lead := viewText(a.emptyAgent(38))
	if !strings.HasPrefix(lead, "  Rex shows your agent's work here as\n  its hooks report it.\n\nFEED ─") {
		t.Fatalf("the lead\n%s", lead)
	}
	if a.settle(120, 12); !strings.HasPrefix(viewText(a.emptyAgent(78)), "  Rex shows your agent's work here as its hooks report it.\nFEED ─") {
		t.Fatalf("no blank row under 16 rows\n%s", viewText(a.emptyAgent(78)))
	}
	if strings.Contains(viewText(a.emptyAgent(38)), "your agent's work here") {
		t.Fatalf("no lead where it and the block don't both fit\n%s", viewText(a.emptyAgent(38)))
	}
	if a.settle(120, 11); !strings.Contains(a.frame(), "FEED ─") || !strings.Contains(a.frame(), "waiting for agent events") || strings.Contains(a.frame(), "your agent's work here") {
		t.Fatalf("with no page title, the FEED header says it, and there is no lead\n%s", a.frame())
	}
	if strings.Contains(viewText(a.helpBody(78)), "your agent's work here") {
		t.Fatal("Help's feed block has no lead")
	}
	a.settle(120, 32)
	typeText(a, "?")
	press(a, "\r")
	if a.overlay[PaneChat] != overlayHelp || !strings.Contains(viewText(a.helpBody(PaneTextWidth(a.columns))), "rex ingest --install claude") {
		t.Fatal("help shows feed health too")
	}
}

// The empty AGENT tab is a page (docs/ux.md §10): it keeps its top and
// crops from the bottom, and its lead comes whole, with the whole block
// under it, or not at all; the blank row under the lead goes first. With
// every harness found but not wired and the skill missing, the first body
// row is the lead's first line or FEED, never the end of a sentence.
func TestEmptyAgentNeverStartsMidSentence(t *testing.T) {
	for _, size := range [][2]int{{80, 12}, {80, 16}, {80, 20}, {50, 12}, {80, 24}, {30, 8}, {24, 7}} {
		a := testApp(t)
		a.health = notWired()
		a.hidden = 4
		press(a, "\t")
		a.settle(size[0], size[1])
		body := agentBody(a)
		full := len(body)
		for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
			body = body[:len(body)-1] // filler under a short page
		}
		lead := wrap(feedLead, max(8, PaneTextWidth(a.columns)-2))
		name := fmt.Sprintf("%d×%d", size[0], size[1])
		switch {
		case strings.HasPrefix(body[0], "FEED ─"):
			if strings.Contains(strings.Join(body, "\n"), "your agent's work") {
				t.Errorf("%s: a lead under FEED\n%s", name, strings.Join(body, "\n"))
			}
		case body[0] == "  "+lead[0]:
			for i, w := range lead {
				if body[i] != "  "+w {
					t.Errorf("%s: the lead is cut at row %d\n%s", name, i, strings.Join(body, "\n"))
				}
			}
			at := len(lead)
			if body[at] == "" {
				at++
			}
			if !strings.HasPrefix(body[at], "FEED ─") || !strings.Contains(body[len(body)-1], "4 events from other folders hidden") {
				t.Errorf("%s: the lead without the whole block under it\n%s", name, strings.Join(body, "\n"))
			}
			if blank := body[len(lead)] == ""; blank && size[1] < 16 || !blank && size[1] >= 16 && len(body) < full {
				t.Errorf("%s: the blank row under the lead is from 16 rows, where it fits\n%s", name, strings.Join(body, "\n"))
			}
		default:
			t.Errorf("%s: the page starts mid-block: %q\n%s", name, body[0], strings.Join(body, "\n"))
		}
	}
	// At 38 cells: the whole page at 80×24; at 80×20 the blank row goes
	// first and the lead stays.
	a := testApp(t)
	a.health = notWired()
	press(a, "\t")
	for rows, want := range map[int]string{
		24: "  Rex shows your agent's work here as\n  its hooks report it.\n\nFEED ─",
		20: "  Rex shows your agent's work here as\n  its hooks report it.\nFEED ─",
	} {
		a.settle(80, rows)
		if body := strings.Join(agentBody(a), "\n"); !strings.HasPrefix(body, want) {
			t.Fatalf("80×%d\n%s", rows, body)
		}
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
		read: now.Add(time.Hour), // never refreshed from the temp HOME mid-test
	}
	for _, name := range []string{"claude", "codex", "cursor", "opencode"} {
		h.statuses = append(h.statuses, watch.HarnessStatus{Name: name, Present: true})
	}
	return h
}

func TestFeedHealthWrapsAndSaysWhatIsWiredHere(t *testing.T) {
	now := time.Now()
	h := feedHealth{
		path: "/home/me/.rex/session.jsonl",
		statuses: []watch.HarnessStatus{
			{Name: "claude", Present: true, Here: true},
			{Name: "codex", Present: true},
		},
		read: now,
	}
	text := viewText(h.lines(38, 0, skill.Current, now, false))
	if !strings.Contains(text, "wired (this repo)") {
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
		skill.Current:      "skill    rex-plan current",
		skill.Missing:      "skill    missing · rex ingest --install-skill",
		skill.Stale:        "skill    out of date · rex ingest --install-skill",
		skill.StaleProject: "skill    out of date · rex ingest --install-skill --project",
	} {
		if text := strings.Join(strings.Fields(viewText(h.lines(78, 0, c, time.Now(), false))), " "); !strings.Contains(text, strings.Join(strings.Fields(want), " ")) {
			t.Errorf("%v: want %q\n%s", c, want, text)
		}
	}
}
