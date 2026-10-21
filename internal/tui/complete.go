package tui

import (
	"errors"
	"strings"

	"github.com/luciancheng/rex/internal/plan"
)

// doneForm is the completion overlay (docs/ux.md §8), bound to one task.
// The focused field is edited in the composer; the other is kept here.
type doneForm struct {
	id       string
	title    string
	seen     plan.Task // the task as it was when the form opened
	intended string
	files    []string
	since    string

	vals  [2]string // did, verified
	json  [2]string // what the file already says, so an untouched value isn't rewritten
	hints [2]string // shown dim in an empty field; ⏎ accepts them
	focus int
	warn  bool // the task changed on disk; the next ⏎ saves anyway
}

var fieldLabels = [2]string{"did ›", "verified ›"}

// doneVerb is d⏎ / done⏎: open the completion form for the effective pin.
func (a *app) doneVerb() {
	if a.doc == nil {
		a.flash("no plan open · tab to plan and open one", "error")
		return
	}
	if why := a.doc.writable(); why != "" {
		a.flash("can't mark done: "+why, "error")
		return
	}
	p := a.doc.plan
	pin := p.Graph().Pin()
	if pin == nil {
		a.flash("no pin · p⏎ a task first", "muted")
		return
	}
	since, label := a.pinStart(plan.PinKey(a.doc.path, pin.ID))
	ev := gather(a.seen, a.scope, since, pin.ID, pin.Verify)
	f := &doneForm{id: pin.ID, title: pin.Title, seen: *pin, intended: strings.Join(pin.Does, " · "), files: ev.files, since: label}
	f.json[0] = strings.Join(pin.Accomplished, " · ")
	f.json[1] = strings.Join(pin.VerifiedHow, " · ")
	f.vals = f.json
	if ev.did != "" {
		f.hints[0] = "agent says: " + ev.did
	} else {
		f.hints[0] = "what was actually done (required)"
	}
	switch {
	case ev.verified != "":
		f.hints[1] = "agent says: " + ev.verified
	case ev.test != "":
		f.hints[1] = ev.test
	default:
		f.hints[1] = "how it was checked"
	}
	a.form[a.pane] = f
	a.overlay[a.pane] = overlayDone
	a.draft = f.vals[0]
}

// hintValue is what ⏎ takes from a dim hint: the agent's words, or Rex's
// suggestion, never the instructions.
func (f *doneForm) hintValue(i int) string {
	h := f.hints[i]
	if v, ok := strings.CutPrefix(h, "agent says: "); ok {
		return v
	}
	if i == 1 && h != "how it was checked" {
		return h
	}
	return ""
}

func (f *doneForm) moveFocus(a *app, delta int) bool {
	next := max(0, min(1, f.focus+delta))
	if next == f.focus {
		return false
	}
	f.vals[f.focus] = a.draft
	f.focus = next
	a.draft = f.vals[next]
	return true
}

// saveDone writes the completion: status done plus the fields that
// differ from the file. An untouched value prefilled from the file is not
// written, so its bytes don't move.
func (a *app) saveDone() bool {
	f := a.form[a.pane]
	f.vals[f.focus] = a.draft
	did := strings.TrimSpace(f.vals[0])
	if did == "" {
		did = f.hintValue(0)
	}
	if did == "" {
		a.flash("did › is required: what was actually done", "error")
		return true
	}
	verified := strings.TrimSpace(f.vals[1])
	if verified == "" {
		verified = f.hintValue(1)
	}
	var c plan.Completion
	if did != f.json[0] {
		c.Accomplished = []string{did}
	}
	if verified != f.json[1] && verified != "" {
		c.VerifiedHow = []string{verified}
	}
	c.AddModified = f.files
	saved, err := plan.Edit(a.doc.path, plan.CompleteOp(f.id, &f.seen, f.warn, c))
	if errors.Is(err, plan.ErrTaskChanged) {
		f.warn = true
		a.flash(f.id+" changed on disk · ⏎ to save anyway", "error")
		return true
	}
	if err != nil {
		a.flash("can't mark done: "+err.Error(), "error")
		return true
	}
	a.doc.took(saved)
	a.overlay[a.pane] = overlayNone
	a.form[a.pane] = nil
	a.draft = ""
	a.tapeLine("done", f.id+": "+did)
	g := saved.Plan.Graph()
	next := g.Pin()
	switch {
	case next != nil:
		a.flash("✓ "+f.id+" done · next up "+next.ID+" · p⏎ pins it", "accent")
		if a.at == atOutline || a.at == atTaskDetail {
			a.outline.sel = next.ID
		}
	case saved.Plan.Status == plan.Done:
		a.flash("✓ "+f.id+" done · the plan is done", "accent")
	default:
		a.flash("✓ "+f.id+" done · nothing else is unblocked", "accent")
	}
	a.notePin()
	return true
}

func (a *app) cancelDone() {
	a.overlay[a.pane] = overlayNone
	a.form[a.pane] = nil
	a.draft = ""
	a.flash("not marked done · nothing written", "muted")
}

// doneKey handles keys in the completion form.
func (a *app) doneKey(key string) bool {
	f := a.form[a.pane]
	switch key {
	case "up":
		return f.moveFocus(a, -1)
	case "down", "\t":
		return f.moveFocus(a, 1)
	case "esc":
		a.cancelDone()
		return true
	case "\r", "\n":
		return a.saveDone()
	}
	return false
}

// doneLines draws the completion form; the focused field shows the live
// draft.
func (a *app) doneLines(width int) []LogLine {
	f := a.form[a.pane]
	inner := max(1, width-2)
	lines := []LogLine{{Kind: "command", Text: rule("done · "+f.id, width)}}
	if f.title != "" {
		lines = append(lines, LogLine{Kind: "title", Text: "  " + clip(f.title, inner)})
	}
	wide := width >= 50
	field := func(label, value, kind string) {
		if wide {
			for i, row := range wrap(value, max(8, width-14)) {
				lead := "            "
				if i == 0 {
					lead = "  " + padRight(label, 10)
				}
				lines = append(lines, LogLine{Kind: kind, Text: lead + row})
			}
			return
		}
		if label != "" {
			lines = append(lines, LogLine{Kind: "muted", Text: "  " + label})
		}
		lines = append(lines, LogLine{Kind: kind, Text: "    " + clip(value, max(1, width-4))})
	}
	if f.intended != "" {
		field("intended", f.intended, "text")
	}
	files := "none seen"
	if len(f.files) > 0 {
		files = strings.Join(f.files, ", ")
	}
	field("files", files, "text")
	field("", f.since+" · may be partial", "muted")
	lines = append(lines, LogLine{Kind: "gap"})
	for i, label := range fieldLabels {
		value, kind := f.vals[i], "text"
		if i == f.focus {
			value = a.draft
		}
		if strings.TrimSpace(value) == "" {
			value, kind = f.hints[i], "muted"
		}
		row := padRight(label, 11) + value
		if i == f.focus {
			lines = append(lines, selectedLine(row, width))
			continue
		}
		lines = append(lines, LogLine{Kind: kind, Text: "  " + clip(row, inner)})
	}
	lines = append(lines, LogLine{Kind: "gap"})
	if f.warn {
		lines = append(lines, LogLine{Kind: "error", Text: "  " + clip(f.id+" changed on disk · ⏎ to save anyway", inner)})
	}
	note := "⏎ writes done + these fields · esc: nothing written"
	if !wide {
		note = "⏎ saves · esc: nothing written"
	}
	return append(lines, LogLine{Kind: "muted", Text: "  " + clip(note, inner)})
}
