package plan

import (
	"fmt"
	"slices"
	"strings"
)

// Pin is the effective pin (docs/ux.md §7): the first active leaf task,
// else the first ready one. The agent writes active, or the person with p
// (PinOp); Rex shows the pin and judges drift against it. nil when
// nothing can be pinned.
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

// BuildsOn is the done task the pin builds on (docs/ux.md §6, §7a), one
// rule for task details and the pin note:
// the pin's last done leaf dependency in depends_on order; else, or with
// no pin, the plan's last done leaf. A parent's done is only a label (§7),
// so a parent dependency names nothing.
func (g *Graph) BuildsOn(pin *Task) *Task {
	if pin != nil {
		for i := len(pin.DependsOn) - 1; i >= 0; i-- {
			if dep := g.Task(pin.DependsOn[i]); dep != nil && dep.Status == Done && g.Leaf(dep) {
				return dep
			}
		}
	}
	var last *Task
	for i := range g.p.Tasks {
		if t := &g.p.Tasks[i]; t.Status == Done && g.Leaf(t) {
			last = t
		}
	}
	return last
}

// DoneWhat is what a done task says it accomplished, else its title.
func (t *Task) DoneWhat() string {
	if len(t.Accomplished) > 0 {
		return strings.Join(t.Accomplished, " · ")
	}
	return t.Title
}

// activeLeaves are the leaves marked active, in file order: the pin and
// any second one.
func (g *Graph) activeLeaves() []*Task {
	var out []*Task
	for i := range g.p.Tasks {
		if t := &g.p.Tasks[i]; t.Status == Active && t.ID != "" && g.byID[t.ID] == t && g.Leaf(t) {
			out = append(out, t)
		}
	}
	return out
}

// Moving lists the other active leaves a pin on t takes the pin off, in
// file order: what the person sees move when they press p.
func (g *Graph) Moving(t *Task) []string {
	var out []string
	for _, other := range g.activeLeaves() {
		if other != t {
			out = append(out, other.ID)
		}
	}
	return out
}

// Seen is what the person saw of a task when they pressed p or d
// (docs/ux.md §7b): its id and status, and for a pin the other active
// leaves it moves off. An op writes only while the file still says so.
type Seen struct {
	ID     string
	Status Status
	Moving []string
}

// seen finds the task s names in p and checks it is as the person saw
// it: there once, with that status, and a leaf.
func seen(p *Plan, s Seen) (*Graph, *Task, error) {
	g := p.Graph()
	t := g.Task(s.ID)
	if s.ID == "" || t == nil {
		return nil, nil, &StaleError{ID: s.ID}
	}
	n := 0
	for i := range p.Tasks {
		if p.Tasks[i].ID == s.ID {
			n++
		}
	}
	switch {
	case n > 1:
		return nil, nil, &TwiceError{ID: s.ID}
	case t.Status != s.Status:
		return nil, nil, &StaleError{ID: s.ID}
	case !g.Leaf(t):
		return nil, nil, fmt.Errorf("%s %w", s.ID, ErrGroup)
	}
	return g, t, nil
}

// PinOp is p on a leaf that isn't active (docs/ux.md §7b): active on it,
// pending on every other active leaf (parents are never written), and
// the plan's own status active if it was pending or done. A done task is
// reopened with its completion fields kept. If another task became active
// in the file since the person saw it, nothing is written.
func PinOp(s Seen) Op {
	return func(p *Plan) ([]Change, error) {
		g, t, err := seen(p, s)
		if err != nil {
			return nil, err
		}
		if g.Cyclic(t) {
			return nil, fmt.Errorf("%s %w", s.ID, ErrLoop)
		}
		if now := g.Moving(t); !slices.Equal(now, s.Moving) {
			return nil, &StaleError{ID: firstApart(now, s.Moving)}
		}
		var changes []Change
		if t.Status != Active {
			changes = append(changes, SetStatus(t.ID, Active))
		}
		for _, other := range g.activeLeaves() {
			if other != t {
				changes = append(changes, SetStatus(other.ID, Pending))
			}
		}
		if p.hasStatus && (p.Status == Pending || p.Status == Done) {
			changes = append(changes, SetStatus("", Active))
		}
		return changes, nil
	}
}

// UnpinOp is p on the active leaf: pending on it, nothing else. Nothing
// is sent to the agent until a task is pinned again.
func UnpinOp(s Seen) Op {
	return func(p *Plan) ([]Change, error) {
		_, t, err := seen(p, s)
		if err != nil {
			return nil, err
		}
		return []Change{SetStatus(t.ID, Pending)}, nil
	}
}

// DoneOp is d on a leaf that isn't done: done on it, and the plan's own
// status done once every leaf is done or skipped. Nothing is pinned in
// its place, and no completion field is written: the agent fills those.
func DoneOp(s Seen) Op {
	return func(p *Plan) ([]Change, error) {
		g, t, err := seen(p, s)
		if err != nil {
			return nil, err
		}
		changes := []Change{SetStatus(t.ID, Done)}
		if p.hasStatus && p.Status != Done && allFinishedWith(p, g, t) {
			changes = append(changes, SetStatus("", Done))
		}
		return changes, nil
	}
}

// ReopenOp is d on a done leaf: pending on it, and the plan's own status
// active if it was done.
func ReopenOp(s Seen) Op {
	return func(p *Plan) ([]Change, error) {
		_, t, err := seen(p, s)
		if err != nil {
			return nil, err
		}
		changes := []Change{SetStatus(t.ID, Pending)}
		if p.hasStatus && p.Status == Done {
			changes = append(changes, SetStatus("", Active))
		}
		return changes, nil
	}
}

// allFinishedWith reports whether every leaf is done or skipped once t is.
func allFinishedWith(p *Plan, g *Graph, t *Task) bool {
	leaves := 0
	for i := range p.Tasks {
		leaf := &p.Tasks[i]
		if !g.Leaf(leaf) {
			continue
		}
		leaves++
		if leaf != t && !Finished(leaf.Status) {
			return false
		}
	}
	return leaves > 0
}

// firstApart is the first id in a that b lacks, else the first in b that
// a lacks.
func firstApart(a, b []string) string {
	for _, pair := range [2][2][]string{{a, b}, {b, a}} {
		for _, id := range pair[0] {
			if !slices.Contains(pair[1], id) {
				return id
			}
		}
	}
	if len(a) > 0 {
		return a[0]
	}
	return ""
}
