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

func TestScopeOriginDecidesOverAFile(t *testing.T) {
	repo, other := t.TempDir(), t.TempDir()
	s := NewScope(repo)
	// Another repo's agent reading a file here is still another repo's.
	ev := Event{Kind: "command", Text: "Read", Meta: map[string]any{"cwd": other, "path": filepath.Join(repo, "a.go")}}
	if in, known := s.Owns(ev); in || !known {
		t.Fatalf("in=%v known=%v", in, known)
	}
	// An agent started above the repo: its file decides, and with no file
	// it is in.
	parent := filepath.Dir(repo)
	if in, _ := s.Owns(Event{Kind: "command", Meta: map[string]any{"cwd": parent, "path": filepath.Join(repo, "a.go")}}); !in {
		t.Fatal("a parent-folder agent editing here is in")
	}
	if in, _ := s.Owns(Event{Kind: "command", Meta: map[string]any{"cwd": parent, "path": filepath.Join(other, "a.go")}}); in {
		t.Fatal("a parent-folder agent editing elsewhere is out")
	}
	if in, known := s.Owns(Event{Kind: "text", Text: "done", Meta: map[string]any{"cwd": parent}}); !in || !known {
		t.Fatal("a parent-folder agent's reply is shown")
	}
	// Prose is never read for a path.
	if _, known := s.Owns(Event{Kind: "text", Text: "see " + filepath.Join(other, "x.go"), Meta: map[string]any{}}); known {
		t.Fatal("a path in a reply is not an origin")
	}
}

func TestScopeDisplayLeavesProseAlone(t *testing.T) {
	repo := t.TempDir()
	s := NewScope(repo)
	reply := "see " + repo + "//a/../b and https://x.dev/"
	if got := s.Display(Event{Kind: "text", Text: reply}).Text; got != reply {
		t.Fatalf("a reply was rewritten: %q", got)
	}
	cmd := s.Display(Event{Kind: "command", Text: "go test " + filepath.Join(repo, "internal") + string(filepath.Separator) + "...", Meta: map[string]any{"command": "x"}})
	if strings.Contains(cmd.Text, repo) || !strings.Contains(cmd.Text, "internal") {
		t.Fatalf("a shell line keeps an absolute repo path: %q", cmd.Text)
	}
}

func TestScopeDisplayKeepsCaseAndHome(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("case-folding filesystems only")
	}
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, "Internal"), 0o755)
	s := NewScope(strings.ToUpper(repo)) // the repo spelled in another case
	abs := filepath.Join(repo, "Internal", "Plans.go")
	if got := s.Display(Event{Kind: "command", Text: "Write " + abs, Meta: map[string]any{"path": abs}}).Text; got != "Write Internal/Plans.go" {
		t.Fatalf("%q", got)
	}
}

func TestStampOriginTakesStringsOnly(t *testing.T) {
	ev := Event{}
	stampOrigin(&ev, map[string]any{"cwd": map[string]any{"x": 1}, "workspace_roots": []any{"/a", 3, map[string]any{}}})
	if _, ok := ev.Meta["cwd"]; ok {
		t.Fatal("an object is no cwd")
	}
	if roots := ev.Meta["roots"].([]any); len(roots) != 1 || roots[0] != "/a" {
		t.Fatalf("roots %v", roots)
	}
}

func TestDedupeKeepsTheSameLineFromTwoRepos(t *testing.T) {
	a := Event{Kind: "command", Text: "go test ./...", TS: 10, Meta: map[string]any{"cwd": "/one"}}
	b := Event{Kind: "command", Text: "go test ./...", TS: 11, Meta: map[string]any{"cwd": "/two"}}
	if IsDuplicate(b, []Event{a}, 4) {
		t.Fatal("two repos running the same command are two events")
	}
	b.Meta["cwd"] = "/one"
	if !IsDuplicate(b, []Event{a}, 4) {
		t.Fatal("the same repo within the window is a duplicate")
	}
}
