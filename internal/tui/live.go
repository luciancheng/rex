package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

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

func clip(text string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(text)
	if len(r) <= width {
		return string(r) + strings.Repeat(" ", width-len(r))
	}
	return string(r[:width])
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
		if runeLen(candidate) <= width {
			current = candidate
			continue
		}
		lines = append(lines, current)
		current = word
	}
	lines = append(lines, current)
	var wrapped []string
	for _, line := range lines {
		if runeLen(line) <= width {
			wrapped = append(wrapped, line)
			continue
		}
		r := []rune(line)
		for i := 0; i < len(r); i += width {
			end := i + width
			if end > len(r) {
				end = len(r)
			}
			wrapped = append(wrapped, string(r[i:end]))
		}
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
	r := []rune(text)
	var out []string
	for i := 0; i < len(r); i += width {
		end := i + width
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[i:end]))
	}
	return out
}

func asLogEntry(event watch.Event) (string, string) {
	if watch.ProseKinds[event.Kind] {
		return event.Kind, event.Text
	}
	return event.Kind, event.DisplayText()
}

func rule(label string, width int) string {
	prefix := "── " + label + " "
	if runeLen(prefix) >= width {
		return clip(label, width)
	}
	return prefix + strings.Repeat("─", width-runeLen(prefix))
}

func padRight(s string, width int) string {
	n := runeLen(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
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
		if n := runeLen(line); n > contentW {
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
	case "text", "error", "idle":
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
		if s, ok := kindStatus[lastKind]; ok && lastKind != "user" {
			return s
		}
		return "running"
	default:
		if s, ok := kindStatus[lastKind]; ok {
			return s
		}
		return mascot.DefaultAnimation
	}
}

func PaneBodyHeight(termLines int) int {
	showBox := termLines >= InputRows+FooterRows+3
	height := termLines - 2 - FooterRows
	if showBox {
		height -= InputRows
	}
	return max(0, height)
}

func PaneTextWidth(columns int) int {
	mascotCols := min(mascot.StageSize, max(1, columns))
	textWidth := max(1, columns) - mascotCols - Gutter
	if textWidth < MinTextCols {
		return 0
	}
	return textWidth
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

func header(width int, meta string, enabled bool) string {
	left := "REX"
	if width < runeLen(left) {
		return mascot.Paint(clip(left, width), mascot.UI["title"], enabled)
	}
	room := width - runeLen(left) - 1
	if room < 1 {
		return mascot.Paint(clip(left, width), mascot.UI["title"], enabled)
	}
	if runeLen(meta) > room {
		meta = clipPathLeft(meta, room)
	}
	gap := width - runeLen(left) - runeLen(meta)
	if gap < 1 {
		gap = 1
	}
	return mascot.Paint(left, mascot.UI["title"], enabled) + strings.Repeat(" ", gap) + mascot.Paint(meta, mascot.UI["muted"], enabled)
}

func chatHeaderMeta(state, planRel string, width int) string {
	state = strings.TrimSpace(state)
	planRel = strings.TrimSpace(planRel)
	if state == "" {
		state = "idle"
	}
	if planRel == "" {
		return state
	}
	meta := state + " · " + planRel
	if width <= 0 {
		return meta
	}
	room := max(8, width-5)
	if runeLen(meta) <= room {
		return meta
	}
	keep := room - runeLen(state) - 3
	if keep < 8 {
		r := []rune(state + " · " + planRel)
		if len(r) > room {
			return string(r[:room])
		}
		return string(r)
	}
	return state + " · " + clipPathLeft(planRel, keep)
}

func clipPathLeft(path string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(path)
	if len(r) <= width {
		return path
	}
	if width <= 1 {
		return "…"
	}
	base := path
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		base = path[i+1:]
	}
	br := []rune(base)
	if 2+len(br) <= width {
		return "…/" + base
	}
	return "…" + string(r[len(r)-(width-1):])
}

func inputBox(draft string, width int, enabled bool, prefix string) []string {
	if prefix == "" {
		prefix = InputPrefix
	}
	inner := max(0, width-2)
	room := max(0, inner-runeLen(prefix))
	visible := draft
	if room == 0 {
		visible = ""
	} else {
		r := []rune(draft)
		if len(r) > room {
			visible = string(r[len(r)-room:])
		}
	}
	content := clip(prefix+visible, inner)
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

func textColumn(width, height int, meta string, opt ComposeOptions) []string {
	if width <= 0 || height <= 0 {
		out := make([]string, max(0, height))
		for i := range out {
			out[i] = ""
		}
		return out
	}
	showBox := height >= InputRows+FooterRows+3
	bodyTop := 2
	bodyBottom := height - FooterRows
	if showBox {
		bodyBottom -= InputRows
	}
	bodyH := max(0, bodyBottom-bodyTop)
	var body []LogLine
	scroll := opt.Scroll
	fromTop := false
	if opt.Pane == PanePlans && opt.Plans != nil {
		body = BuildPlanView(opt.Plans, width)
		scroll = planWindowStart(body, bodyH)
		fromTop = true
	} else {
		body = BuildLogView(opt.Log, width)
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
	var footerText string
	var footerColor mascot.RGB
	if opt.Pane == PanePlans {
		footerText, footerColor = planFooter(opt.Plans, opt.QuitPending)
	} else if opt.QuitPending {
		footerText = "press ctrl+c again to exit"
		footerColor = mascot.UI["error"]
	} else if scroll > 0 {
		footerText = fmt.Sprintf("↑↓ %d above live · ctrl+e bottom · ctrl+c twice", scroll)
		footerColor = mascot.UI["muted"]
	} else {
		footerText = "↑↓ scroll · tab plans · ctrl+c twice"
		footerColor = mascot.UI["muted"]
	}
	enabled := opt.Enabled
	rows := make([]string, height)
	rows[0] = header(width, meta, enabled)
	if height > 1 {
		rows[1] = strings.Repeat(" ", width)
	}
	for offset, line := range visible {
		if line.Kind == "gap" {
			rows[bodyTop+offset] = strings.Repeat(" ", width)
			continue
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
			rows[bodyTop+offset] = mascot.PaintFill(text, rgb, fill, enabled)
			continue
		}
		rows[bodyTop+offset] = mascot.Paint(text, rgb, enabled)
	}
	for index := bodyTop; index < bodyBottom; index++ {
		if rows[index] == "" {
			rows[index] = strings.Repeat(" ", width)
		}
	}
	if showBox {
		box := inputBox(opt.Draft, width, enabled, composerPrefix(opt))
		boxStart := height - FooterRows - InputRows
		for offset, line := range box {
			rows[boxStart+offset] = line
		}
	}
	rows[len(rows)-1] = mascot.Paint(clip(footerText, width), footerColor, enabled)
	return rows
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
}

func ComposeFrame(spriteName, color string, opt ComposeOptions) string {
	columns := max(1, opt.Columns)
	lines := max(1, opt.Lines)
	mascotCols := min(mascot.StageSize, columns)
	textWidth := columns - mascotCols - Gutter
	if textWidth < MinTextCols {
		textWidth = 0
		mascotCols = min(mascot.StageSize, columns)
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
	pal := mascot.MustPalette(color)
	mascotRows := mascot.MustRenderRows(staged, pal, opt.Enabled)
	blankMascot := strings.Repeat(" ", mascotCols)
	status := opt.Status
	if status == "" {
		status = opt.AnimationName
	}
	text := textColumn(textWidth, lines, status, opt)
	out := make([]string, 0, lines)
	for index := 0; index < lines; index++ {
		left := blankMascot
		if index < len(mascotRows) {
			left = mascotRows[index]
		}
		if textWidth <= 0 {
			out = append(out, left)
			continue
		}
		out = append(out, left+strings.Repeat(" ", Gutter)+text[index])
	}
	frame := Home + Clear + strings.Join(out, "\r\n")
	if textWidth >= 4 && lines >= InputRows+FooterRows+3 {
		prefix := composerPrefix(opt)
		inner := textWidth - 2
		room := max(0, inner-runeLen(prefix))
		visible := opt.Draft
		if room == 0 {
			visible = ""
		} else {
			r := []rune(opt.Draft)
			if len(r) > room {
				visible = string(r[len(r)-room:])
			}
		}
		cursorRow := lines - 2
		cursorCol := mascotCols + Gutter + 1 + 1 + runeLen(prefix) + runeLen(visible)
		frame += fmt.Sprintf("\033[%d;%dH%s", cursorRow, cursorCol, ShowCursor)
	} else {
		frame += HideCursor
	}
	return frame
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
		if isPrintableKey(key) && runeLen(*draft) < 256 {
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
	sequence := mascot.MustAnimation(animation)
	frameIndex := 0
	hold := sequence.Frames[0].Hold
	spriteName := sequence.Frames[0].Sprite
	columns, lines := Size()
	animated := len(sequence.Frames) > 1
	draft := ""
	chatDraft := ""
	var log []watch.Event
	quitPending := false
	quitTicks := 0
	status := sequence.Name
	lastKind := ""
	scroll := 0
	pane := PanePlans
	plans := NewPlanBrowser("")
	path := eventsPath
	if path == "" {
		path = watch.DefaultEventPath()
	}
	tail := watch.NewEventTail(path, true)
	turn := TurnIdle
	code := 0
	err := LiveScreen(stdout, stdin, func() error {
		play := func(name string) {
			if sequence.Name == name {
				return
			}
			sequence = mascot.MustAnimation(name)
			frameIndex = 0
			hold = sequence.Frames[0].Hold
			spriteName = sequence.Frames[0].Sprite
			animated = len(sequence.Frames) > 1
		}
		syncTurn := func() {
			want := AnimationForTurn(turn)
			if sequence.Name == "breakdance" {
				return
			}
			play(want)
		}
		draw := func() {
			meta := status
			if pane == PanePlans {
				meta = "plans"
				switch plans.Screen {
				case ScreenCreate:
					meta = "new plan"
				case ScreenFolder:
					meta = "new folder"
				case ScreenBrowse:
					meta = "open"
				}
			} else {
				meta = chatHeaderMeta(statusForTurn(turn, lastKind), plans.CurrentRel(), PaneTextWidth(columns))
			}
			_, _ = io.WriteString(stdout, ComposeFrame(spriteName, color, ComposeOptions{
				Enabled:       enabled,
				Columns:       columns,
				Lines:         lines,
				AnimationName: sequence.Name,
				Draft:         draft,
				Log:           log,
				QuitPending:   quitPending,
				Status:        meta,
				Scroll:        scroll,
				Pane:          pane,
				Plans:         plans,
			}))
			_ = stdout.Sync()
		}
		draw()
		for {
			key, haveKey := ReadKey(stdin, time.Duration(FrameTick)*time.Millisecond)
			changed := false
			fresh := tail.Poll()
			if len(fresh) > 0 {
				textW := PaneTextWidth(columns)
				before := 0
				if textW > 0 {
					before = len(BuildLogView(log, textW))
				}
				prevTurn := turn
				for _, ev := range fresh {
					turn = ApplyTurn(turn, ev)
					if watch.KeepInPane(ev) {
						lastKind = ev.Kind
					}
				}
				added := ExtendLog(&log, fresh)
				after := 0
				if textW > 0 {
					after = len(BuildLogView(log, textW))
				}
				if len(added) > 0 && scroll > 0 {
					scroll += max(0, after-before)
				}
				if len(added) > 0 || turn != prevTurn {
					status = statusForTurn(turn, lastKind)
					syncTurn()
					changed = true
				}
			}
			textW, bodyH := PaneTextWidth(columns), PaneBodyHeight(lines)
			totalLines := 0
			if textW > 0 {
				totalLines = len(BuildLogView(log, textW))
			}
			maxScroll := ClampScroll(totalLines, bodyH, totalLines)
			if haveKey {
				switch {
				case key == CtrlC:
					if quitPending {
						code = 0
						return nil
					}
					quitPending = true
					quitTicks = QuitWindowTicks
					changed = true
				case key == "\t":
					quitPending = false
					if pane == PanePlans {
						if plans.Browsing() {
							plans.Filter = draft
						}
						pane = PaneChat
						draft = chatDraft
					} else {
						chatDraft = draft
						pane = PanePlans
						draft = ""
						if plans.Browsing() {
							draft = plans.Filter
						}
						plans.Rebuild()
					}
					changed = true
				case pane == PanePlans:
					quitPending = false
					var openChat bool
					changed, openChat = handlePlanKey(plans, &draft, key, bodyH)
					if openChat {
						pane = PaneChat
						draft = chatDraft
					}
				default:
					switch key {
					case "up":
						scroll = min(maxScroll, scroll+1)
						changed = true
					case "down":
						scroll = max(0, scroll-1)
						changed = true
					case "pageup":
						scroll = min(maxScroll, scroll+max(1, bodyH))
						changed = true
					case "pagedown":
						scroll = max(0, scroll-max(1, bodyH))
						changed = true
					case "home":
						scroll = maxScroll
						changed = true
					case "end":
						scroll = 0
						changed = true
					case "\r", "\n":
						quitPending = false
						if strings.TrimSpace(draft) != "" {
							text := strings.TrimSpace(draft)
							next := ComposerAnimation(text, sequence.Name)
							if next != sequence.Name {
								if next == mascot.DefaultAnimation {
									turn = TurnIdle
								}
								play(next)
								status = sequence.Name
							}
							log = append(log, watch.NewEvent("user", "text", text, nil))
							draft = ""
							changed = true
						}
					case "\x7f", "\x08":
						quitPending = false
						if draft != "" {
							r := []rune(draft)
							draft = string(r[:len(r)-1])
							changed = true
						}
					default:
						if isPrintableKey(key) {
							quitPending = false
							if runeLen(draft) < 256 {
								draft += key
								changed = true
							}
						} else if quitPending {
							quitPending = false
							changed = true
						}
					}
				}
			} else {
				if quitPending {
					quitTicks--
					if quitTicks <= 0 {
						quitPending = false
						changed = true
					}
				}
				if animated {
					var finished bool
					frameIndex, hold, finished = advance(sequence.Frames, frameIndex, hold, animationLoops(sequence.Name))
					if finished {
						play(AnimationForTurn(turn))
						status = statusForTurn(turn, "")
						changed = true
					} else {
						nextName := sequence.Frames[frameIndex].Sprite
						if nextName != spriteName {
							spriteName = nextName
							changed = true
						}
					}
				}
			}
			if pane == PanePlans && plans.Browsing() && plans.Filter != draft {
				plans.Filter = draft
				plans.Rebuild()
				changed = true
			}
			newColumns, newLines := Size()
			if newColumns != columns || newLines != lines {
				columns, lines = newColumns, newLines
				changed = true
			}
			textW, bodyH = PaneTextWidth(columns), PaneBodyHeight(lines)
			totalLines = 0
			if textW > 0 {
				totalLines = len(BuildLogView(log, textW))
			}
			clamped := ClampScroll(totalLines, bodyH, scroll)
			if clamped != scroll {
				scroll = clamped
				changed = true
			}
			if changed {
				draw()
			}
		}
	})
	return code, err
}
