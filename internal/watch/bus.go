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
	start := info.Size() - limit
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
		if i := strings.IndexByte(string(buf), '\n'); i >= 0 {
			buf = buf[i+1:]
		} else {
			return nil
		}
	}
	return buf
}

// AppendEvent adds one line to the feed. The feed is append-only: a reply
// that grows is written again in full and ExtendLog merges it on screen, so
// a tail never reads a line that was rewritten under it.
func AppendEvent(path string, event Event) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
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

// capText keeps event text under limit bytes, cut on a rune boundary, so a
// single line always fits in the tail window LastEvents reads.
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
		return nil
	}
	size := info.Size()
	if size < t.offset {
		t.offset = 0
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
