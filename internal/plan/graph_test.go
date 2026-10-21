package plan

import (
	"reflect"
	"testing"
)

func TestGraph(t *testing.T) {
	p := mustParse(t, `{"tasks": [
		{"id": "ui", "title": "UI"},
		{"id": "a", "parent": "ui", "status": "done"},
		{"id": "b", "parent": "ui", "depends_on": ["a"]},
		{"id": "c", "parent": "b", "depends_on": ["zz", "a"]},
		{"id": "d", "parent": "c", "status": "skipped"},
		{"id": "e", "parent": "d"},
		{"id": "x", "depends_on": ["y"]},
		{"id": "y", "depends_on": ["x"]},
		{"id": "z", "depends_on": ["b"]}
	]}`)
	g := p.Graph()
	if g.Leaf(g.Task("ui")) || !g.Leaf(g.Task("a")) {
		t.Fatal("leaf")
	}
	if g.Depth(g.Task("e")) != 4 || g.Depth(g.Task("ui")) != 0 {
		t.Fatal(g.Depth(g.Task("e")))
	}
	if got := g.Unmet(g.Task("c")); !reflect.DeepEqual(got, []string{"?zz"}) {
		t.Fatal(got)
	}
	if !g.Cyclic(g.Task("x")) || g.Cyclic(g.Task("z")) {
		t.Fatal("cyclic")
	}
	if g.Ready(g.Task("b")) || g.Ready(g.Task("x")) || g.Ready(g.Task("z")) || !g.Ready(g.Task("e")) {
		t.Fatal("ready: b is a parent, x loops, z waits on b, e is free")
	}
	if done, total := g.Rollup(g.Task("ui")); done != 1 || total != 2 {
		t.Fatalf("rollup %d/%d", done, total) // a (done) and e
	}
	if done, total := g.Progress(); done != 1 || total != 5 {
		t.Fatalf("progress %d/%d", done, total) // leaves a, e, x, y, z
	}
	if deps := g.Dependents("b"); len(deps) != 1 || deps[0].ID != "z" {
		t.Fatal(deps)
	}
	loop := mustParse(t, `{"tasks": [{"id": "a", "parent": "b"}, {"id": "b", "parent": "a"}]}`).Graph()
	if loop.Depth(loop.Task("a")) != 1 {
		t.Fatal("depth stops at a parent loop")
	}
}
