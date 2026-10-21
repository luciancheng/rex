package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const example = `{
  "version": 1,
  "id": "file-storage",
  "title": "Plan file storage and navigator",
  "goal": "People can create, open, and share plans under rex/plans.",
  "status": "active",
  "scope": {
    "in": ["JSON plan format", "folder browser"],
    "out": ["agent auto-loads the plan", "extra palettes"]
  },
  "tasks": [
    {
      "id": "plans-ui",
      "title": "Plans navigator",
      "status": "done",
      "does": ["Create, open, browse rex/plans in the TUI"],
      "accomplished": ["Home cards, browse, duplicate warn"],
      "modified": ["internal/tui/plans.go"]
    },
    {
      "id": "card-highlight",
      "title": "Selected plan card is one block",
      "parent": "plans-ui",
      "status": "active",
      "depends_on": ["plans-ui"],
      "needs": ["pick / pickbg colors exist"],
      "does": ["Bar, border, and fill cover the whole selected card"],
      "verify": ["Open latest: ▌ on all three card rows"],
      "files": ["internal/tui/plans.go", "internal/tui/live.go"]
    }
  ]
}`

func mustParse(t *testing.T, src string) *Plan {
	t.Helper()
	p, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return p
}

func problems(p *Plan) string {
	var out []string
	for _, pr := range p.Problems {
		out = append(out, pr.String())
	}
	return strings.Join(out, "\n")
}

func TestParseDesignExample(t *testing.T) {
	p := mustParse(t, example)
	if len(p.Problems) != 0 || p.ReadOnly != "" {
		t.Fatalf("problems:\n%s\nread-only %q", problems(p), p.ReadOnly)
	}
	if p.Version != 1 || p.ID != "file-storage" || p.Status != Active || len(p.Scope.Out) != 2 || len(p.Tasks) != 2 {
		t.Fatalf("%+v", p)
	}
	ch := p.Task("card-highlight")
	if ch == nil || ch.Parent != "plans-ui" || ch.DependsOn[0] != "plans-ui" || len(ch.Files) != 2 || ch.Index != 1 {
		t.Fatalf("%+v", ch)
	}
	if done := p.Task("plans-ui"); done.Status != Done || done.Modified[0] != "internal/tui/plans.go" {
		t.Fatalf("%+v", done)
	}
}

func TestEmptyObjectIsAPlan(t *testing.T) {
	for _, src := range []string{"{}", "{}\n", "\xef\xbb\xbf{}", `{"tasks": null, "scope": null}`} {
		p := mustParse(t, src)
		if len(p.Problems) != 0 || len(p.Tasks) != 0 || p.Status != Pending || p.ReadOnly != "" {
			t.Fatalf("%q: %+v", src, p)
		}
	}
}

func TestUnknownFieldsAreIgnored(t *testing.T) {
	p := mustParse(t, `{"owner": "lucian", "tasks": [{"id": "a", "estimate": 3}]}`)
	if len(p.Problems) != 0 || p.Task("a") == nil {
		t.Fatal(problems(p))
	}
}

func TestStub(t *testing.T) {
	data := Stub("My Plan_2")
	p := mustParse(t, string(data))
	if len(p.Problems) != 0 || p.Version != Version || p.ID != "my-plan-2" || p.Status != Pending {
		t.Fatalf("%s\n%+v", data, p)
	}
	var keys []string
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.Token()
	for dec.More() {
		k, _ := dec.Token()
		keys = append(keys, k.(string))
		var skip json.RawMessage
		dec.Decode(&skip)
	}
	if strings.Join(keys, ",") != "version,id,title,goal,status,scope,tasks" {
		t.Fatalf("stub keys %v", keys)
	}
	if !strings.Contains(string(data), `"in": []`) || !strings.Contains(string(data), `"tasks": []`) {
		t.Fatalf("empty lists are written as []: %s", data)
	}
	if Slug("  ") != "plan" || Slug("auth--v2") != "auth-v2" {
		t.Fatal(Slug("auth--v2"))
	}
}

func TestLenientFields(t *testing.T) {
	p := mustParse(t, `{
  "goal": ["one", "two"],
  "status": " DONE ",
  "tasks": [{"id": " a ", "does": "a single string", "files": ["x.go", 3, null, ""], "verify": null, "status": "Active"}]
}`)
	if p.Goal != "one\ntwo" || p.Status != Done {
		t.Fatalf("%+v", p)
	}
	a := p.Task("a")
	if a == nil || len(a.Does) != 1 || a.Does[0] != "a single string" || len(a.Files) != 1 || a.Verify != nil || a.Status != Active {
		t.Fatalf("%+v", a)
	}
	if !strings.Contains(problems(p), "a: files[1] is not text") || len(p.Problems) != 1 {
		t.Fatalf("problems:\n%s", problems(p))
	}
}

func TestProblems(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"missing id", `{"tasks":[{"title":"x"}]}`, "tasks[0]: has no id"},
		{"empty id", `{"tasks":[{"id":" "}]}`, "tasks[0]: has an empty id"},
		{"non-string id", `{"tasks":[{"id":7}]}`, "tasks[0]: id is not a string"},
		{"duplicate id", `{"tasks":[{"id":"a"},{"id":"a"}]}`, "a: id is used by 2 tasks"},
		{"unknown dep", `{"tasks":[{"id":"a","depends_on":["zz"]}]}`, `a: depends on "zz", which is not a task here`},
		{"unknown parent", `{"tasks":[{"id":"a","parent":"zz"}]}`, `a: parent "zz" is not a task here`},
		{"dep cycle", `{"tasks":[{"id":"a","depends_on":["b"]},{"id":"b","depends_on":["a"]}]}`, "a: depends_on loops: a → b → a"},
		{"self dep", `{"tasks":[{"id":"a","depends_on":"a"}]}`, "a: depends_on loops: a → a"},
		{"parent cycle", `{"tasks":[{"id":"a","parent":"b"},{"id":"b","parent":"a"}]}`, "a: parent loops: a → b → a"},
		{"two active", `{"tasks":[{"id":"a","status":"active"},{"id":"b","status":"active"}]}`, "plan: 2 tasks are active (a, b) · one pin at a time"},
		{"unknown status", `{"tasks":[{"id":"a","status":"wip"}]}`, `a: unknown status "wip" · read as pending`},
		{"task not object", `{"tasks":["a"]}`, "tasks[0]: is not an object"},
		{"bad scope", `{"scope":["a"]}`, `plan: scope is not {"in": [...], "out": [...]}`},
		{"bad version", `{"version":"1"}`, "plan: version is not a whole number"},
	}
	for _, c := range cases {
		p := mustParse(t, c.src)
		if !strings.Contains(problems(p), c.want) {
			t.Errorf("%s: want %q in\n%s", c.name, c.want, problems(p))
		}
		if p.ReadOnly != "" {
			t.Errorf("%s: a problem alone does not lock the file (%q)", c.name, p.ReadOnly)
		}
	}
	for _, src := range []string{`{"version":"two"}`, `{"version":1.5}`, `{"version":0}`} {
		if p := mustParse(t, src); p.ReadOnly == "" {
			t.Errorf("%s: a version Rex can't read locks the file", src)
		}
	}
	if p := mustParse(t, `{"title":"x"}`); p.Version != Version || p.ReadOnly != "" {
		t.Fatalf("a missing version reads as %d: %d %q", Version, p.Version, p.ReadOnly)
	}
	if p := mustParse(t, `{"tasks":[{"id":"a","status":"wip"}]}`); p.Tasks[0].Status != Pending {
		t.Fatal("unknown status reads as pending")
	}
}

func TestReadOnly(t *testing.T) {
	for _, src := range []string{`{"version": 2, "tasks": []}`, `{"tasks": {"a": {}}}`} {
		if p := mustParse(t, src); p.ReadOnly == "" {
			t.Errorf("%s should be read-only", src)
		}
	}
	if p := mustParse(t, `{"version": 1}`); p.ReadOnly != "" {
		t.Fatal(p.ReadOnly)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"{\n  \"id\": \"a\",\n  \"tasks\": [\n    {\"id\": \"b\"}\n    {\"id\": \"c\"}\n  ]\n}", "5:5: "},
		{`[]`, "top level must be an object"},
		{`{"id": "a", "id": "b"}`, `duplicate key "id"`},
		{"{\n<<<<<<< HEAD\n  \"id\": \"a\"\n=======\n  \"id\": \"b\"\n>>>>>>> main\n}", "line 2: merge conflict markers"},
		{"\xff\xfe{\x00}\x00", "UTF-16"},
		{``, "not valid JSON"},
		// Positions are the same on every Go release, for errors inside
		// values too.
		{"{\n  \"tasks\": [\n    {\"id\": tru}\n  ]\n}", "3:15: "},
		{"{\n  \"id\": \"a\",\n  \"id\": \"b\"\n}", `3:3: duplicate key "id"`},
		{"{\"id\": \"a\"} x", "1:13: "},
		{"{\n  \"tasks\": [\n", "3:1: the file ends before the JSON does"},
		{"{\"t\": \"é\", x}", "1:12: "},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want %q", c.src, err, c.want)
		}
	}
	_, err := Parse([]byte("{\n<<<<<<< HEAD\n}"))
	if !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

// Every field Parse reads is a JSON tag on Plan or Task, and the other way
// round, so docs, the stub and the reader cannot drift apart.
func TestEveryTagIsRead(t *testing.T) {
	full := `{"version":1,"id":"p","title":"t","goal":"g","status":"done","scope":{"in":["i"],"out":["o"]},"tasks":[{
		"id":"a","title":"t","parent":"","status":"done","scope":{"in":["i"],"out":["o"]},"needs":["n"],"does":["d"],
		"description":"x","depends_on":[],"verify":["v"],"files":["f"],"accomplished":["a"],"modified":["m"],
		"verified_how":["h"],"completed_notes":"c"}]}`
	p := mustParse(t, full)
	if len(p.Problems) != 0 {
		t.Fatal(problems(p))
	}
	check := func(v reflect.Value) {
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if tag := f.Tag.Get("json"); tag == "-" || tag == "depends_on" || tag == "parent" || tag == "version" {
				continue
			}
			if v.Field(i).IsZero() {
				t.Errorf("%s (%s) was not read", f.Name, f.Tag.Get("json"))
			}
		}
	}
	check(reflect.ValueOf(*p))
	check(reflect.ValueOf(p.Tasks[0]))
}

// Every plan committed to this repo must load.
func TestRepoPlansParse(t *testing.T) {
	root := filepath.Join("..", "..", "rex", "plans")
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".rex.json") {
			return err
		}
		n++
		p, err := Load(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		if len(p.Problems) > 0 || p.ReadOnly != "" {
			t.Errorf("%s:\n%s", path, problems(p))
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no plans found under rex/plans")
	}
}

// docs/plan-format.md is the contract: its example must parse cleanly and
// its "A new plan" block must be exactly what Create writes.
func TestFormatDocMatchesCode(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "plan-format.md"))
	if err != nil {
		t.Fatal(err)
	}
	var blocks []string
	// A Windows checkout may turn the doc's line endings into CRLF.
	doc := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, part := range strings.Split(doc, "```json\n")[1:] {
		blocks = append(blocks, part[:strings.Index(part, "```")])
	}
	if len(blocks) < 2 {
		t.Fatalf("want the example and the stub, found %d json blocks", len(blocks))
	}
	p := mustParse(t, blocks[0])
	if len(p.Problems) != 0 || len(p.Tasks) == 0 {
		t.Fatalf("example:\n%s", problems(p))
	}
	if stub := blocks[len(blocks)-1]; stub != string(Stub("auth-flow")) {
		t.Fatalf("doc stub\n%s\ncode stub\n%s", stub, Stub("auth-flow"))
	}
}

func TestDenseLoopsAreCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"tasks":[`)
	const n = 40
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":"t%d","depends_on":[`, i)
		for j := 0; j < n; j++ {
			if j > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `"t%d"`, j)
		}
		b.WriteString("]}")
	}
	b.WriteString("]}")
	p := mustParse(t, b.String())
	loops := strings.Count(problems(p), "depends_on loops")
	if loops == 0 || loops > maxLoops {
		t.Fatalf("%d loops reported", loops)
	}
}
