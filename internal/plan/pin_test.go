package plan

import (
	"os"
	"strings"
	"testing"
)

const pinFixture = `{
  "status": "pending",
  "tasks": [
    {"id": "ui", "title": "UI", "status": "active"},
    {"id": "a", "parent": "ui", "status": "active"},
    {"id": "b", "parent": "ui", "status": "pending", "depends_on": ["a"]},
    {"id": "c", "status": "done", "accomplished": ["shipped"]},
    {"id": "d", "status": "blocked"},
    {"id": "x", "depends_on": ["y"]},
    {"id": "y", "depends_on": ["x"]}
  ]
}
`

func TestEffectivePin(t *testing.T) {
	g := mustParse(t, pinFixture).Graph()
	if pin := g.Pin(); pin == nil || pin.ID != "a" {
		t.Fatalf("the first active leaf, not the active parent: %+v", pin)
	}
	g = mustParse(t, `{"tasks": [{"id": "p"}, {"id": "q", "parent": "p", "depends_on": ["r"]}, {"id": "r", "parent": "p"}]}`).Graph()
	if pin := g.Pin(); pin == nil || pin.ID != "r" {
		t.Fatalf("else the first ready leaf: %+v", pin)
	}
	if mustParse(t, `{"tasks": [{"id": "a", "status": "done"}]}`).Graph().Pin() != nil {
		t.Fatal("nothing left, no pin")
	}
}

func TestPinTargetAndWarnings(t *testing.T) {
	g := mustParse(t, pinFixture).Graph()
	if _, err := g.PinTarget(g.Task("ui")); err == nil || !strings.Contains(err.Error(), "nothing under ui is ready") {
		t.Fatal(err) // b waits on a
	}
	if _, err := g.PinTarget(g.Task("x")); err == nil || !strings.Contains(err.Error(), "loop") {
		t.Fatal(err)
	}
	w := strings.Join(g.PinWarnings(g.Task("b")), "; ")
	if w != "it waits on a (active); it moves the pin off a" {
		t.Fatal(w)
	}
	if w := strings.Join(g.PinWarnings(g.Task("d")), "; "); w != "d is marked blocked; it moves the pin off a" {
		t.Fatal(w)
	}
	g2 := mustParse(t, `{"tasks": [{"id": "p"}, {"id": "q", "parent": "p"}]}`).Graph()
	if target, err := g2.PinTarget(g2.Task("p")); err != nil || target.ID != "q" {
		t.Fatal("a parent pins its first ready child")
	}
}

func TestPinOpChangesOnlyStatuses(t *testing.T) {
	path := writePlan(t, pinFixture)
	if _, err := Edit(path, PinOp("b", Pending)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := strings.NewReplacer(
		`"status": "pending",
  "tasks"`, `"status": "active",
  "tasks"`,
		`{"id": "a", "parent": "ui", "status": "active"}`, `{"id": "a", "parent": "ui", "status": "pending"}`,
		`{"id": "b", "parent": "ui", "status": "pending"`, `{"id": "b", "parent": "ui", "status": "active"`,
	).Replace(pinFixture)
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(string(got), `{"id": "ui", "title": "UI", "status": "active"}`) {
		t.Fatal("a parent's status is never written")
	}
}

func TestPinOpReopensAndRefuses(t *testing.T) {
	path := writePlan(t, pinFixture)
	saved, err := Edit(path, PinOp("c", Done))
	if err != nil {
		t.Fatal(err)
	}
	if c := saved.Plan.Task("c"); c.Status != Active || c.Accomplished[0] != "shipped" {
		t.Fatalf("reopen keeps the retrospective: %+v", c)
	}
	before, _ := os.ReadFile(path)
	for _, c := range []struct {
		id   string
		seen Status
		want string
	}{
		{"c", Done, "changed to active in the file"},
		{"zz", Pending, "no longer in the plan"},
		{"ui", Active, "has tasks under it"},
		{"x", Pending, "loop"},
	} {
		if _, err := Edit(path, PinOp(c.id, c.seen)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.id, err)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("refused pins write nothing")
	}
}
