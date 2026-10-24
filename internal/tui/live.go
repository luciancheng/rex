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
	InputRows       = 3
	FooterRows      = 1
	MaxLog          = 400
	maxPromptRows   = 6 // a pasted wall of text must not push the stream away
)

var kindColor = map[string]string{
	"before":   "muted",
	"plan":     "muted",
	"drift":    "muted", // its rows carry hard's or soft's color
	"thought":  "muted",
	"command":  "accent",
	"question": "error",
	"text":     "text",
	"user":     "you",
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

func rule(label string, width int) string {
	label = strings.ReplaceAll(watch.CleanText(label), "\n", " ")
	prefix := "── " + label + " "
	if cellLen(prefix) >= width {
		return clip(label, width)
	}
	return prefix + strings.Repeat("─", width-cellLen(prefix))
}

// userRows is a prompt (docs/ux.md §10): a box at most half the row wide,
// right-aligned, its text left-aligned inside, capped at maxPromptRows.
// Short screens (not tall) drop its blank rows.
func userRows(text string, width int, tall bool) []string {
	boxW := max(8, width/2)
	chunks := watch.MarkdownLines(text)
	var wrapped []string
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			if tall {
				wrapped = append(wrapped, "")
			}
			continue
		}
		var lines []string
		if strings.HasPrefix(chunk, "│ ") {
			lines = hardWrap(chunk, boxW)
		} else {
			lines = wrap(chunk, boxW)
		}
		if len(lines) == 0 {
			lines = []string{""}
		}
		wrapped = append(wrapped, lines...)
	}
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	if len(wrapped) > maxPromptRows {
		more := len(wrapped) - maxPromptRows
		wrapped = append(wrapped[:maxPromptRows], fmt.Sprintf("(+%d more rows)", more))
	}
	contentW := 0
	for _, line := range wrapped {
		if n := cellLen(line); n > contentW {
			contentW = n
		}
	}
	if contentW > boxW {
		contentW = boxW
	}
	pad := max(0, width-contentW)
	var rows []string
	for _, line := range wrapped {
		if strings.TrimSpace(line) == "" {
			rows = append(rows, strings.Repeat(" ", width))
			continue
		}
		rows = append(rows, strings.Repeat(" ", pad)+padRight(line, contentW))
	}
	if len(rows) == 0 {
		return []string{strings.Repeat(" ", width)}
	}
	return rows
}

type LogLine struct {
	Kind     string
	Text     string
	Fill     string
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

// PaneBodyHeight is AGENT's body height: it has no composer, and a page
// title only from agentTitleRows rows.
func PaneBodyHeight(termLines int) int {
	l := layoutFor(TextOnlyBelow, termLines).bare()
	if termLines < agentTitleRows {
		l = l.untitled()
	}
	return l.bodyH
}

func PaneTextWidth(columns int) int {
	return layoutFor(columns, 24).textW
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

func inputBox(draft string, width int, enabled bool, prefix, placeholder string) []string {
	inner := max(0, width-2)
	top := "╭" + strings.Repeat("─", inner) + "╮"
	bot := "╰" + strings.Repeat("─", inner) + "╯"
	if width < 4 {
		out := make([]string, InputRows)
		for i := range out {
			out[i] = clip("", width)
		}
		return out
	}
	middle := mascot.Paint(clip(prefix+visibleDraft(draft, inner-cellLen(prefix)), inner), mascot.UI["text"], enabled)
	if draft == "" && placeholder != "" {
		room := max(0, inner-cellLen(prefix))
		middle = mascot.Paint(prefix, mascot.UI["text"], enabled) + mascot.Paint(clip(hintText(placeholder, enabled), room), mascot.UI["border"], enabled)
	}
	return []string{
		mascot.Paint(top, mascot.UI["border"], enabled),
		mascot.Paint("│", mascot.UI["border"], enabled) + middle + mascot.Paint("│", mascot.UI["border"], enabled),
		mascot.Paint(bot, mascot.UI["border"], enabled),
	}
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
	bodyTop := 2 + l.page
	bodyBottom := height - FooterRows - l.composer
	bodyH := max(0, bodyBottom-bodyTop)
	var body, pinned []LogLine
	scroll := opt.Scroll
	fromTop := false
	if opt.Overlay != nil {
		body = opt.Overlay.Lines
		scroll = min(opt.Overlay.Scroll, max(0, len(body)-bodyH))
		fromTop = true
	} else if opt.Pane == PanePlans && opt.PlanView != nil {
		// The outline's top lines and warnings stay put; the
		// tasks under them scroll.
		head := headOf(opt.PlanView)
		pinned = planHead(opt.PlanView[:head], len(opt.PlanView), bodyH)
		body = opt.PlanView[head:]
		bodyTop += len(pinned)
		bodyH -= len(pinned)
		scroll = min(opt.PlanScroll, max(0, len(body)-bodyH))
		fromTop = true
	} else if opt.Pane == PanePlans && opt.Plans != nil {
		// The plan list's folder, notice and error stay put; only the list
		// under them scrolls.
		all := BuildPlanView(opt.Plans, width)
		head := min(len(all), opt.Plans.headLen())
		pinned = planHead(all[:head], len(all), bodyH)
		body = all[head:]
		bodyTop += len(pinned)
		bodyH -= len(pinned)
		scroll = opt.Plans.window(body, bodyH)
		fromTop = true
	} else {
		body = opt.LogView
		if body == nil {
			// app.viewOpt's rule, read from the turn's word: a run is live
			// while the turn is in flight, scrolled or not.
			live := opt.Status == "thinking" || opt.Status == "running"
			body = BuildLogView(opt.Log, width, viewOpt{live: live, tall: height >= agentTitleRows})
		}
		if len(opt.Log) == 0 {
			// The empty tab is a page, not a stream: it keeps its top and
			// crops from the bottom (docs/ux.md §10).
			body = body[:min(len(body), bodyH)]
		}
		scroll = ClampScroll(len(body), bodyH, scroll)
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
		if fromTop && len(visible) > 2 {
			// Say when the list goes on past the window, without covering the selection.
			if start > 0 && !visible[0].Selected {
				visible[0] = LogLine{Kind: "idle", Text: "  ↑ more"}
			}
			if end < len(body) && !visible[len(visible)-1].Selected {
				visible[len(visible)-1] = LogLine{Kind: "idle", Text: "  ↓ more"}
			}
		}
	}
	enabled := opt.Enabled
	rows := make([]string, height)
	rows[0] = paintLine(headerRow(width, opt.Pane, opt.Cue, opt.Status, opt.Follow, opt.PlanRel), width, enabled)
	if height > 1 {
		rows[1] = statusRow(opt.Line, width, enabled)
	}
	if l.page > 0 && opt.Page != nil {
		rows[2] = paintLine(pageLine(*opt.Page, width), width, enabled)
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
	for index := bodyTop; index < bodyBottom; index++ {
		if rows[index] == "" {
			rows[index] = strings.Repeat(" ", width)
		}
	}
	for offset, line := range composerRows(l, opt.Draft, composerPrefix(opt), opt.Placeholder, enabled) {
		rows[height-FooterRows-l.composer+offset] = line
	}
	if height > 2 {
		segs, kind := footerSegments(opt, scroll)
		if opt.Overlay != nil && !opt.QuitPending {
			segs, kind = opt.Overlay.Footer, "muted"
		}
		if !l.typing && opt.Composer {
			segs = withoutEnter(segs, opt.Draft)
		}
		rows[len(rows)-1] = paintLine(footerLine(segs, width, kind), width, enabled)
	}
	return rows
}

// StatusLine is row 1: the single most important thing to know right now
// (docs/ux.md §4). Kind picks the color.
type StatusLine struct {
	Text  string
	Short string                 // used instead of Text when Text doesn't fit
	Fit   func(width int) string // instead of Short: Text fitted to width cells
	Kind  string
	// Spans styles the text as it is drawn, fitted or not: keys bold, the
	// pin's mark. Nil draws it all in Kind.
	Spans func(text string) []Span
	// Right is the open plan's progress, "1/3" ("1/3 done" from
	// progressWide cells), right-aligned while the left keeps statusMin
	// cells and every word it shows without it.
	Right string
	// Clip says Fit only clips the pin's title (to its id at the least),
	// which progress may cost (docs/ux.md §4).
	Clip bool
}

const (
	progressWide = 60
	statusMin    = 12
)

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
	room, text, right := width, fit(width), ""
	// Progress never costs the line its words (docs/ux.md §4): it sits
	// beside the line only where the line reads just as it does across the
	// whole width, or where only a pin's title clips for it; "1/3 done"
	// from progressWide cells, else "1/3", and keeps statusMin cells on
	// its left.
	var forms []string
	if s.Right != "" && width >= progressWide {
		forms = append(forms, s.Right+" done")
	}
	if s.Right != "" {
		forms = append(forms, s.Right)
	}
	for _, p := range forms {
		r := width - cellLen(p) - 2
		if t := fit(r); r >= statusMin && cellLen(t) <= r && (t == text || s.Clip) {
			room, text, right = r, t, p
			break
		}
	}
	var spans []Span
	if s.Spans != nil {
		spans = s.Spans(text)
	}
	if spans == nil {
		spans = []Span{{Text: text}}
	}
	row := paintSpans(spans, kind, "", room, enabled)
	if right != "" {
		row += "  " + mascot.Paint(right, mascot.UI["muted"], enabled)
	}
	return row
}

func paintLine(line LogLine, width int, enabled bool) string {
	if line.Kind == "gap" {
		return strings.Repeat(" ", width)
	}
	if len(line.Spans) > 0 {
		return paintSpans(line.Spans, line.Kind, line.Fill, width, enabled)
	}
	rgb, ok := mascot.UI[line.Kind]
	if !ok {
		colorName := kindColor[line.Kind]
		if colorName == "" {
			colorName = "text"
		}
		rgb = mascot.UI[colorName]
	}
	text := clip(line.Text, width)
	if fill, ok := mascot.UI[line.Fill]; ok {
		return mascot.PaintFill(text, rgb, fill, enabled)
	}
	return mascot.Paint(text, rgb, enabled)
}

type ComposeOptions struct {
	Enabled     bool
	Columns     int
	Lines       int
	Composer    bool // the plan list's composer is drawn and takes typing (app.composing, docs/ux.md §2)
	Draft       string
	Log         []watch.Event
	QuitPending bool
	Status      string
	Cue         string // the other place's news on the tab bar: "", "•" or "!"
	Scroll      int
	Pane        PaneKind
	Plans       *PlanBrowser
	LogView     []LogLine // laid-out AGENT stream, when the caller already has it
	PlanRel     string    // open plan, repo-relative, for the header
	Follow      string    // "following" or "latched" while a plan is open
	Line        StatusLine
	Page        *Page // row 2's page title; nil on AGENT under agentTitleRows rows
	Overlay     *Overlay
	Placeholder string      // dim hint in an empty composer
	Preview     string      // what enter will do with the draft, shown in the footer
	PlanView    []LogLine   // the open plan's screen, when PLAN shows it
	PlanScroll  int         // its first visible line
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
	if !opt.Composer {
		l = l.bare()
	}
	if opt.Page == nil {
		l = l.untitled()
	}
	if l.locked {
		rows := make([]string, lines)
		rows[0] = mascot.Paint(clip(fmt.Sprintf("widen to %d cols", MinTextCols), columns), mascot.UI["title"], opt.Enabled)
		if msg := fmt.Sprintf("REX · widen to %d cols", MinTextCols); cellLen(msg) <= columns {
			rows[0] = mascot.Paint(clip(msg, columns), mascot.UI["title"], opt.Enabled)
		}
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
	f.CursorRow, f.CursorCol = cursorFor(l, lines, opt.Draft, composerPrefix(opt))
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
