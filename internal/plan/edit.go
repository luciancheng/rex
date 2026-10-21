package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

// Change sets one field of one task, or of the plan when Task is "".
// Rex only ever writes status and the completion fields.
type Change struct {
	Task  string
	Field string
	Value any // string or []string
}

func SetStatus(task string, s Status) Change {
	return Change{Task: task, Field: "status", Value: string(s)}
}

func SetList(task, field string, items []string) Change {
	if items == nil {
		items = []string{}
	}
	return Change{Task: task, Field: field, Value: items}
}

func SetString(task, field, value string) Change {
	return Change{Task: task, Field: field, Value: value}
}

var writable = map[string]bool{
	"status": true, "accomplished": true, "modified": true, "verified_how": true, "completed_notes": true,
}

// Op decides the changes from the plan as it is on disk right now. Edit
// may call it more than once if the file changes underneath, so an Op
// re-checks what the user saw (the task still exists, its status is
// unchanged) and returns an error to abort.
type Op func(p *Plan) ([]Change, error)

// Saved is the plan file as Rex just wrote it.
type Saved struct {
	Data []byte
	Plan *Plan
}

var (
	ErrReadOnly = errors.New("read-only")
	ErrBusy     = errors.New("the plan file keeps changing; try again")
)

// Hash identifies a version of a plan file's bytes.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Edit applies op to the plan at path. Only the members op changes are
// rewritten, inside their own object; every other byte (order, spacing,
// line endings, a BOM, unknown fields) stays. If someone else writes the
// file between Rex's read and its rename, Edit starts over on the new
// content. It never writes a file it could not fully read.
func Edit(path string, op Op) (*Saved, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	for attempt := 0; attempt < 3; attempt++ {
		data, err := ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("%s is gone", filepath.Base(path))
			}
			return nil, err
		}
		p, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrReadOnly, err)
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
		if bytes.Equal(out, data) {
			return &Saved{Data: data, Plan: p}, nil
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
		return &Saved{Data: out, Plan: np}, nil
	}
	return nil, ErrBusy
}

// Apply returns data with changes made, checked against the tree it should
// produce. It does not touch the disk.
func Apply(data []byte, changes []Change) ([]byte, error) {
	want, err := tree(data)
	if err != nil {
		return nil, err
	}
	out := data
	for _, c := range changes {
		if !writable[c.Field] || (c.Task == "" && c.Field != "status") {
			return nil, fmt.Errorf("rex does not write %q", c.Field)
		}
		if err := checkValue(c); err != nil {
			return nil, err
		}
		doc, err := jsonedit.Parse(out)
		if err != nil {
			return nil, err
		}
		obj, index, err := target(doc, c.Task)
		if err != nil {
			return nil, err
		}
		if out, err = doc.Set(obj, c.Field, c.Value); err != nil {
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

// checkValue refuses a value Rex would never write: a status that isn't
// one of the five, a list field that isn't a list of strings, notes that
// aren't text.
func checkValue(c Change) error {
	switch c.Field {
	case "status":
		if s, ok := c.Value.(string); !ok || !statuses[Status(s)] {
			return fmt.Errorf("%v is not a status", c.Value)
		}
	case "completed_notes":
		if _, ok := c.Value.(string); !ok {
			return fmt.Errorf("%s must be text", c.Field)
		}
	default:
		if _, ok := c.Value.([]string); !ok {
			return fmt.Errorf("%s must be a list of text", c.Field)
		}
	}
	return nil
}

// target finds the object a change applies to: the root, or the one task
// with that id. index is the task's position in tasks, or -1 for the root.
func target(doc *jsonedit.Doc, task string) (*jsonedit.Node, int, error) {
	root := doc.Root()
	if task == "" {
		return root, -1, nil
	}
	tasks := root.Member("tasks")
	if tasks == nil || tasks.Kind != '[' {
		return nil, 0, fmt.Errorf("task %s is gone", task)
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
			return nil, 0, fmt.Errorf("two tasks are %s; fix the file first", task)
		}
		found, index = n, i
	}
	if found == nil {
		return nil, 0, fmt.Errorf("task %s is gone", task)
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
	raw, err := json.Marshal(c.Value)
	if err != nil {
		return err
	}
	v, err := tree(raw)
	if err != nil {
		return err
	}
	obj[c.Field] = v
	return nil
}

var errChanged = errors.New("changed on disk")

// replace swaps out for the file at path, which must still hold before.
// The new file keeps the old one's mode. A leftover temp file from a
// crashed write is cleared first.
func replace(path string, before, out []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o200 == 0 {
		return fmt.Errorf("%w: %s is read-only on disk", ErrReadOnly, filepath.Base(path))
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
		if now, rerr := os.ReadFile(path); rerr != nil || !bytes.Equal(now, before) {
			return errChanged
		}
		if werr := os.WriteFile(path, out, info.Mode().Perm()); werr != nil {
			return fmt.Errorf("%v (and writing in place failed: %v)", err, werr)
		}
	}
	return nil
}

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
