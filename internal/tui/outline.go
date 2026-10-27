package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// outlineView is where you are in the open plan: the selected task by
// identity (a task key; "" only while no task can be selected), the
// first line of the task list's window, and a page's scroll.
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

func findTask(p *plan.Plan, key string) *plan.Task {
	for i := range p.Tasks {
		if taskKey(&p.Tasks[i]) == key {
			return &p.Tasks[i]
		}
	}
	return nil
}

// taskKeys are the tasks the cursor reaches, in the list's order
// (docs/ux.md §6): every leaf, and every task in a parent loop, in file
// order, a repeated id only once. Other parents are group labels or
// nothing, so the cursor never lands on one. Callers never change it.
func taskKeys(p *plan.Plan) []string { return planIndexOf(p).keys }

// defaultSel is where the cursor lands on a plan (docs/ux.md §6): the
// pin, else the first task, else nowhere.
func defaultSel(p *plan.Plan) string {
	if p == nil {
		return ""
	}
	x := planIndexOf(p)
	switch {
	case x.pin != nil:
		return taskKey(x.pin)
	case len(x.keys) > 0:
		return x.keys[0]
	}
	return ""
}

// keepSel keeps the selection on the same task after a reload; if it is
// gone, or there was none (a plan that gains its first tasks), it lands
// on the pin, as an open does (docs/ux.md §1).
func (v *outlineView) keepSel(p *plan.Plan) {
	// (Task detail keeps its task even when it leaves the file, so the
	// screen can say so instead of showing another task.)
	if p == nil || v.sel != "" && slices.Contains(taskKeys(p), v.sel) {
		return
	}
	v.sel = defaultSel(p)
}

func (v *outlineView) move(p *plan.Plan, delta int) {
	keys := taskKeys(p)
	if len(keys) == 0 {
		return
	}
	at := max(0, slices.Index(keys, v.sel))
	v.sel = keys[max(0, min(len(keys)-1, at+delta))]
}

func (v *outlineView) edge(p *plan.Plan, last bool) {
	keys := taskKeys(p)
	switch {
	case len(keys) == 0:
	case last:
		v.sel = keys[len(keys)-1]
	default:
		v.sel = keys[0]
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

// glyph is a task's mark and tag as the plan list's accordion draws them
// (docs/ux.md §11): the pin's ▸, ○ ready, · waiting, ✔ done, – skipped,
// ! blocked. The task list has its own quieter marks (taskList.draw).
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
	case t.Status == plan.Active:
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

// listLine is one line of the task list before it is drawn: a task's
// row, a group's label, or (t nil) the blank row above a group.
type listLine struct {
	t      *plan.Task
	group  bool // t is a top-level parent: its label
	inside bool // a task in a group, 2 cells in
}

// planIndex is what the task list needs of one read of a plan, worked out
// once (docs/ux.md §6): its graph and pin; the list's lines without and
// with group labels; the keys the cursor reaches; the id column; and the
// tally row 0 and About count, done and finished (done or skipped) of the
// tasks the list shows. Each task's depth is worked out once, so a deep
// chain of parents costs its length, not its length squared, and a key
// or a frame never walks it again.
type planIndex struct {
	p              *plan.Plan
	g              *plan.Graph
	pin            *plan.Task
	lines          [2][]listLine // [0] without labels, [1] with them
	keys           []string
	idCol          int
	done, finished int
}

// indexCache keeps the index of the plan read last: the open plan's, read
// once a poll.
var indexCache struct {
	sync.Mutex
	x *planIndex
}

// planIndexOf is p's index, from the cache while p is the plan read last.
func planIndexOf(p *plan.Plan) *planIndex {
	indexCache.Lock()
	defer indexCache.Unlock()
	if x := indexCache.x; x != nil && x.p == p {
		return x
	}
	x := newIndex(p)
	indexCache.x = x
	return x
}

// newIndex lays out p's list (docs/ux.md §6): a row per leaf in file
// order, a task under a parent indented 2. A top-level parent is a group
// label, with a blank row above it and one before the ungrouped tasks
// that follow it; deeper parents have no line of their own, and only
// details name them. A task in a parent loop has no top to sit under: it
// is a row of its own at the top, tagged cycle. Without labels (under 12
// rows) the indent alone keeps the grouping.
func newIndex(p *plan.Plan) *planIndex {
	g := p.Graph()
	x := &planIndex{p: p, g: g, pin: g.Pin()}
	depth, loop := depths(p, g)
	seen := map[string]bool{}
	first, group := true, false
	for i := range p.Tasks {
		t := &p.Tasks[i]
		key := taskKey(t)
		if seen[key] {
			continue // a repeated id: only the first is reachable
		}
		seen[key] = true
		row, top := g.Leaf(t) || loop[i], depth[i] == 0
		if top {
			if !first && (!row || group) {
				x.lines[1] = append(x.lines[1], listLine{})
			}
			first, group = false, !row
		}
		switch {
		case row:
			ln := listLine{t: t, inside: !top}
			x.lines[0], x.lines[1] = append(x.lines[0], ln), append(x.lines[1], ln)
			x.keys = append(x.keys, key)
			n := cellLen(flatHead(key, idMax+1)) + 2
			if !top {
				n += 2
			}
			x.idCol = max(x.idCol, n)
			if t.Status == plan.Done {
				x.done++
			}
			if plan.Finished(t.Status) {
				x.finished++
			}
		case top:
			x.lines[1] = append(x.lines[1], listLine{t: t, group: true})
		}
	}
	x.idCol = max(idMin, min(idMax, x.idCol))
	return x
}

// depths is how many parents sit above each task, by its position in
// p.Tasks, and whether it is in a parent loop (a loop's tasks count as
// the top). Each task is walked once: a walk up stops at the first task
// it already knows.
func depths(p *plan.Plan, g *plan.Graph) (depth []int, loop []bool) {
	n := len(p.Tasks)
	at := make(map[*plan.Task]int, n)
	for i := range p.Tasks {
		at[&p.Tasks[i]] = i
	}
	parent := func(i int) int {
		if q := g.Task(p.Tasks[i].Parent); q != nil {
			return at[q]
		}
		return -1
	}
	depth, loop = make([]int, n), make([]bool, n)
	const (
		unseen = iota
		walking
		known
	)
	state := make([]int, n)
	for i := range p.Tasks {
		var path []int
		j := i
		for ; j >= 0 && state[j] == unseen; j = parent(j) {
			state[j] = walking
			path = append(path, j)
		}
		d, k := 0, len(path)
		switch {
		case j >= 0 && state[j] == walking: // back on its own path: a loop
			k = slices.Index(path, j)
			for _, l := range path[k:] {
				loop[l], state[l] = true, known
			}
			d = 1
		case j >= 0:
			d = depth[j] + 1
		}
		for l := k - 1; l >= 0; l-- {
			depth[path[l]], state[path[l]] = d, known
			d++
		}
	}
	return depth, loop
}

// taskList is the open plan's list laid out at one width. Its lines are
// drawn only once they are on screen, and a row reads only a row's cells
// of a task's text, so a frame costs the rows it shows, not the plan.
type taskList struct {
	g     *plan.Graph
	pin   *plan.Task
	lines []listLine
	width int
	idCol int // the id column, its indent included: the longest + 2, 8 to 18 cells
}

// idMin and idMax bound the list's id column (docs/ux.md §6).
const (
	idMin = 8
	idMax = 18
)

// narrowList is the text width under which a task row is its id and its
// tag (docs/ux.md §6).
const narrowList = 40

// newTaskList is p's task list at width, in rows body rows: its blank
// rows between groups only where the whole list fits with them, which the
// selection never decides, so nothing moves with the cursor.
func newTaskList(p *plan.Plan, width, rows int, labels bool) *taskList {
	x := planIndexOf(p)
	lines := x.lines[0]
	if labels {
		lines = x.lines[1]
	}
	if len(lines) > rows {
		kept := make([]listLine, 0, len(lines))
		for _, ln := range lines {
			if ln.t != nil {
				kept = append(kept, ln)
			}
		}
		lines = kept
	}
	return &taskList{g: x.g, pin: x.pin, lines: lines, width: width, idCol: x.idCol}
}

// index is the line of the task key names, or -1.
func (l *taskList) index(key string) int {
	for i, ln := range l.lines {
		if ln.t != nil && !ln.group && taskKey(ln.t) == key {
			return i
		}
	}
	return -1
}

// task reports a line the cursor reaches.
func (l *taskList) task(i int) bool { return l.lines[i].t != nil && !l.lines[i].group }

// tag is a task row's one tag (docs/ux.md §6): the agent's pin, in the
// pin's yellow; what Rex would pin, muted; and in red a problem in the
// plan, a second active task or a task in a loop.
func (l *taskList) tag(t *plan.Task) (string, string) {
	pin := l.pin
	switch {
	case l.g.Cyclic(t):
		return "cycle", "error"
	case t == pin && t.Status == plan.Active:
		return "pinned", "accent"
	case t == pin:
		return "next up", "muted"
	case t.Status == plan.Active && pin != nil && pin.Status == plan.Active:
		return "also active", "error"
	}
	return "", ""
}

// taskMark is a task's glyph on the list (docs/ux.md §5): ✔ done, –
// skipped, ! blocked, and blank for the rest.
func taskMark(s plan.Status) string {
	switch s {
	case plan.Done:
		return doneMark
	case plan.Skipped:
		return "–"
	case plan.Blocked:
		return "!"
	}
	return " "
}

// draw is line i as the screen shows it (docs/ux.md §6). A task row is
// the gutter, its glyph, its id in the id column, its title, and its tag
// at the right edge, a cell short of it; done and skipped rows are muted.
// Titles clip first, then ids; the tag stays. Under narrowList cells a
// row is its id and tag. The selected row is ❯ and purple, keeping its
// tag's color and ✔ its green. A group label is its parent's title, bold,
// with the group's progress muted on the right.
func (l *taskList) draw(i int, selected bool) LogLine {
	ln := l.lines[i]
	inner := max(1, l.width-3)
	if ln.t == nil {
		return LogLine{Kind: "gap"}
	}
	t := ln.t
	if ln.group {
		done, total := l.g.Rollup(t)
		name := flatHead(t.Title, inner+1)
		if strings.TrimSpace(name) == "" {
			name = flatHead(taskKey(t), inner+1)
		}
		row := sideSpans([]Span{{Text: name, Role: "text", Bold: true}}, Span{Text: fmt.Sprintf("%d/%d", done, total), Role: "muted"}, inner)
		return spanLine("text", append([]Span{{Text: "  "}}, row...)...)
	}
	kind := "text"
	if plan.Finished(t.Status) {
		kind = "muted"
	}
	indent := ""
	if ln.inside {
		indent = "  "
	}
	head := indent + taskMark(t.Status) + " "
	id := flatHead(taskKey(t), inner+1)
	left := []Span{{Text: head + id}}
	if l.width >= narrowList {
		if room := l.idCol - cellLen(indent) - 2; cellLen(id) > room {
			id = ellipsis(id, room)
		}
		name := flatHead(t.Title, inner+1)
		if strings.TrimSpace(name) == "" {
			name = "(untitled)"
		}
		left = []Span{{Text: head + padRight(id, l.idCol-cellLen(indent))}, {Text: name}}
	}
	row := cutSpans(left, inner)
	if tag, role := l.tag(t); tag != "" {
		row = sideSpans(left, Span{Text: tag, Role: role}, inner)
	}
	lead := Span{Text: "  "}
	if selected {
		kind, lead, row = "pick", Span{Text: selMark}, picked(row)
	}
	line := spanLine(kind, append([]Span{lead}, row...)...)
	line.Selected = selected
	return line
}

// moreLine is a scroll marker in the body, muted: "↓ 3 more".
func moreLine(arrow string, n int) LogLine {
	return LogLine{Kind: "muted", Text: fmt.Sprintf("  %s %d more", arrow, n)}
}

// listWindow is which of n lines show in room rows with line sel in view
// (docs/ux.md §6): lines[from:to], and whether lines go on above and
// below it. Where they go on below, ↓ N more takes the window's last row;
// above, ↑ N more takes its first, or with upFree row 2's right end,
// which costs no row. The selection keeps a margin of two lines (one in a
// short window) from a marker, and the window starts as near prev as that
// allows, so the list moves only when the cursor would leave it. Under 3
// rows there are no markers.
func listWindow(n, room, sel, prev int, upFree bool) (from, to int, up, down bool) {
	if room <= 0 {
		return 0, 0, false, false
	}
	if n <= room {
		return 0, n, false, false
	}
	sel = max(0, min(n-1, sel))
	marks := room >= 3
	try := func(s int) (int, bool, bool, bool) {
		if s < 0 || s >= n {
			return 0, false, false, false
		}
		c, u := room, marks && s > 0
		if u && !upFree {
			c--
		}
		d := marks && s+c < n
		if d {
			c--
		}
		if !marks && s+c > n || !d && s+c != n && marks {
			return 0, false, false, false // the end in view leaves no row empty
		}
		m := 0
		if marks {
			m = min(2, (c-1)/2)
		}
		lo, hi := s, s+c-1
		if u {
			lo += m
		}
		if d {
			hi -= m
		}
		return s + c, u, d, sel >= lo && sel <= hi
	}
	prev = max(0, min(n-1, prev))
	for dist := 0; dist <= n; dist++ {
		for _, s := range []int{prev - dist, prev + dist} {
			if to, u, d, ok := try(s); ok {
				return s, to, u, d
			}
		}
	}
	s := max(0, min(n-room, sel))
	return s, s + room, false, false
}

// outlineScreen is the open plan's task list in rows body rows
// (docs/ux.md §6): its window around the selection, ↓ N more under it
// where the list goes on, and ↑ N more over it, or, where roomy (from 12
// rows), on row 2's right end. It returns how many lines are above the
// window for row 2 to say.
func outlineScreen(p *plan.Plan, v *outlineView, width, rows int, roomy bool) ([]LogLine, int) {
	l := newTaskList(p, width, rows, roomy)
	sel := l.index(v.sel)
	from, to, up, down := listWindow(len(l.lines), rows, sel, v.view, roomy)
	v.view = from
	var shown []LogLine
	if up && !roomy {
		shown = append(shown, moreLine("↑", from))
	}
	for i := from; i < to; i++ {
		shown = append(shown, l.draw(i, i == sel))
	}
	if down {
		shown = append(shown, moreLine("↓", len(l.lines)-to))
	}
	if up && roomy {
		return shown, from
	}
	return shown, 0
}

// outlinePage is the task PgUp or PgDn selects (docs/ux.md §3, §6): the
// task just past the last line the window shows, or just before its
// first, so a page never skips a task the window didn't show. With no
// more tasks that way, the last or the first.
func outlinePage(p *plan.Plan, v outlineView, width, rows int, roomy, down bool) string {
	l := newTaskList(p, width, rows, roomy)
	sel := l.index(v.sel)
	from, to, _, _ := listWindow(len(l.lines), rows, sel, v.view, roomy)
	if down {
		for i := max(to, sel+1); i < len(l.lines); i++ {
			if l.task(i) {
				return taskKey(l.lines[i].t)
			}
		}
		for i := len(l.lines) - 1; i >= 0; i-- {
			if l.task(i) {
				return taskKey(l.lines[i].t)
			}
		}
		return v.sel
	}
	for i := min(from, sel) - 1; i >= 0; i-- {
		if l.task(i) {
			return taskKey(l.lines[i].t)
		}
	}
	for i := range l.lines {
		if l.task(i) {
			return taskKey(l.lines[i].t)
		}
	}
	return v.sel
}

// listless is the open plan's page when it has no list (docs/ux.md §6):
// why it can't be read, or, with no tasks, what happens next; nil while
// it has a list. The page scrolls like details.
func listless(d *planDoc, name, rel string, width int) []LogLine {
	switch {
	case d.plan == nil:
		return unreadablePlan(d, width)
	case len(d.plan.Tasks) == 0:
		return emptyPlan(name, rel, width)
	}
	return nil
}

// emptyPlan is a plan with no tasks yet (docs/ux.md §6): what happens
// next, with nothing for you to copy.
func emptyPlan(name, rel string, width int) []LogLine {
	inner := max(1, width-3)
	lines := []LogLine{{Kind: "text", Text: "  No tasks yet."}}
	for _, row := range wrapPath("Ask your agent to plan "+name+". It writes the tasks into "+rel+", and they show up here as it saves.", inner) {
		lines = append(lines, LogLine{Kind: "muted", Text: "  " + row})
	}
	return lines
}

// unreadablePlan is a plan Rex has no good copy of (docs/ux.md §6): that
// it can't read it, why, and that it shows once it can.
func unreadablePlan(d *planDoc, width int) []LogLine {
	inner := max(1, width-3)
	lines := []LogLine{{Kind: "error", Text: "  Can't read this plan."}}
	why := strings.TrimPrefix(d.err, unreadable)
	if d.gone {
		why = "The file is missing."
	}
	for _, row := range wrap(why, inner) {
		lines = append(lines, LogLine{Kind: "text", Text: "  " + row})
	}
	for _, row := range wrap("Rex shows it as soon as the file is valid JSON again.", inner) {
		lines = append(lines, LogLine{Kind: "muted", Text: "  " + row})
	}
	return lines
}

// pageWindow is what of a page (task details, About) room rows show from
// line *at (docs/ux.md §6), and how many lines are above them: ↓ N more
// takes the last row while the page goes on below, and ↑ N more the
// first, or with upFree row 2's right end. It clamps *at to the page.
func pageWindow(lines []LogLine, at *int, room int, upFree bool) ([]LogLine, int) {
	n := len(lines)
	if n <= room || room < 3 {
		*at = max(0, min(*at, n-room))
		return lines[*at:min(n, *at+max(0, room))], 0
	}
	end := room // the rows the last window gives lines: ↑ may take one
	if !upFree {
		end--
	}
	s := max(0, min(*at, n-end))
	*at = s
	c, up := room, s > 0
	if up && !upFree {
		c--
	}
	down := s+c < n
	if down {
		c--
	}
	var shown []LogLine
	if up && !upFree {
		shown = append(shown, moreLine("↑", s))
	}
	shown = append(shown, lines[s:s+c]...)
	if down {
		shown = append(shown, moreLine("↓", n-s-c))
	}
	if up && upFree {
		return shown, s
	}
	return shown, 0
}

// labelCol is a page's label column (docs/ux.md §5): "Out of scope" and
// two blanks. Under stackedBelow body cells labels sit above their values.
const (
	labelCol     = 14
	stackedBelow = 50
)

// defList is a page's definition list (docs/ux.md §6): a muted label in
// the label column and its values beside it in text, each value starting
// a row and its wrapped rows hanging under its first; a value's own line
// breaks start rows too. Narrow, the label sits above its values.
type defList struct {
	width int
	lines []LogLine
}

func (b *defList) field(label string, values []string, role string) {
	if len(values) == 0 {
		return
	}
	stacked := b.width-2 < stackedBelow
	lead, room := "  "+padRight(label, labelCol), max(1, b.width-3-labelCol)
	if stacked {
		b.lines = append(b.lines, LogLine{Kind: "muted", Text: "  " + label})
		lead, room = "    ", max(1, b.width-5)
	}
	for _, v := range values {
		for _, row := range paraRows(v, room, 0) {
			b.lines = append(b.lines, spanLine("text", Span{Text: lead, Role: "muted"}, Span{Text: row, Role: role}))
			lead = strings.Repeat(" ", cellLen(lead))
		}
	}
}

// heading is a page's first lines: its title, bold, then muted lines
// saying what the page is, each wrapped to the page.
func (b *defList) heading(title string, notes ...string) {
	inner := max(1, b.width-3)
	for _, row := range wrapMost(flat(title), inner, 0) {
		if row != "" {
			b.lines = append(b.lines, spanLine("text", Span{Text: "  "}, Span{Text: row, Bold: true}))
		}
	}
	for _, note := range notes {
		for _, row := range wrapMost(flat(note), inner, 0) {
			b.lines = append(b.lines, LogLine{Kind: "muted", Text: "  " + row})
		}
	}
}

// notePinned is the line under a task's title (docs/ux.md §6): whether
// your agent gets its fields. Only the agent's pin of a plan Rex may
// send reaches it (§7a).
func notePinned(t, pin *plan.Task, p *plan.Plan) string {
	switch {
	case p.ReadOnly != "":
		return "A read-only plan sends your agent nothing."
	case t == pin && t.Status == plan.Active:
		return "Your agent gets these fields with every prompt."
	}
	return "Once this task is pinned, your agent gets these fields with every prompt."
}

// noteCap is the pin note's size, as the page that leaves fields out for
// it says (docs/ux.md §7a).
const noteCap = "1,500 bytes"

// taskPage is a task's details (docs/ux.md §6), a page of their own: its
// title, one muted line saying whether these fields reach your agent,
// then the fields of the note that pins it, in the note's order, as the
// note sends them (plan.SentFields, the pass plan.PinNote words): each
// value cleaned and cut as the note cuts it, a list's first values and
// "(+N more in the file)". A field the note drops to stay under its cap
// still shows, muted, whole as the file has it, and a line names what
// isn't sent. A read-only plan sends nothing, so all of it is muted. A
// done task leads with what your agent wrote when it finished it, then
// what was planned, muted. rel is the plan's path from the repo, as the
// note names it.
func taskPage(d *planDoc, rel, key string, width int) []LogLine {
	var t *plan.Task
	if d.plan != nil {
		t = findTask(d.plan, key)
	}
	if t == nil {
		return []LogLine{{Kind: "muted", Text: "  " + clip("This task is no longer in the file.", max(1, width-3))}}
	}
	p := d.plan
	pin := planIndexOf(p).pin
	b := &defList{width: width}
	if t.Status == plan.Done {
		note := "what your agent wrote when it finished it"
		if len(t.Accomplished)+len(t.Modified)+len(t.VerifiedHow) == 0 && strings.TrimSpace(t.CompletedNotes) == "" {
			note = "no notes yet"
		}
		b.heading(t.Title, doneMark+" Done · "+note)
		b.field("Did", t.Accomplished, "")
		b.field("Changed", t.Modified, "")
		b.field("Checked", t.VerifiedHow, "")
		b.field("Notes", nonEmpty(t.CompletedNotes), "")
		for _, f := range plan.NoteFields(p, t) {
			switch f.Label {
			case "Does":
				b.field("Planned", f.Items, "muted")
			case "Description", "Verify", "Files", "Branch":
				b.field(f.Label, f.Items, "muted")
			}
		}
		return b.lines
	}
	fields := plan.SentFields(&plan.Pinned{Rel: rel, Plan: p, Task: t})
	var dropped []string
	for _, f := range fields {
		if len(f.Sent) == 0 && (f.Label != "Also active" || t == pin) {
			dropped = append(dropped, f.Label)
		}
	}
	notes := []string{notePinned(t, pin, p)}
	if len(dropped) > 0 && p.ReadOnly == "" {
		notes = append(notes, "Not sent (the note is capped at "+noteCap+"): "+strings.Join(dropped, ", "))
	}
	b.heading(t.Title, notes...)
	for _, f := range fields {
		if f.Label == "Also active" && t != pin {
			continue // only the pin's note names the others
		}
		values, role := f.Sent, ""
		if len(values) == 0 {
			values, role = f.Items, "muted"
		}
		switch f.Label {
		case "Waits on":
			var waits []string
			for _, w := range values {
				waits = append(waits, w+" (not done)")
			}
			values = waits
		case "Builds on":
			if b := f.Tasks[0]; role == "muted" {
				values = []string{strings.TrimPrefix(b.ID+" (done): "+b.DoneWhat(), " (done): ")}
			}
		}
		if f.More > 0 {
			values = append(slices.Clip(values), fmt.Sprintf("(+%d more in the file)", f.More))
		}
		switch {
		case p.ReadOnly != "":
			role = "muted"
		case f.Label == "Also active" && role == "":
			role = "error"
		}
		b.field(f.Label, values, role)
	}
	return b.lines
}

func nonEmpty(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []string{s}
}

// aboutPage is About (docs/ux.md §6), on i from the task list: the goal,
// scope, progress and stack that are read once per plan, then the plan's
// problems and why it is read-only, in red. Its progress leads with row
// 0's count, of the tasks the list shows.
func aboutPage(d *planDoc, width int) []LogLine {
	p := d.plan
	if p == nil {
		return unreadablePlan(d, width)
	}
	x := planIndexOf(p)
	g := x.g
	b := &defList{width: width}
	b.field("Goal", nonEmpty(p.Goal), "")
	if len(p.Scope.In) > 0 {
		b.field("In scope", []string{strings.Join(p.Scope.In, " · ")}, "")
	}
	if len(p.Scope.Out) > 0 {
		b.field("Out of scope", []string{strings.Join(p.Scope.Out, " · ")}, "")
	}
	skipped, blocked := 0, 0
	for _, l := range x.lines[0] {
		switch l.t.Status {
		case plan.Skipped:
			skipped++
		case plan.Blocked:
			blocked++
		}
	}
	total := len(x.keys)
	progress := []string{fmt.Sprintf("%d/%d done", x.done, total)}
	if skipped > 0 {
		progress = append(progress, fmt.Sprintf("%d skipped", skipped))
	}
	if blocked > 0 {
		progress = append(progress, fmt.Sprintf("%d blocked", blocked))
	}
	if left := total - x.done - skipped - blocked; left > 0 {
		progress = append(progress, fmt.Sprintf("%d to go", left))
	}
	if total == 0 {
		progress = []string{"no tasks yet"}
	}
	b.field("Progress", []string{strings.Join(progress, " · ")}, "")
	if p.Stacked() {
		var stack []string
		if p.Base != "" {
			stack = append(stack, "lands on "+p.Base)
		}
		branches := 0
		for i := range p.Tasks {
			if t := &p.Tasks[i]; t.Branch != "" && g.Leaf(t) {
				branches++
			}
		}
		if branches > 0 {
			stack = append(stack, plural(branches, "%d branch", "%d branches"))
		}
		b.field("Stack", []string{strings.Join(stack, " · ")}, "")
	}
	var problems []string
	for _, pr := range p.Problems {
		problems = append(problems, pr.String())
	}
	b.field("Problems", problems, "error")
	b.field("Read-only", nonEmpty(p.ReadOnly), "error")
	return b.lines
}

// taskState is a task's state as its details' row 0 says it (docs/ux.md
// §4): "pinned" in the pin's yellow for the agent's pin, "next up" for the
// pin Rex worked out, "✔ done", else its status; a task in a loop and a
// second active one are red.
func taskState(t *plan.Task, g *plan.Graph, pin *plan.Task) (word, role string) {
	switch {
	case g.Cyclic(t):
		return "cycle", "error"
	case t == pin && t.Status == plan.Active:
		return "pinned", "accent"
	case t == pin:
		return "next up", "muted"
	case t.Status == plan.Done:
		return doneMark + " done", "muted"
	case t.Status == plan.Active && pin != nil && pin.Status == plan.Active:
		return "also active", "error"
	}
	return string(t.Status), "muted"
}

// taskPlace is a task's details' row 1 (docs/ux.md §4): where the task is
// in the list, "task 2 of 8", and the group and parents it is in, "in
// Plan views › Plan list".
func taskPlace(p *plan.Plan, key string) string {
	keys := taskKeys(p)
	at := slices.Index(keys, key)
	if at < 0 {
		return ""
	}
	s := fmt.Sprintf("task %d of %d", at+1, len(keys))
	t := findTask(p, key)
	g := planIndexOf(p).g
	var in []string
	seen := map[*plan.Task]bool{}
	up := g.Task(t.Parent)
	for ; up != nil && up != t && !seen[up]; up = g.Task(up.Parent) {
		seen[up] = true
		name := strings.TrimSpace(flat(up.Title))
		if name == "" {
			name = taskKey(up)
		}
		in = append([]string{name}, in...)
	}
	if len(in) > 0 && up != t { // in a parent loop, it is in nothing
		s += " · in " + strings.Join(in, " › ")
	}
	return s
}

var errPos = regexp.MustCompile(`^(\d+:\d+): `)

// docStatus is the open plan's error for the notice row (docs/ux.md §4),
// or false when the plan has nothing to say.
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

// pinStatus is the task list's row 1, the pin line (docs/ux.md §4): the
// agent's pin, "pinned  <id> · <title>" with the id in the pin's yellow;
// else what Rex would pin next, which is never sent, nor yellow; else why
// nothing is pinned. Narrow, the title clips, and with fewer than
// pinTitleMin cells of it the line is the id alone. The agent writes the
// pin; Rex only shows it (docs/ux.md §7).
func pinStatus(p *plan.Plan) StatusLine {
	muted := func(text string) StatusLine { return StatusLine{Text: text, Kind: "muted"} }
	if p == nil {
		return StatusLine{}
	}
	if len(p.Tasks) == 0 {
		return muted("nothing pinned · no tasks yet")
	}
	x := planIndexOf(p)
	pin := x.pin
	if pin == nil {
		if total := len(x.keys); total > 0 && x.finished == total {
			return muted(doneMark + " all tasks done")
		}
		return muted("nothing pinned · nothing unblocked")
	}
	lead, role := "pinned  ", "accent"
	if pin.Status != plan.Active {
		lead, role = "nothing pinned · next up ", "text"
	}
	id, title := flat(pin.ID), flat(pin.Title)
	s := StatusLine{Text: lead + id, Kind: "muted"}
	if strings.TrimSpace(title) != "" {
		s.Text += " · " + title
		s.Fit = func(w int) string {
			room := w - cellLen(lead+id+" · ")
			if room < pinTitleMin {
				return lead + id
			}
			return lead + id + " · " + ellipsis(title, room)
		}
	}
	s.Spans = func(text string) []Span {
		rest, ok := strings.CutPrefix(text, lead)
		if !ok {
			return nil
		}
		spans := []Span{{Text: lead}}
		if rest, ok = strings.CutPrefix(rest, id); !ok {
			return append(spans, Span{Text: rest, Role: role})
		}
		spans = append(spans, Span{Text: id, Role: role})
		if body, ok := strings.CutPrefix(rest, " · "); ok {
			return append(spans, Span{Text: " · "}, Span{Text: body, Role: "text"})
		}
		return append(spans, Span{Text: rest})
	}
	return s
}

// pinTitleMin is the fewest title cells the pin line keeps; with fewer,
// it is just the pin's id.
const pinTitleMin = 8

// progressWide is the text width from which the plan list's rows carry
// their titles and its accordion its labels (docs/ux.md §11).
const progressWide = 60

// progress is the open plan's progress for row 0's meta: the done tasks
// of those the list shows, "2/8", or "" before it has any. Skipped tasks
// aren't done; About says how many there are.
func progress(p *plan.Plan) string {
	if p == nil {
		return ""
	}
	x := planIndexOf(p)
	if len(x.keys) == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", x.done, len(x.keys))
}
