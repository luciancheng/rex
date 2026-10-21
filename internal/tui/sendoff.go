package tui

import (
	"errors"
	"strings"

	"github.com/luciancheng/rex/internal/plan"
)

// briefView is the Send-off overlay (docs/ux.md §9): a brief generated
// from the open plan, copied for you to paste. Rex never sends it.
type briefView struct {
	planning bool   // b⏎: the planning brief, not a send-off
	task     string // the pin the send-off is for
	text     string
	status   string // what copying did: "copied (pbcopy)", "sent (OSC 52) · may be blocked", …
}

// copyText copies a brief and says honestly what happened. Without a
// clipboard Rex says so, and o⏎ prints the brief plain.
func (a *app) copyText(text string) string {
	if a.copy == nil {
		return "not copied · o⏎ prints it plain"
	}
	return a.copy(text)
}

// sendoffVerb is s⏎ / sendoff⏎ / handoff⏎.
func (a *app) sendoffVerb() {
	if a.doc == nil || a.doc.plan == nil {
		a.flash("no plan open · tab to plan and open one", "error")
		return
	}
	text, err := plan.Sendoff(a.doc.plan, a.plans.CurrentRel())
	if errors.Is(err, plan.ErrEmpty) {
		a.flash(err.Error(), "error")
		return
	}
	task := "none"
	if pin := a.doc.plan.Graph().Pin(); pin != nil {
		task = pin.ID
	}
	a.openBrief(&briefView{task: task, text: text})
}

// briefVerb is b⏎ / brief⏎: ask an agent to fill or refine the plan.
func (a *app) briefVerb() {
	if a.doc == nil {
		a.flash("no plan open · tab to plan and open one", "error")
		return
	}
	a.openBrief(&briefView{planning: true, text: plan.Brief(a.doc.plan, a.plans.CurrentRel())})
}

func (a *app) openBrief(v *briefView) {
	v.status = a.copyText(v.text)
	a.briefs[a.pane] = v
	a.overlay[a.pane] = overlaySendoff
	a.overlayScroll[a.pane] = 0
	if v.planning {
		a.tapeLine("brief", v.status)
	} else {
		a.tapeLine("sendoff", v.task+" · "+v.status)
	}
}

// briefKey is ⏎ in the Send-off overlay: c⏎ copies again, o⏎ prints the
// brief plain on the normal screen; anything else is dropped.
func (a *app) briefKey() bool {
	v := a.briefs[a.pane]
	text := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(a.draft), "/")))
	a.draft = ""
	switch text {
	case "c", "copy":
		v.status = a.copyText(v.text)
	case "o", "open", "print":
		a.plainOut = v.text
	}
	return true
}

func (a *app) briefLines(width int) []LogLine {
	v := a.briefs[a.pane]
	inner := max(1, width-2)
	head := "sendoff · " + v.task
	note := "Paste into your agent chat. Rex never sends it."
	if v.planning {
		head = "brief"
		note = "Paste into your agent chat to fill this plan. Rex never sends it."
	}
	lines := []LogLine{{Kind: "command", Text: sideBySide(rule(head, max(8, width-cellLen(v.status)-1)), v.status, width)}}
	if cellLen(head)+cellLen(v.status)+8 > width {
		lines = []LogLine{{Kind: "command", Text: rule(head, width)}, {Kind: "muted", Text: "  " + clip(v.status, inner)}}
	}
	for _, row := range wrap(note, inner) {
		lines = append(lines, LogLine{Kind: "accent", Text: "  " + row})
	}
	lines = append(lines, LogLine{Kind: "gap"})
	for _, para := range strings.Split(strings.TrimRight(v.text, "\n"), "\n") {
		if para == "" {
			lines = append(lines, LogLine{Kind: "gap"})
			continue
		}
		for _, row := range wrap(para, inner) {
			lines = append(lines, LogLine{Kind: "text", Text: "  " + row})
		}
	}
	return lines
}

// plainScreen leaves the alternate screen and prints the brief as plain
// text, so it can be selected with the mouse and stays in scrollback.
func plainScreen(text string) string {
	return AltLeave + ShowCursor + WrapOn + "\r\n" + strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\r\n") +
		"\r\n\r\n── rex · ⏎ returns ──\r\n"
}
