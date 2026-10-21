package tui

import (
	"strings"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/watch"
)

// app is the live screen's state. RunLoop feeds it keys, hook events and
// ticks and draws app.frame(); tests drive it the same way without a TTY.
type app struct {
	color   string
	enabled bool

	sequence   mascot.Animation
	frameIndex int
	hold       int
	spriteName string
	animated   bool

	columns, lines int

	pane      PaneKind
	plans     *PlanBrowser
	draft     string
	chatDraft string

	log      []watch.Event
	scroll   int
	turn     Turn
	lastKind string

	quitPending bool
	quitTicks   int
	quit        bool
}

func newApp(color string, enabled bool, animation string, plans *PlanBrowser, columns, lines int) *app {
	a := &app{color: color, enabled: enabled, pane: PanePlans, plans: plans, columns: columns, lines: lines}
	a.sequence = mascot.MustAnimation(animation)
	a.frameIndex = 0
	a.hold = a.sequence.Frames[0].Hold
	a.spriteName = a.sequence.Frames[0].Sprite
	a.animated = len(a.sequence.Frames) > 1
	return a
}

func (a *app) play(name string) {
	if a.sequence.Name == name {
		return
	}
	a.sequence = mascot.MustAnimation(name)
	a.frameIndex = 0
	a.hold = a.sequence.Frames[0].Hold
	a.spriteName = a.sequence.Frames[0].Sprite
	a.animated = len(a.sequence.Frames) > 1
}

// syncTurn shows the pose for the current turn, but lets a one-shot
// breakdance finish first.
func (a *app) syncTurn() {
	if a.sequence.Name == "breakdance" {
		return
	}
	a.play(AnimationForTurn(a.turn))
}

func (a *app) frame() string {
	meta := chatHeaderMeta(statusForTurn(a.turn, a.lastKind), a.plans.CurrentRel(), PaneTextWidth(a.columns))
	if a.pane == PanePlans {
		switch a.plans.Screen {
		case ScreenCreate:
			meta = "new plan"
		case ScreenFolder:
			meta = "new folder"
		case ScreenBrowse:
			meta = "open"
		default:
			meta = "plans"
		}
	}
	return ComposeFrame(a.spriteName, a.color, ComposeOptions{
		Enabled:       a.enabled,
		Columns:       a.columns,
		Lines:         a.lines,
		AnimationName: a.sequence.Name,
		Draft:         a.draft,
		Log:           a.log,
		QuitPending:   a.quitPending,
		Status:        meta,
		Scroll:        a.scroll,
		Pane:          a.pane,
		Plans:         a.plans,
	})
}

func (a *app) logLines() int {
	textW := PaneTextWidth(a.columns)
	if textW <= 0 {
		return 0
	}
	return len(BuildLogView(a.log, textW))
}

// ingest applies hook events: turn state for every event, the tape for the
// ones worth drawing. A scrolled-up view keeps showing the same lines while
// new ones arrive below (until MaxLog trims the oldest).
func (a *app) ingest(fresh []watch.Event) bool {
	if len(fresh) == 0 {
		return false
	}
	before := a.logLines()
	prevTurn := a.turn
	for _, ev := range fresh {
		a.turn = ApplyTurn(a.turn, ev)
		if watch.KeepInPane(ev) || ev.Kind == "idle" { // a turn end clears an old error
			a.lastKind = ev.Kind
		}
	}
	added := ExtendLog(&a.log, fresh)
	if len(added) > 0 && a.scroll > 0 {
		a.scroll += max(0, a.logLines()-before)
	}
	if len(added) > 0 || a.turn != prevTurn {
		a.syncTurn()
		return true
	}
	return false
}

// handleKey applies one key. It sets a.quit on the second Ctrl+C.
func (a *app) handleKey(key string) bool {
	if text, ok := IsPaste(key); ok {
		// A paste is typed into the draft as one line; it never submits.
		a.quitPending = false
		next := capDraft(a.draft + text)
		if next == a.draft {
			return false
		}
		a.draft = next
		return true
	}
	bodyH := PaneBodyHeight(a.lines)
	total := a.logLines()
	maxScroll := ClampScroll(total, bodyH, total)
	switch {
	case key == CtrlC:
		if a.quitPending {
			a.quit = true
			return false
		}
		a.quitPending = true
		a.quitTicks = QuitWindowTicks
		return true
	case key == "\t":
		a.quitPending = false
		if a.pane == PanePlans {
			if a.plans.Browsing() {
				a.plans.Filter = a.draft
			}
			a.pane = PaneChat
			a.draft = a.chatDraft
		} else {
			a.chatDraft = a.draft
			a.pane = PanePlans
			a.draft = ""
			if a.plans.Browsing() {
				a.draft = a.plans.Filter
			}
			a.plans.Rebuild()
		}
		return true
	case a.pane == PanePlans:
		a.quitPending = false
		changed, openChat := handlePlanKey(a.plans, &a.draft, key, bodyH)
		if openChat {
			a.pane = PaneChat
			a.draft = a.chatDraft
		}
		return changed
	}
	switch key {
	case "up":
		a.scroll = min(maxScroll, a.scroll+1)
		return true
	case "down":
		a.scroll = max(0, a.scroll-1)
		return true
	case "pageup":
		a.scroll = min(maxScroll, a.scroll+max(1, bodyH))
		return true
	case "pagedown":
		a.scroll = max(0, a.scroll-max(1, bodyH))
		return true
	case "home":
		a.scroll = maxScroll
		return true
	case "end":
		a.scroll = 0
		return true
	case "\r", "\n":
		a.quitPending = false
		text := strings.TrimSpace(a.draft)
		if text == "" {
			return false
		}
		next := ComposerAnimation(text, a.sequence.Name)
		if next != a.sequence.Name {
			if next == mascot.DefaultAnimation {
				a.turn = TurnIdle
			}
			a.play(next)
		}
		a.log = append(a.log, watch.NewEvent("user", "text", text, nil))
		a.draft = ""
		return true
	case "\x7f", "\x08":
		a.quitPending = false
		if a.draft == "" {
			return false
		}
		r := []rune(a.draft)
		a.draft = string(r[:len(r)-1])
		return true
	}
	if isPrintableKey(key) {
		a.quitPending = false
		if runeLen(a.draft) < draftCap {
			a.draft += key
			return true
		}
		return false
	}
	if a.quitPending {
		a.quitPending = false
		return true
	}
	return false
}

// tick runs when no key arrived: the quit window counts down and the
// mascot advances a frame.
func (a *app) tick() bool {
	changed := false
	if a.quitPending {
		a.quitTicks--
		if a.quitTicks <= 0 {
			a.quitPending = false
			changed = true
		}
	}
	if !a.animated {
		return changed
	}
	var finished bool
	a.frameIndex, a.hold, finished = advance(a.sequence.Frames, a.frameIndex, a.hold, animationLoops(a.sequence.Name))
	if finished {
		a.play(AnimationForTurn(a.turn))
		return true
	}
	if next := a.sequence.Frames[a.frameIndex].Sprite; next != a.spriteName {
		a.spriteName = next
		changed = true
	}
	return changed
}

// settle runs after every key or tick: it applies the browse filter, picks
// up a resize, and keeps the tape scroll in range.
func (a *app) settle(columns, lines int) bool {
	changed := false
	if a.pane == PanePlans && a.plans.Browsing() && a.plans.Filter != a.draft {
		a.plans.Filter = a.draft
		a.plans.Rebuild()
		changed = true
	}
	if columns != a.columns || lines != a.lines {
		a.columns, a.lines = columns, lines
		changed = true
	}
	total := a.logLines()
	if clamped := ClampScroll(total, PaneBodyHeight(a.lines), a.scroll); clamped != a.scroll {
		a.scroll = clamped
		changed = true
	}
	return changed
}

const draftCap = 256

// capDraft keeps the composer to draftCap whole runes.
func capDraft(s string) string {
	if r := []rune(s); len(r) > draftCap {
		return string(r[:draftCap])
	}
	return s
}
