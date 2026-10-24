package plan

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
