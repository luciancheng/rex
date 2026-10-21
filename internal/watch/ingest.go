package watch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var CursorHookEvents = []string{
	"beforeSubmitPrompt",
	"afterAgentThought",
	"beforeShellExecution",
	"preToolUse",
	"postToolUse",
	"postToolUseFailure",
	"afterFileEdit",
	"afterAgentResponse",
	"stop",
}

func IngestCommand() string {
	return strconv.Quote(rexExecutable()) + " ingest --source cursor"
}

func rexExecutable() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "rex"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	if !isEphemeralGoBuild(exe) {
		return exe
	}
	if home, err := os.UserHomeDir(); err == nil {
		stable := filepath.Join(home, ".local", "bin", "rex")
		if info, err := os.Stat(stable); err == nil && !info.IsDir() {
			return stable
		}
	}
	return "rex"
}

func isEphemeralGoBuild(exe string) bool {
	return strings.Contains(filepath.ToSlash(exe), "/go-build")
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
				m["timeout"] = 20
				entries = append(entries, m)
				continue
			}
			if isRexIngestCommand(existing) {
				continue
			}
			entries = append(entries, entry)
		}
		if !already {
			entries = append(entries, map[string]any{"command": command, "timeout": 20})
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
	// Cursor on Windows prefixes stdin with a UTF-8 BOM.
	stripped := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "\uFEFF"))
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
	written, _ := ingest(raw, source, path)
	return written
}

// ingest is IngestRaw that also returns the first write error, so a hook
// can report it on stderr. It keeps going after an error.
func ingest(raw, source, path string) ([]Event, error) {
	var written []Event
	var first error
	for _, payload := range PayloadsFromRaw(raw) {
		event := MapPayload(payload, source)
		if event == nil || strings.TrimSpace(event.Text) == "" {
			continue
		}
		ok, err := AppendEvent(path, *event)
		if err != nil && first == nil {
			first = err
		}
		if err != nil || !ok {
			continue
		}
		written = append(written, *event)
	}
	return written, first
}

// RunIngest maps one hook payload from stdin into the feed. It runs inside
// agent hooks, so it must never block or fail the agent: the caller exits 0
// whatever happens here, and nothing is written to stdout.
func RunIngest(stdin io.Reader, source string, eventsPath string) error {
	path := eventsPath
	if path == "" {
		path = DefaultEventPath()
	}
	_, err := ingest(string(readPayload(stdin, stdinWait)), source, path)
	return err
}

const (
	stdinLimit = 16 << 20
	stdinWait  = 2 * time.Second
	stdinIdle  = 50 * time.Millisecond
)

// readPayload reads a hook payload without trusting the harness to close
// stdin. It returns at EOF, shortly after a complete JSON value has
// arrived, or after wait, whichever comes first.
func readPayload(r io.Reader, wait time.Duration) []byte {
	chunks := make(chan []byte)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(chunks)
		buf := make([]byte, 32<<10)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				select {
				case chunks <- append([]byte(nil), buf[:n]...):
				case <-done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	var data []byte
	var idle <-chan time.Time
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				return data
			}
			data = append(data, chunk...)
			if len(data) >= stdinLimit {
				return data[:stdinLimit]
			}
			idle = nil
			if completeJSON(data) {
				idle = time.After(stdinIdle)
			}
		case <-idle:
			return data
		case <-deadline.C:
			return data
		}
	}
}

func completeJSON(data []byte) bool {
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(data), []byte("\uFEFF")))
	if len(trimmed) == 0 {
		return false
	}
	if last := trimmed[len(trimmed)-1]; last != '}' && last != ']' {
		return false
	}
	return json.Valid(trimmed)
}
