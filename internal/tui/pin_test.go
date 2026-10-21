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

func TestPinConfirmRefusesAStaleClaim(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	press(a, "down")
	typeText(a, "p")
	press(a, "\r") // asks: moves the pin off card-highlight
	// An agent claims another task while the confirm is open.
	claimed := strings.Replace(demoPlan, `"status": "done",
      "does": ["Create, open`, `"status": "active",
      "does": ["Create, open`, 1)
	os.WriteFile(path, []byte(claimed), 0o644)
	later(a, c)
	typeText(a, "p")
	press(a, "\r")
	if after, _ := os.ReadFile(path); string(after) != claimed {
		t.Fatalf("a claim made after the warning was shown is never overwritten\n%s", after)
	}
	if !strings.Contains(a.statusLine().Text, "look again") {
		t.Fatalf("%q", a.statusLine().Text)
	}
}

func TestPinConfirmBelongsToItsPlan(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 120, 32)
	press(a, "\t")
	typeText(a, "pin load-outline")
	press(a, "\r")
	if a.overlay[PaneChat] != overlayPin {
		t.Fatal("the tape asks first")
	}
	// Open another plan in PLAN.
	other := strings.Replace(path, "demo.rex.json", "other.rex.json", 1)
	os.WriteFile(other, []byte(demoPlan), 0o644)
	press(a, "\t")
	a.plans.Current = other
	a.openedPlan()
	if a.overlay[PaneChat] == overlayPin {
		t.Fatal("opening another plan closes a confirm asked about the old one")
	}
	if got, _ := os.ReadFile(other); string(got) != demoPlan {
		t.Fatal("the other plan is untouched")
	}
}

func TestPinFromTheTapeLeavesPlanAlone(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 120, 32)
	press(a, "down", "\r") // load-outline's detail
	if a.at != atTaskDetail || a.outline.sel != "load-outline" {
		t.Fatalf("at=%v sel=%q", a.at, a.outline.sel)
	}
	press(a, "\t")
	typeText(a, "pin plans-ui")
	press(a, "\r") // reopening plans-ui moves the pin off card-highlight: asks
	typeText(a, "p")
	press(a, "\r")
	if p, _ := plan.Load(path); p.Task("plans-ui").Status != plan.Active {
		t.Fatal("the tape pinned plans-ui")
	}
	if a.at != atTaskDetail || a.outline.sel != "load-outline" {
		t.Fatalf("PLAN stays where it was: at=%v sel=%q", a.at, a.outline.sel)
	}
}

func TestPinPreviewNamesTheLeaf(t *testing.T) {
	src := `{"tasks": [{"id": "ui", "title": "UI"}, {"id": "card", "parent": "ui", "title": "Card"}]}`
	a, _, _ := openDemo(t, src, 120, 32)
	for a.outline.sel != "ui" {
		press(a, "up")
	}
	typeText(a, "p")
	if frame := a.frame(); !strings.Contains(frame, "⏎ pin card") {
		t.Fatalf("p⏎ on a parent pins its first ready leaf; the footer names it\n%s", frame)
	}
}

func TestPinConfirmScrollsOnAShortScreen(t *testing.T) {
	src := `{"tasks": [{"id": "a", "status": "active"}, {"id": "b", "status": "active"}, {"id": "c", "status": "active"}, {"id": "d", "status": "active"}, {"id": "e", "status": "blocked", "depends_on": ["a", "b", "c", "d", "zz"]}]}`
	a, _, _ := openDemo(t, src, 80, 14)
	press(a, "end")
	typeText(a, "p")
	press(a, "\r")
	if a.overlay[PanePlans] != overlayPin {
		t.Fatal("asks")
	}
	for i := 0; i < 20; i++ {
		press(a, "down")
	}
	if frame := a.frame(); !strings.Contains(frame, "p⏎ pins it anyway") {
		t.Fatalf("the confirm's own instruction can be scrolled to\n%s", frame)
	}
}

func TestReadOnlyRowOneOffersNoPin(t *testing.T) {
	a, _, _ := openDemo(t, `{"version": 2, "tasks": [{"id": "a"}]}`, 120, 32)
	if text := a.statusLine().Text; strings.Contains(text, "p⏎") {
		t.Fatalf("a read-only plan doesn't offer p⏎: %q", text)
	}
}

func TestPinSurvivesARepeatedIdLoop(t *testing.T) {
	a, _, _ := openDemo(t, `{"tasks":[{"id":"x"},{"id":"y","parent":"x"},{"id":"x","parent":"y"}]}`, 120, 32)
	typeText(a, "p")
	press(a, "\r") // used to recurse until the stack overflowed
	typeText(a, "pin x")
	press(a, "\r")
}

func TestRevertingRexsPinIsAnOutsideEdit(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	press(a, "down")
	typeText(a, "p")
	press(a, "\r")
	typeText(a, "p")
	press(a, "\r")                              // pinned load-outline
	os.WriteFile(path, []byte(demoPlan), 0o644) // someone puts the old file back
	later(a, c)
	if !strings.Contains(a.statusLine().Text, "plan updated") {
		t.Fatalf("a revert of Rex's own write is announced: %q", a.statusLine().Text)
	}
}
