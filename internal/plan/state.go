package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

// State is what Rex remembers between runs, outside any repo: the plan
// last opened in each repo. It is a convenience; a missing or broken state
// file is an empty State. (An older Rex also kept pin start times under
// "pins"; they are ignored, and dropped on the next write.)
type State struct {
	Version int                  `json:"version"`
	Repos   map[string]RepoState `json:"repos"`
}

type RepoState struct {
	Plan   string  `json:"plan"` // absolute path
	Opened float64 `json:"opened"`
}

// stateTTL is how long an entry Rex has not touched is kept.
const stateTTL = 90 * 24 * time.Hour

// StatePath is ~/.rex/state.json.
func StatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".rex", "state.json")
}

// LoadState reads path. Any error gives an empty State.
func LoadState(path string) State {
	s := State{Version: 1, Repos: map[string]RepoState{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var read State
	if json.Unmarshal(data, &read) != nil {
		return s
	}
	for k, v := range read.Repos {
		s.Repos[k] = v
	}
	return s
}

// Last is the plan last opened in repo, as the state names it, or "". A
// key that spells the same folder another way (a symlink, another case)
// counts, and of those the newest wins: an agent's hook may name the repo
// by its real path while Rex was started through a link. The path is not
// checked: callers put it through Jail.
func (s State) Last(repo string) string {
	keys := make([]string, 0, len(s.Repos))
	for k := range s.Repos {
		if k != repo {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if _, ok := s.Repos[repo]; ok {
		keys = append([]string{repo}, keys...) // a tie goes to the exact key
	}
	info, err := os.Stat(repo)
	var last *RepoState
	for _, k := range keys {
		if k != repo {
			if err != nil {
				break
			}
			if i, err := os.Stat(k); err != nil || !os.SameFile(info, i) {
				continue
			}
		}
		if r := s.Repos[k]; last == nil || r.Opened > last.Opened {
			last = &r
		}
	}
	if last == nil {
		return ""
	}
	return last.Plan
}

// UpdateState re-reads path, applies mutate, prunes repos that are gone or
// untouched for 90 days, and writes it atomically, private to the user.
// Re-reading first keeps what another Rex window wrote in the meantime.
func UpdateState(path string, now time.Time, mutate func(*State)) error {
	if path == "" {
		return nil
	}
	s := LoadState(path)
	mutate(&s)
	cutoff := float64(now.Add(-stateTTL).Unix())
	for repo, r := range s.Repos {
		if r.Opened < cutoff || !exists(repo) || !exists(r.Plan) {
			delete(s.Repos, repo)
		}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state.json.rex-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		if runtime.GOOS != "windows" {
			return err
		}
		// Windows refuses to replace a file another process holds open.
		return os.WriteFile(path, append(data, '\n'), 0o600)
	}
	return nil
}

// exists is false only for a path known to be gone; one that can't be
// read right now (no permission) is kept. An unmounted volume's path is
// gone as far as the OS says, so its entries go too.
func exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
