package tui

import (
	"fmt"
	"strings"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/watch"
)

// TextOnlyBelow is the width under which the mascot is hidden so the text
// column keeps the whole terminal (docs/ux.md §12).
const TextOnlyBelow = 40 + Gutter + MinTextCols

// layout is how a terminal size splits into mascot, text and composer.
type layout struct {
	mascotCols int // 0 in text-only mode
	gutter     int
	textW      int // 0 when the text column would be too narrow
	composer   int // 3 = boxed, 1 = one bare row, 0 = none
	bodyH      int
	locked     bool // too narrow: only Ctrl+C (and Ctrl+L) do anything
	typing     bool // a composer is visible, so printable keys are accepted
}

func layoutFor(columns, lines int) layout {
	columns, lines = max(1, columns), max(1, lines)
	var l layout
	if columns >= TextOnlyBelow {
		l.mascotCols = min(mascot.StageSize, columns)
		l.gutter = Gutter
		l.textW = columns - l.mascotCols - l.gutter
	} else {
		l.textW = columns
	}
	if l.textW < MinTextCols {
		l.textW, l.mascotCols, l.gutter, l.locked = 0, 0, 0, true
	}
	switch {
	case lines >= 16:
		l.composer = InputRows
	case lines >= 7:
		l.composer = 1
	}
	l.typing = l.composer > 0 && !l.locked
	l.bodyH = max(0, lines-2-FooterRows-l.composer)
	return l
}

// placeWord names the Tab place in the header.
func placeWord(pane PaneKind) string {
	if pane == PanePlans {
		return "plan"
	}
	return "tape"
}

// headerRow is row 0: "REX plan|tape", then the colored turn state and the
// open plan's path, shortened to fit (docs/ux.md §4).
func headerRow(width int, place, state, planRel string, enabled bool) string {
	left := "REX " + place
	state = strings.TrimSpace(strings.ReplaceAll(watch.CleanText(state), "\n", " "))
	if state == "" {
		state = "idle"
	}
	if width < cellLen(left)+2 {
		return mascot.Paint(clip("REX", width), mascot.UI["title"], enabled)
	}
	room := width - cellLen(left) - 2
	path := ""
	if planRel = strings.TrimSpace(strings.ReplaceAll(watch.CleanText(planRel), "\n", " ")); planRel != "" {
		path = shortPlanPath(planRel, room-cellLen(state)-3)
	}
	meta := state
	if path != "" {
		meta += " · " + path
	}
	if cellLen(meta) > room {
		meta = cutCells(state, room)
		path = ""
	}
	gap := width - cellLen(left) - cellLen(meta)
	stateColor := mascot.UI["muted"]
	switch state {
	case "thinking", "running":
		stateColor = mascot.UI["accent"]
	case "ask", "error":
		stateColor = mascot.UI["error"]
	}
	out := mascot.Paint("REX", mascot.UI["title"], enabled) + mascot.Paint(" "+place, mascot.UI["muted"], enabled) +
		strings.Repeat(" ", gap) + mascot.Paint(cutCells(meta, cellLen(state)), stateColor, enabled)
	if rest := meta[len(cutCells(meta, cellLen(state))):]; rest != "" {
		out += mascot.Paint(rest, mascot.UI["muted"], enabled)
	}
	return out
}

// shortPlanPath fits a repo-relative plan path into room cells: the whole
// path when it fits, then without "rex/plans/", then without ".rex.json",
// then clipped from the left.
func shortPlanPath(path string, room int) string {
	if room <= 0 {
		return ""
	}
	if cellLen(path) <= room {
		return path
	}
	path = strings.TrimPrefix(path, "rex/plans/")
	if cellLen(path) <= room {
		return path
	}
	path = strings.TrimSuffix(path, ".rex.json")
	if cellLen(path) <= room {
		return path
	}
	if room == 1 {
		return "…"
	}
	return "…" + tailCells(path, room-1)
}

// fitFooter joins ranked segments with " · ", dropping whole segments from
// the lowest rank until the line fits. Words are never cut, except the
// first segment on a terminal too narrow even for it.
func fitFooter(segments []string, width int) string {
	for n := len(segments); n > 0; n-- {
		line := strings.Join(segments[:n], " · ")
		if cellLen(line) <= width {
			return line
		}
	}
	if len(segments) == 0 {
		return ""
	}
	return clip(segments[0], width)
}

// homeCreates reports that ⏎ on Library Home creates a plan: the cursor
// is on Create. It reads the rows, not the disk.
func homeCreates(b *PlanBrowser) bool {
	return b.Cursor < len(b.Rows) && b.Rows[b.Cursor].Kind == PlanRowNew
}

// withoutEnter drops the ⏎ segments from a footer, for screens too short
// to show the composer (⏎ does nothing there).
func withoutEnter(segs []string) []string {
	var out []string
	for _, s := range segs {
		if !strings.Contains(s, "⏎") {
			out = append(out, s)
		}
	}
	return out
}

// footerSegments is the footer for the current screen, highest rank first
// (docs/ux.md §4): where you can go, not what you can type.
func footerSegments(opt ComposeOptions, scroll int) ([]string, string) {
	segs, kind := placeFooter(opt, scroll)
	if n := runeLen(opt.Draft); n >= draftCap-16 && kind != "error" && len(segs) > 0 {
		// The cap is not silent: near it, the count is a must-have.
		segs = append([]string{segs[0], fmt.Sprintf("%d/%d", n, draftCap)}, segs[1:]...)
	}
	return segs, kind
}

// hintText is how a placeholder reads: dim when there is color, in
// parentheses when there isn't, so it never looks like typed text.
func hintText(placeholder string, enabled bool) string {
	if enabled || placeholder == "" {
		return placeholder
	}
	return "(" + placeholder + ")"
}

func placeFooter(opt ComposeOptions, scroll int) ([]string, string) {
	if opt.QuitPending {
		return []string{"ctrl+c again to quit"}, "error"
	}
	if opt.Pane == PanePlans && opt.PlanFooter != nil {
		return opt.PlanFooter, "muted"
	}
	if opt.Pane == PanePlans && opt.Plans != nil && opt.Preview != "" {
		word := map[PlanScreen]string{ScreenCreate: "new plan", ScreenFolder: "new folder", ScreenBrowse: "open"}[opt.Plans.Screen]
		if word == "" {
			word = "plans"
		}
		return []string{word, opt.Preview}, "muted"
	}
	if opt.Pane == PanePlans && opt.Plans != nil {
		switch opt.Plans.Screen {
		case ScreenCreate:
			return []string{"new plan", "⏎ create", "esc cancel", "tab tape"}, "muted"
		case ScreenFolder:
			return []string{"new folder", "⏎ create", "esc cancel", "tab tape"}, "muted"
		case ScreenBrowse:
			return []string{"open", "⏎ open", "esc back", "tab tape", "type to filter"}, "muted"
		}
		if b := opt.Plans; b.Find != "" {
			if b.Cursor < len(b.Rows) && b.Rows[b.Cursor].Kind == PlanRowCreateNamed {
				return []string{"find", "⏎ create", "esc clear", "tab tape"}, "muted"
			}
			return []string{"find", "⏎ open", "esc clear", "tab tape"}, "muted"
		}
		if homeCreates(opt.Plans) {
			return []string{"plans", "⏎ create", "tab tape", "type to find", "ctrl+c×2 quit"}, "muted"
		}
		return []string{"plans", "⏎ open", "tab tape", "type to find", "ctrl+c×2 quit"}, "muted"
	}
	if strings.TrimSpace(opt.Draft) != "" && opt.Preview != "" {
		return []string{"tape", opt.Preview}, "muted"
	}
	if scroll > 0 {
		return []string{"tape", fmt.Sprintf("%d↑", scroll), "end live", "tab plan"}, "muted"
	}
	return []string{"tape", "tab plan", "↑↓ scroll", "?⏎ help", "ctrl+c×2 quit"}, "muted"
}

// composerRows draws the composer: a box when there is room, a bare row on
// short terminals.
func composerRows(l layout, draft, prefix, placeholder string, enabled bool) []string {
	switch l.composer {
	case InputRows:
		return inputBox(draft, l.textW, enabled, prefix, placeholder)
	case 1:
		if draft == "" && placeholder != "" {
			return []string{mascot.Paint(prefix, mascot.UI["text"], enabled) + mascot.Paint(clip(hintText(placeholder, enabled), max(0, l.textW-cellLen(prefix))), mascot.UI["border"], enabled)}
		}
		line := clip(prefix+visibleDraft(draft, l.textW-cellLen(prefix)), l.textW)
		return []string{mascot.Paint(line, mascot.UI["text"], enabled)}
	}
	return nil
}

// cursorFor places the cursor after the draft, or hides it (row 0).
func cursorFor(l layout, lines int, draft, prefix string) (int, int) {
	visible := visibleDraft(draft, l.textW-l.composerChrome()-cellLen(prefix))
	switch l.composer {
	case InputRows:
		return lines - 2, l.mascotCols + l.gutter + 1 + 1 + cellLen(prefix) + cellLen(visible)
	case 1:
		return lines - 1, l.mascotCols + l.gutter + 1 + cellLen(prefix) + cellLen(visible)
	}
	return 0, 0
}

func (l layout) composerChrome() int {
	if l.composer == InputRows {
		return 2
	}
	return 0
}
