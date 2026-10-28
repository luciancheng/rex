package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/jsonedit"
)

// Change sets one status: the task's with id Task, or the plan's own when
// Task is "". It is all Rex ever writes into a plan (docs/ux.md §7b): the
// same status values the rex-plan skill has the agent write, never a
// completion field, a branch, a title or a task.
type Change struct {
	Task   string
	Status Status
}

func SetStatus(task string, s Status) Change { return Change{Task: task, Status: s} }

// Op decides the changes from the plan as it is on disk right now. Edit
// may call it more than once if the file changes underneath, so an Op
// re-checks what the person saw (the task is still there, once, a leaf,
// with the status they saw) and returns an error to write nothing.
type Op func(p *Plan) ([]Change, error)

// Saved is the plan file as Rex just wrote it, and what it was before.
type Saved struct {
	Data []byte
	Plan *Plan
	// Changes are the status values Rex wrote, each member once.
	Changes []Change
	// From is the hash of the bytes Rex read and edited, and Base their
	// plan: when From isn't the plan on screen, the file changed between
	// the frame and the key, and the write carries that change too.
	From string
	Base *Plan
}

var (
	// ErrReadOnly: the plan is one a v1 tool shouldn't write (a newer
	// version, conflict markers, tasks that aren't a list), or the file is
	// read-only on disk.
	ErrReadOnly = errors.New("read-only")
	// ErrUnreadable: the file is gone, can't be read, or doesn't parse.
	ErrUnreadable = errors.New("can't read the plan")
	// ErrBusy: the file kept changing under three tries.
	ErrBusy = errors.New("the plan keeps changing")
	// ErrTwice: more than one task has the id (TwiceError names it).
	ErrTwice = errors.New("two tasks have this id")
	// ErrGroup: the task has tasks under it; only leaves take a status
	// from Rex.
	ErrGroup = errors.New("has tasks under it")
	// ErrLoop: the task is in a depends_on or parent loop, so a pin on it
	// would never show.
	ErrLoop = errors.New("is in a loop")
)

// StaleError means task ID is not what the person saw when they pressed
// the key: it left the file, or the file gives it another status now (or,
// for a pin, another task became active).
type StaleError struct{ ID string }

func (e *StaleError) Error() string { return e.ID + " just changed in the file" }

// TwiceError is ErrTwice for task ID: the id a write would touch, which
// may be another task's than the one the key was pressed on (a pin moves
// off every other active leaf).
type TwiceError struct{ ID string }

func (e *TwiceError) Error() string { return e.ID + ": " + ErrTwice.Error() }
func (e *TwiceError) Unwrap() error { return ErrTwice }

// editTries is how many times Edit starts over on a file that changed
// between its read and its rename.
const editTries = 3

// beforeRename, when set (by a test), runs just before Edit's last
// re-read of the file, where another writer's save would land.
var beforeRename func(path string)

// Edit applies op to the plan at path (docs/ux.md §7b). Only the status
// values op changes are rewritten, inside their own object; every other
// byte (order, spacing, line endings, a BOM, unknown fields) stays. If
// someone else writes the file between Rex's read and its rename, Edit
// starts over on the new content, and after editTries gives up with
// ErrBusy. It never writes a file it could not fully read, a read-only
// plan, or one that would come out over MaxFile.
func Edit(path string, op Op) (*Saved, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	for attempt := 0; attempt < editTries; attempt++ {
		data, err := ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("%w: the file is gone", ErrUnreadable)
			}
			return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
		}
		p, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
		}
		if p.ReadOnly != "" {
			return nil, fmt.Errorf("%w: %s", ErrReadOnly, p.ReadOnly)
		}
		changes, err := op(p)
		if err != nil {
			return nil, err
		}
		out, err := Apply(data, changes)
		if err != nil {
			return nil, err
		}
		from := Hash(data)
		if bytes.Equal(out, data) {
			return &Saved{Data: data, Plan: p, Changes: changes, From: from, Base: p}, nil
		}
		if len(out) > MaxFile {
			return nil, fmt.Errorf("the plan would be over %d MB; nothing written", MaxFile>>20)
		}
		np, err := Parse(out)
		if err != nil {
			return nil, fmt.Errorf("rex made an invalid plan (%v); nothing written", err)
		}
		switch err := replace(path, data, out); {
		case errors.Is(err, errChanged):
			continue
		case err != nil:
			return nil, err
		}
		return &Saved{Data: out, Plan: np, Changes: changes, From: from, Base: p}, nil
	}
	return nil, ErrBusy
}

// Apply returns data with changes made, checked against the tree it should
// produce. It does not touch the disk. A task with no status member gets
// one, after its last member in the file's own layout; the plan's own
// status is only ever replaced, never added (Rex inserts nothing at a
// plan's root).
func Apply(data []byte, changes []Change) ([]byte, error) {
	want, err := tree(data)
	if err != nil {
		return nil, err
	}
	out := data
	for _, c := range changes {
		if !statuses[c.Status] {
			return nil, fmt.Errorf("%q is not a status", c.Status)
		}
		doc, err := jsonedit.Parse(out)
		if err != nil {
			return nil, err
		}
		obj, index, err := target(doc, c.Task)
		if err != nil {
			return nil, err
		}
		if c.Task == "" && obj.Member("status") == nil {
			return nil, errors.New("rex never adds a member to the plan's root; nothing written")
		}
		if out, err = doc.Set(obj, "status", string(c.Status)); err != nil {
			return nil, err
		}
		if err := setTree(want, index, c); err != nil {
			return nil, err
		}
	}
	got, err := tree(out)
	if err != nil || !reflect.DeepEqual(got, want) {
		return nil, errors.New("rex's edit did not come out as planned; nothing written")
	}
	return out, nil
}

// target finds the object a change applies to: the root, or the one task
// with that id. index is the task's position in tasks, or -1 for the root.
func target(doc *jsonedit.Doc, task string) (*jsonedit.Node, int, error) {
	root := doc.Root()
	if root.Kind != '{' {
		return nil, 0, errors.New("not a plan")
	}
	if task == "" {
		return root, -1, nil
	}
	tasks := root.Member("tasks")
	if tasks == nil || tasks.Kind != '[' {
		return nil, 0, &StaleError{ID: task}
	}
	var found *jsonedit.Node
	index := -1
	for i, n := range tasks.Elems {
		if n.Kind != '{' {
			continue
		}
		id := n.Member("id")
		if id == nil || id.Kind != '"' {
			continue
		}
		var s string
		// The same id rule as Parse: surrounding spaces don't count.
		if doc.Decode(id, &s) != nil || strings.TrimSpace(s) != strings.TrimSpace(task) {
			continue
		}
		if found != nil {
			return nil, 0, &TwiceError{ID: strings.TrimSpace(task)}
		}
		found, index = n, i
	}
	if found == nil {
		return nil, 0, &StaleError{ID: task}
	}
	return found, index, nil
}

func tree(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func setTree(root any, index int, c Change) error {
	obj, _ := root.(map[string]any)
	if index >= 0 {
		tasks, _ := obj["tasks"].([]any)
		if index >= len(tasks) {
			return errors.New("task moved")
		}
		obj, _ = tasks[index].(map[string]any)
	}
	if obj == nil {
		return errors.New("not an object")
	}
	obj["status"] = string(c.Status)
	return nil
}

var errChanged = errors.New("changed on disk")

// replace swaps out for the file at path, which must still hold before.
// The new file keeps the old one's mode. A leftover temp file from a
// crashed write is cleared first. A file with hard links is written in
// place instead, after the same re-check: a rename would give the plan a
// new file and leave its other names on the old one.
func replace(path string, before, out []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o200 == 0 {
		return fmt.Errorf("%w: the file is read-only on disk", ErrReadOnly)
	}
	if links(info) > 1 {
		if beforeRename != nil {
			beforeRename(path)
		}
		return inPlace(path, before, out)
	}
	dir, base := filepath.Dir(path), filepath.Base(path)
	clearStale(dir, base)
	tmp, err := os.CreateTemp(dir, "."+base+".rex-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, info.Mode().Perm()); err != nil {
		return err
	}
	if beforeRename != nil {
		beforeRename(path)
	}
	if now, err := os.ReadFile(path); err != nil || !bytes.Equal(now, before) {
		return errChanged
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if runtime.GOOS != "windows" {
			return err
		}
		// Windows refuses to replace a file another process has open
		// (an editor, an indexer). Write in place instead, after checking
		// once more that nobody wrote it meanwhile.
		if werr := inPlace(path, before, out); werr != nil {
			if errors.Is(werr, errChanged) {
				return werr
			}
			return fmt.Errorf("%v (and writing in place failed: %v)", err, werr)
		}
	}
	return nil
}

// inPlace writes out over the file at path, keeping the file itself (its
// links, its mode), once a last read shows it still holds before.
func inPlace(path string, before, out []byte) error {
	if now, err := os.ReadFile(path); err != nil || !bytes.Equal(now, before) {
		return errChanged
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := f.Write(out); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// clearStale removes temp files a crashed write left beside the plan.
func clearStale(dir, base string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "."+base+".rex-") {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > time.Minute {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
