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
	s := string(RexPlan)
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
