package tui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// viewOpt is how AGENT's stream is laid out (docs/ux.md §10): live (the
// turn is in flight) lets the newest block, when it is a thought run, go
// on over a second row, and tall (agentTitleRows rows and up) puts blank
// rows between blocks. Both key the view cache, so a turn that ends
// settles the run with no new event.
type viewOpt struct {
	live, tall bool
}

const (
	// thinkFrom is the text width from which a thought run shows its
	// latest thought beside the label; narrower, only "thinking ×N".
	thinkFrom = 30
	// cmdCap is the most commands a run shows once it folds; the earlier
	// ones go into "$ … +N earlier". A run of cmdCap+1 shows whole, since
	// its fold would save no row.
	cmdCap = 4
	// errRows is the most rows a turn's own error takes.
	errRows = 3
)

// block is one block of the stream: a run of thoughts or of commands, or
// a single event.
type block struct {
	kind string
	evs  []watch.Event
}

// BuildLogView lays out AGENT's stream (docs/ux.md §10). A block is named
// by its first characters (thinking, $, ✗, [ASK], rex ›); only a reply and
// the before-rex-started line have a rule. File looks and results stay
// hidden, and no file body is ever shown.
func BuildLogView(log []watch.Event, width int, opt viewOpt) []LogLine {
	if width <= 0 {
		return nil
	}
	if len(log) == 0 {
		return []LogLine{{Kind: "idle", Text: "Ready. Watching for agent events."}}
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
		lines = append(lines, b.lines(width, opt, i == len(blocks)-1)...)
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
	if j < len(log) {
		opt.live = false // the head's last block isn't the newest
	}
	return len(BuildLogView(log[:j], width, opt)) - len(BuildLogView(log[n:j], width, opt))
}

// hiddenInStream says AGENT draws no block for ev (docs/ux.md §10): a file
// look, a result echo or a turn end.
func hiddenInStream(ev watch.Event) bool {
	return !watch.KeepInPane(ev)
}

// spaced says a blank row goes between two blocks (docs/ux.md §10): before
// a reply or an ask and around a prompt, never under the before-rex-started
// line. Everything else packs.
func spaced(prev, next string) bool {
	return prev != "before" && (prev == "user" || next == "user" || next == "text" || next == "question")
}

func (b block) lines(width int, opt viewOpt, newest bool) []LogLine {
	ev := b.evs[0]
	switch b.kind {
	case "thought":
		rows := 1
		if opt.live && opt.tall && newest {
			rows = 2
		}
		return thoughtRun(b.evs, width, rows)
	case "command":
		return commandRun(b.evs, width)
	case "error":
		return errorLines(ev, width)
	case "text":
		return replyLines(ev.Text, width, opt.tall)
	case "question":
		return askLines(ev.Text, width)
	case "user":
		var lines []LogLine
		for _, row := range userRows(watch.CleanText(ev.Text), width, opt.tall) {
			lines = append(lines, LogLine{Kind: "user", Text: row})
		}
		return lines
	case "plan":
		return []LogLine{rexLine("plan", "plan · "+squash(ev.Text), width)}
	case "drift":
		return driftLines(ev, width)
	case "before":
		return []LogLine{{Kind: "before", Text: rule(ev.Text, width)}}
	}
	return []LogLine{{Kind: b.kind, Text: ellipsis(squash(ev.Text), width)}}
}

// squash is agent text as one clean line, its whitespace collapsed.
func squash(s string) string {
	return strings.Join(strings.Fields(watch.CleanText(s)), " ")
}

// proseLine is prose as one clean line: Markdown stripped, code marks
// dropped, whitespace collapsed.
func proseLine(s string) string {
	parts := watch.MarkdownLines(watch.CleanText(s))
	for i, p := range parts {
		parts[i] = strings.TrimPrefix(p, "│ ")
	}
	return squash(strings.Join(parts, " "))
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

// thoughtRun is a run of thoughts (docs/ux.md §10): "thinking ×N" with the
// latest thought beside it ("thinking" alone for one), in one row; a live
// run goes on over rows rows. Under thinkFrom cells only the label shows.
// The fold is display only: the feed keeps every thought.
func thoughtRun(evs []watch.Event, width, rows int) []LogLine {
	label := "thinking"
	if len(evs) > 1 {
		label = fmt.Sprintf("thinking ×%d", len(evs))
	}
	if width < thinkFrom {
		return []LogLine{spanLine("thought", Span{Text: ellipsis(label, width), Bold: true})}
	}
	var lines []LogLine
	for i, row := range hang(label+"  ", proseLine(evs[len(evs)-1].Text), width, 2, rows) {
		if i == 0 {
			lines = append(lines, spanLine("thought", Span{Text: label, Bold: true}, Span{Text: row[len(label):]}))
			continue
		}
		lines = append(lines, LogLine{Kind: "thought", Text: row})
	}
	return lines
}

// commandRun is a run of commands, one clipped row each (docs/ux.md §10).
// Past cmdCap+1, the earlier ones fold into a muted "$ … +N earlier", so a
// fold always hides at least two and a run takes at most cmdCap+1 rows.
func commandRun(evs []watch.Event, width int) []LogLine {
	var lines []LogLine
	if n := len(evs) - cmdCap; len(evs) > cmdCap+1 {
		lines = append(lines, spanLine("command", Span{Text: ellipsis(fmt.Sprintf("$ … +%d earlier", n), width), Role: "muted"}))
		evs = evs[n:]
	}
	for _, ev := range evs {
		lines = append(lines, LogLine{Kind: "command", Text: ellipsis("$ "+squash(ev.Text), width)})
	}
	return lines
}

// errorLines is an error (docs/ux.md §10). A failed tool call, after which
// the turn goes on, is one row, "✗ <what> · <why>". The turn's own error
// is "✗ error · <text>" over up to errRows rows.
func errorLines(ev watch.Event, width int) []LogLine {
	if watch.MetaTool(ev) == "" {
		var lines []LogLine
		for _, row := range hang("✗ error · ", watch.TurnErrorText(ev), width, 2, errRows) {
			lines = append(lines, LogLine{Kind: "error", Text: row})
		}
		return lines
	}
	what, why, path := watch.FailParts(ev)
	return []LogLine{{Kind: "error", Text: whatWhy("✗ ", what, why, width, path)}}
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

// listMark is a Markdown list item's lead, as MarkdownLines writes it.
var listMark = regexp.MustCompile(`^\s*(• |\d+\. )`)

// replyLines is a reply (docs/ux.md §10): "reply ───" across the row, then
// its Markdown indented 2: headings bold, code muted, and a list item's
// later rows hang under its text. Short screens drop its blank rows.
func replyLines(text string, width int, tall bool) []LogLine {
	lines := []LogLine{spanLine("text", Span{Text: "reply", Bold: true}, Span{Text: " " + strings.Repeat("─", max(1, width-cellLen("reply "))), Role: "border"})}
	inner := max(1, width-2)
	for _, md := range watch.MarkdownBlocks(watch.CleanText(text)) {
		switch {
		case strings.TrimSpace(md.Text) == "":
			if tall {
				lines = append(lines, LogLine{Kind: "text"})
			}
		case strings.HasPrefix(md.Text, "│ "):
			for _, row := range hardWrap(md.Text, inner) {
				lines = append(lines, spanLine("text", Span{Text: "  " + row, Role: "muted"}))
			}
		default:
			rows := wrap(md.Text, inner)
			if m := listMark.FindString(md.Text); m != "" {
				rows = hang(m, squash(md.Text[len(m):]), inner, cellLen(m), 0)
			}
			for _, row := range rows {
				lines = append(lines, spanLine("text", Span{Text: "  "}, Span{Text: row, Bold: md.Heading}))
			}
		}
	}
	for len(lines) > 1 && lines[len(lines)-1].Text == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// askLines is the agent asking (docs/ux.md §10): an [ASK] pill and the
// question, wrapped under the question.
func askLines(text string, width int) []LogLine {
	const lead = "[ASK] "
	var lines []LogLine
	for i, row := range hang(lead, squash(text), width, cellLen(lead), 0) {
		if i == 0 {
			lines = append(lines, spanLine("question", append(pill("ASK", "pickbg", "error"), Span{Text: row[len("[ASK]"):], Role: "text"})...))
			continue
		}
		lines = append(lines, spanLine("question", Span{Text: row, Role: "text"}))
	}
	return lines
}

// rexLine is one of Rex's own lines (docs/ux.md §10): "rex › plan · …" in
// muted, "rex ›" bold. They never come from or go to the feed.
func rexLine(kind, text string, width int) LogLine {
	row := ellipsis("rex › "+text, width)
	return spanLine(kind, Span{Text: "rex ›", Role: "muted", Bold: true}, Span{Text: strings.TrimPrefix(row, "rex ›"), Role: "muted"})
}

// driftLines is the drift seen while one task was the pin (docs/ux.md
// §9a): "rex › drift · <pin>", then a row per path or prompt laid out like
// a failed tool call, "<level> <subject> · <why>", hard ones in the error
// color and soft ones in the accent. The rows come from the event's
// meta, which only Rex writes (the block never reaches the feed).
func driftLines(ev watch.Event, width int) []LogLine {
	pin, _ := ev.Meta["pin"].(string)
	lines := []LogLine{rexLine("drift", "drift · "+squash(pin), width)}
	rows, _ := ev.Meta["rows"].([]driftNote)
	for _, n := range rows {
		role := "accent"
		if n.d.Level == plan.Hard {
			role = "error"
		}
		row := whatWhy("  "+n.d.Level.String()+" ", squash(n.d.Subject), squash(n.d.Reason), width, n.rel != "")
		lines = append(lines, spanLine("drift", Span{Text: row, Role: role}))
	}
	return lines
}
