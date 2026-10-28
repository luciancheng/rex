package tui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// viewOpt is how AGENT's stream is laid out (docs/ux.md §10): tall
// (blankFrom rows and up) puts blank rows around prompts and before
// replies and asks. It keys the view cache.
type viewOpt struct {
	tall bool
}

const (
	// thinkFrom is the text width from which a thought run shows its
	// latest thought beside the label; narrower, only "thinking ×N".
	thinkFrom = 30
	// cmdCap is the most steps a run shows once it folds; the earlier ones
	// go into "… +N earlier". A run of cmdCap+1 shows whole, since its fold
	// would save no row.
	cmdCap = 4
	// errRows is the most rows a turn's own error takes.
	errRows = 3
)

// The stream's gutter (docs/ux.md §10): its two cells hold the only marks,
// your prompt's ›, a failed step's ✗ and your agent's question ?;
// everything else starts after it.
const (
	gutterPad  = "  "
	promptMark = "›"
	failMark   = "✗"
	askMark    = "?"
)

// block is one block of the stream: a run of thoughts or of steps, or a
// single event.
type block struct {
	kind string
	evs  []watch.Event
}

// BuildLogView lays out AGENT's stream (docs/ux.md §10): marks in the
// gutter, steps muted, a reply plain text, no rules. File looks and
// results stay hidden, and no file body is ever shown.
func BuildLogView(log []watch.Event, width int, opt viewOpt) []LogLine {
	if width <= 0 {
		return nil
	}
	if len(log) == 0 {
		return []LogLine{{Kind: "idle", Text: gutterPad + "Ready. Watching for agent events."}}
	}
	var blocks []block
	for _, ev := range log {
		if hiddenInStream(ev) {
			continue
		}
		if n := len(blocks); n > 0 && blocks[n-1].kind == ev.Kind && (ev.Kind == "thought" || ev.Kind == "command") {
			blocks[n-1].evs = append(blocks[n-1].evs, ev)
			continue
		}
		blocks = append(blocks, block{kind: ev.Kind, evs: []watch.Event{ev}})
	}
	var lines []LogLine
	for i, b := range blocks {
		if opt.tall && i > 0 && spaced(blocks[i-1].kind, b.kind) {
			lines = append(lines, LogLine{Kind: "gap"})
		}
		lines = append(lines, b.lines(width, opt)...)
	}
	return lines
}

// trimmedRows is how many rows of BuildLogView(log, width, opt) go when its
// first n events are trimmed. Only the head is laid out: from the first
// block after the one holding log[n], both layouts are the same rows under
// a block of the same kind.
func trimmedRows(log []watch.Event, n, width int, opt viewOpt) int {
	n = min(n, len(log))
	j := n
	for j < len(log) && hiddenInStream(log[j]) {
		j++
	}
	if j < len(log) {
		k := log[j].Kind
		for j++; j < len(log); j++ {
			if ev := log[j]; !hiddenInStream(ev) && (ev.Kind != k || (k != "thought" && k != "command")) {
				break
			}
		}
	}
	return len(BuildLogView(log[:j], width, opt)) - len(BuildLogView(log[n:j], width, opt))
}

// hiddenInStream says AGENT draws no block for ev (docs/ux.md §10): a file
// look, a result echo, a turn end, or a reply with nothing left to show
// once it is cleaned (only an escape sequence, an empty fence), which
// would otherwise be a blank row.
func hiddenInStream(ev watch.Event) bool {
	return !watch.KeepInPane(ev) || ev.Kind == "text" && emptyReply(ev.Text)
}

// emptyReply says a reply lays out to no rows, as replyLines reads it.
func emptyReply(text string) bool {
	for _, md := range watch.MarkdownBlocks(watch.CleanText(text)) {
		if strings.TrimSpace(md.Text) != "" {
			return false
		}
	}
	return true
}

// spaced says a blank row goes between two blocks (docs/ux.md §10): before
// a reply or an ask and around a prompt, never under the before-Rex-started
// line. Everything else packs.
func spaced(prev, next string) bool {
	return prev != "before" && (prev == "user" || next == "user" || next == "text" || next == "question")
}

func (b block) lines(width int, opt viewOpt) []LogLine {
	ev := b.evs[0]
	switch b.kind {
	case "thought":
		return []LogLine{thoughtRow(b.evs, width)}
	case "command":
		return stepRun(b.evs, width)
	case "error":
		return errorLines(ev, width)
	case "text":
		return replyLines(ev.Text, width, opt.tall)
	case "question":
		return askLines(ev.Text, width)
	case "user":
		return promptLines(ev, width, opt.tall)
	case "plan":
		return []LogLine{rexLine(squash(ev.Text), width)}
	case "drift":
		return []LogLine{driftStep(ev, width)}
	case "before":
		// Rex's own line, from no text: the warm start's (docs/ux.md §10).
		return []LogLine{{Kind: "before", Text: ellipsis(gutterPad+"before Rex started", width)}}
	}
	return []LogLine{{Kind: b.kind, Text: ellipsis(gutterPad+squash(ev.Text), width)}}
}

// tagRoom is the cells a drift tag takes at a row's right end: "off pin?",
// two blanks before it and the margin cell after it.
var tagRoom = cellLen(softTag) + 3

// The drift tags (docs/ux.md §9a, §10): hard drift in the pin's yellow,
// since it is off the pin, soft drift muted; never an error's red.
const (
	hardTag = "off pin"
	softTag = "off pin?"
)

// tagSpan is a drift level as its tag, or nothing on the pin.
func tagSpan(level plan.DriftLevel) Span {
	switch level {
	case plan.Hard:
		return Span{Text: hardTag, Role: "accent"}
	case plan.Soft:
		return Span{Text: softTag, Role: "muted"}
	}
	return Span{}
}

// withTag is left with its drift's tag at the row's right end, a cell
// short of the edge, the tag in its own color, and before it, muted, why
// (docs/ux.md §10): "next to chrome.go · off pin?". left is already
// fitted to the room the tag leaves (tagRoom), and never gives up a cell
// for why: why takes what is free between them, at least two blanks after
// left, and clips; with fewer than whyMin cells it goes, and the tag
// stands alone. With no tag, left is the row.
func withTag(kind string, left []Span, t driftTag, width int) LogLine {
	tag := tagSpan(t.level)
	if tag.Text == "" || width-tagRoom < 1 {
		return spanLine(kind, cutSpans(left, width)...)
	}
	left = cutSpans(left, width-tagRoom)
	end := width - 1 - cellLen(tag.Text) // the tag's first cell
	if why := fitWhy(squash(t.why), end-spansLen(left)-2-cellLen(whySep)); why != "" {
		left = append(padSpans(left, end-cellLen(whySep)-cellLen(why)), Span{Text: why + whySep, Role: "muted"})
	}
	return spanLine(kind, append(padSpans(left, end), tag)...)
}

// whySep joins a drift's reason to its tag.
const whySep = " · "

// whyMin is the fewest cells of a reason worth showing cut: fewer, and the
// tag stands alone ("outside… · off pin" says nothing).
const whyMin = 12

// fitWhy is why in room cells: whole, else cut with "…" while whyMin cells
// of it show (a cut can come out short of room: a wide glyph it can't
// split, the blanks it trims), else nothing.
func fitWhy(why string, room int) string {
	if why == "" || cellLen(why) <= room {
		return why
	}
	if s := ellipsis(why, room); cellLen(s) >= whyMin {
		return s
	}
	return ""
}

// squash is agent text as one clean line, its whitespace collapsed.
func squash(s string) string {
	return strings.Join(strings.Fields(watch.CleanText(s)), " ")
}

// proseLine is prose as one clean line: Markdown stripped, code marks
// dropped, whitespace collapsed.
func proseLine(s string) string {
	parts := mdLines(watch.CleanText(s))
	for i, p := range parts {
		parts[i] = strings.TrimPrefix(p, "│ ")
	}
	return squash(strings.Join(parts, " "))
}

// listMark is a Markdown list item's lead, as the stream writes it.
var listMark = regexp.MustCompile(`^\s*(· |\d+\. )`)

// bullet is a Markdown line with its list item's "• " as "· " (docs/ux.md
// §5: the stream's one bullet is the middle dot).
func bullet(line string) string {
	body := strings.TrimLeft(line, " ")
	if rest, ok := strings.CutPrefix(body, "• "); ok {
		return line[:len(line)-len(body)] + "· " + rest
	}
	return line
}

// mdLines is watch.MarkdownLines with the stream's bullet.
func mdLines(text string) []string {
	lines := watch.MarkdownLines(text)
	for i, l := range lines {
		lines[i] = bullet(l)
	}
	return lines
}

// hang wraps text after lead into rows of width cells, the later rows
// indented by indent cells; with most > 0 it keeps that many rows, the last
// ending in "…" when text is left over. text is single-spaced (squash).
func hang(lead, text string, width, indent, most int) []string {
	if most == 1 {
		return []string{ellipsis(strings.TrimRight(lead+text, " "), width)}
	}
	first := wrap(text, max(1, width-cellLen(lead)))[0]
	out := []string{strings.TrimRight(lead+first, " ")}
	rest := strings.TrimSpace(strings.TrimPrefix(text, first))
	if rest == "" {
		return out
	}
	more := wrap(rest, max(1, width-indent))
	if most > 1 && len(more) > most-1 {
		keep := most - 1
		more[keep-1] = ellipsis(more[keep-1]+" "+more[keep], width-indent)
		more = more[:keep]
	}
	pad := strings.Repeat(" ", indent)
	for _, m := range more {
		out = append(out, pad+m)
	}
	return out
}

// thoughtRow is a run of thoughts (docs/ux.md §10): one muted row,
// "thinking ×N · <latest>" ("thinking · <text>" for one). Under thinkFrom
// cells only the label shows. The fold is display only: the feed keeps
// every thought.
func thoughtRow(evs []watch.Event, width int) LogLine {
	label := "thinking"
	if len(evs) > 1 {
		label = fmt.Sprintf("thinking ×%d", len(evs))
	}
	row := gutterPad + label
	if latest := proseLine(evs[len(evs)-1].Text); width >= thinkFrom && latest != "" {
		row += " · " + latest
	}
	return LogLine{Kind: "thought", Text: ellipsis(row, width)}
}

// stepRun is a run of the agent's steps (docs/ux.md §10), one muted row
// each, a drift tag at the end of an edit off the pin. Past cmdCap+1, the
// earlier ones fold into "… +N earlier", which takes the worst tag of the
// steps it hides, so a fold always hides at least two, takes at most
// cmdCap+1 rows, and never hides drift.
func stepRun(evs []watch.Event, width int) []LogLine {
	var lines []LogLine
	if n := len(evs) - cmdCap; len(evs) > cmdCap+1 {
		worst := plan.OnPin
		for _, ev := range evs[:n] {
			worst = max(worst, driftOf(ev))
		}
		// The fold keeps the worst tag it hides, not a reason: whose would it be?
		lines = append(lines, stepRow(fmt.Sprintf("… +%d earlier", n), "", driftTag{level: worst}, width))
		evs = evs[n:]
	}
	for _, ev := range evs {
		verb, what := stepText(ev)
		lines = append(lines, stepRow(verb, what, tagOf(ev), width))
	}
	return lines
}

// stepText is one step as its row reads it (docs/ux.md §10): a shell
// command after "$" (any command no tool names too), a change to a file
// "edit <path>" (a write, a patch, a delete: a step doesn't say which),
// and any other tool as its adapter wrote it. path is what follows the
// verb, a path that clips from the left.
func stepText(ev watch.Event) (verb, path string) {
	tool := watch.MetaTool(ev)
	line := squash(ev.Text)
	switch {
	case tool == "" || watch.IsShellTool(tool):
		return "$ " + line, ""
	case watch.IsFileEditTool(tool) && len(watch.EventPaths(ev)) > 0:
		_, rest, _ := strings.Cut(line, " ")
		return "edit", rest
	}
	return line, ""
}

// stepRow is one step's row: muted, after the gutter, text then path
// (which keeps its file name as it clips), and its drift tag with why.
func stepRow(text, path string, tag driftTag, width int) LogLine {
	room := width - cellLen(gutterPad)
	if tagSpan(tag.level).Text != "" && width-tagRoom >= 1 {
		room = width - tagRoom - cellLen(gutterPad)
	}
	row := ellipsis(text, max(1, room))
	if path != "" {
		if lead := text + " "; cellLen(lead) < room {
			row = lead + pathEllipsis(path, room-cellLen(lead))
		}
	}
	return withTag("command", []Span{{Text: gutterPad + row}}, tag, width)
}

// driftStep is an edit off the pin whose step AGENT doesn't show (a
// harness that reported only its completion): Rex draws the step from the
// drift's own meta, never from text (docs/ux.md §9a).
func driftStep(ev watch.Event, width int) LogLine {
	rel, _ := ev.Meta["rel"].(string)
	return stepRow("edit", squash(rel), tagOf(ev), width)
}

// errorLines is an error (docs/ux.md §10), ✗ in the gutter. A failed tool
// call, after which the turn goes on, is one muted step row, "✗ <what> ·
// <why>", only the ✗ red. The turn's own error is "✗ error · <text>" in
// red over up to errRows rows.
func errorLines(ev watch.Event, width int) []LogLine {
	if watch.MetaTool(ev) == "" {
		var lines []LogLine
		for _, row := range hang(failMark+" error · ", watch.TurnErrorText(ev), width, 2, errRows) {
			lines = append(lines, LogLine{Kind: "error", Text: row})
		}
		return lines
	}
	what, why, path := watch.FailParts(ev)
	row := whatWhy("", what, why, max(1, width-2), path)
	return []LogLine{spanLine("error", Span{Text: failMark, Role: "error"}, Span{Text: " " + row, Role: "muted"})}
}

// whatWhy is one row, "<lead><what> · <why>" (docs/ux.md §10): when it
// doesn't fit, what clips first and why keeps at least half the row. A
// path clips from the left, so its file name stays; anything else from
// the right.
func whatWhy(lead, what, why string, width int, path bool) string {
	fit := ellipsis
	if path {
		fit = pathEllipsis
	}
	if why == "" {
		return lead + fit(what, max(1, width-cellLen(lead)))
	}
	tail := " · " + ellipsis(why, max(width/2, width-cellLen(lead+what+" · ")))
	return lead + fit(what, max(1, width-cellLen(lead)-cellLen(tail))) + tail
}

// pathSeps are where pathEllipsis cuts: "/", and the OS's own, which a
// path outside the repo and home keeps (watch.Scope.Display).
const pathSeps = "/" + string(filepath.Separator)

// pathEllipsis is a path cut to width cells from the left: "…/" and as
// many of its last parts as fit, else its file name alone, else "…" and
// the name's tail.
func pathEllipsis(p string, width int) string {
	if cellLen(p) <= width {
		return p
	}
	out := ""
	for i := strings.LastIndexAny(p, pathSeps); i > 0; i = strings.LastIndexAny(p[:i], pathSeps) {
		if cellLen("…"+p[i:]) > width {
			break
		}
		out = "…" + p[i:]
	}
	switch base := p[strings.LastIndexAny(p, pathSeps)+1:]; {
	case out != "":
		return out
	case cellLen(base) <= width:
		return base
	case width <= 1:
		return cutCells("…", width)
	default:
		return "…" + tailCells(base, width-1)
	}
}

// replyLines is a reply (docs/ux.md §10): its Markdown after the gutter in
// plain text, no rule over it: headings bold, code muted "│ " lines, and a
// list item's later rows hang under its text. Short screens drop its
// blank rows.
func replyLines(text string, width int, tall bool) []LogLine {
	var lines []LogLine
	inner := max(1, width-2)
	for _, md := range watch.MarkdownBlocks(watch.CleanText(text)) {
		md.Text = bullet(md.Text)
		switch {
		case strings.TrimSpace(md.Text) == "":
			if tall && len(lines) > 0 {
				lines = append(lines, LogLine{Kind: "text"})
			}
		case strings.HasPrefix(md.Text, "│ "):
			for _, row := range hardWrap(md.Text, inner) {
				lines = append(lines, spanLine("text", Span{Text: gutterPad + row, Role: "muted"}))
			}
		default:
			rows := wrap(md.Text, inner)
			if m := listMark.FindString(md.Text); m != "" {
				rows = hang(m, squash(md.Text[len(m):]), inner, cellLen(m), 0)
			}
			for _, row := range rows {
				lines = append(lines, spanLine("text", Span{Text: gutterPad}, Span{Text: row, Bold: md.Heading}))
			}
		}
	}
	for len(lines) > 0 && lines[len(lines)-1].Text == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// askLines is your agent asking (docs/ux.md §10): a bold ? in the gutter,
// then the question, wrapped after the gutter.
func askLines(text string, width int) []LogLine {
	var lines []LogLine
	for i, row := range wrap(squash(text), max(1, width-2)) {
		lead := Span{Text: gutterPad}
		if i == 0 {
			lead = Span{Text: askMark + " ", Role: "text", Bold: true}
		}
		lines = append(lines, spanLine("question", lead, Span{Text: row, Role: "text"}))
	}
	return lines
}

// promptLines is your prompt (docs/ux.md §10): a muted › in the gutter
// and its text after it, left-aligned, its paragraphs kept, capped at
// maxPromptRows; its drift tag on its first row, which always leaves the
// tag's room so a tag never moves a row; and under it, after the gutter,
// what it carried to your agent (carriedTag): "with pin <id>" muted with
// the id in the pin's yellow, or "with plan <name>". Only a prompt rex
// ingest stamped has that row, so it never claims more than happened (§7a).
func promptLines(ev watch.Event, width int, tall bool) []LogLine {
	inner := max(1, width-2)
	first := inner
	if width-tagRoom >= 2+promptMin {
		first = width - tagRoom - 2
	}
	rows, more := promptRows(watch.CleanText(ev.Text), first, inner, tall)
	var lines []LogLine
	for i, row := range rows {
		switch {
		case i == 0:
			left := []Span{{Text: promptMark + " ", Role: "muted"}, {Text: row}}
			lines = append(lines, withTag("user", left, tagOf(ev), width))
		case row == "":
			lines = append(lines, LogLine{Kind: "user"})
		default:
			lines = append(lines, LogLine{Kind: "user", Text: gutterPad + row})
		}
	}
	if more > 0 {
		lines = append(lines, spanLine("user", Span{Text: ellipsis(fmt.Sprintf("%s(+%d more rows)", gutterPad, more), width), Role: "muted"}))
	}
	lead, what := carriedTag(ev)
	if what == "" {
		return lines
	}
	shown := ellipsis(lead+what, inner)
	spans := []Span{{Text: gutterPad}, {Text: shown, Role: "muted"}}
	if id, ok := strings.CutPrefix(shown, lead); ok && lead == "with pin " {
		spans = []Span{spans[0], {Text: lead, Role: "muted"}, {Text: id, Role: "accent"}}
	}
	return append(lines, spanLine("user", spans...))
}

// promptMin is the fewest cells a prompt's first row keeps beside the
// room it leaves for a drift tag; narrower, the row takes it all and the
// tag clips the row.
const promptMin = 10

// promptRows is a prompt's text as rows (docs/ux.md §10): its first row
// first cells wide, the rest width cells; its Markdown paragraphs kept,
// blank rows between them only when tall; code hard-wrapped. It keeps
// maxPromptRows rows and says how many more there were.
func promptRows(text string, first, width int, tall bool) (rows []string, more int) {
	for _, chunk := range mdLines(text) {
		code := strings.HasPrefix(chunk, "│ ")
		if !code {
			chunk = strings.Join(strings.Fields(chunk), " ")
		}
		if chunk == "" {
			if tall && len(rows) > 0 {
				rows = append(rows, "")
			}
			continue
		}
		if len(rows) == 0 && first < width {
			// The first row leaves room for a drift tag; the rest of its
			// paragraph wraps to the full width.
			if code {
				head := hardWrap(chunk, first)[0]
				rows = append(rows, head)
				chunk = chunk[len(head):]
			} else {
				head := wrap(chunk, first)[0]
				rows = append(rows, head)
				chunk = strings.TrimSpace(strings.TrimPrefix(chunk, head))
			}
			if chunk == "" {
				continue
			}
		}
		if code {
			rows = append(rows, hardWrap(chunk, width)...)
		} else {
			rows = append(rows, wrap(chunk, width)...)
		}
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	if len(rows) == 0 {
		return []string{""}, 0
	}
	if len(rows) > maxPromptRows {
		return rows[:maxPromptRows], len(rows) - maxPromptRows
	}
	return rows, 0
}

// rexLine is one of Rex's own lines (docs/ux.md §10): "rex · <text>",
// muted, after the gutter. They never come from or go to the feed.
func rexLine(text string, width int) LogLine {
	return LogLine{Kind: "plan", Text: ellipsis(gutterPad+"rex · "+text, width)}
}
