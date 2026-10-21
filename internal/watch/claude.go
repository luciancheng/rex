package watch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/luciancheng/rex/internal/jsonedit"
)

// ClaudeHookEvents are the Claude Code hooks Rex registers. There is no
// hook for thinking; Stop carries the final reply.
var ClaudeHookEvents = []string{
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"Notification",
	"Stop",
	"StopFailure",
	"SessionStart",
	"SessionEnd",
}

const claudeHookTimeout = 5

// claudeDir is Claude Code's config folder: $CLAUDE_CONFIG_DIR or ~/.claude.
func claudeDir(home string) string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".claude")
}

// claudeHandler is the hook entry. On POSIX it is a shell command (Claude
// runs it with sh -c, which reads no profile). On Windows, where the shell
// may be Git Bash or PowerShell, it is exec form: the .exe plus args.
func claudeHandler(exe, goos string) map[string]any {
	if goos == "windows" {
		return map[string]any{"type": "command", "command": exe, "args": []any{"ingest", "--source", "claude"}, "timeout": claudeHookTimeout}
	}
	return map[string]any{"type": "command", "command": shellQuote(exe) + " ingest --source claude", "timeout": claudeHookTimeout}
}

func isRexClaudeHandler(h map[string]any, ours map[string]any) bool {
	cmd, _ := h["command"].(string)
	if cmd == ours["command"] {
		return true
	}
	if args, ok := h["args"].([]any); ok && len(args) == 3 && args[0] == "ingest" && args[2] == "claude" {
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(strings.ReplaceAll(cmd, `\`, "/")), ".exe"))
		return base == "rex" || strings.HasPrefix(base, "rex")
	}
	return isRexIngestCommand(cmd, fmt.Sprint(ours["command"]), "claude")
}

// installClaude merges Rex's handler into a Claude Code settings file. Each
// event gets one matcher-less group holding Rex's handler; other groups,
// other handlers in shared groups, and every other setting keep their bytes.
func installClaude(path string, out io.Writer) error {
	ours := claudeHandler(rexExecutable(), runtimeGOOS)
	changed, err := UpdateJSONFile(path, []byte("{}\n"), func(d *jsonedit.Doc) ([]byte, error) {
		return mergeClaudeHooks(d, ours, true)
	})
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(out, "installed claude code hooks at %s\n", path)
	} else {
		fmt.Fprintf(out, "claude code hooks already installed at %s\n", path)
	}
	fmt.Fprintf(out, "feed: %s\n", DefaultEventPath())
	return nil
}

func mergeClaudeHooks(d *jsonedit.Doc, ours map[string]any, want bool) ([]byte, error) {
	e := &editor{doc: d}
	if e.doc.Root().Kind != '{' {
		return nil, fmt.Errorf("settings file is not a JSON object")
	}
	group := map[string]any{"hooks": []any{ours}}
	hooks := e.doc.Root().Member("hooks")
	switch {
	case hooks == nil || (hooks.Kind == '{' && len(hooks.Members) == 0):
		if !want {
			return e.doc.Bytes(), nil
		}
		fresh := map[string]any{}
		for _, name := range ClaudeHookEvents {
			fresh[name] = []any{group}
		}
		if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Set(d.Root(), "hooks", fresh) }); err != nil {
			return nil, err
		}
		return e.doc.Bytes(), nil
	case hooks.Kind != '{':
		return nil, fmt.Errorf(`"hooks" in the settings file is not an object`)
	}
	names := ClaudeHookEvents
	if !want {
		names = nil
		for _, m := range hooks.Members {
			names = append(names, m.Key)
		}
	}
	for _, name := range names {
		if err := mergeClaudeGroups(e, name, ours, group, want); err != nil {
			return nil, err
		}
	}
	return e.doc.Bytes(), nil
}

// mergeClaudeGroups edits one event's list of matcher groups. A group that
// holds only Rex handlers is removed (or kept, once, if it is ours); a
// group that mixes Rex with other handlers is rewritten without Rex.
func mergeClaudeGroups(e *editor, name string, ours, group map[string]any, want bool) error {
	hooks := func(d *jsonedit.Doc) *jsonedit.Node { return d.Root().Member("hooks") }
	node := hooks(e.doc).Member(name)
	if node == nil {
		if want {
			return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Set(hooks(d), name, []any{group}) })
		}
		return nil
	}
	if node.Kind != '[' {
		if want {
			return fmt.Errorf(`"hooks.%s" in the settings file is not a list; fix it by hand, rex did not change it`, name)
		}
		return nil
	}
	kept, touched := false, false
	for i := len(node.Elems) - 1; i >= 0; i-- {
		var g map[string]any
		if e.doc.Decode(node.Elems[i], &g) != nil {
			continue
		}
		handlers, _ := g["hooks"].([]any)
		var others []any
		rex := 0
		for _, h := range handlers {
			if m, ok := h.(map[string]any); ok && isRexClaudeHandler(m, ours) {
				rex++
				continue
			}
			others = append(others, h)
		}
		if rex == 0 {
			continue
		}
		touched = true
		_, hasMatcher := g["matcher"]
		if want && !kept && len(others) == 0 && rex == 1 && !hasMatcher && sameJSON(handlers[0], ours) {
			kept = true
			continue
		}
		var err error
		if len(others) == 0 {
			err = e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.RemoveElem(hooks(d).Member(name), i) })
		} else {
			g["hooks"] = others
			err = e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.ReplaceElem(hooks(d).Member(name), i, g) })
		}
		if err != nil {
			return err
		}
		node = hooks(e.doc).Member(name)
	}
	if want && !kept {
		return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Append(hooks(d).Member(name), group) })
	}
	if !want && touched && len(node.Elems) == 0 {
		return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Remove(hooks(d), name) })
	}
	return nil
}

func uninstallClaude(path string, out io.Writer) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Fprintf(out, "claude: no settings at %s\n", path)
		return nil
	}
	ours := claudeHandler(rexExecutable(), runtimeGOOS)
	changed, err := UpdateJSONFile(path, []byte("{}\n"), func(d *jsonedit.Doc) ([]byte, error) {
		return mergeClaudeHooks(d, ours, false)
	})
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(out, "removed rex from claude code hooks at %s\n", path)
	} else {
		fmt.Fprintf(out, "claude code settings at %s had no rex hooks\n", path)
	}
	return nil
}

func claudeWired(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &settings) != nil {
		return false
	}
	ours := claudeHandler(rexExecutable(), runtimeGOOS)
	for _, groups := range settings.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if isRexClaudeHandler(h, ours) {
					return true
				}
			}
		}
	}
	return false
}

// ClaudeProjectSettings is the per-repo, uncommitted settings file used by
// `--install claude --project`: it wires one repo only and is kept out of
// git through .git/info/exclude.
func ClaudeProjectSettings(repo string) string {
	return filepath.Join(repo, ".claude", "settings.local.json")
}

// InstallClaudeProject wires Claude Code for one repo.
func InstallClaudeProject(repo string, out io.Writer) error {
	path := ClaudeProjectSettings(repo)
	if err := installClaude(path, out); err != nil {
		return err
	}
	excludeFromGit(repo, ".claude/settings.local.json")
	return nil
}

// UninstallClaudeProject takes Rex out of one repo's Claude settings.
func UninstallClaudeProject(repo string, out io.Writer) error {
	return uninstallClaude(ClaudeProjectSettings(repo), out)
}

// excludeFromGit adds pattern to .git/info/exclude so a local file never
// shows up as an untracked change.
func excludeFromGit(repo, pattern string) {
	info := filepath.Join(repo, ".git", "info")
	if st, err := os.Stat(filepath.Join(repo, ".git")); err != nil || !st.IsDir() {
		return
	}
	path := filepath.Join(info, "exclude")
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == pattern {
			return
		}
	}
	if err := os.MkdirAll(info, 0o755); err != nil {
		return
	}
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}
	_ = os.WriteFile(path, append(data, []byte(pattern+"\n")...), 0o644)
}

func init() {
	registerHarness(Harness{
		Name:    "claude",
		Label:   "Claude Code",
		Config:  func(home string) string { return filepath.Join(claudeDir(home), "settings.json") },
		Present: func(home string) bool { return dirExists(claudeDir(home)) },
		Wired:   func(home string) bool { return claudeWired(filepath.Join(claudeDir(home), "settings.json")) },
		Install: func(home string, out io.Writer) error {
			if !dirExists(claudeDir(home)) {
				fmt.Fprintf(out, "claude: Claude Code isn't set up here (no %s); nothing changed\n", claudeDir(home))
				return nil
			}
			return installClaude(filepath.Join(claudeDir(home), "settings.json"), out)
		},
		Uninstall: func(home string, out io.Writer) error {
			return uninstallClaude(filepath.Join(claudeDir(home), "settings.json"), out)
		},
	})
}
