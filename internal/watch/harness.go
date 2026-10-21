package watch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/luciancheng/rex/internal/jsonedit"
)

// Harness is one coding agent Rex can watch: where its hook config lives
// and how Rex's ingest command is put there and taken out. Installing only
// ever happens on an explicit `rex ingest --install`; nothing here runs on
// its own.
type Harness struct {
	Name  string // as used on the command line
	Label string // as shown to people

	// Present reports whether the harness looks installed on this machine.
	Present func(home string) bool
	// Wired reports whether Rex's hook is in its config.
	Wired func(home string) bool
	// Install merges Rex's hook into its config and says what it did.
	Install func(home string, out io.Writer) error
	// Uninstall removes only Rex's hook.
	Uninstall func(home string, out io.Writer) error
	// Config is the file Rex edits, for messages.
	Config func(home string) string
}

var harnesses = map[string]Harness{}

func registerHarness(h Harness) { harnesses[h.Name] = h }

// Harnesses lists the known harnesses by name.
func Harnesses() []Harness {
	out := make([]Harness, 0, len(harnesses))
	for _, h := range harnesses {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// HarnessNames lists the names --install accepts, plus "auto".
func HarnessNames() []string {
	var names []string
	for _, h := range Harnesses() {
		names = append(names, h.Name)
	}
	return append(names, "auto")
}

// InstallHarness installs one harness by name, or every present one for
// "auto". A harness that isn't on this machine is skipped with a message.
// With auto, one failure doesn't stop the others, but it is returned.
func InstallHarness(name string, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if name == "auto" {
		found := false
		var errs []error
		for _, h := range Harnesses() {
			if !h.Present(home) {
				continue
			}
			found = true
			if err := h.Install(home, out); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", h.Name, err))
			}
		}
		if !found {
			fmt.Fprintln(out, "no supported agent found on this machine")
		}
		return errors.Join(errs...)
	}
	h, ok := harnesses[name]
	if !ok {
		return fmt.Errorf("unknown harness %q (choose from %s)", name, strings.Join(HarnessNames(), ", "))
	}
	return h.Install(home, out)
}

// UninstallHarness removes Rex's hook from one harness.
func UninstallHarness(name string, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	h, ok := harnesses[name]
	if !ok {
		names := HarnessNames()
		return fmt.Errorf("unknown harness %q (choose from %s)", name, strings.Join(names[:len(names)-1], ", "))
	}
	return h.Uninstall(home, out)
}

// HarnessStatus is one row of feed health: whether a harness is on this
// machine and whether Rex is wired into it. Read-only.
type HarnessStatus struct {
	Name, Label string
	Present     bool
	Wired       bool
}

func HarnessStatuses() []HarnessStatus {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []HarnessStatus
	for _, h := range Harnesses() {
		out = append(out, HarnessStatus{Name: h.Name, Label: h.Label, Present: h.Present(home), Wired: h.Wired(home)})
	}
	return out
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// jsonHookCommands reads the "command" of every hook entry under the
// "hooks" key of a JSON config, for Wired checks: entries are objects in
// a list, the only shape a harness runs (and the only one Uninstall
// removes). A missing or unreadable file has none.
func jsonHookCommands(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var doc map[string]any
	if json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &doc) != nil {
		return nil
	}
	var out []string
	var walk func(v any, inList bool)
	walk = func(v any, inList bool) {
		switch t := v.(type) {
		case map[string]any:
			if c, ok := t["command"].(string); ok && inList {
				out = append(out, c)
			}
			for _, child := range t {
				walk(child, false)
			}
		case []any:
			for _, child := range t {
				walk(child, true)
			}
		}
	}
	walk(doc["hooks"], false)
	return out
}

func init() {
	registerHarness(Harness{
		Name:  "cursor",
		Label: "Cursor",
		Config: func(home string) string {
			return filepath.Join(home, ".cursor", "hooks.json")
		},
		Present: func(home string) bool { return dirExists(filepath.Join(home, ".cursor")) },
		Wired: func(home string) bool {
			ours := ingestCommand("cursor", rexExecutable(), runtimeGOOS)
			for _, c := range jsonHookCommands(filepath.Join(home, ".cursor", "hooks.json")) {
				if isRexIngestCommand(c, ours, "cursor") {
					return true
				}
			}
			return false
		},
		Install: func(home string, out io.Writer) error { return installCursor(home, out) },
		Uninstall: func(home string, out io.Writer) error {
			path := filepath.Join(home, ".cursor", "hooks.json")
			if data, err := os.ReadFile(path); os.IsNotExist(err) || (err == nil && len(bytes.TrimSpace(data)) == 0) {
				fmt.Fprintf(out, "cursor: no hooks at %s\n", path) // and an empty file stays empty
				return nil
			}
			command := IngestCommand()
			changed, err := UpdateJSONFile(path, []byte("{}"), func(d *jsonedit.Doc) ([]byte, error) {
				return removeCursorHooks(d, command)
			})
			if err != nil {
				return err
			}
			if changed {
				fmt.Fprintf(out, "removed rex from cursor hooks at %s\n", path)
			} else {
				fmt.Fprintf(out, "cursor hooks at %s had no rex entries\n", path)
			}
			return nil
		},
	})
}
