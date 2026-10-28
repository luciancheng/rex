package skill

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/plan"
)

func TestFrontmatter(t *testing.T) {
	s := strings.ReplaceAll(string(RexPlan), "\r\n", "\n") // a Windows checkout may turn the file CRLF
	if !strings.HasPrefix(s, "---\nname: rex-plan\ndescription: ") {
		t.Fatalf("frontmatter must start the file: %q", s[:60])
	}
	end := strings.Index(s[4:], "\n---\n")
	if end < 0 {
		t.Fatal("frontmatter is closed")
	}
	desc := s[strings.Index(s, "description: ")+len("description: ") : 4+end]
	if strings.Contains(desc, "\n") || len(desc) > 1024 {
		t.Fatalf("description is one line under 1024 bytes: %d", len(desc))
	}
	// Valid YAML for strict loaders: quoted, and the quote closes it.
	if len(desc) < 2 || desc[0] != '\'' || desc[len(desc)-1] != '\'' || strings.Contains(desc[1:len(desc)-1], "'") {
		t.Fatalf("the description is one single-quoted YAML scalar: %s", desc)
	}
	if !strings.Contains(s, marker) {
		t.Fatal("the generated marker lets Rex tell its file from yours")
	}
}

// The skill teaches every field the plan format has, and nothing else
// changes the format without the skill changing too.
func TestSkillNamesEveryField(t *testing.T) {
	s := string(RexPlan)
	for _, typ := range []reflect.Type{reflect.TypeOf(plan.Plan{}), reflect.TypeOf(plan.Task{}), reflect.TypeOf(plan.Scope{})} {
		for i := 0; i < typ.NumField(); i++ {
			tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			if !strings.Contains(s, "`"+tag+"`") || !strings.Contains(s, `"`+tag+`"`) {
				t.Errorf("SKILL.md does not teach %q (in the example and the field list)", tag)
			}
		}
	}
	for _, rule := range []string{"One task, one PR", "Never in a plan"} {
		if !strings.Contains(s, rule) {
			t.Errorf("missing %q", rule)
		}
	}
}

// The agent works the plan from the file (docs/ux.md §7, §7b, §8, §9): the
// skill teaches which plan and task to work, how to start and finish a
// task in the file, that the person can pin and finish tasks in Rex too
// (only status values), and never sends the person to Rex to paste.
func TestTheSkillHasTheAgentWorkThePlan(t *testing.T) {
	s := string(RexPlan)
	for _, want := range []string{
		"## Working a plan", "re-read it at the start of every turn", "whose top-level `status` is `active`", "else ask",
		"**The current task** is the leaf task with `\"status\": \"active\"`", "first `pending` leaf, in file order",
		"**Starting a task.**", "set any other `active` leaf to `pending`", "Never write a parent's status", "Save this first, before any other edit",
		"a `depends_on` that names a parent is met once every leaf under it is `done` or `skipped` (a task under that parent never waits on itself)",
		"**Finishing a task.**", "set the plan's own top-level `status` to `done`", "Then stop and report",
		"fill its completion fields", "Then set its `status` to `done`, in the same save if you can",
		"inside the task objects you change (plus the plan's own `status`)", "keep every other byte", "never mark a task done that you did not finish",
		"The person can also create a plan, pin a task (`p`) or mark one done (`d`) in Rex. Rex writes only `status` values (and an empty new plan), the same ones you write; re-read the file and treat it as the truth.",
		"If the active leaf changed since your last turn, the person moved the pin: say which task you are on now.",
		"A task the person marked done in Rex may have no completion fields. Fill them when asked, and never set it back to pending or active without saying so.",
		"### Its PR, when the plan is a stack", "nearest earlier leaf task that is `done`", "later leaves that are `done`",
		"### Rex notes", "a note that starts with `Rex note`", "While a task is pinned it is the current task", "stay inside its files and scope",
		"run its verify list before calling it done", "say so and ask first", "If a note and the file disagree, the file wins", "notes from earlier in the conversation are out of date",
		"The one the person names, else the plan a Rex note names", "a plan the person has open in Rex that has no tasks yet",
		"when they ask you to plan it, or to plan without naming a file or the work", "Other work still gets its own file",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	// Starting a task demotes another task's active leaf, so edits are
	// never confined to one task's object.
	for _, never := range []string{"rex-sendoff", "send-off", "sendoff", "trailer", "d⏎", "p⏎", "s⏎", "b⏎", "paste", "inside that task's object", "rex never sends you anything", "rex has no actions", "rex only reads the file"} {
		if strings.Contains(strings.ToLower(s), never) {
			t.Errorf("the skill still speaks of %q", never)
		}
	}
	desc := s[:strings.Index(s, "\n---")]
	for _, trigger := range []string{"plan work for Rex", ".rex.json", "work, continue or pick up a Rex plan or one of its tasks"} {
		if !strings.Contains(desc, trigger) {
			t.Errorf("the description doesn't trigger on %q", trigger)
		}
	}
}

func TestInstallAndUninstall(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills", "rex-plan")
	var out bytes.Buffer
	if err := Install(dir, &out); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if !bytes.Equal(got, RexPlan) {
		t.Fatal("installed bytes")
	}
	out.Reset()
	Install(dir, &out)
	if !strings.Contains(out.String(), "already installed") {
		t.Fatal(out.String())
	}
	if err := Uninstall(dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the emptied folder is removed")
	}
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("my own skill"), 0o644)
	if Install(dir, io.Discard) == nil || Uninstall(dir, io.Discard) == nil {
		t.Fatal("a SKILL.md Rex didn't write is never replaced or removed")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "SKILL.md")); string(got) != "my own skill" {
		t.Fatal("untouched")
	}
}

// Codex reads .agents/skills, Claude Code .claude/skills (or
// $CLAUDE_CONFIG_DIR), Cursor and OpenCode both: every agent gets a copy.
func TestDirs(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	h, r := filepath.Join("h"), filepath.Join("r")
	want := []string{filepath.Join(h, ".claude", "skills", "rex-plan"), filepath.Join(h, ".agents", "skills", "rex-plan")}
	if got := Dirs(h, r, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("user %q", got)
	}
	want = []string{filepath.Join(r, ".claude", "skills", "rex-plan"), filepath.Join(r, ".agents", "skills", "rex-plan")}
	if got := Dirs(h, r, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("project %q", got)
	}
	if got := Dirs("", r, false); got[0] != "" || got[1] != "" {
		t.Fatalf("no home, no folder (never one relative to the cwd): %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join("c"))
	if got := Dirs(h, r, false); got[0] != filepath.Join("c", "skills", "rex-plan") || got[1] != filepath.Join(h, ".agents", "skills", "rex-plan") {
		t.Fatalf("CLAUDE_CONFIG_DIR moves Claude Code's copy only: %q", got)
	}
	if got := Dirs("", r, false); got[0] == "" || got[1] != "" {
		t.Fatalf("CLAUDE_CONFIG_DIR without a home: %q", got)
	}
}

// Look finds Rex's SKILL.md in any of the four folders, and never a
// SKILL.md Rex didn't write.
func TestLook(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home, repo := t.TempDir(), t.TempDir()
	if Look(home, repo) != Missing || Look(home, "") != Missing {
		t.Fatal("nothing installed yet")
	}
	for _, dir := range append(Dirs(home, repo, false), Dirs(home, repo, true)...) {
		if err := Install(dir, io.Discard); err != nil {
			t.Fatal(err)
		}
		if Look(home, repo) != Current {
			t.Fatalf("installed at %s", dir)
		}
		Uninstall(dir, io.Discard)
		if Look(home, repo) != Missing {
			t.Fatalf("uninstalled at %s", dir)
		}
	}
	own := filepath.Join(Dirs(home, repo, true)[1], "SKILL.md")
	os.MkdirAll(filepath.Dir(own), 0o755)
	os.WriteFile(own, []byte("my own skill"), 0o644)
	if Look(home, repo) != Missing {
		t.Fatal("a SKILL.md Rex didn't write is not Rex's skill")
	}

	// Claude Code reads $CLAUDE_CONFIG_DIR, but Cursor and OpenCode still
	// read ~/.claude/skills (Legacy): a copy in either one counts.
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	for _, dir := range []string{Dirs(home, repo, false)[0], filepath.Join(home, ".claude", "skills", "rex-plan")} {
		if err := Install(dir, io.Discard); err != nil {
			t.Fatal(err)
		}
		if Look(home, repo) != Current {
			t.Fatalf("with CLAUDE_CONFIG_DIR, installed at %s", dir)
		}
		Uninstall(dir, io.Discard)
		if Look(home, repo) != Missing {
			t.Fatalf("with CLAUDE_CONFIG_DIR, uninstalled at %s", dir)
		}
	}
}

// A copy an older Rex wrote still teaches its workflow (Rex marking tasks
// done, a send-off trailer) until --install-skill replaces it, so Look
// calls it Stale, this user's before the repo's.
func TestLookFindsAnOlderRexsCopy(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home, repo := t.TempDir(), t.TempDir()
	old := []byte("---\nname: rex-plan\n---\n" + marker + " -->\nThe person marks tasks done in Rex (d⏎).\n")
	plant := func(dir string) {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "SKILL.md"), old, 0o644)
	}
	mine, project := Dirs(home, repo, false), Dirs(home, repo, true)
	plant(mine[0])
	plant(project[1])
	if got := Look(home, repo); got != Stale {
		t.Fatalf("an old copy in ~/.claude/skills: %v", got)
	}
	for _, dir := range mine {
		if err := Install(dir, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if got := Look(home, repo); got != StaleProject {
		t.Fatalf("this user's copies are new, the repo's .agents/skills isn't: %v", got)
	}
	if got := Look(home, ""); got != Current {
		t.Fatalf("outside a repo only this user's copies count: %v", got)
	}
	for _, dir := range project {
		if err := Install(dir, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if got := Look(home, repo); got != Current {
		t.Fatalf("every copy replaced: %v", got)
	}

	// With $CLAUDE_CONFIG_DIR set, Cursor and OpenCode still read
	// ~/.claude/skills, and --install-skill updates Rex's copy there too:
	// an old one is Stale like any of this user's.
	for _, dir := range append(mine, project...) {
		Uninstall(dir, io.Discard)
	}
	plant(mine[0])
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if got := Look(home, repo); got != Stale {
		t.Fatalf("an old copy in ~/.claude/skills beside CLAUDE_CONFIG_DIR: %v", got)
	}
	for _, dir := range Dirs(home, repo, false) {
		if err := Install(dir, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if got := Look(home, repo); got != Stale {
		t.Fatalf("the new copies don't hide the old one Cursor and OpenCode read: %v", got)
	}
	if err := Install(Legacy(home), io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := Look(home, repo); got != Current {
		t.Fatalf("every copy replaced: %v", got)
	}
}

// The pin note came with skill v3 (docs/ux.md §7a): Rex's copies in both
// folders carry it, and a v2 copy, which says Rex never sends the agent
// anything, reads as out of date.
func TestSkillV3(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if version != 3 {
		t.Fatalf("skill v%d", version)
	}
	home, repo := t.TempDir(), t.TempDir()
	for _, project := range []bool{false, true} {
		for _, dir := range Dirs(home, repo, project) {
			if err := Install(dir, io.Discard); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(filepath.Join(dir, "SKILL.md")); !bytes.Contains(got, []byte(marker+" · skill v3 · ")) {
				t.Fatalf("%s: %.120s", dir, got)
			}
		}
	}
	v2 := bytes.Replace(RexPlan, []byte("skill v3 ·"), []byte("skill v2 ·"), 1)
	for _, c := range []struct {
		project bool
		want    Copy
	}{{false, Stale}, {true, StaleProject}} {
		home, repo := t.TempDir(), t.TempDir()
		for _, dir := range Dirs(home, repo, c.project) {
			os.MkdirAll(dir, 0o755)
			os.WriteFile(filepath.Join(dir, "SKILL.md"), v2, 0o644)
		}
		if got := Look(home, repo); got != c.want {
			t.Errorf("a v2 copy (project %v): %v, want %v", c.project, got, c.want)
		}
	}
}

// A copy is judged by the skill version in its marker, not its bytes: a
// CRLF checkout of a committed --project copy is current, and so is a
// newer Rex's, which --install-skill never replaces (teammates on two Rex
// versions share it); an older one, or one with no version, is out of
// date until --install-skill replaces it.
func TestLookAndInstallGoByTheSkillsVersion(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	tag := fmt.Sprintf("skill v%d ·", version)
	if version < 2 || !bytes.Contains(RexPlan, []byte(marker+" · "+tag)) {
		t.Fatalf("the marker carries the skill version: v%d", version)
	}
	for in, want := range map[string]int{marker + " · skill v12 · x -->": 12, marker + " -->\nskill v9": 0, "my own skill v3": 0, marker + " · skill v -->": 0} {
		if got := versionOf([]byte(in)); got != want {
			t.Errorf("versionOf(%q) = %d, want %d", in, got, want)
		}
	}
	at := func(v int) []byte {
		return bytes.Replace(RexPlan, []byte(tag), []byte(fmt.Sprintf("skill v%d ·", v)), 1)
	}
	for _, c := range []struct {
		name    string
		body    []byte
		look    Copy
		replace bool
		says    string
	}{
		{"a CRLF checkout", bytes.ReplaceAll(RexPlan, []byte("\n"), []byte("\r\n")), Current, false, "already installed"},
		{"a newer Rex's", append(at(version+1), "\nA newer Rex adds a rule here.\n"...), Current, false, "kept the rex-plan skill"},
		{"a newer Rex's, CRLF", bytes.ReplaceAll(at(version+1), []byte("\n"), []byte("\r\n")), Current, false, "a newer Rex wrote it"},
		{"this version, edited", append(append([]byte(nil), RexPlan...), "\nedited\n"...), Current, true, "installed the rex-plan skill"},
		{"an older Rex's", at(version - 1), StaleProject, true, "installed the rex-plan skill"},
		{"one with no version", bytes.Replace(RexPlan, []byte(tag+" "), nil, 1), StaleProject, true, "installed the rex-plan skill"},
	} {
		home, repo := t.TempDir(), t.TempDir()
		for _, dir := range Dirs(home, repo, true) {
			os.MkdirAll(dir, 0o755)
			os.WriteFile(filepath.Join(dir, "SKILL.md"), c.body, 0o644)
		}
		if got := Look(home, repo); got != c.look {
			t.Errorf("%s: Look = %v, want %v", c.name, got, c.look)
		}
		for _, dir := range Dirs(home, repo, true) {
			var out bytes.Buffer
			if err := Install(dir, &out); err != nil || !strings.Contains(out.String(), c.says) {
				t.Errorf("%s: Install = %v, %q", c.name, err, out.String())
			}
			want := c.body
			if c.replace {
				want = RexPlan
			}
			if got, _ := os.ReadFile(filepath.Join(dir, "SKILL.md")); !bytes.Equal(got, want) {
				t.Errorf("%s: want the copy replaced=%v", c.name, c.replace)
			}
		}
		if got := Look(home, repo); got != Current {
			t.Errorf("%s: after --install-skill, Look = %v", c.name, got)
		}
	}
}

// Legacy is ~/.claude/skills/rex-plan only while $CLAUDE_CONFIG_DIR moves
// Claude Code's copy somewhere else.
func TestLegacy(t *testing.T) {
	home := filepath.Join("h")
	old := filepath.Join(home, ".claude", "skills", "rex-plan")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got := Legacy(home); got != "" {
		t.Fatalf("no CLAUDE_CONFIG_DIR: %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	if got := Legacy(home); got != "" {
		t.Fatalf("CLAUDE_CONFIG_DIR is ~/.claude: %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join("c"))
	if got := Legacy(home); got != old {
		t.Fatalf("%q", got)
	}
	if got := Legacy(""); got != "" {
		t.Fatalf("no home: %q", got)
	}
	dir := t.TempDir()
	if Ours(dir) {
		t.Fatal("no SKILL.md")
	}
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("my own skill"), 0o644)
	if Ours(dir) {
		t.Fatal("not Rex's")
	}
	os.WriteFile(filepath.Join(dir, "SKILL.md"), RexPlan, 0o644)
	if !Ours(dir) {
		t.Fatal("Rex's")
	}
}

func TestInstallLeavesAnUnreadableSkillAlone(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	os.WriteFile(path, []byte("my own skill"), 0o000)
	defer os.Chmod(path, 0o644)
	if err := Install(dir, io.Discard); err == nil {
		t.Fatal("a SKILL.md Rex can't read is never replaced")
	}
	os.Chmod(path, 0o644)
	if data, _ := os.ReadFile(path); string(data) != "my own skill" {
		t.Fatal("it is untouched")
	}
}

func TestTheSkillsExampleIsAValidPlan(t *testing.T) {
	s := strings.ReplaceAll(string(RexPlan), "\r\n", "\n")
	start := strings.Index(s, "```json\n")
	if start < 0 {
		t.Fatal("the skill has a JSON example")
	}
	body := s[start+len("```json\n"):]
	body = body[:strings.Index(body, "\n```")]
	p, err := plan.Parse([]byte(body))
	if err != nil || len(p.Problems) != 0 || p.Graph().Pin() == nil {
		t.Fatalf("%v %+v", err, p)
	}
}

func TestTheSkillTeachesAReviewPass(t *testing.T) {
	s := string(RexPlan)
	for _, want := range []string{"### Review before the PR", "Reviewers change nothing", "**Design:**", "**UI**, only when", "**Break it:**", "**Lint:**", "final readiness check", "own review rules"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// The agent writes the plan and Rex opens it: the skill never sends the
// person to Rex to create or open one (docs/ux.md §11).
func TestTheSkillHasTheAgentWriteThePlan(t *testing.T) {
	s := string(RexPlan)
	for _, want := range []string{"## Writing the file", "Write it yourself at `rex/plans/<name>.rex.json`", "A running Rex that is following opens it", "Rex offers yours on its status line", "file write or edit tool", "Don't ask them to create or open it in Rex."} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	// Rex may be latched on another plan, not running, or not wired: the
	// skill never promises the plan is on screen.
	for _, never := range []string{"Create a plan", "Open a plan", "already on their screen", "shows the outline of the plan file you write"} {
		if strings.Contains(s, never) {
			t.Errorf("the skill sends the person to Rex: %q", never)
		}
	}
}
