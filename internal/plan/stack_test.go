package plan

import (
	"strings"
	"testing"
)

func TestStackFields(t *testing.T) {
	p := mustParse(t, `{"base": " dev ", "tasks": [{"id": "a", "branch": " feature/a "}]}`)
	if p.Base != "dev" || p.Tasks[0].Branch != "feature/a" || !p.Stacked() || len(p.Problems) != 0 {
		t.Fatalf("%+v %s", p, problems(p))
	}
	p = mustParse(t, `{"base": ["a", "b"], "tasks": [{"id": "a", "branch": 3}]}`)
	if !strings.Contains(problems(p), "plan: base is not a plain branch name") || !strings.Contains(problems(p), "a: branch is not text") || p.ReadOnly != "" {
		t.Fatal(problems(p))
	}
	if p := mustParse(t, `{"tasks": [{"id": "a", "depends_on": ["b"]}, {"id": "b"}]}`); p.Stacked() || len(p.Problems) != 0 {
		t.Fatalf("a plan without base or branch is not a stack and gains no problems: %s", problems(p))
	}
	if string(Stub("auth")) != strings.Replace(string(Stub("auth")), `"base"`, "", 1) {
		t.Fatal("the stub has no base")
	}
}

func TestPlainRef(t *testing.T) {
	for _, s := range []string{"dev", "main", "feature/task_completion", "fix/key-input.v2", "release/1.2", strings.Repeat("x", 80)} {
		if !plainRef(s) {
			t.Errorf("%q is plain", s)
		}
	}
	for _, s := range []string{"", "-x", "--upload-pack=x", "/x", "x/", ".x", "x.", "a..b", "a//b", "a/.b", "x.lock", "a.lock/b", "HEAD", "a b", "a;b", "$(rm -rf ~)", "a`b`", "ü", "a\x1b[2J", "a@{u}", strings.Repeat("x", 81)} {
		if plainRef(s) {
			t.Errorf("%q is not plain", s)
		}
	}
}

func TestStackProblems(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"bad branch", `{"base": "dev", "tasks": [{"id": "a", "branch": "a b"}]}`, "a: branch is not a plain branch name"},
		{"branch is base", `{"base": "dev", "tasks": [{"id": "a", "branch": "dev"}]}`, `a: branch "dev" is the plan's base`},
		{"shared branch", `{"base": "dev", "tasks": [{"id": "a", "branch": "x"}, {"id": "b", "branch": "x"}]}`, `b: branch "x" is also a's`},
		{"branch on a parent", `{"base": "dev", "tasks": [{"id": "ui", "branch": "x"}, {"id": "a", "parent": "ui"}]}`, "ui: has tasks under it, so its branch is ignored"},
		{"later dep", `{"base": "dev", "tasks": [{"id": "a", "depends_on": ["b"]}, {"id": "b"}]}`, `a: depends on "b", which comes later in the file`},
		{"later dep via parent", `{"base": "dev", "tasks": [{"id": "ui"}, {"id": "card", "parent": "ui"}, {"id": "x", "depends_on": ["ui"]}, {"id": "late", "parent": "ui"}]}`, `x: depends on "ui", whose task "late" comes later in the file`},
	}
	for _, c := range cases {
		p := mustParse(t, c.src)
		if !strings.Contains(problems(p), c.want) || p.ReadOnly != "" {
			t.Errorf("%s: want %q in\n%s", c.name, c.want, problems(p))
		}
	}
	for _, src := range []string{
		`{"base": "dev", "tasks": [{"id": "ui"}, {"id": "a", "parent": "ui", "depends_on": ["ui"]}]}`,    // its own parent
		`{"base": "dev", "tasks": [{"id": "a", "depends_on": ["b"]}, {"id": "b", "status": "skipped"}]}`, // no PR, no code
		`{"base": "dev", "tasks": [{"id": "a", "depends_on": ["b"]}, {"id": "b", "depends_on": ["a"]}]}`, // a loop, reported as one
	} {
		if got := problems(mustParse(t, src)); strings.Contains(got, "later in the file") {
			t.Errorf("%s:\n%s", src, got)
		}
	}
}

func TestNestedBranchProblemsKeepTheirOrder(t *testing.T) {
	src := `{"base": "dev", "tasks": [{"id": "a", "branch": "x/1"}, {"id": "b", "branch": "x/2"}, {"id": "c", "branch": "x/3"}, {"id": "d", "branch": "x"}]}`
	want := problems(mustParse(t, src))
	if !strings.Contains(want, `a's "x/1"`) || !strings.Contains(want, `c's "x/3"`) || strings.Index(want, `a's`) > strings.Index(want, `c's`) {
		t.Fatalf("in file order: %s", want)
	}
	for i := 0; i < 50; i++ {
		if got := problems(mustParse(t, src)); got != want {
			t.Fatalf("the order changed:\n%s\n%s", got, want)
		}
	}
}
