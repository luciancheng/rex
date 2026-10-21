package tui

import (
	"strings"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// briefView is the Send-off overlay (docs/ux.md §9): a brief generated
// from the open plan, copied for you to paste. Rex never sends it.
type briefView struct {
	path     string // the plan it was made from
	planning bool   // b⏎: the planning brief, not a send-off
	task     string // the pin the send-off is for
	text     string
	status   string // what copying did: "copied (pbcopy)", "sent (OSC 52) · may be blocked", …
	refused  bool   // status says why the plan can't be sent off right now
	pending  <-chan copyResult
	recorded bool // its line is on the tape
}

// startCopy copies the brief off the UI goroutine. Until the tool answers
// it says "copying…"; without a clipboard it says so, and o⏎ prints the
// brief plain.
func (a *app) startCopy(v *briefView) {
	if a.copy == nil {
		v.status, v.pending = "not copied · o⏎ prints it plain", nil
		a.recordBrief(v)
		return
	}
	v.status, v.pending = "copying…", a.copy(v.text)
}

// pollCopies takes in finished copies: the status they earned, the OSC 52
// sequence for the loop to write, and the brief's line on the tape.
func (a *app) pollCopies() bool {
	changed := false
	for _, v := range a.briefs {
		if v == nil || v.pending == nil {
			continue
		}
		select {
		case r := <-v.pending:
			v.status, v.pending = r.status, nil
			a.oscOut += r.osc
			a.recordBrief(v)
			changed = true
		default:
		}
	}
	return changed
}

// recordBrief leaves one line on the tape the first time a brief is
// copied (or not).
func (a *app) recordBrief(v *briefView) {
	if v.recorded {
		return
	}
	v.recorded = true
	// The tape keeps the outcome; the o⏎ hint belongs in the overlay.
	outcome := strings.TrimSuffix(v.status, " · o⏎ prints it plain")
	if v.planning {
		a.tapeLine("brief", outcome)
	} else {
		a.tapeLine("sendoff", v.task+" · "+outcome)
	}
}

// sendoffVerb is s⏎ / sendoff⏎ / handoff⏎.
func (a *app) sendoffVerb() {
	if why := a.briefBlocked(); why != "" {
		a.flash(why, "error")
		return
	}
	v := &briefView{path: a.doc.path}
	if err := a.build(v); err != nil {
		a.flash(err.Error(), "error")
		return
	}
	a.openBrief(v)
}

// briefBlocked says why no brief can be made from the open plan: there
// is none, or the file on disk is gone or doesn't parse (a brief from the
// last good copy would point the next chat at a file that says otherwise).
func (a *app) briefBlocked() string {
	switch {
	case a.doc == nil:
		return "no plan open · tab to plan and open one"
	case a.doc.gone:
		return "can't send off: the plan file is missing"
	case a.doc.plan == nil || a.doc.err != "":
		return "can't send off: the plan file doesn't parse · fix it first"
	}
	return ""
}

// build makes v's text from the open plan as it is now.
func (a *app) build(v *briefView) error {
	if v.planning {
		v.text = plan.Brief(a.doc.plan, a.plans.CurrentRel())
		return nil
	}
	text, err := plan.Sendoff(a.doc.plan, a.plans.CurrentRel())
	if err != nil {
		return err
	}
	v.task = "none"
	if pin := a.doc.plan.Graph().Pin(); pin != nil {
		v.task = pin.ID
	}
	v.text = text
	return nil
}

// refreshBriefs rebuilds open Send-off overlays after the plan changed,
// so c⏎ and o⏎ never hand off an old pin.
func (a *app) refreshBriefs() {
	for _, v := range a.briefs {
		if v == nil || a.doc == nil || v.path != a.doc.path {
			continue
		}
		if why := a.stale(v); why != "" {
			v.status, v.refused = why, true
			continue
		}
		before := v.text
		if a.build(v) == nil && (v.text != before || v.refused) {
			v.status, v.refused = "plan changed · c⏎ copies this one", false
		}
	}
}

// stale says why v can't be copied or printed from the plan as it is now:
// the file is gone, doesn't parse, or has no tasks left. A planning brief
// is always fine; it asks for the plan to be filled.
func (a *app) stale(v *briefView) string {
	if v.planning {
		return ""
	}
	if why := a.briefBlocked(); why != "" {
		return why
	}
	if _, err := plan.Sendoff(a.doc.plan, a.plans.CurrentRel()); err != nil {
		return err.Error()
	}
	return ""
}

// briefVerb is b⏎ / brief⏎: ask an agent to fill or refine the plan.
func (a *app) briefVerb() {
	if a.doc == nil {
		a.flash("no plan open · tab to plan and open one", "error")
		return
	}
	v := &briefView{path: a.doc.path, planning: true}
	_ = a.build(v)
	a.openBrief(v)
}

func (a *app) openBrief(v *briefView) {
	a.briefs[a.pane] = v
	a.overlay[a.pane] = overlaySendoff
	a.overlayScroll[a.pane] = 0
	a.startCopy(v)
}

// briefKey is ⏎ in the Send-off overlay: c⏎ copies again, o⏎ prints the
// brief plain on the normal screen; anything else is dropped.
func (a *app) briefKey() bool {
	v := a.briefs[a.pane]
	text := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(a.draft), "/")))
	a.draft = ""
	if a.doc != nil && v.path == a.doc.path && (text == "c" || text == "copy" || text == "o" || text == "open" || text == "print") {
		if why := a.stale(v); why != "" {
			v.status, v.refused = why, true // never the last good copy
			a.flash(why, "error")
			return true
		}
		_ = a.build(v) // c⏎ and o⏎ use the plan as it is now
	}
	switch text {
	case "c", "copy":
		a.startCopy(v)
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
	lines := []LogLine{{Kind: "command", Text: rule(head, width-cellLen(v.status)-1) + " " + v.status}}
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
		// Keep the brief's own indent (nested tasks, Done sub-lines).
		trimmed := strings.TrimLeft(para, " ")
		indent := "  " + strings.Repeat(" ", min(len(para)-len(trimmed), max(0, inner-12)))
		for _, row := range wrap(trimmed, max(8, width-cellLen(indent))) {
			lines = append(lines, LogLine{Kind: "text", Text: indent + row})
		}
	}
	return lines
}

// plainScreen leaves the alternate screen and prints the brief as plain
// text, so it can be selected with the mouse and stays in scrollback.
func plainScreen(text string) string {
	// Plain text goes straight to stdout, so it is cleaned here too: nothing
	// from a plan can reach the terminal as an escape sequence.
	text = watch.CleanText(text)
	return AltLeave + ShowCursor + WrapOn + "\r\n" + strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\r\n") +
		"\r\n\r\n── rex · ⏎ returns ──\r\n"
}
