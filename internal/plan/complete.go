package plan

import (
	"fmt"
	"reflect"
)

// Completion is what marking a task done writes. A nil list leaves that
// field as it is in the file, so a value nobody edited keeps its bytes.
// Modified paths are added to what the file already lists.
type Completion struct {
	Accomplished []string
	VerifiedHow  []string
	AddModified  []string
	Notes        *string
}

// CompleteOp marks task id done with its retrospective (docs/ux.md §8).
// seen is the task as the user saw it when the form opened; if the file
// moved on since, the op refuses unless force is set. The plan becomes
// done once every leaf is done or skipped. No other task is written.
func CompleteOp(id string, seen *Task, force bool, c Completion) Op {
	return func(p *Plan) ([]Change, error) {
		g := p.Graph()
		t := g.Task(id)
		if t == nil {
			return nil, fmt.Errorf("%s is no longer in the plan", id)
		}
		if !g.Leaf(t) {
			return nil, fmt.Errorf("%s has tasks under it now; mark those done", id)
		}
		if seen != nil && !force && !sameTask(*t, *seen) {
			return nil, ErrTaskChanged
		}
		var changes []Change
		if t.Status != Done {
			changes = append(changes, SetStatus(id, Done))
		}
		if c.Accomplished != nil && !reflect.DeepEqual(c.Accomplished, t.Accomplished) {
			changes = append(changes, SetList(id, "accomplished", c.Accomplished))
		}
		if c.VerifiedHow != nil && !reflect.DeepEqual(c.VerifiedHow, t.VerifiedHow) {
			changes = append(changes, SetList(id, "verified_how", c.VerifiedHow))
		}
		if merged := mergePaths(t.Modified, c.AddModified); len(merged) != len(t.Modified) {
			changes = append(changes, SetList(id, "modified", merged))
		}
		if c.Notes != nil && *c.Notes != t.CompletedNotes {
			changes = append(changes, SetString(id, "completed_notes", *c.Notes))
		}
		if p.Status != Done && allFinishedWith(p, g, t) {
			changes = append(changes, SetStatus("", Done))
		}
		return changes, nil
	}
}

// ErrTaskChanged means the task was edited on disk after the completion
// form opened.
var ErrTaskChanged = fmt.Errorf("the task changed on disk")

func sameTask(a, b Task) bool {
	a.Index, b.Index = 0, 0
	return reflect.DeepEqual(a, b)
}

// mergePaths is have plus the paths in add it doesn't list yet, in order.
func mergePaths(have, add []string) []string {
	out := append([]string(nil), have...)
	seen := map[string]bool{}
	for _, p := range have {
		seen[p] = true
	}
	for _, p := range add {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
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
