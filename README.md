<p align="center">
  <img src="assets/rex.png" width="200" alt="REX — a pixel-art dinosaur mascot" />
</p>

<h1 align="center">REX</h1>

<p align="center">
  <strong>Small dino. Big opinions.</strong>
</p>

<p align="center">
  Your coding agent writes and works the plan;<br />
  watch it and its work from a terminal that stays out of the way.
</p>

<p align="center">
  <a href="https://github.com/luciancheng/rex/actions/workflows/ci.yml"><img src="https://github.com/luciancheng/rex/actions/workflows/ci.yml/badge.svg?branch=dev" alt="CI" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/luciancheng/rex" alt="License: MIT" /></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/luciancheng/rex" alt="Go version" /></a>
  <a href="https://github.com/luciancheng/rex/stargazers"><img src="https://img.shields.io/github/stars/luciancheng/rex" alt="GitHub stars" /></a>
</p>

<p align="center">macOS · Linux · Windows 10+</p>

Rex is a terminal sidecar for developers who work with an AI coding agent: Cursor, Claude Code, Codex or OpenCode. You keep talking to your agent as usual. Rex sits in a second terminal with a pixel-art dino and shows two tabs, always both on its top row (`[PLAN] AGENT`: the one on screen is highlighted, or in brackets without color):
- **PLAN**: a JSON file in your repo that breaks the work into PR-sized tasks;
- **AGENT**: what the agent is doing right now, as it happens: your prompts, its thoughts (a run of them folds to one line with the latest), the commands it runs, failed steps, questions and replies.

Rex is **watch-only**, with one thing it hands over: before each prompt, the task pinned in your plan reaches the agent as a short note (Claude Code, Codex, Cursor and OpenCode). It never runs tools, approves them or steers the agent, and it never edits your plan. The agent drives the plan: you ask it to plan, to work a task or to continue, and it writes the plan file (which task is active, what is done and how it was checked). Rex shows each change as it lands. There is nothing to copy from Rex into the chat.

```
REX [PLAN] AGENT idle            following rex/plans/cuj/sendoff-demo.rex.json
▸ card-highlight  Selected plan card is one block                     1/3 done
  PLANS › Send-off demo                                                active
  A teammate opens this plan in Rex and sees what the work is and which task …

╭─[PINNED]─ card-highlight · Selected plan card is one block ────────────────╮
│ does    Bar, border and fill cover all three rows of the selected card     │
│ needs   the pick and pickbg colors exist in mascot.UI                      │
│ verify  go test ./internal/tui -run PlanCardSelection                      │
│         Open latest: ▌ on all three card rows                              │
│ files   plans.go  live.go  plans_test.go                                   │
│ done    ✓ plans-ui  Home cards, folder browser, duplicate names refused    │
╰────────────────────────────────────────────────────────────────────────────╯

TASKS ─────────────────────────────────────────────────────────────── 1/3 done
  ✓ plans-ui        Plans navigator · Home cards, folder browser, duplicate n…
▌ ▸ card-highlight  Selected plan card is one block · Bar, border and…  pinned
  · load-outline    Outline view · Opening a plan shows…  after card-highlight

esc plans  tab agent  enter open  ? help  ↑↓ move
```

The top row is the tabs (a `•` between them means the other tab has news, `!` that it needs you), the turn and the open plan; the second, the task the agent is on and the plan's progress; the third, where you are (Esc goes back along it). Under the plan's goal, the **PINNED** card is what a fresh chat needs to work that task, read from the plan file: what it does and needs, how to check it, its files, and the done task it builds on (`NEXT UP` when no task is active yet). Then every task, the pinned one in yellow, each done one with what it accomplished. The bottom row lists the keys that work here.

## Requirements

- [Go 1.22+](https://go.dev/dl/). There are no prebuilt binaries yet, so both installs build from source.
- macOS, Linux or Windows 10+, and a terminal at least 24 columns wide (66+ to show the dino). It works in Terminal.app, iTerm, Windows Terminal and the VS Code terminal.
- A git repo for your plans (outside one, Rex says where plans would go).

## Install

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/luciancheng/rex/HEAD/scripts/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/luciancheng/rex/HEAD/scripts/install.ps1 | iex
```

The script builds `rex` into `~/.local/bin`. PowerShell adds that folder to your user PATH; the shell script prints the `export PATH=…` line when it isn't on yours yet. If Cursor is installed, the script wires it. For Claude Code, Codex and OpenCode it prints the one command that wires each one it finds. To install a specific tag, branch or commit, set `REX_REF`, for example `curl -fsSL …/install.sh | REX_REF=dev bash`.

Or with `go install`:

```bash
go install github.com/luciancheng/rex/cmd/rex@latest
```

This puts `rex` in `$(go env GOPATH)/bin`. Then wire your agent (see [Agents](#agents)).

## Quick start

1. Wire your agent (see [Agents](#agents)) and teach it the plan format with `rex ingest --install-skill`. Open a new terminal, `cd` into your repo and run `rex start`. It opens the plan you had open last time, else the newest plan in `rex/plans/`, else waits for one.
2. Ask your agent to plan the work in `rex/plans/<name>.rex.json`. Rex opens the outline as soon as the agent writes the file, and updates it as the agent fills it in.
3. Ask your agent to work the plan ("work the plan", "continue rex/plans/<name>.rex.json"). It sets the task it starts to `active`, and Rex shows it as the pin on the status line and on the outline's PINNED card. When it finishes, it marks the task `done` with what it did, what it changed and how it checked, and Rex shows `✓ <task> done · next up <next>`; the card moves on to the next task. Enter on a task shows all of it; enter on the plan's title row shows its goal, scope and everything done so far. Each task is one PR; ask it for the next one when you're ready.
4. Press Tab to watch the agent while it works, and Tab again to go back to the plan. `?` shows every key.
5. The top row says `following` while Rex opens each plan your agent writes. Pick another plan yourself (Esc, then pick it from the list, or type a name and enter) and it says `latched`: a plan the agent writes then waits on the status line until you press enter.
6. Ctrl+C twice quits.

**How does the agent know the pinned task?** In Claude Code, Codex and Cursor the prompt hook, and in OpenCode Rex's plugin, adds it to every prompt you send: a short, factual note that starts with `Rex note`, read from the plan file just then (what the task does and needs, how to verify it, its files, what it builds on, what is out of scope). It is sent only while a task is `active` in the plan Rex last opened in this repo (a task that is only next up is never sent, and a plan you never opened in Rex sends nothing, whatever it pins), it is capped at 1,500 bytes, and it is left out if it isn't ready within 750 ms of the hook starting (OpenCode's plugin waits for it at most 1.5 s, then sends the prompt without it). While nothing is pinned and the plan Rex last opened in this repo has no tasks yet, the note just names that plan, so "plan this" works. With nothing pinned and no such empty plan, nothing is sent: that is the off switch. Cursor reads the note on every prompt in recent builds (seen working in Cursor 3.20 and its CLI 2026.10; the field Rex uses there is undocumented, and an older build, such as 3.2, ignores it); it also gets it at least once per chat, from a `sessionStart` hook, as of the moment the chat starts. To stop the note, set the task back to `pending` (ask your agent, or edit its `status`); `rex ingest --uninstall claude` (or `codex`, `cursor` or `opencode`) takes Rex's hooks out altogether. The `rex-plan` skill also tells the agent to re-read the plan at the start of every turn and work its `active` task, so a fresh chat picks up where the last one stopped when you say "continue the plan".

If the agent edits files outside the pinned task, or you ask for something the plan's `scope.out` rules out, Rex shows **drift** on the status line and the AGENT tab. It never corrects the agent.

## Agents

Rex watches agents through their hooks: each wired agent runs `rex ingest` on its events and appends them to a local feed. Apart from the install script wiring Cursor, nothing is wired until you run `rex ingest --install`.

| Agent | Wire it | Rex edits | Notes |
| --- | --- | --- | --- |
| Cursor | the install script, or `rex ingest --install cursor` | `~/.cursor/hooks.json` | Cursor reports only the final reply of each turn. The prompt hook adds the pinned task to each prompt on recent Cursor builds (seen working in 3.20 and the CLI 2026.10), and a `sessionStart` hook adds it once when each chat starts. After upgrading from a Rex without it, run `rex ingest --install cursor` again to add that hook. Rex's entries for both go first in `~/.cursor/hooks.json`, so your own hooks there answer after Rex's. Rex's prompt reply says `"continue": true`, and Cursor doesn't document how that merges with another hook's block: Enterprise, Team and project hooks always answer before user hooks, so a prompt blocker there (a secret scanner, say) must not count on outlasting it. Cursor may also keep only one hook's context per event, so Rex's note can replace another hook's or be replaced by it ([SECURITY.md](SECURITY.md)). |
| Claude Code | `rex ingest --install claude` (add `--project` for this repo only) | `~/.claude/settings.json` (or `$CLAUDE_CONFIG_DIR`), or this repo's git-excluded `.claude/settings.local.json` | The prompt hook adds the pinned task to each prompt. |
| Codex | `rex ingest --install codex` | `~/.codex/hooks.json` (or `$CODEX_HOME`) | Then trust the hooks once in Codex's `/hooks`, and again after moving `rex` (the hook names its path). The prompt hook now also adds the pinned task to each prompt; the hook itself is unchanged, so hooks you already trusted stay trusted. |
| OpenCode | `rex ingest --install opencode` | `~/.config/opencode/plugins/rex.js` (or `$XDG_CONFIG_HOME`) | Restart OpenCode to load the plugin. The plugin adds the pinned task to each prompt, out of your sight (it waits for Rex at most 1.5 s; the prompt goes on without the note if Rex is slower). After upgrading from a Rex without it, run `rex ingest --install opencode` again, then restart OpenCode. Rex's plugin needs OpenCode 1.x: the V2 CLI doesn't load it. |

- `rex ingest --install auto` wires every agent found on this machine.
- Your other hooks and settings are kept byte for byte, and the previous file is saved next to it as `.bak`.
- `rex ingest --install-skill` installs the `rex-plan` skill for all four agents: `~/.claude/skills/rex-plan` for Claude Code (with `CLAUDE_CONFIG_DIR` set it goes there instead, and a copy Rex left in `~/.claude/skills`, which Cursor and OpenCode still read, is updated too) and `~/.agents/skills/rex-plan` for Codex. Cursor and OpenCode read both folders and may list the skill twice; the copies are identical. Add `--project` to put both copies in this repo's `.claude/skills` and `.agents/skills` instead. It teaches the plan format, how to work a plan (start a task, mark it done, re-read the file every turn, read Rex's notes), stacked PRs and a review pass before each PR. Run it again (with `--project` where you used that) after upgrading Rex: an older copy keeps teaching the older workflow. It never replaces a copy a newer Rex wrote (a teammate's, say); it says so and leaves it.

## Keys

Keys act at once, and every screen's bottom row lists the ones that work there. The only place you type is the plan list, to find, filter or name a plan.

| Key | Does |
| --- | --- |
| Tab | switch between PLAN and AGENT |
| Enter | open the selection; switch to a plan the agent wrote when the status line offers it |
| Esc | go up one level, back along the page title (in the plan list, clear what you typed first) |
| ↑ ↓ · PgUp PgDn | move, or scroll |
| ← → | up a level · into a folder or a task's detail |
| Home End | first and last; on AGENT, oldest and live (also Ctrl+A, Ctrl+E) |
| `?` | every key, and feed health |
| Ctrl+L | repaint |
| Ctrl+C twice | quit |

The plan list (Esc from the outline) groups the open plan, the plans you opened before it and the newest others, and the folders under `rex/plans/`, each plan once, and the selected plan opens in place to show its goal, its pinned task, what comes next and what was last done. There, typing finds a plan or names a new one, Ctrl+U and Ctrl+W clear the line or a word, and a leading `/` runs a verb: `/help`, `/breakdance` (one dance). Pinning a task, marking it done and handing off to a fresh chat are your agent's job: ask it. The full key map is in [docs/ux.md](docs/ux.md).

## Plans

Plans live in the repo at `rex/plans/` as `<name>.rex.json`; folders group them (`rex/plans/onboarding/auth.rex.json`). A plan is a goal, hard scope edges, and a flat list of PR-sized tasks, with `parent` for the outline and `depends_on` for order. A plan can also be a stack of PRs, one per task; the agent works out from the plan which branch to cut and which PR to stack on. The format is [docs/plan-format.md](docs/plan-format.md).

Commit the `rex/plans/` folder so teammates open the same plans from git. A plan that is a stack of PRs is committed before the stack starts or after it lands, not on its branches (see [Stacks](docs/plan-format.md#stacks)).

## What Rex stores

Rex makes no network connections, and everything stays on this machine:

- **`~/.rex/session.jsonl`** (and the `session.jsonl.lock` next to it): the feed every wired agent appends to. It holds your prompts (and, for one that carried the pin, the task's id and its plan's path), the agent's thoughts and replies, the commands it runs, file paths and working folders, from every repo on this machine. File bodies and diffs are left out, and each event's text is capped at 32 KB. Only your user can read it (`0600`, in a `0700` folder). It only grows; delete it whenever you like. Set `REX_EVENTS` to keep it somewhere else.
- **`~/.rex/state.json`**: the last plan opened in each repo (by you, or followed because your agent wrote it), the only plan whose pinned task the prompt hooks hand your agent.
- **Your repo**: Rex never edits a plan. `+ New plan`, `+ Create` and `+ New folder` in the plan list write a new empty plan or a folder (and `rex/plans/.gitkeep` the first time), never over an existing file. Everything else in `rex/plans/` is written by your agent or by you.
- **Agent configs**: only the files in the [Agents](#agents) table, each with a `.bak` copy of what was there, and only when you run `--install`. `--install claude --project` also adds two lines to the repo's `.git/info/exclude`, and `--install-skill` writes the two `rex-plan` skill folders (and, with `CLAUDE_CONFIG_DIR` set, updates a copy Rex left in `~/.claude/skills`).

The install script downloads Rex's source through Go and git.

## Configuration

| Setting | Effect |
| --- | --- |
| `REX_EVENTS` | where the feed lives (default `~/.rex/session.jsonl`). Hooks read it from the agent's environment. |
| `rex start --events <path>` · `rex ingest --events <path>` | the same, for one run |
| `NO_COLOR` · `rex start --no-color` | no colour |
| `FORCE_COLOR` | colour even when the output isn't a terminal |
| `TERM=dumb` · `rex start --static` | print one static frame and exit (pipes and CI do this too) |
| `CLAUDE_CONFIG_DIR` · `CODEX_HOME` · `XDG_CONFIG_HOME` | where Claude Code's, Codex's and OpenCode's configs live |
| `REX_REF` · `REX_REPO` · `REX_BIN` | for the install script: what to build, from which repo, into which folder |

## Uninstall

1. Take Rex out of each agent you wired. Your other hooks are left alone.
   ```bash
   rex ingest --uninstall cursor
   rex ingest --uninstall claude      # in each repo you wired with --project: rex ingest --uninstall claude --project
   rex ingest --uninstall codex
   rex ingest --uninstall opencode
   rex ingest --uninstall-skill       # if you installed it; every copy Rex wrote (add --project in each repo)
   ```
2. Then delete the binary: `rm "$(command -v rex)"` (PowerShell: `Remove-Item ~\.local\bin\rex.exe`, then take that folder out of your user PATH). Do this last: hooks call `rex` by its path, so deleting it first leaves them pointing at nothing.
3. Delete Rex's data: `rm -rf ~/.rex` (PowerShell: `Remove-Item -Recurse -Force ~\.rex`), and the file `REX_EVENTS` points to if you set it. The `.bak` files next to your agent configs still hold Rex's hooks: delete them, don't restore them. Your plans in `rex/plans/` are yours to keep or delete.

## Troubleshooting

- **The AGENT tab stays empty.** Press `?`. The FEED block shows the feed path, the last event, and whether each agent is wired, with the command that wires it. Start a new agent session after wiring.
- **Codex shows nothing.** Trust the Rex hooks in Codex's `/hooks`, and again after moving `rex`.
- **Events stopped after moving `rex`, or installing it to a different folder.** Hooks call the binary by its path. Run the `rex ingest --install <agent>` command again.
- **Events land in another file.** Hooks use the agent's `REX_EVENTS`, not your shell's. Set it in both places, or in neither.
- **No dino, or "widen".** The dino needs 66+ columns. Under 24 columns of text Rex asks you to widen the terminal.
- **The agent doesn't get the pinned task.** Claude Code, Codex, Cursor and OpenCode get the note, and only while a task is `active` in the plan Rex last opened in that repo: open that plan in Rex once. Any other plan sends nothing, even one with an `active` task. A subagent never gets it. Cursor takes it before each prompt only on recent builds (3.20 and the CLI 2026.10 were seen taking it); it also gets it when a chat starts, if `~/.cursor/hooks.json` has Rex's `sessionStart` entry (run `rex ingest --install cursor` again to add it), and that note is as of the chat's start: a pin moved later reaches it only through the plan file. Cursor's cloud agents don't run your user hooks. OpenCode needs the plugin from this Rex: run `rex ingest --install opencode` again, then restart OpenCode.
- **The agent still gets a task you have moved on from.** It is still `active` in the plan. Ask your agent to set it back to `pending` (or edit its `status` yourself), or to finish it.
- **The agent doesn't pin or finish tasks in the plan.** Install the skill (`rex ingest --install-skill`, then start a new agent session), and ask it to work the plan. The wait screen and Help (`?`, the `skill` row) say when the skill is installed nowhere Rex knows, or when a copy is from an older Rex (run the command they show); an older copy is also a status-line notice when Rex starts on a plan.
- **"rex/ is a file here · plans off".** A file named `rex` sits at the repo root, where the plans folder goes. Move it.

## Contributing

Contributions are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md), and [AGENTS.md](AGENTS.md) for the code conventions, the workflow and the pre-PR gate. To hack on it:

```bash
git clone https://github.com/luciancheng/rex.git && cd rex
go test ./...
go run ./cmd/rex start
go build -o dist/rex ./cmd/rex     # not -o rex: rex/ is the plans folder
```

Please report security issues privately: see [SECURITY.md](SECURITY.md).

## Authors

- [Nicholas Jano](https://github.com/nicholasjano)
- [Lucian Cheng](https://github.com/luciancheng)
- [Brian Manomaisupat](https://github.com/brianmano)

And [everyone who has contributed](https://github.com/luciancheng/rex/graphs/contributors).

## License

MIT. See [LICENSE](LICENSE).

The idle dino (`assets/idle.png`, and `assets/rex.png` scaled from it) was drawn by hand by Lucian Cheng and is MIT like the code. The breakdance and typing sheets (`assets/breakdance.png`, `assets/typing.png`) were made with an AI image generator.
