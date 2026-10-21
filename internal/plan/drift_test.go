package plan

import "testing"

const driftPlan = `{
  "goal": "Selected plan cards read as one block",
  "scope": {"in": ["plan cards"], "out": ["extra palettes", "agent auto-loads the plan"]},
  "tasks": [
    {"id": "card-highlight", "title": "Selected plan card is one block", "status": "active",
     "does": ["Bar, border, and fill cover the whole selected card"],
     "files": ["internal/tui/plans.go", "internal/tui/live.go", "docs/cards/"]},
    {"id": "load-outline", "title": "Outline view", "files": ["internal/tui/outline.go"]},
    {"id": "skin", "title": "Skins", "files": ["assets/**/*.png"]}
  ]
}`

// The CUJ H table (docs/ux.md §9a).
func TestCheckEdit(t *testing.T) {
	p := mustParse(t, driftPlan)
	pin := p.Graph().Pin()
	cases := []struct {
		rel    string
		level  DriftLevel
		reason string
	}{
		{"internal/tui/plans.go", OnPin, ""},
		{"internal/tui/plans_test.go", OnPin, ""},
		{"docs/cards/one.md", OnPin, ""},
		{"rex/plans/demo.rex.json", OnPin, ""},
		{"internal/tui/live_view.go", Soft, "next to plans.go"},
		{"internal/tui/outline.go", Soft, "listed for load-outline"},
		{"assets/skins/blue.png", Soft, "listed for skin"},
		{"README.md", Hard, "outside card-highlight files"},
		{"internal/watch/bus.go", Hard, "outside card-highlight files"},
		{"internal/mascot/palettes/blue.go", Hard, "touches palettes, which is out of scope"},
	}
	for _, c := range cases {
		d := CheckEdit(p, pin, c.rel, false)
		if d.Level != c.level || d.Reason != c.reason || d.Subject != c.rel {
			t.Errorf("%s: %v %q", c.rel, d.Level, d.Reason)
		}
	}
	if d := CheckEdit(p, pin, "Internal/TUI/Plans.go", true); d.Level != OnPin {
		t.Fatal("case-folded where the filesystem is")
	}
	if d := CheckEdit(p, nil, "README.md", false); d.Level != OnPin {
		t.Fatal("no pin, no drift")
	}
	bare := mustParse(t, `{"scope": {"out": ["extra palettes"]}, "tasks": [{"id": "a"}]}`)
	if d := CheckEdit(bare, bare.Graph().Pin(), "README.md", false); d.Level != OnPin {
		t.Fatal("a pin with no files only drifts on scope.out")
	}
	if d := CheckEdit(bare, bare.Graph().Pin(), "palettes/x.go", false); d.Level != Hard {
		t.Fatal(d)
	}
}

func TestCheckPrompt(t *testing.T) {
	p := mustParse(t, driftPlan)
	pin := p.Graph().Pin()
	cases := []struct {
		prompt string
		level  DriftLevel
		reason string
	}{
		{"make the selected card highlight cover all three rows", OnPin, ""},
		{"now add a few extra palettes for dark mode", Hard, "asks for extra palettes, which is out of scope"},
		{"go do load-outline next", Soft, "names load-outline, not the pin"},
		{"rewrite the billing service invoice exporter in rust today", Soft, "shares no words with card-highlight"},
		{"fix it", OnPin, ""},
		{"Send-off: x\nrex-sendoff: rex/plans/x.rex.json#other\nload-outline extra palettes", OnPin, ""},
	}
	for _, c := range cases {
		d := CheckPrompt(p, pin, c.prompt)
		if d.Level != c.level || d.Reason != c.reason {
			t.Errorf("%q: %v %q", c.prompt, d.Level, d.Reason)
		}
	}
}

func TestMatchPath(t *testing.T) {
	for _, c := range []struct {
		pattern, rel string
		want         bool
	}{
		{"internal/tui", "internal/tui/live.go", true},
		{"internal/tui/", "internal/tui/live.go", true},
		{"internal/*/live.go", "internal/tui/live.go", true},
		{"**/*.go", "a/b/c.go", true},
		{"internal/**", "internal/x/y/z.go", true},
		{"internal/tu", "internal/tui/live.go", false},
		{"*.go", "a/b.go", false},
	} {
		if got := matchPath(c.pattern, c.rel); got != c.want {
			t.Errorf("%s %s: %v", c.pattern, c.rel, got)
		}
	}
}
