package watch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/jsonedit"
)

// CursorHookEvents are the Cursor hooks Rex registers. beforeShellExecution
// is not one of them: preToolUse already reports shell commands, and having
// both made every command arrive twice.
var CursorHookEvents = []string{
	"beforeSubmitPrompt",
	"afterAgentThought",
	"preToolUse",
	"postToolUse",
	"postToolUseFailure",
	"afterFileEdit",
	"afterAgentResponse",
	"stop",
}

// retiredCursorHookEvents are events older Rex versions registered. Rex
// removes its own entries from them on install and leaves anything else.
var retiredCursorHookEvents = []string{"beforeShellExecution"}

const cursorHookTimeout = 20

func IngestCommand() string {
	return ingestCommand("cursor", rexExecutable(), runtime.GOOS)
}

// ingestCommand is the shell command a hook runs. Cursor runs hooks through
// a shell, so the path is quoted for one: single quotes on POSIX, and on
// Windows a forward-slash path (quoted only when it has spaces), which
// cmd, PowerShell and Git Bash all accept.
func ingestCommand(source, exe, goos string) string {
	if goos == "windows" {
		exe = strings.ReplaceAll(exe, `\`, "/")
		if strings.ContainsAny(exe, " \t") {
			exe = `"` + exe + `"`
		}
		return exe + " ingest --source " + source
	}
	return shellQuote(exe) + " ingest --source " + source
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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

// InstallCursorHook merges Rex's ingest command into ~/.cursor/hooks.json.
// Other hooks, other keys and their formatting are kept byte for byte; an
// unparseable file is reported and left alone.
func InstallCursorHook(stdout io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".cursor", "hooks.json")
	command := IngestCommand()
	changed, err := UpdateJSONFile(path, []byte("{\n  \"version\": 1,\n  \"hooks\": {}\n}\n"), func(d *jsonedit.Doc) ([]byte, error) {
		return mergeCursorHooks(d, command)
	})
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(stdout, "installed cursor hooks at %s\n", path)
	} else {
		fmt.Fprintf(stdout, "cursor hooks already installed at %s\n", path)
	}
	fmt.Fprintf(stdout, "feed: %s\n", DefaultEventPath())
	return nil
}

func mergeCursorHooks(d *jsonedit.Doc, command string) ([]byte, error) {
	e := &editor{doc: d}
	if e.doc.Root().Kind != '{' {
		return nil, fmt.Errorf("hooks.json is not a JSON object")
	}
	if e.doc.Root().Member("version") == nil {
		if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Set(d.Root(), "version", 1) }); err != nil {
			return nil, err
		}
	}
	switch hooks := e.doc.Root().Member("hooks"); {
	case hooks == nil:
		if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Set(d.Root(), "hooks", map[string]any{}) }); err != nil {
			return nil, err
		}
	case hooks.Kind != '{':
		return nil, fmt.Errorf(`"hooks" in hooks.json is not an object`)
	}
	ours := map[string]any{"command": command, "timeout": cursorHookTimeout}
	for _, name := range CursorHookEvents {
		if err := mergeHookList(e, name, ours, true); err != nil {
			return nil, err
		}
	}
	for _, name := range retiredCursorHookEvents {
		if err := mergeHookList(e, name, ours, false); err != nil {
			return nil, err
		}
	}
	return e.doc.Bytes(), nil
}

// mergeHookList edits one event's entry list element by element, so the
// user's other entries keep their exact bytes. Stale Rex entries are
// removed; with want, exactly one copy of ours is kept or appended.
func mergeHookList(e *editor, name string, ours map[string]any, want bool) error {
	hooks := func(d *jsonedit.Doc) *jsonedit.Node { return d.Root().Member("hooks") }
	node := hooks(e.doc).Member(name)
	if node == nil {
		if !want {
			return nil
		}
		return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Set(hooks(d), name, []any{ours}) })
	}
	if node.Kind != '[' {
		var existing any
		if err := e.doc.Decode(node, &existing); err != nil {
			return err
		}
		next := []any{existing}
		if m, ok := existing.(map[string]any); ok && isRexIngestCommand(fmt.Sprint(m["command"]), "cursor") {
			next = nil
		}
		if want {
			next = append(next, ours)
		}
		return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Set(hooks(d), name, next) })
	}
	kept := false
	for i := len(node.Elems) - 1; i >= 0; i-- {
		var entry map[string]any
		if e.doc.Decode(node.Elems[i], &entry) != nil {
			continue
		}
		cmd := fmt.Sprint(entry["command"])
		if !isRexIngestCommand(cmd, "cursor") {
			continue
		}
		if want && !kept && cmd == ours["command"] {
			kept = true
			if !sameJSON(entry, ours) {
				if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) {
					return d.ReplaceElem(hooks(d).Member(name), i, ours)
				}); err != nil {
					return err
				}
			}
		} else if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) {
			return d.RemoveElem(hooks(d).Member(name), i)
		}); err != nil {
			return err
		}
		node = hooks(e.doc).Member(name)
	}
	if want && !kept {
		return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Append(hooks(d).Member(name), ours) })
	}
	if !want && len(node.Elems) == 0 {
		return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Remove(hooks(d), name) })
	}
	return nil
}

func sameJSON(a, b any) bool {
	ab, aerr := json.Marshal(a)
	bb, berr := json.Marshal(b)
	return aerr == nil && berr == nil && bytes.Equal(ab, bb)
}

// isRexIngestCommand recognizes Rex hook commands from any version: Go- or
// shell-quoted paths, any install location.
func isRexIngestCommand(command, source string) bool {
	return strings.Contains(command, "rex") && strings.Contains(command, "ingest --source "+source)
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
