package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/watch"
)

// docs/ux.md §7a: a prompt with no text of its own (an image alone) gets
// the note like any other, and its event, "(no text)", says it carried
// the pin, so Rex's send state never says less than happened.
func TestAPromptWithNoTextSaysItCarriedThePin(t *testing.T) {
	repo, feed := pinnedRepo(t)
	// A folder each: the same words from one folder with the same pin
	// within seconds are one prompt reported twice.
	for i, source := range []string{"claude", "codex"} {
		p := prompt(filepath.Join(repo, []string{"internal", "docs"}[i]), "session_id", source)
		p["prompt"] = ""
		if source == "codex" {
			p["turn_id"] = "t1"
		}
		code, out, errs := hookRun(t, source, feed, p)
		if code != 0 || errs != "" || !strings.Contains(out, "Pinned task: card-highlight") {
			t.Fatalf("%s: %d %q %q", source, code, out, errs)
		}
		ev := lastUser(t, feed)
		if ev.Source != source || ev.Text != watch.NoText || ev.Meta["pin"] != "card-highlight" || ev.Meta["pin_plan"] != "rex/plans/cuj/sendoff-demo.rex.json" || ev.Meta["pin_repo"] != repo {
			t.Fatalf("%s: %+v", source, ev)
		}
	}
	// With nothing to hand over, such a prompt is no event, as before.
	lastPlan(t, repo, putPlan(t, repo, "idle.rex.json", `{"version": 1, "tasks": [{"id": "a"}]}`))
	n := feedLen(feed)
	p := prompt(repo, "session_id", "s3")
	p["prompt"] = ""
	if _, out, _ := hookRun(t, "claude", feed, p); out != "" || feedLen(feed) != n {
		t.Fatalf("nothing pinned: %q, %d events", out, feedLen(feed)-n)
	}
}

// gitRepo is a new git repo at dir, with plan (pinning id) as the plan
// Rex last opened there.
func gitRepo(t *testing.T, dir, id string) string {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	lastPlan(t, dir, putPlan(t, dir, "p.rex.json", `{"version": 1, "tasks": [{"id": "`+id+`", "status": "active"}]}`))
	return dir
}

// docs/ux.md §7a: the prompt's event names the repo its note was looked
// up in (pin_repo), so only that repo's Rex counts it: never a repo the
// prompt merely came from too (another root of the workspace, a folder
// above or below it), whatever plan path and task id they share. A
// payload with no folder of its own comes from the folder looked up in.
func TestStampsNameTheRepoTheNoteCameFrom(t *testing.T) {
	repo, feed := pinnedRepo(t)
	pinRepo := func() any { return lastUser(t, feed).Meta["pin_repo"] }

	// Cursor: $CURSOR_PROJECT_DIR, with no root in the payload: that
	// folder is its origin too.
	t.Setenv("CURSOR_PROJECT_DIR", repo)
	if _, out, _ := hookRun(t, "cursor", feed, cursorPrompt("from the env")); !strings.Contains(out, "Pinned task: card-highlight") {
		t.Fatalf("%q", out)
	}
	ev := lastUser(t, feed)
	if roots, _ := ev.Meta["roots"].([]any); len(roots) != 1 || roots[0] != repo || ev.Meta["pin"] != "card-highlight" || ev.Meta["pin_repo"] != repo {
		t.Fatalf("%+v", ev.Meta)
	}
	if in, known := watch.NewScope(repo).Owns(ev); !in || !known {
		t.Fatal("this repo's")
	}
	// A root outside git, and $CURSOR_PROJECT_DIR: the note is the env
	// repo's, whatever folder the prompt came from.
	other := t.TempDir()
	if _, out, _ := hookRun(t, "cursor", feed, cursorPrompt("from its root", other)); !strings.Contains(out, "Pinned task: card-highlight") {
		t.Fatalf("%q", out)
	}
	if roots, _ := lastUser(t, feed).Meta["roots"].([]any); len(roots) != 1 || roots[0] != other || pinRepo() != repo {
		t.Fatalf("its root is its origin, and the note is still the env repo's: %v %v", roots, pinRepo())
	}
	t.Setenv("CURSOR_PROJECT_DIR", "")

	// A workspace of two repos, both pinning setup: the note is the
	// first's, and says so.
	a, b := gitRepo(t, t.TempDir(), "setup"), gitRepo(t, t.TempDir(), "setup")
	hookRun(t, "cursor", feed, cursorPrompt("both roots", a, b))
	if ev := lastUser(t, feed); ev.Meta["pin"] != "setup" || ev.Meta["pin_repo"] != a {
		t.Fatalf("multi-root: %+v", ev.Meta)
	}
	// A repo inside another: each prompt names the repo it came from.
	sub := gitRepo(t, filepath.Join(a, "sub"), "setup")
	hookRun(t, "claude", feed, prompt(filepath.Join(sub, "internal"), "prompt", "from below", "session_id", "s2"))
	if pinRepo() != sub {
		t.Fatalf("nested, from inside: %v", pinRepo())
	}
	hookRun(t, "claude", feed, prompt(a, "prompt", "from above", "session_id", "s3"))
	if pinRepo() != a {
		t.Fatalf("nested, from outside: %v", pinRepo())
	}
	// OpenCode: the prompt's own folder, worked out as it is recorded.
	_, out, _ := hookRun(t, "opencode", feed, openCodeAsk(sub))
	hookRun(t, "opencode", feed, openCodePrompt(filepath.Join(sub, "internal"), "opencode", openCodeRead(t, out)))
	if ev := lastUser(t, feed); ev.Meta["pin"] != "setup" || ev.Meta["pin_repo"] != sub {
		t.Fatalf("opencode: %+v", ev.Meta)
	}
}

// docs/ux.md §7a: the note is written first, in one write, and only a
// note the harness was handed is recorded as carried: a stdout that fails
// (a harness that closed it, a pipe gone) leaves the prompt unstamped,
// and the exit is still 0.
func TestAFailedWriteLeavesNoStamp(t *testing.T) {
	repo, feed := pinnedRepo(t)
	closed, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	for i, out := range []io.Writer{brokenPipe{}, closed} {
		data, _ := json.Marshal(prompt(repo, "prompt", fmt.Sprintf("lost %d", i)))
		var stderr bytes.Buffer
		if code := Main([]string{"ingest", "--source", "claude", "--events", feed}, bytes.NewReader(data), out, &stderr); code != 0 {
			t.Fatalf("%d: exit %d %q", i, code, stderr.String())
		}
		ev := lastUser(t, feed)
		if ev.Text != fmt.Sprintf("lost %d", i) || ev.Meta["pin"] != nil || ev.Meta["pin_plan"] != nil || ev.Meta["pin_repo"] != nil {
			t.Fatalf("%d: the prompt is recorded, unstamped: %+v", i, ev)
		}
	}
	var stdout bytes.Buffer
	data, _ := json.Marshal(prompt(repo, "prompt", "written"))
	Main([]string{"ingest", "--source", "claude", "--events", feed}, bytes.NewReader(data), &stdout, io.Discard)
	if ev := lastUser(t, feed); strings.Count(stdout.String(), "\n") != 1 || ev.Meta["pin"] != "card-highlight" {
		t.Fatalf("written once, then stamped: %q %+v", stdout.String(), ev.Meta)
	}
}

// docs/ux.md §7a: Cursor before 3.20 ignores a prompt's
// additional_context, so its prompt's event never says it carried the
// pin (the reply still goes, harmlessly), but says it went unread
// (pin_unread), so Rex's send state can say so; 3.20 and its CLI from
// 2026.10 read it.
func TestOldCursorPromptIsNeverSaidToCarryThePin(t *testing.T) {
	repo, feed := pinnedRepo(t)
	for _, c := range []struct {
		version string
		carried bool
	}{{"3.2.11", false}, {"1.7.54", false}, {"2025.08.25", false}, {"3.20.17", true}, {"2026.10.21", true}, {"", true}} {
		text := "carry on, " + c.version + "."
		p := cursorPrompt(text, filepath.Join(repo, "internal"))
		if c.version == "" {
			delete(p, "cursor_version")
		} else {
			p["cursor_version"] = c.version
		}
		code, out, _ := hookRun(t, "cursor", feed, p)
		if note, _ := cursorReply(t, out)["additional_context"].(string); code != 0 || !strings.Contains(note, "Pinned task: card-highlight") {
			t.Fatalf("%s: the reply goes either way: %q", c.version, out)
		}
		ev := lastUser(t, feed)
		_, has := ev.Meta["pin"]
		unread, _ := ev.Meta["pin_unread"].(bool)
		if ev.Text != text || has != c.carried || (ev.Meta["pin_plan"] != nil) != c.carried || unread == c.carried || ev.Meta["pin_repo"] != repo {
			t.Fatalf("%s: carried %v: %+v", c.version, c.carried, ev.Meta)
		}
	}
}
