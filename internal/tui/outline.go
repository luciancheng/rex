package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// planDoc is the open plan file: the last good read, and what is on disk
// now. Rex never pushes a plan anywhere; it re-reads the file every
// docPollEvery and notices a change by content hash (docs/ux.md §6).
type planDoc struct {
	path string
	plan *plan.Plan // last good read; nil until the file has parsed once
	hash string     // the bytes on disk at the last read
	good string     // hash of the bytes behind plan
	own  string     // hash of Rex's own last write: taken in quietly

	bad      string // why the bytes on disk don't parse, not yet shown
	badPolls int
	err      string // why they don't parse, shown
	gone     bool
	polled   time.Time
}

const docPollEvery = 500 * time.Millisecond

func openDoc(path string, now time.Time) *planDoc {
	d := &planDoc{path: path}
	d.read(now, true)
	return d
}

// read re-reads the file. It reports whether anything on screen changes,
// and what changed in the plan when someone else edited it.
func (d *planDoc) read(now time.Time, first bool) (changed bool, summary string) {
	d.polled = now
	data, err := plan.ReadFile(d.path)
	if os.IsNotExist(err) {
		changed = !d.gone
		d.gone = true
		return changed, ""
	}
	if err != nil {
		// There, but not readable as a plan (a folder, a FIFO, too big, no
		// permission): say so, keep the last good copy.
		why := "can't read the file: " + err.Error()
		changed = d.err != why || d.gone
		d.err, d.gone, d.hash = why, false, ""
		return changed, ""
	}
	if d.gone {
		d.gone, changed = false, true
	}
	h := plan.Hash(data)
	if h == d.hash {
		// Agents save in pieces: a parse error is shown only once the same
		// bytes have been on disk for two polls.
		if d.bad != "" && d.err == "" {
			if d.badPolls++; d.badPolls >= 2 {
				d.err, changed = d.bad, true
			}
		}
		return changed, ""
	}
	d.hash = h
	p, perr := plan.Parse(data)
	if perr != nil {
		d.bad, d.badPolls = perr.Error(), 1
		if first || d.plan == nil || d.err != "" {
			d.err, changed = d.bad, true
		}
		return changed, ""
	}
	old, oldGood := d.plan, d.good
	d.plan, d.good, d.bad, d.badPolls, d.err = p, h, "", 0, ""
	if old == nil || h == d.own || h == oldGood {
		// First read, Rex's own write, or a save that tore and came back
		// to the same bytes: nothing to announce.
		return true, ""
	}
	return true, planChanges(old, p)
}

// took records a plan Rex just wrote, so the next poll doesn't report
// Rex's own edit as someone else's.
func (d *planDoc) took(saved *plan.Saved) {
	d.own = plan.Hash(saved.Data)
	d.hash, d.good, d.plan = d.own, d.own, saved.Plan // so going back to the old bytes reads as an edit
	d.bad, d.badPolls, d.err, d.gone = "", 0, "", false
}

func (d *planDoc) poll(now time.Time) (bool, string) {
	if now.Sub(d.polled) < docPollEvery {
		return false, ""
	}
	return d.read(now, false)
}

// writable says why Rex can't write the plan now, or "".
func (d *planDoc) writable() string {
	switch {
	case d.plan == nil || d.err != "":
		return "the plan file doesn't parse"
	case d.gone:
		return "the plan file is missing"
	case d.plan.ReadOnly != "":
		return "read-only: " + d.plan.ReadOnly
	}
	return ""
}

// planChanges sums up an outside edit: status moves, then tasks added or
// removed, e.g. "plans-ui → done · +1 task".
func planChanges(old, now *plan.Plan) string {
	before := map[string]plan.Status{}
	for _, t := range old.Tasks {
		if t.ID != "" {
			before[t.ID] = t.Status
		}
	}
	var parts []string
	moved, added := 0, 0
	seen := map[string]bool{}
	for _, t := range now.Tasks {
		if t.ID == "" || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		st, ok := before[t.ID]
		switch {
		case !ok:
			added++
		case st != t.Status:
			if moved++; moved <= 2 {
				parts = append(parts, t.ID+" → "+string(t.Status))
			}
		}
	}
	if moved > 2 {
		parts = append(parts, fmt.Sprintf("+%d more moved", moved-2))
	}
	removed := 0
	for id := range before {
		if !seen[id] {
			removed++
		}
	}
	if added > 0 {
		parts = append(parts, plural(added, "+%d task", "+%d tasks"))
	}
	if removed > 0 {
		parts = append(parts, plural(removed, "−%d task", "−%d tasks"))
	}
	if len(parts) == 0 {
		return "edited"
	}
	return strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf(one, n)
	}
	return fmt.Sprintf(many, n)
}

// planAt is the screen inside PLAN once a plan is open.
type planAt int

const (
	atLibrary planAt = iota
	atOutline
	atPlanDetail
	atTaskDetail
)

// outlineView is where you are in the open plan: the selected row by
// identity ("" is the title rule, else a task key), the list's first
// visible line, and the detail scroll.
type outlineView struct {
	sel    string
	view   int
	scroll int
}

// taskKey identifies a task across reloads: its id, or its position when
// it has none.
func taskKey(t *plan.Task) string {
	if t.ID != "" {
		return t.ID
	}
	return "#" + strconv.Itoa(t.Index)
}

// outlineRows are the selectable rows in order: the title rule, then
// every task in file order.
func outlineRows(p *plan.Plan) []string {
	rows := []string{""}
	seen := map[string]bool{}
	for i := range p.Tasks {
		k := taskKey(&p.Tasks[i])
		if !seen[k] {
			seen[k] = true
			rows = append(rows, k)
		}
	}
	return rows
}

func findTask(p *plan.Plan, key string) *plan.Task {
	for i := range p.Tasks {
		if taskKey(&p.Tasks[i]) == key {
			return &p.Tasks[i]
		}
	}
	return nil
}

// defaultSel is where the cursor lands on a freshly opened plan: the pin,
// else the first task, else the title.
func defaultSel(p *plan.Plan) string {
	if p == nil || len(p.Tasks) == 0 {
		return ""
	}
	if pin := p.Graph().Pin(); pin != nil {
		return taskKey(pin)
	}
	return taskKey(&p.Tasks[0])
}

// keepSel keeps the selection on the same task after a reload; if it is
// gone, it falls back to the pin, then the title.
func (v *outlineView) keepSel(p *plan.Plan) {
	// (Task detail keeps its task even when it leaves the file, so the
	// screen can say so instead of showing another task.)
	if p == nil || v.sel == "" || findTask(p, v.sel) != nil {
		return
	}
	v.sel = defaultSel(p)
}

func (v *outlineView) move(p *plan.Plan, delta int) {
	rows := outlineRows(p)
	at := 0
	for i, k := range rows {
		if k == v.sel {
			at = i
		}
	}
	at = max(0, min(len(rows)-1, at+delta))
	v.sel = rows[at]
}

func (v *outlineView) edge(p *plan.Plan, last bool) {
	rows := outlineRows(p)
	v.sel = rows[0]
	if last {
		v.sel = rows[len(rows)-1]
	}
}

func planTitle(p *plan.Plan, path string) string {
	switch {
	case p != nil && strings.TrimSpace(p.Title) != "":
		return p.Title
	case p != nil && strings.TrimSpace(p.ID) != "":
		return p.ID
	}
	return planStem(filepath.Base(path))
}

// glyph is a task's mark in the outline (docs/ux.md §5) and the tag that
// goes on the right of its row.
func glyph(t *plan.Task, g *plan.Graph, pin *plan.Task) (mark, tag, tagKind string) {
	if !g.Leaf(t) {
		done, total := g.Rollup(t)
		return " ", fmt.Sprintf("%d/%d", done, total), "muted"
	}
	if g.Cyclic(t) {
		return "·", "cycle", "error"
	}
	switch {
	case pin != nil && t == pin:
		return "▸", "", ""
	case t.Status == plan.Done:
		return "✓", "", ""
	case t.Status == plan.Skipped:
		return "–", "skipped", "muted"
	case t.Status == plan.Blocked:
		return "!", "blocked", "error"
	case t.Status == plan.Active:
		return "▸", "", "" // a second active task; row 1 warns
	}
	if unmet := g.Unmet(t); len(unmet) > 0 {
		kind := "muted"
		if strings.HasPrefix(unmet[0], "?") {
			kind = "error"
		}
		return "·", "after " + unmet[0], kind
	}
	return "○", "", ""
}

func blurb(t *plan.Task) string {
	if len(t.Does) > 0 {
		return strings.Join(t.Does, " · ")
	}
	return t.Description
}

// lastDone is the "where we left off" line: the last finished task in
// file order that says what it accomplished.
func lastDone(p *plan.Plan) string {
	for i := len(p.Tasks) - 1; i >= 0; i-- {
		t := &p.Tasks[i]
		if t.Status == plan.Done && len(t.Accomplished) > 0 {
			return "last ✓ " + taskKey(t) + ": " + strings.Join(t.Accomplished, " · ")
		}
	}
	return ""
}

// selectedLine paints a row as the selection: the ▌ bar and fill.
func selectedLine(text string, width int) LogLine {
	return LogLine{Kind: "pick", Fill: "pickbg", Text: "▌ " + clip(text, max(1, width-2)), Selected: true}
}

// sideBySide fits left and a right-hand tag into width cells, cutting the
// left side first so the tag stays readable.
func sideBySide(left, right string, width int) string {
	if right == "" || width < cellLen(right)+12 {
		return left
	}
	room := width - cellLen(right) - 2
	if cellLen(left) > room {
		left = cutCells(left, room-1) + "…"
	}
	return padRight(left, width-cellLen(right)) + right
}

// outlineBody lays out the outline for width cells and viewH body rows.
func outlineBody(d *planDoc, v *outlineView, width, viewH int) []LogLine {
	inner := max(1, width-2)
	title := planTitle(nil, d.path)
	if d.plan == nil {
		lines := []LogLine{{Kind: "command", Text: rule(title, width)}, {Kind: "gap"}}
		lines = append(lines, LogLine{Kind: "error", Text: "  " + clip("can't read this plan", inner)})
		for _, row := range wrap(d.err, inner-2) {
			lines = append(lines, LogLine{Kind: "error", Text: "    " + clip(row, inner-2)})
		}
		if d.gone {
			lines = append(lines, LogLine{Kind: "error", Text: "  " + clip("the file is missing", inner)})
		}
		return append(lines, LogLine{Kind: "idle", Text: "  " + clip("Rex shows it once it is valid JSON", inner)})
	}
	p := d.plan
	g := p.Graph()
	pin := g.Pin()
	done, total := g.Progress()
	// measured as rule() paints it, so a tab or newline can't push out the tail
	title = strings.ReplaceAll(watch.CleanText(planTitle(p, d.path)), "\n", " ")
	tail := " · " + string(p.Status) + " · " + fmt.Sprintf("%d/%d", done, total)
	// fit is the rule's label for room cells: when it is narrow, the title
	// is cut and the status and progress stay.
	fit := func(room int) string {
		switch r := room - 6 - cellLen(tail); {
		case cellLen(title+tail)+6 <= room:
			return title + tail
		case r >= 4:
			return cutCells(title, r-1) + "…" + tail
		case cellLen(tail)+3 <= room:
			return strings.TrimPrefix(tail, " · ") // no room for any title
		}
		return cutCells(title+tail, max(1, room-7)) + "…"
	}
	rw := width // the rule's width; the selection bar takes two cells
	if v.sel == "" {
		rw = max(1, width-2)
	}
	head := fit(rw)

	var tasks []LogLine
	idW := 0
	for i := range p.Tasks {
		idW = max(idW, cellLen(taskKey(&p.Tasks[i])))
	}
	wide := width >= 60
	gap := "  "
	if !wide {
		gap = " "
	}
	idW = min(idW, 16, max(6, inner-14)) // leave the title at least ~10 cells
	indentOf := func(t *plan.Task) string {
		depth := g.Depth(t)
		indent := strings.Repeat("  ", min(depth, 2))
		if depth > 2 {
			indent += "↳ "
		}
		return indent
	}
	// Titles line up across indent levels.
	headW := 0
	for i := range p.Tasks {
		t := &p.Tasks[i]
		headW = max(headW, cellLen(indentOf(t))+2+min(cellLen(taskKey(t)), idW))
	}
	for i := range p.Tasks {
		t := &p.Tasks[i]
		key := taskKey(t)
		if findTask(p, key) != t {
			continue // a repeated id: only the first is reachable
		}
		indent := indentOf(t)
		mark, tag, tagKind := glyph(t, g, pin)
		if !wide && tagKind != "error" && g.Leaf(t) {
			tag = "" // narrow rows keep the title; the glyph still says it, and a parent keeps its rollup
		}
		name := t.Title
		if name == "" {
			name = "(untitled)"
		}
		id := key
		if cellLen(id) > idW {
			id = cutCells(id, idW-1) + "…"
		}
		text := padRight(indent+mark+" "+id, headW) + gap + name
		if b := blurb(t); wide && b != "" {
			text += " · " + b
		}
		selected := key == v.sel
		kind := "text"
		switch {
		case !g.Leaf(t):
			kind = "title"
		case plan.Finished(t.Status):
			kind = "muted"
		case pin != nil && t == pin:
			kind = "accent"
		}
		if selected {
			tasks = append(tasks, selectedLine(sideBySide(text, tag, inner), width))
			if b := blurb(t); !wide && b != "" {
				tasks = append(tasks, selectedLine(indent+"  "+b, width))
			}
			continue
		}
		line := LogLine{Kind: kind, Text: "  " + clip(sideBySide(text, tag, inner), inner)}
		if tag != "" && tagKind == "error" {
			line.Kind = "error"
		}
		tasks = append(tasks, line)
	}

	var top []LogLine
	if last := lastDone(p); last != "" {
		top = append(top, LogLine{Kind: "muted", Text: "  " + clip(last, inner)})
	}
	if p.ReadOnly != "" {
		top = append(top, LogLine{Kind: "error", Text: "  " + clip("read-only: "+p.ReadOnly, inner)})
	}
	if n := len(p.Problems); n > 0 {
		top = append(top, LogLine{Kind: "error", Text: "  " + clip(plural(n, "! %d problem", "! %d problems")+" · ⏎ on the title lists them", inner)})
	}
	if len(p.Tasks) == 0 {
		top = append(top, LogLine{Kind: "gap"}, LogLine{Kind: "idle", Text: "  " + clip("No tasks yet · your agent fills this file · b⏎ copies a planning brief", inner)})
	} else {
		top = append(top, LogLine{Kind: "gap"})
	}

	ruleText := rule(head, rw)
	if 1+len(top)+len(tasks) > viewH && len(p.Tasks) > 0 {
		at := 0
		if v.sel != "" {
			for i, k := range outlineRows(p)[1:] {
				if k == v.sel {
					at = i + 1
				}
			}
		}
		tag := fmt.Sprintf("tasks %d/%d", at, len(outlineRows(p))-1)
		room := rw - cellLen(tag) - 1
		ruleText = rule(fit(room), room) + " " + tag
	}
	lines := []LogLine{{Kind: "command", Text: ruleText}}
	if v.sel == "" {
		lines[0] = selectedLine(ruleText, width)
	}
	lines = append(lines, top...)
	for i := range lines {
		lines[i].Head = true // the rule, last ✓ and warnings stay put while tasks scroll
	}
	return append(lines, tasks...)
}

// detailField is one labelled section of a detail screen.
func detailField(label string, items []string, width int) []LogLine {
	if len(items) == 0 {
		return nil
	}
	inner := max(1, width-4)
	lines := []LogLine{{Kind: "muted", Text: "  " + clip(label, width-2)}}
	for _, item := range items {
		first := true
		for _, para := range strings.Split(item, "\n") {
			for _, row := range wrap(para, inner-2) {
				prefix := "    "
				if first && len(items) > 1 {
					prefix = "  • "
				}
				first = false
				lines = append(lines, LogLine{Kind: "text", Text: prefix + clip(row, inner)})
			}
		}
	}
	return lines
}

func nonEmpty(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []string{s}
}

func planDetail(d *planDoc, width int) []LogLine {
	p := d.plan
	lines := []LogLine{{Kind: "command", Text: rule("plan · "+planTitle(p, d.path), width)}}
	if p == nil {
		return append(lines, LogLine{Kind: "error", Text: "  " + clip(d.err, width-2)})
	}
	g := p.Graph()
	done, total := g.Progress()
	meta := fmt.Sprintf("%s · %d/%d done · %s", p.Status, done, total, filepath.Base(d.path))
	lines = append(lines, LogLine{Kind: "muted", Text: "  " + clip(meta, width-2)}, LogLine{Kind: "gap"})
	add := func(label string, items []string) {
		if f := detailField(label, items, width); f != nil {
			lines = append(lines, f...)
			lines = append(lines, LogLine{Kind: "gap"})
		}
	}
	add("goal", nonEmpty(p.Goal))
	add("in scope", p.Scope.In)
	add("out of scope", p.Scope.Out)
	if p.ReadOnly != "" {
		add("read-only", []string{p.ReadOnly})
	}
	var problems []string
	for _, pr := range p.Problems {
		problems = append(problems, pr.String())
	}
	if len(problems) > 0 {
		for _, l := range detailField("problems", problems, width) {
			if l.Kind == "text" {
				l.Kind = "error"
			}
			lines = append(lines, l)
		}
	}
	if p.Goal == "" && len(p.Scope.In) == 0 && len(p.Scope.Out) == 0 && len(problems) == 0 {
		lines = append(lines, LogLine{Kind: "idle", Text: "  " + clip("No goal or scope yet · your agent fills this file", width-2)})
	}
	return lines
}

func taskDetail(d *planDoc, key string, width int) []LogLine {
	p := d.plan
	var t *plan.Task
	if p != nil {
		t = findTask(p, key)
	}
	if t == nil {
		return []LogLine{{Kind: "command", Text: rule(key, width)}, {Kind: "idle", Text: "  " + clip("this task is no longer in the file", width-2)}}
	}
	g := p.Graph()
	pin := g.Pin()
	status := string(t.Status)
	if t == pin {
		status += " · pinned"
		if t.Status != plan.Active {
			status = "next up · pinned"
		}
	}
	lines := []LogLine{{Kind: "command", Text: rule(taskKey(t)+" · "+status, width)}}
	for _, row := range wrap(t.Title, width-2) {
		lines = append(lines, LogLine{Kind: "title", Text: "  " + clip(row, width-2)})
	}
	lines = append(lines, LogLine{Kind: "gap"})
	add := func(label string, items []string) {
		if f := detailField(label, items, width); f != nil {
			lines = append(lines, f...)
			lines = append(lines, LogLine{Kind: "gap"})
		}
	}
	if parent := g.Task(t.Parent); parent != nil {
		add("under", []string{parent.ID + ": " + parent.Title})
	} else if t.Parent != "" {
		add("under", []string{"?" + t.Parent + " (no such task)"})
	}
	var deps []string
	for _, id := range t.DependsOn {
		dep := g.Task(id)
		switch {
		case dep == nil:
			deps = append(deps, "? "+id+" (no such task)")
		case plan.Finished(dep.Status):
			deps = append(deps, "✓ "+id+": "+dep.Title)
		default:
			deps = append(deps, "· "+id+": "+dep.Title+" ("+string(dep.Status)+")")
		}
	}
	add("after", deps)
	var after []string
	for _, dt := range g.Dependents(t.ID) {
		after = append(after, taskKey(dt)+": "+dt.Title)
	}
	add("then", after)
	if kids := g.Children(t.ID); len(kids) > 0 {
		var rows []string
		for _, c := range kids {
			rows = append(rows, taskKey(c)+": "+c.Title+" ("+string(c.Status)+")")
		}
		add("tasks under it", rows)
	}
	add("does", t.Does)
	add("needs", t.Needs)
	add("verify", t.Verify)
	add("files", t.Files)
	add("in scope", t.Scope.In)
	add("out of scope", t.Scope.Out)
	add("description", nonEmpty(t.Description))
	if t.Status == plan.Done || len(t.Accomplished)+len(t.Modified)+len(t.VerifiedHow) > 0 || t.CompletedNotes != "" {
		lines = append(lines, LogLine{Kind: "command", Text: rule("retrospective", width)})
		add("accomplished", t.Accomplished)
		add("modified", t.Modified)
		add("verified", t.VerifiedHow)
		add("notes", nonEmpty(t.CompletedNotes))
	}
	return lines
}

var errPos = regexp.MustCompile(`^(\d+:\d+): `)

// docStatus is row 1 for the open plan (docs/ux.md §4), or false when
// the plan has nothing to say.
func docStatus(d *planDoc) (StatusLine, bool) {
	if d == nil {
		return StatusLine{}, false
	}
	last := ""
	if d.plan != nil {
		last = " · showing last good"
	}
	switch {
	case d.err != "":
		if m := errPos.FindStringSubmatch(d.err); m != nil {
			return StatusLine{Text: "plan JSON error " + m[1] + last, Kind: "error"}, true
		}
		return StatusLine{Text: "plan error: " + d.err + last, Kind: "error"}, true
	case d.gone:
		return StatusLine{Text: "plan file missing" + last, Kind: "error"}, true
	}
	return StatusLine{}, false
}

// pinStatus is row 1's pin line: the pin, a warning about two active
// tasks, or why there is no pin. writable is false when p⏎ would be
// refused, so the line doesn't offer it.
func pinStatus(p *plan.Plan, writable bool) StatusLine {
	if p == nil || len(p.Tasks) == 0 {
		return StatusLine{}
	}
	g := p.Graph()
	pin := g.Pin()
	var active []string // leaves only: a parent marked active is no second pin
	for i := range p.Tasks {
		if t := &p.Tasks[i]; t.Status == plan.Active && g.Leaf(t) && g.Task(t.ID) == t {
			active = append(active, taskKey(t))
		}
	}
	if len(active) > 1 && pin != nil {
		return StatusLine{Text: fmt.Sprintf("%d active: %s · pinned %s", len(active), strings.Join(active, ", "), pin.ID), Kind: "error"}
	}
	if pin == nil {
		if done, total := g.Progress(); done == total {
			return StatusLine{Text: "no pin · all tasks done", Kind: "muted"}
		}
		return StatusLine{Text: "no pin · nothing unblocked", Kind: "muted"}
	}
	if pin.Status != plan.Active {
		if !writable {
			return StatusLine{Text: "▸ " + pin.ID + "  " + pin.Title + " (next up)", Short: "▸ " + pin.ID + " (next up)", Kind: "accent"}
		}
		return StatusLine{Text: "▸ " + pin.ID + "  " + pin.Title + " (next up · p⏎ pins it)", Short: "▸ " + pin.ID + " (next up · p⏎)", Kind: "accent"}
	}
	return StatusLine{Text: "▸ " + pin.ID + "  " + pin.Title + " (active)", Short: "▸ " + pin.ID + " (active)", Kind: "accent"}
}
