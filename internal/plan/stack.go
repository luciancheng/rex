package plan

import "strings"

// Stacked reports a plan that is one stack of PRs (docs/plan-format.md,
// Stacks): it names a base, or a task names a branch.
func (p *Plan) Stacked() bool {
	if p.Base != "" {
		return true
	}
	for i := range p.Tasks {
		if p.Tasks[i].Branch != "" {
			return true
		}
	}
	return false
}

// plainRef reports a plain branch name (docs/plan-format.md, Stacks): a
// shell-safe subset of git check-ref-format, since an agent puts these
// names into git commands.
func plainRef(s string) bool {
	if s == "" || len(s) > 80 || s == "HEAD" || strings.ContainsAny(s[:1], "-./") || strings.ContainsAny(s[len(s)-1:], "./") ||
		strings.Contains(s, "..") || strings.Contains(s, "//") || strings.Contains(s, "/.") {
		return false
	}
	for _, c := range s {
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.ContainsRune("._/-", c)) {
			return false
		}
	}
	for _, part := range strings.Split(s, "/") {
		if strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

const plainRule = "letters, digits and . _ - /, at most 80 bytes, not starting with - . or /, no .. or .lock"

// nests is the longer of a and b when one is a folder of the other
// (feature/a and feature/a/b): git can't hold both refs.
func nests(a, b string) string {
	switch {
	case a == "" || b == "":
	case strings.HasPrefix(a, b+"/"):
		return a
	case strings.HasPrefix(b, a+"/"):
		return b
	}
	return ""
}

// stack reports what keeps a stacked plan from landing as one stack.
// Like every problem, these never lock the file.
func (r *reader) stack() {
	p := r.p
	if p.Base != "" && !plainRef(p.Base) {
		r.problem("plan", "base is not a plain branch name · "+plainRule)
	}
	g := p.Graph()
	owner := map[string]string{}
	var order []string // owner's keys in file order, so problems come out the same each time
	for i := range p.Tasks {
		t := &p.Tasks[i]
		if t.ID == "" || g.byID[t.ID] != t {
			continue // reachable only by position: not in the stack
		}
		switch {
		case t.Branch == "":
		case !g.Leaf(t):
			r.problem(t.ID, "has tasks under it, so its branch is ignored · only leaf tasks are PRs")
		case !plainRef(t.Branch):
			r.problem(t.ID, "branch is not a plain branch name · "+plainRule)
		case t.Branch == p.Base:
			r.problem(t.ID, "branch %q is the plan's base · each task needs its own", t.Branch)
		case owner[t.Branch] != "":
			r.problem(t.ID, "branch %q is also %s's · one branch per PR", t.Branch, owner[t.Branch])
		case nests(t.Branch, p.Base) != "":
			r.problem(t.ID, "branch %q and the base %q can't both exist in git · one can't sit under the other", t.Branch, p.Base)
		default:
			for _, b := range order {
				if nests(t.Branch, b) != "" {
					r.problem(t.ID, "branch %q and %s's %q can't both exist in git · one can't sit under the other", t.Branch, owner[b], b)
				}
			}
			owner[t.Branch] = t.ID
			order = append(order, t.Branch)
		}
		if !g.Leaf(t) || g.Cyclic(t) {
			continue
		}
		for _, id := range t.DependsOn {
			late := g.later(t, g.byID[id])
			switch {
			case late == nil:
				continue
			case late.ID == id:
				r.problem(t.ID, "depends on %q, which comes later in the file · a stack builds in file order", id)
			default:
				r.problem(t.ID, "depends on %q, whose task %q comes later in the file · a stack builds in file order", id, late.ID)
			}
			break
		}
	}
}

// later is what t waits on through dep that comes after t in the file and
// is not skipped: dep itself, or for a parent, a leaf under it other than
// t. nil when nothing does, or dep is in a loop (reported already).
func (g *Graph) later(t, dep *Task) *Task {
	if dep == nil || g.Cyclic(dep) {
		return nil
	}
	if g.Leaf(dep) {
		if dep.Index > t.Index && dep.Status != Skipped {
			return dep
		}
		return nil
	}
	seen := map[string]bool{}
	var walk func(id string) *Task
	walk = func(id string) *Task {
		if seen[id] {
			return nil
		}
		seen[id] = true
		for _, c := range g.Children(id) {
			switch {
			case c == t:
			case !g.Leaf(c):
				if late := walk(c.ID); late != nil {
					return late
				}
			case c.Index > t.Index && c.Status != Skipped:
				return c
			}
		}
		return nil
	}
	return walk(dep.ID)
}

// StackBase is where t's PR goes in a stacked plan (docs/plan-format.md,
// Stacks): the branch of the nearest earlier leaf that is done, else the
// plan's base. Only plain names count, and "" means nothing to go on: not
// a stack, a nearest done leaf with no plain branch (an older one's would
// leave its commits out), or no done leaf below and no plain base.
func (g *Graph) StackBase(t *Task) string {
	if !g.p.Stacked() {
		return ""
	}
	var below *Task
	for i := range g.p.Tasks {
		e := &g.p.Tasks[i]
		if e == t {
			break
		}
		if e.Status == Done && e.ID != "" && g.byID[e.ID] == e && g.Leaf(e) {
			below = e
		}
	}
	switch {
	case below != nil:
		if plainRef(below.Branch) {
			return below.Branch
		}
	case plainRef(g.p.Base):
		return g.p.Base
	}
	return ""
}
