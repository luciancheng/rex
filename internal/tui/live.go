package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/watch"
)

const (
	FrameTick       = 80 // milliseconds
	QuitWindowTicks = 25
	Gutter          = 2
	MinTextCols     = 24
	FooterRows      = 1
	MaxLog          = 400
	maxPromptRows   = 6 // a pasted wall of text must not push the stream away
)

var kindColor = map[string]string{
	"before":   "muted",
	"plan":     "muted",
	"thought":  "muted",
	"command":  "muted",
	"question": "text",
	"text":     "text",
	"user":     "text",
	"result":   "muted",
	"error":    "error",
	"idle":     "muted",
}

var kindStatus = map[string]string{
	"thought":  "thinking",
	"command":  "running",
	"question": "ask",
	"text":     "idle",
	"user":     "idle",
	"result":   "idle",
	"error":    "error",
	"idle":     "idle",
}

func runeLen(s string) int {
	return len([]rune(s))
}

func wrap(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		candidate := current + " " + word
		if cellLen(candidate) <= width {
			current = candidate
			continue
		}
		lines = append(lines, current)
		current = word
	}
	lines = append(lines, current)
	var wrapped []string
	for _, line := range lines {
		if cellLen(line) <= width {
			wrapped = append(wrapped, line)
			continue
		}
		wrapped = append(wrapped, hardWrap(line, width)...)
	}
	return wrapped
}

func hardWrap(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	if text == "" {
		return []string{""}
	}
	var out []string
	for text != "" {
		part := cutCells(text, width)
		if part == "" {
			_, size := utf8.DecodeRuneInString(text)
			part = text[:size]
		}
		out = append(out, part)
		text = text[len(part):]
	}
	return out
}

// wrapPath is wrap for text that names a path (docs/ux.md §11): a path
// too long for its row starts a row of its own and breaks after a "/",
// never inside a name, and a name still too long keeps its tail behind
// "…".
func wrapPath(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		if cur != "" && cellLen(cur+" "+word) <= width {
			cur += " " + word
			continue
		}
		if cur != "" {
			lines, cur = append(lines, cur), ""
		}
		for _, part := range strings.SplitAfter(word, "/") {
			switch {
			case part == "":
			case cellLen(cur+part) <= width:
				cur += part
			default:
				if cur != "" {
					lines = append(lines, cur)
				}
				cur = part
				if cellLen(part) > width {
					cur = "…" + tailCells(part, width-1)
				}
			}
		}
	}
	return append(lines, cur)
}

type LogLine struct {
	Kind     string
	Text     string
	Spans    []Span // styled runs, when the line has more than one style; Text is their join
	Selected bool   // part of the selected row; the plan pane keeps it in view
	Head     bool   // leads a plan screen and stays put while the rest scrolls
}

// headOf is how many lines at the top of body are Head lines.
func headOf(body []LogLine) int {
	n := 0
	for n < len(body) && body[n].Head {
		n++
	}
	return n
}

// ExtendLog adds the events AGENT draws to log and returns them: a reply
// that grows replaces its last copy, a repeat is dropped, and the log keeps
// its newest MaxLog events.
func ExtendLog(log *[]watch.Event, incoming []watch.Event) []watch.Event {
	added, _ := extendLog(log, incoming)
	return added
}

// extendLog is ExtendLog that also says how many of the oldest events the
// MaxLog cap dropped.
func extendLog(log *[]watch.Event, incoming []watch.Event) ([]watch.Event, int) {
	var added []watch.Event
	for _, event := range incoming {
		if !watch.KeepInPane(event) {
			continue
		}
		if n := len(*log); n > 0 && watch.IsGrowingText((*log)[n-1], event) {
			(*log)[n-1].Text = watch.LongerText((*log)[n-1].Text, event.Text)
			added = append(added, (*log)[n-1])
			continue
		}
		start := 0
		if n := len(*log); n > 12 {
			start = n - 12
		}
		if watch.IsDuplicate(event, (*log)[start:], 4) {
			continue
		}
		*log = append(*log, event)
		added = append(added, event)
	}
	dropped := max(0, len(*log)-MaxLog)
	*log = (*log)[dropped:]
	return added, dropped
}

type Turn int

const (
	TurnIdle Turn = iota
	TurnThinking
	TurnRunning
)

func ApplyTurn(turn Turn, ev watch.Event) Turn {
	if ev.Source == "user" || ev.Source == "rex" {
		return turn // Rex's own lines never start or end a turn
	}
	switch ev.Kind {
	case "user", "thought":
		return TurnThinking
	case "command", "question":
		return TurnRunning
	case "result":
		return turn
	case "error":
		// A failed tool call doesn't end the turn; the agent carries on.
		if watch.MetaTool(ev) != "" {
			return TurnRunning
		}
		return TurnIdle
	case "text", "idle":
		return TurnIdle
	default:
		return turn
	}
}

func AnimationForTurn(turn Turn) string {
	switch turn {
	case TurnThinking, TurnRunning:
		return "typing"
	default:
		return mascot.DefaultAnimation
	}
}

func statusForTurn(turn Turn, lastKind string) string {
	switch turn {
	case TurnThinking:
		return "thinking"
	case TurnRunning:
		// A tool that failed mid-turn doesn't stop the turn: still running.
		if s, ok := kindStatus[lastKind]; ok && lastKind != "user" && lastKind != "error" {
			return s
		}
		return "running"
	default:
		// Idle never claims work in flight: after a turn ends with no reply
		// (Cursor stop), the last kind may still be a command or thought.
		if s, ok := kindStatus[lastKind]; ok && s != "running" && s != "thinking" {
			return s
		}
		return mascot.DefaultAnimation
	}
}

// PaneBodyHeight is AGENT's body height.
func PaneBodyHeight(termLines int) int {
	return layoutFor(TextOnlyBelow, termLines).bodyH
}

// PaneTextWidth is the text column's width: the mascot takes its share
// only on a terminal big enough for it (docs/ux.md §12).
func PaneTextWidth(columns, lines int) int {
	return layoutFor(columns, lines).textW
}

func ClampScroll(total, viewH, scroll int) int {
	maxScroll := max(0, total-viewH)
	if scroll < 0 {
		return 0
	}
	if scroll > maxScroll {
		return maxScroll
	}
	return scroll
}

// visibleDraft is the end of the draft that fits room cells, so the
// cursor always sits after what was just typed.
// visibleDraft is the end of the draft that fits in room cells, with "…"
// on the left when it was cut. It leaves the last cell free for the
// cursor.
func visibleDraft(draft string, room int) string {
	room--
	if room <= 0 {
		return ""
	}
	clean := strings.ReplaceAll(watch.CleanText(draft), "\n", " ")
	if cellLen(clean) <= room {
		return clean
	}
	if room == 1 {
		return "…"
	}
	return "…" + tailCells(clean, room-1)
}

func textColumn(l layout, height int, opt ComposeOptions) []string {
	width := l.textW
	if width <= 0 || height <= 0 {
		out := make([]string, max(0, height))
		for i := range out {
			out[i] = ""
		}
		return out
	}
	bodyTop := 2 + l.blank
	bodyBottom := height - FooterRows - l.notice
	fillBottom := bodyBottom // rows a shorter view leaves are blank
	bodyH := max(0, bodyBottom-bodyTop)
	var body, pinned []LogLine
	scroll := opt.Scroll
	fromTop, list, stream := false, false, false
	more := opt.More
	if opt.Overlay != nil {
		body = opt.Overlay.Lines
		scroll = min(opt.Overlay.Scroll, max(0, len(body)-bodyH))
		fromTop = true
	} else if opt.Pane == PanePlans && opt.Plans != nil {
		// The text field stays put; only the list under it scrolls.
		all := planList(opt.Plans, width, listFit{body: bodyH, tall: l.boxed, short: height < blankFrom, field: opt.Composer && l.typing, draft: opt.Draft})
		head := min(headOf(all), bodyH)
		pinned = all[:head]
		body = all[head:]
		bodyTop += len(pinned)
		bodyH -= len(pinned)
		scroll = opt.Plans.window(body, bodyH)
		fromTop, list = true, true
	} else if opt.Pane == PanePlans {
		// The open plan's screen, even an empty one. The task list and its
		// pages come laid out to their rows (docs/ux.md §6), PlanRows of
		// them where it says.
		if opt.PlanRows > 0 && opt.PlanRows < bodyH {
			bodyH, bodyBottom = opt.PlanRows, bodyTop+opt.PlanRows
		}
		head := min(headOf(opt.PlanView), bodyH)
		pinned = opt.PlanView[:head]
		body = opt.PlanView[head:]
		bodyTop += len(pinned)
		bodyH -= len(pinned)
		scroll = min(opt.PlanScroll, max(0, len(body)-bodyH))
		fromTop = true
	} else {
		body = opt.LogView
		if body == nil {
			body = BuildLogView(opt.Log, width, viewOpt{tall: height >= blankFrom}) // app.viewOpt's rule
		}
		if len(opt.Log) == 0 {
			// The empty tab is a page, not a stream: it starts at its top
			// and scrolls from there (docs/ux.md §10).
			scroll = min(scroll, max(0, len(body)-bodyH))
			fromTop = true
		} else {
			scroll = ClampScroll(len(body), bodyH, scroll)
			stream = true
		}
	}
	var visible []LogLine
	if bodyH > 0 {
		var start, end int
		if fromTop {
			start = scroll
			end = min(len(body), start+bodyH)
		} else {
			end = len(body) - scroll
			start = max(0, end-bodyH)
			if end > len(body) {
				end = len(body)
			}
		}
		if start > end {
			start = end
		}
		visible = append([]LogLine(nil), body[start:end]...)
		switch {
		case fromTop && len(visible) > 2 && list:
			visible = listMore(visible, body[:start], body[end:])
		case fromTop && len(visible) > 2:
			// Say when the list goes on past the window, without covering the selection.
			if start > 0 && !visible[0].Selected {
				visible[0] = LogLine{Kind: "idle", Text: "  ↑ more"}
			}
			if end < len(body) && !visible[len(visible)-1].Selected {
				visible[len(visible)-1] = LogLine{Kind: "idle", Text: "  ↓ more"}
			}
		case stream && start > 0 && l.blank > 0:
			// The stream's older rows are cut off the top: row 2 says how
			// many, costing no row (docs/ux.md §10).
			more = fmt.Sprintf("↑ %d earlier", start)
		case stream && start > 0 && len(visible) > 2:
			visible[0] = LogLine{Kind: "idle", Text: fmt.Sprintf("  ↑ %d earlier", start+1)}
		}
	}
	enabled := opt.Enabled
	rows := make([]string, height)
	page := Page{Crumbs: []string{"Plans"}}
	if opt.Pane == PaneChat {
		page = Page{Crumbs: []string{"Agent"}}
	}
	if opt.Page != nil {
		page = *opt.Page
	}
	rows[0] = paintLine(titleLine(page, width, opt.Pane), width, enabled)
	if height > 1 {
		// Under blankFrom rows there is no notice row: a notice takes row 1
		// while it shows (docs/ux.md §4).
		line := opt.Context
		if l.notice == 0 && opt.Notice.Text != "" {
			line = opt.Notice
		}
		rows[1] = statusRow(line, width, enabled)
	}
	if l.blank > 0 {
		// Blank, but for ↑ N more at its right end while the open plan's
		// list or page is scrolled (docs/ux.md §4), or ↑ N earlier while
		// AGENT's older rows are cut off (§10).
		rows[2] = strings.Repeat(" ", width)
		if n := cellLen(more); n > 0 && n < width {
			rows[2] = strings.Repeat(" ", width-1-n) + paintSpans([]Span{{Text: more}}, "muted", n, enabled) + " "
		}
	}
	for offset, line := range pinned {
		rows[bodyTop-len(pinned)+offset] = paintLine(line, width, enabled)
	}
	for offset, line := range visible {
		if bodyTop+offset >= bodyBottom {
			break
		}
		rows[bodyTop+offset] = paintLine(line, width, enabled)
	}
	for index := bodyTop; index < fillBottom; index++ {
		if rows[index] == "" {
			rows[index] = strings.Repeat(" ", width)
		}
	}
	if l.notice > 0 {
		rows[height-FooterRows-1] = statusRow(opt.Notice, width, enabled)
	}
	if height > 2 {
		segs := footerSegments(opt, scroll)
		if opt.Overlay != nil && !opt.QuitPending {
			segs = opt.Overlay.Footer
		}
		if !l.typing && opt.Plans != nil && opt.Overlay == nil {
			segs = withoutEnter(segs, opt.Draft, opt.Plans.Prompting())
		}
		rows[len(rows)-1] = paintLine(footerLine(segs, width), width, enabled)
	}
	return rows
}

// StatusLine is a one-line row of the chrome (docs/ux.md §4): row 1's
// context, or the notice. Kind picks the color.
type StatusLine struct {
	Text  string
	Short string                 // used instead of Text when Text doesn't fit
	Fit   func(width int) string // instead of Short: Text fitted to width cells
	Kind  string
	// Spans styles the text as it is drawn, fitted or not: the pin's id,
	// a ✗. Nil draws it all in Kind.
	Spans func(text string) []Span
	// Right sits at the row's right end while the left keeps every word
	// it shows without it, two cells from it: AGENT's pin, the pin's send
	// state.
	Right []Span
	// Keep, when set, is the fewest cells the left may be fitted to (by
	// Fit) to keep Right beside it: the pin line's title clips before the
	// send state goes (docs/ux.md §4).
	Keep int
}

// statusMin is the fewest cells row 1's left keeps beside its right side.
const statusMin = 12

// statusRow draws a StatusLine one cell short of the edge, fitted to the
// room: its Fit or Short where its Text doesn't fit. The right side goes
// before the left loses a word; with Keep, before the left is fitted
// under Keep cells.
func statusRow(s StatusLine, width int, enabled bool) string {
	if strings.TrimSpace(s.Text) == "" {
		return strings.Repeat(" ", width)
	}
	kind := s.Kind
	if _, ok := mascot.UI[kind]; !ok {
		kind = "muted"
	}
	fit := func(room int) string {
		switch {
		case cellLen(s.Text) <= room:
		case s.Fit != nil:
			return s.Fit(room)
		case s.Short != "":
			return s.Short
		}
		return s.Text
	}
	room := width - 1
	var right []Span
	if r := spansLen(s.Right); r > 0 {
		need := cellLen(s.Text)
		if s.Keep > 0 && s.Fit != nil {
			need = min(need, s.Keep)
		}
		if left := room - r - 2; left >= statusMin && need <= left {
			room, right = left, s.Right
		}
	}
	text := fit(room)
	var spans []Span
	if s.Spans != nil {
		spans = s.Spans(text)
	}
	if spans == nil {
		spans = []Span{{Text: text}}
	}
	if right == nil {
		return paintSpans(spans, kind, room, enabled) + " "
	}
	return paintSpans(spans, kind, room+2, enabled) + paintSpans(right, kind, width-room-2, enabled)
}

func paintLine(line LogLine, width int, enabled bool) string {
	if line.Kind == "gap" {
		return strings.Repeat(" ", width)
	}
	spans := line.Spans
	if len(spans) == 0 {
		spans = []Span{{Text: line.Text}}
	}
	return paintSpans(spans, line.Kind, width, enabled)
}

type ComposeOptions struct {
	Enabled     bool
	Columns     int
	Lines       int
	Composer    bool // Plans' text field is open: drawn at the top of its body, the cursor in it (app.fieldOpen, docs/ux.md §2)
	Draft       string
	Log         []watch.Event
	QuitPending bool
	Status      string // the turn's word, for AGENT's live layout
	Scroll      int
	Pane        PaneKind
	Plans       *PlanBrowser // Plans, set only while it is PLAN's screen
	LogView     []LogLine    // laid-out AGENT stream, when the caller already has it
	Page        *Page        // row 0's title; nil names the place
	Context     StatusLine   // row 1 (docs/ux.md §4)
	Notice      StatusLine   // the notice row, or row 1 under blankFrom rows
	Overlay     *Overlay
	Preview     string      // what enter will do with the draft, shown in the footer
	PlanView    []LogLine   // the open plan's screen, when PLAN shows one
	PlanScroll  int         // its first visible line
	PlanRows    int         // the body rows the open plan's screen takes, when fewer than the screen has
	More        string      // "↑ 3 more": the open plan's list or page goes on above, said at row 2's right end
	Footer      []FooterSeg // the screen's footer, when the caller has it
}

// Frame is one screen: full-width rows plus where the cursor goes.
type Frame struct {
	Rows      []string
	CursorRow int // 1-based; 0 hides the cursor
	CursorCol int
}

// ComposeFrame renders the whole screen as one string (home, clear, rows,
// cursor). The live loop paints only changed rows instead; see renderer.
func ComposeFrame(spriteName, color string, opt ComposeOptions) string {
	f := composeRows(spriteName, color, opt)
	frame := Home + Clear + strings.Join(f.Rows, "\r\n")
	if f.CursorRow > 0 {
		return frame + fmt.Sprintf("\033[%d;%dH%s", f.CursorRow, f.CursorCol, ShowCursor)
	}
	return frame + HideCursor
}

func composeRows(spriteName, color string, opt ComposeOptions) Frame {
	columns := max(1, opt.Columns)
	lines := max(1, opt.Lines)
	l := layoutFor(columns, lines)
	if l.locked {
		rows := make([]string, lines)
		msg := fmt.Sprintf("widen to %d columns", MinTextCols)
		if cellLen(msg) > columns {
			msg = fmt.Sprintf("widen to %d cols", MinTextCols)
		}
		rows[0] = mascot.Paint(clip(msg, columns), mascot.UI["title"], opt.Enabled)
		for i := 1; i < lines; i++ {
			rows[i] = strings.Repeat(" ", columns)
		}
		if opt.QuitPending && lines > 1 {
			rows[1] = mascot.Paint(clip("ctrl+c again to quit", columns), mascot.UI["error"], opt.Enabled)
		}
		return Frame{Rows: rows}
	}
	if opt.Status == "" {
		opt.Status = "idle"
	}
	var mascotRows []string
	if l.mascotCols > 0 {
		mascotRows = stagedMascot(spriteName, color, l.mascotCols, opt.Enabled)
	}
	blankMascot := strings.Repeat(" ", l.mascotCols)
	gutter := strings.Repeat(" ", l.gutter)
	text := textColumn(l, lines, opt)
	out := make([]string, 0, lines)
	for index := 0; index < lines; index++ {
		left := blankMascot
		if index < len(mascotRows) {
			left = mascotRows[index]
		}
		out = append(out, left+gutter+text[index])
	}
	f := Frame{Rows: out}
	if opt.Composer && opt.Plans != nil && opt.Overlay == nil && l.typing && l.textW > 0 {
		// The cursor sits after the text in Plans' field, at the top of the
		// body (docs/ux.md §2).
		_, row, col := fieldLines(opt.Plans.fieldLabel(), opt.Draft, l.textW, l.boxed)
		f.CursorRow, f.CursorCol = 2+l.blank+row+1, l.mascotCols+l.gutter+col+1
	}
	return f
}

type mascotKey struct {
	sprite, color string
	cols          int
	enabled       bool
	depth         mascot.Depth
}

var (
	mascotMu    sync.Mutex
	mascotCache = map[mascotKey][]string{}
)

// stagedMascot renders a sprite once per size and color mode; the typing
// sheet would otherwise re-render ~800 cells on every frame.
func stagedMascot(spriteName, color string, mascotCols int, enabled bool) []string {
	key := mascotKey{spriteName, color, mascotCols, enabled, mascot.ColorDepth}
	mascotMu.Lock()
	defer mascotMu.Unlock()
	if rows, ok := mascotCache[key]; ok {
		return rows
	}
	sprite := mascot.MustSprite(spriteName)
	staged := mascot.FitStageDefault(sprite.Rows)
	if mascotCols < mascot.StageSize {
		cropped := make([]string, len(staged))
		for i, row := range staged {
			r := []rune(row)
			if mascotCols < len(r) {
				cropped[i] = string(r[:mascotCols])
			} else {
				cropped[i] = row
			}
		}
		staged = cropped
	}
	rows := mascot.MustRenderRows(staged, mascot.MustPalette(color), enabled)
	if len(mascotCache) > 64 {
		mascotCache = map[mascotKey][]string{}
	}
	mascotCache[key] = rows
	return rows
}

func PrintCard(spriteName, color string, enabled bool, stdout io.Writer) error {
	sprite := mascot.MustSprite(spriteName)
	pal, err := mascot.PaletteFor(color)
	if err != nil {
		return err
	}
	rows, err := mascot.RenderRows(sprite.Rows, pal, enabled)
	if err != nil {
		return err
	}
	title := mascot.Paint("REX", mascot.UI["title"], enabled)
	tagline := mascot.Paint("Small dino. Big opinions.", mascot.UI["muted"], enabled)
	_, _ = io.WriteString(stdout, "\n")
	_, _ = fmt.Fprintf(stdout, "  %s   %s\n\n", title, tagline)
	for _, row := range rows {
		_, _ = fmt.Fprintf(stdout, "  %s\n", row)
	}
	_, _ = fmt.Fprintf(stdout, "\n  %s\n\n", mascot.Paint(sprite.Name, mascot.UI["muted"], enabled))
	return nil
}

func advance(frames []mascot.Frame, index, hold int, loop bool) (nextIndex, nextHold int, done bool) {
	hold--
	if hold > 0 {
		return index, hold, false
	}
	next := index + 1
	if next >= len(frames) {
		if !loop {
			return index, 1, true
		}
		next = 0
	}
	return next, frames[next].Hold, false
}

func animationLoops(name string) bool {
	return name != "breakdance"
}

func isPrintableKey(key string) bool {
	r := []rune(key)
	return len(r) == 1 && unicode.IsPrint(r[0])
}

func RunLoop(color string, enabled bool, animation string, stdin, stdout *os.File, eventsPath string) (int, error) {
	columns, lines := Size()
	a := newApp(color, enabled, animation, NewPlanBrowser(""), columns, lines)
	path := eventsPath
	if path == "" {
		path = watch.DefaultEventPath()
	}
	tail := watch.NewEventTail(path, true)
	a.health = readHealth(path, a.plans.Repo, time.Now())
	a.warmStart(watch.LastEvents(path, warmRead), time.Now())
	keys := newKeyReader(stdin)
	paint := &renderer{full: true}
	err := LiveScreen(stdout, stdin, func(sig screenSignals) error {
		draw := func() {
			if a.repaint {
				paint.invalidate()
				a.repaint = false
			}
			_, _ = io.WriteString(stdout, paint.render(a.frameRows()))
			_ = stdout.Sync()
		}
		draw()
		frame := time.Duration(FrameTick) * time.Millisecond
		nextTick := time.Now().Add(frame)
		for {
			select {
			case <-sig.quit:
				return nil
			case <-sig.resume:
				reenterRaw(int(stdin.Fd()))
				_, _ = io.WriteString(stdout, screenSetup) // bracketed paste etc. are off after a stop
				paint.invalidate()
				draw()
			default:
			}
			pressed := keys.Read(max(time.Millisecond, time.Until(nextTick)))
			changed := a.step(pressed, tail.Poll())
			if a.quit {
				return nil
			}
			// The mascot advances on the clock, not on reads: key-ups and
			// dropped sequences produce reads with no keys in them.
			if now := time.Now(); !now.Before(nextTick) {
				nextTick = now.Add(frame)
				if a.tick() {
					changed = true
				}
			}
			if a.settle(Size()) {
				changed = true
			}
			if changed {
				draw()
			}
		}
	})
	if err == nil {
		_, _ = io.WriteString(stdout, a.exitLine()+"\n")
	}
	return 0, err
}
