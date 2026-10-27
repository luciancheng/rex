package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/plan"
)

// planDoc is the open plan file: the last good read, and what is on disk
// now. Rex never writes it; it re-reads the file every docPollEvery and
// notices a change by content hash (docs/ux.md §6).
type planDoc struct {
	path string
	plan *plan.Plan // last good read; nil until the file has parsed once
	hash string     // the bytes on disk at the last read
	good string     // hash of the bytes behind plan

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
// and what changed in the plan when it was edited.
func (d *planDoc) read(now time.Time, first bool) (changed bool, news planNews) {
	d.polled = now
	data, err := plan.ReadFile(d.path)
	if os.IsNotExist(err) {
		changed = !d.gone
		d.gone = true
		return changed, planNews{}
	}
	if err != nil {
		// There, but not readable as a plan (a folder, a FIFO, too big, no
		// permission): say so, keep the last good copy.
		why := unreadable + cause(err)
		changed = d.err != why || d.gone
		d.err, d.gone, d.hash = why, false, ""
		return changed, planNews{}
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
		return changed, planNews{}
	}
	d.hash = h
	p, perr := plan.Parse(data)
	if perr != nil {
		d.bad, d.badPolls = perr.Error(), 1
		if first || d.plan == nil || d.err != "" {
			d.err, changed = d.bad, true
		}
		return changed, planNews{}
	}
	old, oldGood := d.plan, d.good
	d.plan, d.good, d.bad, d.badPolls, d.err = p, h, "", 0, ""
	if old == nil || h == oldGood {
		// First read, or a save that tore and came back to the same bytes:
		// nothing to announce.
		return true, planNews{}
	}
	return true, planChanges(old, p)
}

func (d *planDoc) poll(now time.Time) (bool, planNews) {
	if now.Sub(d.polled) < docPollEvery {
		return false, planNews{}
	}
	return d.read(now, false)
}

// planNews is what an edit of the plan changed: text sums it up for the
// toast and AGENT ("" when nothing was announced), and tasks says a
// task's status moved or a task came or went.
type planNews struct {
	text  string
	tasks bool
}

// planChanges sums up an edit of the file: task status moves, the plan's
// own status, then tasks added or removed, e.g. "plans-ui → done · plan →
// done · +1 task".
func planChanges(old, now *plan.Plan) planNews {
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
	if old.Status != now.Status {
		parts = append(parts, "plan → "+string(now.Status))
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
	news := planNews{text: "edited", tasks: moved+added+removed > 0}
	if len(parts) > 0 {
		news.text = strings.Join(parts, " · ")
	}
	return news
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
	atWait // no plan yet: Rex waits for the agent to write one (docs/ux.md §11)
)

// outlineView is where you are in the open plan: the selected row by
// identity ("" is the title row, else a task key), where the list's
// window starts, and the detail scroll.
type outlineView struct {
	sel    string
	view   int
	scroll int
	pos    string // where the selection is while the list scrolls with TASKS gone: the page title says it (docs/ux.md §4)
}

// taskKey identifies a task across reloads: its id, or its position when
// it has none.
func taskKey(t *plan.Task) string {
	if t.ID != "" {
		return t.ID
	}
	return "#" + strconv.Itoa(t.Index)
}

// outlineRows are the selectable rows in order: the title row, then
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

// glyph is a task's mark in the outline (docs/ux.md §5), the tag that
// goes on the right of its row and the tag's role: the agent's "pinned"
// in the pin's yellow, "next up", a parent's rollup, a block and a wait
// muted, and a problem in the plan red.
func glyph(t *plan.Task, g *plan.Graph, pin *plan.Task) (mark, tag, tagKind string) {
	if !g.Leaf(t) {
		done, total := g.Rollup(t)
		return " ", fmt.Sprintf("%d/%d", done, total), "muted"
	}
	if g.Cyclic(t) {
		return "·", "cycle", "error"
	}
	switch {
	case pin != nil && t == pin && t.Status == plan.Active:
		return "▸", "pinned", "accent"
	case pin != nil && t == pin:
		return "▸", "next up", "muted"
	case t.Status == plan.Done:
		return doneMark, "", ""
	case t.Status == plan.Skipped:
		return "–", "skipped", "muted"
	case t.Status == plan.Blocked:
		return "!", "blocked", "muted"
	case t.Status == plan.Active && pin != nil && pin.Status == plan.Active:
		return "▸", "also active", "error" // a second active task; row 1 warns
	case t.Status == plan.Active:
		// Active but never the pin (it has no id, which Problems reports),
		// and the pin isn't active: "also" would point at nothing.
		return "▸", "", ""
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

// blurb is what a task does: its does, else its description.
func blurb(t *plan.Task) string {
	if len(t.Does) > 0 {
		return strings.Join(t.Does, " · ")
	}
	return t.Description
}

// rowBlurb is a task's blurb on its outline row, flat and cut to cells:
// what a done task accomplished, else what it does (docs/ux.md §6). It
// reads only as much of the task as a row shows.
func rowBlurb(t *plan.Task, cells int) string {
	switch {
	case t.Status == plan.Done && len(t.Accomplished) > 0:
		return flatJoin(t.Accomplished, cells)
	case len(t.Does) > 0:
		return flatJoin(t.Does, cells)
	}
	return flatHead(t.Description, cells)
}

// doneRef is a done task as a card or About names it: "✔ <id>  <what it
// accomplished>".
func doneRef(t *plan.Task) string {
	return strings.TrimSpace(doneMark + " " + taskKey(t) + "  " + flat(t.DoneWhat()))
}

// filesLine is the pin's files on one row of room cells (docs/ux.md §6):
// the paths when they all fit, else their names, as many as fit, then +N;
// a first name too long for the row is cut so the +N still shows. Each is
// one pass that stops where the row does, so a pin listing 100k files
// reads only the files a row can show.
func filesLine(files []string, room int) string {
	var paths []string
	n := -2 // the cells so far; the first file has no "  " before it
	for _, f := range files {
		p := flat(f)
		if n += 2 + cellLen(p); n > room {
			break
		}
		paths = append(paths, p)
	}
	if len(paths) == len(files) {
		return strings.Join(paths, "  ")
	}
	name := func(f string) string {
		p := flat(f)
		if base := filepath.Base(p); !strings.ContainsAny(base, "*?[") {
			return base // a glob keeps its folders: "**" alone says nothing
		}
		return p
	}
	more := func(k int) string {
		if k == len(files) {
			return ""
		}
		return fmt.Sprintf("  +%d", len(files)-k)
	}
	// A prefix fits only where its names alone do: the pass stops at the
	// first name past the row, and k is the most names that fit beside
	// their +N.
	var names []string
	k, n := 0, -2
	for _, f := range files {
		nm := name(f)
		if n += 2 + cellLen(nm); n > room {
			break
		}
		if names = append(names, nm); n+cellLen(more(len(names))) <= room {
			k = len(names)
		}
	}
	if k > 0 {
		return strings.Join(names[:k], "  ") + more(k)
	}
	if len(files) == 1 {
		return name(files[0])
	}
	return ellipsis(name(files[0]), room-cellLen(more(1))) + more(1)
}

// outlineTall is the outline's body at 16 terminal rows: from there it
// keeps 5 task rows in view, not 3, and details get their blank rows
// (docs/ux.md §6, §12).
var outlineTall = layoutFor(TextOnlyBelow, 16).bare().bodyH

// cardFrom is the text width from which the pin gets its card; narrower,
// it is always the strip (docs/ux.md §6).
const cardFrom = 30

// cardTier is how much of the PINNED card the outline shows.
type cardTier int

const (
	cardFull    cardTier = iota
	cardCompact          // the edges, the title, one does row
	cardStrip            // "[PINNED] <id>  <title>", no box
	cardNone
)

// outlineFit is what of the outline's head the collapse ladder keeps
// (docs/ux.md §6).
type outlineFit struct {
	done, needs, second bool // the card's done and needs rows; the second rows of does and verify
	blanks, goal        bool // the blank rows above the card and TASKS; the goal line
	card                cardTier
	tasks               bool // the TASKS H2
	problems, readOnly  bool // the red warnings
}

// ladder is the collapse ladder, in order (docs/ux.md §6): each step
// drops one more piece of the head. The blank rows between top-level
// groups are no step: they go in only where the whole list fits.
var ladder = []func(*outlineFit){
	func(f *outlineFit) { f.done = false },
	func(f *outlineFit) { f.needs = false },
	func(f *outlineFit) { f.second = false },
	func(f *outlineFit) { f.blanks = false },
	func(f *outlineFit) { f.goal = false },
	func(f *outlineFit) { f.card = max(f.card, cardCompact) },
	func(f *outlineFit) { f.card = max(f.card, cardStrip) },
	func(f *outlineFit) { f.tasks = false },
	func(f *outlineFit) { f.card = cardNone },
	func(f *outlineFit) { f.problems = false },
	func(f *outlineFit) { f.readOnly = false },
}

// outline is an open plan laid out at one width (docs/ux.md §6). The
// pieces of its head are drawn once a layout: the ladder asks for them on
// every step.
type outline struct {
	p     *plan.Plan
	g     *plan.Graph
	pin   *plan.Task
	width int
	rows  int // task rows, one per task the cursor reaches

	goal, readOnly, problems []LogLine
	cards                    map[outlineFit][]LogLine
}

// newOutline is p laid out at width, with the goal and warning lines its
// head may show.
func newOutline(p *plan.Plan, width int) *outline {
	g := p.Graph()
	o := &outline{p: p, g: g, pin: g.Pin(), width: width, rows: len(outlineRows(p)) - 1, cards: map[outlineFit][]LogLine{}}
	if strings.TrimSpace(p.Goal) != "" {
		o.goal = []LogLine{{Kind: "muted", Text: "  " + clip(p.Goal, max(1, width-2))}}
	}
	if p.ReadOnly != "" {
		o.readOnly = warnLines("read-only: "+p.ReadOnly, width)
	}
	if n := len(p.Problems); n > 0 {
		o.problems = warnLines(plural(n, "! %d problem", "! %d problems")+" · enter on the title lists them", width)
	}
	return o
}

// wantRows is how many of n task rows the ladder keeps in view in rows
// body rows: 3, or 5 from 16 terminal rows, or all of a shorter list.
func wantRows(rows, n int) int {
	if rows >= outlineTall {
		return min(n, 5)
	}
	return min(n, 3)
}

// outlineScreen is the open plan's outline in rows body rows under its page
// title (docs/ux.md §6): the head the ladder keeps, which stays put, then
// the window of tasks around the selection, laid out to the row. It leaves
// in v where the selection is while the list scrolls with TASKS gone, for
// the page title to say (§4).
func outlineScreen(d *planDoc, v *outlineView, width, rows int) []LogLine {
	v.pos = ""
	if d.plan == nil {
		inner := max(1, width-2)
		lines := []LogLine{{Kind: "error", Text: "  " + clip("can't read this plan", inner)}}
		for _, row := range wrap(d.err, inner-2) {
			lines = append(lines, LogLine{Kind: "error", Text: "    " + clip(row, inner-2)})
		}
		if d.gone {
			lines = append(lines, LogLine{Kind: "error", Text: "  " + clip("the file is missing", inner)})
		}
		return append(lines, LogLine{Kind: "idle", Text: "  " + clip("Rex shows it once it is valid JSON", inner)})
	}
	head, list, pos := outlineLayout(d.plan, v, width, rows)
	v.pos = pos
	shown := outlineWindow(list, max(0, rows-len(head)), wantRows(rows, len(outlineRows(d.plan))-1), v)
	for i := range head {
		head[i].Head = true
	}
	return append(head, shown...)
}

// outlineWindow is what of the outline's list room rows show (docs/ux.md
// §6): the selection with its margin, from v.view, where last frame's
// window started. Where the list goes on past the window, ↑ more and
// ↓ more take the rows its want task rows leave over, ↓ more first, and
// rows of their own, never the selection's margin. With the title row
// selected the list starts at its top and ↓ more takes its last row: no
// cursor is there to keep in view.
func outlineWindow(list []LogLine, room, want int, v *outlineView) []LogLine {
	from, to, up, down := windowRange(list, room, want, v)
	more := func(text string) LogLine { return LogLine{Kind: "idle", Text: "  " + text} }
	var shown []LogLine
	if up {
		shown = append(shown, more("↑ more"))
	}
	shown = append(shown, list[from:to]...)
	if down {
		shown = append(shown, more("↓ more"))
	}
	return shown
}

// windowRange is the part of the list outlineWindow shows, list[from:to],
// and whether ↑ more and ↓ more go above and under it.
func windowRange(list []LogLine, room, want int, v *outlineView) (from, to int, up, down bool) {
	switch {
	case len(list) <= room:
		v.view = 0
		return 0, len(list), false, false
	case v.sel == "":
		v.view = 0
		if room < 2 {
			return 0, room, false, false
		}
		return 0, room - 1, false, true
	}
	marks := min(2, room-want-(len(list)-taskRows(list)))
	if marks <= 0 {
		v.view = windowStart(list, room, v.view)
		return v.view, v.view + room, false, false
	}
	// The window is laid out in the rows the markers leave; a marker the
	// list doesn't need gives its row back. v.view keeps where that window
	// starts, so the next frame finds the same one.
	s := windowStart(list, room-marks, v.view)
	v.view = s
	switch {
	case s+room-marks >= len(list): // the end is in view: only ↑ more
		return len(list) - room + 1, len(list), true, false
	case marks == 2 && s > 0:
		return s, s + room - 2, true, true
	}
	// The top is in view, or only one marker fits: ↓ more.
	return s, s + room - 1, false, true
}

// outlinePage is the row PgUp or PgDn selects (docs/ux.md §3, §6): the
// task just past the last one the outline's window shows, or just before
// its first, so a page never skips a task the window didn't show. From
// the title row, above the window, PgDn lands on the first task under it.
func outlinePage(p *plan.Plan, v outlineView, width, rows int, down bool) string {
	keys := outlineRows(p)
	at := max(0, slices.Index(keys, v.sel))
	head, list, _ := outlineLayout(p, &v, width, rows)
	from, to, _, _ := windowRange(list, max(0, rows-len(head)), wantRows(rows, len(keys)-1), &v)
	// The list's task rows are the tasks in outlineRows' order, so the
	// task rows above a line say which task it is.
	next := max(at+1, taskRows(list[:to])+1)
	if !down {
		next = min(at-1, taskRows(list[:from]))
	}
	return keys[max(0, min(len(keys)-1, next))]
}

// taskRows counts the task rows in lines: not the blank rows, nor the
// selection's blurb row under it.
func taskRows(lines []LogLine) int {
	n := 0
	for i, l := range lines {
		if l.Kind != "gap" && (!l.Selected || i == 0 || !lines[i-1].Selected) {
			n++
		}
	}
	return n
}

// outlineLayout is the outline in rows body rows under its page title
// (docs/ux.md §6): its head, which stays put, its task list, which the
// selection's window shows, and where the selection is in the list while
// it scrolls with TASKS gone. The collapse ladder fits the head. Whether
// the list scrolls and has blank rows between groups is decided without
// the selection's blurb row, so moving the cursor never changes them; the
// blurb row takes only a row the list leaves over: one it doesn't fill,
// or, while it scrolls, one past its wanted rows and both markers. The
// page title is chrome and the selected row is in the list, so neither is
// ever dropped.
func outlineLayout(p *plan.Plan, v *outlineView, width, rows int) (head, list []LogLine, pos string) {
	o := newOutline(p, width)
	sel, want := v.sel, wantRows(rows, o.rows)
	on, head := o.collapse(rows, want)
	f := o.fitFor(on)
	head = head[:min(len(head), max(0, rows))]
	room := rows - len(head)
	// The list is drawn once, with its blank rows and the selection's
	// blurb row; the layout keeps what fits of them.
	all := o.tasks(sel, f.card <= cardCompact)
	gaps := true
	if list = listOf(all, true, false); len(list) > room {
		gaps, list = false, listOf(all, false, false)
	}
	scrolls := len(list) > room
	spare := room - len(list)
	if scrolls {
		spare = room - want - 2
	}
	if spare > 0 {
		list = listOf(all, gaps, true)
	}
	h2 := false
	for i := range head {
		if head[i].Kind == "h2" {
			head[i], h2 = o.tasksH2(sel, scrolls), true
		}
	}
	if scrolls && !h2 {
		pos = o.position(sel)
	}
	return head, list, pos
}

// fitFor is the fit the ladder's steps in on leave: steps apply in the
// ladder's order, so a later step on the card outranks an earlier one.
func (o *outline) fitFor(on []bool) outlineFit {
	f := outlineFit{done: true, needs: true, second: true, blanks: true, goal: true, tasks: o.rows > 0, problems: true, readOnly: true}
	if o.width < cardFrom {
		f.card = cardStrip
	}
	for i, step := range ladder {
		if on[i] {
			step(&f)
		}
	}
	return f
}

// collapse runs the collapse ladder (docs/ux.md §6) for a head over want
// task rows in rows body rows: it takes the steps in order while the head
// doesn't fit, then walks back over them, last first, and gives back each
// piece that fits again now that a bigger step made room. It returns the
// steps it kept and the head they leave. With no task rows the warnings
// stay: the screen keeps what of them fits.
func (o *outline) collapse(rows, want int) ([]bool, []LogLine) {
	on := make([]bool, len(ladder))
	fits := func(head []LogLine) bool { return len(head)+want <= rows }
	head := o.head(o.fitFor(on))
	for i := range ladder {
		if fits(head) || (i >= len(ladder)-2 && o.rows == 0) {
			break
		}
		on[i] = true
		head = o.head(o.fitFor(on))
	}
	for i := len(ladder) - 1; i >= 0; i-- {
		if !on[i] {
			continue
		}
		on[i] = false
		if back := o.head(o.fitFor(on)); fits(back) {
			head = back
		} else {
			on[i] = true
		}
	}
	return on, head
}

// head is the outline's head for f: the goal line, the warnings, the
// PINNED card (or what stands in for it) and the TASKS H2, with blank rows
// above the card and TASKS. With no goal the card's blank row is the one
// under the page title, as About and task detail have.
func (o *outline) head(f outlineFit) []LogLine {
	var head []LogLine
	if f.goal {
		head = append(head, o.goal...)
	}
	if f.readOnly {
		head = append(head, o.readOnly...)
	}
	if f.problems {
		head = append(head, o.problems...)
	}
	card := o.card(f)
	if f.blanks && len(card) > 0 {
		head = append(head, LogLine{Kind: "gap"})
	}
	head = append(head, card...)
	if f.tasks {
		if f.blanks {
			head = append(head, LogLine{Kind: "gap"})
		}
		head = append(head, o.tasksH2("", false))
	}
	return head
}

// warnLines is a red line under the goal, its second row hanging under
// the first; two rows at most.
func warnLines(text string, width int) []LogLine {
	row, rest := cutRow(strings.TrimSpace(flat(text)), max(1, width-2))
	lines := []LogLine{{Kind: "error", Text: "  " + row}}
	if rest != "" {
		lines = append(lines, LogLine{Kind: "error", Text: "    " + ellipsis(rest, max(1, width-4))})
	}
	return lines
}

// tasksH2 heads the list: TASKS with the plan's progress, or, while the
// list scrolls, where the selection is in it (docs/ux.md §6).
func (o *outline) tasksH2(sel string, scrolls bool) LogLine {
	done, total := o.g.Progress()
	meta := fmt.Sprintf("%d/%d done", done, total)
	if o.width < cardFrom {
		meta = fmt.Sprintf("%d/%d", done, total)
	}
	if at := o.position(sel); scrolls && at != "" {
		meta = at
	}
	h := h2Line("TASKS", meta, o.width)
	h.Kind = "h2"
	return h
}

// position is where the selection is in the list, "3 of 8"; "" on the
// title row.
func (o *outline) position(sel string) string {
	if at := slices.Index(outlineRows(o.p), sel); at > 0 {
		return fmt.Sprintf("%d of %d", at, o.rows)
	}
	return ""
}

// bareCard is the muted row inside a PINNED card with no fields yet, as
// an agent's first save has it, and what it shrinks to (docs/ux.md §6).
var bareCard = []string{"no does, verify or files yet · the agent adds them", "no does, verify or files yet", "no details yet"}

// card is the PINNED card (docs/ux.md §6), the send-off kept in the plan:
// what a fresh chat needs for the pin, read from the file. It is display
// only; the agent reads the same file. With no pin, one quiet line says
// why.
func (o *outline) card(f outlineFit) []LogLine {
	if f.card == cardNone {
		return nil
	}
	key := outlineFit{card: f.card, done: f.done, needs: f.needs, second: f.second}
	lines, ok := o.cards[key]
	if !ok {
		lines = o.drawCard(f)
		o.cards[key] = lines
	}
	return lines
}

func (o *outline) drawCard(f outlineFit) []LogLine {
	pin, w := o.pin, o.width
	if pin == nil {
		text := "no pin · nothing unblocked"
		if done, total := o.g.Progress(); len(o.p.Tasks) == 0 {
			text = "No tasks yet · your agent fills this file"
		} else if done == total {
			text = doneMark + " all tasks done"
		}
		var lines []LogLine
		for _, row := range wrapMost(text, max(1, w-2), 2) {
			lines = append(lines, LogLine{Kind: "muted", Text: "  " + row})
		}
		return lines
	}
	// The agent's pin is yellow, edge to edge; a pin Rex worked out is not
	// the agent's and is never sent, so its box is slate (docs/ux.md §5,
	// §7).
	edgeRole, badge, id := "accent", pill("PINNED", "accent"), Span{Text: flat(pin.ID), Role: "accent", Bold: true}
	if pin.Status != plan.Active {
		edgeRole, badge, id = "border", pill("NEXT UP", "muted"), Span{Text: flat(pin.ID), Bold: true}
	}
	title := strings.TrimSpace(flat(pin.Title))
	if f.card == cardStrip {
		spans := append(append([]Span{}, badge...), Span{Text: " "}, id)
		if room := w - spansLen(spans) - 2; title != "" && room >= pinTitleMin {
			spans = append(spans, Span{Text: "  " + ellipsis(title, room)})
		}
		return []LogLine{spanLine("text", cutSpans(spans, w)...)}
	}
	edge := func(s string) Span { return Span{Text: s, Role: edgeRole} }
	lead := append(append([]Span{edge("╭─")}, badge...), edge("─ "))
	room := max(1, w-spansLen(lead)-3)
	// From 60 cells the title joins the edge where pinTitleMin cells of it
	// fit; else it is the first row inside, so a long id never cuts it away.
	inEdge := w >= progressWide && title != "" && room-cellLen(id.Text)-3 >= pinTitleMin
	label := []Span{id}
	if inEdge {
		label = append(label, Span{Text: " · "}, Span{Text: title, Role: "title", Bold: true})
	}
	label = cutSpans(label, room)
	top := append(append(lead, label...), Span{Text: " "}, edge(strings.Repeat("─", max(1, w-spansLen(lead)-spansLen(label)-2))+"╮"))
	lines := []LogLine{spanLine("text", top...)}
	inside := func(spans ...Span) {
		lines = append(lines, boxed(spanLine("text", append([]Span{{Text: "  "}}, spans...)...), w, edgeRole))
	}
	if !inEdge && title != "" {
		inside(Span{Text: title, Role: "title", Bold: true})
	}
	empty := len(lines)
	for _, fl := range o.cardFields(f) {
		for _, l := range fieldLines(fl, w-2, 8) {
			lines = append(lines, boxed(l, w, edgeRole))
		}
	}
	if len(lines) == empty {
		text := bareCard[len(bareCard)-1]
		for _, t := range bareCard {
			if cellLen(t) <= w-4 {
				text = t
				break
			}
		}
		inside(Span{Text: text, Role: "muted"})
	}
	return append(lines, spanLine("text", edge("╰"+strings.Repeat("─", max(0, w-2))+"╯")))
}

// cardFields are the PINNED card's fields for f (docs/ux.md §6): does,
// needs, verify, files and done, each held to its rows, verify ending in
// +N for the items it leaves out. The compact card has one row: what the
// pin does, else the first row of its first other field.
func (o *outline) cardFields(f outlineFit) []h3 {
	pin, full := o.pin, f.card == cardFull
	room := o.width - 12 // a value's cells: the box's edges and margins, and the label column
	rows := 2
	if !full || !f.second {
		rows = 1
	}
	var fields []h3
	if does := flat(blurb(pin)); strings.TrimSpace(does) != "" {
		fields = append(fields, h3{label: "does", items: []string{does}, rows: rows})
	}
	if len(pin.Needs) > 0 && (f.needs || !full) {
		fields = append(fields, h3{label: "needs", items: []string{strings.Join(pin.Needs, " · ")}, rows: 1})
	}
	if len(pin.Verify) > 0 {
		verify := slices.Clone(pin.Verify[:min(len(pin.Verify), rows)])
		if n := len(pin.Verify) - len(verify); n > 0 {
			more := fmt.Sprintf("  +%d", n)
			last := len(verify) - 1
			verify[last] = ellipsis(flat(verify[last]), room-cellLen(more)) + more
		}
		fields = append(fields, h3{label: "verify", items: verify, bare: true, rows: 1})
	}
	if len(pin.Files) > 0 {
		fields = append(fields, h3{label: "files", items: []string{filesLine(pin.Files, room)}, rows: 1})
	}
	if t := o.g.BuildsOn(pin); t != nil && (f.done || !full) {
		fields = append(fields, h3{label: "done", items: []string{doneRef(t)}, role: "muted", rows: 1})
	}
	if !full && len(fields) > 1 {
		fields = fields[:1]
	}
	if !full && len(fields) == 1 {
		fields[0].items = fields[0].items[:1]
	}
	return fields
}

// boxed puts a field line inside the PINNED card: its 2-cell lead becomes
// the card's left edge, and the right edge closes it at width.
func boxed(l LogLine, width int, edge string) LogLine {
	spans := append([]Span{{Text: "│ ", Role: edge}}, l.Spans[1:]...)
	return spanLine("text", append(padSpans(cutSpans(spans, width-2), width-2), Span{Text: " │", Role: edge})...)
}

// tasks is the outline's list (docs/ux.md §6): a row per task in file
// order, parent as indent. The agent's pin's ▸, id and tag are the only
// yellow on it; parents are bold with a muted rollup; done rows are
// quiet, with what they accomplished as their blurb from 60 cells. The
// selection gets ❯ and purple text and keeps the pin's yellow; under 60
// cells it gets its blurb on a row of its own, except the pin while the
// card shows what it does. A blank row goes between top-level groups.
// listOf takes the blank rows and the blurb row out where the layout has
// no room for them.
func (o *outline) tasks(sel string, cardDoes bool) []LogLine {
	p, g, pin := o.p, o.g, o.pin
	inner := max(1, o.width-2)
	idW := 0
	for i := range p.Tasks {
		idW = max(idW, cellLen(taskKey(&p.Tasks[i])))
	}
	wide := o.width >= progressWide
	gap := "  "
	if !wide {
		gap = " "
	}
	idW = min(idW, 16, max(6, inner-14)) // leave the title at least ~10 cells
	// Titles line up across indent levels.
	depths := make([]int, len(p.Tasks))
	indents := make([]string, len(p.Tasks))
	headW := 0
	for i := range p.Tasks {
		t := &p.Tasks[i]
		depths[i] = g.Depth(t)
		indents[i] = strings.Repeat("  ", min(depths[i], 2))
		if depths[i] > 2 {
			indents[i] += "↳ "
		}
		headW = max(headW, cellLen(indents[i])+2+min(cellLen(taskKey(t)), idW))
	}
	var lines []LogLine
	first, group := true, false
	seen := map[string]bool{}
	for i := range p.Tasks {
		t := &p.Tasks[i]
		key := taskKey(t)
		if seen[key] {
			continue // a repeated id: only the first is reachable
		}
		seen[key] = true
		parent := !g.Leaf(t)
		if depths[i] == 0 {
			if !first && (parent || group) {
				lines = append(lines, LogLine{Kind: "gap"})
			}
			first, group = false, parent
		}
		indent := indents[i]
		mark, tag, tagKind := glyph(t, g, pin)
		// A block needs you as a problem does, so its tag shows at every
		// width too, though only a problem is red.
		needs := tagKind == "error" || t.Status == plan.Blocked && !parent
		if !wide && !needs && !parent {
			tag = "" // narrow rows keep the title; the glyph still says it, and a parent keeps its rollup
		}
		// A row shows at most inner cells of its title and blurb: one cell
		// more is enough for cutSpans and ellipsis to cut them as they would
		// the whole text.
		name := flatHead(t.Title, inner+1)
		b := rowBlurb(t, inner+1)
		if name == "" {
			name = "(untitled)"
		}
		id := key
		if cellLen(id) > idW {
			id = cutCells(id, idW-1) + "…"
		}
		kind := "text"
		head := Span{Text: mark + " " + id}
		switch {
		case parent:
			head.Bold = true
		case t == pin && t.Status == plan.Active:
			head.Role, head.Bold = "accent", true
		case t == pin:
			head.Bold = true
		case plan.Finished(t.Status):
			kind = "muted"
		}
		if tag != "" && tagKind == "error" {
			kind = "error"
		}
		left := []Span{{Text: indent}, head, {Text: strings.Repeat(" ", max(0, headW-cellLen(indent)-cellLen(head.Text))) + gap}, {Text: name, Bold: parent}}
		if wide && b != "" {
			left = append(left, Span{Text: " · " + b})
		}
		row := cutSpans(left, inner)
		// A red tag needs you, so it shows at every width, down to 6 cells
		// of the row's left side (docs/ux.md §6, Rows).
		if tag != "" && (inner >= cellLen(tag)+12 || needs && inner >= cellLen(tag)+8) {
			row = sideSpans(left, Span{Text: tag, Role: tagKind}, inner)
		}
		lead := Span{Text: "  "}
		selected := key == sel
		if selected {
			kind, lead, row = "pick", Span{Text: selMark}, picked(row)
		}
		line := spanLine(kind, append([]Span{lead}, row...)...)
		line.Selected = selected
		lines = append(lines, line)
		if selected && !wide && b != "" && !(t == pin && cardDoes) {
			lines = append(lines, LogLine{Kind: "pick", Selected: true, Text: "  " + ellipsis(indent+"    "+b, inner)})
		}
	}
	return lines
}

// listOf is the outline's list from what tasks drew: with its blank rows
// only where gaps, and the selection's blurb row only where blurb.
func listOf(lines []LogLine, gaps, blurb bool) []LogLine {
	out := make([]LogLine, 0, len(lines))
	for i, l := range lines {
		switch {
		case l.Kind == "gap" && !gaps:
		case l.Selected && i > 0 && lines[i-1].Selected && !blurb:
		default:
			out = append(out, l)
		}
	}
	return out
}

// detailBody lays out a detail screen (docs/ux.md §6): lines on top, then
// each section under its H2, blank rows from 16 rows.
type detailBody struct {
	lines []LogLine
	width int
	tall  bool
}

func (b *detailBody) gap() {
	if b.tall {
		b.lines = append(b.lines, LogLine{Kind: "gap"})
	}
}

// section adds an H2 and what it heads; nothing when that is empty.
func (b *detailBody) section(label, meta string, body []LogLine) {
	if len(body) == 0 {
		return
	}
	b.gap()
	b.lines = append(b.lines, h2Line(label, meta, b.width))
	b.lines = append(b.lines, body...)
}

// fields is a section of H3 fields in one label column.
func (b *detailBody) fields(col int, fields ...h3) []LogLine {
	var lines []LogLine
	for _, f := range fields {
		lines = append(lines, fieldLines(f, b.width, col)...)
	}
	return lines
}

// para is text under an H2, with a 2-cell margin on either side.
func (b *detailBody) para(f h3) []LogLine {
	return fieldLines(f, b.width-2, 0)
}

// title is a bright bold title, wrapped.
func (b *detailBody) title(text string) {
	for _, row := range wrapMost(flat(text), max(1, b.width-2), 0) {
		b.lines = append(b.lines, spanLine("text", Span{Text: "  "}, Span{Text: row, Role: "title", Bold: true}))
	}
}

func nonEmpty(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []string{s}
}

// barCells is About's progress bar from 60 cells (docs/ux.md §6).
const barCells = 20

// planDetail is About (docs/ux.md §6), under PLANS › title › about: the
// title, the status and progress, the path, then GOAL, SCOPE, STACK and
// DONE, every done task and what it accomplished, which is the send-off's
// Done section kept in the plan; then PROBLEMS and READ-ONLY.
func planDetail(d *planDoc, rel string, width int, tall bool) []LogLine {
	p := d.plan
	if p == nil {
		return []LogLine{{Kind: "error", Text: "  " + clip(d.err, width-2)}}
	}
	g := p.Graph()
	b := &detailBody{width: width, tall: tall}
	b.gap()
	b.title(planTitle(p, d.path))
	done, total := g.Progress()
	sum := fmt.Sprintf("%d of %d done", done, total)
	if total == 0 {
		sum = "no tasks yet"
	}
	if p.Status != "" {
		sum = string(p.Status) + " · " + sum
	}
	status := []Span{{Text: "  "}, {Text: sum, Role: "muted"}}
	if width >= progressWide && total > 0 {
		n := (barCells*done + total/2) / total
		status = append(status, Span{Text: "  "}, Span{Text: strings.Repeat("━", n)}, Span{Text: strings.Repeat("─", barCells-n), Role: "border"})
	}
	b.lines = append(b.lines, spanLine("text", status...), LogLine{Kind: "muted", Text: "  " + clip(rel, max(1, width-2))})
	b.section("GOAL", "", b.para(h3{items: nonEmpty(p.Goal)}))
	// in and out read as a pair, in their own narrow column, bulleted.
	b.section("SCOPE", "", b.fields(5, h3{label: "in", items: p.Scope.In, list: true}, h3{label: "out", items: p.Scope.Out, list: true}))
	b.section("STACK", "", b.fields(8, h3{label: "base", items: nonEmpty(p.Base)}))
	var dones []string
	skipped := 0
	for i := range p.Tasks {
		t := &p.Tasks[i]
		if !g.Leaf(t) || findTask(p, taskKey(t)) != t {
			continue
		}
		switch t.Status {
		case plan.Done:
			dones = append(dones, doneRef(t))
		case plan.Skipped:
			skipped++
		}
	}
	// The status line counts skipped tasks as finished, as TASKS and row 1
	// do; DONE's count says how many of them were skipped.
	meta := fmt.Sprint(len(dones))
	if skipped > 0 {
		meta += fmt.Sprintf(" · %d skipped", skipped)
	}
	b.section("DONE", meta, b.fields(0, h3{items: dones, role: "muted", bare: true, rows: 1}))
	var problems []string
	for _, pr := range p.Problems {
		problems = append(problems, pr.String())
	}
	b.section("PROBLEMS", fmt.Sprint(len(problems)), b.para(h3{items: problems, role: "error"}))
	b.section("READ-ONLY", "", b.para(h3{items: nonEmpty(p.ReadOnly), role: "error"}))
	if p.Goal == "" && len(p.Scope.In) == 0 && len(p.Scope.Out) == 0 && len(problems) == 0 {
		b.gap()
		b.lines = append(b.lines, LogLine{Kind: "idle", Text: "  " + clip("No goal or scope yet · your agent fills this file", width-2)})
	}
	return b.lines
}

// taskState is a task detail's status line (docs/ux.md §6): the pin's
// badge and status, else its glyph and state, and the parent it is under.
func taskState(t *plan.Task, g *plan.Graph, pin *plan.Task) []Span {
	spans := []Span{{Text: "  "}}
	switch mark, tag, kind := glyph(t, g, pin); {
	case t == pin && t.Status == plan.Active:
		spans = append(append(spans, pill("PINNED", "accent")...), Span{Text: " active", Role: "muted"})
	case t == pin:
		spans = append(append(spans, pill("NEXT UP", "muted")...), Span{Text: " " + string(t.Status), Role: "muted"})
	case !g.Leaf(t):
		spans = append(spans, Span{Text: tag + " done", Role: "muted"})
	default:
		words := map[string]string{doneMark: "done", "–": "skipped", "!": "blocked", "▸": "active", "○": "ready"}[mark]
		switch {
		case tag == "cycle":
			words = "cycle"
		case tag == "also active":
			words = "active · also active"
		case mark == "·":
			words = "waiting · " + tag
		}
		if kind != "error" {
			kind = "muted"
		}
		spans = append(spans, Span{Text: mark + " " + words, Role: kind})
	}
	if parent := g.Task(t.Parent); parent != nil {
		spans = append(spans, Span{Text: " · under " + taskKey(parent), Role: "muted"})
	}
	return spans
}

// taskDetail is a task under its page title (PLANS › title › id, docs/ux.md
// §6): its status line and title, then DOES, VERIFY, FILES, CONTEXT and
// DESCRIPTION, and SO FAR when it has completion fields; a done task leads
// with DONE, what it did, then INTENDED.
func taskDetail(d *planDoc, key string, width int, tall bool) []LogLine {
	p := d.plan
	var t *plan.Task
	if p != nil {
		t = findTask(p, key)
	}
	if t == nil {
		return []LogLine{{Kind: "idle", Text: "  " + clip("this task is no longer in the file", width-2)}}
	}
	g := p.Graph()
	pin := g.Pin()
	ref := func(r *plan.Task) string {
		mark, _, _ := glyph(r, g, pin)
		what := r.Title
		if r.Status == plan.Done {
			what = r.DoneWhat()
		}
		return strings.TrimSpace(mark + " " + taskKey(r) + "  " + flat(what))
	}
	var after, then, kids []string
	for _, id := range t.DependsOn {
		if dep := g.Task(id); dep != nil {
			after = append(after, ref(dep))
		} else {
			after = append(after, "? "+id+"  no such task")
		}
	}
	for _, dt := range g.Dependents(t.ID) {
		then = append(then, ref(dt))
	}
	for _, c := range g.Children(t.ID) {
		kids = append(kids, ref(c))
	}
	under := []string(nil)
	if parent := g.Task(t.Parent); parent != nil {
		under = []string{strings.TrimSpace(taskKey(parent) + "  " + flat(parent.Title))}
	} else if t.Parent != "" {
		under = []string{"?" + t.Parent + "  no such task"}
	}
	context := []h3{
		{label: "needs", items: t.Needs, rows: 2},
		{label: "after", items: after, role: "muted", bare: true, rows: 2},
		{label: "then", items: then, role: "muted", bare: true, rows: 2},
		{label: "under", items: under, role: "muted", rows: 2},
		{label: "tasks", items: kids, role: "muted", bare: true, rows: 2},
		{label: "branch", items: nonEmpty(t.Branch), rows: 2},
		{label: "in", items: t.Scope.In, list: len(t.Scope.Out) > 1, rows: 2},
		{label: "out", items: t.Scope.Out, list: len(t.Scope.In) > 1, rows: 2},
	}
	did := []h3{
		{label: "did", items: t.Accomplished},
		{label: "changed", items: t.Modified},
		{label: "checked", items: t.VerifiedHow},
		{label: "notes", items: nonEmpty(t.CompletedNotes)},
	}
	intended := []h3{{label: "does", items: t.Does}, {label: "verify", items: t.Verify}, {label: "files", items: t.Files}}
	done := t.Status == plan.Done
	col := h3Col(context...)
	if done || len(t.Accomplished)+len(t.Modified)+len(t.VerifiedHow) > 0 || t.CompletedNotes != "" {
		col = h3Col(append(append(context, did...), intended...)...)
	}
	b := &detailBody{width: width, tall: tall}
	b.gap()
	b.lines = append(b.lines, spanLine("muted", taskState(t, g, pin)...))
	if strings.TrimSpace(t.Title) != "" {
		b.title(t.Title)
	}
	if done {
		b.section("DONE", "", b.fields(col, did...))
		b.section("INTENDED", "", b.fields(col, intended...))
	} else {
		b.section("DOES", "", b.para(h3{items: t.Does}))
		b.section("VERIFY", countOf(t.Verify), b.para(h3{items: t.Verify}))
		b.section("FILES", countOf(t.Files), b.fields(0, h3{items: t.Files, bare: true}))
	}
	b.section("CONTEXT", "", b.fields(col, context...))
	b.section("DESCRIPTION", "", b.para(h3{items: nonEmpty(t.Description)}))
	if !done {
		// Completion fields the agent wrote before it set done: so far,
		// not done.
		b.section("SO FAR", "", b.fields(col, did...))
	}
	return b.lines
}

// countOf is a section's count meta: "" for none.
func countOf(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return fmt.Sprint(len(items))
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
		return StatusLine{Text: "plan error: " + strings.TrimPrefix(d.err, unreadable) + last, Kind: "error"}, true
	case d.gone:
		return StatusLine{Text: "plan file missing" + last, Kind: "error"}, true
	}
	return StatusLine{}, false
}

// pinStatus is row 1's pin line: the pin, a warning about two active
// tasks, or why there is no pin. The agent writes the pin; Rex only shows
// it (docs/ux.md §7).
func pinStatus(p *plan.Plan) StatusLine {
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
	// The agent's active task needs no word and is the pin's yellow; a pin
	// Rex worked out says "(next up)" and is not (docs/ux.md §4, §5, §7).
	mark, tail, role := "▸ "+flat(pin.ID), "", "accent"
	if pin.Status != plan.Active {
		tail, role = " (next up)", "text"
	}
	title := flat(pin.Title)
	if strings.TrimSpace(title) == "" {
		return StatusLine{Text: mark + tail, Kind: "text", Spans: func(text string) []Span {
			return []Span{{Text: mark, Role: role, Bold: true}, {Text: strings.TrimPrefix(text, mark), Role: "muted"}}
		}}
	}
	return StatusLine{
		Text:  mark + "  " + title + tail,
		Short: mark + tail,
		Kind:  "text",
		Clip:  true,
		Fit: func(w int) string {
			room := w - cellLen(mark+"  "+tail)
			if room < pinTitleMin {
				return mark + tail
			}
			return mark + "  " + ellipsis(title, room) + tail
		},
		Spans: func(text string) []Span {
			rest, ok := strings.CutPrefix(text, mark)
			if !ok {
				return nil
			}
			spans := []Span{{Text: mark, Role: role, Bold: true}}
			if body, ok := strings.CutSuffix(rest, tail); ok && tail != "" {
				return append(spans, Span{Text: body}, Span{Text: tail, Role: "muted"})
			}
			return append(spans, Span{Text: rest})
		},
	}
}

// pinTitleMin is the fewest title cells row 1's pin line keeps; with
// fewer, it is just the pin's id.
const pinTitleMin = 8

// progress is the open plan's progress for row 1: "1/3", or "" before it
// has tasks.
func progress(p *plan.Plan) string {
	if p == nil {
		return ""
	}
	done, total := p.Graph().Progress()
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", done, total)
}
