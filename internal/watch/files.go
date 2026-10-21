package watch

import (
	"os"
	"path/filepath"
	"strings"
)

func toolMeta(tool, path string) map[string]any {
	meta := map[string]any{"tool": tool}
	if path != "" {
		meta["path"] = path
	}
	return meta
}

func MetaTool(ev Event) string {
	if ev.Meta == nil {
		return ""
	}
	tool, _ := ev.Meta["tool"].(string)
	return tool
}

func EventPath(ev Event) string {
	if ev.Meta != nil {
		if p, ok := ev.Meta["path"].(string); ok && strings.TrimSpace(p) != "" {
			return strings.TrimSpace(p)
		}
	}
	return pathFromText(ev.Text)
}

// EventPaths is every file an event names: all files of a multi-file
// patch (meta "paths"), else EventPath.
func EventPaths(ev Event) []string {
	if list, ok := ev.Meta["paths"].([]any); ok && len(list) > 0 {
		var out []string
		for _, p := range list {
			if s, ok := p.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if p := EventPath(ev); p != "" {
		return []string{p}
	}
	return nil
}

func pathFromText(text string) string {
	s := strings.TrimSpace(text)
	s = strings.TrimPrefix(s, "$ ")
	lower := strings.ToLower(s)
	for _, prefix := range []string{"wrote ", "deleted ", "writing ", "read "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(s[len(prefix):])
		}
	}
	parts := strings.SplitN(s, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if strings.ContainsAny(parts[0], `/\:`) {
		return ""
	}
	rest := strings.TrimSpace(parts[1])
	if rest == "" || strings.Contains(rest, "://") {
		return ""
	}
	if strings.ContainsAny(rest, `/\:`) || strings.HasPrefix(rest, "~") {
		return rest
	}
	return ""
}

func IsShellTool(tool string) bool {
	t := strings.ToLower(strings.TrimSpace(tool))
	t = strings.ReplaceAll(t, "_", "")
	t = strings.ReplaceAll(t, "-", "")
	switch t {
	case "shell", "bash", "zsh", "sh", "cmd", "powershell", "pwsh", "terminal":
		return true
	}
	return false
}

func IsFileLookTool(tool string) bool {
	t := strings.ToLower(strings.TrimSpace(tool))
	t = strings.ReplaceAll(t, "_", "")
	t = strings.ReplaceAll(t, "-", "")
	if t == "" {
		return false
	}
	for _, needle := range []string{
		"read", "grep", "glob", "search", "semsearch", "semanticsearch",
		"ls", "listdir", "readdir", "cat", "view",
	} {
		if t == needle || strings.HasPrefix(t, needle) {
			return true
		}
	}
	return t == "list"
}

func IsFileEditEvent(ev Event) bool {
	if IsFileEditTool(MetaTool(ev)) {
		return true
	}
	t := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ev.Text), "$ ")))
	for _, prefix := range []string{"wrote ", "deleted ", "writing ", "write ", "strreplace "} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

func IsFileLookEvent(ev Event) bool {
	if ev.Kind != "command" && ev.Kind != "result" {
		return false
	}
	if IsFileEditEvent(ev) || IsShellTool(MetaTool(ev)) {
		return false
	}
	tool := MetaTool(ev)
	if IsFileLookTool(tool) {
		return true
	}
	if p, _ := ev.Meta["path"].(string); strings.TrimSpace(p) != "" {
		return !strings.Contains(p, "://")
	}
	if tool != "" {
		// A named tool that isn't a file look ("plan ready /r/plan.md",
		// "Agent explore internal/watch"): a slash in its line is no path.
		return false
	}
	p := pathFromText(ev.Text)
	return p != "" && !strings.Contains(p, "://")
}

func ShortPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, p); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			p = rel
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		home = filepath.Clean(home)
		if p == home {
			return "~"
		}
		prefix := home + string(os.PathSeparator)
		if strings.HasPrefix(p, prefix) {
			return "~/" + filepath.ToSlash(p[len(prefix):])
		}
	}
	return filepath.ToSlash(p)
}

func IsQuietResult(ev Event) bool {
	return ev.Kind == "result"
}

// KeepInPane says whether an event is drawn on the tape. Idle events only
// end the turn; they have nothing to show.
func KeepInPane(ev Event) bool {
	return ev.Kind != "idle" && !IsFileLookEvent(ev) && !IsQuietResult(ev)
}
