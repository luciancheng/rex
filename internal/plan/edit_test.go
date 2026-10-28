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

func changes(cs ...Change) Op {
	return func(*Plan) ([]Change, error) { return cs, nil }
}

// edit runs op on a plan file holding src and returns what is on disk.
func edit(t *testing.T, src string, op Op) string {
	t.Helper()
	path := writePlan(t, src)
	if _, err := Edit(path, op); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	return string(got)
}

func TestEditChangesOnlyStatusValues(t *testing.T) {
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
	if saved.Plan.Task("b").Status != Active || !bytes.Equal(saved.Data, []byte(want)) || saved.From != Hash([]byte(editPlan)) || saved.Base.Task("b").Status != Pending {
		t.Fatal("Saved reports what was written, and what it was written over")
	}
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 { // Windows has no Unix modes
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

// docs/ux.md §7b: a write splices only status values. Line endings, a
// BOM, tabs, unknown fields and a minified layout stay byte for byte, and
// a task with no status gets one as its last member, laid out like its
// neighbours.
func TestEditKeepsEveryOtherByte(t *testing.T) {
	crlf := "\xef\xbb\xbf" + strings.ReplaceAll(editPlan, "\n", "\r\n")
	tabs := "{\n\t\"version\": 1,\n\t\"status\": \"active\",\n\t\"x-team\": {\"owner\": \"ana\"},\n\t\"tasks\": [\n\t\t{\n\t\t\t\"id\": \"a\",\n\t\t\t\"title\": \"A\",\n\t\t\t\"x-points\": [1, 2]\n\t\t},\n\t\t{\n\t\t\t\"id\": \"b\",\n\t\t\t\"status\": \"done\"\n\t\t}\n\t]\n}\n"
	minified := `{"version":1,"status":"active","tasks":[{"id":"a","status":"pending","x":{"y":[1,2.50]}},{"id":"b","status":"active"}]}`
	for _, c := range []struct {
		name, src string
		op        Op
		want      string
	}{
		{"CRLF and a BOM", crlf, PinOp(Seen{ID: "b", Status: Pending, Moving: []string{"a"}}), strings.NewReplacer(
			"\"status\": \"pending\",\r\n  \"tasks\"", "\"status\": \"active\",\r\n  \"tasks\"",
			"\"title\": \"First\",\r\n      \"status\": \"active\"", "\"title\": \"First\",\r\n      \"status\": \"pending\"",
			`"title": "Inline", "status": "pending"`, `"title": "Inline", "status": "active"`,
		).Replace(crlf)},
		{"tabs and no status", tabs, PinOp(Seen{ID: "a", Status: Pending}), strings.Replace(tabs, "\t\t\t\"x-points\": [1, 2]\n", "\t\t\t\"x-points\": [1, 2],\n\t\t\t\"status\": \"active\"\n", 1)},
		{"minified", minified, DoneOp(Seen{ID: "b", Status: Active}), strings.Replace(minified, `{"id":"b","status":"active"}`, `{"id":"b","status":"done"}`, 1)},
		{"unknown fields", editPlan, DoneOp(Seen{ID: "c", Status: Pending}), strings.Replace(editPlan, `"id": "c",
      "status": "pending"`, `"id": "c",
      "status": "done"`, 1)},
	} {
		if got := edit(t, c.src, c.op); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// docs/ux.md §7b: the plan's own status follows the tasks only where the
// file has one; Rex never adds a member at the plan's root.
func TestEditPlanStatusRules(t *testing.T) {
	plan := func(status string, tasks string) string {
		if status == "" {
			return `{"version": 1, "tasks": [` + tasks + `]}`
		}
		return `{"version": 1, "status": "` + status + `", "tasks": [` + tasks + `]}`
	}
	two := `{"id": "a", "status": "%s"}, {"id": "b", "status": "%s"}`
	tasks := func(a, b Status) string {
		return strings.Replace(strings.Replace(two, "%s", string(a), 1), "%s", string(b), 1)
	}
	for _, c := range []struct {
		name     string
		src      string
		op       Op
		task, pl Status // a's status and the plan's after, "" for no plan status
	}{
		{"p: pending plan goes active", plan("pending", tasks(Pending, Pending)), PinOp(Seen{ID: "a", Status: Pending}), Active, Active},
		{"p: done plan goes active", plan("done", tasks(Done, Done)), PinOp(Seen{ID: "a", Status: Done}), Active, Active},
		{"p: a blocked plan stays", plan("blocked", tasks(Pending, Pending)), PinOp(Seen{ID: "a", Status: Pending}), Active, Blocked},
		{"p: no plan status, none added", plan("", tasks(Pending, Pending)), PinOp(Seen{ID: "a", Status: Pending}), Active, ""},
		{"p: a skipped task", plan("active", tasks(Skipped, Pending)), PinOp(Seen{ID: "a", Status: Skipped}), Active, Active},
		{"p: a blocked task", plan("active", tasks(Blocked, Pending)), PinOp(Seen{ID: "a", Status: Blocked}), Active, Active},
		{"unpin: the plan stays", plan("active", tasks(Active, Pending)), UnpinOp(Seen{ID: "a", Status: Active}), Pending, Active},
		{"d: the last leaf finishes the plan", plan("active", tasks(Active, Skipped)), DoneOp(Seen{ID: "a", Status: Active}), Done, Done},
		{"d: a leaf left, the plan stays", plan("active", tasks(Active, Pending)), DoneOp(Seen{ID: "a", Status: Active}), Done, Active},
		{"d: no plan status, none added", plan("", tasks(Active, Done)), DoneOp(Seen{ID: "a", Status: Active}), Done, ""},
		{"reopen: a done plan goes active", plan("done", tasks(Done, Done)), ReopenOp(Seen{ID: "a", Status: Done}), Pending, Active},
		{"reopen: an active plan stays", plan("active", tasks(Done, Pending)), ReopenOp(Seen{ID: "a", Status: Done}), Pending, Active},
		{"reopen: no plan status, none added", plan("", tasks(Done, Done)), ReopenOp(Seen{ID: "a", Status: Done}), Pending, ""},
	} {
		got := edit(t, c.src, c.op)
		p := mustParse(t, got)
		if p.Task("a").Status != c.task {
			t.Errorf("%s: a is %s, want %s\n%s", c.name, p.Task("a").Status, c.task, got)
		}
		switch {
		case c.pl == "" && strings.Count(got, `"status"`) != 2:
			t.Errorf("%s: a status was added at the plan's root\n%s", c.name, got)
		case c.pl != "" && p.Status != c.pl:
			t.Errorf("%s: the plan is %s, want %s\n%s", c.name, p.Status, c.pl, got)
		}
	}
	// And Apply itself refuses to add one.
	if _, err := Apply([]byte(`{"tasks": []}`), []Change{SetStatus("", Active)}); err == nil {
		t.Fatal("a status added at the root")
	}
}

// docs/ux.md §7b: p moves the pin off every other active leaf and never
// writes a parent, even one marked active; p and d refuse a parent.
func TestEditNeverWritesAParent(t *testing.T) {
	src := `{
  "status": "active",
  "tasks": [
    {"id": "ui", "status": "active"},
    {"id": "a", "parent": "ui", "status": "active"},
    {"id": "b", "parent": "ui", "status": "pending"},
    {"id": "c", "status": "active"}
  ]
}
`
	g := mustParse(t, src).Graph()
	if moving := g.Moving(g.Task("b")); strings.Join(moving, ",") != "a,c" {
		t.Fatalf("the active leaves a pin moves off: %v", moving)
	}
	got := edit(t, src, PinOp(Seen{ID: "b", Status: Pending, Moving: []string{"a", "c"}}))
	want := strings.NewReplacer(
		`"id": "a", "parent": "ui", "status": "active"`, `"id": "a", "parent": "ui", "status": "pending"`,
		`"id": "b", "parent": "ui", "status": "pending"`, `"id": "b", "parent": "ui", "status": "active"`,
		`"id": "c", "status": "active"`, `"id": "c", "status": "pending"`,
	).Replace(src)
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	path := writePlan(t, src)
	for _, op := range []Op{PinOp(Seen{ID: "ui", Status: Active}), UnpinOp(Seen{ID: "ui", Status: Active}), DoneOp(Seen{ID: "ui", Status: Active}), ReopenOp(Seen{ID: "ui", Status: Active})} {
		if _, err := Edit(path, op); !errors.Is(err, ErrGroup) {
			t.Fatalf("a parent: %v", err)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != src {
		t.Fatal("a refused write wrote")
	}
}

// docs/ux.md §7b: the key carries the status the person saw; a task the
// file moved on since is refused, and so is a pin that would move off a
// task that became active meanwhile, a task that left, and an id used
// twice.
func TestEditRefusesWhatThePersonDidntSee(t *testing.T) {
	path := writePlan(t, editPlan)
	stale := func(op Op, id string) {
		t.Helper()
		var s *StaleError
		if _, err := Edit(path, op); !errors.As(err, &s) || s.ID != id {
			t.Fatalf("want %s stale, got %v", id, err)
		}
		if err := (&StaleError{ID: id}).Error(); err != id+" just changed in the file" {
			t.Fatal(err)
		}
	}
	stale(PinOp(Seen{ID: "b", Status: Done, Moving: []string{"a"}}), "b")    // b is pending in the file
	stale(PinOp(Seen{ID: "b", Status: Pending}), "a")                        // a is active in the file now
	stale(PinOp(Seen{ID: "b", Status: Pending, Moving: []string{"c"}}), "a") // and c isn't
	stale(DoneOp(Seen{ID: "gone", Status: Pending}), "gone")
	stale(UnpinOp(Seen{ID: "b", Status: Active}), "b")
	stale(ReopenOp(Seen{ID: "c", Status: Done}), "c")
	if after, _ := os.ReadFile(path); string(after) != editPlan {
		t.Fatal("a refused write wrote")
	}
	dup := writePlan(t, `{"tasks": [{"id": "a"}, {"id": " a "}]}`)
	for _, op := range []Op{PinOp(Seen{ID: "a", Status: Pending}), DoneOp(Seen{ID: "a", Status: Pending}), changes(SetStatus("a", Done))} {
		if _, err := Edit(dup, op); !errors.Is(err, ErrTwice) {
			t.Fatalf("ids that differ only by spaces are one id, twice: %v", err)
		}
	}
	loop := writePlan(t, `{"tasks": [{"id": "x", "depends_on": ["y"]}, {"id": "y", "depends_on": ["x"]}]}`)
	if _, err := Edit(loop, PinOp(Seen{ID: "x", Status: Pending})); !errors.Is(err, ErrLoop) {
		t.Fatalf("a task in a loop is never the pin: %v", err)
	}
}

// docs/ux.md §7b: another writer saves between Rex's read and its rename:
// Rex starts over on the new bytes, keeping their change; one that keeps
// saving wins, and after three tries Rex writes nothing.
func TestEditRetriesThenGivesUp(t *testing.T) {
	path := writePlan(t, editPlan)
	agent := strings.Replace(editPlan, `"title": "Inline"`, `"title": "Inline (renamed by agent)"`, 1)
	calls := 0
	beforeRename = func(p string) {
		if calls++; calls == 1 {
			os.WriteFile(p, []byte(agent), 0o640)
		}
	}
	defer func() { beforeRename = nil }()
	if _, err := Edit(path, DoneOp(Seen{ID: "c", Status: Pending})); err != nil || calls != 2 {
		t.Fatalf("%v after %d tries", err, calls)
	}
	want := strings.Replace(agent, `"id": "c",
      "status": "pending"`, `"id": "c",
      "status": "done"`, 1)
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Fatalf("both edits land\n%s", got)
	}

	// Each try reads what the last one's writer left: the op checks again.
	calls = 0
	beforeRename = func(p string) {
		calls++
		os.WriteFile(p, []byte(strings.Replace(editPlan, `"owner": "lucian"`, `"owner": "try `+string(rune('0'+calls))+`"`, 1)), 0o640)
	}
	path = writePlan(t, editPlan)
	if _, err := Edit(path, DoneOp(Seen{ID: "c", Status: Pending})); !errors.Is(err, ErrBusy) || calls != editTries {
		t.Fatalf("%v after %d tries", err, calls)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), `"owner": "try 3"`) || !strings.Contains(string(got), `"id": "c",
      "status": "pending"`) {
		t.Fatalf("Rex wrote nothing over the last save\n%s", got)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}

	// The other writer moves the task Rex is writing: the next try refuses.
	beforeRename = func(p string) {
		os.WriteFile(p, []byte(strings.Replace(editPlan, `"id": "c",
      "status": "pending"`, `"id": "c",
      "status": "active"`, 1)), 0o640)
	}
	path = writePlan(t, editPlan)
	var s *StaleError
	if _, err := Edit(path, DoneOp(Seen{ID: "c", Status: Pending})); !errors.As(err, &s) || s.ID != "c" {
		t.Fatalf("the agent took c meanwhile: %v", err)
	}
}

// docs/ux.md §7b: the same key undoes. p p and d d leave the file byte for
// byte as it was.
func TestEditSameKeyUndoes(t *testing.T) {
	plan := func(planStatus, a, b string) string {
		return "{\n  \"version\": 1,\n  \"status\": \"" + planStatus + "\",\n  \"tasks\": [\n    {\"id\": \"a\", \"status\": \"" + a + "\", \"x-n\": 1},\n    {\n      \"id\": \"b\",\n      \"status\": \"" + b + "\"\n    }\n  ]\n}\n"
	}
	for _, c := range []struct {
		name, src   string
		first, then Op
	}{
		{"p p on a pending task", plan("active", "pending", "done"), PinOp(Seen{ID: "a", Status: Pending}), UnpinOp(Seen{ID: "a", Status: Active})},
		{"p p on the pin", plan("active", "active", "pending"), UnpinOp(Seen{ID: "a", Status: Active}), PinOp(Seen{ID: "a", Status: Pending})},
		{"d d on a pending task", plan("active", "pending", "pending"), DoneOp(Seen{ID: "a", Status: Pending}), ReopenOp(Seen{ID: "a", Status: Done})},
		{"d d on the last task of a done plan", plan("done", "done", "done"), ReopenOp(Seen{ID: "b", Status: Done}), DoneOp(Seen{ID: "b", Status: Pending})},
		{"d d finishing the plan", plan("active", "skipped", "pending"), DoneOp(Seen{ID: "b", Status: Pending}), ReopenOp(Seen{ID: "b", Status: Done})},
	} {
		path := writePlan(t, c.src)
		if _, err := Edit(path, c.first); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got, _ := os.ReadFile(path); string(got) == c.src {
			t.Fatalf("%s: the first key wrote nothing", c.name)
		}
		if _, err := Edit(path, c.then); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got, _ := os.ReadFile(path); string(got) != c.src {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.src)
		}
	}
}

func TestEditRefusals(t *testing.T) {
	cases := []struct {
		name, src string
		want      error
	}{
		{"newer version", `{"version": 2, "tasks": [{"id": "a"}]}`, ErrReadOnly},
		{"conflict", "{\n<<<<<<< HEAD\n\"tasks\": []\n=======\n>>>>>>> x\n}", ErrUnreadable},
		{"invalid", `{"tasks": [`, ErrUnreadable},
		{"tasks not list", `{"tasks": {}}`, ErrReadOnly},
		{"utf16", "\xff\xfe{\x00}\x00", ErrUnreadable},
	}
	for _, c := range cases {
		path := writePlan(t, c.src)
		_, err := Edit(path, changes(SetStatus("a", Done)))
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
		if got, _ := os.ReadFile(path); string(got) != c.src {
			t.Errorf("%s: file touched", c.name)
		}
	}
	if _, err := Edit(filepath.Join(t.TempDir(), "gone.rex.json"), changes()); !errors.Is(err, ErrUnreadable) || !strings.Contains(err.Error(), "gone") {
		t.Fatal(err)
	}
	path := writePlan(t, editPlan)
	if _, err := Edit(path, changes(SetStatus("a", "wip"))); err == nil {
		t.Error("a status that isn't one of the five was written")
	}
	if runtime.GOOS == "windows" {
		return
	}
	os.Chmod(path, 0o444)
	defer os.Chmod(path, 0o644)
	if _, err := Edit(path, changes(SetStatus("b", Active))); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("a read-only file: %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != editPlan {
		t.Fatal("a read-only file was rewritten")
	}
}

// docs/ux.md §7b: writes are bounded by MaxFile. A plan just under it
// that a new status member would take over it is left alone.
func TestEditStaysUnderMaxFile(t *testing.T) {
	head := `{"tasks": [{"id": "a", "x": "`
	tail := `"}]}`
	src := head + strings.Repeat("x", MaxFile-len(head)-len(tail)) + tail
	path := writePlan(t, src)
	if _, err := Edit(path, PinOp(Seen{ID: "a", Status: Pending})); err == nil || !strings.Contains(err.Error(), "MB") {
		t.Fatalf("over MaxFile: %v", err)
	}
	if info, _ := os.Stat(path); info.Size() != MaxFile {
		t.Fatal("the file changed")
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

func TestEditMatchesIdsLikeParse(t *testing.T) {
	path := writePlan(t, `{"tasks":[{"id":" a ","status":"pending"}]}`)
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Edit(path, PinOp(Seen{ID: p.Tasks[0].ID, Status: Pending})); err != nil {
		t.Fatalf("a padded id is the task Parse shows: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != `{"tasks":[{"id":" a ","status":"active"}]}` {
		t.Fatalf("%s", got)
	}
}

func FuzzEdit(f *testing.F) {
	f.Add(editPlan, "a", "done")
	f.Add(strings.ReplaceAll(editPlan, "\n", "\r\n"), "b", "active")
	f.Add(`{"tasks":[{"id":"a","z":{}}]}`, "a", "pending")
	f.Add("{\"tasks\":[\n\t{\n\t\t\"id\": \"a\"\n\t}\n]}", "a", "skipped")
	f.Fuzz(func(t *testing.T, src, task, status string) {
		p, err := Parse([]byte(src))
		if err != nil || p.ReadOnly != "" || !statuses[Status(status)] || task == "" {
			return
		}
		out, err := Apply([]byte(src), []Change{SetStatus(task, Status(status))})
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
		if got := np.Task(strings.TrimSpace(task)); got != nil && got.Status != Status(status) && len(np.Problems) == len(p.Problems) {
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

// A plan with hard links stays one file: Rex writes it in place (after
// the same re-check), so every name sees the new statuses. A rename would
// leave the other names on the old file.
func TestEditKeepsHardLinks(t *testing.T) {
	path := writePlan(t, editPlan)
	other := filepath.Join(filepath.Dir(path), "other.rex.json")
	if err := os.Link(path, other); err != nil {
		t.Skip("no hard links here:", err)
	}
	if info, _ := os.Stat(path); links(info) < 2 {
		t.Skip("this system doesn't report link counts")
	}
	if _, err := Edit(path, DoneOp(Seen{ID: "c", Status: Pending})); err != nil {
		t.Fatal(err)
	}
	a, _ := os.Stat(path)
	b, _ := os.Stat(other)
	got, _ := os.ReadFile(other)
	if !os.SameFile(a, b) || !strings.Contains(string(got), `"id": "c",
      "status": "done"`) {
		t.Fatalf("the link was split off:\n%s", got)
	}
	if runtime.GOOS != "windows" && a.Mode().Perm() != 0o640 {
		t.Fatalf("mode %o", a.Mode().Perm())
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 2 {
		t.Fatalf("leftovers: %v", entries)
	}
	// The re-check holds in place too: a save in between starts it over.
	calls := 0
	beforeRename = func(p string) {
		if calls++; calls == 1 {
			os.WriteFile(p, []byte(strings.Replace(string(got), `"owner": "lucian"`, `"owner": "agent"`, 1)), 0o640)
		}
	}
	defer func() { beforeRename = nil }()
	if _, err := Edit(path, ReopenOp(Seen{ID: "c", Status: Done})); err != nil || calls != 2 {
		t.Fatalf("%v after %d tries", err, calls)
	}
	if got, _ := os.ReadFile(other); !strings.Contains(string(got), `"owner": "agent"`) || !strings.Contains(string(got), `"id": "c",
      "status": "pending"`) {
		t.Fatalf("both edits, through both names:\n%s", got)
	}
}

// The duplicate-id refusal names the id that is used twice, which may be
// another task's than the one pinned: a pin moves off every active leaf.
func TestEditNamesTheDuplicatedId(t *testing.T) {
	path := writePlan(t, `{"tasks": [{"id": "a", "status": "active"}, {"id": "a"}, {"id": "b"}]}`)
	var twice *TwiceError
	if _, err := Edit(path, PinOp(Seen{ID: "b", Status: Pending, Moving: []string{"a"}})); !errors.As(err, &twice) || twice.ID != "a" || !errors.Is(err, ErrTwice) {
		t.Fatalf("%v", err)
	}
	if _, err := Edit(path, DoneOp(Seen{ID: "a", Status: Active})); !errors.As(err, &twice) || twice.ID != "a" {
		t.Fatalf("%v", err)
	}
}

// docs/ux.md §11: a link in rex/plans counts only where it leads to a plan
// file under rex/plans; one that leads out, or to a file that isn't a
// plan, leads nowhere.
func TestFilesSkipsLinksOut(t *testing.T) {
	root := filepath.Join(t.TempDir(), "rex", "plans")
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(root, "team"), 0o755)
	os.WriteFile(filepath.Join(root, "real.rex.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(outside, "evil.rex.json"), []byte("{}"), 0o644)
	os.MkdirAll(filepath.Join(outside, "dir"), 0o755)
	os.WriteFile(filepath.Join(outside, "dir", "deep.rex.json"), []byte("{}"), 0o644)
	for link, to := range map[string]string{
		"evil.rex.json":     filepath.Join(outside, "evil.rex.json"),
		"team/up.rex.json":  filepath.Join("..", "..", "..", "x.rex.json"),
		"alias.rex.json":    filepath.Join(root, "real.rex.json"),
		"team/rel.rex.json": filepath.Join("..", "real.rex.json"),
		"notes.rex.json":    filepath.Join(root, "notes.txt"),
		"outdir":            filepath.Join(outside, "dir"),
	} {
		if err := os.Symlink(to, filepath.Join(root, filepath.FromSlash(link))); err != nil {
			t.Skip("no symlinks here:", err)
		}
	}
	os.WriteFile(filepath.Join(filepath.Dir(filepath.Dir(root)), "x.rex.json"), []byte("{}"), 0o644)
	var got []string
	for _, f := range Files(root) {
		rel, _ := filepath.Rel(root, f.Path)
		got = append(got, filepath.ToSlash(rel))
	}
	if strings.Join(got, " ") != "alias.rex.json real.rex.json team/rel.rex.json" {
		t.Fatalf("%v", got)
	}
	if _, ok := Within(root, filepath.Join(root, "evil.rex.json")); ok {
		t.Fatal("a link out is not within")
	}
	if rel, ok := Within(root, filepath.Join(root, "alias.rex.json")); !ok || rel != "real.rex.json" {
		t.Fatalf("a link in is where it leads: %q", rel)
	}
}
