package plan

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The example in issue #9, as a plan.
const issue9Plan = `{
  "version": 1,
  "id": "file-storage",
  "title": "Plan file storage",
  "goal": "People can create, open, and share plans under rex/plans.",
  "status": "active",
  "scope": {"in": ["JSON plan format", "folder browser"], "out": ["auto-inject into the agent", "extra palettes"]},
  "tasks": [
    {"id": "plans-ui", "status": "done"},
    {"id": "card-highlight", "status": "active", "depends_on": ["plans-ui"],
     "does": ["selected card is one highlighted block (bar + border + fill)"],
     "needs": ["pick / pickbg colors exist"],
     "verify": ["Open latest — ▌ on all three card rows", "go test ./internal/tui -run PlanCardSelection"],
     "files": ["internal/tui/plans.go", "internal/tui/live.go", "internal/tui/plans_test.go"]},
    {"id": "load-outline", "status": "pending", "depends_on": ["schema"]}
  ]
}`

const issue9Brief = `Send-off: Plan file storage
File: rex/plans/rex_file_tasks/file_storage.rex.json
Status: active · pinned task: card-highlight

Goal
People can create, open, and share plans under rex/plans.

Scope
- In: JSON plan format, folder browser
- Out: auto-inject into the agent, extra palettes

Tasks
- [done] plans-ui
- [active] card-highlight (depends on: plans-ui)
- [pending] load-outline (depends on: schema)

Next
### card-highlight
Does: selected card is one highlighted block (bar + border + fill)
Needs: pick / pickbg colors exist
Verify: Open latest — ▌ on all three card rows; go test ./internal/tui -run PlanCardSelection
Files: internal/tui/plans.go, internal/tui/live.go, internal/tui/plans_test.go

For the next chat
Work only on the pinned task. Stay in scope. Open the plan file in Rex.
Do not inject tools or expand into scope.out. If the plan is wrong, edit the JSON.
Before you open or update the PR, review it read-only, one reviewer per job (design, the UI if it changed, breaking it, lint and checks); fix or explain each finding, then a final readiness check. Repo review rules win.
When you finish, end your final reply with these lines:
accomplished: <what you actually did>
verified: <how you checked it>
and, last, the rex-sendoff line below as it is.

rex-sendoff: rex/plans/rex_file_tasks/file_storage.rex.json#card-highlight
`

const issue9Rel = "rex/plans/rex_file_tasks/file_storage.rex.json"

func TestSendoffIsIssue9sTemplate(t *testing.T) {
	got, err := Sendoff(mustParse(t, issue9Plan), issue9Rel)
	if err != nil {
		t.Fatal(err)
	}
	if got != issue9Brief {
		t.Fatalf("got\n%s\nwant\n%s", got, issue9Brief)
	}
}

func TestSendoffDoneSection(t *testing.T) {
	src := strings.Replace(issue9Plan, `{"id": "plans-ui", "status": "done"}`,
		`{"id": "plans-ui", "status": "done", "does": ["navigator"], "accomplished": ["cards and browse"], "modified": ["internal/tui/plans.go"], "verified_how": ["go test ./internal/tui"]}`, 1)
	got, _ := Sendoff(mustParse(t, src), issue9Rel)
	want := "Done\n- plans-ui\n  intended: navigator\n  actual: cards and browse\n  modified: internal/tui/plans.go\n  verified: go test ./internal/tui\n"
	if !strings.Contains(got, want) {
		t.Fatalf("%s", got)
	}
	if i, j := strings.Index(got, "Next\n"), strings.Index(got, "Done\n"); j < i {
		t.Fatal("Done follows Next")
	}
}

func TestSendoffEdges(t *testing.T) {
	if _, err := Sendoff(mustParse(t, `{}`), "x"); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
	got, _ := Sendoff(mustParse(t, `{"tasks": [{"id": "a", "status": "done"}]}`), "p.rex.json")
	if !strings.Contains(got, "pinned task: none") || !strings.Contains(got, "All tasks are done.") || !strings.HasSuffix(got, "rex-sendoff: p.rex.json#none\n") {
		t.Fatalf("%s", got)
	}
	got, _ = Sendoff(mustParse(t, `{"title": "t\u001b]52;c;ZXZpbA==\u0007x", "tasks": [{"id": "a", "does": ["\u001b[2Jwipe"]}]}`), "p.rex.json")
	if strings.ContainsAny(got, "\x1b\x07") || !strings.Contains(got, "Send-off: tx") || !strings.Contains(got, "Does: wipe") {
		t.Fatalf("no terminal control characters in a brief: %q", got)
	}
}

func TestSendoffStaysUnderTheCap(t *testing.T) {
	var tasks []string
	long := strings.Repeat("a long sentence about the work ", 30)
	for i := 0; i < 80; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"id": "t%02d", "status": "done", "does": [%q], "accomplished": [%q], "modified": ["f%d.go"]}`, i, long, long, i))
	}
	tasks = append(tasks, `{"id": "last", "does": ["`+long+`"]}`)
	got, _ := Sendoff(mustParse(t, `{"title": "big", "goal": "`+long+long+`", "tasks": [`+strings.Join(tasks, ",")+`]}`), "big.rex.json")
	if len(got) > sendoffCap || !strings.HasSuffix(got, "rex-sendoff: big.rex.json#last\n") || !strings.Contains(got, "(see plan file)") {
		t.Fatalf("%d bytes:\n%s", len(got), got[len(got)-400:])
	}
	if !strings.Contains(got, "### last") || !strings.Contains(got, "Done\n") {
		t.Fatalf("the pin survives trimming:\n%s", got)
	}
}

func TestBrief(t *testing.T) {
	got := Brief(mustParse(t, string(Stub("auth"))), "rex/plans/auth.rex.json")
	for _, want := range []string{"Planning brief: rex/plans/auth.rex.json", `"depends_on"`, "file order is do order", "one task should fit one PR", "Tasks now: none", "fill or refine rex/plans/auth.rex.json"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q\n%s", want, got)
		}
	}
}

func TestSendoffCarriesNotesAndTheLastAttempt(t *testing.T) {
	got, _ := Sendoff(mustParse(t, `{"tasks": [
		{"id": "a", "status": "done", "completed_notes": "the API key lives in .env.example"},
		{"id": "b", "status": "active", "accomplished": ["half of it"], "completed_notes": "the flaky test is TestX"}
	]}`), "p.rex.json")
	for _, want := range []string{"- a\n  notes: the API key lives in .env.example", "Last time:\n  actual: half of it\n  notes: the flaky test is TestX"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q\n%s", want, got)
		}
	}
}

func TestSendoffKeepsThePinWithManyOpenTasks(t *testing.T) {
	var tasks []string
	long := strings.Repeat("an open task with a long name ", 3)
	for i := 0; i < 150; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"id": "open-%03d", "title": %q, "depends_on": ["pin"]}`, i, long))
	}
	pin := `{"id": "pin", "status": "active", "does": ["the real work"], "verify": ["go test ./..."], "files": ["a.go"]}`
	got, _ := Sendoff(mustParse(t, `{"tasks": [`+pin+","+strings.Join(tasks, ",")+`]}`), "p.rex.json")
	if len(got) > sendoffCap {
		t.Fatalf("%d bytes", len(got))
	}
	for _, want := range []string{"### pin", "Does: the real work", "Verify: go test ./...", "Files: a.go", "more in the plan file", "rex-sendoff: p.rex.json#pin"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q\n%s", want, got)
		}
	}
}

func TestBriefsNeverCarryEscapes(t *testing.T) {
	p := mustParse(t, "{\"title\": \"t\\u001b]0;pwned\\u0007\", \"tasks\": [{\"title\": \"x\\u001b[2J\"}, {\"id\": \"a\\u001b]52;c;aGk=\\u0007\"}]}")
	for _, s := range []string{Brief(p, "p.rex.json"), func() string { s, _ := Sendoff(p, "p.rex.json"); return s }()} {
		if strings.ContainsRune(s, 0x1b) || strings.ContainsRune(s, 0x07) {
			t.Fatalf("an escape reached a brief: %q", s)
		}
	}
}

func TestSendoffAlwaysEndsWithItsFingerprint(t *testing.T) {
	big := strings.Repeat("every field of this reopened pin is near its cap ", 8)
	fields := ""
	for _, f := range []string{"does", "needs", "verify", "files", "accomplished", "modified", "verified_how"} {
		fields += fmt.Sprintf(`, %q: [%q, %q]`, f, big, big)
	}
	pin := `{"id": "pin", "title": "` + big + `", "description": "` + big + `", "status": "active", "completed_notes": "` + big + `", "scope": {"in": ["` + big + `"], "out": ["` + big + `"]}` + fields + `}`
	got, _ := Sendoff(mustParse(t, `{"title": "`+big+`", "goal": "`+big+big+`", "scope": {"in": ["`+big+`"], "out": ["`+big+`"]}, "tasks": [`+pin+`]}`), "p.rex.json")
	if len(got) > sendoffCap || !strings.Contains(got, "For the next chat\n") || !strings.Contains(got, "accomplished: <what you actually did>") || !strings.HasSuffix(got, "rex-sendoff: p.rex.json#pin\n") {
		t.Fatalf("%d bytes, ends:\n%s", len(got), got[max(0, len(got)-600):])
	}
}

func TestSendoffSurvivesAHugePinID(t *testing.T) {
	id := strings.Repeat("x", 5000)
	got, err := Sendoff(mustParse(t, `{"tasks": [{"id": "`+id+`", "does": ["work"]}]}`), "p.rex.json")
	if err != nil || len(got) > sendoffCap {
		t.Fatalf("%v, %d bytes", err, len(got))
	}
}

func TestSendoffClipsAHugeTitle(t *testing.T) {
	got, _ := Sendoff(mustParse(t, `{"title": "`+strings.Repeat("x", 9000)+`", "tasks": [{"id": "pin", "does": ["work"]}]}`), "p.rex.json")
	if len(got) > sendoffCap || !strings.Contains(got, "### pin") || !strings.HasSuffix(got, "rex-sendoff: p.rex.json#pin\n") {
		t.Fatalf("%d bytes", len(got))
	}
}

func TestSendoffAsksForAReviewPass(t *testing.T) {
	got, _ := Sendoff(mustParse(t, issue9Plan), issue9Rel)
	i := strings.Index(got, "review it read-only, one reviewer per job")
	if i < 0 || i > strings.Index(got, "When you finish") || !strings.Contains(got, "Before you open or update the PR") {
		t.Fatalf("the review pass comes before the trailer request:\n%s", got)
	}
	stacked, _ := Sendoff(mustParse(t, `{"base": "dev", "tasks": [{"id": "a", "status": "active", "branch": "f/a"}]}`), "p.rex.json")
	if r := strings.Index(stacked, "review it read-only"); r < 0 || r > strings.Index(stacked, "\nStack: ") || !strings.Contains(stacked, "checks pass and the review is done, push it") {
		t.Fatalf("in a stack, the review comes before the step that opens the PR:\n%s", stacked)
	}
	if done, _ := Sendoff(mustParse(t, `{"tasks": [{"id": "a", "status": "done"}]}`), "p.rex.json"); strings.Contains(done, "review it read-only") {
		t.Fatal("with nothing pinned there is no PR to review")
	}
	if b := Brief(mustParse(t, issue9Plan), issue9Rel); !strings.Contains(b, "before it opens, it gets a read-only review") {
		t.Fatalf("the planning brief says each PR is reviewed:\n%s", b)
	}
}
