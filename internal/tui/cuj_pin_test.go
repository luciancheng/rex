package tui

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/boot"
	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// IngestRun is rex ingest's entry point (cli.Main), set by ingest_test.go.
var IngestRun func(args []string, stdin io.Reader, stdout, stderr io.Writer) int

// The journey with you at the keys (docs/ux.md §7, §7b, §8, §14): start on Plans →
// enter on Continue → p pins the next up task → your next prompt, through
// Claude Code's hook and rex ingest, hands the agent that task → your
// agent finishes it in the file and Rex says so → d reopens it, and the
// next prompt hands the agent nothing → quit → start again on Continue. The file then holds exactly your agent's
// writes plus the status values you wrote, byte for byte.
func TestCUJHumanPinAndDone(t *testing.T) {
	if IngestRun == nil {
		t.Fatal("ingest_test.go links in rex ingest")
	}
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(PlansDir(repo), "cuj", "sendoff-demo.rex.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	// The CUJ plan with nothing pinned yet: card-highlight is next up.
	planned := mustReplace(t, mustReplace(t, string(cujFixture(t)), `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "pending",
      "depends_on": ["plans-ui"],`), `"status": "active",
  "scope"`, `"status": "pending",
  "scope"`)
	os.WriteFile(path, []byte(planned), 0o644)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	if err := plan.UpdateState(plan.StatePath(), c.t, func(s *plan.State) {
		s.Repos[repo] = plan.RepoState{Plan: path, Opened: float64(c.t.Unix())}
	}); err != nil {
		t.Fatal(err)
	}

	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 80, 24)
	a.now = c.now
	k := &keys{a: a}
	if row := a.plans.Rows[a.plans.Cursor]; a.at != atLibrary || row.Kind != PlanRowContinue || row.Path != path {
		t.Fatalf("start is Plans, on Continue: %+v", row)
	}
	k.press("\r")
	k.spent(t, 1, "open last time's plan (enter on Continue)")
	if a.at != atOutline || !a.follow || a.outline.sel != "card-highlight" {
		t.Fatalf("Continue opens it, following, on its next up: sel=%q", a.outline.sel)
	}

	// p: the pin is yours, written into the file at once.
	k.press("p")
	k.spent(t, 1, "pin a task (p)")
	if got := a.notice().Text; got != "Pinned card-highlight · sends with your next prompt" {
		t.Fatalf("the notice: %q", got)
	}
	pinned, _ := os.ReadFile(path)
	if want := strings.NewReplacer(`"status": "pending",
      "depends_on": ["plans-ui"],`, `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "pending",
  "scope"`, `"status": "active",
  "scope"`).Replace(planned); string(pinned) != want {
		t.Fatalf("p writes the task and the plan active, nothing else:\n%s", pinned)
	}
	if got := a.context().Text; got != "pinned  card-highlight · Selected plan card is one block" {
		t.Fatalf("row 1: %q", got)
	}

	// Your next prompt: Claude Code's UserPromptSubmit hook runs rex ingest,
	// which hands your agent the task you pinned.
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "UserPromptSubmit", "prompt": "carry on with the plan", "cwd": repo, "session_id": "s1"})
	was := boot.At
	boot.At = time.Now() // the hook's deadline counts from its own process's start
	defer func() { boot.At = was }()
	var out, errs bytes.Buffer
	if code := IngestRun([]string{"ingest", "--source", "claude", "--events", feed}, bytes.NewReader(payload), &out, &errs); code != 0 || errs.Len() != 0 {
		t.Fatalf("ingest: %d %q", code, errs.String())
	}
	var reply struct {
		Out struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil || !strings.Contains(reply.Out.Context, "\nPinned task: card-highlight · Selected plan card is one block (status active, in plan \"Send-off demo\")\n") {
		t.Fatalf("the prompt carries the pin: %v %q", err, out.String())
	}
	evs := watch.LastEvents(feed, 10)
	if len(evs) != 1 || evs[0].Kind != "user" || evs[0].Meta["pin"] != "card-highlight" {
		t.Fatalf("the prompt's event says so: %+v", evs)
	}
	a.ingest(evs)

	// Your agent finishes the task: it re-reads the file, fills the
	// completion fields and marks it done; Rex says it was your agent.
	agent := mustReplace(t, string(pinned), `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],
      "accomplished": ["the bar, border and fill cover all three card rows"],
      "modified": ["internal/tui/plans.go"],
      "verified_how": ["go test ./internal/tui -run PlanCardSelection (passed)"],`)
	os.WriteFile(path, []byte(agent), 0o644)
	a.ingest([]watch.Event{at(c, time.Second, wrote(repo, path))})
	k.spent(t, 0, "your agent marks it done")
	if got := a.notice().Text; got != "Your agent finished card-highlight ✔ · next up load-outline" {
		t.Fatalf("the notice: %q", got)
	}
	if !strings.HasPrefix(rowWith(a.frameRows(), "❯ ✔ card-highlight"), "❯ ✔ card-highlight") {
		t.Fatalf("the cursor stays on it, done\n%s", screenText(a))
	}

	// d reopens it: the cursor is still on it.
	c.t = c.t.Add(toastFor)
	k.press("d")
	k.spent(t, 1, "reopen (d)")
	if got := a.notice().Text; got != "card-highlight reopened" {
		t.Fatalf("the notice: %q", got)
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "you reopened card-highlight" {
		t.Fatalf("AGENT records it: %+v", last)
	}
	// Nothing is pinned now, and the plan has tasks: your next prompt hands
	// your agent nothing (docs/ux.md §7a).
	out.Reset()
	boot.At = time.Now() // a new hook process: its own deadline
	payload, _ = json.Marshal(map[string]any{"hook_event_name": "UserPromptSubmit", "prompt": "what next?", "cwd": repo, "session_id": "s1"})
	if code := IngestRun([]string{"ingest", "--source", "claude", "--events", feed}, bytes.NewReader(payload), &out, &errs); code != 0 || errs.Len() != 0 || out.Len() != 0 {
		t.Fatalf("nothing pinned, nothing sent: %d %q %q", code, out.String(), errs.String())
	}
	if evs := watch.LastEvents(feed, 10); len(evs) != 2 || evs[1].Text != "what next?" || evs[1].Meta["pin"] != nil || evs[1].Meta["pin_plan"] != nil {
		t.Fatalf("and the prompt's event carries no pin: %+v", evs)
	}

	k.press(CtrlC, CtrlC)
	k.spent(t, 2, "quit")
	if !a.quit || a.exitLine() != "rex: rex/plans/cuj/sendoff-demo.rex.json · next up card-highlight" {
		t.Fatalf("quit: %q", a.exitLine())
	}
	want := mustReplace(t, agent, `"status": "done",
      "depends_on": ["plans-ui"],`, `"status": "pending",
      "depends_on": ["plans-ui"],`)
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Fatalf("the file is your agent's writes plus your status values:\n%s\nwant\n%s", got, want)
	}

	// Start again: Plans, on Continue, one enter from it.
	b := newApp("yellow", false, "idle", NewPlanBrowser(repo), 80, 24)
	kb := &keys{a: b}
	if row := b.plans.Rows[b.plans.Cursor]; row.Kind != PlanRowContinue || row.Path != path {
		t.Fatalf("the next start is on Continue: %+v", row)
	}
	kb.press("\r")
	kb.spent(t, 1, "open last time's plan (enter on Continue)")
	if b.at != atOutline || !b.follow || b.outline.sel != "card-highlight" {
		t.Fatalf("it opens again, following, on its next up: sel=%q", b.outline.sel)
	}
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Fatal("opening it writes nothing")
	}
}
