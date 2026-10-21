# REX — for agents and contributors

Terminal sidecar with a pixel-art dinosaur. Mascot on the left, ingestible agent tape on the right. Watch-only: it does not run, approve, or steer the coding agent.

`rex start` works from any directory. The feed is `~/.rex/session.jsonl` (override with `REX_EVENTS`).

Install and user docs live in `README.md`. This file is how to change the code.

## Do not

- Add palettes or a `--color` flag. Yellow from `assets/idle.png` is the only look. (On Terminal.app without truecolor the *same* palette is quantized to xterm-256 — `mascot.DetectDepth`; that is not a second palette.)
- Add dependencies, frameworks, or a config system without a concrete need. Go 1.22+ stdlib plus `golang.org/x/sys` and `golang.org/x/term` for the TTY.
- Scrape transcripts or use the Cursor SDK to watch the in-IDE agent. Hooks only.
- Steer, inject prompts, or auto-approve tools unless the human asked for that this turn.
- Invent a custom `.rex` language. Plan files are JSON named `<name>.rex.json` under `rex/plans/` (commit that folder). Do not load them into the agent this turn. Don't `go build -o rex` at the repo root — that path is the plans directory.
- Dump Write bodies, duplicate thoughts, or flatten markdown into one blob in the pane.
- Giant ASCII, extra emoji, or a web UI squeezed into the terminal.
- Wire a harness without an explicit `rex ingest --install <name>`. Wired today: Cursor (the install script does it), and opt-in Claude Code, Codex and OpenCode (a generated `plugins/rex.js` that only pipes events to `rex ingest --source opencode`, sends allow-listed fields only (no file bodies, diffs, attachments or tool output), skips a subagent's prompts, replies, thoughts, errors and turn ends (its tool calls still show), sends the turn's last text with its idle, never throws and never waits). Codex's `hooks.json` may only hold `description` and `hooks`, and Codex trusts a hook by a hash of its handler, so the entry must stay byte-stable; the user trusts it once in Codex's `/hooks`.
- Add plan fields beyond [docs/plan-format.md](docs/plan-format.md) v1 or extra mascot poses unless asked this turn. `internal/plan` parses plans leniently (Problems, not errors); keep it that way.

Idle and future poses are **40×40**. Ctrl+C twice to quit. Respect `NO_COLOR`. macOS, Linux, and Windows 10+ (VT).

## Repo map

| Area | Path |
| --- | --- |
| CLI | `internal/cli/` · `cmd/rex` |
| Mascot | `internal/mascot/` |
| Live UI / keys | `internal/tui/` |
| Event feed | `internal/watch/` |
| Plan files | `internal/plan/` · format in `docs/plan-format.md` |
| Agent skill | `internal/skill/rex-plan/SKILL.md` (embedded; installed only by `rex ingest --install-skill [--project]`) |
| Idle PNG | `assets/idle.png` |
| Breakdance sheet | `assets/breakdance.png` (40×40 cells) |
| Typing sheet | `assets/typing.png` (40×40 cells) |
| README art | `assets/rex.png` (scaled idle) |
| Importer | `cmd/importsprite` |
| Tests | `*_test.go` next to the package |
| UX spec | `docs/ux.md` (target CUJ, keys, friction register) |

## Mascot

Keep rendering separate from watch/TUI logic.

To change idle: replace `assets/idle.png`, then:

```bash
go run ./cmd/importsprite assets/idle.png --apply
```

That writes the idle `Pixels(...)` grid and the yellow palette 1:1. **Leave blink, glance, and squat as transforms** on `IdleRows` (`PaintPixels` / `ShiftBody`). Do not hand-edit a second grid that drifts from the PNG.

Blink drops the top eye pixel (the face `k` at column 25, rows 13–14 on the current sprite). Squat is `ShiftBody(..., dy=1, keepBottom=7)` so the shadow stays planted.

Breakdance is the 40×40 sheet `assets/breakdance.png` (left-to-right, top-to-bottom). Type `breakdance` on the tape (`/breakdance` in the Library) to play it **once**, then resume the turn pose. Typing is `assets/typing.png`. Hook `user` / `thought` → thinking; shell / tools → running; `text` / `idle` (turn end) → idle; `error` → idle, except a failed tool call (meta `tool`), which keeps the turn running. Thinking and running play **typing**; idle is only when nothing is in flight. Local composer does not start or end a turn. Map sheet pixels onto the yellow palette — do not blit the PNG. Do not dump Write bodies.

If the README hero should match, rebuild `assets/rex.png` from `idle.png` with nearest-neighbor scale.

Animation must stay slow. Never let it eat input or overwrite the log. The live loop never clears the screen per frame: `renderer` repaints only changed rows (each erased from column 1, then written whole and reset, never erased after the write, since that would wipe the last column; inside DEC 2026 sync, cursor hidden while painting) and does a full repaint only on the first frame, resize, Ctrl+L and SIGCONT. `TERM=dumb` gets the static card without color. Rendered mascot rows and the tape layout are cached.

Unix live mode is cbreak (ECHO/ICANON/ISIG/IEXTEN off, IXON off so Ctrl+S can't freeze output) and **must keep OPOST**. Do not use `term.MakeRaw`. On Unix, input goes through `keyDecoder`: whole escape sequences, whole UTF-8 runes, Alt chords dropped (never Esc plus a key), bracketed paste as one token that never submits, even when it stalls. On Windows, `consoleKeys` maps key records (Esc, surrogate pairs), a burst with text after a newline (or 16+ characters) becomes one paste on a best-effort basis, bracketed paste stays off (the console has no markers), and `ENABLE_PROCESSED_INPUT` / VT input are cleared so Ctrl+C is a key. Keys from one read are applied one at a time (`app.keys`); the mascot ticks on the clock, not on empty reads. SIGTERM/SIGHUP/interrupt leave through the same restore path as Ctrl+C twice.

## Watch feed

Cursor hooks (`~/.cursor/hooks.json`) spawn `rex ingest --source cursor`; Claude Code hooks (`~/.claude/settings.json`, or the repo's git-excluded `.claude/settings.local.json` with `--project`) spawn `rex ingest --source claude`; Codex hooks (`$CODEX_HOME/hooks.json`, default `~/.codex`) spawn `rex ingest --source codex`; OpenCode's generated plugin (`$XDG_CONFIG_HOME/opencode/plugins/rex.js`, default `~/.config/opencode`) spawns `rex ingest --source opencode`, and sends a turn's reply with its `session.idle`. Adapters map vendor JSON to `watch.Event`; `AppendEvent` dedupes and only ever appends (a growing reply is a new line; `ExtendLog` merges it); `NewEventTail(path, true)` paints new lines, and on start `warmStart` paints the last ~20 events of this repo under `── before rex started ──` (restoring the turn only if the newest is under 2 minutes old). Ingest stamps `meta.cwd` / `roots` / `session` / `tool_use_id` from the payload (never the hook's own cwd); `watch.Scope` decides whether an event is this repo's from its cwd or roots (its file only when it has neither) and shows tool paths repo-relative; the TUI hides other repos' events (counted under Help), keeps events with no known origin, and drops feed lines that claim to be Rex's own blocks.

`rex ingest` runs inside agent hooks: run from a hook it **always exits 0 and never writes stdout** (harnesses read exit 2 as "block" and some feed stdout to the agent); only `-h`, `--version` and the interactive `--install*` / `--uninstall` flags print, and only those can exit non-zero (1, never 2). An unknown top-level command exits 1, never 2. Stdin reads and the feed lock are time-bounded. The feed is private (file 0600, `~/.rex` 0700, tightened on old installs). Keep it that way. Visible assistant replies come from `afterAgentResponse`, which Cursor currently fires with the **final** message of the turn. In-between chat lines are not on any hook — do not scrape transcripts to invent them. Thoughts still arrive via `afterAgentThought`. Do not map Cursor's `stop` as text. Claude Code's `Stop` carries `last_assistant_message`, the turn's final reply: that one is text. Claude hook payloads that carry `cursor_version` are Cursor running Claude's hooks and are dropped (Cursor's own hooks already report them).

New harness = adapter + a `Harness` registered in `internal/watch/harness.go` (Present / Wired / Install / Uninstall). Installing happens only on an explicit `rex ingest --install <name|auto>`; `--uninstall <name>` takes Rex's entries back out and leaves everything else. Do not special-case vendors in `live.go`. Tests run with HOME pointed at a temp dir (a `TestMain` in each package that touches HOME: `cli`, `plan`, `skill`, `tui`, `watch`), with `REX_EVENTS` unset. Installers edit the harness's config through `watch.UpdateJSONFile` + `internal/jsonedit`: splice only what Rex owns, keep every other byte, refuse a file that does not parse, keep a `.bak`, write atomically. Never round-trip a user's config through a `map`.

Kinds: `thought`, `text`, `user`, `command`, `question`, `result`, `idle`, `error`. Rex's own local kinds, `note`, `plan`, `done`, `sendoff`, `brief`, `drift` and `before` (source `rex`), never come from the feed.

## Plan files

`internal/plan` reads the v1 format in `docs/plan-format.md`. Rex writes a plan only when the human acts in Rex, and only through `plan.Edit`: a task's `status` and completion fields (`accomplished`, `modified`, `verified_how`; `completed_notes` is the agent's or a human's), spliced into that task's own object with `internal/jsonedit`, and the plan's own `status` (active when a task is pinned, done when every leaf task is done or skipped). Task ids match as `Parse` reads them (trimmed). Every write re-reads the file, re-runs the `Op` against what is on disk (abort if the task is gone or its status moved), checks the result against the expected tree, refuses a file that is read-only on disk, and renames a temp file into place (in-place only on Windows, after one more check). Never marshal a plan back out of a struct or a `map`. What Rex remembers between runs (last plan per repo, pin start times) goes in `~/.rex/state.json`, never in the repo. It also reads the optional stack fields `base` and `branch` (docs/plan-format.md, Stacks); agents write them and Rex never does. Rex never runs git: the send-off's Stack part only names the pin's branch and base, worked out from file order and status.

## TUI

The target loop, key map and screen states are in `docs/ux.md`. TUI changes cite the section they implement.

- Left ~40 columns: mascot (at 66+ columns; narrower terminals are text-only, under 24 text columns Rex shows "widen" and ignores every key but Ctrl+C and Ctrl+L; under 7 rows nothing edits or submits the hidden draft; see `layoutFor`). Right: labeled log blocks (`thinking`, `run`, `ask`, `reply`, plus Rex's own `note · local`, which never comes from or goes to the feed). Commands yellow, replies bright, thoughts muted. Skip file-look commands and `done`/result echoes. Each assistant `text` event is its own reply block.
- The header (row 0) on **every** screen shows `REX plan|tape`, the turn state **and** the open plan as a repo-relative path (`thinking · rex/plans/foo.rex.json`), shortened only when it doesn't fit: drop `rex/plans/`, then `.rex.json`, then clip with `…` from the left (docs/ux.md §4). Row 1 is the status line. The footer is ranked segments that drop whole, never mid-word.
- User prompts: no `YOU` rule. Invisible column, at most half the pane, **right-aligned as a box**, **left-aligned text inside**. Color `UI["you"]`.
- Markdown → headings, lists, stripped emphasis, fenced code as `│ ` lines.
- Start opens the **plans** home: Continue (the open plan, else the last one opened in this repo, from `~/.rex/state.json`), Create, Open, Open latest (cards with `2/5 · ▸ id` progress), then recents; on first run only Create. The cursor starts on Continue, else Open latest. Start never opens a plan by itself. Typing on Home finds across `rex/plans/**` and ends the list with `[ + Create 'x' ]` (⏎ creates it at once). Open browses `rex/plans/` like a file picker: cyan `▌` selection, orange folders, white plans, then green `[ + New folder ]` / `[ + New plan ]` below the listing; the cursor starts on the first folder or plan and a filter selects its first match. Names may be `a/b` (plan b in folder a). Selection follows the plan, not the row index, and long lists scroll with a two-line margin and `↑ more` / `↓ more`. Duplicate names warn and do not overwrite. Starting Rex never deletes or creates files: `rex/plans/` (with `.gitkeep`) is made on the first Create or New folder; a file (or dangling link) named `rex` at the root turns plans off with a notice instead of being removed; outside git, Home says where plans would go; an old `rex-plans/` folder is only reported, never moved.
- Opening or creating a plan lands on its **outline** (`outline.go`): tasks in file order, `parent` as indent, glyphs `▸ ○ · ✓ – !`, the effective pin selected, ⏎ / → for plan or task detail, Esc back up to the Library. The open plan is re-read every 500 ms and compared by hash: outside edits keep the selection by task id, toast on row 1 and leave one `plan` line on the tape; invalid JSON shows only after two polls and keeps the last good copy; Rex's own writes (`planDoc.own`) are taken in quietly. Reading a plan never writes it.
- **The pin** is `plan.Graph.Pin()`: the first active leaf, else the first ready leaf. `p⏎` pins the selection (on the tape, next up); `pin <id>⏎` any task. It writes `active` on the target and `pending` on other active leaves through `plan.PinOp`, never a parent's status (a parent marked active is only a label and never counts as a second pin), and asks first (Pin confirm, `p⏎` again) when the task is blocked, waits on unfinished work, or moves the pin. The confirm belongs to the plan it asked about, and `PinOp` refuses if another task became active since the warning was shown. A done task is reopened with its retrospective kept. The start time goes in `~/.rex/state.json`, never in the plan. Nothing is auto-pinned.
- **Done** (`d⏎`) opens the completion form for the effective pin (docs/ux.md §8). `did ›` is required; the agent's `accomplished:` reply trailer seen since the pin started is a dim placeholder that ⏎ accepts (a reply about another task's send-off never counts). `verified ›` takes the `verified:` trailer, else a seen test command marked `(passed)`, `(failed)` or `(result not seen)`. Files are read-only: completed edits in this repo since the pin, never `rex/plans/**`, always "may be partial". `plan.CompleteOp` writes `done` plus only the fields that changed (an untouched value from the file keeps its bytes; `modified` is merged), and the plan becomes done once every leaf is done or skipped. A task edited on disk while the form is open needs a second ⏎. Nothing is auto-pinned after.
- **Send-off** (`s⏎` / `sendoff⏎` / `handoff⏎`) is `plan.Sendoff`: generated only from the open JSON (issue #9's template, a Done section, a request to review the pin's PR read-only by role before opening or updating it, for a stacked plan a `Stack:` part saying where that PR goes, the reply-trailer request, the fingerprint `rex-sendoff: <path>#<task>`), cleaned, under 4 KB. It is shown and copied for the human to paste; Rex never sends, injects or saves it. `b⏎` is the planning brief (`plan.Brief`). `o⏎` prints the brief plain on the normal screen. A prompt that comes back with the fingerprint collapses to one `sendoff` line on the tape. Copy status is honest: `clipboard` (`clipboard.go`) uses `pbcopy`, `clip.exe` (UTF-16LE + BOM), `wl-copy` or `xclip`/`xsel`, off the UI goroutine with a 2 s deadline, and says `copied (<tool>)` only when the tool exited 0; over SSH or with no tool it writes OSC 52 between frames and says `sent (OSC 52) · may be blocked`.
- **Drift** (`plan.CheckEdit` / `plan.CheckPrompt`, `drift.go`) judges live completed edits and user prompts in this repo against the effective pin: none for a listed file, glob, folder or `_test` sibling; soft for a file next to a listed one, a file another task lists, a prompt naming another task, or a long prompt that shares no words with the pin; hard for anything that hits `scope.out` or an edit outside the pin's folders (with no files listed, only `scope.out` counts). Reads, `rex/plans/**`, other repos, denied writes and pre-start history never count. It is display only: row 1 and one `drift · <pin>` block per pin on the tape. Never in the feed, the plan, stdout, or anything sent to the agent.
- Two places, PLAN and TAPE; **Tab** toggles them and keeps each place's screen, draft, scroll and overlay. **Nothing fires without ⏎**: typed words are verbs (`?`/`help`, `breakdance`, `pin`/`p`, `done`/`d`, `sendoff`/`s`, `brief`/`b`); anything else on the tape is a local `note` (muted, in memory only, never written to the feed, never a turn change). In the Library, text is a find/filter/name unless it starts with `/` (a lone `?` is help everywhere). **Esc** clears the draft, then climbs one level, never switches place; Backspace only deletes; Ctrl+U/Ctrl+W clear line/word; the first Ctrl+C clears the draft and arms quit.
- Tape keys: ↑↓, page, Home / Ctrl+A (oldest), End / Ctrl+E (live). Composer does not send to the agent. Prompt boxes cap at 6 rows.
- Fit text by terminal **cells**: `cellLen` / `clip` / `padRight` / `wrap` (East Asian wide and emoji = 2, combining = 0, ambiguous = 1; hand-kept table in `width.go`). `clip` is the choke point: it also runs `watch.CleanText`, so agent or plan text can never paint escape sequences. Crop from the bottom; keep the mascot top-left.

## Commands and tests

```bash
go test ./...
go run ./cmd/rex start
```

CI: GitHub Actions, Ubuntu/macOS/Windows, Go 1.22 and 1.24: `go vet`, `go test`, then `go build -o dist/rex ./cmd/rex` (never `-o rex`; `rex/` is the plans directory). A separate job on stable Go runs gofmt and `go vet` for darwin, linux and windows, whose stdversion check catches APIs newer than go.mod's 1.22.

`internal/tui/cuj_test.go` holds the whole journey (create → open → watch → done → pin → send off → drift → quit) against the keystroke budget in docs/ux.md §14, and a frame matrix: every screen at 24/80/120 columns × 7/12/24 rows must fit, carry no control bytes, and show the header and a footer. A new screen or overlay joins the matrix. The CUJ fixture is `internal/tui/testdata/sendoff-demo.rex.json`, a copy of the human demo `rex/plans/cuj/sendoff-demo.rex.json` (pressing `d⏎` on the demo must not break the tests).

After sprite or TUI changes, restart a running `rex start`. Tests do not replace looking at the terminal.

## Code conventions

- Match the code around you. Comments are rare and say *why*. Names are short (`b` for a browser, `ev` for an event, `opt` for compose options).
- Tests go in a `*_test.go` file in the same package. Use `t.TempDir()` for any repo, plans dir or feed file, and never touch the real `~` or `~/.rex`. Assert on rendered text with `viewText(BuildLogView(...))` / `viewText(BuildPlanView(...))`, and use `NO_COLOR`-style `Enabled: false` frames when you need plain text.
- `LogLine.Kind` picks the color. It is looked up in `mascot.UI`, then in `kindColor`. A new UI role goes in `mascot.UI`, not in a new palette.
- Pane width math uses `cellLen` / `clip` / `padRight`, never `len()` or rune counts (`runeLen` is only for the draft cap).
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

- **Before opening a PR,** run the gate and a review pass. Save the gate as `gate.sh` at the repo root (git ignores it) and run it with `bash gate.sh <base-branch>` (don't paste it into your login shell: it stops at the first failure):

```bash
set -euo pipefail
base=${1:?usage: bash gate.sh <base-branch>}
out=$(gofmt -l .); [ -z "$out" ] || { echo "gofmt needed: $out"; exit 1; }
for os in darwin linux windows; do GOOS=$os go vet ./...; done
staticcheck ./...
go test -race ./...
GOTOOLCHAIN=local go1.22.12 test ./...      # go install golang.org/dl/go1.22.12@latest && go1.22.12 download
git diff --exit-code "$base" -- go.mod go.sum  # no new deps, no toolchain line
echo "gate ok"
```

  staticcheck: `go install honnef.co/go/tools/cmd/staticcheck@2026.2.2`.

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
