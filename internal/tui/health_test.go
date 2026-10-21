package tui

import (
	"strings"
	"testing"
	"time"

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
	}
	text := viewText(h.lines(78, 4, now))
	for _, want := range []string{"── feed · waiting for agent events", "last event 3h ago (cursor)", "cursor   wired", "claude   not wired · rex ingest --install claude", "4 events from other folders hidden"} {
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
	if !strings.Contains(a.frame(), "rex ingest --install claude") {
		t.Fatal("help shows feed health too")
	}
}
