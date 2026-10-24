package watch

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Event struct {
	Source string         `json:"source"`
	Kind   string         `json:"kind"`
	Text   string         `json:"text"`
	TS     float64        `json:"ts"`
	Meta   map[string]any `json:"meta"`
}

func NewEvent(source, kind, text string, meta map[string]any) Event {
	if meta == nil {
		meta = map[string]any{}
	}
	return Event{
		Source: source,
		Kind:   kind,
		Text:   text,
		TS:     float64(time.Now().UnixNano()) / 1e9,
		Meta:   meta,
	}
}

func (e Event) ToJSON() (string, error) {
	if e.Meta == nil {
		e.Meta = map[string]any{}
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func EventFromJSON(line string) (Event, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(line), &data); err != nil {
		return Event{}, err
	}
	meta, _ := data["meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	ts, _ := toFloat(data["ts"])
	return Event{
		Source: fmt.Sprint(or(data["source"], "demo")),
		Kind:   fmt.Sprint(or(data["kind"], "text")),
		Text:   fmt.Sprint(or(data["text"], "")),
		TS:     ts,
		Meta:   meta,
	}, nil
}

func or(v any, fallback string) any {
	if v == nil {
		return fallback
	}
	s := fmt.Sprint(v)
	if s == "" || s == "<nil>" {
		return fallback
	}
	return v
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
