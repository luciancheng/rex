package skill

import (
	"bytes"
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
	for _, rule := range []string{"accomplished:", "verified:", "rex-sendoff:", "One task, one PR", "Never in a plan"} {
		if !strings.Contains(s, rule) {
			t.Errorf("missing %q", rule)
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

func TestDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if Dir("/h", "/r", false) != filepath.Join("/h", ".claude", "skills", "rex-plan") || Dir("/h", "/r", true) != filepath.Join("/r", ".claude", "skills", "rex-plan") {
		t.Fatal(Dir("/h", "/r", false))
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/c")
	if Dir("/h", "/r", false) != filepath.Join("/c", "skills", "rex-plan") {
		t.Fatal("CLAUDE_CONFIG_DIR")
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
