package watch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/jsonedit"
)

// CursorHookEvents are the Cursor hooks Rex registers. beforeShellExecution
// is not one of them: preToolUse already reports shell commands, and having
// both made every command arrive twice. sessionStart records nothing; it
// only hands a new chat the pin (docs/ux.md §7a).
var CursorHookEvents = []string{
	"sessionStart",
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

var runtimeGOOS = runtime.GOOS

func IngestCommand() string {
	return ingestCommand("cursor", rexExecutable(), runtimeGOOS)
}

// ingestCommand is the shell command a hook runs. Cursor runs hooks through
// a shell, so the path is quoted for one: single quotes on POSIX. On
// Windows a plain forward-slash path works in cmd, PowerShell and Git Bash.
// A path that needs quoting gets PowerShell's call operator with a literal
// single-quoted path (& '…', ' doubled), so $ and ` stay literal; that form
// needs PowerShell.
func ingestCommand(source, exe, goos string) string {
	if goos == "windows" {
		exe = strings.ReplaceAll(exe, `\`, "/")
		if strings.ContainsAny(exe, " \t&'()^%!;,$`\"") {
			return `& '` + strings.ReplaceAll(exe, "'", "''") + `' ingest --source ` + source
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

// installCursor merges Rex's ingest command into ~/.cursor/hooks.json.
// Other hooks, other keys and their formatting are kept byte for byte; an
// unparseable file is reported and left alone.
func installCursor(home string, stdout io.Writer) error {
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
	fmt.Fprintln(stdout, "cursor: when a chat starts, and before each prompt on Cursor builds that take it, the hook also adds the task pinned in this repo's Rex plan, if one is, to the agent's context")
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
	ours := map[string]any{"command": command, "timeout": cursorHookTimeout}
	switch hooks := e.doc.Root().Member("hooks"); {
	case hooks == nil || (hooks.Kind == '{' && len(hooks.Members) == 0):
		// A missing or empty hooks object is written whole instead of
		// growing an inline {} one key at a time.
		fresh := map[string]any{}
		for _, name := range CursorHookEvents {
			fresh[name] = []any{ours}
		}
		if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Set(d.Root(), "hooks", fresh) }); err != nil {
			return nil, err
		}
		return e.doc.Bytes(), nil
	case hooks.Kind != '{':
		return nil, fmt.Errorf(`"hooks" in hooks.json is not an object`)
	}
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

// cursorFirstEvents are the hooks whose reply carries the pin note. Rex's
// entry goes first in their lists, so the person's own hooks there (a
// prompt blocker, another tool's context) answer after Rex's: for fields
// it doesn't merge, Cursor keeps the last answer (docs/ux.md §7a).
var cursorFirstEvents = map[string]bool{"sessionStart": true, "beforeSubmitPrompt": true}

// mergeHookList edits one event's entry list element by element, so the
// user's other entries keep their exact bytes. Stale Rex entries are
// removed; with want, exactly one copy of ours is kept or added, at the
// end, or first for cursorFirstEvents (moved there if it isn't). Fields
// the user added to our entry (a matcher, say) are kept.
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
		var single map[string]any
		if !want && e.doc.Decode(node, &single) == nil && isRexIngestCommand(fmt.Sprint(single["command"]), ours["command"].(string), "cursor") {
			return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Remove(hooks(d), name) })
		}
		if !want {
			return nil
		}
		return fmt.Errorf(`"hooks.%s" in hooks.json is not a list; fix it by hand, rex did not change it`, name)
	}
	kept, removed := false, false
	for i := len(node.Elems) - 1; i >= 0; i-- {
		var entry map[string]any
		if e.doc.Decode(node.Elems[i], &entry) != nil {
			continue
		}
		cmd := fmt.Sprint(entry["command"])
		if !isRexIngestCommand(cmd, ours["command"].(string), "cursor") {
			continue
		}
		if want && !kept && cmd == ours["command"] {
			kept = true
			merged := map[string]any{}
			for k, v := range entry {
				merged[k] = v
			}
			merged["timeout"] = ours["timeout"]
			if !sameJSON(entry, merged) {
				if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) {
					return d.ReplaceElem(hooks(d).Member(name), i, merged)
				}); err != nil {
					return err
				}
			}
		} else if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) {
			return d.RemoveElem(hooks(d).Member(name), i)
		}); err != nil {
			return err
		} else {
			removed = true
		}
		node = hooks(e.doc).Member(name)
	}
	if !want {
		if removed && len(node.Elems) == 0 {
			return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Remove(hooks(d), name) })
		}
		return nil
	}
	if !cursorFirstEvents[name] {
		if kept {
			return nil
		}
		return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Append(hooks(d).Member(name), ours) })
	}
	for i := 0; kept && i < len(node.Elems); i++ {
		var entry map[string]any
		if e.doc.Decode(node.Elems[i], &entry) != nil || entry["command"] != ours["command"] {
			continue
		}
		if i == 0 {
			return nil
		}
		if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.RemoveElem(hooks(d).Member(name), i) }); err != nil {
			return err
		}
		ours = entry
		break
	}
	return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Prepend(hooks(d).Member(name), ours) })
}

// removeCursorHooks takes every Rex entry out of hooks.json and drops
// event lists that become empty.
func removeCursorHooks(d *jsonedit.Doc, command string) ([]byte, error) {
	e := &editor{doc: d}
	hooks := e.doc.Root().Member("hooks")
	if e.doc.Root().Kind != '{' || hooks == nil || hooks.Kind != '{' {
		return e.doc.Bytes(), nil
	}
	ours := map[string]any{"command": command, "timeout": cursorHookTimeout}
	var names []string
	for _, m := range hooks.Members {
		names = append(names, m.Key)
	}
	for _, name := range names {
		node := e.doc.Root().Member("hooks").Member(name)
		if node == nil || node.Kind != '[' || !hasRexEntry(e.doc, node, command) {
			continue // leave lists Rex has nothing in exactly as they are
		}
		if err := mergeHookList(e, name, ours, false); err != nil {
			return nil, err
		}
	}
	return e.doc.Bytes(), nil
}

func hasRexEntry(d *jsonedit.Doc, list *jsonedit.Node, command string) bool {
	for _, el := range list.Elems {
		var entry map[string]any
		if d.Decode(el, &entry) == nil && isRexIngestCommand(fmt.Sprint(entry["command"]), command, "cursor") {
			return true
		}
	}
	return false
}

func sameJSON(a, b any) bool {
	ab, aerr := json.Marshal(a)
	bb, berr := json.Marshal(b)
	return aerr == nil && berr == nil && bytes.Equal(ab, bb)
}

// rexCommand matches a whole hook command that only runs rex ingest: an
// optional PowerShell "&", one path (bare, 'single' quoted the POSIX or the
// PowerShell way, or "double" quoted) whose file name starts with rex, then
// "ingest --source <source>".
var rexCommand = regexp.MustCompile(`^\s*(?:&\s*)?(?:"([^"]+)"|'((?:[^']|'\\''|'')+)'|(\S+))\s+ingest\s+--source\s+(\S+)\s*$`)

// isRexIngestCommand recognizes Rex's own hook command, from this install
// (an exact match) or any earlier one. A user's compound command or a
// different tool that happens to mention rex is never matched.
func isRexIngestCommand(command, ours, source string) bool {
	if command == ours {
		return true
	}
	m := rexCommand.FindStringSubmatch(command)
	if m == nil || m[4] != source {
		return false
	}
	path := m[1] + strings.ReplaceAll(m[2], `'\''`, "'") + m[3]
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(path, `\`, "/")))
	base = strings.TrimSuffix(base, ".exe")
	return base == "rex" || strings.HasPrefix(base, "rex-") || strings.HasPrefix(base, "rex_")
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

// Record maps hook payloads from source and appends their events to the
// feed at path (DefaultEventPath when ""). stamp, when set, adds fields to
// the meta of payload i's event: the pin a prompt hook just handed over.
// It returns the first write error, and keeps going after one, so a hook
// can report it on stderr.
func Record(payloads []map[string]any, source, path string, stamp func(i int) map[string]any) error {
	if path == "" {
		path = DefaultEventPath()
	}
	_, err := record(payloads, source, path, stamp)
	return err
}

// record is Record, and what it wrote.
func record(payloads []map[string]any, source, path string, stamp func(i int) map[string]any) ([]Event, error) {
	var written []Event
	var first error
	for i, payload := range payloads {
		event := MapPayload(payload, source)
		if event == nil || strings.TrimSpace(event.Text) == "" {
			continue
		}
		stampOrigin(event, payload)
		if stamp != nil {
			for k, v := range stamp(i) {
				event.Meta[k] = v
			}
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

// ReadPayload reads one hook payload from stdin, within stdinWait.
func ReadPayload(stdin io.Reader) []byte { return readPayload(stdin, stdinWait) }

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
