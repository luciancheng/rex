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

// plainRef reports a branch name Rex may put in a command it writes. The
// send-off is pasted into an agent with a shell, so this is a shell-safe
// subset of git check-ref-format.
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

// ref is s when it is a plain branch name, else "".
func ref(s string) string {
	if plainRef(s) {
		return s
	}
	return ""
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

// branches are the stack members' branches Rex may name in a command:
// plain, not the base, not nested with the base or another, and only the
// first task that claims one (Parse reports the rest). why says what is
// wrong with a branch that is set but left out.
func (g *Graph) branches() (use map[*Task]string, why map[*Task]string) {
	p := g.p
	use, why = map[*Task]string{}, map[*Task]string{}
	owner := map[string]bool{}
	for i := range p.Tasks {
		t := &p.Tasks[i]
		if !g.inStack(t) || t.Branch == "" {
			continue
		}
		b := t.Branch
		switch {
		case !plainRef(b):
			why[t] = "not a plain branch name"
		case b == p.Base:
			why[t] = "it is the plan's base"
		case owner[b]:
			why[t] = "an earlier task has it"
		case nests(b, p.Base) != "":
			why[t] = "it nests with the base"
		default:
			for o := range owner {
				if nests(b, o) != "" {
					why[t] = "it nests with " + o
				}
			}
		}
		if why[t] == "" {
			use[t] = b
		}
		owner[b] = true
	}
	return use, why
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
			for b, id := range owner {
				if nests(t.Branch, b) != "" {
					r.problem(t.ID, "branch %q and %s's %q can't both exist in git · one can't sit under the other", t.Branch, id, b)
				}
			}
			owner[t.Branch] = t.ID
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

// inStack is a leaf task that is a PR in a stacked plan.
func (g *Graph) inStack(t *Task) bool {
	return t.ID != "" && g.byID[t.ID] == t && g.Leaf(t) && t.Status != Skipped
}

// below is where t's PR goes: base is the nearest earlier leaf that is
// done (nil: the plan's base), and open are the earlier leaves above base
// that are not done yet, in file order. Done stands in for "has a branch";
// Rex never asks git.
func (g *Graph) below(t *Task) (base *Task, open []*Task) {
	for i := range g.p.Tasks {
		c := &g.p.Tasks[i]
		if c == t {
			break
		}
		if !g.inStack(c) {
			continue
		}
		if c.Status == Done {
			base, open = c, nil
		} else {
			open = append(open, c)
		}
	}
	return base, open
}

// above are the later leaves that are already done: PRs that sit on t
// and move when it changes, in file order.
func (g *Graph) above(t *Task) []*Task {
	var out []*Task
	after := false
	for i := range g.p.Tasks {
		c := &g.p.Tasks[i]
		if c == t {
			after = true
		} else if after && g.inStack(c) && c.Status == Done {
			out = append(out, c)
		}
	}
	return out
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

// stackLines are the send-off's Stack part, inside For the next chat,
// which is never trimmed: where the pin's PR goes, from the file alone.
// rel is the plan file, carried across branch switches as it is.
func stackLines(line func(string, ...any), p *Plan, g *Graph, pin *Task, rel string) {
	use, why := g.branches()
	trunk, trunkArg := ref(p.Base), ref(p.Base)
	if trunk == "" {
		trunk, trunkArg = "the trunk", "<trunk>"
		line("Stack: each leaf task is one PR, stacked in plan file order on the trunk (the repo's base branch: its instructions, else origin's default).")
	} else {
		line("Stack: each leaf task is one PR, stacked in plan file order on %s.", trunk)
	}
	head := use[pin]
	reopened := len(pin.Accomplished)+len(pin.Modified)+len(pin.VerifiedHow) > 0 || clean(pin.CompletedNotes) != ""
	switch {
	case head != "":
		line("- Branch: %s", head)
	case pin.Branch != "":
		line(`- Branch: this task's "branch" can't be used (%s) · name it the way this repo names branches and fix it in the plan`, why[pin])
	case reopened:
		line(`- Branch: none in the plan, but this task had a PR · find it (its body has this task's rex-sendoff line), keep its branch and write it into this task as "branch"`)
	default:
		hint := ""
		if s := ref(p.ID + "/" + pin.ID); s != "" && nests(s, p.Base) == "" {
			hint = " (else " + s + ")"
		}
		line(`- Branch: none in the plan · name it the way this repo names branches%s and write it into this task as "branch"`, hint)
	}
	if head == "" {
		head = "<branch>"
	}
	base, open := g.below(pin)
	on := trunkArg
	switch {
	case base == nil && len(open) == 0:
		line("- Base: %s (the bottom of the stack)", trunk)
	case base == nil:
		line("- Base: %s (nothing below it is done yet)", trunk)
	case use[base] != "":
		on = use[base]
		line("- Base: %s (%s, done) · once its PR has merged, use %s", on, clip(clean(base.ID), idCap), trunk)
	case base.Branch != "":
		on = "<base>"
		line(`- Base: the branch of %s (done; its "branch" in the plan can't be used: %s) · once that PR has merged, use %s`, clip(clean(base.ID), idCap), why[base], trunk)
	default:
		on = "<base>"
		line("- Base: the branch of %s (done; not in the plan: find its PR by that task's id) · once that PR has merged, use %s", clip(clean(base.ID), idCap), trunk)
	}
	if len(open) > 0 {
		var ids []string
		for _, t := range open {
			ids = append(ids, t.ID+" ("+string(t.Status)+")")
		}
		line("- Below it, not done yet: %s. Each joins the stack under this PR when it is done.", list(ids, ", "))
	}
	body := "the rex-sendoff line"
	if base != nil {
		body += " and the PR it sits on"
	}
	line("- Cut the branch from the base. If it already exists, switch to it and add commits, and if it isn't on the base yet, rebase it onto the base and retarget its PR. Once the repo's checks pass, push it and, if it has no PR yet, open one against the base with %s in the body (GitHub: gh pr create --base %s --head %s).", body, on, head)
	if base != nil {
		line("- If the forge links stacks, add this PR to the stack the PR below is in (GitHub: %s). Without that, the chain of base branches is the stack.", g.linkCommand(pin, use, trunkArg, head))
	}
	if up := g.above(pin); len(up) > 0 {
		var names []string
		for _, t := range up {
			if b := use[t]; b != "" {
				names = append(names, b)
			} else {
				names = append(names, t.ID+"'s branch")
			}
		}
		line("- PRs above it, already done: %s. Once this branch is pushed, rebase them onto it from the top of the stack (git rebase --autostash --update-refs %s; leave out any whose PR merged), push each moved branch with --force-with-lease, and retarget the PR of %s to %s.", list(names, ", "), head, clip(clean(names[0]), idCap), head)
	}
	plan := "<plan file>"
	if plainRef(rel) {
		plan = rel
	}
	line("- The plan file is the stack's, not this PR's: commit its changes only on the top branch of the stack, and keep it as it is across every branch switch (copy it out of the repo, git checkout -- %s, switch, copy it back), so Rex keeps reading the current plan.", plan)
	line(`- Repo rules on branches and PRs win: if you use another branch, write it into this task as "branch".`)
}

// linkCommand is gh-stack's link for the pin: every done branch below it
// from the bottom, then the pin, since --base names the bottom's base and
// link corrects the PR bases it is given. A done task with no usable
// branch, or a long stack, leaves the list to the agent.
func (g *Graph) linkCommand(pin *Task, use map[*Task]string, trunkArg, head string) string {
	var chain []string
	for i := range g.p.Tasks {
		c := &g.p.Tasks[i]
		if c == pin {
			break
		}
		if !g.inStack(c) || c.Status != Done {
			continue
		}
		if use[c] == "" {
			return "gh stack link --base " + trunkArg + " <the stack's branches, bottom first> " + head + ", leaving out any whose PR merged"
		}
		chain = append(chain, use[c])
	}
	if len(chain) > 6 {
		chain = append(chain[:2:2], "…", chain[len(chain)-1])
		return "gh stack link --base " + trunkArg + " " + strings.Join(chain, " ") + " " + head + ", every branch from the bottom, leaving out any whose PR merged"
	}
	return "gh stack link --base " + trunkArg + " " + strings.Join(chain, " ") + " " + head + ", leaving out any whose PR merged"
}
