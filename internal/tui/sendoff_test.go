package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/watch"
)

func TestSendoffOverlay(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 40)
	var copied []string
	a.copy = func(text string) string {
		copied = append(copied, text)
		return "copied (fake)"
	}
	typeText(a, "s")
	press(a, "\r")
	if a.overlay[PanePlans] != overlaySendoff || len(copied) != 1 {
		t.Fatalf("s⏎ opens the send-off and copies it: overlay %d, %d copies", a.overlay[PanePlans], len(copied))
	}
	frame := a.frame()
	for _, want := range []string{"── sendoff · card-highlight", "copied (fake)", "Paste into your agent chat. Rex never sends it.", "Send-off: Send-off demo", "File: rex/plans/cuj/demo.rex.json", "sendoff · esc close · c⏎ copy · o⏎ print plain"} {
		if !strings.Contains(frame, want) {
			t.Errorf("missing %q\n%s", want, frame)
		}
	}
	if !strings.HasSuffix(copied[0], "rex-sendoff: rex/plans/cuj/demo.rex.json#card-highlight\n") {
		t.Fatalf("%q", copied[0])
	}
	if last := a.log[len(a.log)-1]; last.Kind != "sendoff" || last.Text != "card-highlight · copied (fake)" {
		t.Fatalf("tape record %+v", last)
	}
	typeText(a, "c")
	press(a, "\r")
	if len(copied) != 2 {
		t.Fatal("c⏎ copies again")
	}
	typeText(a, "zap")
	press(a, "\r")
	if len(copied) != 2 || a.overlay[PanePlans] != overlaySendoff || a.plainOut != "" {
		t.Fatal("other text does nothing in the overlay")
	}
	typeText(a, "o")
	press(a, "\r")
	if a.plainOut != copied[0] {
		t.Fatal("o⏎ prints the brief plain")
	}
	screen := plainScreen(a.plainOut)
	if !strings.HasPrefix(screen, AltLeave) || !strings.Contains(screen, "Send-off: Send-off demo\r\n") || !strings.Contains(screen, "⏎ returns") {
		t.Fatalf("%q", screen)
	}
	a.plainOut, a.plainShown = "", true
	press(a, "x", "esc")
	if !a.plainShown {
		t.Fatal("only ⏎ comes back from the plain brief")
	}
	press(a, "\r")
	if a.plainShown || !a.plainBack {
		t.Fatal("⏎ returns to the live screen")
	}
	press(a, "esc")
	if a.overlay[PanePlans] != overlayNone {
		t.Fatal("Esc closes")
	}
}

func TestSendoffWithoutAClipboardSaysSo(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	press(a, "\t")
	typeText(a, "sendoff")
	press(a, "\r")
	if !strings.Contains(a.frame(), "not copied · o⏎ prints it plain") {
		t.Fatal("a missing clipboard is never reported as copied")
	}
}

func TestEmptyPlanBrief(t *testing.T) {
	a, _, _ := openDemo(t, `{"version": 1, "id": "auth", "tasks": []}`, 120, 32)
	if !strings.Contains(a.frame(), "b⏎ copies a planning brief") || a.placeholder() != "b⏎ brief · ?⏎ help" {
		t.Fatalf("%q\n%s", a.placeholder(), a.frame())
	}
	typeText(a, "s")
	press(a, "\r")
	if a.statusLine().Text != "nothing to send · this plan has no tasks · b⏎ copies a planning brief" {
		t.Fatal(a.statusLine().Text)
	}
	typeText(a, "b")
	press(a, "\r")
	if a.overlay[PanePlans] != overlaySendoff || !strings.Contains(a.frame(), "Planning brief: rex/plans/cuj/demo.rex.json") || !strings.Contains(a.frame(), "brief · esc close") {
		t.Fatalf("%s", a.frame())
	}
	if last := a.log[len(a.log)-1]; last.Kind != "brief" {
		t.Fatalf("%+v", last)
	}
}

func TestPastedSendoffCollapsesOnTheTape(t *testing.T) {
	brief := "Send-off: demo\nFile: x\n\nTasks\n- [active] a\n\nrex-sendoff: rex/plans/x.rex.json#a\n"
	view := viewText(BuildLogView([]watch.Event{{Source: "claude", Kind: "user", Text: brief}}, 60))
	if !strings.Contains(view, "── sendoff") || !strings.Contains(view, "a pasted (7 lines)") || strings.Contains(view, "Send-off: demo") {
		t.Fatalf("%s", view)
	}
}

func TestSendoffOverlayFollowsThePlan(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 40)
	var copied []string
	a.copy = func(text string) string { copied = append(copied, text); return "copied (fake)" }
	press(a, "\t")
	typeText(a, "s")
	press(a, "\r")
	// The pin moves while the overlay stays open on the tape.
	moved := strings.Replace(strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"]`, `"status": "done",
      "depends_on": ["plans-ui"]`, 1), `"status": "pending",
      "depends_on": ["card-highlight"]`, `"status": "active",
      "depends_on": ["card-highlight"]`, 1)
	os.WriteFile(path, []byte(moved), 0o644)
	later(a, c)
	typeText(a, "c")
	press(a, "\r")
	if last := copied[len(copied)-1]; !strings.HasSuffix(last, "#load-outline\n") {
		t.Fatalf("c⏎ copies the brief for the pin as it is now:\n%s", last)
	}
	other := strings.Replace(path, "demo.rex.json", "other.rex.json", 1)
	os.WriteFile(other, []byte(demoPlan), 0o644)
	a.plans.Current = other
	a.openedPlan()
	if a.overlay[PaneChat] == overlaySendoff {
		t.Fatal("opening another plan closes a send-off of the old one")
	}
}

func TestSendoffRefusesAMissingOrBrokenFile(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 40)
	os.WriteFile(path, []byte(`{"tasks": [`), 0o644)
	later(a, c)
	later(a, c)
	typeText(a, "s")
	press(a, "\r")
	if a.overlay[PanePlans] == overlaySendoff || !strings.Contains(a.toast.Text, "doesn't parse") {
		t.Fatalf("no brief from a broken file: %q", a.toast.Text)
	}
	os.Remove(path)
	later(a, c)
	typeText(a, "s")
	press(a, "\r")
	if a.overlay[PanePlans] == overlaySendoff {
		t.Fatal("no brief from a missing file")
	}
}

func TestPlainScreenKeys(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 40)
	typeText(a, "s")
	press(a, "\r")
	typeText(a, "o")
	press(a, "\r")
	a.plainText, a.plainOut, a.plainShown = a.plainOut, "", true // what the loop does
	press(a, CtrlC)
	if a.plainShown || !a.quitPending || a.quit {
		t.Fatal("Ctrl+C on the plain screen comes back with quit armed, where it shows")
	}
	if !strings.Contains(a.frame(), "ctrl+c again") {
		t.Fatalf("the armed quit is on screen\n%s", a.frame())
	}
	typeText(a, "o")
	press(a, "\r")
	a.plainText, a.plainOut, a.plainShown = a.plainOut, "", true
	press(a, "\r")
	if a.plainShown || a.quitPending {
		t.Fatal("⏎ comes back and disarms quit")
	}
}

func TestPlainScreenNeverPrintsEscapes(t *testing.T) {
	out := plainScreen("Send-off: x\x1b]0;pwned\x07\nrest")
	if strings.Contains(out, "]0;pwned") {
		t.Fatalf("%q", out)
	}
}

func TestCollapseNeedsTheWholeFingerprintLine(t *testing.T) {
	mention := "what does rex-sendoff: a#b mean?"
	if view := viewText(BuildLogView([]watch.Event{{Source: "claude", Kind: "user", Text: mention}}, 60)); strings.Contains(view, "pasted") {
		t.Fatalf("a mention is not a pasted brief\n%s", view)
	}
	brief := "careful with the tests\n\nSend-off: demo\n\nrex-sendoff: rex/plans/my plan.rex.json#a\n"
	view := viewText(BuildLogView([]watch.Event{{Source: "claude", Kind: "user", Text: brief}}, 80))
	if !strings.Contains(view, "a pasted (5 lines) · careful with the tests") {
		t.Fatalf("a path with a space collapses, and the user's own words stay\n%s", view)
	}
}

func TestSendoffOverlayLooksRight(t *testing.T) {
	a, _, _ := openDemo(t, strings.Replace(demoPlan, `"id": "load-outline",`, `"id": "load-outline", "parent": "plans-ui",`, 1), 120, 40)
	a.copy = func(string) string { return "copied (fake)" }
	typeText(a, "s")
	press(a, "\r")
	rows := a.frameRows().Rows
	for _, row := range rows {
		if strings.Contains(row, "── sendoff") && strings.Contains(row, "…") {
			t.Fatalf("the title rule fits without a cut: %q", row)
		}
	}
	if view := viewText(a.overlayBody(78)); !strings.Contains(view, "    - [pending] load-outline") {
		t.Fatalf("nested tasks keep their indent\n%s", view)
	}
	if last := a.log[len(a.log)-1]; strings.Contains(last.Text, "o⏎") {
		t.Fatalf("the tape keeps the outcome only: %q", last.Text)
	}
}
