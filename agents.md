# REX — for agents and contributors

Terminal sidecar with a pixel-art dinosaur. Mascot on the left, ingestible agent tape on the right. Watch-only: it does not run, approve, or steer the coding agent.

`rex start` works from any directory. The feed is `~/.rex/session.jsonl` (override with `REX_EVENTS`).

Install and user docs live in `README.md`. This file is how to change the code.

## Do not

- Add palettes or a `--color` flag. Yellow from `assets/idle.png` is the only look.
- Add dependencies, frameworks, or a config system without a concrete need. Go 1.22+ stdlib plus `golang.org/x/sys` and `golang.org/x/term` for the TTY.
- Scrape transcripts or use the Cursor SDK to watch the in-IDE agent. Hooks only.
- Steer, inject prompts, or auto-approve tools unless the human asked for that this turn.
- Invent a custom `.rex` language. Plan files are JSON named `<name>.rex.json` under `rex/plans/` (commit that folder). Do not load them into the agent this turn. Don't `go build -o rex` at the repo root — that path is the plans directory.
- Dump Write bodies, duplicate thoughts, or flatten markdown into one blob in the pane.
- Giant ASCII, extra emoji, or a web UI squeezed into the terminal.
- Assume Claude Code or Codex are wired. Mappers exist; only Cursor hooks are installed.
- Build plan JSON schema, drift detection, or extra mascot poses unless asked this turn.

Idle and future poses are **40×40**. Ctrl+C twice to quit. Respect `NO_COLOR`. macOS, Linux, and Windows 10+ (VT).

## Repo map

| Area | Path |
| --- | --- |
| CLI | `internal/cli/` · `cmd/rex` |
| Mascot | `internal/mascot/` |
| Live UI / keys | `internal/tui/` |
| Event feed | `internal/watch/` |
| Idle PNG | `assets/idle.png` |
| Breakdance sheet | `assets/breakdance.png` (40×40 cells) |
| Typing sheet | `assets/typing.png` (40×40 cells) |
| README art | `assets/rex.png` (scaled idle) |
| Importer | `cmd/importsprite` |
| Tests | `*_test.go` next to the package |

## Mascot

Keep rendering separate from watch/TUI logic.

To change idle: replace `assets/idle.png`, then:

```bash
go run ./cmd/importsprite assets/idle.png --apply
```

That writes the idle `Pixels(...)` grid and the yellow palette 1:1. **Leave blink, glance, and squat as transforms** on `IdleRows` (`PaintPixels` / `ShiftBody`). Do not hand-edit a second grid that drifts from the PNG.

Blink drops the top eye pixel (the face `k` at column 25, rows 13–14 on the current sprite). Squat is `ShiftBody(..., dy=1, keepBottom=7)` so the shadow stays planted.

Breakdance is the 40×40 sheet `assets/breakdance.png` (left-to-right, top-to-bottom). Type `breakdance` in the composer to play it **once**, then resume the turn pose. Typing is `assets/typing.png`. Hook `user` / `thought` → thinking; shell / tools → running; `text` / `error` → idle. Thinking and running play **typing**; idle is only when nothing is in flight. Local composer does not start or end a turn. Map sheet pixels onto the yellow palette — do not blit the PNG. Do not dump Write bodies.

If the README hero should match, rebuild `assets/rex.png` from `idle.png` with nearest-neighbor scale.

Animation must stay slow. Never let it eat input or overwrite the log.

Unix live mode is cbreak (ECHO/ICANON/ISIG off) and **must keep OPOST**. Do not use `term.MakeRaw`.

## Watch feed

Cursor hooks (`~/.cursor/hooks.json`) spawn `rex ingest --source cursor`. Adapters map vendor JSON to `watch.Event`; `AppendEvent` dedupes; `NewEventTail(path, true)` paints new lines. Visible assistant replies come from `afterAgentResponse`, which Cursor currently fires with the **final** message of the turn. In-between chat lines are not on any hook — do not scrape transcripts to invent them. Thoughts still arrive via `afterAgentThought`. Do not map `stop` as text.

New harness = adapter (+ installer). Do not special-case vendors in `live.go`.

Kinds: `thought`, `text`, `user`, `command`, `question`, `result`, `idle`, `error`.

## TUI

- Left ~40 columns: mascot. Right: labeled log blocks (`thinking`, `run`, `ask`, `reply`). Commands yellow, replies bright, thoughts muted. Skip file-look commands and `done`/result echoes. Each assistant `text` event is its own reply block.
- Chat header always shows turn state **and** the open plan as a repo-relative path (`thinking · rex/plans/foo.rex.json`).
- User prompts: no `YOU` rule. Invisible column, at most half the pane, **right-aligned as a box**, **left-aligned text inside**. Color `UI["you"]`.
- Markdown → headings, lists, stripped emphasis, fenced code as `│ ` lines.
- Start opens the **plans** home: Create, Open, Open latest (cards), then recents. Open browses `rex/plans/` like a file picker: cyan `▌` selection, orange folders, green `[ + New folder ]` / `[ + New plan ]`, white plans. Duplicate names warn and do not overwrite. Tab switches to chat. Opening a plan is a no-op load.
- Chat keys: ↑↓, page, Home / Ctrl+A (oldest), End / Ctrl+E (live). Composer does not send to the agent.
- Clip ANSI by cells (runes), not `len()` bytes. Crop from the bottom; keep the mascot top-left.

## Commands and tests

```bash
go test ./...
go run ./cmd/rex start
```

CI: GitHub Actions, Ubuntu/macOS/Windows, Go 1.22 and 1.24.

After sprite or TUI changes, restart a running `rex start`. Tests do not replace looking at the terminal.

## Code conventions

- Match the code around you. Comments are rare and say *why*. Names are short (`b` for a browser, `ev` for an event, `opt` for compose options).
- Tests go in a `*_test.go` file in the same package. Use `t.TempDir()` for any repo, plans dir or feed file, and never touch the real `~` or `~/.rex`. Assert on rendered text with `viewText(BuildLogView(...))` / `viewText(BuildPlanView(...))`, and use `NO_COLOR`-style `Enabled: false` frames when you need plain text.
- `LogLine.Kind` picks the color. It is looked up in `mascot.UI`, then in `kindColor`. A new UI role goes in `mascot.UI`, not in a new palette.
- Pane width math uses `runeLen` / `clip` / `padRight`, never `len()`.
- Vendor names live in `internal/watch` (adapters, installers), in the `rex ingest` flags, `--source` list and help in `internal/cli`, and in `scripts/install.*`. Adding a harness touches all three. The TUI paints event kinds and never branches on a vendor.
- Keep stdout clean in `rex ingest`. Hooks treat stdout as agent input, and exit code 2 as "block".

### Go 1.22 floor

`go.mod` says `go 1.22` and CI runs Go 1.22. Code built with a newer toolchain can still break the 1.22 floor:

- **The compiler** rejects newer language features, such as range-over-func, because `go.mod` sets `-lang`.
- **`go vet`** on Go 1.23+ flags any standard-library API newer than `go.mod` through its `stdversion` check. `go test` runs that check only on the newest toolchains (not on 1.23 or 1.24), so always run `go vet` explicitly, with a new toolchain. Examples it catches:
  - 1.23: `iter`, `maps.Keys/Values`, `slices.Collect/Sorted/Chunk`, `unique`
  - 1.24: `os.Root`, `t.Chdir`, `t.Context`, `b.Loop`, `strings.Lines/SplitSeq/FieldsSeq`, `bytes.Lines`
  - 1.25+: `testing/synctest`, `sync.WaitGroup.Go`, `t.Output`, `errors.AsType` (1.26), `encoding/json/v2` (1.27)
- **Nothing catches struct-tag options.** `json:",omitzero"` (1.24) compiles on 1.22 and is ignored there, so the same code writes different JSON depending on the toolchain. Don't use it. Rex writes the feed and plan files as JSON.

`min`/`max`, `clear`, range over an int, and the 1.21 `slices`/`maps` functions are fine.

## Workflow

- **Branches.** Branch from `dev`. When a change depends on unmerged work, stack it: cut the branch from the previous one and open the PR against it. Name branches after the issue: `feature/task_completion`, `fix/key_input`, `docs/ux_spec`.
- **One branch, one issue.** Each branch closes exactly one issue. Work with no issue gets one first.
- **PR body.** Include the problem, what changed, why it is needed, how it works, the tests run (with output), terminal captures for TUI changes, and any existing behavior or test that changed on purpose. Stacked PRs say what they sit on: merge them bottom-up and don't squash.
- **Fixing a lower PR in a stack.** Add a commit to that branch, never amend. Then rebase everything above it and push each moved branch (`--update-refs` needs Git 2.38+):

```bash
git checkout <top-of-stack>
git rebase --update-refs <fixed-branch>   # moves every branch in between too
git push --force-with-lease origin <each moved branch>
```

- **Before opening a PR,** run the gate and a review pass. Save the gate as a script and run it with `bash gate.sh <base-branch>` (don't paste it into your login shell: it stops at the first failure):

```bash
set -euo pipefail
out=$(gofmt -l .); [ -z "$out" ] || { echo "gofmt needed: $out"; exit 1; }
for os in darwin linux windows; do GOOS=$os go vet ./...; done
staticcheck ./...                           # go install honnef.co/go/tools/cmd/staticcheck@2026.2.2
go test -race ./...
GOTOOLCHAIN=local go1.22.12 test ./...      # go install golang.org/dl/go1.22.12@latest && go1.22.12 download
git diff --exit-code "$1" -- go.mod go.sum  # no new deps, no toolchain line
echo "gate ok"
```

  `dev` has two staticcheck findings until #19 lands; until then the staticcheck step means "no new findings compared with the base branch".

- **Review pass.** Read-only reviewers, each with one job:
  - critique the design and how it interacts with the system;
  - drive the TUI in tmux on its own server (`tmux -L <name>`, never the default one), only when the UI changed;
  - try to break it and check for regressions;
  - lint and format;
  - a final readiness check after fixes.

  Fix each finding, or say in the PR why it was skipped.

## Definition of done

- Follows this file and any PNG the human attached.
- The pre-PR gate in Workflow passes.
- Terminal state restores on quit / Ctrl+C.
- No speculative product, extra commands, or architecture for a feature that was not requested.
