package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// stampOrigin copies where an event came from out of the raw hook payload:
// the working directory, workspace roots, session and tool call ids. The
// field names are the ones harnesses share; nothing here is per vendor.
func stampOrigin(ev *Event, payload map[string]any) {
	if ev.Meta == nil {
		ev.Meta = map[string]any{}
	}
	for _, key := range []string{"cwd", "directory", "workspace"} {
		if v := strings.TrimSpace(strField(payload, key)); v != "" {
			ev.Meta["cwd"] = v
			break
		}
	}
	if roots, ok := payload["workspace_roots"].([]any); ok {
		var list []any
		for _, r := range roots {
			if s := strings.TrimSpace(fmt.Sprint(r)); s != "" && s != "<nil>" {
				list = append(list, s)
			}
		}
		if len(list) > 0 {
			ev.Meta["roots"] = list
		}
	}
	for _, key := range []string{"session_id", "conversation_id", "sessionID", "thread_id"} {
		if v := strField(payload, key); v != "" {
			ev.Meta["session"] = v
			break
		}
	}
	for _, key := range []string{"tool_use_id", "callID", "call_id"} {
		if v := strField(payload, key); v != "" {
			ev.Meta["tool_use_id"] = v
			break
		}
	}
}

// Scope decides whether an event belongs to this repo. The feed is one
// file per machine, so another repo's agent writes to it too.
type Scope struct {
	root string
	fold bool
}

func NewScope(repo string) Scope {
	return Scope{root: resolvePath(repo), fold: runtime.GOOS == "darwin" || runtime.GOOS == "windows"}
}

// resolvePath cleans a path and resolves symlinks (macOS /tmp is
// /private/tmp) through its nearest existing ancestor, so a file that was
// deleted or not yet written still resolves like its folder.
func resolvePath(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// Rel returns path relative to the repo, if it is inside it.
func (s Scope) Rel(path string) (string, bool) {
	if s.root == "" || path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(filepath.Clean(path)), true
	}
	p := resolvePath(path)
	root := s.root
	if s.fold {
		p, root = strings.ToLower(p), strings.ToLower(root)
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if s.fold {
		// Hand back the original case for display.
		if r, err := filepath.Rel(s.root, resolvePath(path)); err == nil && strings.EqualFold(r, rel) {
			rel = r
		}
	}
	return filepath.ToSlash(rel), true
}

// Contains reports whether path is inside the repo.
func (s Scope) Contains(path string) bool {
	_, ok := s.Rel(path)
	return ok && filepath.IsAbs(path)
}

// Owns says whether ev belongs to this repo. known is false when the event
// carries no working directory, root or absolute path; such events are shown.
func (s Scope) Owns(ev Event) (in, known bool) {
	var dirs []string
	if cwd, _ := ev.Meta["cwd"].(string); cwd != "" {
		dirs = append(dirs, cwd)
	}
	if roots, ok := ev.Meta["roots"].([]any); ok {
		for _, r := range roots {
			if s, ok := r.(string); ok {
				dirs = append(dirs, s)
			}
		}
	}
	if p := EventPath(ev); filepath.IsAbs(p) {
		dirs = append(dirs, p)
	}
	for _, d := range dirs {
		if !filepath.IsAbs(d) {
			continue
		}
		known = true
		if s.Contains(d) || s.inside(d) {
			return true, true
		}
	}
	return false, known
}

// inside covers a cwd that is the repo root itself or a folder above a
// path in it (an agent started in a parent folder of the repo).
func (s Scope) inside(dir string) bool {
	d := resolvePath(dir)
	root := s.root
	if s.fold {
		d, root = strings.ToLower(d), strings.ToLower(root)
	}
	return d == root
}

// Display rewrites absolute paths inside the repo to repo-relative ones and
// paths under home to ~, for the tape.
func (s Scope) Display(ev Event) Event {
	p := EventPath(ev)
	if p == "" || !filepath.IsAbs(p) {
		return ev
	}
	short := ShortPath(p)
	if rel, ok := s.Rel(p); ok {
		short = rel
	} else if home, err := os.UserHomeDir(); err == nil {
		if r, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(r, "..") {
			short = "~/" + filepath.ToSlash(r)
		}
	}
	ev.Text = strings.ReplaceAll(ev.Text, p, short)
	return ev
}
