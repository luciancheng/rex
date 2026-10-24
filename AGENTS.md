# REX — for agents and contributors

Terminal sidecar with a pixel-art dinosaur. Mascot on the left, what the agent is doing on the right. Watch-only: it does not run, approve, or steer the coding agent.

`rex start` works from any directory. The feed is `~/.rex/session.jsonl` (override with `REX_EVENTS`).

Install and user docs live in `README.md`. This file is how to change the code.

## Do not

- Add palettes or a `--color` flag. Yellow from `assets/idle.png` is the only look. (On Terminal.app without truecolor the *same* palette is quantized to xterm-256 — `mascot.DetectDepth`; that is not a second palette.)
- Add dependencies, frameworks, or a config system without a concrete need. Go 1.22+ stdlib plus `golang.org/x/sys` and `golang.org/x/term` for the TTY.
- Scrape transcripts or use the Cursor SDK to watch the in-IDE agent. Hooks only.
- Steer, inject prompts, or auto-approve tools unless the human asked for that this turn. Hooks never write stdout or inject anything, not even the pinned task: the agent learns the pin by re-reading the plan file, as the `rex-plan` skill teaches.
- Give Rex actions on a plan again (pin, done, send-off, brief, notes, a clipboard), or make the human copy anything from Rex into an agent. The agent drives the plan; Rex only shows it.
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
| Byte-preserving JSON edits | `internal/jsonedit/` (installers, and the plan reader's positions) |
| Agent skill | `internal/skill/rex-plan/SKILL.md` (embedded; installed only by `rex ingest --install-skill [--project]`, to both `.claude/skills` and `.agents/skills`, plus Rex's own old copy in `~/.claude/skills` when `$CLAUDE_CONFIG_DIR` moves Claude Code's: `skill.Legacy`) |
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
go run ./cmd/importsprite --apply assets/idle.png
```

That writes the idle `Pixels(...)` grid into `internal/mascot/sprites.go` and the sampled RGB values into the yellow palette, 1:1; restart `rex start` after it. The grid's letters are palette roles; the current idle grid uses these (the full set, with eye, white, mark, green, red and teal, is `mascot.PixelRoles`):

| Letter | Role | Letter | Role |
| --- | --- | --- | --- |
| `k` | outline | `n` | mid |
| `y` | body | `a` | accent |
| `Y` | highlight | `b` | belly |
| `o` | shade | `p` | blush |
| `d` | dark | `s` | shadow |

**Leave blink, glance, and squat as transforms** on `IdleRows` (`PaintPixels` / `ShiftBody`). Do not hand-edit a second grid that drifts from the PNG.

Blink drops the top eye pixel (the face `k` at column 25, rows 13–14 on the current sprite). Squat is `ShiftBody(..., dy=1, keepBottom=7)` so the shadow stays planted.

Breakdance is the 40×40 sheet `assets/breakdance.png` (left-to-right, top-to-bottom). Type `/breakdance` in the plan list's composer to play it **once**, then resume the turn pose. Typing is `assets/typing.png`. Hook `user` / `thought` → thinking; shell / tools → running; `text` / `idle` (turn end) → idle; `error` → idle, except a failed tool call (meta `tool`), which keeps the turn running. Thinking and running play **typing**; idle is only when nothing is in flight. Rex's own lines never start or end a turn. Map sheet pixels onto the yellow palette — do not blit the PNG. Do not dump Write bodies.

If the README hero should match, rebuild `assets/rex.png` from `idle.png` with nearest-neighbor scale.

Animation must stay slow. Never let it eat input or overwrite the log. The live loop never clears the screen per frame: `renderer` repaints only changed rows (each erased from column 1, then written whole and reset, never erased after the write, since that would wipe the last column; inside DEC 2026 sync, cursor hidden while painting) and does a full repaint only on the first frame, resize, Ctrl+L and SIGCONT. `TERM=dumb` gets the static card without color. Rendered mascot rows and the AGENT layout are cached.

Unix live mode is cbreak (ECHO/ICANON/ISIG/IEXTEN off, IXON off so Ctrl+S can't freeze output) and **must keep OPOST**. Do not use `term.MakeRaw`. On Unix, input goes through `keyDecoder`: whole escape sequences, whole UTF-8 runes, Alt chords dropped (never Esc plus a key), bracketed paste as one token that never submits, even when it stalls. On Windows, `consoleKeys` maps key records (Esc, surrogate pairs), a burst with text after a newline (or 16+ characters) becomes one paste on a best-effort basis, bracketed paste stays off (the console has no markers), and `ENABLE_PROCESSED_INPUT` / VT input are cleared so Ctrl+C is a key. Keys from one read are applied one at a time (`app.keys`); the mascot ticks on the clock, not on empty reads. SIGTERM/SIGHUP/interrupt leave through the same restore path as Ctrl+C twice.

## Watch feed

Cursor hooks (`~/.cursor/hooks.json`) spawn `rex ingest --source cursor`; Claude Code hooks (`~/.claude/settings.json`, or the repo's git-excluded `.claude/settings.local.json` with `--project`) spawn `rex ingest --source claude`; Codex hooks (`$CODEX_HOME/hooks.json`, default `~/.codex`) spawn `rex ingest --source codex`; OpenCode's generated plugin (`$XDG_CONFIG_HOME/opencode/plugins/rex.js`, default `~/.config/opencode`) spawns `rex ingest --source opencode`, and sends a turn's reply with its `session.idle`. Adapters map vendor JSON to `watch.Event`; `AppendEvent` dedupes and only ever appends (a growing reply is a new line; `ExtendLog` merges it); `NewEventTail(path, true)` paints new lines, and on start `warmStart` paints the last ~20 events of this repo under `── before rex started ──` (restoring the turn only if the newest is under 2 minutes old). Ingest stamps `meta.cwd` / `roots` / `session` / `tool_use_id` from the payload (never the hook's own cwd); `watch.Scope` decides whether an event is this repo's from its cwd or roots (its file only when it has neither) and shows tool paths repo-relative; the TUI hides other repos' events (counted under Help), keeps events with no known origin, and drops feed lines that claim to be Rex's own blocks.

`rex ingest` runs inside agent hooks: run from a hook it **always exits 0 and never writes stdout** (harnesses read exit 2 as "block" and some feed stdout to the agent); only `-h`, `--version` and the interactive `--install*` / `--uninstall` flags print, and only those can exit non-zero (1, never 2). An unknown top-level command exits 1, never 2. Stdin reads and the feed lock are time-bounded. The feed is private (file 0600, `~/.rex` 0700, tightened on old installs). Keep it that way. Visible assistant replies come from `afterAgentResponse`, which Cursor currently fires with the **final** message of the turn. In-between chat lines are not on any hook — do not scrape transcripts to invent them. Thoughts still arrive via `afterAgentThought`. Do not map Cursor's `stop` as text. Claude Code's `Stop` carries `last_assistant_message`, the turn's final reply: that one is text. Claude hook payloads that carry `cursor_version` are Cursor running Claude's hooks and are dropped (Cursor's own hooks already report them).

New harness = adapter + a `Harness` registered in `internal/watch/harness.go` (Present / Wired / Install / Uninstall). Installing happens only on an explicit `rex ingest --install <name|auto>`; `--uninstall <name>` takes Rex's entries back out and leaves everything else. Do not special-case vendors in `live.go`. Tests run with HOME pointed at a temp dir (a `TestMain` in each package that touches HOME: `cli`, `plan`, `skill`, `tui`, `watch`), with `REX_EVENTS` unset. Installers edit the harness's config through `watch.UpdateJSONFile` + `internal/jsonedit`: splice only what Rex owns, keep every other byte, refuse a file that does not parse, keep a `.bak`, write atomically. Never round-trip a user's config through a `map`.

Kinds: `thought`, `text`, `user`, `command`, `question`, `result`, `idle`, `error`. Rex's own local kinds, `plan`, `drift` and `before` (source `rex`), never come from the feed.

## Plan files

`internal/plan` reads the v1 format in `docs/plan-format.md`. **Rex never writes a plan**, except the new empty v1 stub the human creates in the plan list (`+ Create 'x'`, the Name prompt), which never overwrites a file. The agent writes everything else, statuses and completion fields included, as `internal/skill/rex-plan/SKILL.md` teaches: it sets a task `active` to start it and `done` with `accomplished`, `modified`, `verified_how` and `completed_notes` to finish it, and Rex reloads and shows it. Never marshal a plan out of a struct or a `map` over a file that exists. What Rex remembers between runs (the last plan per repo) goes in `~/.rex/state.json`, never in the repo; an old file's `pins` are ignored. Rex reads the optional stack fields `base` and `branch` (docs/plan-format.md, Stacks) and reports bad ones as Problems; agents write them and work out where a stacked PR goes from the file. Rex never runs git.

## TUI

The target loop, key map and screen states are in `docs/ux.md`. TUI changes cite the section they implement.

- Left ~40 columns: mascot (at 66+ columns; narrower terminals are text-only, under 24 text columns Rex shows "widen" and ignores every key but Ctrl+C and Ctrl+L; under 7 rows nothing edits or submits the plan list's hidden draft, and its footer drops `type to …` and every enter but `enter open` on an empty draft (`withoutEnter`); see `layoutFor`). Right: the screen's text, on AGENT labeled log blocks (`thinking`, `run`, `ask`, `reply`, plus Rex's own `plan`, `drift` and `before rex started` lines, which never come from or go to the feed). Commands yellow, replies bright, thoughts muted. Skip file-look commands and `done`/result echoes. Each assistant `text` event is its own reply block.
- **Rows 0 to 2 are chrome on every screen** (docs/ux.md §4, `headerRow`, `statusRow`, `pageLine`). Row 0 is the tab bar: `REX` from 42 text cells, then both places, always in this order, the one on screen a pill (`[PLAN] AGENT`, `PLAN [AGENT]`; its brackets are painted in its fill, so they show only without color), with the cue cell between them (`•` news, `!` needs you, `app.unseen`, memory only: a reply, a question or a turn's error light AGENT's from PLAN; a plan change, an attach or an offer light PLAN's `•`, an unreadable plan (attached or reloaded) its `!`, which a good reload turns back to `•`; showing a place clears it, and a cue never moves you). The block is 12 cells, so the turn word after it never moves. On the right, while a plan is open, `following` or `latched` and the plan's repo-relative path, shortened only when it doesn't fit: drop `rex/plans/`, then `.rex.json`, then `…/` and its last part, then `…` and its tail down to 5 cells, then nothing. The word outranks the path's tail, judged against `thinking` and `following`, so it never comes and goes with the turn and both words share one cut: from 38 text cells (80 columns) it always shows, and below that it yields before the path. Row 1 is the status line, with the open plan's progress (`1/3`, `1/3 done` from 60 cells) right-aligned only where the line reads just as it would without it, or where only the pin's title clips for it: progress never costs row 1 its words. Row 2 is the page title, a slate bar with a breadcrumb (`PLANS › Send-off demo › card-highlight`) that is also the Esc ladder, and meta on the right; it is chrome (`ComposeOptions.Page`), never a body line; AGENT has one only from 12 rows. The footer is keys only (`fitFooter`): bold key words (`enter`, `esc`, `tab`, `?`, never `⏎`), two spaces apart, rank = display order, the last kept segment may shorten (`? help` → `?`); each screen's 38-cell footer is pinned by a test.
- User prompts: no `YOU` rule. Invisible column, at most half the pane, **right-aligned as a box**, **left-aligned text inside**. Color `UI["you"]`.
- Markdown → headings, lists, stripped emphasis, fenced code as `│ ` lines.
- **Start lands on a plan, not the plan list** (`attach.go`, docs/ux.md §11): the last plan opened in this repo (`~/.rex/state.json`), else the newest `rex/plans/**/*.rex.json`, else the **Wait** screen (`PLANS › waiting for a plan`: `no plan yet · tell your agent to make one`, `HOW IT GOES`, the esc and tab keys, and `agents learn plans from the rex-plan skill · rex ingest --install-skill` while `skill.Look` finds the skill nowhere; `rex-plan skill is out of date · rex ingest --install-skill [--project]` while a copy is an older Rex's; on a short screen the blank rows, the keys, HOW IT GOES and the long hint go first, never the headline or the short hint, and what doesn't fit scrolls; paths break after a `/` and the command is never clipped). Start usually lands on a plan instead, so an out-of-date skill is also a row-1 toast on the first frames (over the plan list's notice), and Help's feed block always has a `skill` row (`feedHealth.lines` takes `skill.Look`'s answer). Either plan is *following*: a completed write (never a read, a failed write or a shell command) of an existing `*.rex.json` under this repo's `rex/plans/` by this repo's agent opens that plan's outline in PLAN, the one exception to "events never move you" (docs/ux.md §1). The path jail (`planFile`): cleaned, symlinks resolved, under `rex/plans/` (a `..` counts by where it leads, a link out leads nowhere); `state.json`'s path gets it too. Plans are compared by file (`samePlan`, `os.SameFile`), never by spelling, and a write of the open plan never hides another plan written in the same batch. Text in the plan list's composer or a Name prompt (either place) holds the attach until it clears, and row 1 says `new plan <name> · opens when the line is clear` (`· opens when the prompt closes` while a Name prompt is open, even an empty one); nothing else does (no other screen has a composer), and the place never changes. An attach leaves one muted `plan` line on AGENT. The loop applies a read's keys before the events polled with it (`app.step`). While no plan is open a poll also looks for the newest plan file; once one is open the folder is never scanned to change the screen. Picking another plan in the plan list (or creating one there) *latches* it (`latch`); nothing else does, and enter on Continue picks nothing new and keeps following. A write held while you pick or create a plan there becomes the row-1 offer (`offerHeld`), neither opened nor lost, unless it is the plan you picked. Latched, another plan's write is only offered on row 1 (`new plan <name> · enter switch`), under the plan file's error, a toast and hard drift and over soft drift and the pin; enter on the outline, a detail or AGENT takes it and follows again, only while row 1 shows it and not while that would lose the plan list's draft or a Name prompt (then enter does what it did before and row 1 drops `· enter switch`). Rex never writes the plan to attach it; it records it as the repo's last plan in `state.json`, as any open does. Agents never write `state.json`. Esc from the outline or the Wait screen is the plan list (`PLANS`): Continue (the open plan), Create, Open, Open latest (cards with `2/5 · ▸ id` progress), then recents; with no plans only Create, plus a line saying the agent can write one; with no plan open, Esc or ← at Home goes back to Wait (its footer says `esc wait`). The cursor starts on Continue, else Open latest. Typing on Home finds across `rex/plans/**` and ends the list with `[ + Create 'x' ]` (enter creates it at once). Open browses `rex/plans/` like a file picker: cyan `▌` selection, orange folders, white plans, then green `[ + New folder ]` / `[ + New plan ]` below the listing; the cursor starts on the first folder or plan and a filter selects its first match. Names may be `a/b` (plan b in folder a). Selection follows the plan, not the row index, and long lists scroll with a two-line margin and `↑ more` / `↓ more`. Duplicate names warn and do not overwrite. Starting Rex never deletes anything and never creates a plan file or `rex/plans/` (it records the plan it opens in `~/.rex/state.json`): `rex/plans/` (with `.gitkeep`) is made on the first Create or New folder; a file (or dangling link) named `rex` at the root turns plans off with a notice instead of being removed; outside git, Home and Wait say where plans would go; an old `rex-plans/` folder is only reported, never moved. When start opens a plan, that notice is a row-1 toast. With plans off, Wait says only that and how to turn them on.
- Opening, creating or attaching a plan lands on its **outline** (`outline.go`): tasks in file order, `parent` as indent, glyphs `▸ ○ · ✓ – !`, the effective pin selected; the page title is the title row (Home or ↑ from the first task), enter / → for plan or task detail, Esc back up to the plan list. The open plan is re-read every 500 ms and compared by hash: edits keep the selection by task id, toast on row 1 and leave one `plan` line on AGENT (this is how the agent's pins and dones arrive); invalid JSON shows only after two polls and keeps the last good copy. Reading a plan never writes it.
- **The pin** is `plan.Graph.Pin()`: the first active leaf, else the first ready leaf (docs/ux.md §7). The agent writes it (`active` on the task, `pending` on any other active leaf, never a parent's status); Rex only shows it: row 1 (`▸ <id>  <title>`, with `(next up)` when computed), `▸` in the outline, drift. The agent knows the pin by re-reading the plan file each turn, as the skill says; nothing is injected and no hook writes stdout.
- **Done** is the agent's too (docs/ux.md §8): it sets the task `done` with its completion fields. When a reload shows the old pin done, row 1 toasts `✓ <id> done · next up <next>` (`· now <next>` when the same save started it, `· the plan is done` when every leaf is done or skipped, as the pin line judges it, `· nothing else is unblocked`; narrow, the done id clips first), AGENT gets the `plan` line, and task detail shows the retrospective. A later save that moves no task (the retrospective, the plan's own status, which `planChanges` reports as `plan → done`) keeps that toast up until it expires. Nothing is auto-pinned.
- **No send-off, brief, clipboard or notes** (docs/ux.md §9): the plan file is the hand-off. A new chat continues the plan when the human asks; the skill tells the agent how. Rex copies nothing and the human never pastes anything from Rex.
- **Drift** (`plan.CheckEdit` / `plan.CheckPrompt`, `drift.go`) judges live completed edits and user prompts in this repo against the effective pin: none for a listed file, glob, folder or `_test` sibling; soft for a file next to a listed one, a file another task lists, a prompt naming another task (`Drift.Names`; held until the turn ends, and dropped if the agent makes that task the pin first, since asking is how the pin moves), or a long prompt that shares no words with the pin; hard for anything that hits `scope.out` or an edit outside the pin's folders (with no files listed, only `scope.out` counts). Reads, `rex/plans/**`, other repos, denied writes and pre-start history never count. A batch that writes the open plan re-reads it at that save (`reloadDoc`): edits reported before it are judged against the old pin, edits after it against the pin it wrote, and a patch touching both counts each file at the lesser (`lesser`). It is display only: row 1 and one `drift · <pin>` block per pin on AGENT. Never in the feed, the plan, stdout, or anything sent to the agent.
- Two places, PLAN and AGENT; **Tab** toggles them and keeps each place's screen, scroll and overlay (and PLAN's draft). **The composer is only in the plan list** (docs/ux.md §2, `app.composing`): Home find, Browse filter, the Name prompt. There text is a find/filter/name unless it starts with `/` (`/help`, `/breakdance`), Backspace only deletes, Ctrl+U/Ctrl+W clear line/word, and Esc clears the draft before it climbs. Every other screen (Wait, outline, details, AGENT, Help) has no composer box (its body takes the rows, `layout.bare`), keys act at once, and every other printable key and every paste does nothing: no hidden draft is built. **`?` is a single key**: it opens Help at once on those screens, and in the plan list on an empty composer. **Esc** climbs one level and never switches place; the first Ctrl+C closes Help, clears the plan list's draft in PLAN, and arms quit.
- AGENT keys: ↑↓, page, Home / Ctrl+A (oldest), End / Ctrl+E (live), enter (takes a new-plan offer). Prompt boxes cap at 6 rows.
- Help (`overlay.go`) is H2 sections under the page title `<page it opened over> › HELP`: THIS PAGE (that page's keys; `app.helpOver` keeps that page's name while Help is open, and Help keeps its scroll, so a plan that opens under it swaps neither; the crumbs come from the page under Help every frame, so they stay the Esc ladder), EVERYWHERE, MARKS, PLAN LIST, REX NEVER, FEED (`feedHealth.lines`, with its `skill` row).
- Fit text by terminal **cells**: `cellLen` / `clip` / `padRight` / `wrap` (East Asian wide and emoji = 2, combining = 0, ambiguous = 1; hand-kept table in `width.go`). `clip` is the choke point: it also runs `watch.CleanText`, so agent or plan text can never paint escape sequences. Crop from the bottom; keep the mascot top-left.

## Commands and tests

```bash
go test ./...
go run ./cmd/rex start
```

CI: GitHub Actions, Ubuntu/macOS/Windows, Go 1.22 and 1.24: `go vet`, `go test`, then `go build -o dist/rex ./cmd/rex` (never `-o rex`; `rex/` is the plans directory). A separate job on stable Go runs gofmt and `go vet` for darwin, linux and windows, whose stdversion check catches APIs newer than go.mod's 1.22.

`internal/tui/cuj_test.go` holds the whole journey (wait → the agent writes the plan and Rex opens it → the agent pins a task → drift → the agent marks it done → AGENT and back → the plan list, where picking a plan latches → quit → start again on that plan, with the plan file still byte for byte what the agent wrote) against the keystroke budget in docs/ux.md §14 (opening last time's plan, creating one through the agent, seeing the pin, pinning and marking done take 0 keys in Rex), and a frame matrix (`matrixScreens`): every screen at 24/80/120 columns × 7/12/24 rows, plus 80×16 and 80×20, must fit, carry no control bytes, show the tab bar with its turn word, the page title on row 2 (AGENT's only from 12 rows) and a footer in words, and never `⏎`, `↔` or "tape"; and every screen's color frame, stripped of SGR, must be its NO_COLOR frame (`TestColorFrameSameText`). A new screen or overlay joins the matrix. The CUJ fixture is `internal/tui/testdata/sendoff-demo.rex.json`, a copy of the human demo `rex/plans/cuj/sendoff-demo.rex.json` (an agent working the demo must not break the tests).

After sprite or TUI changes, restart a running `rex start`. Tests do not replace looking at the terminal.

## Code conventions

- Match the code around you. Comments are rare and say *why*. Names are short (`b` for a browser, `ev` for an event, `opt` for compose options).
- Tests go in a `*_test.go` file in the same package. Use `t.TempDir()` for any repo, plans dir or feed file, and never touch the real `~` or `~/.rex`. Assert on rendered text with `viewText(BuildLogView(...))` / `viewText(BuildPlanView(...))`, and use `NO_COLOR`-style `Enabled: false` frames when you need plain text.
- `LogLine.Kind` picks the color. It is looked up in `mascot.UI`, then in `kindColor`. A new UI role goes in `mascot.UI`, not in a new palette. A line with more than one style carries `Spans` (`style.go`): its `Text` stays their plain join, a span without a role or fill takes the line's, and `paintSpans` cleans and clips them by cells like `clip`. Bold (`mascot.PaintStyle`) is the only attribute; no dim, reverse or underline. Yellow means the pin in PLAN, live agent work on AGENT, and news (the `•` cue, row 1's toasts and offer), plus the `REX` brand; titles, rules and parents are not yellow, except the outline's parent rows and Task detail's title and `retrospective` rule until the outline is redesigned (docs/ux.md §5). Color never changes the characters: a pill is `[WORD]` in both modes.
- Pane width math uses `cellLen` / `clip` / `padRight`, never `len()` or rune counts (`runeLen` is only for the draft cap).
- Whether a frame has the composer is `ComposeOptions.Composer`, set from `app.composing()`; drawing, the cursor and the footer read it, never their own guess.
- Vendor names live in `internal/watch` (adapters, installers), in the `rex ingest` flags, `--source` list and help in `internal/cli`, and in `scripts/install.*`. Adding a harness touches all three. The TUI paints event kinds and never branches on a vendor.
- Keep stdout clean in `rex ingest`. Hooks treat stdout as agent input, and exit code 2 as "block".

### Go 1.22 floor

`go.mod` says `go 1.22` and CI runs Go 1.22. Code built with a newer toolchain can still break the 1.22 floor:

- **The compiler** rejects newer language features, such as range-over-func, because `go.mod` sets `-lang`.
- **`go vet`** on Go 1.23+ flags any standard-library API newer than `go.mod` through its `stdversion` check. `go test` runs that check only on the newest toolchains (not on 1.23 or 1.24), so always run `go vet` explicitly, with a new toolchain. Examples it catches:
  - 1.23: `iter`, `maps.Keys/Values`, `slices.Collect/Sorted/Chunk`, `unique`
  - 1.24: `os.Root`, `t.Chdir`, `t.Context`, `b.Loop`, `strings.Lines/SplitSeq/FieldsSeq`, `bytes.Lines`
  - 1.25+: `testing/synctest`, `sync.WaitGroup.Go`, `t.Output`, `errors.AsType` (1.26), `encoding/json/v2` (1.27)
- **Nothing catches struct-tag options.** `json:",omitzero"` (1.24) compiles on 1.22 and is ignored there, so the same code writes different JSON depending on the toolchain. Don't use it. Rex writes the feed, `state.json` and new plan stubs as JSON.

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
