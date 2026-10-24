package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/watch"
)

func TestFeedHealthOnTheEmptyTape(t *testing.T) {
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
	for _, want := range []string{"── feed · waiting for agent events", "last event 3h ago (cursor)", "cursor   wired", "claude   not wired · rex ingest --install claude", "skill    missing · rex ingest --install-skill", "4 events from other folders hidden"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q\n%s", want, text)
		}
	}
	a := testApp(t)
	a.health = h
	press(a, "\t")
	if !strings.Contains(a.frame(), "waiting for agent events") || strings.Contains(a.frame(), "Ready. Watching") {
		t.Fatal("the empty tape shows feed health")
	}
	typeText(a, "?")
	press(a, "\r")
	if a.overlay[PaneChat] != overlayHelp || !strings.Contains(viewText(a.helpBody(PaneTextWidth(a.columns))), "rex ingest --install claude") {
		t.Fatal("help shows feed health too")
	}
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

// A word wider than the value column, such as --install-skill at 24-25
// text cells (66-67 columns), gets a row of its own, whole: a command
// split mid-word can't be copied (docs/ux.md §10).
func TestFeedHealthNeverSplitsAWord(t *testing.T) {
	h := feedHealth{path: "/x/session.jsonl", statuses: []watch.HarnessStatus{{Name: "codex", Present: true}}, read: time.Now()}
	for _, width := range []int{24, 25, 38} {
		for _, c := range []skill.Copy{skill.Missing, skill.StaleProject} {
			lines := strings.Split(viewText(h.lines(width, 0, c, time.Now(), false)), "\n")
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
	text := viewText(h.lines(24, 0, skill.Missing, time.Now(), false))
	if want := "  skill    missing · rex\n           ingest\n    --install-skill"; !strings.Contains(text, want) {
		t.Fatalf("want\n%s\n%s", want, text)
	}
	for _, cols := range []int{66, 67} {
		a := testApp(t)
		a.settle(cols, 44)
		press(a, "\t")
		if f := a.frame(); !strings.Contains(f, "--install-skill") {
			t.Fatalf("%d columns: the empty tape keeps the command whole\n%s", cols, f)
		}
	}
}
