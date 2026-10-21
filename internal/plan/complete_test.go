package plan

import (
	"errors"
	"os"
	"strings"
	"testing"
)

const doneFixture = `{
  "status": "active",
  "tasks": [
    {"id": "a", "status": "done"},
    {
      "id": "b",
      "status": "active",
      "does": ["the thing"],
      "modified": ["kept.go"]
    },
    {"id": "c", "status": "skipped"}
  ]
}
`

func TestCompleteWritesOnlyWhatChanged(t *testing.T) {
	path := writePlan(t, doneFixture)
	p, _ := Load(path)
	seen := *p.Task("b")
	saved, err := Edit(path, CompleteOp("b", &seen, false, Completion{
		Accomplished: []string{"did the thing"},
		VerifiedHow:  []string{"go test ./... (passed)"},
		AddModified:  []string{"kept.go", "new.go"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	b := saved.Plan.Task("b")
	if b.Status != Done || b.Accomplished[0] != "did the thing" || strings.Join(b.Modified, ",") != "kept.go,new.go" || b.VerifiedHow[0] != "go test ./... (passed)" {
		t.Fatalf("%+v", b)
	}
	if saved.Plan.Status != Done {
		t.Fatal("every leaf done or skipped: the plan is done")
	}
	got, _ := os.ReadFile(path)
	added := diffLines(doneFixture, string(got))
	for _, line := range added {
		if strings.Contains(line, `"id": "a"`) || strings.Contains(line, `"id": "c"`) {
			t.Fatalf("another task changed: %q", line)
		}
	}
	// Nothing new to say: nothing is written.
	again, _ := Load(path)
	seen = *again.Task("b")
	before, _ := os.ReadFile(path)
	if _, err := Edit(path, CompleteOp("b", &seen, false, Completion{AddModified: []string{"new.go"}})); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatalf("a completion with nothing new rewrote the file:\n%s", after)
	}
}

func TestCompleteRefusesATaskThatMovedOn(t *testing.T) {
	path := writePlan(t, doneFixture)
	p, _ := Load(path)
	seen := *p.Task("b")
	os.WriteFile(path, []byte(strings.Replace(doneFixture, `"the thing"`, `"the new thing"`, 1)), 0o644)
	if _, err := Edit(path, CompleteOp("b", &seen, false, Completion{Accomplished: []string{"x"}})); !errors.Is(err, ErrTaskChanged) {
		t.Fatal(err)
	}
	if _, err := Edit(path, CompleteOp("b", &seen, true, Completion{Accomplished: []string{"x"}})); err != nil {
		t.Fatal("force saves anyway:", err)
	}
	if _, err := Edit(path, CompleteOp("zz", nil, false, Completion{})); err == nil {
		t.Fatal("a gone task is refused")
	}
}

func TestCompleteKeepsModifiedItCantRead(t *testing.T) {
	path := writePlan(t, strings.Replace(doneFixture, `"modified": ["kept.go"]`, `"modified": ["kept.go", {"path": "agent.go"}]`, 1))
	saved, err := Edit(path, CompleteOp("b", nil, false, Completion{Accomplished: []string{"x"}, AddModified: []string{"new.go"}}))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(saved.Data); !strings.Contains(got, `"modified": ["kept.go", {"path": "agent.go"}]`) || strings.Contains(got, "new.go") {
		t.Fatalf("a list Rex can't read is left as it is:\n%s", got)
	}
}

func TestCompleteLeavesThePlanOpenWhileWorkRemains(t *testing.T) {
	path := writePlan(t, strings.Replace(doneFixture, `"status": "skipped"`, `"status": "pending"`, 1))
	saved, err := Edit(path, CompleteOp("b", nil, false, Completion{Accomplished: []string{"x"}}))
	if err != nil || saved.Plan.Status != Active || saved.Plan.Task("c").Status != Pending {
		t.Fatalf("%v %+v", err, saved.Plan)
	}
}
