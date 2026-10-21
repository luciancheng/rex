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
	InputPrefix     = " > "
	MaxLog          = 400
)

var kindColor = map[string]string{
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

var kindLabel = map[string]string{
	"thought":  "thinking",
	"command":  "run",
	"question": "ask",
	"text":     "reply",
	"user":     "you",
	"result":   "done",
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

func asLogEntry(event watch.Event) (string, string) {
	if watch.ProseKinds[event.Kind] {
		return event.Kind, watch.CleanText(event.Text)
	}
	return event.Kind, watch.CleanText(event.DisplayText())
}

func rule(label string, width int) string {
	label = strings.ReplaceAll(watch.CleanText(label), "\n", " ")
	prefix := "── " + label + " "
	if cellLen(prefix) >= width {
		return clip(label, width)
	}
	return prefix + strings.Repeat("─", width-cellLen(prefix))
}

func userRows(text string, width int) []string {
	boxW := max(8, width/2)
	chunks := watch.MarkdownLines(text)
	var wrapped []string
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			wrapped = append(wrapped, "")
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

func bodyRows(kind, text string, width, inner int) []string {
	if kind == "user" {
		return userRows(text, width)
	}
	var chunks []string
	if watch.ProseKinds[kind] {
		chunks = watch.MarkdownLines(text)
	} else {
		chunks = []string{text}
	}
	var rows []string
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			rows = append(rows, "")
			continue
		}
		var wrapped []string
		if strings.HasPrefix(chunk, "│ ") {
			wrapped = hardWrap(chunk, inner)
		} else {
			wrapped = wrap(chunk, inner)
		}
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for _, row := range wrapped {
			rows = append(rows, "  "+row)
		}
	}
	if len(rows) == 0 {
		return []string{""}
	}
	return rows
}

type LogLine struct {
	Kind string
	Text string
	Fill string
}

func BuildLogView(log []watch.Event, width int) []LogLine {
	if width <= 0 {
		return nil
	}
	if len(log) == 0 {
		return []LogLine{{Kind: "idle", Text: "Ready. Watching for agent events."}}
	}
	type group struct {
		kind  string
		texts []string
	}
	var groups []group
	for _, entry := range log {
		if watch.IsFileLookEvent(entry) || watch.IsQuietResult(entry) {
			continue
		}
		kind, text := asLogEntry(entry)
		if n := len(groups); n > 0 && groups[n-1].kind == kind && kind != "text" && kind != "user" {
			groups[n-1].texts = append(groups[n-1].texts, text)
			continue
		}
		groups = append(groups, group{kind: kind, texts: []string{text}})
	}
	inner := max(1, width-2)
	var lines []LogLine
	for index, g := range groups {
		if index > 0 {
			lines = append(lines, LogLine{Kind: "gap", Text: ""})
		}
		if g.kind != "user" {
			label := kindLabel[g.kind]
			if label == "" {
				label = g.kind
			}
			lines = append(lines, LogLine{Kind: g.kind, Text: rule(label, width)})
		}
		for _, text := range g.texts {
			for _, row := range bodyRows(g.kind, text, width, inner) {
				lines = append(lines, LogLine{Kind: g.kind, Text: row})
			}
		}
	}
	return lines
}

func ExtendLog(log *[]watch.Event, incoming []watch.Event) []watch.Event {
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
	if len(*log) > MaxLog {
		*log = (*log)[len(*log)-MaxLog:]
	}
	return added
}

type Turn int

const (
	TurnIdle Turn = iota
	TurnThinking
	TurnRunning
)

func ApplyTurn(turn Turn, ev watch.Event) Turn {
	if ev.Source == "user" {
		return turn
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

func PaneBodyHeight(termLines int) int {
	return layoutFor(TextOnlyBelow, termLines).bodyH
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

func inputBox(draft string, width int, enabled bool, prefix string) []string {
	if prefix == "" {
		prefix = InputPrefix
	}
	inner := max(0, width-2)
	content := clip(prefix+visibleDraft(draft, inner-cellLen(prefix)), inner)
	top := "╭" + strings.Repeat("─", inner) + "╮"
	bot := "╰" + strings.Repeat("─", inner) + "╯"
	if width < 4 {
		out := make([]string, InputRows)
		for i := range out {
			out[i] = clip("", width)
		}
		return out
	}
	return []string{
		mascot.Paint(top, mascot.UI["border"], enabled),
		mascot.Paint("│", mascot.UI["border"], enabled) + mascot.Paint(content, mascot.UI["text"], enabled) + mascot.Paint("│", mascot.UI["border"], enabled),
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
	bodyTop := 2
	bodyBottom := height - FooterRows - l.composer
	bodyH := max(0, bodyBottom-bodyTop)
	var body []LogLine
	scroll := opt.Scroll
	fromTop := false
	if opt.Pane == PanePlans && opt.Plans != nil {
		body = BuildPlanView(opt.Plans, width)
		scroll = planWindowStart(body, bodyH)
		fromTop = true
	} else {
		body = opt.LogView
		if body == nil {
			body = BuildLogView(opt.Log, width)
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
		visible = body[start:end]
	}
	enabled := opt.Enabled
	rows := make([]string, height)
	rows[0] = headerRow(width, placeWord(opt.Pane), opt.Status, opt.PlanRel, enabled)
	if height > 1 {
		rows[1] = statusRow(opt.Line, width, enabled)
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
	for offset, line := range composerRows(l, opt.Draft, composerPrefix(opt), enabled) {
		rows[height-FooterRows-l.composer+offset] = line
	}
	if height > 2 {
		segs, kind := footerSegments(opt, scroll)
		if !l.typing {
			segs = withoutEnter(segs)
		}
		rows[len(rows)-1] = mascot.Paint(clip(fitFooter(segs, width), width), mascot.UI[kind], enabled)
	}
	return rows
}

// StatusLine is row 1: the single most important thing to know right now
// (docs/ux.md §4). Kind picks the color.
type StatusLine struct {
	Text string
	Kind string
}

func statusRow(s StatusLine, width int, enabled bool) string {
	if strings.TrimSpace(s.Text) == "" {
		return strings.Repeat(" ", width)
	}
	kind := s.Kind
	if _, ok := mascot.UI[kind]; !ok {
		kind = "muted"
	}
	return mascot.Paint(clip(s.Text, width), mascot.UI[kind], enabled)
}

func paintLine(line LogLine, width int, enabled bool) string {
	if line.Kind == "gap" {
		return strings.Repeat(" ", width)
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

func planWindowStart(body []LogLine, viewH int) int {
	if viewH <= 0 || len(body) <= viewH {
		return 0
	}
	sel := 0
	for i, line := range body {
		trimmed := strings.TrimLeft(line.Text, " ")
		if strings.HasPrefix(line.Text, "▌") || strings.HasPrefix(trimmed, "▌") || strings.HasPrefix(line.Text, "> ") {
			sel = i
			break
		}
	}
	start := sel - viewH + 1
	if start < 0 {
		start = 0
	}
	maxStart := len(body) - viewH
	if start > maxStart {
		start = maxStart
	}
	return start
}

type ComposeOptions struct {
	Enabled       bool
	Columns       int
	Lines         int
	AnimationName string
	Draft         string
	Log           []watch.Event
	QuitPending   bool
	Status        string
	Scroll        int
	Pane          PaneKind
	Plans         *PlanBrowser
	LogView       []LogLine // laid-out tape, when the caller already has it
	PlanRel       string    // open plan, repo-relative, for the header
	Line          StatusLine
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
		rows[0] = mascot.Paint(clip(fitFooter([]string{fmt.Sprintf("widen to %d cols", MinTextCols)}, columns), columns), mascot.UI["title"], opt.Enabled)
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

func handlePlanKey(b *PlanBrowser, draft *string, key string, page int) (bool, bool) {
	switch key {
	case "up":
		b.Move(-1)
		return true, false
	case "down":
		b.Move(1)
		return true, false
	case "pageup":
		b.Move(-max(1, page))
		return true, false
	case "pagedown":
		b.Move(max(1, page))
		return true, false
	case "home":
		b.Cursor = 0
		return true, false
	case "end":
		if n := len(b.Rows); n > 0 {
			b.Cursor = n - 1
		}
		return true, false
	case "left":
		return b.Up(), false
	case "esc":
		if b.Prompting() {
			b.CancelPrompt()
			*draft = ""
			return true, false
		}
		if b.Up() {
			*draft = ""
			return true, false
		}
		return true, true
	case "\r", "\n":
		if b.Screen == ScreenCreate {
			act := b.CommitCreate(*draft)
			if act == PlanOpenChat {
				*draft = ""
			}
			return true, act == PlanOpenChat
		}
		if b.Screen == ScreenFolder {
			_ = b.CommitFolder(*draft)
			*draft = ""
			return true, false
		}
		act := b.Activate()
		switch act {
		case PlanStartCreate:
			b.Screen = ScreenCreate
			*draft = ""
			return true, false
		case PlanStartFolder:
			b.Screen = ScreenFolder
			*draft = ""
			return true, false
		case PlanStartBrowse:
			*draft = ""
			return true, false
		case PlanOpenChat:
			return true, true
		default:
			return true, false
		}
	case "\x7f", "\x08":
		if *draft != "" {
			r := []rune(*draft)
			*draft = string(r[:len(r)-1])
			return true, false
		}
		if !b.Prompting() {
			return b.Up(), false
		}
		return false, false
	default:
		if isPrintableKey(key) && runeLen(*draft) < draftCap {
			*draft += key
			return true, false
		}
		return false, false
	}
}

func ComposerAnimation(text, current string) string {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "breakdance":
		return "breakdance"
	case "idle":
		return "idle"
	default:
		return current
	}
}

func RunLoop(color string, enabled bool, animation string, stdin, stdout *os.File, eventsPath string) (int, error) {
	columns, lines := Size()
	a := newApp(color, enabled, animation, NewPlanBrowser(""), columns, lines)
	path := eventsPath
	if path == "" {
		path = watch.DefaultEventPath()
	}
	tail := watch.NewEventTail(path, true)
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
			changed := a.ingest(tail.Poll())
			if a.keys(pressed) {
				changed = true
			}
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
