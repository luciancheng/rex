package tui

import (
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
