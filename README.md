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

Rex is a terminal sidecar for developers who work with an AI coding agent: Cursor, Claude Code, Codex or OpenCode. You keep talking to your agent as usual. Rex sits in a second terminal with a pixel-art dino and shows two places, named at the end of its top row (`plan · agent`, the one on screen brighter), and Tab switches between them:
- **PLAN**: a JSON file in your repo that breaks the work into PR-sized tasks;
- **AGENT**: what the agent is doing right now, as it happens: your prompts, its thoughts (a run of them folds to one line with the latest), the commands it runs, failed steps, questions and replies.

Rex is **watch-only**, with one thing it hands over: before each prompt, the task pinned in your plan reaches the agent as a short note (Claude Code, Codex, Cursor and OpenCode). It never runs tools, approves them or steers the agent, and it writes nothing into your plan but status values when you press `p` or `d` (the task's, any task a pin moves off, and the plan's own top-level `status`). You and the agent both drive the plan: you ask it to plan, to work a task or to continue, and it writes the plan file (which task is active, what is done and how it was checked); you can also pin a task (`p`) or mark one done (`d`) in Rex. Rex shows each change as it lands, and who made it. There is nothing to copy from Rex into the chat.

```
Send-off demo                               1/3 done · following   plan · agent
pinned  card-highlight · Selected plan card is one block  sent with last prompt

  ✔ plans-ui        Plans navigator
❯   card-highlight  Selected plan card is one block                      pinned
    load-outline    Outline view

Opened Send-off demo · rex/plans/cuj/sendoff-demo.rex.json
↑↓ select · enter details · i about · esc plans · tab agent · ? help
```

The top row says where you are (Esc goes back along it), the plan's progress, that Rex follows the plans your agent writes, and which place you are in; the second, the task that is pinned. Then every task, one row each: a green `✔` on the done ones, the pinned one tagged `pinned` in yellow (`next up`, muted, while no task is active yet), and a purple `❯` on the one your cursor is on. Enter opens a task's details on a page of their own: the fields your agent gets with every prompt while that task is pinned (what it does and needs, what it waits on, how to check it, its files, the done task it builds on), or, for a done task, what your agent wrote when it finished it. `i` shows the plan's goal, scope and progress. Yellow is only ever the pin (and an edit that went off it). The row above the bottom one says what just happened, one thing at a time: which plan opened, the plan changed, a task got done, your agent is asking you something, or it wrote another plan (`o` opens it). The bottom row lists the keys that work here.

## Requirements

- [Go 1.22+](https://go.dev/dl/). There are no prebuilt binaries yet, so both installs build from source.
- macOS, Linux or Windows 10+, and a terminal at least 24 columns wide. The dino shows from 110 columns and 16 rows; on a smaller terminal the plan gets the whole width. It works in Terminal.app, iTerm, Windows Terminal and the VS Code terminal.
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

1. Wire your agent (see [Agents](#agents)) and teach it the plan format with `rex ingest --install-skill`. Open a new terminal, `cd` into your repo and run `rex start`. It starts on Plans, with the plan you had open last time selected as **Continue**: press enter to open it. **Open latest** is the newest plan, **Open a plan** browses `rex/plans/`, and **Create a plan** makes an empty one you name (`team/auth` puts it in a folder); typing finds a plan by name or title.
2. Or ask your agent to plan the work in `rex/plans/<name>.rex.json`. Rex opens its task list as soon as the agent writes the file, and updates it as the agent fills it in.
3. Ask your agent to work the plan ("work the plan", "continue rex/plans/<name>.rex.json"). It sets the task it starts to `active`, and Rex shows it as the pin on the second row and tags it `pinned` in the list. When it finishes, it marks the task `done` with what it did, what it changed and how it checked, and Rex shows `Your agent finished <task> ✔ · next up <next>`; the tag moves on to the next task. You can pin a task yourself with `p` (your next prompt hands it to the agent) and mark one done with `d`; the same key puts the task back (the plan's own `status` may stay as the first key set it). Enter on a task shows all of it; `i` shows the plan's goal, scope and progress. Each task is one PR; ask it for the next one when you're ready.
4. Press Tab to watch the agent while it works, and Tab again to go back to the plan. `?` shows every key.
5. The top row says `following` while Rex opens each plan your agent writes, as it does after Continue or Open latest. Pick another plan yourself (Esc to Plans, then a plan under Recent, Open a plan, or type a name and enter) and Rex stays on it: a plan the agent writes then waits on the row above the keys, `Your agent wrote … · o opens it`, and in Open latest, until you open it.
6. Ctrl+C twice quits.

**How does the agent know the pinned task?** In Claude Code, Codex and Cursor the prompt hook, and in OpenCode Rex's plugin, adds it to every prompt you send: a short, factual note that starts with `Rex note`, read from the plan file just then (what the task does and needs, how to verify it, its files, what it builds on, what is out of scope). It is sent only while a task is `active` in the plan Rex last opened in this repo (a task that is only next up is never sent, and a plan you never opened in Rex sends nothing, whatever it pins), it is capped at 1,500 bytes, and it is left out if it isn't ready within 750 ms of the hook starting (OpenCode's plugin waits for it at most 1.5 s, then sends the prompt without it). While nothing is pinned and the plan Rex last opened in this repo has no tasks yet, the note just names that plan, so "plan this" works. With nothing pinned and no such empty plan, nothing is sent: that is the off switch. Cursor reads the note on every prompt in recent builds (seen working in Cursor 3.20 and its CLI 2026.10; the field Rex uses there is undocumented, and an older build, such as 3.2, ignores it); it also gets it at least once per chat, from a `sessionStart` hook, as of the moment the chat starts. To stop the note, set the task back to `pending` (ask your agent, or edit its `status`); `rex ingest --uninstall claude` (or `codex`, `cursor` or `opencode`) takes Rex's hooks out altogether. The `rex-plan` skill also tells the agent to re-read the plan at the start of every turn and work its `active` task, so a fresh chat picks up where the last one stopped when you say "continue the plan". Rex shows whether it went: the pin line says `sent with last prompt` once a prompt carried it (Rex reads that from the feed, so it never claims more than happened), `sends with next prompt` until then, or `not sent · ? help` when no wired agent can take it, and AGENT shows `with pin <id>` under each prompt that carried it.

If the agent edits files outside the pinned task, or you ask for something the plan's `scope.out` rules out, Rex shows **drift** on the row above the keys (`off pin · …`) and on the AGENT tab. It never corrects the agent.

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

Keys act at once, and every screen's bottom row lists the ones that work there. The only place you type is Plans, to find, filter or name a plan.

| Key | Does |
| --- | --- |
| Tab | switch between PLAN and AGENT |
| Enter | open the selection |
| `p` | on a task: pin it (your agent gets it with your next prompt), or unpin the pinned one |
| `d` | on a task: mark it done, or reopen a done one |
| `o` | open a plan the agent wrote, when the row above the keys offers it (it names `o`) |
| Esc | go up one level, back along the title on the top row (on Plans, clear what you typed first) |
| ↑ ↓ · PgUp PgDn | move, or scroll |
| ← → | up a level · into a folder or a task's detail |
| Home End | first and last; on AGENT, oldest and live (also Ctrl+A, Ctrl+E) |
| `?` | every key, and feed health |
| Ctrl+L | repaint |
| Ctrl+C twice | quit |

Plans (where Rex starts, and Esc from the task list) has four cards, each only when it does something: Continue (the plan you had open), Create a plan, Open a plan (browse `rex/plans/`, folders then plans) and Open latest (the newest plan); then Recent, the plans you opened before and the newest others, each plan once. Continue and Open latest follow the plans your agent writes; any other pick stays put. There, typing finds a plan by name or title, or names a new one (`+ Create <name>`), Ctrl+U and Ctrl+W clear the line or a word, and a leading `/` runs a verb: `/help`, `/breakdance` (one dance). Pinning a task, marking it done and handing off to a fresh chat are your agent's job: ask it. The full key map is in [docs/ux.md](docs/ux.md).

## Plans

Plans live in the repo at `rex/plans/` as `<name>.rex.json`; folders group them (`rex/plans/onboarding/auth.rex.json`). A plan is a goal, hard scope edges, and a flat list of PR-sized tasks, with `parent` for the outline and `depends_on` for order. A plan can also be a stack of PRs, one per task; the agent works out from the plan which branch to cut and which PR to stack on. The format is [docs/plan-format.md](docs/plan-format.md).

Commit the `rex/plans/` folder so teammates open the same plans from git. A plan that is a stack of PRs is committed before the stack starts or after it lands, not on its branches (see [Stacks](docs/plan-format.md#stacks)).

## What Rex stores

Rex makes no network connections, and everything stays on this machine:

- **`~/.rex/session.jsonl`** (and the `session.jsonl.lock` next to it): the feed every wired agent appends to. It holds your prompts (and, for one that carried the pin, the task's id and its plan's path), the agent's thoughts and replies, the commands it runs, file paths and working folders, from every repo on this machine. File bodies and diffs are left out, and each event's text is capped at 32 KB. Only your user can read it (`0600`, in a `0700` folder). It only grows; delete it whenever you like. Set `REX_EVENTS` to keep it somewhere else.
- **`~/.rex/state.json`**: the last plan opened in each repo (by you, or followed because your agent wrote it), the only plan whose pinned task the prompt hooks hand your agent.
- **Your repo**: Rex never edits a plan beyond its `status` values when you press `p` or `d` (the task's, any task a pin moves off, and the plan's own top-level `status`: the same values your agent writes; only those bytes change, the file is re-checked just before an atomic rename, a task that changed since you looked is refused, and a plan that is a link out of `rex/plans/` is never written). Create a plan and `+ Create` on Plans write a new empty plan (and `rex/plans/.gitkeep` and any folders it names the first time), never over an existing file. Everything else in `rex/plans/` is written by your agent or by you.
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
- **No dino, or "widen".** The dino shows only from 110 columns and 16 rows, so a smaller terminal gives the plan all of its width. Under 24 columns Rex asks you to widen the terminal.
- **The agent doesn't get the pinned task.** The pin line says whether it went; `not sent · ? help` means no wired agent takes it, and `?`'s FEED says which command fixes that, agent by agent. Claude Code, Codex, Cursor and OpenCode get the note, and only while a task is `active` in the plan Rex last opened in that repo: open that plan in Rex once. Any other plan sends nothing, even one with an `active` task. A subagent never gets it. Cursor takes it before each prompt only on recent builds (3.20 and the CLI 2026.10 were seen taking it); it also gets it when a chat starts, if `~/.cursor/hooks.json` has Rex's `sessionStart` entry (run `rex ingest --install cursor` again to add it), and that note is as of the chat's start: a pin moved later reaches it only through the plan file. Cursor's cloud agents don't run your user hooks. OpenCode needs the plugin from this Rex: run `rex ingest --install opencode` again, then restart OpenCode.
- **The agent still gets a task you have moved on from.** It is still `active` in the plan. Ask your agent to set it back to `pending` (or edit its `status` yourself), or to finish it.
- **The agent doesn't pin or finish tasks in the plan.** Install the skill (`rex ingest --install-skill`, then start a new agent session), and ask it to work the plan. Plans (before your first plan) and Help (`?`, the `skill` row) say when the skill is installed nowhere Rex knows, or when a copy is from an older Rex (run the command they show); an older copy is also a notice when Rex starts.
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
