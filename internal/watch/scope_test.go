package watch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIngestStampsOrigin(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	raw := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"},"cwd":"/work/app","session_id":"s1","tool_use_id":"t9"}`
	got := IngestRaw(raw, "claude", feed)
	if len(got) != 1 {
		t.Fatalf("%#v", got)
	}
	m := got[0].Meta
	if m["cwd"] != "/work/app" || m["session"] != "s1" || m["tool_use_id"] != "t9" {
		t.Fatalf("%#v", m)
	}
	cursor := IngestRaw(`{"hook_event_name":"beforeSubmitPrompt","prompt":"hi","workspace_roots":["/a","/b"],"conversation_id":"c1"}`, "cursor", feed)
	if roots, _ := cursor[0].Meta["roots"].([]any); len(roots) != 2 || cursor[0].Meta["session"] != "c1" {
		t.Fatalf("%#v", cursor[0].Meta)
	}
}

func TestScopeOwns(t *testing.T) {
	repo := t.TempDir()
	other := t.TempDir()
	s := NewScope(repo)
	cases := []struct {
		meta      map[string]any
		in, known bool
	}{
		{map[string]any{"cwd": repo}, true, true},
		{map[string]any{"cwd": filepath.Join(repo, "sub")}, true, true},
		{map[string]any{"cwd": other}, false, true},
		{map[string]any{"roots": []any{other, repo}}, true, true},
		{map[string]any{"path": filepath.Join(repo, "a.go")}, true, true},
		{map[string]any{"path": filepath.Join(other, "a.go")}, false, true},
		{map[string]any{}, false, false},
		{map[string]any{"cwd": "relative/dir"}, false, false},
	}
	for _, c := range cases {
		in, known := s.Owns(Event{Kind: "command", Text: "x", Meta: c.meta})
		if in != c.in || known != c.known {
			t.Errorf("%v: in=%v known=%v", c.meta, in, known)
		}
	}
}

func TestScopeSeesThroughSymlinksAndCase(t *testing.T) {
	repo := t.TempDir()
	resolved, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	s := NewScope(repo)
	if in, _ := s.Owns(Event{Meta: map[string]any{"cwd": resolved}}); !in {
		t.Fatalf("%s vs %s", repo, resolved)
	}
	link := filepath.Join(t.TempDir(), "link")
	if os.Symlink(repo, link) == nil {
		if in, _ := s.Owns(Event{Meta: map[string]any{"cwd": link}}); !in {
			t.Fatal("a symlink to the repo is the repo")
		}
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		if in, _ := s.Owns(Event{Meta: map[string]any{"cwd": strings.ToUpper(resolved)}}); !in {
			t.Fatal("case-insensitive filesystems compare case-folded")
		}
	}
}

func TestScopeDisplayIsRepoRelative(t *testing.T) {
	repo := t.TempDir()
	s := NewScope(repo)
	abs := filepath.Join(repo, "internal", "tui", "plans.go")
	ev := s.Display(Event{Kind: "command", Text: "Write " + abs, Meta: map[string]any{"tool": "Write", "path": abs}})
	if ev.Text != "Write internal/tui/plans.go" {
		t.Fatalf("%q", ev.Text)
	}
}
