package watch

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvPath   = "REX_EVENTS"
	tailBytes = 65536
	recentN   = 12
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if len(data) > tailBytes {
		data = data[len(data)-tailBytes:]
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

func AppendEvent(path string, event Event) (bool, error) {
	if IsDuplicate(event, LastEvents(path, recentN), 4) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	line, err := event.ToJSON()
	if err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return false, err
	}
	return true, nil
}

type EventTail struct {
	Path   string
	offset int64
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
	var events []Event
	for _, line := range strings.Split(string(buf[:n]), "\n") {
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
