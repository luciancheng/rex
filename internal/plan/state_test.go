package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writePlan(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cards.rex.json")
	if err := os.WriteFile(path, []byte(src), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStateMergesAndPrunes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".rex", "state.json")
	repo, other := t.TempDir(), t.TempDir()
	planPath := writePlan(t, `{"tasks": []}`)
	now := time.Unix(1_800_000_000, 0)
	if err := UpdateState(path, now, func(s *State) {
		s.Repos[repo] = RepoState{Plan: planPath, Opened: float64(now.Unix())}
		s.Repos["/gone/repo"] = RepoState{Plan: planPath, Opened: float64(now.Unix())}
		s.Repos[t.TempDir()] = RepoState{Plan: planPath, Opened: float64(now.Add(-100 * 24 * time.Hour).Unix())}
	}); err != nil {
		t.Fatal(err)
	}
	// Another window adds its own entry; this one's update must keep it.
	s := LoadState(path)
	s.Repos[other] = RepoState{Plan: planPath, Opened: float64(now.Unix())}
	data, _ := json.Marshal(s)
	os.WriteFile(path, data, 0o600)
	UpdateState(path, now, func(*State) {})
	s = LoadState(path)
	if len(s.Repos) != 2 || s.Repos[repo].Plan != planPath || s.Repos[other].Plan != planPath {
		t.Fatalf("repos %+v", s.Repos)
	}
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 { // Windows has no Unix modes
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	os.WriteFile(path, []byte("not json"), 0o600)
	if s := LoadState(path); len(s.Repos) != 0 || s.Repos == nil {
		t.Fatal("a broken state file is an empty state")
	}
}

// An older Rex kept pin start times beside the last plans. Such a file
// still reads, and the next write keeps only the plans.
func TestStateReadsAnOldFileWithPins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	repo := t.TempDir()
	planPath := writePlan(t, `{"tasks": []}`)
	now := time.Unix(1_800_000_000, 0)
	old, _ := json.Marshal(map[string]any{
		"version": 1,
		"repos":   map[string]any{repo: map[string]any{"plan": planPath, "opened": now.Unix()}},
		"pins":    map[string]any{planPath + "#a": now.Unix()},
	})
	os.WriteFile(path, old, 0o600)
	if s := LoadState(path); s.Repos[repo].Plan != planPath {
		t.Fatalf("the last plan reads past the old pins: %+v", s)
	}
	if err := UpdateState(path, now, func(*State) {}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var got map[string]any
	if json.Unmarshal(data, &got) != nil || got["pins"] != nil || got["repos"] == nil {
		t.Fatalf("pins are dropped on the next write, the plans kept:\n%s", data)
	}
}
