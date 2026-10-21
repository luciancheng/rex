// Package plan reads v1 .rex.json plan files (docs/plan-format.md).
//
// Plans are written by agents and humans, so Parse keeps whatever it can
// read and reports the rest as Problems instead of failing: a half-filled
// plan still shows. Only a file that is not JSON at all, or not an object,
// is an error.
package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/luciancheng/rex/internal/jsonedit"
)

// Version is the plan format this Rex reads and writes.
const Version = 1

type Status string

const (
	Pending Status = "pending"
	Active  Status = "active"
	Done    Status = "done"
	Blocked Status = "blocked"
	Skipped Status = "skipped"
)

var statuses = map[Status]bool{Pending: true, Active: true, Done: true, Blocked: true, Skipped: true}

// Scope is the hard edge of a plan or task: what is the job and what is not.
type Scope struct {
	In  []string `json:"in"`
	Out []string `json:"out"`
}

// Plan is one unit of work: a feature, a fix, a migration.
type Plan struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Goal    string `json:"goal"`
	Status  Status `json:"status"`
	// Base is the branch a stacked plan's bottom PR targets (optional).
	Base  string `json:"base,omitempty"`
	Scope Scope  `json:"scope"`
	Tasks []Task `json:"tasks"`

	// Problems are what Parse could not read or does not trust.
	Problems []Problem `json:"-"`
	// ReadOnly says why Rex must not write this file, or is "".
	ReadOnly string `json:"-"`
}

// Task is one PR-sized slice of the plan. Parent is outline (the task sits
// under that heading); DependsOn is order (do not start until those are
// done). The last four fields are the retrospective written on completion.
type Task struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Parent      string   `json:"parent"`
	Status      Status   `json:"status"`
	Scope       Scope    `json:"scope"`
	Needs       []string `json:"needs"`
	Does        []string `json:"does"`
	Description string   `json:"description"`
	DependsOn   []string `json:"depends_on"`
	Verify      []string `json:"verify"`
	Files       []string `json:"files"`
	// Branch is the head branch of this leaf task's PR (optional).
	Branch string `json:"branch,omitempty"`

	Accomplished   []string `json:"accomplished"`
	Modified       []string `json:"modified"`
	VerifiedHow    []string `json:"verified_how"`
	CompletedNotes string   `json:"completed_notes"`

	// Index is the task's position in the file's tasks array.
	Index int `json:"-"`
}

// Problem is one thing in the file Rex could not read or does not trust.
// Where is "plan", a task id, or "tasks[i]" for a task with no usable id.
type Problem struct {
	Where string
	Msg   string
}

func (p Problem) String() string { return p.Where + ": " + p.Msg }

// MaxFile is the largest plan file Rex reads.
const MaxFile = 4 << 20

// ReadFile reads a plan file: a regular file (a symlink to one is fine) of
// at most MaxFile bytes. A FIFO, a device or a folder named *.rex.json is
// refused before it is opened, so it can never hang or flood Rex.
func ReadFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	if info.Size() > MaxFile {
		return nil, fmt.Errorf("%s is over %d MB; plans are small", filepath.Base(path), MaxFile>>20)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFile+1))
	if err == nil && len(data) > MaxFile {
		err = fmt.Errorf("%s is over %d MB; plans are small", filepath.Base(path), MaxFile>>20)
	}
	return data, err
}

// Load reads and parses the plan at path.
func Load(path string) (*Plan, error) {
	data, err := ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// ErrConflict means the file still has git merge conflict markers.
var ErrConflict = errors.New("merge conflict markers")

// Parse reads a plan. An error means there is no plan to show: the file is
// not JSON, is not an object, or is mid-merge.
func Parse(data []byte) (*Plan, error) {
	if line := conflictLine(data); line > 0 {
		return nil, fmt.Errorf("line %d: %w · resolve them in git", line, ErrConflict)
	}
	doc, err := jsonedit.Parse(data)
	if err != nil {
		return nil, describe(data, err)
	}
	root := doc.Root()
	if root.Kind != '{' {
		return nil, errors.New("not a plan: the top level must be an object")
	}
	r := &reader{doc: doc, p: &Plan{Status: Pending}}
	r.plan(root)
	r.check()
	return r.p, nil
}

func conflictLine(data []byte) int {
	for i, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if bytes.HasPrefix(line, []byte("<<<<<<< ")) || bytes.HasPrefix(line, []byte(">>>>>>> ")) || bytes.Equal(line, []byte("=======")) {
			return i + 1
		}
	}
	return 0
}

// describe turns a JSON error into "line:col: message". The position comes
// from encoding/json's own pass over the whole file, whose offset is the
// same on every Go release (one byte past the bad one); the decoder's
// token offsets are not. A duplicate key, which encoding/json accepts, is
// found by its own walk.
func describe(data []byte, err error) error {
	if errors.Is(err, jsonedit.ErrUTF16) {
		return err
	}
	body := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if len(bytes.TrimSpace(body)) == 0 {
		return errors.New("not valid JSON: the file is empty")
	}
	if errors.Is(err, jsonedit.ErrDuplicateKey) {
		if at := duplicateKey(body); at >= 0 {
			line, col := position(body, at)
			return fmt.Errorf("%d:%d: %v", line, col, err)
		}
		return err
	}
	var raw json.RawMessage
	var syn *json.SyntaxError
	if uerr := json.Unmarshal(body, &raw); errors.As(uerr, &syn) {
		msg, at := syn.Error(), int(syn.Offset)-1
		if int(syn.Offset) >= len(body) && strings.Contains(msg, "end of JSON input") {
			msg, at = "the file ends before the JSON does", len(body)
		}
		line, col := position(body, at)
		return fmt.Errorf("%d:%d: %s", line, col, msg)
	}
	return fmt.Errorf("not valid JSON: %w", err)
}

// duplicateKey is the byte offset of the first key that repeats one
// already in its object, or -1.
func duplicateKey(body []byte) int {
	type frame struct {
		obj, wantKey bool
		keys         map[string]bool
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	var stack []*frame
	for {
		before := int(dec.InputOffset()) // the key, if this is one, starts at the next '"'
		tok, err := dec.Token()
		if err != nil {
			return -1
		}
		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		switch t := tok.(type) {
		case json.Delim:
			if t == '}' || t == ']' {
				stack = stack[:len(stack)-1]
				continue
			}
			if top != nil && top.obj {
				top.wantKey = true // after this value, a key
			}
			stack = append(stack, &frame{obj: t == '{', wantKey: true, keys: map[string]bool{}})
		default:
			if top == nil || !top.obj {
				continue
			}
			if !top.wantKey {
				top.wantKey = true
				continue
			}
			key, _ := t.(string)
			if top.keys[key] {
				if i := bytes.IndexByte(body[before:], '"'); i >= 0 {
					return before + i
				}
				return before
			}
			top.keys[key] = true
			top.wantKey = false
		}
	}
}

// position is the 1-based line and column (in characters) of offset.
func position(data []byte, offset int) (line, col int) {
	offset = max(0, min(offset, len(data)))
	start := bytes.LastIndexByte(data[:offset], '\n') + 1
	return 1 + bytes.Count(data[:offset], []byte("\n")), 1 + utf8.RuneCount(data[start:offset])
}

type reader struct {
	doc *jsonedit.Doc
	p   *Plan
}

func (r *reader) problem(where, format string, args ...any) {
	r.p.Problems = append(r.p.Problems, Problem{Where: where, Msg: fmt.Sprintf(format, args...)})
}

func (r *reader) null(n *jsonedit.Node) bool {
	return n.Kind == 0 && string(r.doc.Raw(n)) == "null"
}

func (r *reader) readOnly(why string) {
	if r.p.ReadOnly == "" {
		r.p.ReadOnly = why
	}
}

func (r *reader) plan(root *jsonedit.Node) {
	p := r.p
	p.Version = Version // a missing version reads as 1
	if n := root.Member("version"); n != nil && !r.null(n) {
		raw := string(r.doc.Raw(n))
		v, err := strconv.Atoi(raw)
		if n.Kind == '"' {
			var quoted string
			r.doc.Decode(n, &quoted)
			if v, err = strconv.Atoi(strings.TrimSpace(quoted)); err == nil {
				r.problem("plan", "version should be a number, not text (read %q as %d)", quoted, v)
			}
		}
		switch {
		case err != nil || v < 1:
			// Rex can't tell which format this is, so it won't write it.
			r.problem("plan", "version is not a whole number from 1")
			r.readOnly("version unreadable · fix it to edit")
		case v > Version:
			r.problem("plan", "version %d is newer than this Rex reads (%d)", v, Version)
			r.readOnly(fmt.Sprintf("version %d · update rex to edit", v))
			p.Version = v
		default:
			p.Version = v
		}
	}
	p.ID = r.text("plan", root, "id")
	p.Title = r.text("plan", root, "title")
	p.Goal = r.text("plan", root, "goal")
	p.Status = r.status("plan", root)
	p.Base = strings.TrimSpace(r.text("plan", root, "base"))
	p.Scope = r.scope("plan", root)
	tasks := root.Member("tasks")
	if tasks == nil || r.null(tasks) {
		return
	}
	if tasks.Kind != '[' {
		r.problem("plan", "tasks is not a list")
		r.readOnly("tasks is not a list")
		return
	}
	for i, n := range tasks.Elems {
		where := fmt.Sprintf("tasks[%d]", i)
		if n.Kind != '{' {
			r.problem(where, "is not an object")
			continue
		}
		p.Tasks = append(p.Tasks, r.task(where, i, n))
	}
}

func (r *reader) task(where string, index int, n *jsonedit.Node) Task {
	t := Task{Index: index}
	if id := n.Member("id"); id == nil || r.null(id) {
		r.problem(where, "has no id")
	} else if id.Kind != '"' {
		r.problem(where, "id is not a string")
	} else {
		r.doc.Decode(id, &t.ID)
		t.ID = strings.TrimSpace(t.ID)
		if t.ID == "" {
			r.problem(where, "has an empty id")
		} else {
			where = t.ID
		}
	}
	t.Title = r.text(where, n, "title")
	t.Parent = strings.TrimSpace(r.text(where, n, "parent"))
	t.Status = r.status(where, n)
	t.Scope = r.scope(where, n)
	t.Needs = r.list(where, n, "needs")
	t.Does = r.list(where, n, "does")
	t.Description = r.text(where, n, "description")
	t.DependsOn = r.list(where, n, "depends_on")
	for i := range t.DependsOn {
		t.DependsOn[i] = strings.TrimSpace(t.DependsOn[i])
	}
	t.Verify = r.list(where, n, "verify")
	t.Files = r.list(where, n, "files")
	t.Branch = strings.TrimSpace(r.text(where, n, "branch"))
	t.Accomplished = r.list(where, n, "accomplished")
	t.Modified = r.list(where, n, "modified")
	t.VerifiedHow = r.list(where, n, "verified_how")
	t.CompletedNotes = r.text(where, n, "completed_notes")
	return t
}

// text reads a string field. A list of strings is joined one per line,
// since agents sometimes write prose fields as lists.
func (r *reader) text(where string, obj *jsonedit.Node, key string) string {
	n := obj.Member(key)
	if n == nil || r.null(n) {
		return ""
	}
	switch n.Kind {
	case '"':
		var s string
		r.doc.Decode(n, &s)
		return s
	case '[':
		return strings.Join(r.list(where, obj, key), "\n")
	}
	r.problem(where, "%s is not text", key)
	return ""
}

// list reads a list of strings. A lone string is a one-item list.
func (r *reader) list(where string, obj *jsonedit.Node, key string) []string {
	n := obj.Member(key)
	if n == nil || r.null(n) {
		return nil
	}
	switch n.Kind {
	case '"':
		var s string
		r.doc.Decode(n, &s)
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return []string{s}
	case '[':
		var out []string
		for i, e := range n.Elems {
			if e.Kind != '"' {
				if !r.null(e) {
					r.problem(where, "%s[%d] is not text", key, i)
				}
				continue
			}
			var s string
			r.doc.Decode(e, &s)
			if strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	r.problem(where, "%s is not a list", key)
	return nil
}

func (r *reader) status(where string, obj *jsonedit.Node) Status {
	n := obj.Member("status")
	if n == nil || r.null(n) {
		return Pending
	}
	if n.Kind != '"' {
		r.problem(where, "status is not text · read as pending")
		return Pending
	}
	var s string
	r.doc.Decode(n, &s)
	st := Status(strings.ToLower(strings.TrimSpace(s)))
	if st == "" {
		return Pending
	}
	if !statuses[st] {
		r.problem(where, "unknown status %q · read as pending", s)
		return Pending
	}
	return st
}

func (r *reader) scope(where string, obj *jsonedit.Node) Scope {
	n := obj.Member("scope")
	if n == nil || r.null(n) {
		return Scope{}
	}
	if n.Kind != '{' {
		r.problem(where, "scope is not {\"in\": [...], \"out\": [...]}")
		return Scope{}
	}
	return Scope{In: r.list(where, n, "in"), Out: r.list(where, n, "out")}
}

func (r *reader) check() {
	p := r.p
	ids := map[string]int{}
	for _, t := range p.Tasks {
		if t.ID != "" {
			ids[t.ID]++
		}
	}
	told := map[string]bool{}
	for _, t := range p.Tasks { // in file order, so problems read the same every time
		if n := ids[t.ID]; n > 1 && !told[t.ID] {
			told[t.ID] = true
			r.problem(t.ID, "id is used by %d tasks", n)
		}
	}
	parents := map[string]bool{}
	for _, t := range p.Tasks {
		if t.Parent != "" && t.Parent != t.ID {
			parents[t.Parent] = true
		}
	}
	var active []string
	for _, t := range p.Tasks {
		where := t.ID
		if where == "" {
			where = fmt.Sprintf("tasks[%d]", t.Index)
		}
		if t.Parent != "" && ids[t.Parent] == 0 {
			r.problem(where, "parent %q is not a task here", t.Parent)
		}
		for _, dep := range t.DependsOn {
			if ids[dep] == 0 {
				r.problem(where, "depends on %q, which is not a task here", dep)
			}
		}
		if t.Status == Active && !parents[t.ID] {
			active = append(active, where) // a parent marked active is only a label; it is never pinned
		}
	}
	if len(active) > 1 {
		r.problem("plan", "%d tasks are active (%s) · one pin at a time", len(active), strings.Join(active, ", "))
	}
	for _, cycle := range p.cycles(func(t Task) []string { return t.DependsOn }) {
		r.problem(cycle[0], "depends_on loops: %s", strings.Join(cycle, " → "))
	}
	for _, cycle := range p.cycles(func(t Task) []string {
		if t.Parent == "" {
			return nil
		}
		return []string{t.Parent}
	}) {
		r.problem(cycle[0], "parent loops: %s", strings.Join(cycle, " → "))
	}
	if p.Stacked() {
		r.stack()
	}
}

// maxLoops caps the loops reported per graph: a dense file could have a
// great many, and a few are enough to fix it.
const maxLoops = 10

// cycles finds loops in the graph given by next, as paths from a task back
// to itself, each set of tasks once, at most maxLoops of them. The walk
// starts from tasks in file order.
func (p *Plan) cycles(next func(Task) []string) [][]string {
	byID := map[string]*Task{}
	for i := range p.Tasks {
		if t := &p.Tasks[i]; t.ID != "" && byID[t.ID] == nil {
			byID[t.ID] = t
		}
	}
	const (
		unseen = iota
		onPath
		finished
	)
	state := map[string]int{}
	var path []string
	var out [][]string
	seen := map[string]bool{}
	var visit func(id string)
	visit = func(id string) {
		state[id] = onPath
		path = append(path, id)
		for _, n := range next(*byID[id]) {
			if byID[n] == nil {
				continue
			}
			switch state[n] {
			case unseen:
				visit(n)
			case onPath:
				for i, v := range path {
					if v != n || len(out) >= maxLoops {
						continue
					}
					loop := append(append([]string(nil), path[i:]...), n)
					if key := loopKey(loop); !seen[key] {
						seen[key] = true
						out = append(out, loop)
					}
					break
				}
			}
		}
		path = path[:len(path)-1]
		state[id] = finished
	}
	for i := range p.Tasks {
		if t := &p.Tasks[i]; t.ID != "" && byID[t.ID] == t && state[t.ID] == unseen {
			visit(t.ID)
		}
	}
	return out
}

// loopKey names a loop by its tasks, wherever it was entered.
func loopKey(loop []string) string {
	ids := append([]string(nil), loop[:len(loop)-1]...)
	sort.Strings(ids)
	return strings.Join(ids, "\x00")
}

// Task returns the first task with this id, or nil.
func (p *Plan) Task(id string) *Task {
	for i := range p.Tasks {
		if p.Tasks[i].ID == id {
			return &p.Tasks[i]
		}
	}
	return nil
}

// Stub is a new, empty v1 plan for stem, every field present (but the
// optional stack base) so an agent or a human can fill it in.
func Stub(stem string) []byte {
	p := Plan{
		Version: Version,
		ID:      Slug(stem),
		Status:  Pending,
		Scope:   Scope{In: []string{}, Out: []string{}},
		Tasks:   []Task{},
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	return append(b, '\n')
}

// Slug is stem as a plan or task id: lowercase words joined by "-".
func Slug(stem string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(stem) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	if b.Len() == 0 {
		return "plan"
	}
	return b.String()
}
