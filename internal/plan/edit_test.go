package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/jsonedit"
)

const editPlan = `{
  "version": 1,
  "id": "cards",
  "owner": "lucian",
  "status": "pending",
  "tasks": [
    {
      "id": "a",
      "title": "First",
      "status": "active",
      "x-estimate": 3
    },
    {"id": "b", "title": "Inline", "status": "pending"},
    {
      "id": "c",
      "status": "pending",
      "accomplished": ["agent wrote this"]
    }
  ]
}
`

func writePlan(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cards.rex.json")
	if err := os.WriteFile(path, []byte(src), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func changes(cs ...Change) Op {
	return func(*Plan) ([]Change, error) { return cs, nil }
}

// diffLines is the lines of b that are not in a, in order.
func diffLines(a, b string) []string {
	have := map[string]int{}
	for _, l := range strings.Split(a, "\n") {
		have[l]++
	}
	var out []string
	for _, l := range strings.Split(b, "\n") {
		if have[l] > 0 {
			have[l]--
			continue
		}
		out = append(out, l)
	}
	return out
}

func TestPinChangesOnlyStatusValues(t *testing.T) {
	path := writePlan(t, editPlan)
	saved, err := Edit(path, changes(SetStatus("a", Pending), SetStatus("b", Active), SetStatus("", Active)))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(
		`"status": "pending",
  "tasks"`, `"status": "active",
  "tasks"`,
		`"title": "First",
      "status": "active"`, `"title": "First",
      "status": "pending"`,
		`"title": "Inline", "status": "pending"`, `"title": "Inline", "status": "active"`,
	).Replace(editPlan)
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if saved.Plan.Task("b").Status != Active || !bytes.Equal(saved.Data, []byte(want)) {
		t.Fatal("Saved reports what was written")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

func TestDoneAddsLinesOnlyInsideTheTask(t *testing.T) {
	path := writePlan(t, editPlan)
	_, err := Edit(path, changes(
		SetStatus("a", Done),
		SetList("a", "accomplished", []string{"did the thing", "and <this>"}),
		SetList("a", "modified", []string{"internal/tui/plans.go"}),
		SetList("a", "verified_how", nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	added := diffLines(editPlan, string(got))
	wantAdded := []string{
		`      "status": "done",`,
		`      "x-estimate": 3,`,
		`      "accomplished": [`,
		`        "did the thing",`,
		`        "and <this>"`,
		`      ],`,
		`      "modified": [`,
		`        "internal/tui/plans.go"`,
		`      ],`,
		`      "verified_how": []`,
	}
	if strings.Join(added, "\n") != strings.Join(wantAdded, "\n") {
		t.Fatalf("added lines:\n%s\nfile:\n%s", strings.Join(added, "\n"), got)
	}
	start := strings.Index(editPlan, `{"id": "b"`)
	if !strings.HasSuffix(string(got), editPlan[start:]) {
		t.Fatal("bytes after the task changed")
	}
}

func TestEditInlineTaskStaysInline(t *testing.T) {
	path := writePlan(t, editPlan)
	if _, err := Edit(path, changes(SetString("b", "completed_notes", "short"))); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `{"id": "b", "title": "Inline", "status": "pending", "completed_notes": "short"},`) {
		t.Fatalf("%s", got)
	}
}

func TestEditKeepsCRLFAndBOM(t *testing.T) {
	src := "\xef\xbb\xbf" + strings.ReplaceAll(editPlan, "\n", "\r\n")
	path := writePlan(t, src)
	if _, err := Edit(path, changes(SetList("a", "modified", []string{"x.go"}))); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.HasPrefix(got, []byte("\xef\xbb\xbf")) || bytes.Count(got, []byte("\n")) != bytes.Count(got, []byte("\r\n")) {
		t.Fatalf("%q", got)
	}
	if !bytes.Contains(got, []byte("\"modified\": [\r\n        \"x.go\"\r\n      ]")) {
		t.Fatalf("%q", got)
	}
}

func TestEditIntoEmptyAndMinimalPlans(t *testing.T) {
	path := writePlan(t, `{"tasks":[{"id":"a"}]}`)
	if _, err := Edit(path, changes(SetStatus("a", Active), SetStatus("", Active))); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != `{"tasks":[{"id":"a", "status":"active"}], "status":"active"}` {
		t.Fatalf("%s", got)
	}
}

func TestEditRefusals(t *testing.T) {
	cases := []struct{ name, src string }{
		{"newer version", `{"version": 2, "tasks": [{"id": "a"}]}`},
		{"conflict", "{\n<<<<<<< HEAD\n\"tasks\": []\n=======\n>>>>>>> x\n}"},
		{"invalid", `{"tasks": [`},
		{"tasks not list", `{"tasks": {}}`},
		{"utf16", "\xff\xfe{\x00}\x00"},
	}
	for _, c := range cases {
		path := writePlan(t, c.src)
		_, err := Edit(path, changes(SetStatus("a", Done)))
		if !errors.Is(err, ErrReadOnly) {
			t.Errorf("%s: %v", c.name, err)
		}
		if got, _ := os.ReadFile(path); string(got) != c.src {
			t.Errorf("%s: file touched", c.name)
		}
	}
	path := writePlan(t, `{"tasks": [{"id": "a"}, {"id": "a"}]}`)
	if _, err := Edit(path, changes(SetStatus("a", Done))); err == nil || !strings.Contains(err.Error(), "two tasks are a") {
		t.Fatal(err)
	}
	if _, err := Edit(path, changes(SetStatus("zz", Done))); err == nil || !strings.Contains(err.Error(), "task zz is gone") {
		t.Fatal(err)
	}
	for _, c := range []Change{SetString("a", "title", "x"), SetString("a", "does", "x"), SetString("", "accomplished", "x")} {
		if _, err := Edit(path, changes(c)); err == nil || !strings.Contains(err.Error(), "does not write") {
			t.Fatalf("%+v: %v", c, err)
		}
	}
	if _, err := Edit(filepath.Join(t.TempDir(), "gone.rex.json"), changes()); err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Fatal(err)
	}
}

// An agent writes the file between Rex's read and its rename: Rex starts
// over on the new content, and the Op sees it.
func TestEditReappliesAfterAConcurrentWrite(t *testing.T) {
	path := writePlan(t, editPlan)
	agent := strings.Replace(editPlan, `"title": "Inline"`, `"title": "Inline (renamed by agent)"`, 1)
	calls := 0
	_, err := Edit(path, func(p *Plan) ([]Change, error) {
		calls++
		if calls == 1 {
			os.WriteFile(path, []byte(agent), 0o640)
		}
		if p.Task("c").Status != Pending {
			return nil, errors.New("c changed")
		}
		return []Change{SetStatus("c", Active)}, nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("%v after %d calls", err, calls)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "renamed by agent") || !strings.Contains(string(got), "\"id\": \"c\",\n      \"status\": \"active\"") {
		t.Fatalf("%s", got)
	}
}

func TestEditAbortsWhenWhatTheUserSawChanged(t *testing.T) {
	path := writePlan(t, strings.Replace(editPlan, `"id": "c",
      "status": "pending"`, `"id": "c",
      "status": "done"`, 1))
	before, _ := os.ReadFile(path)
	_, err := Edit(path, func(p *Plan) ([]Change, error) {
		if p.Task("c").Status != Pending {
			return nil, errors.New("c is done now")
		}
		return []Change{SetStatus("c", Active)}, nil
	})
	if err == nil || err.Error() != "c is done now" {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("aborted edits write nothing")
	}
}

func TestEditFollowsSymlinksAndClearsStaleTemps(t *testing.T) {
	path := writePlan(t, editPlan)
	link := filepath.Join(t.TempDir(), "link.rex.json")
	if err := os.Symlink(path, link); err != nil {
		t.Skip(err)
	}
	stale := filepath.Join(filepath.Dir(path), ".cards.rex.json.rex-123")
	os.WriteFile(stale, []byte("x"), 0o600)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(stale, old, old)
	if _, err := Edit(link, changes(SetStatus("b", Done))); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a file")
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), `"title": "Inline", "status": "done"`) {
		t.Fatalf("%s", got)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale temp left")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
}

func TestStateMergesAndPrunes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".rex", "state.json")
	repo := t.TempDir()
	planPath := writePlan(t, editPlan)
	now := time.Unix(1_800_000_000, 0)
	if err := UpdateState(path, now, func(s *State) {
		s.Repos[repo] = RepoState{Plan: planPath, Opened: float64(now.Unix())}
		s.Repos["/gone/repo"] = RepoState{Plan: planPath, Opened: float64(now.Unix())}
		s.Pins[PinKey(planPath, "a")] = float64(now.Unix())
		s.Pins[PinKey(planPath, "old")] = float64(now.Add(-100 * 24 * time.Hour).Unix())
	}); err != nil {
		t.Fatal(err)
	}
	// Another window adds its own entry; this one's update must keep it.
	other := LoadState(path)
	other.Pins[PinKey(planPath, "b")] = float64(now.Unix())
	data, _ := json.Marshal(other)
	os.WriteFile(path, data, 0o600)
	UpdateState(path, now, func(s *State) { s.Pins[PinKey(planPath, "c")] = float64(now.Unix()) })
	s := LoadState(path)
	if len(s.Repos) != 1 || s.Repos[repo].Plan != planPath {
		t.Fatalf("repos %+v", s.Repos)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, ok := s.Pins[PinKey(planPath, id)]; !ok {
			t.Fatalf("pin %s lost: %+v", id, s.Pins)
		}
	}
	if _, ok := s.Pins[PinKey(planPath, "old")]; ok {
		t.Fatal("old pin kept")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	os.WriteFile(path, []byte("not json"), 0o600)
	if s := LoadState(path); len(s.Repos) != 0 || s.Pins == nil {
		t.Fatal("a broken state file is an empty state")
	}
}

func FuzzEdit(f *testing.F) {
	f.Add(editPlan, "a", "accomplished", "did it")
	f.Add(strings.ReplaceAll(editPlan, "\n", "\r\n"), "b", "status", "done")
	f.Add(`{"tasks":[{"id":"a","z":{}}]}`, "a", "completed_notes", "")
	f.Add("{\"tasks\":[\n\t{\n\t\t\"id\": \"a\"\n\t}\n]}", "a", "modified", "x\ny")
	f.Fuzz(func(t *testing.T, src, task, field, value string) {
		p, err := Parse([]byte(src))
		if err != nil || p.ReadOnly != "" || !writable[field] {
			return
		}
		c := SetString(task, field, value)
		if field == "accomplished" || field == "modified" || field == "verified_how" {
			c = SetList(task, field, strings.Split(value, "\n"))
		}
		out, err := Apply([]byte(src), []Change{c})
		if err != nil {
			return
		}
		if !json.Valid(bytes.TrimPrefix(out, []byte("\xef\xbb\xbf"))) {
			t.Fatalf("invalid JSON\n%s", out)
		}
		np, err := Parse(out)
		if err != nil {
			t.Fatal(err)
		}
		if field == "status" && np.Task(task) != nil && value != "" && np.Task(task).Status != Status(strings.ToLower(strings.TrimSpace(value))) && statuses[Status(strings.ToLower(strings.TrimSpace(value)))] {
			t.Fatalf("status not written\n%s", out)
		}
		// Bytes outside the target task's object never change.
		doc, _ := jsonedit.Parse([]byte(src))
		obj, _, err := target(doc, task)
		if err != nil {
			return
		}
		bom := len(src) - len(strings.TrimPrefix(src, "\xef\xbb\xbf"))
		head, tail := src[:bom+obj.Start], src[bom+obj.End:]
		if !strings.HasPrefix(string(out), head) || !strings.HasSuffix(string(out), tail) {
			t.Fatalf("bytes outside the task changed\nsrc %q\nout %q", src, out)
		}
	})
}

func TestStateKeepsALongPinAndOddIds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	planPath := writePlan(t, editPlan)
	now := time.Unix(1_800_000_000, 0)
	long := float64(now.Add(-200 * 24 * time.Hour).Unix())
	UpdateState(path, now, func(s *State) {
		s.Pins[PinKey(planPath, "a")] = long
		s.Pins[PinKey(planPath, "issue#12")] = long - 1
	})
	s := LoadState(path)
	if _, ok := s.Pins[PinKey(planPath, "a")]; !ok {
		t.Fatalf("a plan's newest pin is kept however long it runs: %+v", s.Pins)
	}
	UpdateState(path, now, func(s *State) { s.Pins[PinKey(planPath, "issue#12")] = float64(now.Unix()) })
	if _, ok := LoadState(path).Pins[PinKey(planPath, "issue#12")]; !ok {
		t.Fatal("a task id with # survives the write that adds it")
	}
}

func TestEditMatchesIdsLikeParse(t *testing.T) {
	path := writePlan(t, `{"tasks":[{"id":" a ","status":"pending"}]}`)
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Edit(path, func(*Plan) ([]Change, error) { return []Change{SetStatus(p.Tasks[0].ID, Active)}, nil }); err != nil {
		t.Fatalf("a padded id is the task Parse shows: %v", err)
	}
	dup := writePlan(t, `{"tasks":[{"id":"a ","status":"pending"},{"id":"a","status":"done"}]}`)
	if _, err := Edit(dup, func(*Plan) ([]Change, error) { return []Change{SetStatus("a", Active)}, nil }); err == nil || !strings.Contains(err.Error(), "two tasks") {
		t.Fatalf("ids that differ only by spaces are duplicates: %v", err)
	}
}

func TestEditRefusesBadValuesAndReadOnlyFiles(t *testing.T) {
	path := writePlan(t, editPlan)
	for _, c := range []Change{SetStatus("a", "wip"), {Task: "a", Field: "modified", Value: "x.go"}, {Task: "a", Field: "completed_notes", Value: []string{"x"}}} {
		if _, err := Edit(path, func(*Plan) ([]Change, error) { return []Change{c}, nil }); err == nil {
			t.Errorf("%+v was written", c)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	os.Chmod(path, 0o444)
	defer os.Chmod(path, 0o644)
	before, _ := os.ReadFile(path)
	if _, err := Edit(path, func(*Plan) ([]Change, error) { return []Change{SetStatus("b", Active)}, nil }); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("a read-only file: %v", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("a read-only file was rewritten")
	}
}
