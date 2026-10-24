package plan

import (
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

func TestPinSurvivesALoopThroughARepeatedId(t *testing.T) {
	g := mustParse(t, `{"tasks":[{"id":"x"},{"id":"y","parent":"x"},{"id":"x","parent":"y"}]}`).Graph()
	done := make(chan struct{})
	go func() {
		defer close(done)
		g.Pin()
	}()
	<-done // a stack overflow would kill the test binary instead
}

func TestOnlyActiveLeavesCountAsTwoActive(t *testing.T) {
	p := mustParse(t, `{"tasks":[{"id":"ui","status":"active"},{"id":"a","parent":"ui","status":"active"}]}`)
	for _, pr := range p.Problems {
		if strings.Contains(pr.Msg, "active") {
			t.Fatalf("a parent marked active is no second pin: %s", pr)
		}
	}
}

func TestIdlessChildrenStayChildren(t *testing.T) {
	g := mustParse(t, `{"tasks":[{"id":"p","title":"Parent"},{"title":"child","parent":"p"}]}`).Graph()
	if g.Leaf(g.Task("p")) {
		t.Fatal("a parent with an id-less child is no leaf")
	}
	if pin := g.Pin(); pin != nil && pin.ID == "p" {
		t.Fatal("a parent is never pinned")
	}
}
