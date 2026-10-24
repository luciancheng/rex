package watch

import (
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
	// Only strings count: an object in these fields is no path.
	for _, key := range []string{"cwd", "directory", "workspace"} {
		if v, _ := payload[key].(string); strings.TrimSpace(v) != "" {
			ev.Meta["cwd"] = strings.TrimSpace(v)
			break
		}
	}
	if roots, ok := payload["workspace_roots"].([]any); ok {
		var list []any
		for _, r := range roots {
			if s, _ := r.(string); strings.TrimSpace(s) != "" {
				list = append(list, strings.TrimSpace(s))
			}
		}
		if len(list) > 0 {
			ev.Meta["roots"] = list
		}
	}
	for _, key := range []string{"session_id", "conversation_id", "sessionID", "thread_id"} {
		if v, _ := payload[key].(string); v != "" {
			ev.Meta["session"] = v
			break
		}
	}
	for _, key := range []string{"tool_use_id", "callID", "call_id"} {
		if v, _ := payload[key].(string); v != "" {
			ev.Meta["tool_use_id"] = v
			break
		}
	}
}

// Scope decides whether an event belongs to this repo. The feed is one
// file per machine, so another repo's agent writes to it too.
type Scope struct {
	root  string // the repo, symlinks resolved
	given string // the repo as Rex was started in it
	fold  bool
}

func NewScope(repo string) Scope {
	given := ""
	if repo != "" {
		given = filepath.Clean(repo)
	}
	return Scope{root: resolvePath(repo), given: given, fold: runtime.GOOS == "darwin" || runtime.GOOS == "windows"}
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
	if orig := resolvePath(path); s.fold && rel != "." && len(orig) >= len(rel) {
		// Hand back the path's own case for display, not the folded one.
		if tail := orig[len(orig)-len(rel):]; strings.EqualFold(tail, rel) {
			rel = tail
		}
	}
	return filepath.ToSlash(rel), true
}

// under reports whether the repo is inside dir: an agent started in a
// parent folder of the repo.
func (s Scope) under(dir string) bool {
	d, root := resolvePath(dir), s.root
	if s.fold {
		d, root = strings.ToLower(d), strings.ToLower(root)
	}
	rel, err := filepath.Rel(d, root)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Contains reports whether path is inside the repo.
func (s Scope) Contains(path string) bool {
	_, ok := s.Rel(path)
	return ok && filepath.IsAbs(path)
}

// Owns says whether ev belongs to this repo. Where the event came from
// decides: its working directory or workspace roots, so another repo's
// agent reading a file here stays another repo's (its turn end would be
// hidden too). An agent started in a folder above the repo is decided by
// the file it touched, and is in when it touched none. Only an event with
// no origin at all is decided by its file. known is false when there is
// nothing to go on; such events are shown.
func (s Scope) Owns(ev Event) (in, known bool) {
	var origins []string
	if cwd, _ := ev.Meta["cwd"].(string); filepath.IsAbs(cwd) {
		origins = append(origins, cwd)
	}
	if roots, ok := ev.Meta["roots"].([]any); ok {
		for _, r := range roots {
			if s, ok := r.(string); ok && filepath.IsAbs(s) {
				origins = append(origins, s)
			}
		}
	}
	above := false
	for _, d := range origins {
		if s.Contains(d) {
			return true, true
		}
		above = above || s.under(d)
	}
	if len(origins) > 0 && !above {
		return false, true
	}
	if file := s.eventFile(ev); file != "" {
		return s.Contains(file), true
	}
	return above, above
}

// eventFile is the absolute file an event is about: its meta path, or for
// a tool line, the path in its text. Prose (a reply, a thought, a prompt)
// is never read for paths.
func (s Scope) eventFile(ev Event) string {
	p, _ := ev.Meta["path"].(string)
	if p = strings.TrimSpace(p); p == "" && !ProseKinds[ev.Kind] {
		p = pathFromText(ev.Text)
	}
	if len(p) > 4096 || !filepath.IsAbs(p) {
		return ""
	}
	return p
}

// Display rewrites absolute paths for the AGENT tab: inside the repo to
// repo-relative, under home to ~. Only tool lines are touched, and only at
// their real paths; a reply, thought or prompt is shown as written.
func (s Scope) Display(ev Event) Event {
	if ProseKinds[ev.Kind] {
		return ev
	}
	if p := s.eventFile(ev); p != "" {
		short := ShortPath(p)
		if rel, ok := s.Rel(p); ok {
			short = rel
		} else if home, err := os.UserHomeDir(); err == nil {
			if r, err := filepath.Rel(home, p); err == nil && r == "." {
				short = "~"
			} else if err == nil && !strings.HasPrefix(r, "..") {
				short = "~/" + filepath.ToSlash(r)
			}
		}
		ev.Text = strings.ReplaceAll(ev.Text, p, short)
	}
	ev.Text = s.Relativize(ev.Text)
	return ev
}

// Relativize drops the repo prefix wherever it starts a path in text: the
// longer spelling first (the resolved /private/tmp/x before the given
// /tmp/x, which it ends with).
func (s Scope) Relativize(text string) string {
	roots := []string{s.root, s.given}
	if len(s.given) > len(s.root) {
		roots[0], roots[1] = s.given, s.root
	}
	for _, root := range roots {
		if root != "" && root != string(filepath.Separator) {
			text = stripPrefixAt(text, root+string(filepath.Separator))
		}
	}
	return text
}

// stripPrefixAt removes prefix wherever it starts a path in text: at the
// start, or after a space, a quote, '=', '(' or ':'. A match inside a
// longer path ("/backup/tmp/x/") is left alone.
func stripPrefixAt(text, prefix string) string {
	var b strings.Builder
	for {
		i := strings.Index(text, prefix)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		if i == 0 || strings.ContainsRune(" \t\"'=(:", rune(text[i-1])) {
			b.WriteString(text[:i])
		} else {
			b.WriteString(text[:i+len(prefix)])
		}
		text = text[i+len(prefix):]
	}
}
