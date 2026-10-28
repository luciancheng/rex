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
	// Old, if set, reports whether the hook Rex wired is an older one that
	// never hands the agent the pin (an OpenCode plugin from before plugin
	// v2); rerunning Install replaces it (docs/ux.md §7a).
	Old func(home string) bool
	// Partial, if set, reports whether Rex's hook is wired for some of the
	// events that hand over the pin but not all (a Cursor install from
	// before sessionStart, which hands a new chat the pin). Rerunning
	// Install adds them.
	Partial func(home string) bool
	// Prompts, if set, reports whether Rex's hook is on an event that
	// hands the agent the pin (a prompt, or a session's start), for the
	// user or in repo's own config (repo "" for none): a hook on the
	// others alone records the feed and hands over nothing. Rerunning
	// Install adds it.
	Prompts func(home, repo string) bool
	// Caveat, if set, is what feed health adds after "sends the pin with
	// each prompt" (docs/ux.md §7a, §10): Cursor reads it per prompt from
	// 3.20; Codex runs a hook only once it is trusted.
	Caveat string
	// WiredHere, if set, reports whether one repo's own config runs Rex
	// (Claude Code's --project install).
	WiredHere func(repo string) bool
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

// InstallHarness installs one harness by name, or for "auto" every one
// found on this machine. With auto, one failure doesn't stop the others,
// and every failure is returned.
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
// machine, whether Rex is wired into it, and whether that wiring hands the
// agent the pin (docs/ux.md §7a, §10). Read-only.
type HarnessStatus struct {
	Name, Label string
	Present     bool
	Wired       bool
	Here        bool   // wired for this repo only
	Old         bool   // wired, but an older hook that never hands over the pin
	Partial     bool   // wired, but without one of the events that hand it over: rerun the install
	Prompts     bool   // Rex's hook is on an event that hands over the pin
	Caveat      string // what feed health adds after "sends the pin with each prompt"
}

// Sends reports whether s's agent gets the pin: Rex is wired into it,
// for the user or this repo, on an event that hands the pin over, and not
// by an older hook that never does.
func (s HarnessStatus) Sends() bool {
	return (s.Wired || s.Here) && s.Prompts && !s.Old
}

// HarnessStatusesFor is every registered harness: whether it is on this
// machine, wired for the user, and wired by repo's own config ("" for none).
func HarnessStatusesFor(repo string) []HarnessStatus {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []HarnessStatus
	for _, h := range Harnesses() {
		st := HarnessStatus{Name: h.Name, Label: h.Label, Present: h.Present(home), Wired: h.Wired(home), Caveat: h.Caveat}
		st.Here = repo != "" && h.WiredHere != nil && h.WiredHere(repo)
		st.Old = st.Wired && h.Old != nil && h.Old(home)
		st.Partial = st.Wired && h.Partial != nil && h.Partial(home)
		st.Prompts = (st.Wired || st.Here) && (h.Prompts == nil || h.Prompts(home, repo))
		out = append(out, st)
	}
	return out
}

// cursorRex reports whether one of commands is Rex's Cursor hook.
func cursorRex(commands []string) bool {
	ours := ingestCommand("cursor", rexExecutable(), runtimeGOOS)
	for _, c := range commands {
		if isRexIngestCommand(c, ours, "cursor") {
			return true
		}
	}
	return false
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
	doc := readHookConfig(path)
	if doc == nil {
		return nil
	}
	return hookCommands(doc["hooks"])
}

// jsonEventCommands is jsonHookCommands for one event: the commands under
// the "hooks" object's key for event (Cursor's shape, a list per event).
func jsonEventCommands(path, event string) []string {
	hooks, _ := readHookConfig(path)["hooks"].(map[string]any)
	return hookCommands(hooks[event])
}

// readHookConfig is a JSON config as a map, or nil when it is missing,
// empty or unreadable.
func readHookConfig(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var doc map[string]any
	if json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &doc) != nil {
		return nil
	}
	return doc
}

// hookCommands are the "command" values of the hook entries in v: objects
// in a list, at any depth.
func hookCommands(v any) []string {
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
	walk(v, false)
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
			return cursorRex(jsonHookCommands(filepath.Join(home, ".cursor", "hooks.json")))
		},
		// An install from before the pin has no sessionStart entry: its
		// prompts still get the pin on builds that read it, a new chat
		// never does (docs/ux.md §7a). Either entry alone hands it over.
		Partial: func(home string) bool {
			path := filepath.Join(home, ".cursor", "hooks.json")
			return !cursorRex(jsonEventCommands(path, "sessionStart")) || !cursorRex(jsonEventCommands(path, "beforeSubmitPrompt"))
		},
		Prompts: func(home, _ string) bool {
			path := filepath.Join(home, ".cursor", "hooks.json")
			return cursorRex(jsonEventCommands(path, "sessionStart")) || cursorRex(jsonEventCommands(path, "beforeSubmitPrompt"))
		},
		Caveat:  "on 3.20+ (older: once per chat)",
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
