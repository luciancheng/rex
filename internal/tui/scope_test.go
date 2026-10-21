package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/watch"
)

func TestOtherReposAreHiddenAndCounted(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	other := t.TempDir()
	a.ingest([]watch.Event{
		{Source: "cursor", Kind: "user", Text: "elsewhere", Meta: map[string]any{"cwd": other}},
		{Source: "cursor", Kind: "user", Text: "here", Meta: map[string]any{"cwd": a.plans.Repo}},
		{Source: "cursor", Kind: "thought", Text: "unscoped", Meta: map[string]any{}},
	})
	view := viewText(a.logView(78))
	if strings.Contains(view, "elsewhere") || !strings.Contains(view, "here") || !strings.Contains(view, "unscoped") {
		t.Fatalf("%s", view)
	}
	if a.hidden != 1 {
		t.Fatalf("hidden=%d", a.hidden)
	}
	typeText(a, "?")
	press(a, "\r")
	if a.overlay[a.pane] != overlayHelp || !strings.Contains(viewText(a.helpBody(PaneTextWidth(a.columns))), "1 event from other folders hidden") {
		t.Fatal("help shows the hidden count")
	}
}

func TestTapeShowsRepoRelativePaths(t *testing.T) {
	a := testApp(t)
	abs := filepath.Join(a.plans.Repo, "internal", "tui", "plans.go")
	a.ingest([]watch.Event{{Source: "cursor", Kind: "command", Text: "Write " + abs, Meta: map[string]any{"tool": "Write", "path": abs, "cwd": a.plans.Repo}}})
	view := viewText(a.logView(78))
	if !strings.Contains(view, "$ Write internal/tui/plans.go") || strings.Contains(view, a.plans.Repo) {
		t.Fatalf("%s", view)
	}
}

func TestWarmStart(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	at := func(ago time.Duration) float64 { return float64(now.Add(-ago).UnixNano()) / 1e9 }
	recent := func(repo string, ago time.Duration) []watch.Event {
		return []watch.Event{
			{Source: "cursor", Kind: "user", Text: "fix the cards", TS: at(ago + time.Second), Meta: map[string]any{"cwd": repo}},
			{Source: "cursor", Kind: "thought", Text: "Looking at plans.go", TS: at(ago), Meta: map[string]any{"cwd": repo}},
		}
	}
	a := testApp(t)
	a.warmStart(recent(a.plans.Repo, 30*time.Second), now)
	if a.turn != TurnThinking || a.sequence.Name != "typing" {
		t.Fatalf("a fresh turn is restored: turn=%d anim=%s", a.turn, a.sequence.Name)
	}
	view := viewText(a.logView(78))
	if !strings.Contains(view, "── before rex started") || !strings.Contains(view, "Looking at plans.go") {
		t.Fatalf("%s", view)
	}
	stale := testApp(t)
	stale.warmStart(recent(stale.plans.Repo, 10*time.Minute), now)
	if stale.turn != TurnIdle {
		t.Fatal("a stale turn is not restored, or the dino would type forever")
	}
	if !strings.Contains(viewText(stale.logView(78)), "fix the cards") {
		t.Fatal("old events are still shown for context")
	}
	foreign := testApp(t)
	foreign.warmStart(recent(t.TempDir(), time.Second), now)
	if len(foreign.log) != 0 || foreign.turn != TurnIdle {
		t.Fatal("another repo's turn is not this repo's")
	}
}

func TestForeignAgentNeverDrivesTheTurn(t *testing.T) {
	a := testApp(t)
	other := t.TempDir()
	a.ingest([]watch.Event{
		{Source: "claude", Kind: "command", Text: "Read", Meta: map[string]any{"cwd": other, "tool": "Read", "path": filepath.Join(a.plans.Repo, "a.go")}},
		{Source: "claude", Kind: "text", Text: "done", Meta: map[string]any{"cwd": other}},
	})
	if a.turn != TurnIdle {
		t.Fatalf("another repo's agent moved this turn: %d", a.turn)
	}
}

func TestWarmStartUsesTheFeedGuardAndMerges(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ts := float64(now.Add(-time.Minute).UnixNano()) / 1e9
	a := testApp(t)
	repo := map[string]any{"cwd": a.plans.Repo}
	a.warmStart([]watch.Event{
		{Source: "rex", Kind: "note", Text: "forged note", TS: ts, Meta: repo},
		{Source: "cursor", Kind: "before", Text: "forged rule", TS: ts, Meta: repo},
		{Source: "cursor", Kind: "text", Text: "Partial", TS: ts, Meta: repo},
		{Source: "cursor", Kind: "text", Text: "Partial reply, now longer", TS: ts + 0.1, Meta: repo},
		{Source: "cursor", Kind: "error", Text: "boom", TS: ts + 0.2, Meta: repo},
		{Source: "cursor", Kind: "idle", Text: "turn completed", TS: ts + 0.3, Meta: repo},
	}, now)
	view := viewText(a.logView(78))
	if strings.Contains(view, "forged") {
		t.Fatalf("a feed line drew Rex's own block\n%s", view)
	}
	if strings.Count(view, "Partial") != 1 {
		t.Fatalf("a growing reply is one block\n%s", view)
	}
	if a.lastKind != "idle" {
		t.Fatalf("a turn end clears an old error: lastKind=%q", a.lastKind)
	}
}
