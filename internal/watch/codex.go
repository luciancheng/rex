package watch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/luciancheng/rex/internal/jsonedit"
)

// CodexHookEvents are the Codex hooks Rex registers. SessionEnd and
// Interrupt are left out: Codex caps them at 3 s, and a login shell plus
// Rex can miss that. Codex has no thinking hook; Stop carries the reply.
var CodexHookEvents = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PreToolUse",
	"PermissionRequest",
	"PostToolUse",
	"Stop",
}

const codexHookTimeout = 5

// codexEvents are the Codex hook names --source auto routes to MapCodex
// when the payload carries a turn_id (a session-scoped hook such as
// SessionStart has none, so auto can't tell it from Claude's). Rex
// registers only some of them.
var codexEvents = map[string]bool{
	"SessionStart": true, "UserPromptSubmit": true, "PreToolUse": true, "PermissionRequest": true,
	"PostToolUse": true, "Stop": true, "SessionEnd": true, "Interrupt": true,
	"PreCompact": true, "PostCompact": true, "SubagentStart": true, "SubagentStop": true,
}

// codexDir is Codex's home: $CODEX_HOME or ~/.codex.
func codexDir(home string) string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".codex")
}

// codexHandler is the hook entry. Codex trusts a hook by a hash of its
// handler, so the entry must come out byte for byte the same on every
// install from the same binary. Codex runs `$SHELL -lc command` on Unix
// and `cmd /C commandWindows` on Windows.
func codexHandler(exe, goos string) map[string]any {
	h := map[string]any{"type": "command", "timeout": codexHookTimeout}
	if goos == "windows" {
		h["command"] = `"` + strings.ReplaceAll(exe, `"`, "") + `" ingest --source codex`
		h["commandWindows"] = h["command"]
		return h
	}
	h["command"] = shellQuote(exe) + " ingest --source codex"
	return h
}

func isRexCodexHandler(h, ours map[string]any) bool {
	for _, key := range []string{"command", "commandWindows"} {
		if cmd, _ := h[key].(string); cmd != "" && isRexIngestCommand(cmd, fmt.Sprint(ours["command"]), "codex") {
			return true
		}
	}
	return false
}

var codexSpec = groupHooks{events: CodexHookEvents, isRex: isRexCodexHandler, file: "hooks.json"}

// installCodex merges Rex's handler into Codex's hooks.json. The file may
// only hold "description" and "hooks" (Codex refuses anything else), so
// nothing else is ever added.
func installCodex(path string, out io.Writer) error {
	ours := codexHandler(rexExecutable(), runtimeGOOS)
	// A new file starts multi-line, so the hooks are written one per line.
	changed, err := UpdateJSONFile(path, []byte("{\n}\n"), func(d *jsonedit.Doc) ([]byte, error) {
		return mergeGroupHooks(d, codexSpec, ours, true)
	})
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(out, "installed codex hooks at %s\n", path)
	} else {
		fmt.Fprintf(out, "codex hooks already installed at %s\n", path)
	}
	fmt.Fprintln(out, "codex: open Codex, run /hooks and trust the rex hooks; Codex skips new hooks until you do")
	fmt.Fprintln(out, "codex: before each prompt the hook also adds the task pinned in this repo's Rex plan, if one is, to the agent's context")
	if runtimeGOOS != "windows" {
		fmt.Fprintln(out, "codex: hooks run in your login shell ($SHELL -lc); anything your shell profile prints is added to the agent's context")
	}
	fmt.Fprintf(out, "feed: %s\n", DefaultEventPath())
	return nil
}

func uninstallCodex(path string, out io.Writer) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Fprintf(out, "codex: no hooks file at %s\n", path)
		return nil
	}
	ours := codexHandler(rexExecutable(), runtimeGOOS)
	changed, err := UpdateJSONFile(path, []byte("{}\n"), func(d *jsonedit.Doc) ([]byte, error) {
		return mergeGroupHooks(d, codexSpec, ours, false)
	})
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(out, "removed rex from codex hooks at %s\n", path)
	} else {
		fmt.Fprintf(out, "codex hooks at %s had no rex hooks\n", path)
	}
	return nil
}

func codexWired(path string, events ...string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	// Decoded loosely: one malformed entry of the user's must not hide ours.
	var file struct {
		Hooks map[string]any `json:"hooks"`
	}
	if json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &file) != nil {
		return false
	}
	ours := codexHandler(rexExecutable(), runtimeGOOS)
	for event, groups := range file.Hooks {
		if len(events) > 0 && !slices.Contains(events, event) {
			continue
		}
		list, _ := groups.([]any)
		for _, g := range list {
			handlers, _ := asDict(g)["hooks"].([]any)
			for _, h := range handlers {
				if m, ok := h.(map[string]any); ok && isRexCodexHandler(m, ours) {
					return true
				}
			}
		}
	}
	return false
}

// mapCodexHook maps one Codex hook payload. Codex sends no thinking and no
// mid-turn replies, only Stop's last message, and no exit codes for
// commands, so a command result says "ran", never "passed".
func mapCodexHook(payload map[string]any, name string) *Event {
	tool := strField(payload, "tool_name")
	input := asDict(payload["tool_input"])
	cwd := strField(payload, "cwd")
	event := func(kind, text string, meta map[string]any) *Event {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		ev := NewEvent("codex", kind, text, meta)
		return &ev
	}
	switch name {
	case "UserPromptSubmit":
		return event("user", strings.TrimSpace(strField(payload, "prompt")), nil)
	case "PreToolUse", "PostToolUse":
		post := name == "PostToolUse"
		switch {
		case tool == "apply_patch":
			files := patchFiles(asCommand(input["command"]), cwd)
			if len(files) == 0 {
				return nil
			}
			// meta.paths is capped, so a huge patch's feed line still fits
			// the tail window dedupe and warm start read.
			paths := make([]any, 0, min(len(files), maxPatchPaths))
			for _, f := range files[:min(len(files), maxPatchPaths)] {
				paths = append(paths, f.path)
			}
			meta := toolMeta("apply_patch", files[0].path)
			meta["paths"] = paths
			text := "Edit " + files[0].path
			if post {
				if failed, why := patchFailed(payload["tool_response"]); failed {
					// A patch that didn't apply wrote nothing: no attach,
					// not drift.
					meta["ok"] = false
					text = "apply_patch failed · " + files[0].path
					if why != "" {
						text += ": " + firstLine(why)
					}
					return event("error", text, meta)
				}
				meta["ok"] = true
				text = files[0].verb + " " + files[0].path
			}
			if len(files) > 1 {
				text += fmt.Sprintf(" (+%d more)", len(files)-1)
			}
			kind := "command"
			if post {
				kind = "result"
			}
			return event(kind, text, meta)
		case IsShellTool(tool):
			cmd := strings.TrimSpace(asCommand(input["command"]))
			meta := toolMeta(tool, "")
			meta["command"] = cmd
			if post {
				return event("result", "ran "+cmd, meta)
			}
			return event("command", cmd, meta)
		case tool == "update_plan":
			if post {
				return event("result", tool, toolMeta(tool, ""))
			}
			text := "plan updated"
			steps, _ := input["plan"].([]any)
			for _, s := range steps {
				if st := asDict(s); strField(st, "status") == "in_progress" {
					text = "plan: " + firstLine(strField(st, "step"))
				}
			}
			return event("command", text, toolMeta(tool, ""))
		default:
			if post {
				return event("result", tool, toolMeta(tool, ""))
			}
			// The same short lines as Claude's tools: never a JSON dump.
			return event(toolKind(tool), claudeToolText(tool, payload["tool_input"]), toolMeta(tool, ""))
		}
	case "PermissionRequest":
		text := tool + " wants permission"
		if cmd := strings.TrimSpace(asCommand(input["command"])); cmd != "" && IsShellTool(tool) {
			text += ": " + cmd
		}
		return event("question", text, toolMeta(tool, ""))
	case "Stop":
		if msg := strings.TrimSpace(strField(payload, "last_assistant_message")); msg != "" {
			return event("text", msg, nil)
		}
		return event("idle", "turn completed", nil)
	case "SessionStart":
		if strField(payload, "source") == "compact" {
			return nil // a compaction mid-turn; the turn goes on
		}
		return event("idle", "session started", nil)
	case "SessionEnd":
		return event("idle", "session ended", nil)
	case "Interrupt":
		return event("idle", "turn interrupted", map[string]any{"status": "aborted"})
	}
	return nil
}

type patchFile struct{ verb, path string }

const maxPatchPaths = 200

// patchFailed reads Codex's PostToolUse tool_response for apply_patch.
// Codex hands it over as it formats tool output for the model: either text
// ("Exit code: N\nWall time: …\nOutput:\n…") or JSON ({"output": …,
// "metadata": {"exit_code": N}}). A non-zero exit code, a false success
// flag, or apply_patch's own failure text is a patch that didn't apply,
// and the reason is the output's first line. Anything else is a write.
func patchFailed(resp any) (bool, string) {
	switch r := resp.(type) {
	case string:
		s := strings.TrimSpace(r)
		if strings.HasPrefix(s, "{") {
			var m map[string]any
			if json.Unmarshal([]byte(s), &m) == nil {
				return patchFailed(m)
			}
		}
		if rest, ok := strings.CutPrefix(s, "Exit code:"); ok {
			code, out, _ := strings.Cut(strings.TrimSpace(rest), "\n")
			if _, body, ok := strings.Cut(out, "Output:"); ok {
				out = body
			}
			if strings.TrimSpace(code) != "0" {
				return true, firstLine(strings.TrimSpace(out))
			}
			s = strings.TrimSpace(out)
		}
		return patchFailedText(s)
	case map[string]any:
		out := strings.TrimSpace(strField(r, "output") + " " + strField(r, "error"))
		if code, ok := asDict(r["metadata"])["exit_code"].(float64); ok && code != 0 {
			return true, firstLine(out)
		}
		if ok, has := r["success"].(bool); has && !ok {
			return true, firstLine(out)
		}
		return patchFailedText(out)
	}
	return false, ""
}

// patchFailedText finds apply_patch's own failure lines in its output.
func patchFailedText(s string) (bool, string) {
	for _, line := range strings.Split(s, "\n") {
		l := strings.TrimSpace(line)
		for _, p := range []string{"apply_patch verification failed", "Failed to ", "Invalid patch", "patch rejected"} {
			if strings.HasPrefix(l, p) {
				return true, l
			}
		}
	}
	return false, ""
}

// patchFiles reads the file headers of an apply_patch body, and never its
// contents: "*** Add File:" and "*** Update File:" are writes, "*** Delete
// File:" a delete, "*** Move to:" the new name. Only a line that starts
// with "*** " is a header; a hunk's context lines start with a space.
// Relative paths are taken from the session's cwd.
func patchFiles(patch, cwd string) []patchFile {
	var out []patchFile
	seen := map[string]bool{}
	for _, line := range strings.Split(patch, "\n") {
		if !strings.HasPrefix(line, "*** ") {
			continue
		}
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		verb := ""
		for prefix, v := range map[string]string{"*** Add File:": "wrote", "*** Update File:": "wrote", "*** Delete File:": "deleted", "*** Move to:": "wrote"} {
			if strings.HasPrefix(line, prefix) {
				verb, line = v, strings.TrimSpace(strings.TrimPrefix(line, prefix))
				break
			}
		}
		if verb == "" || line == "" {
			continue
		}
		p := line
		if !filepath.IsAbs(p) && cwd != "" {
			p = filepath.Join(cwd, p)
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, patchFile{verb: verb, path: p})
	}
	return out
}

func init() {
	hooksFile := func(home string) string { return filepath.Join(codexDir(home), "hooks.json") }
	registerHarness(Harness{
		Name:    "codex",
		Label:   "Codex",
		Config:  hooksFile,
		Present: func(home string) bool { return dirExists(codexDir(home)) },
		Wired:   func(home string) bool { return codexWired(hooksFile(home)) },
		Prompts: func(home, _ string) bool { return codexWired(hooksFile(home), "UserPromptSubmit") },
		// Codex runs a hook only once the person trusts it in /hooks, and
		// Rex can't see whether they did.
		Caveat: "once trusted in Codex's /hooks",
		Install: func(home string, out io.Writer) error {
			if !dirExists(codexDir(home)) {
				fmt.Fprintf(out, "codex: Codex isn't set up here (no %s); nothing changed\n", codexDir(home))
				return nil
			}
			return installCodex(hooksFile(home), out)
		},
		Uninstall: func(home string, out io.Writer) error { return uninstallCodex(hooksFile(home), out) },
	})
}
