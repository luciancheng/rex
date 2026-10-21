package plan

import (
	"errors"
	"fmt"
)

// Pin is the effective pin (docs/ux.md §7): the first active leaf task,
// else the first ready one. Drift, the send-off and completion all read
// it. nil when nothing can be pinned.
func (g *Graph) Pin() *Task {
	for i := range g.p.Tasks {
		if t := &g.p.Tasks[i]; t.Status == Active && t.ID != "" && g.byID[t.ID] == t && g.Leaf(t) && !g.Cyclic(t) {
			return t
		}
	}
	for i := range g.p.Tasks {
		if t := &g.p.Tasks[i]; g.byID[t.ID] == t && g.Ready(t) {
			return t
		}
	}
	return nil
}

// PinTarget is the task a pin on t lands on: t itself when it is a leaf,
// else the first ready leaf under it.
func (g *Graph) PinTarget(t *Task) (*Task, error) {
	if t.ID == "" || g.byID[t.ID] != t {
		return nil, errors.New("this task has no usable id; give it one in the file first")
	}
	if g.Leaf(t) {
		if g.Cyclic(t) {
			return nil, fmt.Errorf("%s is in a loop; fix its depends_on or parent first", t.ID)
		}
		return t, nil
	}
	var walk func(id string) *Task
	walk = func(id string) *Task {
		for _, c := range g.Children(id) {
			if g.Leaf(c) && g.Ready(c) {
				return c
			}
			if !g.Leaf(c) && !g.Cyclic(c) {
				if found := walk(c.ID); found != nil {
					return found
				}
			}
		}
		return nil
	}
	if found := walk(t.ID); found != nil {
		return found, nil
	}
	return nil, fmt.Errorf("nothing under %s is ready to pin", t.ID)
}

// PinWarnings are the reasons pinning t needs a confirm: it is blocked,
// it waits on unfinished tasks, or it moves the pin off another task.
func (g *Graph) PinWarnings(t *Task) []string {
	var out []string
	if t.Status == Blocked {
		out = append(out, t.ID+" is marked blocked")
	}
	for _, dep := range g.Unmet(t) {
		if dep[0] == '?' {
			out = append(out, "it depends on "+dep[1:]+", which is not a task here")
		} else {
			out = append(out, "it waits on "+dep+" ("+string(g.byID[dep].Status)+")")
		}
	}
	for _, other := range g.activeLeaves() {
		if other != t {
			out = append(out, "it moves the pin off "+other.ID)
		}
	}
	return out
}

func (g *Graph) activeLeaves() []*Task {
	var out []*Task
	for i := range g.p.Tasks {
		if t := &g.p.Tasks[i]; t.Status == Active && t.ID != "" && g.Leaf(t) {
			out = append(out, t)
		}
	}
	return out
}

// PinOp pins task id: active on it, pending on every other active leaf
// (parents are never written), and the plan active if it was pending or
// done. seen is the status the user saw; if the file moved on since, the
// pin is refused. A done task is reopened with its retrospective kept.
func PinOp(id string, seen Status) Op {
	return func(p *Plan) ([]Change, error) {
		g := p.Graph()
		t := g.Task(id)
		if t == nil {
			return nil, fmt.Errorf("%s is no longer in the plan", id)
		}
		if t.Status != seen {
			return nil, fmt.Errorf("%s changed to %s in the file; look again", id, t.Status)
		}
		if !g.Leaf(t) {
			return nil, fmt.Errorf("%s has tasks under it now; pin one of those", id)
		}
		if g.Cyclic(t) {
			return nil, fmt.Errorf("%s is in a loop; fix its depends_on or parent first", id)
		}
		var changes []Change
		if t.Status != Active {
			changes = append(changes, SetStatus(id, Active))
		}
		for _, other := range g.activeLeaves() {
			if other != t {
				changes = append(changes, SetStatus(other.ID, Pending))
			}
		}
		if p.Status == Pending || p.Status == Done {
			changes = append(changes, SetStatus("", Active))
		}
		return changes, nil
	}
}
