package watch

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var CursorHookEvents = []string{
	"beforeSubmitPrompt",
	"afterAgentThought",
	"beforeShellExecution",
	"preToolUse",
	"afterAgentResponse",
}

func IngestCommand() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "rex"
	}
	return strconv.Quote(exe) + " ingest --source cursor"
}

func InstallCursorHook(stdout io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data := map[string]any{"version": 1, "hooks": map[string]any{}}
	if raw, err := os.ReadFile(path); err == nil {
		var loaded map[string]any
		if json.Unmarshal(raw, &loaded) == nil {
			for k, v := range loaded {
				data[k] = v
			}
			if hooks, ok := loaded["hooks"].(map[string]any); ok {
				copied := map[string]any{}
				for k, v := range hooks {
					copied[k] = v
				}
				data["hooks"] = copied
			}
		}
	}
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		data["hooks"] = hooks
	}
	command := IngestCommand()
	for _, name := range CursorHookEvents {
		var entries []any
		already := false
		for _, entry := range toList(hooks[name]) {
			m, ok := entry.(map[string]any)
			if !ok {
				entries = append(entries, entry)
				continue
			}
			existing := fmt.Sprint(m["command"])
			if existing == command {
				already = true
				entries = append(entries, entry)
				continue
			}
			if isRexIngestCommand(existing) {
				continue
			}
			entries = append(entries, entry)
		}
		if !already {
			entries = append(entries, map[string]any{"command": command})
		}
		hooks[name] = entries
	}
	version := 1
	switch v := data["version"].(type) {
	case float64:
		if int(v) != 0 {
			version = int(v)
		}
	case int:
		if v != 0 {
			version = v
		}
	}
	data["version"] = version
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "installed cursor hooks at %s\n", path)
	fmt.Fprintf(stdout, "feed: %s\n", DefaultEventPath())
	return nil
}

func isRexIngestCommand(command string) bool {
	return strings.Contains(command, "ingest --source cursor") && strings.Contains(command, "rex")
}

func toList(v any) []any {
	switch t := v.(type) {
	case []any:
		return append([]any{}, t...)
	case nil:
		return nil
	default:
		return []any{t}
	}
}

func PayloadsFromRaw(raw string) []map[string]any {
	stripped := strings.TrimSpace(raw)
	if stripped == "" {
		return nil
	}
	var parsed any
	if json.Unmarshal([]byte(stripped), &parsed) == nil {
		if m, ok := parsed.(map[string]any); ok {
			return []map[string]any{m}
		}
		if list, ok := parsed.([]any); ok {
			var out []map[string]any
			for _, item := range list {
				if m, ok := item.(map[string]any); ok {
					out = append(out, m)
				}
			}
			return out
		}
	}
	var payloads []map[string]any
	for _, line := range strings.Split(stripped, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item map[string]any
		if json.Unmarshal([]byte(line), &item) != nil {
			continue
		}
		payloads = append(payloads, item)
	}
	return payloads
}

func IngestRaw(raw, source, path string) []Event {
	var written []Event
	for _, payload := range PayloadsFromRaw(raw) {
		event := MapPayload(payload, source)
		if event == nil || strings.TrimSpace(event.Text) == "" {
			continue
		}
		ok, err := AppendEvent(path, *event)
		if err != nil || !ok {
			continue
		}
		written = append(written, *event)
	}
	return written
}

func RunIngest(stdin io.Reader, stdout io.Writer, source string, installCursorHook bool, eventsPath string) error {
	if installCursorHook {
		return InstallCursorHook(stdout)
	}
	path := eventsPath
	if path == "" {
		path = DefaultEventPath()
	}
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	IngestRaw(string(raw), source, path)
	return nil
}
