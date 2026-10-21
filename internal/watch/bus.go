package watch

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	EnvPath      = "REX_EVENTS"
	tailBytes    = 65536
	recentN      = 12
	maxEventText = 32 << 10
)

func DefaultEventPath() string {
	if override := os.Getenv(EnvPath); override != "" {
		if strings.HasPrefix(override, "~/") {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, override[2:])
		}
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "session.jsonl"
	}
	return filepath.Join(home, ".rex", "session.jsonl")
}

func LastEvents(path string, limit int) []Event {
	if limit <= 0 {
		return nil
	}
	data := readTail(path, tailBytes)
	if len(data) == 0 {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	var events []Event
	for _, line := range lines {
		ev, err := EventFromJSON(line)
		if err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events
}

// readTail returns at most limit bytes from the end of path, starting on a
// line boundary. Hooks call this on every event, so it must not read the
// whole feed.
func readTail(path string, limit int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	// Read one byte before the window: if it is a newline, the window
	// already starts on a line boundary and its first line is whole.
	start := info.Size() - limit - 1
	if start < 0 {
		start = 0
	}
	buf := make([]byte, info.Size()-start)
	n, err := f.ReadAt(buf, start)
	if err != nil && n == 0 {
		return nil
	}
	buf = buf[:n]
	if start > 0 {
		i := strings.IndexByte(string(buf), '\n')
		if i < 0 {
			return nil
		}
		buf = buf[i+1:]
	}
	return buf
}

// AppendEvent adds one line to the feed. The feed is append-only: a reply
// that grows is written again in full and ExtendLog merges it on screen, so
// a tail never reads a line that was rewritten under it.
func AppendEvent(path string, event Event) (bool, error) {
	if err := ensureFeedDir(path); err != nil {
		return false, err
	}
	event.Text = capText(event.Text, maxEventText)
	var wrote bool
	err := withFeedLock(path, func() error {
		recent := LastEvents(path, recentN)
		if IsDuplicate(event, recent, 4) {
			return nil
		}
		line, err := event.ToJSON()
		if err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		privatize(f)
		_, err = f.WriteString(line + "\n")
		cerr := f.Close()
		if err != nil {
			return err
		}
		if cerr != nil {
			return cerr
		}
		wrote = true
		return nil
	})
	return wrote, err
}

// The feed holds prompts and commands, so it is private to the user:
// 0600 for the file and 0700 for ~/.rex, tightened on older installs too.
func ensureFeedDir(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(dir) == filepath.Join(home, ".rex") {
		if info, err := os.Stat(dir); err == nil && info.Mode().Perm()&0o077 != 0 {
			_ = os.Chmod(dir, 0o700)
		}
	}
	return nil
}

func privatize(f *os.File) {
	if info, err := f.Stat(); err == nil && info.Mode().Perm()&0o077 != 0 {
		_ = f.Chmod(0o600)
	}
}

// capText keeps event text under limit bytes, cut on a rune boundary. With
// the 32KB cap a line fits the 64KB tail window LastEvents reads unless the
// text is mostly characters JSON must escape; such a line is only missed
// by duplicate detection, never lost from the feed.
func capText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

type EventTail struct {
	Path    string
	offset  int64
	pending string
}

func NewEventTail(path string, fromEnd bool) *EventTail {
	t := &EventTail{Path: path}
	if fromEnd {
		if info, err := os.Stat(path); err == nil {
			t.offset = info.Size()
		}
	}
	return t
}

func (t *EventTail) Poll() []Event {
	info, err := os.Stat(t.Path)
	if err != nil {
		t.offset = 0
		t.pending = ""
		return nil
	}
	size := info.Size()
	if size < t.offset {
		// The feed was truncated or replaced: start over and drop the
		// partial line, or it would glue onto the next event.
		t.offset = 0
		t.pending = ""
	}
	if size == t.offset {
		return nil
	}
	f, err := os.Open(t.Path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if _, err := f.Seek(t.offset, 0); err != nil {
		return nil
	}
	buf := make([]byte, size-t.offset)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil
	}
	t.offset, _ = f.Seek(0, 1)
	chunk := t.pending + string(buf[:n])
	t.pending = ""
	parts := strings.Split(chunk, "\n")
	if !strings.HasSuffix(chunk, "\n") {
		t.pending = parts[len(parts)-1]
		parts = parts[:len(parts)-1]
	}
	var events []Event
	for _, line := range parts {
		raw := strings.TrimSpace(line)
		if raw == "" {
			continue
		}
		ev, err := EventFromJSON(raw)
		if err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events
}
