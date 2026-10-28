package plan

import "strings"

// Pin is the effective pin (docs/ux.md §7): the first active leaf task,
// else the first ready one. The agent writes active; Rex only shows the
// pin and judges drift against it. nil when nothing can be pinned.
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
