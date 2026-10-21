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
	if args, ok := h["args"].([]any); ok && len(args) == 3 && args[0] == "ingest" && args[1] == "--source" && args[2] == "claude" {
		// Exec form: the same binary name as ours, or one called rex.
		base := func(p string) string {
			return strings.ToLower(strings.TrimSuffix(filepath.Base(strings.ReplaceAll(p, `\`, "/")), ".exe"))
		}
		return base(cmd) == "rex" || base(cmd) == base(fmt.Sprint(ours["command"]))
	}
	return isRexIngestCommand(cmd, fmt.Sprint(ours["command"]), "claude")
}

// installClaude merges Rex's handler into a Claude Code settings file. Each
// event gets one matcher-less group holding Rex's handler; other groups,
// other handlers in shared groups, and every other setting keep their bytes.
func installClaude(path string, out io.Writer) error {
	ours := claudeHandler(rexExecutable(), runtimeGOOS)
	// A new file starts multi-line, so the hooks are written one per line.
	changed, err := UpdateJSONFile(path, []byte("{\n}\n"), func(d *jsonedit.Doc) ([]byte, error) {
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
	return mergeGroupHooks(d, groupHooks{events: ClaudeHookEvents, isRex: isRexClaudeHandler, file: "settings file"}, ours, want)
}

// groupHooks describes a config whose hooks are event → matcher groups →
// handlers, as in Claude Code and Codex.
type groupHooks struct {
	events []string
	isRex  func(h, ours map[string]any) bool
	file   string // for error messages: "settings file", "hooks.json"
}

func mergeGroupHooks(d *jsonedit.Doc, spec groupHooks, ours map[string]any, want bool) ([]byte, error) {
	e := &editor{doc: d}
	if e.doc.Root().Kind != '{' {
		return nil, fmt.Errorf("%s is not a JSON object", spec.file)
	}
	group := map[string]any{"hooks": []any{ours}}
	hooks := e.doc.Root().Member("hooks")
	switch {
	case hooks == nil || (hooks.Kind == '{' && len(hooks.Members) == 0):
		if !want {
			return e.doc.Bytes(), nil
		}
		fresh := map[string]any{}
		for _, name := range spec.events {
			fresh[name] = []any{group}
		}
		if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Set(d.Root(), "hooks", fresh) }); err != nil {
			return nil, err
		}
		return e.doc.Bytes(), nil
	case hooks.Kind != '{':
		return nil, fmt.Errorf(`"hooks" in the %s is not an object`, spec.file)
	}
	names := spec.events
	if !want {
		names = nil
		for _, m := range hooks.Members {
			names = append(names, m.Key)
		}
	}
	for _, name := range names {
		if err := mergeGroups(e, spec, name, ours, group, want); err != nil {
			return nil, err
		}
	}
	if h := e.doc.Root().Member("hooks"); !want && len(hooks.Members) > 0 && h != nil && h.Kind == '{' && len(h.Members) == 0 {
		// Rex emptied it: take the key out too.
		if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Remove(d.Root(), "hooks") }); err != nil {
			return nil, err
		}
	}
	return e.doc.Bytes(), nil
}

// mergeGroups edits one event's list of matcher groups. Only Rex's
// handlers are spliced out; a group left with none of the user's handlers
// goes too. Rex's own group (no matcher, Rex's handler alone) stays where
// it is, and on reinstall only a changed command is rewritten, so the
// user's tweaks to it (a longer timeout) survive.
func mergeGroups(e *editor, spec groupHooks, name string, ours, group map[string]any, want bool) error {
	hooks := func(d *jsonedit.Doc) *jsonedit.Node { return d.Root().Member("hooks") }
	list := func(d *jsonedit.Doc) *jsonedit.Node { return hooks(d).Member(name) }
	node := list(e.doc)
	if node == nil {
		if want {
			return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Set(hooks(d), name, []any{group}) })
		}
		return nil
	}
	if node.Kind != '[' {
		if want {
			return fmt.Errorf(`"hooks.%s" in the %s is not a list; fix it by hand, rex did not change it`, name, spec.file)
		}
		return nil
	}
	kept, touched := false, false
	for i := len(node.Elems) - 1; i >= 0; i-- {
		g := node.Elems[i]
		inner := g.Member("hooks")
		if g.Kind != '{' || inner == nil || inner.Kind != '[' {
			continue
		}
		var rex []int
		for j, h := range inner.Elems {
			var m map[string]any
			if e.doc.Decode(h, &m) == nil && spec.isRex(m, ours) {
				rex = append(rex, j)
			}
		}
		if len(rex) == 0 {
			continue
		}
		touched = true
		only := len(rex) == len(inner.Elems)
		if want && !kept && only && len(rex) == 1 && g.Member("matcher") == nil {
			kept = true
			if err := refreshRexHandler(e, func(d *jsonedit.Doc) *jsonedit.Node { return list(d).Elems[i].Member("hooks").Elems[0] }, ours); err != nil {
				return err
			}
			continue
		}
		if only {
			if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.RemoveElem(list(d), i) }); err != nil {
				return err
			}
		} else {
			for k := len(rex) - 1; k >= 0; k-- {
				j := rex[k]
				if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.RemoveElem(list(d).Elems[i].Member("hooks"), j) }); err != nil {
					return err
				}
			}
		}
		node = list(e.doc)
	}
	if want && !kept {
		return e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Append(list(d), group) })
	}
	if !want && touched && len(node.Elems) == 0 {
		if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Remove(hooks(d), name) }); err != nil {
			return err
		}
	}
	return nil
}

// refreshRexHandler rewrites the command of Rex's handler (and its args or
// commandWindows) if the binary moved, and leaves its other fields alone.
func refreshRexHandler(e *editor, handler func(*jsonedit.Doc) *jsonedit.Node, ours map[string]any) error {
	for _, key := range []string{"command", "args", "commandWindows"} {
		want, ok := ours[key]
		if !ok {
			// Ours has no args (the shell form): an exec-form entry's args
			// would make the new command string fail, so they go. A
			// commandWindows stays: the same file may be read on Windows.
			if key == "args" && handler(e.doc).Member(key) != nil {
				if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Remove(handler(d), key) }); err != nil {
					return err
				}
			}
			continue
		}
		var have any
		if n := handler(e.doc).Member(key); n != nil {
			_ = e.doc.Decode(n, &have)
		}
		if sameJSON(have, want) {
			continue
		}
		if err := e.apply(func(d *jsonedit.Doc) ([]byte, error) { return d.Set(handler(d), key, want) }); err != nil {
			return err
		}
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
	// Decoded loosely: one malformed entry of the user's must not hide ours.
	var settings struct {
		Hooks map[string]any `json:"hooks"`
	}
	if json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &settings) != nil {
		return false
	}
	ours := claudeHandler(rexExecutable(), runtimeGOOS)
	for _, groups := range settings.Hooks {
		list, _ := groups.([]any)
		for _, g := range list {
			handlers, _ := asDict(g)["hooks"].([]any)
			for _, h := range handlers {
				if m, ok := h.(map[string]any); ok && isRexClaudeHandler(m, ours) {
					return true
				}
			}
		}
	}
	return false
}

// ClaudeProjectSettings is the per-repo, uncommitted settings file used by
// `--install claude --project`: it wires one repo only and is kept out of
// git through the repo's info/exclude.
func ClaudeProjectSettings(repo string) string {
	return filepath.Join(repo, ".claude", "settings.local.json")
}

// InstallClaudeProject wires Claude Code for one git repo. The file names
// this machine's rex binary, so it, and its .bak, are git-excluded first;
// outside a repo nothing is written.
func InstallClaudeProject(repo string, out io.Writer) error {
	dir, err := gitDir(repo)
	if err != nil {
		return fmt.Errorf("--project needs a git repo, and %s isn't one; run it from inside the repo", repo)
	}
	for _, pattern := range []string{".claude/settings.local.json", ".claude/settings.local.json.bak"} {
		if err := excludeFromGit(dir, pattern); err != nil {
			return fmt.Errorf("couldn't keep %s out of git (%v); nothing changed", pattern, err)
		}
	}
	if err := installClaude(ClaudeProjectSettings(repo), out); err != nil {
		return err
	}
	fmt.Fprintf(out, "kept out of git via %s\n", filepath.Join(dir, "info", "exclude"))
	return nil
}

// UninstallClaudeProject takes Rex out of one repo's Claude settings.
func UninstallClaudeProject(repo string, out io.Writer) error {
	return uninstallClaude(ClaudeProjectSettings(repo), out)
}

// ClaudeProjectWired reports whether repo's own Claude settings run Rex:
// the git-excluded local file, or a committed .claude/settings.json.
func ClaudeProjectWired(repo string) bool {
	return repo != "" && (claudeWired(ClaudeProjectSettings(repo)) || claudeWired(filepath.Join(repo, ".claude", "settings.json")))
}

// gitDir is the folder holding repo's info/exclude: .git itself, or, in a
// worktree or submodule where .git is a file, the git dir it points to
// (for a worktree, the main repo's, through commondir).
func gitDir(repo string) (string, error) {
	dot := filepath.Join(repo, ".git")
	st, err := os.Stat(dot)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return dot, nil
	}
	data, err := os.ReadFile(dot)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	if !strings.HasPrefix(line, "gitdir:") {
		return "", fmt.Errorf("%s is not a git link", dot)
	}
	dir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repo, dir)
	}
	if c, err := os.ReadFile(filepath.Join(dir, "commondir")); err == nil {
		common := strings.TrimSpace(string(c))
		if !filepath.IsAbs(common) {
			common = filepath.Join(dir, common)
		}
		dir = common
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s points at %s, which is not a folder", dot, dir)
	}
	return filepath.Clean(dir), nil
}

// excludeFromGit adds pattern to <gitdir>/info/exclude so a local file
// never shows up as an untracked change.
func excludeFromGit(dir, pattern string) error {
	info := filepath.Join(dir, "info")
	path := filepath.Join(info, "exclude")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}
	if err := os.MkdirAll(info, 0o755); err != nil {
		return err
	}
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}
	return os.WriteFile(path, append(data, []byte(pattern+"\n")...), 0o644)
}

func init() {
	registerHarness(Harness{
		Name:      "claude",
		Label:     "Claude Code",
		Config:    func(home string) string { return filepath.Join(claudeDir(home), "settings.json") },
		Present:   func(home string) bool { return dirExists(claudeDir(home)) },
		Wired:     func(home string) bool { return claudeWired(filepath.Join(claudeDir(home), "settings.json")) },
		WiredHere: ClaudeProjectWired,
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
