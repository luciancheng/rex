package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/plan"
)

func TestPinNeedsConfirmWhenItMovesThePin(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	press(a, "down")
	typeText(a, "p")
	if !strings.Contains(a.frame(), "⏎ pin load-outline") {
		t.Fatalf("the footer says what p⏎ will pin\n%s", a.frame())
	}
	press(a, "\r")
	if a.overlay[PanePlans] != overlayPin {
		t.Fatal("pinning a waiting task over an active one asks first")
	}
	frame := a.frame()
	for _, want := range []string{"── pin load-outline", "! it waits on card-highlight (active)", "! it moves the pin off card-highlight", "pin · p⏎ move pin · esc cancel"} {
		if !strings.Contains(frame, want) {
			t.Errorf("missing %q\n%s", want, frame)
		}
	}
	before, _ := os.ReadFile(path)
	press(a, "\r")
	if a.overlay[PanePlans] != overlayPin {
		t.Fatal("a bare ⏎ does nothing")
	}
	press(a, "esc")
	if after, _ := os.ReadFile(path); string(after) != string(before) || a.overlay[PanePlans] != overlayNone {
		t.Fatal("Esc cancels, nothing written")
	}
	typeText(a, "p")
	press(a, "\r")
	typeText(a, "p")
	press(a, "\r")
	after, _ := os.ReadFile(path)
	want := strings.Replace(strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"]`, `"status": "pending",
      "depends_on": ["plans-ui"]`, 1), `"status": "pending",
      "depends_on": ["card-highlight"]`, `"status": "active",
      "depends_on": ["card-highlight"]`, 1)
	if string(after) != want {
		t.Fatalf("p⏎ changes only the two statuses\n%s", after)
	}
	if a.statusLine().Text != "▸ load-outline pinned" {
		t.Fatalf("%q", a.statusLine().Text)
	}
	if st := plan.LoadState(a.statePath); st.Pins[plan.PinKey(path, "load-outline")] == 0 {
		t.Fatal("the pin start is remembered outside the repo")
	}
	logs := len(a.log)
	later(a, c)
	if len(a.log) != logs || strings.HasPrefix(a.statusLine().Text, "plan updated") {
		t.Fatal("Rex's own pin does not read as an outside edit")
	}
	if a.exitLine() != "rex: rex/plans/cuj/demo.rex.json · pinned load-outline" {
		t.Fatal(a.exitLine())
	}
}

func TestPinCancelsOnOtherText(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 120, 32)
	before, _ := os.ReadFile(path)
	press(a, "down")
	typeText(a, "p")
	press(a, "\r")
	typeText(a, "zap")
	press(a, "\r")
	if after, _ := os.ReadFile(path); string(after) != string(before) || a.overlay[PanePlans] != overlayNone || a.statusLine().Text != "pin cancelled · nothing written" {
		t.Fatalf("%q", a.statusLine().Text)
	}
}

func TestPinFromTheTape(t *testing.T) {
	src := strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"]`, `"status": "pending",
      "depends_on": ["plans-ui"]`, 1)
	a, _, path := openDemo(t, src, 120, 32)
	if a.statusLine().Text != "▸ card-highlight  Selected plan card is one block (next up · p⏎ pins it)" {
		t.Fatal(a.statusLine().Text)
	}
	press(a, "\t")
	typeText(a, "p")
	press(a, "\r")
	if p, _ := plan.Load(path); p.Task("card-highlight").Status != plan.Active || p.Status != plan.Active {
		t.Fatal("a bare p⏎ on the tape pins next up without asking")
	}
	typeText(a, "p")
	press(a, "\r")
	if a.statusLine().Text != "▸ card-highlight is already pinned" {
		t.Fatal(a.statusLine().Text)
	}
	typeText(a, "pin plans-ui")
	press(a, "\r")
	if a.overlay[PaneChat] != overlayPin {
		t.Fatal("pin <id> over the active pin asks")
	}
	typeText(a, "p")
	press(a, "\r")
	p, _ := plan.Load(path)
	if pu := p.Task("plans-ui"); pu.Status != plan.Active || pu.Accomplished[0] != "Home cards, browse, duplicate warn" {
		t.Fatal("pinning a done task reopens it and keeps its retrospective")
	}
	typeText(a, "pin nope")
	press(a, "\r")
	if a.statusLine().Text != "no task nope in this plan" {
		t.Fatal(a.statusLine().Text)
	}
}

func TestPinRefusesReadOnlyPlans(t *testing.T) {
	a, _, path := openDemo(t, `{"version": 2, "tasks": [{"id": "a"}]}`, 120, 32)
	typeText(a, "p")
	press(a, "\r")
	if !strings.HasPrefix(a.statusLine().Text, "can't pin: read-only: version 2") {
		t.Fatal(a.statusLine().Text)
	}
	if got, _ := os.ReadFile(path); string(got) != `{"version": 2, "tasks": [{"id": "a"}]}` {
		t.Fatal("file touched")
	}
	b := testApp(t)
	typeText(b, "/p")
	press(b, "\r")
	if b.statusLine().Text != "no plan open · tab to plan and open one" {
		t.Fatal(b.statusLine().Text)
	}
}
