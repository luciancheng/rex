package plan

// Graph answers outline questions about a plan: what sits under what, and
// what is waiting on what. Tasks with a missing or repeated id are only
// reachable by position; the first task with an id wins.
type Graph struct {
	p        *Plan
	byID     map[string]*Task
	children map[string][]*Task
	cyclic   map[string]bool
}

// Graph indexes p. It is cheap; build one per frame.
func (p *Plan) Graph() *Graph {
	g := &Graph{p: p, byID: map[string]*Task{}, children: map[string][]*Task{}, cyclic: map[string]bool{}}
	for i := range p.Tasks {
		t := &p.Tasks[i]
		if t.ID != "" && g.byID[t.ID] == nil {
			g.byID[t.ID] = t
		}
	}
	for i := range p.Tasks {
		t := &p.Tasks[i]
		if t.Parent != "" && t.Parent != t.ID && g.byID[t.Parent] != nil {
			g.children[t.Parent] = append(g.children[t.Parent], t)
		}
	}
	for _, next := range []func(Task) []string{
		func(t Task) []string { return t.DependsOn },
		func(t Task) []string {
			if t.Parent == "" {
				return nil
			}
			return []string{t.Parent}
		},
	} {
		for _, loop := range p.cycles(next) {
			for _, id := range loop {
				g.cyclic[id] = true
			}
		}
	}
	return g
}

// Finished reports a status that no longer blocks anything.
func Finished(s Status) bool { return s == Done || s == Skipped }

func (g *Graph) Task(id string) *Task { return g.byID[id] }

// Children are the tasks whose parent is id, in file order.
func (g *Graph) Children(id string) []*Task {
	if id == "" {
		return nil
	}
	return g.children[id]
}

// Leaf reports a task nobody names as parent. Only leaves are pinned.
func (g *Graph) Leaf(t *Task) bool { return len(g.Children(t.ID)) == 0 }

// Depth is how many parents sit above t, stopping at a loop.
func (g *Graph) Depth(t *Task) int {
	seen := map[string]bool{t.ID: true}
	depth := 0
	for p := g.byID[t.Parent]; p != nil && !seen[p.ID]; p = g.byID[p.Parent] {
		seen[p.ID] = true
		depth++
	}
	return depth
}

// Cyclic reports a task in a depends_on or parent loop.
func (g *Graph) Cyclic(t *Task) bool { return t.ID != "" && g.cyclic[t.ID] }

// Unmet lists t's dependencies that are not finished, in order. An id
// that names no task is returned with a leading "?". A parent is finished
// when it is marked so, or when every leaf under it is (Rex never writes a
// parent's status, so waiting on the mark alone could wait forever).
func (g *Graph) Unmet(t *Task) []string {
	var out []string
	for _, id := range t.DependsOn {
		dep := g.byID[id]
		switch {
		case dep == nil:
			out = append(out, "?"+id)
		case Finished(dep.Status):
		case !g.Leaf(dep):
			// A task under dep never waits on itself: only dep's other
			// leaves count.
			if done, total := g.rollupWithout(dep, t); total == 0 || done < total {
				out = append(out, id)
			}
		default:
			out = append(out, id)
		}
	}
	return out
}

// Ready reports a pending leaf whose dependencies are all finished.
func (g *Graph) Ready(t *Task) bool {
	return t.Status == Pending && t.ID != "" && g.Leaf(t) && !g.Cyclic(t) && len(g.Unmet(t)) == 0
}

// Dependents are the tasks that list id in depends_on, in file order.
func (g *Graph) Dependents(id string) []*Task {
	var out []*Task
	for i := range g.p.Tasks {
		t := &g.p.Tasks[i]
		for _, dep := range t.DependsOn {
			if dep == id {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// Rollup counts the finished leaves under t, and all of them.
func (g *Graph) Rollup(t *Task) (finished, total int) { return g.rollupWithout(t, nil) }

// rollupWithout is Rollup leaving out skip and everything under it.
func (g *Graph) rollupWithout(t, skip *Task) (finished, total int) {
	seen := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, c := range g.Children(id) {
			if c == skip {
				continue
			}
			if g.Leaf(c) {
				total++
				if Finished(c.Status) {
					finished++
				}
				continue
			}
			walk(c.ID)
		}
	}
	walk(t.ID)
	return finished, total
}

// Progress counts the finished leaf tasks, and all of them.
func (g *Graph) Progress() (finished, total int) {
	for i := range g.p.Tasks {
		t := &g.p.Tasks[i]
		if !g.Leaf(t) {
			continue
		}
		total++
		if Finished(t.Status) {
			finished++
		}
	}
	return finished, total
}
